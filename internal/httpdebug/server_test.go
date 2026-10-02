package httpdebug

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/Otoru/daedalus/utils/vision"
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
		{path: "/debug/platform-layout.fixture.json", contentType: "application/json", snippet: `"jump_graph"`},
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

// TestPlatformDebugAssetsExposeDirectedGraphControls locks the local fixture
// seam until GeneratePlatform is wired. The renderer must make direction,
// ability filtering, the three-valued judgement, and cell-to-node inspection
// visible without depending on a remote asset or endpoint.
func TestPlatformDebugAssetsExposeDirectedGraphControls(t *testing.T) {
	t.Parallel()

	app, err := assets.ReadFile(debugJSPath)
	require.NoError(t, err)
	page, err := assets.ReadFile(debugIndexPath)
	require.NoError(t, err)
	fixture, err := assets.ReadFile(debugPlatformFixturePath)
	require.NoError(t, err)

	assert.Contains(t, string(app), "function drawJumpGraph(context, scale)")
	assert.Contains(t, string(app), "function setLayoutForRendering(layout)")
	assert.Contains(t, string(app), "motion_node_ids")
	assert.Contains(t, string(app), "drawArrow")
	assert.Contains(t, string(page), `id="ability-dash"`)
	assert.Contains(t, string(page), `id="ability-double-jump"`)
	assert.Contains(t, string(page), `id="ability-wall-jump"`)
	assert.Contains(t, string(fixture), `"semi-solid"`)
	assert.Contains(t, string(fixture), `"hazard"`)
	assert.Contains(t, string(fixture), `"verdict":"unknown"`)
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

// TestDebugPageExploresTheFieldThroughComputeSteps checks that the embedded
// page posts a real ComputeSteps body — base64 costs, no numeric cost array —
// and that the controls name the goal click, the position click, flee, and
// the Unreachable and Blocked outcomes.
func TestDebugPageExploresTheFieldThroughComputeSteps(t *testing.T) {
	t.Parallel()

	script, err := fs.ReadFile(assets, "assets/app.js")
	require.NoError(t, err)
	page, err := fs.ReadFile(assets, "assets/index.html")
	require.NoError(t, err)
	source := string(script)
	markup := string(page)

	assert.Contains(t, source, "/api/v1/compute-steps")
	assert.Contains(t, source, "CELL_KIND_ROOM")
	assert.Contains(t, source, "CELL_KIND_CORRIDOR")
	assert.Contains(t, source, "btoa")
	assert.NotContains(t, source, "costs: [")
	assert.Contains(t, source, "STEP_STATUS_UNREACHABLE")
	assert.Contains(t, source, "STEP_STATUS_BLOCKED")
	assert.Contains(t, markup, `id="flee"`)
	assert.Contains(t, markup, `id="click-mode"`)
	assert.Contains(t, markup, "goal")
	assert.Contains(t, markup, "position")
	assert.Contains(t, source, "/api/v1/compute-visibility")
	assert.Contains(t, source, "opacity_grid")
	assert.Contains(t, source, "visible_to_observer")
	assert.Contains(t, markup, `id="visibility-radius"`)
	assert.Contains(t, markup, `id="visibility-mode"`)
	assert.Contains(t, source, "/api/v1/build-gating-plan")
	assert.Contains(t, source, "drawGatingPlan")
	assert.Contains(t, markup, `id="build-gating"`)
	assert.Contains(t, markup, "Main gate")
}

// TestDebugMapExampleMarksRoomRoles checks that the shipped example asks for
// one Start, one Boss and a few Treasure rooms, that the server accepts that
// literal request, and that the page names each role in the legend and draws
// a marker from the room role.
func TestDebugMapExampleMarksRoomRoles(t *testing.T) {
	t.Parallel()

	script, err := fs.ReadFile(assets, "assets/app.js")
	require.NoError(t, err)
	page, err := fs.ReadFile(assets, "assets/index.html")
	require.NoError(t, err)
	style, err := fs.ReadFile(assets, "assets/styles.css")
	require.NoError(t, err)

	example := exampleRequestFromScript(t, string(script))
	var request struct {
		Config struct {
			RoomRoleRequests []struct {
				Role  string `json:"role"`
				Count uint32 `json:"count"`
			} `json:"room_role_requests"`
		} `json:"config"`
	}
	require.NoError(t, json.Unmarshal([]byte(example), &request))

	counts := map[string]uint32{}
	for _, item := range request.Config.RoomRoleRequests {
		counts[item.Role] = item.Count
	}
	assert.Equal(t, uint32(1), counts["ROOM_ROLE_START"])
	assert.Equal(t, uint32(1), counts["ROOM_ROLE_BOSS"])
	assert.GreaterOrEqual(t, counts["ROOM_ROLE_TREASURE"], uint32(2))
	assert.LessOrEqual(t, counts["ROOM_ROLE_TREASURE"], uint32(4))

	source := string(script)
	assert.Contains(t, source, "drawRoleMarkers(context, scale)")
	assert.Contains(t, source, "ROOM_ROLE_START")
	assert.Contains(t, source, "ROOM_ROLE_BOSS")
	assert.Contains(t, source, "ROOM_ROLE_TREASURE")
	assert.Contains(t, source, "cellColors.door")

	markup := string(page)
	assert.Contains(t, markup, ">Start<")
	assert.Contains(t, markup, ">Boss<")
	assert.Contains(t, markup, ">Treasure<")
	assert.Contains(t, markup, `class="role-mark"`)
	assert.Contains(t, string(style), ".role-mark")
	assert.Contains(t, string(style), "var(--door-cell)")

	generator := daedalus.Generator{}
	server := newTestServer(t, generator.GenerateContext, service.NewAdmission(1), zap.NewNop())
	response := executeRequest(
		server.Handler(), http.MethodPost, "/api/v1/generate", strings.NewReader(example),
	)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	var layout struct {
		Rooms []struct {
			Role string `json:"role"`
		} `json:"rooms"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &layout))
	assigned := map[string]int{}
	for _, room := range layout.Rooms {
		if room.Role != "" {
			assigned[room.Role]++
		}
	}
	assert.Equal(t, 1, assigned["ROOM_ROLE_START"])
	assert.Equal(t, 1, assigned["ROOM_ROLE_BOSS"])
	assert.Equal(t, int(counts["ROOM_ROLE_TREASURE"]), assigned["ROOM_ROLE_TREASURE"])
}

// TestDebugPageUsesIndependentSidePanels checks that sending a request and
// reading the map live in two edge panels that start closed, so the map is
// full width, and that opening one does not close the other. The canvas still
// sizes its backing store from the CSS box and the device pixel ratio.
func TestDebugPageUsesIndependentSidePanels(t *testing.T) {
	t.Parallel()

	script, err := fs.ReadFile(assets, "assets/app.js")
	require.NoError(t, err)
	page, err := fs.ReadFile(assets, "assets/index.html")
	require.NoError(t, err)
	style, err := fs.ReadFile(assets, "assets/styles.css")
	require.NoError(t, err)

	markup := string(page)
	source := string(script)
	css := string(style)

	assert.NotContains(t, markup, `class="toolbar"`)
	assert.NotContains(t, markup, `id="json-drawer"`)
	assert.NotContains(t, css, ".toolbar")
	assert.NotContains(t, css, ".drawer")
	assert.Regexp(t, `(?s)body\s*\{[^}]*flex-direction:\s*row`, css)
	assert.Contains(t, css, ".side-panel")
	assert.Contains(t, css, "flex: 0 0 min(400px, 38vw)")

	requestPane := elementOuterHTML(t, markup, "request-pane")
	responsePane := elementOuterHTML(t, markup, "response-pane")
	viewport := elementOuterHTML(t, markup, "map-viewport")

	assert.Contains(t, openingTag(t, requestPane), "hidden")
	assert.Contains(t, openingTag(t, responsePane), "hidden")
	assert.NotContains(t, requestPane, `id="request-trigger"`)
	assert.NotContains(t, responsePane, `id="response-trigger"`)
	assert.Contains(t, markup, `id="request-trigger"`)
	assert.Contains(t, markup, `aria-controls="request-pane"`)
	assert.Contains(t, markup, `aria-expanded="false"`)
	assert.Contains(t, markup, `id="response-trigger"`)
	assert.Contains(t, markup, `aria-controls="response-pane"`)

	assert.Contains(t, requestPane, `id="generate"`)
	assert.Contains(t, requestPane, `id="load-example"`)
	assert.Contains(t, requestPane, `id="request-editor"`)
	assert.NotContains(t, requestPane, `id="response-viewer"`)
	assert.NotContains(t, requestPane, `id="zoom"`)

	assert.Contains(t, responsePane, `id="zoom"`)
	assert.Contains(t, responsePane, `id="fit-map"`)
	assert.Contains(t, responsePane, `id="show-centerline"`)
	assert.Contains(t, responsePane, `id="flee"`)
	assert.Contains(t, responsePane, `class="legend"`)
	assert.Contains(t, responsePane, `id="cell-inspector"`)
	assert.Contains(t, responsePane, `id="response-viewer"`)
	assert.NotContains(t, responsePane, `id="request-editor"`)
	assert.NotContains(t, viewport, `id="cell-inspector"`)

	assert.Contains(t, source, "openPanels")
	assert.NotContains(t, source, "openDrawer")
	assert.NotContains(t, source, "pane.hidden = !open")
	assert.NotContains(t, source, `id="json-drawer"`)
	assert.Contains(t, source, `event.key !== "Escape"`)
	assert.Contains(t, source, "new ResizeObserver")
	assert.Contains(t, source, "canvas.width = Math.max(1, Math.round(cssWidth * dpr))")
	assert.Contains(t, source, "canvas.height = Math.max(1, Math.round(cssHeight * dpr))")
	assert.Contains(t, source, "scheduleViewportSync")
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

func elementOuterHTML(t *testing.T, markup, id string) string {
	t.Helper()

	idAt := strings.Index(markup, `id="`+id+`"`)
	require.NotEqual(t, -1, idAt, id)
	tagStart := strings.LastIndex(markup[:idAt], "<")
	require.NotEqual(t, -1, tagStart, id)
	openLength, name := readTag(markup[tagStart:])
	openTag := markup[tagStart : tagStart+openLength]
	if strings.HasSuffix(strings.TrimSpace(openTag), "/>") {
		return openTag
	}

	pos := tagStart + openLength
	depth := 1
	lower := strings.ToLower(markup)
	name = strings.ToLower(name)
	for depth > 0 && pos < len(markup) {
		next := strings.Index(lower[pos:], "<")
		require.NotEqual(t, -1, next, id)
		pos += next
		if strings.HasPrefix(lower[pos:], "</"+name) && tagBoundary(lower[pos+2+len(name):]) {
			depth--
			end := strings.Index(markup[pos:], ">")
			require.NotEqual(t, -1, end, id)
			if depth == 0 {
				return markup[tagStart : pos+end+1]
			}
			pos += end + 1
			continue
		}
		if strings.HasPrefix(lower[pos:], "<"+name) && tagBoundary(lower[pos+1+len(name):]) {
			end := strings.Index(markup[pos:], ">")
			require.NotEqual(t, -1, end, id)
			tag := markup[pos : pos+end+1]
			if !strings.HasSuffix(strings.TrimSpace(tag), "/>") {
				depth++
			}
			pos += end + 1
			continue
		}
		pos++
	}
	t.Fatalf("unclosed element %s", id)
	return ""
}

func openingTag(t *testing.T, element string) string {
	t.Helper()
	end := strings.Index(element, ">")
	require.NotEqual(t, -1, end)
	return element[:end+1]
}

func readTag(from string) (int, string) {
	index := 1
	for index < len(from) && (from[index] == '/' || from[index] == ' ' || from[index] == '\n' || from[index] == '\t') {
		index++
	}
	start := index
	for index < len(from) && from[index] != ' ' && from[index] != '>' && from[index] != '/' && from[index] != '\n' && from[index] != '\t' {
		index++
	}
	end := strings.Index(from, ">")
	if end < 0 {
		return len(from), from[start:index]
	}
	return end + 1, from[start:index]
}

func tagBoundary(rest string) bool {
	if rest == "" {
		return false
	}
	switch rest[0] {
	case ' ', '\n', '\t', '>', '/':
		return true
	default:
		return false
	}
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
				"max_footprint_cells": 1,
				"min_room_gap": 0,
				"shapes": [{
					"shape": "ROOM_SHAPE_RECTANGLE",
					"weight": 1,
					"width": {"min": 1, "max": 1},
					"height": {"min": 1, "max": 1}
				}]
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
			MaxFootprintCells: 1,
			Shapes: []daedalus.RoomShapeWeight{{
				Shape: daedalus.RoomShapeRectangle, Weight: 1,
				Width:  daedalus.DimensionRange{Min: 1, Max: 1},
				Height: daedalus.DimensionRange{Min: 1, Max: 1},
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
				"max_footprint_cells": 9,
				"min_room_gap": 2,
				"shapes": [{
					"shape": "ROOM_SHAPE_RECTANGLE",
					"weight": 1,
					"width": {"min": 2, "max": 3},
					"height": {"min": 2, "max": 3}
				}]
			},
			"corridor_geometry": {
				"widths": [{"width": 1, "weight": 1}, {"width": 2, "weight": 8}]
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
// 32 MiB returns 413 before generation runs.
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

// TestComputeStepsHTTPMatchesTheService checks that POST /api/v1/compute-steps
// accepts canonical ProtoJSON, including a base64 costs payload, and returns
// the same steps the gRPC method returns for that request. Flee is a field on
// that request, not a second algorithm.
func TestComputeStepsHTTPMatchesTheService(t *testing.T) {
	t.Parallel()

	var generations atomic.Int32
	serviceServer := service.New(func(context.Context, daedalus.Config) (daedalus.Layout, error) {
		generations.Add(1)
		return daedalus.Layout{}, nil
	}, service.NewAdmission(1))
	server := New(serviceServer, testVersion, zap.NewNop())

	for _, flee := range []bool{false, true} {
		flee := flee
		t.Run(fmt.Sprintf("flee=%t", flee), func(t *testing.T) {
			body := computeStepsBody(t, []byte{1, 1, 1, 1}, 4, 1, flee)
			response := executeRequest(
				server.Handler(), http.MethodPost, "/api/v1/compute-steps", strings.NewReader(body),
			)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			assert.Equal(t, "application/json", response.Header().Get("Content-Type"))

			var wire daedalusv1.ComputeStepsRequest
			require.NoError(t, (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal([]byte(body), &wire))
			direct, err := serviceServer.ComputeSteps(context.Background(), &wire)
			require.NoError(t, err)
			encoded, err := (protojson.MarshalOptions{
				UseProtoNames:   true,
				EmitUnpopulated: true,
			}).Marshal(direct)
			require.NoError(t, err)
			assert.JSONEq(t, string(encoded), response.Body.String())
			assert.NotContains(t, response.Body.String(), "request_id")
		})
	}
	assert.Zero(t, generations.Load())

	without := executeRequest(
		server.Handler(), http.MethodPost, "/api/v1/compute-steps",
		strings.NewReader(computeStepsBody(t, []byte{1, 1, 1, 1}, 4, 1, false)),
	)
	with := executeRequest(
		server.Handler(), http.MethodPost, "/api/v1/compute-steps",
		strings.NewReader(computeStepsBody(t, []byte{1, 1, 1, 1}, 4, 1, true)),
	)
	require.Equal(t, http.StatusOK, without.Code, without.Body.String())
	require.Equal(t, http.StatusOK, with.Code, with.Body.String())
	assert.NotEqual(t, without.Body.String(), with.Body.String(), "flee must change the steps")
}

func TestComputeVisibilityHTTPMatchesTheServiceContract(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(context.Context, daedalus.Config) (daedalus.Layout, error) {
		return daedalus.Layout{}, nil
	}, service.NewAdmission(1), zap.NewNop())
	body := `{"opacity_grid":{"width":3,"height":1,"transparent":"Bw=="},"queries":[{"origin":{"x":1,"y":0},"radius":1}]}`
	response := executeRequest(server.Handler(), http.MethodPost, "/api/v1/compute-visibility", strings.NewReader(body))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, "application/json", response.Header().Get("Content-Type"))

	var decoded struct {
		Fields []struct {
			Visible string `json:"visible"`
		} `json:"fields"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &decoded))
	require.Len(t, decoded.Fields, 1)
	assert.Equal(t, "Bw==", decoded.Fields[0].Visible)
}

