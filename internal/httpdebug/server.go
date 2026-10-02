// Package httpdebug implements the optional local HTTP development
// server. It is off unless enabled, binds only to a literal loopback
// address, has no CORS and no authentication, and shares the gRPC
// admission limit instead of keeping its own queue. It uses the same
// generator as gRPC, keeps no state between requests, and does not write
// Layouts to disk. UI assets are embedded; the handler does not fetch
// remote resources, load plugins, or read user files.
package httpdebug

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
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
	"strings"
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
	// MaxHTTPDebugBodyBytes is 32 MiB. Platform layouts carry a tile grid plus
	// their derived jump graph and witnesses, so the former 1 MiB limit was too
	// small for a modest multi-room debug response. The limit is applied before
	// the body is read into memory; a larger body is rejected with 413.
	MaxHTTPDebugBodyBytes = 32 << 20

	requestIDBytes   = 16
	debugIndexPath   = "assets/index.html"
	debugCSSPath     = "assets/styles.css"
	debugJSPath      = "assets/app.js"
	debugUILogicPath = "assets/ui-logic.js"
	// Debug JSON requests and responses use Content-Type application/json.
	// A successful generate response is 200 with the full Layout. Errors
	// are JSON with code, an English message, and an opaque request_id,
	// and they carry no stack trace and no sensitive data.
	contentTypeHeader = "Content-Type"
	jsonMediaType     = "application/json"
	// No status is defined for a request whose client canceled the
	// connection. The response uses the conventional 499 so a client
	// cancel is not reported as an internal 500 failure.
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
	mux.HandleFunc("POST /api/v1/generate-platform", server.generatePlatform)
	mux.HandleFunc("POST /api/v1/compute-steps", server.computeSteps)
	mux.HandleFunc("POST /api/v1/compute-visibility", server.computeVisibility)
	mux.HandleFunc("POST /api/v1/build-gating-plan", server.buildGatingPlan)
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
			// Logs record request_id, status, and duration. The body,
			// Config, and Seed are never written.
			server.logger.Info(
				"debug HTTP request completed",
				zap.String("request_id", requestID),
				zap.Int("status", observed.status),
				zap.Duration("duration", time.Since(started)),
			)
		}()

		if err := validateLocalRequest(request); err != nil {
			// Access is loopback only. RemoteAddr must be loopback even
			// behind a local proxy, and there is no CORS middleware and no
			// authentication: the bind is the restriction. The refusal
			// status is not fixed; 403 rejects the origin without implying
			// that credentials would make a remote client allowed.
			writeError(observed, request, http.StatusForbidden, "access_denied", "local access refused")
			return
		}
		next.ServeHTTP(observed, request)
	})
}

// health answers GET /healthz with 200 application/json while the
// service is accepting work, and 503 during shutdown. It does not
// start a generation.
func (server *Server) health(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set(contentTypeHeader, jsonMediaType)
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
	// GET / redirects to /debug/ on the same origin. The redirect code is
	// not fixed; 307 keeps the original method, so a non-GET is not
	// rewritten into a GET of the debug page.
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
	case "/debug/ui-logic.js":
		assetPath = debugUILogicPath
		contentType = "text/javascript; charset=utf-8"
	default:
		// /debug/ serves only the embedded page, stylesheet, and script,
		// with no CDN and no external resource. An unknown path has no
		// defined fallback; 404 is returned instead of filling the gap
		// with the page HTML.
		http.NotFound(writer, request)
		return
	}

	content, err := assets.ReadFile(assetPath)
	if err != nil {
		http.Error(writer, "debug asset unavailable", http.StatusInternalServerError)
		return
	}
	writer.Header().Set(contentTypeHeader, contentType)
	_, _ = writer.Write(content)
}

