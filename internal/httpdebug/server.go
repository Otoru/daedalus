// Package httpdebug implements the optional local HTTP development
// server of the Daedalus subprocess.
package httpdebug

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	daedalusv1 "github.com/Otoru/daedalus/internal/gen/go/daedalus/v1"
	"github.com/Otoru/daedalus/internal/service"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const (
	// MaxHTTPDebugBodyBytes is the normative 1 MiB limit from section 2.3.
	MaxHTTPDebugBodyBytes = 1 << 20

	requestIDBytes = 16
	debugIndexPath = "assets/index.html"
	debugCSSPath   = "assets/styles.css"
	debugJSPath    = "assets/app.js"
	// The specification does not define the status of a request whose client
	// canceled the connection. The conservative reading uses the conventional
	// code 499, without turning it into an internal 500 failure.
	clientClosedRequestStatus = 499
)

type requestIDContextKey struct{}

//go:embed assets/*
var assets embed.FS

// Server adapts the same gRPC service to the HTTP debug routes.
type Server struct {
	service *service.Server
	version string
	logger  *zap.Logger
	handler http.Handler
}

// New creates the HTTP handler without opening a listener or starting goroutines.
func New(serviceServer *service.Server, version string, logger *zap.Logger) *Server {
	if serviceServer == nil {
		panic("serviceServer cannot be nil")
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	server := &Server{
		service: serviceServer,
		version: version,
		logger:  logger,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /", server.root)
	mux.HandleFunc("GET /debug/", server.debug)
	mux.HandleFunc("POST /api/v1/generate", server.generate)
	server.handler = server.observeAndRestrict(mux)
	return server
}

// Handler returns the HTTP routes with origin validation and logging.
func (server *Server) Handler() http.Handler {
	return server.handler
}

func (server *Server) observeAndRestrict(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		requestID := newRequestID()
		observed := &statusWriter{ResponseWriter: writer, status: http.StatusOK}
		request = request.WithContext(context.WithValue(
			request.Context(), requestIDContextKey{}, requestID,
		))
		defer func() {
			server.logger.Info(
				"debug HTTP request completed",
				zap.String("request_id", requestID),
				zap.Int("status", observed.status),
				zap.Duration("duration", time.Since(started)),
			)
		}()

		if err := validateLocalRequest(request); err != nil {
			// Section 2.3 requires refusing non-local access, but does not
			// fix the status. The conservative interpretation uses 403 so
			// authentication is not implied to make the origin supported.
			writeError(observed, request, http.StatusForbidden, "access_denied", "local access refused")
			return
		}
		next.ServeHTTP(observed, request)
	})
}

func (server *Server) health(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	statusText := "ok"
	statusCode := http.StatusOK
	if !server.service.Serving() {
		statusText = "unavailable"
		statusCode = http.StatusServiceUnavailable
	}
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}{Status: statusText, Version: server.version})
}

func (server *Server) root(writer http.ResponseWriter, request *http.Request) {
	// Section 2.3 requires a redirect, but does not choose the code. The
	// conservative interpretation uses 307, which does not rewrite the method.
	http.Redirect(writer, request, "/debug/", http.StatusTemporaryRedirect)
}

func (server *Server) debug(writer http.ResponseWriter, request *http.Request) {
	var assetPath string
	var contentType string
	switch request.URL.Path {
	case "/debug/":
		assetPath = debugIndexPath
		contentType = "text/html; charset=utf-8"
	case "/debug/styles.css":
		assetPath = debugCSSPath
		contentType = "text/css; charset=utf-8"
	case "/debug/app.js":
		assetPath = debugJSPath
		contentType = "text/javascript; charset=utf-8"
	default:
		// Section 2.3 does not define a fallback for unknown asset
		// paths. The conservative reading returns 404 instead of
		// masking a missing resource with the page HTML.
		http.NotFound(writer, request)
		return
	}

	content, err := assets.ReadFile(assetPath)
	if err != nil {
		http.Error(writer, "debug asset unavailable", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", contentType)
	_, _ = writer.Write(content)
}

func (server *Server) generate(writer http.ResponseWriter, request *http.Request) {
	// Optional MIME parameters, such as charset, do not change the media type
	// application/json required by section 2.3.
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(
			writer, request, http.StatusBadRequest,
			"invalid_content_type", "Content-Type must be application/json",
		)
		return
	}
	if request.ContentLength > MaxHTTPDebugBodyBytes {
		writeError(
			writer, request, http.StatusRequestEntityTooLarge,
			"body_too_large", "request body exceeds the 1 MiB limit",
		)
		return
	}

	request.Body = http.MaxBytesReader(writer, request.Body, MaxHTTPDebugBodyBytes)
	body, err := io.ReadAll(request.Body)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(
				writer, request, http.StatusRequestEntityTooLarge,
				"body_too_large", "request body exceeds the 1 MiB limit",
			)
			return
		}
		writeError(writer, request, http.StatusBadRequest, "invalid_json", "could not read the JSON body")
		return
	}

	var protoRequest daedalusv1.GenerateRequest
	if err := validateCanonicalRequest(body, protoRequest.ProtoReflect().Descriptor()); err != nil {
		writeError(writer, request, http.StatusBadRequest, "invalid_json", "invalid ProtoJSON request")
		return
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, &protoRequest); err != nil {
		writeError(writer, request, http.StatusBadRequest, "invalid_json", "invalid ProtoJSON request")
		return
	}

	response, err := server.service.Generate(request.Context(), &protoRequest)
	if err != nil {
		httpStatus, code, message := mapServiceError(err)
		writeError(writer, request, httpStatus, code, message)
		return
	}
	encoded, err := (protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: true,
	}).Marshal(response.Layout)
	if err != nil {
		writeError(writer, request, http.StatusInternalServerError, "internal_failure", "internal failure while serializing layout")
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(encoded)
}

