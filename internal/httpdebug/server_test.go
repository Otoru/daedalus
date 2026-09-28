package httpdebug

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
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
	remoteLoopbackAddress = "127.0.0.1:54321"
	loopbackTestHost      = "127.0.0.1:8090"
	testVersion           = "v0.1.0-test"
)

// TestBasicRoutesServeHealthRedirectAndLocalAsset checks that GET /healthz and
// GET /debug/ respond from the embedded page, with no external URL.
func TestBasicRoutesServeHealthRedirectAndLocalAsset(t *testing.T) {
	t.Parallel()

	var generations atomic.Int32
	server := newTestServer(t, func(context.Context, daedalus.Config) (daedalus.Layout, error) {
		generations.Add(1)
		return daedalus.Layout{}, nil
	}, service.NewAdmission(1), zap.NewNop())

	health := executeRequest(server.Handler(), http.MethodGet, "/healthz", nil)
	assert.Equal(t, http.StatusOK, health.Code)
	assert.Equal(t, "application/json", health.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"status":"ok","version":"v0.1.0-test"}`, health.Body.String())

	root := executeRequest(server.Handler(), http.MethodGet, "/", nil)
	assert.Equal(t, http.StatusTemporaryRedirect, root.Code)
	assert.Equal(t, "/debug/", root.Header().Get("Location"))

	debug := executeRequest(server.Handler(), http.MethodGet, "/debug/", nil)
	assert.Equal(t, http.StatusOK, debug.Code)
	assert.Equal(t, "text/html; charset=utf-8", debug.Header().Get("Content-Type"))
	assert.Contains(t, debug.Body.String(), "<!doctype html>")
	assert.NotContains(t, debug.Body.String(), "http://")
	assert.NotContains(t, debug.Body.String(), "https://")
	assert.Zero(t, generations.Load(), "GET routes must not start generation")
}

// TestUIAssetsAreEmbeddedLocalAndCORSFree checks that the debug UI is
// same-origin, ships no CORS allow-origin header and references no external host.
func TestUIAssetsAreEmbeddedLocalAndCORSFree(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(context.Context, daedalus.Config) (daedalus.Layout, error) {
		return daedalus.Layout{}, nil
	}, service.NewAdmission(1), zap.NewNop())
	expectedAssets := []struct {
		path        string
		contentType string
		snippet     string
	}{
		{path: "/debug/", contentType: "text/html; charset=utf-8", snippet: `id="request-editor"`},
		{path: "/debug/styles.css", contentType: "text/css; charset=utf-8", snippet: ".map-canvas"},
		{path: "/debug/app.js", contentType: "text/javascript; charset=utf-8", snippet: "/api/v1/generate"},
	}

	for _, expected := range expectedAssets {
		expected := expected
		t.Run(expected.path, func(t *testing.T) {
			response := executeRequest(server.Handler(), http.MethodGet, expected.path, nil)

			require.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, expected.contentType, response.Header().Get("Content-Type"))
			assert.Contains(t, response.Body.String(), expected.snippet)
			assert.NotContains(t, response.Body.String(), "http://")
			assert.NotContains(t, response.Body.String(), "https://")
			assert.NotRegexp(t, `(?i)(?:src|href)\s*=\s*["']//`, response.Body.String())
			assert.Empty(t, response.Header().Get("Access-Control-Allow-Origin"))
		})
	}
}

// TestAllEmbeddedAssetsDoNotReferenceAnExternalHost checks that every embedded
// HTML, CSS and JavaScript file is free of an external host.
func TestAllEmbeddedAssetsDoNotReferenceAnExternalHost(t *testing.T) {
	t.Parallel()

	var checked int
	err := fs.WalkDir(assets, "assets", func(assetPath string, entry fs.DirEntry, walkErr error) error {
		require.NoError(t, walkErr)
		if entry.IsDir() {
			return nil
		}
		switch path.Ext(assetPath) {
		case ".html", ".css", ".js":
		default:
			return nil
		}

		content, err := fs.ReadFile(assets, assetPath)
		require.NoError(t, err)
		checked++
		assert.NotContains(t, string(content), "http://", assetPath)
		assert.NotContains(t, string(content), "https://", assetPath)
		assert.NotRegexp(t, `(?i)["'(]//[[:alnum:].-]+(?:[/:"')]|$)`, string(content), assetPath)
		return nil
	})

	require.NoError(t, err)
	assert.GreaterOrEqual(t, checked, 3, "HTML, CSS and JavaScript must be checked")
}