func (server *Server) generate(writer http.ResponseWriter, request *http.Request) {
	// Optional MIME parameters, such as charset, do not change the required
	// media type application/json. The body is canonical ProtoJSON:
	// snake_case names, the same presence rules as protobuf, and a uint64
	// such as Seed written as a decimal string. Missing required fields
	// are rejected; they are not filled in with defaults.
	body, ok := readDebugJSON(writer, request)
	if !ok {
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
	writer.Header().Set(contentTypeHeader, jsonMediaType)
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(encoded)
}

func (server *Server) generatePlatform(writer http.ResponseWriter, request *http.Request) {
	// Same admission, body ceiling, canonical ProtoJSON and error object as
	// generate. A Rejected or Unknown judgement is the layout's verdict, so it
	// is returned with the map on 200. Only a failure to draw the map is an
	// HTTP error.
	body, ok := readDebugJSON(writer, request)
	if !ok {
		return
	}

	var protoRequest daedalusv1.GeneratePlatformRequest
	if err := validateCanonicalRequest(body, protoRequest.ProtoReflect().Descriptor()); err != nil {
		writeError(writer, request, http.StatusBadRequest, "invalid_json", "invalid ProtoJSON request")
		return
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, &protoRequest); err != nil {
		writeError(writer, request, http.StatusBadRequest, "invalid_json", "invalid ProtoJSON request")
		return
	}

	response, err := server.service.GeneratePlatform(request.Context(), &protoRequest)
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
	writer.Header().Set(contentTypeHeader, jsonMediaType)
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(encoded)
}

func (server *Server) computeSteps(writer http.ResponseWriter, request *http.Request) {
	// Same admission, body ceiling, request id, and error object as generate.
	// The page posts the ComputeSteps a game would post: a base64 cost grid
	// and queries. There is no debug-only field or shortcut route.
	body, ok := readDebugJSON(writer, request)
	if !ok {
		return
	}

	var protoRequest daedalusv1.ComputeStepsRequest
	if err := validateCanonicalRequest(body, protoRequest.ProtoReflect().Descriptor()); err != nil {
		writeError(writer, request, http.StatusBadRequest, "invalid_json", "invalid ProtoJSON request")
		return
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, &protoRequest); err != nil {
		writeError(writer, request, http.StatusBadRequest, "invalid_json", "invalid ProtoJSON request")
		return
	}

	response, err := server.service.ComputeSteps(request.Context(), &protoRequest)
	if err != nil {
		httpStatus, code, message := mapServiceError(err)
		writeError(writer, request, httpStatus, code, message)
		return
	}
	encoded, err := (protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: true,
	}).Marshal(response)
	if err != nil {
		writeError(writer, request, http.StatusInternalServerError, "internal_failure", "internal failure while serializing steps")
		return
	}
	writer.Header().Set(contentTypeHeader, jsonMediaType)
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(encoded)
}

func (server *Server) computeVisibility(writer http.ResponseWriter, request *http.Request) {
	body, ok := readDebugJSON(writer, request)
	if !ok {
		return
	}

	var protoRequest daedalusv1.ComputeVisibilityRequest
	if err := validateCanonicalRequest(body, protoRequest.ProtoReflect().Descriptor()); err != nil {
		writeError(writer, request, http.StatusBadRequest, "invalid_json", "invalid ProtoJSON request")
		return
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, &protoRequest); err != nil {
		writeError(writer, request, http.StatusBadRequest, "invalid_json", "invalid ProtoJSON request")
		return
	}

	response, err := server.service.ComputeVisibility(request.Context(), &protoRequest)
	if err != nil {
		httpStatus, code, message := mapServiceError(err)
		writeError(writer, request, httpStatus, code, message)
		return
	}
	encoded, err := (protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: true,
	}).Marshal(response)
	if err != nil {
		writeError(writer, request, http.StatusInternalServerError, "internal_failure", "internal failure while serializing visibility")
		return
	}
	writer.Header().Set(contentTypeHeader, jsonMediaType)
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(encoded)
}

func (server *Server) buildGatingPlan(writer http.ResponseWriter, request *http.Request) {
	body, ok := readDebugJSON(writer, request)
	if !ok {
		return
	}

	var protoRequest daedalusv1.BuildGatingPlanRequest
	if err := validateCanonicalRequest(body, protoRequest.ProtoReflect().Descriptor()); err != nil {
		writeError(writer, request, http.StatusBadRequest, "invalid_json", "invalid ProtoJSON request")
		return
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, &protoRequest); err != nil {
		writeError(writer, request, http.StatusBadRequest, "invalid_json", "invalid ProtoJSON request")
		return
	}

	response, err := server.service.BuildGatingPlan(request.Context(), &protoRequest)
	if err != nil {
		httpStatus, code, message := mapServiceError(err)
		writeError(writer, request, httpStatus, code, message)
		return
	}
	encoded, err := (protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: true,
	}).Marshal(response)
	if err != nil {
		writeError(writer, request, http.StatusInternalServerError, "internal_failure", "internal failure while serializing gating plan")
		return
	}
	writer.Header().Set(contentTypeHeader, jsonMediaType)
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(encoded)
}