func TestBuildGatingPlanHTTPReturnsTheServicePlan(t *testing.T) {
	t.Parallel()
	server := newTestServer(t, nil, service.NewAdmission(1), zap.NewNop())
	request := &daedalusv1.BuildGatingPlanRequest{
		Layout: &daedalusv1.Layout{
			Grid: &daedalusv1.Grid{Width: 1, Height: 1, Cells: []*daedalusv1.CellState{{At: &daedalusv1.Cell{}}}},
			Rooms: []*daedalusv1.Room{
				{Id: 0, At: &daedalusv1.Cell{}, Origin: &daedalusv1.Cell{}, DoorIds: []uint32{0}},
				{Id: 1, At: &daedalusv1.Cell{}, Origin: &daedalusv1.Cell{}, DoorIds: []uint32{1}},
			},
			Corridors: []*daedalusv1.Corridor{{Id: 0, FromRoomId: 0, ToRoomId: 1, FromDoorId: 0, ToDoorId: 1}},
			Doors:     []*daedalusv1.Door{{Id: 0, RoomId: 0, At: &daedalusv1.Cell{}, CorridorIds: []uint32{0}}, {Id: 1, RoomId: 1, At: &daedalusv1.Cell{}, CorridorIds: []uint32{0}}},
		},
		Request: &daedalusv1.GatingRequest{Seed: 7, StartRoomId: 0, MainGateCount: 1},
	}
	body, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(request)
	require.NoError(t, err)
	response := executeRequest(server.Handler(), http.MethodPost, "/api/v1/build-gating-plan", bytes.NewReader(body))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var plan daedalusv1.BuildGatingPlanResponse
	require.NoError(t, (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(response.Body.Bytes(), &plan))
	require.NotNil(t, plan.Plan)
	require.Len(t, plan.Plan.Gates, 1)
	assert.Equal(t, uint32(0), plan.Plan.Gates[0].DoorId)
}

func TestGeneratedLayoutDefaultOpacityRoundTripsThroughVisibilityHTTP(t *testing.T) {
	t.Parallel()

	generator := daedalus.Generator{}
	layout, err := generator.GenerateContext(context.Background(), daedalus.Config{
		Width: 32, Height: 32, Seed: 4242, MaxAttempts: 30, MaxRooms: 32,
	})
	require.NoError(t, err)
	opacity := vision.NewOpacityGrid(layout)
	require.NoError(t, opacity.Validate())

	var origin daedalus.Cell
	found := false
	for index, cell := range layout.Grid.Cells {
		if vision.DefaultOpacityRule(cell) {
			origin = daedalus.Cell{X: int32(index % int(layout.Grid.Width)), Y: int32(index / int(layout.Grid.Width))}
			found = true
			break
		}
	}
	require.True(t, found)
	direct, err := vision.Compute(context.Background(), opacity, origin, 6)
	require.NoError(t, err)

	server := newTestServer(t, generator.GenerateContext, service.NewAdmission(1), zap.NewNop())
	body := fmt.Sprintf(`{"opacity_grid":{"width":%d,"height":%d,"transparent":%q},"queries":[{"origin":{"x":%d,"y":%d},"radius":6}]}`,
		layout.Grid.Width, layout.Grid.Height, base64.StdEncoding.EncodeToString(opacity.Transparent), origin.X, origin.Y)
	response := executeRequest(server.Handler(), http.MethodPost, "/api/v1/compute-visibility", strings.NewReader(body))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var wire daedalusv1.ComputeVisibilityResponse
	require.NoError(t, (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(response.Body.Bytes(), &wire))
	require.Len(t, wire.Fields, 1)
	assert.Equal(t, direct.Visible, wire.Fields[0].Visible)
}

func computeStepsBody(t *testing.T, costs []byte, width, height int, flee bool) string {
	t.Helper()
	return fmt.Sprintf(`{
		"cost_grid": {"width": %d, "height": %d, "costs": %q},
		"queries": [{
			"sources": [{"at": {"x": 0, "y": 0}}],
			"positions": [{"x": %d, "y": 0}],
			"flee": %t
		}]
	}`, width, height, base64.StdEncoding.EncodeToString(costs), width-1, flee)
}

// TestComputeStepsRejectsNonBase64CostsAndAMissingGrid checks that a bytes
// field has to be base64 ProtoJSON, and that the structured error shape is
// the same one generate uses.
func TestComputeStepsRejectsNonBase64CostsAndAMissingGrid(t *testing.T) {
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
		status      int
	}{
		{name: "missing content type", body: `{"cost_grid":{"width":1,"height":1,"costs":"AQ=="}}`, status: http.StatusBadRequest},
		{name: "incorrect content type", contentType: "text/plain", body: `{}`, status: http.StatusBadRequest},
		{name: "missing cost grid", contentType: "application/json", body: `{}`, status: http.StatusBadRequest},
		{name: "missing costs", contentType: "application/json", body: `{"cost_grid":{"width":1,"height":1}}`, status: http.StatusBadRequest},
		{name: "costs as a number array", contentType: "application/json", body: `{"cost_grid":{"width":1,"height":1,"costs":[1]}}`, status: http.StatusBadRequest},
		{name: "costs not base64", contentType: "application/json", body: `{"cost_grid":{"width":1,"height":1,"costs":"****"}}`, status: http.StatusBadRequest},
		{name: "camel case", contentType: "application/json", body: `{"costGrid":{"width":1,"height":1,"costs":"AQ=="}}`, status: http.StatusBadRequest},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			request := newLocalRequest(http.MethodPost, "/api/v1/compute-steps", strings.NewReader(testCase.body))
			if testCase.contentType != "" {
				request.Header.Set("Content-Type", testCase.contentType)
			}
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			assert.Equal(t, testCase.status, response.Code)
			assertStructuredError(t, response)
			assert.NotContains(t, response.Body.String(), "panic")
		})
	}
	assert.Zero(t, generations.Load())
}

// TestComputeStepsRejectsAnOversizedBody checks the same 32 MiB ceiling as
// generate, before the service reads the grid.
func TestComputeStepsRejectsAnOversizedBody(t *testing.T) {
	t.Parallel()

	var generations atomic.Int32
	server := newTestServer(t, func(context.Context, daedalus.Config) (daedalus.Layout, error) {
		generations.Add(1)
		return daedalus.Layout{}, nil
	}, service.NewAdmission(1), zap.NewNop())
	body := strings.Repeat("x", MaxHTTPDebugBodyBytes+1)

	response := executeRequest(
		server.Handler(), http.MethodPost, "/api/v1/compute-steps", strings.NewReader(body),
	)

	assert.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
	assertStructuredError(t, response)
	assert.Zero(t, generations.Load())
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