// TestDebugMapExampleDrawsWideCorridors checks that the embedded example asks
// for a corridor width distribution that is mostly one Cell wide, and that the
// page draws the occupied band, the ordered centerline and a door's real span.
func TestDebugMapExampleDrawsWideCorridors(t *testing.T) {
	t.Parallel()

	script, err := fs.ReadFile(assets, "assets/app.js")
	require.NoError(t, err)
	page, err := fs.ReadFile(assets, "assets/index.html")
	require.NoError(t, err)

	var request struct {
		Config struct {
			CorridorGeometry struct {
				Widths []struct {
					Width  uint32 `json:"width"`
					Weight uint32 `json:"weight"`
				} `json:"widths"`
			} `json:"corridor_geometry"`
		} `json:"config"`
	}
	require.NoError(t, json.Unmarshal([]byte(exampleRequestFromScript(t, string(script))), &request))

	var narrowWeight uint32
	var wideWeight uint32
	var widest uint32
	for _, item := range request.Config.CorridorGeometry.Widths {
		require.Positive(t, item.Weight)
		if item.Width == 1 {
			narrowWeight += item.Weight
		}
		if item.Width > 1 {
			wideWeight += item.Weight
			if item.Width > widest {
				widest = item.Width
			}
		}
	}
	assert.Greater(t, narrowWeight, wideWeight, "the example stays mostly one Cell wide")
	assert.Greater(t, widest, uint32(1), "the example includes a wider run")

	source := string(script)
	assert.Contains(t, source, "centerline")
	assert.Contains(t, source, "door.span")
	assert.Contains(t, source, "corridor_ids")
	assert.Contains(t, string(page), `id="show-centerline"`)
}

func exampleRequestFromScript(t *testing.T, script string) string {
	t.Helper()

	const marker = "const exampleRequest = `"
	start := strings.Index(script, marker)
	require.NotEqual(t, -1, start, "example request template")
	rest := script[start+len(marker):]
	end := strings.Index(rest, "`;")
	require.NotEqual(t, -1, end, "example request terminator")
	return rest[:end]
}

// TestGenerateAcceptsProtoJSONSnakeCaseAndReturnsTheSameGeneratorLayout checks
// that a valid ProtoJSON POST returns the same Layout the SDK Generate
// produces for that Config and Seed, using proto field names.
func TestGenerateAcceptsProtoJSONSnakeCaseAndReturnsTheSameGeneratorLayout(t *testing.T) {
	t.Parallel()

	generator := daedalus.Generator{}
	server := newTestServer(t, generator.GenerateContext, service.NewAdmission(1), zap.NewNop())
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

	response := executeRequest(
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

// TestGenerateCarriesCenterlineAndDoorSpan checks that a request with
// corridor_geometry returns the ordered route and a doorway wider than one
// Cell, and that a request without it still succeeds with every span at 1.
func TestGenerateCarriesCenterlineAndDoorSpan(t *testing.T) {
	t.Parallel()

	generator := daedalus.Generator{}
	server := newTestServer(t, generator.GenerateContext, service.NewAdmission(1), zap.NewNop())

	wide := executeRequest(server.Handler(), http.MethodPost, "/api/v1/generate", strings.NewReader(`{
		"config": {
			"width": 20,
			"height": 14,
			"seed": "0",
			"max_rooms": 4,
			"min_distance": 5,
			"room_geometry": {
				"min_width": 2,
				"max_width": 3,
				"min_height": 2,
				"max_height": 3,
				"max_footprint_cells": 9,
				"min_room_gap": 2,
				"shapes": [{"shape": "ROOM_SHAPE_RECTANGLE", "weight": 1}]
			},
			"corridor_geometry": {
				"widths": [{"width": 2, "weight": 1}]
			}
		}
	}`))
	require.Equal(t, http.StatusOK, wide.Code, wide.Body.String())

	var wideLayout daedalusv1.Layout
	require.NoError(t, (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(wide.Body.Bytes(), &wideLayout))
	var widest uint32
	centerlineCells := 0
	for _, door := range wideLayout.Doors {
		if door.GetSpan() > widest {
			widest = door.GetSpan()
		}
	}
	for _, corridor := range wideLayout.Corridors {
		centerlineCells += len(corridor.GetCenterline())
	}
	assert.Greater(t, widest, uint32(1))
	assert.Greater(t, centerlineCells, 0)
	assert.Contains(t, wide.Body.String(), `"centerline"`)
	assert.Contains(t, wide.Body.String(), `"span"`)

	plain := executeRequest(server.Handler(), http.MethodPost, "/api/v1/generate", strings.NewReader(`{
		"config": {
			"width": 16,
			"height": 16,
			"seed": "0",
			"max_rooms": 4
		}
	}`))
	require.Equal(t, http.StatusOK, plain.Code, plain.Body.String())

	var plainLayout daedalusv1.Layout
	require.NoError(t, (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(plain.Body.Bytes(), &plainLayout))
	require.NotEmpty(t, plainLayout.Doors)
	for _, door := range plainLayout.Doors {
		assert.Equal(t, uint32(1), door.GetSpan())
	}
}

// TestGenerateRequiresCanonicalProtoJSONAndRequiredFields checks that invalid
// JSON, Content-Type or Config returns a structured 400, with no panic and no
// Layout. Error text is English; this test does not scan message wording.
func TestGenerateRequiresCanonicalProtoJSONAndRequiredFields(t *testing.T) {
	t.Parallel()

	var generations atomic.Int32
	server := newTestServer(t, func(context.Context, daedalus.Config) (daedalus.Layout, error) {
		generations.Add(1)
		return daedalus.Layout{}, nil
	}, service.NewAdmission(1), zap.NewNop())

	cases := []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "missing content type", body: `{"config":{"width":1,"height":1,"seed":"0"}}`},
		{name: "incorrect content type", contentType: "text/plain", body: `{}`},
		{name: "invalid json", contentType: "application/json", body: `{"config":`},
		{name: "unknown field", contentType: "application/json", body: `{"config":{"width":1,"height":1,"seed":"0","unknown":1}}`},
		{name: "camel case name", contentType: "application/json", body: `{"config":{"width":1,"height":1,"seed":"0","maxRooms":1}}`},
		{name: "missing config", contentType: "application/json", body: `{}`},
		{name: "missing width", contentType: "application/json", body: `{"config":{"height":1,"seed":"0"}}`},
		{name: "missing height", contentType: "application/json", body: `{"config":{"width":1,"seed":"0"}}`},
		{name: "missing seed", contentType: "application/json", body: `{"config":{"width":1,"height":1}}`},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			request := newLocalRequest(
				http.MethodPost, "/api/v1/generate", strings.NewReader(testCase.body),
			)
			if testCase.contentType != "" {
				request.Header.Set("Content-Type", testCase.contentType)
			}
			response := httptest.NewRecorder()

			server.Handler().ServeHTTP(response, request)

			assert.Equal(t, http.StatusBadRequest, response.Code)
			assertStructuredError(t, response)
			assert.NotContains(t, response.Body.String(), "layout")
			assert.NotContains(t, response.Body.String(), "panic")
		})
	}
	assert.Zero(t, generations.Load())

	defaultServer := New(service.New(nil, service.NewAdmission(1)), testVersion, zap.NewNop())
	invalidConfig := executeRequest(
		defaultServer.Handler(), http.MethodPost, "/api/v1/generate",
		strings.NewReader(`{"config":{"width":0,"height":1,"seed":"0"}}`),
	)
	assert.Equal(t, http.StatusBadRequest, invalidConfig.Code)
	assertStructuredError(t, invalidConfig)
}

