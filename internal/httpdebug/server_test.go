package httpdebug

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Otoru/daedalus"
	daedalusv1 "github.com/Otoru/daedalus/internal/gen/go/daedalus/v1"
	"github.com/Otoru/daedalus/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	enderecoRemotoLoopback = "127.0.0.1:54321"
	hostLoopbackTeste      = "127.0.0.1:8090"
	versaoTeste            = "v0.1.0-teste"
)

func TestRotasBasicasServemSaudeRedirecionamentoEAssetLocal(t *testing.T) {
	t.Parallel()

	var geracoes atomic.Int32
	server := novoServidorTeste(t, func(context.Context, daedalus.Config) (daedalus.Layout, error) {
		geracoes.Add(1)
		return daedalus.Layout{}, nil
	}, service.NewAdmission(1), zap.NewNop())

	health := executarRequisicao(server.Handler(), http.MethodGet, "/healthz", nil)
	assert.Equal(t, http.StatusOK, health.Code)
	assert.Equal(t, "application/json", health.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"status":"ok","version":"v0.1.0-teste"}`, health.Body.String())

	root := executarRequisicao(server.Handler(), http.MethodGet, "/", nil)
	assert.Equal(t, http.StatusTemporaryRedirect, root.Code)
	assert.Equal(t, "/debug/", root.Header().Get("Location"))

	debug := executarRequisicao(server.Handler(), http.MethodGet, "/debug/", nil)
	assert.Equal(t, http.StatusOK, debug.Code)
	assert.Equal(t, "text/html; charset=utf-8", debug.Header().Get("Content-Type"))
	assert.Contains(t, debug.Body.String(), "<!doctype html>")
	assert.NotContains(t, debug.Body.String(), "http://")
	assert.NotContains(t, debug.Body.String(), "https://")
	assert.Zero(t, geracoes.Load(), "rotas GET não podem iniciar geração")
}

func TestGenerateAceitaProtoJSONSnakeCaseEDevolveLayoutDoMesmoGerador(t *testing.T) {
	t.Parallel()

	generator := daedalus.Generator{}
	server := novoServidorTeste(t, generator.GenerateContext, service.NewAdmission(1), zap.NewNop())
	body := `{
		"config": {
			"width": 9,
			"height": 9,
			"seed": "18446744073709551615",
			"max_rooms": 3,
			"room_geometry": {
				"min_width": 1,
				"max_width": 1,
				"min_height": 1,
				"max_height": 1,
				"max_footprint_cells": 1,
				"min_room_gap": 0,
				"shapes": [{"shape": "ROOM_SHAPE_RECTANGLE", "weight": 1}]
			}
		}
	}`

	response := executarRequisicao(
		server.Handler(), http.MethodPost, "/api/v1/generate", strings.NewReader(body),
	)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
	assert.Empty(t, response.Header().Get("Access-Control-Allow-Origin"))

	var got daedalusv1.Layout
	require.NoError(t, (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(response.Body.Bytes(), &got))
	expected, err := generator.Generate(daedalus.Config{
		Width: 9, Height: 9, Seed: daedalus.Seed(^uint64(0)), MaxRooms: 3,
		RoomGeometry: &daedalus.RoomGeometry{
			MinWidth: 1, MaxWidth: 1, MinHeight: 1, MaxHeight: 1,
			MaxFootprintCells: 1,
			Shapes: []daedalus.RoomShapeWeight{{
				Shape: daedalus.RoomShapeRectangle, Weight: 1,
			}},
		},
	})
	require.NoError(t, err)
	assert.True(t, proto.Equal(service.LayoutToProto(expected), &got))
	assert.Contains(t, response.Body.String(), `"cell_size"`)
	assert.NotContains(t, response.Body.String(), `"cellSize"`)
	assert.Contains(t, response.Body.String(), `"seed":"18446744073709551615"`)
}

func TestGenerateExigeJSONProtoCanonicoECamposObrigatorios(t *testing.T) {
	t.Parallel()

	var geracoes atomic.Int32
	server := novoServidorTeste(t, func(context.Context, daedalus.Config) (daedalus.Layout, error) {
		geracoes.Add(1)
		return daedalus.Layout{}, nil
	}, service.NewAdmission(1), zap.NewNop())

	cases := []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "content type ausente", body: `{"config":{"width":1,"height":1,"seed":"0"}}`},
		{name: "content type incorreto", contentType: "text/plain", body: `{}`},
		{name: "json inválido", contentType: "application/json", body: `{"config":`},
		{name: "campo desconhecido", contentType: "application/json", body: `{"config":{"width":1,"height":1,"seed":"0","desconhecido":1}}`},
		{name: "nome camel case", contentType: "application/json", body: `{"config":{"width":1,"height":1,"seed":"0","maxRooms":1}}`},
		{name: "config ausente", contentType: "application/json", body: `{}`},
		{name: "width ausente", contentType: "application/json", body: `{"config":{"height":1,"seed":"0"}}`},
		{name: "height ausente", contentType: "application/json", body: `{"config":{"width":1,"seed":"0"}}`},
		{name: "seed ausente", contentType: "application/json", body: `{"config":{"width":1,"height":1}}`},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			request := novaRequisicaoLocal(
				http.MethodPost, "/api/v1/generate", strings.NewReader(testCase.body),
			)
			if testCase.contentType != "" {
				request.Header.Set("Content-Type", testCase.contentType)
			}
			response := httptest.NewRecorder()

			server.Handler().ServeHTTP(response, request)

			assert.Equal(t, http.StatusBadRequest, response.Code)
			assertErroEstruturadoEmPortugues(t, response)
			assert.NotContains(t, response.Body.String(), "layout")
			assert.NotContains(t, response.Body.String(), "panic")
		})
	}
	assert.Zero(t, geracoes.Load())

	defaultServer := New(service.New(nil, service.NewAdmission(1)), versaoTeste, zap.NewNop())
	invalidConfig := executarRequisicao(
		defaultServer.Handler(), http.MethodPost, "/api/v1/generate",
		strings.NewReader(`{"config":{"width":0,"height":1,"seed":"0"}}`),
	)
	assert.Equal(t, http.StatusBadRequest, invalidConfig.Code)
	assertErroEstruturadoEmPortugues(t, invalidConfig)
}

func TestGenerateRejeitaCorpoAcimaDoLimiteAntesDaGeracao(t *testing.T) {
	t.Parallel()

	var geracoes atomic.Int32
	server := novoServidorTeste(t, func(context.Context, daedalus.Config) (daedalus.Layout, error) {
		geracoes.Add(1)
		return daedalus.Layout{}, nil
	}, service.NewAdmission(1), zap.NewNop())
	body := strings.Repeat("x", MaxHTTPDebugBodyBytes+1)

	response := executarRequisicao(
		server.Handler(), http.MethodPost, "/api/v1/generate", strings.NewReader(body),
	)

	assert.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
	assertErroEstruturadoEmPortugues(t, response)
	assert.Zero(t, geracoes.Load())
}

func TestGenerateMapeiaCategoriasDeErroSemVazarDetalhes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		err    error
		status int
	}{
		{name: "limite", err: daedalus.ErrLimitExceeded, status: http.StatusRequestEntityTooLarge},
		{name: "plant", err: daedalus.ErrNoCompatiblePlant, status: http.StatusUnprocessableEntity},
		{name: "rota", err: daedalus.ErrUnroutableEdge, status: http.StatusUnprocessableEntity},
		{name: "deadline", err: context.DeadlineExceeded, status: http.StatusGatewayTimeout},
		{name: "interna", err: errors.New("segredo-interno"), status: http.StatusInternalServerError},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			server := novoServidorTeste(t, func(context.Context, daedalus.Config) (daedalus.Layout, error) {
				return daedalus.Layout{}, testCase.err
			}, service.NewAdmission(1), zap.NewNop())

			response := executarRequisicao(
				server.Handler(), http.MethodPost, "/api/v1/generate",
				strings.NewReader(`{"config":{"width":1,"height":1,"seed":"0"}}`),
			)

			assert.Equal(t, testCase.status, response.Code)
			assertErroEstruturadoEmPortugues(t, response)
			assert.NotContains(t, response.Body.String(), "segredo-interno")
		})
	}
}

func TestHTTPERPCCompartilhamAMesmaAdmissao(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{}, 2)
	release := make(chan struct{}, 2)
	sharedAdmission := service.NewAdmission(1)
	grpcServer := service.New(func(ctx context.Context, _ daedalus.Config) (daedalus.Layout, error) {
		entered <- struct{}{}
		select {
		case <-release:
			return daedalus.Layout{}, nil
		case <-ctx.Done():
			return daedalus.Layout{}, ctx.Err()
		}
	}, sharedAdmission)
	httpServer := New(grpcServer, versaoTeste, zap.NewNop())

	httpDone := make(chan *httptest.ResponseRecorder)
	go func() {
		httpDone <- executarRequisicao(
			httpServer.Handler(), http.MethodPost, "/api/v1/generate",
			strings.NewReader(`{"config":{"width":1,"height":1,"seed":"0"}}`),
		)
	}()
	<-entered

	rpcDone := make(chan error)
	go func() {
		_, err := grpcServer.Generate(context.Background(), &daedalusv1.GenerateRequest{
			Config: &daedalusv1.Config{Width: 1, Height: 1, Seed: 1},
		})
		rpcDone <- err
	}()
	select {
	case <-entered:
		t.Fatal("RPC entrou enquanto a geração HTTP ocupava a única vaga")
	case <-time.After(30 * time.Millisecond):
	}
	release <- struct{}{}
	<-entered
	release <- struct{}{}

	assert.Equal(t, http.StatusOK, (<-httpDone).Code)
	require.NoError(t, <-rpcDone)
}

func TestCancelamentoDoClienteChegaAoGeradorHTTP(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	canceled := make(chan struct{})
	server := novoServidorTeste(t, func(ctx context.Context, _ daedalus.Config) (daedalus.Layout, error) {
		close(entered)
		<-ctx.Done()
		close(canceled)
		return daedalus.Layout{}, ctx.Err()
	}, service.NewAdmission(1), zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	request := novaRequisicaoLocal(
		http.MethodPost, "/api/v1/generate",
		strings.NewReader(`{"config":{"width":1,"height":1,"seed":"0"}}`),
	).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		server.Handler().ServeHTTP(response, request)
		close(done)
	}()
	select {
	case <-entered:
	case <-done:
		t.Fatalf("handler encerrou antes da geração: status=%d corpo=%s", response.Code, response.Body.String())
	case <-time.After(time.Second):
		t.Fatal("handler não iniciou a geração")
	}

	cancel()

	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("gerador não observou o cancelamento do cliente")
	}
	<-done
}

func TestSegurancaRecusaOrigemRemotaERegistraSomenteMetadados(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	encoder := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	logger := zap.New(zapcore.NewCore(encoder, zapcore.AddSync(&logs), zapcore.InfoLevel))
	server := novoServidorTeste(t, func(context.Context, daedalus.Config) (daedalus.Layout, error) {
		return daedalus.Layout{}, nil
	}, service.NewAdmission(1), logger)
	segredo := "seed-super-secreta"

	remoteRequest := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	remoteRequest.RemoteAddr = "192.0.2.10:1234"
	remoteRequest.Host = hostLoopbackTeste
	remoteResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(remoteResponse, remoteRequest)
	assert.Equal(t, http.StatusForbidden, remoteResponse.Code)

	originRequest := novaRequisicaoLocal(http.MethodGet, "/healthz", nil)
	originRequest.Header.Set("Origin", "http://127.0.0.1:9999")
	originResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(originResponse, originRequest)
	assert.Equal(t, http.StatusForbidden, originResponse.Code)

	body := `{"config":{"width":1,"height":1,"seed":"0"},"` + segredo + `":"x"}`
	response := executarRequisicao(
		server.Handler(), http.MethodPost, "/api/v1/generate", strings.NewReader(body),
	)
	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Empty(t, response.Header().Get("Access-Control-Allow-Origin"))
	assert.NotContains(t, logs.String(), segredo)
	assert.Contains(t, logs.String(), "request_id")
	assert.Contains(t, logs.String(), "status")
	assert.Contains(t, logs.String(), "duracao")
}

func TestHealthPassaAIndisponivelDuranteShutdown(t *testing.T) {
	t.Parallel()

	serviceServer := service.New(
		func(context.Context, daedalus.Config) (daedalus.Layout, error) {
			return daedalus.Layout{}, nil
		},
		service.NewAdmission(1),
	)
	server := New(serviceServer, versaoTeste, zap.NewNop())
	serviceServer.BeginShutdown()

	response := executarRequisicao(server.Handler(), http.MethodGet, "/healthz", nil)

	assert.Equal(t, http.StatusServiceUnavailable, response.Code)
	assert.JSONEq(t, `{"status":"indisponivel","version":"v0.1.0-teste"}`, response.Body.String())
}

func novoServidorTeste(
	t *testing.T,
	generate service.GenerateFunc,
	admission *service.Admission,
	logger *zap.Logger,
) *Server {
	t.Helper()
	return New(service.New(generate, admission), versaoTeste, logger)
}

func executarRequisicao(
	handler http.Handler,
	method string,
	target string,
	body io.Reader,
) *httptest.ResponseRecorder {
	request := novaRequisicaoLocal(method, target, body)
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func novaRequisicaoLocal(
	method string,
	target string,
	body io.Reader,
) *http.Request {
	request := httptest.NewRequest(method, target, body)
	request.RemoteAddr = enderecoRemotoLoopback
	request.Host = hostLoopbackTeste
	return request
}

func assertErroEstruturadoEmPortugues(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
	var got struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &got))
	assert.NotEmpty(t, got.Code)
	assert.NotEmpty(t, got.Message)
	assert.NotEmpty(t, got.RequestID)
}