func validateCanonicalRequest(body []byte, descriptor protoreflect.MessageDescriptor) error {
	required := map[protoreflect.FullName][]protoreflect.Name{
		"daedalus.v1.GenerateRequest": {"config"},
		"daedalus.v1.Config":          {"width", "height", "seed"},
	}
	return validateCanonicalMessage(body, descriptor, required)
}

func validateCanonicalMessage(
	raw []byte,
	descriptor protoreflect.MessageDescriptor,
	required map[protoreflect.FullName][]protoreflect.Name,
) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return fmt.Errorf("invalid JSON object")
	}
	for _, name := range required[descriptor.FullName()] {
		if _, present := object[string(name)]; !present {
			return fmt.Errorf("required field %s missing", name)
		}
	}
	for name, value := range object {
		field := descriptor.Fields().ByName(protoreflect.Name(name))
		if field == nil {
			return fmt.Errorf("non-canonical or unknown field %s", name)
		}
		if isProtoJSONQuotedInteger(field.Kind()) {
			var text string
			if err := json.Unmarshal(value, &text); err != nil {
				return fmt.Errorf("64-bit integer %s must be a decimal string", name)
			}
			if _, err := strconv.ParseUint(text, 10, 64); err != nil {
				return fmt.Errorf("invalid 64-bit integer %s", name)
			}
		}
		if field.Kind() != protoreflect.MessageKind || string(value) == "null" {
			continue
		}
		if field.IsList() {
			var items []json.RawMessage
			if err := json.Unmarshal(value, &items); err != nil {
				return fmt.Errorf("invalid list %s", name)
			}
			for _, item := range items {
				if err := validateCanonicalMessage(item, field.Message(), required); err != nil {
					return err
				}
			}
			continue
		}
		if err := validateCanonicalMessage(value, field.Message(), required); err != nil {
			return err
		}
	}
	return nil
}

func isProtoJSONQuotedInteger(kind protoreflect.Kind) bool {
	switch kind {
	case protoreflect.Int64Kind,
		protoreflect.Sint64Kind,
		protoreflect.Uint64Kind,
		protoreflect.Fixed64Kind,
		protoreflect.Sfixed64Kind:
		return true
	default:
		return false
	}
}

func validateLocalRequest(request *http.Request) error {
	remoteHost, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return err
	}
	remoteIP := net.ParseIP(remoteHost)
	if remoteIP == nil || !remoteIP.IsLoopback() {
		return fmt.Errorf("remote origin is not loopback")
	}

	host, _, err := net.SplitHostPort(request.Host)
	if err != nil {
		return err
	}
	hostIP := net.ParseIP(host)
	if hostIP == nil || !hostIP.IsLoopback() {
		return fmt.Errorf("host is not a literal loopback IP")
	}

	origin := request.Header.Get("Origin")
	if origin == "" {
		return nil
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "http" || parsed.Host != request.Host {
		return fmt.Errorf("origin does not match the same origin")
	}
	return nil
}

func mapServiceError(err error) (int, string, string) {
	switch status.Code(err) {
	case codes.InvalidArgument:
		return http.StatusBadRequest, "invalid_config", "invalid configuration"
	case codes.ResourceExhausted:
		return http.StatusRequestEntityTooLarge, "limit_exceeded", "resource limit exceeded"
	case codes.FailedPrecondition:
		return http.StatusUnprocessableEntity, "infeasible_generation", "could not generate the requested layout"
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout, "deadline_exceeded", "generation deadline exceeded"
	case codes.Canceled:
		return clientClosedRequestStatus, "generation_canceled", "generation canceled by the client"
	case codes.Unavailable:
		return http.StatusServiceUnavailable, "service_unavailable", "service is shutting down"
	default:
		return http.StatusInternalServerError, "internal_failure", "internal failure while generating layout"
	}
}

func writeError(
	writer http.ResponseWriter,
	request *http.Request,
	statusCode int,
	code string,
	message string,
) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}{
		Code: code, Message: message,
		RequestID: request.Context().Value(requestIDContextKey{}).(string),
	})
}

func newRequestID() string {
	bytes := make([]byte, requestIDBytes)
	if _, err := rand.Read(bytes); err == nil {
		return hex.EncodeToString(bytes)
	}
	// crypto/rand being unavailable must not block the error response. The
	// value stays opaque and limited to the request, without carrying a payload.
	return hex.EncodeToString([]byte(strconv.FormatInt(time.Now().UnixNano(), 10)))
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (writer *statusWriter) WriteHeader(statusCode int) {
	writer.status = statusCode
	writer.ResponseWriter.WriteHeader(statusCode)
}

func (writer *statusWriter) Write(body []byte) (int, error) {
	return writer.ResponseWriter.Write(body)
}