// TestGenerateRejectsOversizedBodyBeforeGeneration checks that a body above
// 1 MiB returns 413 before generation runs.
func TestGenerateRejectsOversizedBodyBeforeGeneration(t *testing.T) {
	t.Parallel()

	var generations atomic.Int32
	server := newTestServer(t, func(context.Context, daedalus.Config) (daedalus.Layout, error) {
		generations.Add(1)
		return daedalus.Layout{}, nil
	}, service.NewAdmission(1), zap.NewNop())
	body := strings.Repeat("x", MaxHTTPDebugBodyBytes+1)

	response := executeRequest(
		server.Handler(), http.MethodPost, "/api/v1/generate", strings.NewReader(body),
	)

	assert.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
	assertStructuredError(t, response)
	assert.Zero(t, generations.Load())
}

// TestGenerateMapsErrorCategoriesWithoutLeakingDetails checks a Grid or Room
// limit: ErrLimitExceeded is mapped to 413, and the body stays free of
// internal detail.
func TestGenerateMapsErrorCategoriesWithoutLeakingDetails(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		err    error
		status int
	}{
		{name: "limit", err: daedalus.ErrLimitExceeded, status: http.StatusRequestEntityTooLarge},
		{name: "plant", err: daedalus.ErrNoCompatiblePlant, status: http.StatusUnprocessableEntity},
		{name: "route", err: daedalus.ErrUnroutableEdge, status: http.StatusUnprocessableEntity},
		{name: "deadline", err: context.DeadlineExceeded, status: http.StatusGatewayTimeout},
		{name: "internal", err: errors.New("internal-secret"), status: http.StatusInternalServerError},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			server := newTestServer(t, func(context.Context, daedalus.Config) (daedalus.Layout, error) {
				return daedalus.Layout{}, testCase.err
			}, service.NewAdmission(1), zap.NewNop())

			response := executeRequest(
				server.Handler(), http.MethodPost, "/api/v1/generate",
				strings.NewReader(`{"config":{"width":1,"height":1,"seed":"0"}}`),
			)

			assert.Equal(t, testCase.status, response.Code)
			assertStructuredError(t, response)
			assert.NotContains(t, response.Body.String(), "internal-secret")
		})
	}
}