// readDebugJSON applies the shared debug POST limits: application/json, then
// a 32 MiB ceiling before the body is buffered. A caller that gets false has
// already received the structured error.
func readDebugJSON(writer http.ResponseWriter, request *http.Request) ([]byte, bool) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get(contentTypeHeader))
	if err != nil || mediaType != jsonMediaType {
		writeError(
			writer, request, http.StatusBadRequest,
			"invalid_content_type", "Content-Type must be application/json",
		)
		return nil, false
	}
	if request.ContentLength > MaxHTTPDebugBodyBytes {
		writeError(
			writer, request, http.StatusRequestEntityTooLarge,
			"body_too_large", "request body exceeds the 32 MiB limit",
		)
		return nil, false
	}

	request.Body = http.MaxBytesReader(writer, request.Body, MaxHTTPDebugBodyBytes)
	body, err := io.ReadAll(request.Body)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(
				writer, request, http.StatusRequestEntityTooLarge,
				"body_too_large", "request body exceeds the 32 MiB limit",
			)
			return nil, false
		}
		writeError(writer, request, http.StatusBadRequest, "invalid_json", "could not read the JSON body")
		return nil, false
	}
	return body, true
}

func validateCanonicalRequest(body []byte, descriptor protoreflect.MessageDescriptor) error {
	required := map[protoreflect.FullName][]protoreflect.Name{
		"daedalus.v1.GenerateRequest":          {"config"},
		"daedalus.v1.Config":                   {"width", "height", "seed"},
		"daedalus.v1.GeneratePlatformRequest":  {"config"},
		"daedalus.v1.PlatformConfig":           {"width", "height", "seed"},
		"daedalus.v1.ComputeStepsRequest":      {"cost_grid"},
		"daedalus.v1.CostGrid":                 {"width", "height", "costs"},
		"daedalus.v1.ComputeVisibilityRequest": {"opacity_grid"},
		"daedalus.v1.OpacityGrid":              {"width", "height", "transparent"},
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
		if err := validateCanonicalField(name, value, descriptor, required); err != nil {
			return err
		}
	}
	return nil
}

func validateCanonicalField(
	name string,
	value json.RawMessage,
	descriptor protoreflect.MessageDescriptor,
	required map[protoreflect.FullName][]protoreflect.Name,
) error {
	field := descriptor.Fields().ByName(protoreflect.Name(name))
	if field == nil {
		return fmt.Errorf("non-canonical or unknown field %s", name)
	}
	if isProtoJSONQuotedInteger(field.Kind()) {
		if err := validateQuotedProtoInteger(name, value); err != nil {
			return err
		}
	}
	// ProtoJSON carries bytes as a base64 string. A JSON array of numbers is
	// a different contract and would let a caller skip the wire encoding the
	// game has to use, so it is rejected here rather than handed to Unmarshal.
	if field.Kind() == protoreflect.BytesKind {
		return validateProtoBytes(name, value)
	}
	if field.Kind() != protoreflect.MessageKind || string(value) == "null" {
		return nil
	}
	if field.IsList() {
		return validateCanonicalList(name, value, field.Message(), required)
	}
	return validateCanonicalMessage(value, field.Message(), required)
}

func validateProtoBytes(name string, value json.RawMessage) error {
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return fmt.Errorf("bytes field %s must be a base64 string", name)
	}
	// Match protojson: standard alphabet, or the URL alphabet when '-' or '_'
	// appears, and no padding when the length is not a multiple of four.
	encoding := base64.StdEncoding
	if strings.ContainsAny(text, "-_") {
		encoding = base64.URLEncoding
	}
	if len(text)%4 != 0 {
		encoding = encoding.WithPadding(base64.NoPadding)
	}
	if _, err := encoding.DecodeString(text); err != nil {
		return fmt.Errorf("bytes field %s is not valid base64", name)
	}
	return nil
}

func validateQuotedProtoInteger(name string, value json.RawMessage) error {
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return fmt.Errorf("64-bit integer %s must be a decimal string", name)
	}
	if _, err := strconv.ParseUint(text, 10, 64); err != nil {
		return fmt.Errorf("invalid 64-bit integer %s", name)
	}
	return nil
}

func validateCanonicalList(
	name string,
	value json.RawMessage,
	message protoreflect.MessageDescriptor,
	required map[protoreflect.FullName][]protoreflect.Name,
) error {
	var items []json.RawMessage
	if err := json.Unmarshal(value, &items); err != nil {
		return fmt.Errorf("invalid list %s", name)
	}
	for _, item := range items {
		if err := validateCanonicalMessage(item, message, required); err != nil {
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

// mapServiceError is the debug HTTP status mapping. Invalid JSON or
// Config is 400, a body or resource limit is 413, no compatible plant
// or an unroutable edge is 422, a deadline is 504, and any other
// generation failure is 500. A client cancel is 499 and shutdown is 503.
// Invalid JSON and an oversized body are rejected before this function,
// as 400 and 413. Messages stay in English and omit stack traces and
// sensitive data.
func mapServiceError(err error) (int, string, string) {
	switch status.Code(err) {
	case codes.InvalidArgument:
		return http.StatusBadRequest, "invalid_config", status.Convert(err).Message()
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
	writer.Header().Set(contentTypeHeader, jsonMediaType)
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
