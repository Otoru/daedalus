package vision_test

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Otoru/daedalus"
	"github.com/Otoru/daedalus/utils/vision"
	"github.com/stretchr/testify/require"
)

var updateVisionGoldens = flag.Bool("update-vision", false, "rewrite vision golden fixtures after explicit review")

type visionGolden struct {
	Name    string   `json:"name"`
	Width   uint32   `json:"width"`
	Height  uint32   `json:"height"`
	Origin  cellJSON `json:"origin"`
	Radius  uint32   `json:"radius"`
	Opacity []string `json:"opacity"`
	Visible []string `json:"visible"`
}

type cellJSON struct {
	X int32 `json:"x"`
	Y int32 `json:"y"`
}

type visionGoldenCase struct {
	name    string
	width   int
	height  int
	origin  daedalus.Cell
	radius  uint32
	opacity []string
}

// TestFrozenVisionGoldens freezes the human-readable decisions that define
// v1 visibility: walls are visible, radius is inclusive, and corner/gap
// rounding remains reviewable as rows of bits. Failures name the first cell.
func TestFrozenVisionGoldens(t *testing.T) {
	for _, testCase := range visionGoldenCases() {
		t.Run(testCase.name, func(t *testing.T) {
			grid := goldenGrid(t, testCase)
			field, err := vision.Compute(context.Background(), grid, testCase.origin, testCase.radius)
			require.NoError(t, err)
			got := goldenValue(testCase, field)
			path := filepath.Join("testdata", "golden", testCase.name+".json")
			if *updateVisionGoldens {
				writeVisionGolden(t, path, got)
			}
			expected := readVisionGolden(t, path)
			if difference := firstGoldenDifference(expected, got); difference != "" {
				t.Fatalf("visibility fixture %s: %s", path, difference)
			}
		})
	}
}

func TestVisionGoldenDiagnosticNamesFirstCell(t *testing.T) {
	expected := visionGolden{Width: 2, Height: 1, Visible: []string{".."}}
	actual := visionGolden{Width: 2, Height: 1, Visible: []string{".#"}}
	require.Equal(t, "visible[0][1] (1,0): expected '.'; actual '#'", firstGoldenDifference(expected, actual))
}

func visionGoldenCases() []visionGoldenCase {
	return []visionGoldenCase{
		{name: "open_disk_inclusive_radius", width: 5, height: 5, origin: daedalus.Cell{X: 2, Y: 2}, radius: 2, opacity: []string{".....", ".....", ".....", ".....", "....."}},
		{name: "solid_wall_visible_and_blocks", width: 7, height: 3, origin: daedalus.Cell{X: 0, Y: 1}, radius: 6, opacity: []string{"..#....", "..#....", "..#...."}},
		{name: "diagonal_corner", width: 5, height: 5, origin: daedalus.Cell{X: 0, Y: 0}, radius: 4, opacity: []string{".....", ".#...", ".....", ".....", "....."}},
		{name: "edge_clipping", width: 6, height: 4, origin: daedalus.Cell{X: 0, Y: 2}, radius: 3, opacity: []string{"......", "......", "......", "......"}},
	}
}

func goldenGrid(t *testing.T, testCase visionGoldenCase) vision.OpacityGrid {
	t.Helper()
	require.Len(t, testCase.opacity, testCase.height)
	grid := vision.OpacityGrid{Width: uint32(testCase.width), Height: uint32(testCase.height), Transparent: make([]byte, (testCase.width*testCase.height+7)/8)}
	for y, row := range testCase.opacity {
		require.Len(t, row, testCase.width)
		for x, cell := range row {
			require.Contains(t, ".#", string(cell))
			grid.SetTransparent(daedalus.Cell{X: int32(x), Y: int32(y)}, cell == '.')
		}
	}
	return grid
}

func goldenValue(testCase visionGoldenCase, field vision.Field) visionGolden {
	return visionGolden{
		Name: testCase.name, Width: field.Width, Height: field.Height,
		Origin: cellJSON{X: testCase.origin.X, Y: testCase.origin.Y}, Radius: testCase.radius,
		Opacity: append([]string(nil), testCase.opacity...), Visible: fieldRows(field),
	}
}

func fieldRows(field vision.Field) []string {
	rows := make([]string, field.Height)
	for y := uint32(0); y < field.Height; y++ {
		var row strings.Builder
		row.Grow(int(field.Width))
		for x := uint32(0); x < field.Width; x++ {
			if field.VisibleAt(daedalus.Cell{X: int32(x), Y: int32(y)}) {
				row.WriteByte('#')
			} else {
				row.WriteByte('.')
			}
		}
		rows[y] = row.String()
	}
	return rows
}

func firstGoldenDifference(expected, actual visionGolden) string {
	if expected.Width != actual.Width || expected.Height != actual.Height {
		return fmt.Sprintf("shape: expected %dx%d; actual %dx%d", expected.Width, expected.Height, actual.Width, actual.Height)
	}
	for y := uint32(0); y < expected.Height; y++ {
		for x := uint32(0); x < expected.Width; x++ {
			expectedCell := rowCell(expected.Visible, y, x)
			actualCell := rowCell(actual.Visible, y, x)
			if expectedCell != actualCell {
				return fmt.Sprintf("visible[%d][%d] (%d,%d): expected %q; actual %q", y, x, x, y, expectedCell, actualCell)
			}
		}
	}
	return ""
}

func rowCell(rows []string, y, x uint32) byte {
	if int(y) >= len(rows) || int(x) >= len(rows[y]) {
		return '?'
	}
	return rows[y][x]
}

func readVisionGolden(t *testing.T, path string) visionGolden {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err, "fixture missing; regenerate explicitly with go test ./utils/vision -run '^TestFrozenVisionGoldens$' -update-vision")
	var fixture visionGolden
	require.NoError(t, json.Unmarshal(data, &fixture))
	return fixture
}

func writeVisionGolden(t *testing.T, path string, fixture visionGolden) {
	t.Helper()
	data, err := json.MarshalIndent(fixture, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(data, '\n'), 0o644))
}