// TestHTTPAndRPCShareTheSameAdmission checks that HTTP and gRPC share one
// MaxConcurrentGenerations admission slot.
func TestHTTPAndRPCShareTheSameAdmission(t *testing.T) {
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
	httpServer := New(grpcServer, testVersion, zap.NewNop())

	httpDone := make(chan *httptest.ResponseRecorder)
	go func() {
		httpDone <- executeRequest(
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
		t.Fatal("RPC entered while the HTTP generation held the only slot")
	case <-time.After(30 * time.Millisecond):
	}
	release <- struct{}{}
	<-entered
	release <- struct{}{}

	assert.Equal(t, http.StatusOK, (<-httpDone).Code)
	require.NoError(t, <-rpcDone)
}

// TestClientCancellationReachesTheHTTPGenerator checks that cancelling the
// POST is observed by the generation context.
func TestClientCancellationReachesTheHTTPGenerator(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	canceled := make(chan struct{})
	server := newTestServer(t, func(ctx context.Context, _ daedalus.Config) (daedalus.Layout, error) {
		close(entered)
		<-ctx.Done()
		close(canceled)
		return daedalus.Layout{}, ctx.Err()
	}, service.NewAdmission(1), zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	request := newLocalRequest(
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
		t.Fatalf("handler finished before generation: status=%d body=%s", response.Code, response.Body.String())
	case <-time.After(time.Second):
		t.Fatal("handler did not start generation")
	}

	cancel()

	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("generator did not observe the client cancellation")
	}
	<-done
}

// TestSecurityRejectsRemoteOriginAndLogsOnlyMetadata checks that a remote Host
// or a mismatched Origin is rejected, and that the request body does not
// appear in the logs.
func TestSecurityRejectsRemoteOriginAndLogsOnlyMetadata(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	encoder := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	logger := zap.New(zapcore.NewCore(encoder, zapcore.AddSync(&logs), zapcore.InfoLevel))
	server := newTestServer(t, func(context.Context, daedalus.Config) (daedalus.Layout, error) {
		return daedalus.Layout{}, nil
	}, service.NewAdmission(1), logger)
	secret := "super-secret-seed"

	remoteRequest := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	remoteRequest.RemoteAddr = "192.0.2.10:1234"
	remoteRequest.Host = loopbackTestHost
	remoteResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(remoteResponse, remoteRequest)
	assert.Equal(t, http.StatusForbidden, remoteResponse.Code)

	originRequest := newLocalRequest(http.MethodGet, "/healthz", nil)
	originRequest.Header.Set("Origin", "http://127.0.0.1:9999")
	originResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(originResponse, originRequest)
	assert.Equal(t, http.StatusForbidden, originResponse.Code)

	body := `{"config":{"width":1,"height":1,"seed":"0"},"` + secret + `":"x"}`
	response := executeRequest(
		server.Handler(), http.MethodPost, "/api/v1/generate", strings.NewReader(body),
	)
	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Empty(t, response.Header().Get("Access-Control-Allow-Origin"))
	assert.NotContains(t, logs.String(), secret)
	assert.Contains(t, logs.String(), "request_id")
	assert.Contains(t, logs.String(), "status")
	assert.Contains(t, logs.String(), "duration")
}

// TestHealthBecomesUnavailableDuringShutdown checks that once shutdown begins,
// GET /healthz returns 503.
func TestHealthBecomesUnavailableDuringShutdown(t *testing.T) {
	t.Parallel()

	serviceServer := service.New(
		func(context.Context, daedalus.Config) (daedalus.Layout, error) {
			return daedalus.Layout{}, nil
		},
		service.NewAdmission(1),
	)
	server := New(serviceServer, testVersion, zap.NewNop())
	serviceServer.BeginShutdown()

	response := executeRequest(server.Handler(), http.MethodGet, "/healthz", nil)

	assert.Equal(t, http.StatusServiceUnavailable, response.Code)
	assert.JSONEq(t, `{"status":"unavailable","version":"v0.1.0-test"}`, response.Body.String())
}

func newTestServer(
	t *testing.T,
	generate service.GenerateFunc,
	admission *service.Admission,
	logger *zap.Logger,
) *Server {
	t.Helper()
	return New(service.New(generate, admission), testVersion, logger)
}

func executeRequest(
	handler http.Handler,
	method string,
	target string,
	body io.Reader,
) *httptest.ResponseRecorder {
	request := newLocalRequest(method, target, body)
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func newLocalRequest(
	method string,
	target string,
	body io.Reader,
) *http.Request {
	request := httptest.NewRequest(method, target, body)
	request.RemoteAddr = remoteLoopbackAddress
	request.Host = loopbackTestHost
	return request
}

func assertStructuredError(t *testing.T, response *httptest.ResponseRecorder) {
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
