// Package httpdebug implementa o servidor HTTP local e opcional de
// desenvolvimento do subprocesso Daedalus.
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
	// MaxHTTPDebugBodyBytes é o limite normativo de 1 MiB da seção 2.3.
	MaxHTTPDebugBodyBytes = 1 << 20

	requestIDBytes = 16
	// A especificação não define o status de uma solicitação cujo cliente
	// cancelou a conexão. A leitura conservadora usa o código convencional
	// 499, sem transformá-lo em uma falha interna 500.
	clientClosedRequestStatus = 499
)

type requestIDContextKey struct{}

//go:embed assets/*
var assets embed.FS

// Server adapta o mesmo serviço gRPC para as rotas HTTP de debug.
type Server struct {
	service *service.Server
	version string
	logger  *zap.Logger
	handler http.Handler
}

// New cria o handler HTTP sem abrir listener nem iniciar goroutines.
func New(serviceServer *service.Server, version string, logger *zap.Logger) *Server {
	if serviceServer == nil {
		panic("serviceServer não pode ser nil")
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

// Handler devolve as rotas HTTP com validação de origem e logging.
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
				"requisição HTTP de debug concluída",
				zap.String("request_id", requestID),
				zap.Int("status", observed.status),
				zap.Duration("duracao", time.Since(started)),
			)
		}()

		if err := validateLocalRequest(request); err != nil {
			// A seção 2.3 exige recusar acesso não local, mas não fixa o
			// status. A interpretação conservadora usa 403 para não sugerir
			// que autenticação tornaria a origem suportada.
			writeError(observed, request, http.StatusForbidden, "acesso_negado", "acesso local recusado")
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
		statusText = "indisponivel"
		statusCode = http.StatusServiceUnavailable
	}
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}{Status: statusText, Version: server.version})
}

func (server *Server) root(writer http.ResponseWriter, request *http.Request) {
	// A seção 2.3 exige redirecionamento, mas não escolhe o código. A
	// interpretação conservadora usa 307, que não reescreve o método.
	http.Redirect(writer, request, "/debug/", http.StatusTemporaryRedirect)
}

func (server *Server) debug(writer http.ResponseWriter, _ *http.Request) {
	content, err := assets.ReadFile("assets/index.html")
	if err != nil {
		http.Error(writer, "asset de debug indisponível", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = writer.Write(content)
}

func (server *Server) generate(writer http.ResponseWriter, request *http.Request) {
	// Parâmetros MIME opcionais, como charset, não mudam o media type
	// application/json exigido pela seção 2.3.
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(
			writer, request, http.StatusBadRequest,
			"content_type_invalido", "Content-Type deve ser application/json",
		)
		return
	}
	if request.ContentLength > MaxHTTPDebugBodyBytes {
		writeError(
			writer, request, http.StatusRequestEntityTooLarge,
			"corpo_excedido", "corpo da solicitação excede o limite de 1 MiB",
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
				"corpo_excedido", "corpo da solicitação excede o limite de 1 MiB",
			)
			return
		}
		writeError(writer, request, http.StatusBadRequest, "json_invalido", "não foi possível ler o corpo JSON")
		return
	}

	var protoRequest daedalusv1.GenerateRequest
	if err := validateCanonicalRequest(body, protoRequest.ProtoReflect().Descriptor()); err != nil {
		writeError(writer, request, http.StatusBadRequest, "json_invalido", "solicitação ProtoJSON inválida")
		return
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, &protoRequest); err != nil {
		writeError(writer, request, http.StatusBadRequest, "json_invalido", "solicitação ProtoJSON inválida")
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
		writeError(writer, request, http.StatusInternalServerError, "falha_interna", "falha interna ao serializar layout")
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
		return fmt.Errorf("objeto JSON inválido")
	}
	for _, name := range required[descriptor.FullName()] {
		if _, present := object[string(name)]; !present {
			return fmt.Errorf("campo obrigatório %s ausente", name)
		}
	}
	for name, value := range object {
		field := descriptor.Fields().ByName(protoreflect.Name(name))
		if field == nil {
			return fmt.Errorf("campo não canônico ou desconhecido %s", name)
		}
		if isProtoJSONQuotedInteger(field.Kind()) {
			var text string
			if err := json.Unmarshal(value, &text); err != nil {
				return fmt.Errorf("inteiro de 64 bits %s deve ser string decimal", name)
			}
			if _, err := strconv.ParseUint(text, 10, 64); err != nil {
				return fmt.Errorf("inteiro de 64 bits %s inválido", name)
			}
		}
		if field.Kind() != protoreflect.MessageKind || string(value) == "null" {
			continue
		}
		if field.IsList() {
			var items []json.RawMessage
			if err := json.Unmarshal(value, &items); err != nil {
				return fmt.Errorf("lista %s inválida", name)
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
		return fmt.Errorf("origem remota não é loopback")
	}

	host, _, err := net.SplitHostPort(request.Host)
	if err != nil {
		return err
	}
	hostIP := net.ParseIP(host)
	if hostIP == nil || !hostIP.IsLoopback() {
		return fmt.Errorf("host não é IP literal de loopback")
	}

	origin := request.Header.Get("Origin")
	if origin == "" {
		return nil
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "http" || parsed.Host != request.Host {
		return fmt.Errorf("origin não corresponde à mesma origem")
	}
	return nil
}

func mapServiceError(err error) (int, string, string) {
	switch status.Code(err) {
	case codes.InvalidArgument:
		return http.StatusBadRequest, "config_invalida", "configuração inválida"
	case codes.ResourceExhausted:
		return http.StatusRequestEntityTooLarge, "limite_excedido", "limite de recursos excedido"
	case codes.FailedPrecondition:
		return http.StatusUnprocessableEntity, "geracao_inviavel", "não foi possível gerar o layout solicitado"
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout, "prazo_excedido", "prazo da geração excedido"
	case codes.Canceled:
		return clientClosedRequestStatus, "geracao_cancelada", "geração cancelada pelo cliente"
	case codes.Unavailable:
		return http.StatusServiceUnavailable, "servico_indisponivel", "serviço em desligamento"
	default:
		return http.StatusInternalServerError, "falha_interna", "falha interna ao gerar layout"
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
	// crypto/rand indisponível não deve impedir a resposta de erro. O valor
	// continua opaco e limitado à solicitação, sem carregar payload.
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
