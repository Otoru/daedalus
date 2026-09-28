package pathfinding

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var updateGoldenFields = flag.Bool("update", false, "rewrite pathfinding golden fixtures after explicit review")

// goldenField is one frozen navigation result. Costs and distances are
// row-major rows so a failing fixture can be read as a grid. Steps are a
// handful of positions, not the whole map: the tie-break and the flee
// composition show up there, and the distance rows come along as the oracle
// for everything that is not a choice.
type goldenField struct {
	Name      string         `json:"name"`
	Width     uint32         `json:"width"`
	Height    uint32         `json:"height"`
	Flee      bool           `json:"flee"`
	Costs     [][]int        `json:"costs"`
	Sources   []goldenSource `json:"sources"`
	Distances [][]int32      `json:"distances"`
	Steps     []goldenStep   `json:"steps"`
}

type goldenSource struct {
	X    int32 `json:"x"`
	Y    int32 `json:"y"`
	Bias int32 `json:"bias"`
}

type goldenStep struct {
	X         int32  `json:"x"`
	Y         int32  `json:"y"`
	Status    string `json:"status"`
	Direction string `json:"direction,omitempty"`
	Distance  int32  `json:"distance"`
}

type goldenFieldCase struct {
	name      string
	flee      bool
	costs     [][]uint8
	sources   []Source
	positions []daedalus.Cell
}

// TestFrozenGoldenFields checks five hand-built grids against
// utils/pathfinding/testdata/golden. A mismatch names the path, the expected
// value, and the actual value, so a regression cannot hide behind a hash.
//
// Two results are choices. Step scans North, East, South, West and keeps the
// first strict minimum. Flee scales by the package factor, truncates toward
// zero, then rescans. The distance array is a pure function of the grid and
// the sources; it is stored in the same file.
//
// -update rewrites a fixture that a person is about to read. It is not a way
// to turn a red test green.
func TestFrozenGoldenFields(t *testing.T) {
	for _, testCase := range goldenFieldCases() {
		t.Run(testCase.name, func(t *testing.T) {
			got := computeGoldenField(t, testCase)
			fixturePath := filepath.Join("testdata", "golden", testCase.name+".json")
			if *updateGoldenFields {
				writeGoldenField(t, fixturePath, got)
			}

			expected := readGoldenField(t, fixturePath)
			failures := goldenFieldFailures(expected, got)
			if len(failures) > 0 {
				assert.Fail(t, "field diverged from frozen fixture", strings.Join(failures, "\n"))
			}
		})
	}
}

// TestGoldenFieldDiagnosticNamesPathExpectedAndActual checks that a mismatch
// is reported as a path plus the two scalars. A hash, or a single "differs",
// would not say which decision moved.
func TestGoldenFieldDiagnosticNamesPathExpectedAndActual(t *testing.T) {
	expected := goldenField{
		Distances: [][]int32{{0, -1}, {2, 3}},
		Steps:     []goldenStep{{X: 1, Y: 1, Status: "moved", Direction: "north", Distance: 0}},
	}
	actual := goldenField{
		Distances: [][]int32{{0, 9}, {2, 3}},
		Steps:     []goldenStep{{X: 1, Y: 1, Status: "moved", Direction: "west", Distance: 0}},
	}

	assert.Equal(t, []string{
		"distances[0][1]: expected -1; actual 9",
		"steps[0].direction: expected north; actual west",
	}, goldenFieldFailures(expected, actual))
	assert.Empty(t, goldenFieldFailures(expected, expected))
}

func goldenFieldCases() []goldenFieldCase {
	// A vertical wall splits the grid. The east component is passable and
	// still Unreachable: the frozen value is -1, not a large finite sentinel.
	wall := [][]uint8{
		{1, 1, 0, 1, 1},
		{1, 1, 0, 1, 1},
		{1, 1, 0, 1, 1},
	}

	// Column 2 of the middle row is the door, cost 15, with walls on either
	// side. The cheap rows reach the far side without paying 15, so the door
	// cell stays expensive and the far side stays cheap.
	door := [][]uint8{
		{1, 1, 1, 1, 1, 1, 1},
		{1, 0, 15, 0, 1, 1, 1},
		{1, 1, 1, 1, 1, 1, 1},
	}

	corridor := make([][]uint8, 1)
	corridor[0] = []uint8{1, 1, 1, 1, 1, 1, 1}

	return []goldenFieldCase{
		{
			name:  "wall_split_unreachable",
			costs: wall,
			sources: []Source{
				{At: daedalus.Cell{X: 0, Y: 0}},
			},
			positions: []daedalus.Cell{
				{X: 0, Y: 0},
				{X: 1, Y: 0},
				{X: 0, Y: 2},
				{X: 4, Y: 0},
				{X: 3, Y: 1},
			},
		},
		{
			name:  "expensive_door",
			costs: door,
			sources: []Source{
				{At: daedalus.Cell{X: 0, Y: 1}},
			},
			positions: []daedalus.Cell{
				{X: 0, Y: 1},
				{X: 2, Y: 0},
				{X: 5, Y: 0},
				{X: 6, Y: 2},
			},
		},
		{
			// Bias 3 on the east goal yields the west goal at x=4 and still
			// wins at x=5. Equal biases would hand x=4 to the east goal.
			name:  "biased_goals",
			costs: corridor,
			sources: []Source{
				{At: daedalus.Cell{X: 0, Y: 0}},
				{At: daedalus.Cell{X: 6, Y: 0}, Bias: 3},
			},
			positions: []daedalus.Cell{
				{X: 0, Y: 0},
				{X: 4, Y: 0},
				{X: 5, Y: 0},
				{X: 6, Y: 0},
			},
		},
		{
			// Row 0 is the corridor. Column 19 is a one-cell branch: (19,1)
			// is the entrance and (19,2) is the dead end. The threat sits at
			// the east end. Flee, not scale alone, is what points the
			// entrance north and leaves the safe pocket at (0,0).
			name:  "flee_rescan",
			flee:  true,
			costs: fleeCorridorCosts(),
			sources: []Source{
				{At: daedalus.Cell{X: fleeCorridorWidth - 1, Y: 0}},
			},
			positions: []daedalus.Cell{
				{X: 0, Y: 0},
				{X: fleeBranchColumn, Y: 1},
				{X: fleeBranchColumn, Y: 2},
				{X: fleeCorridorWidth - 1, Y: 0},
				{X: 1, Y: 0},
			},
		},
		{
			// Four sources on the edge centres. The centre's four neighbours
			// share a distance, and the step is North because that direction
			// is scanned first.
			name:  "four_way_tie",
			costs: [][]uint8{{1, 1, 1}, {1, 1, 1}, {1, 1, 1}},
			sources: []Source{
				{At: daedalus.Cell{X: 1, Y: 0}},
				{At: daedalus.Cell{X: 2, Y: 1}},
				{At: daedalus.Cell{X: 1, Y: 2}},
				{At: daedalus.Cell{X: 0, Y: 1}},
			},
			positions: []daedalus.Cell{
				{X: 1, Y: 1},
				{X: 1, Y: 0},
				{X: 2, Y: 1},
				{X: 1, Y: 2},
				{X: 0, Y: 1},
			},
		},
	}
}

const (
	fleeCorridorWidth = 24
	fleeBranchColumn  = 19
)

func fleeCorridorCosts() [][]uint8 {
	rows := [][]uint8{
		make([]uint8, fleeCorridorWidth),
		make([]uint8, fleeCorridorWidth),
		make([]uint8, fleeCorridorWidth),
	}
	for x := range rows[0] {
		rows[0][x] = uint8(MinCost)
	}
	rows[1][fleeBranchColumn] = uint8(MinCost)
	rows[2][fleeBranchColumn] = uint8(MinCost)
	return rows
}

func computeGoldenField(t *testing.T, testCase goldenFieldCase) goldenField {
	t.Helper()
	grid := testCase.grid(t)
	field, err := Compute(context.Background(), grid, testCase.sources)
	require.NoError(t, err)
	if testCase.flee {
		err = Flee(context.Background(), &field, grid, defaultFleeNumerator, defaultFleeDenominator)
		require.NoError(t, err)
	}

	return goldenField{
		Name:      testCase.name,
		Width:     grid.Width,
		Height:    grid.Height,
		Flee:      testCase.flee,
		Costs:     costRows(testCase.costs),
		Sources:   goldenSources(testCase.sources),
		Distances: distanceRows(field),
		Steps:     goldenSteps(field, testCase.positions),
	}
}

func (testCase goldenFieldCase) grid(t *testing.T) CostGrid {
	t.Helper()
	require.NotEmpty(t, testCase.costs)
	width := len(testCase.costs[0])
	require.Positive(t, width)
	costs := make([]Cost, 0, len(testCase.costs)*width)
	for _, row := range testCase.costs {
		require.Len(t, row, width)
		for _, cost := range row {
			costs = append(costs, Cost(cost))
		}
	}
	grid := CostGrid{Width: uint32(width), Height: uint32(len(testCase.costs)), Costs: costs}
	require.NoError(t, grid.Validate())
	return grid
}

func costRows(rows [][]uint8) [][]int {
	recorded := make([][]int, len(rows))
	for y, row := range rows {
		recorded[y] = make([]int, len(row))
		for x, cost := range row {
			recorded[y][x] = int(cost)
		}
	}
	return recorded
}

func goldenSources(sources []Source) []goldenSource {
	recorded := make([]goldenSource, len(sources))
	for index, source := range sources {
		recorded[index] = goldenSource{X: source.At.X, Y: source.At.Y, Bias: int32(source.Bias)}
	}
	return recorded
}

func distanceRows(field Field) [][]int32 {
	rows := make([][]int32, field.Height)
	for y := range rows {
		rows[y] = make([]int32, field.Width)
		for x := range rows[y] {
			rows[y][x] = int32(field.DistanceAt(daedalus.Cell{X: int32(x), Y: int32(y)}))
		}
	}
	return rows
}

func goldenSteps(field Field, positions []daedalus.Cell) []goldenStep {
	recorded := make([]goldenStep, len(positions))
	for index, at := range positions {
		result := field.Step(at)
		recorded[index] = goldenStep{
			X:        at.X,
			Y:        at.Y,
			Status:   stepStatusName(result.Status),
			Distance: int32(result.Distance),
		}
		if result.Status == StepStatusMoved {
			recorded[index].Direction = directionName(result.Direction)
		}
	}
	return recorded
}

func stepStatusName(status StepStatus) string {
	names := [...]string{"moved", "arrived", "unreachable", "outside", "blocked"}
	if int(status) >= len(names) {
		return fmt.Sprintf("status-%d", status)
	}
	return names[status]
}

func directionName(direction daedalus.Direction) string {
	names := [...]string{"north", "east", "south", "west"}
	if int(direction) >= len(names) {
		return fmt.Sprintf("direction-%d", direction)
	}
	return names[direction]
}

func writeGoldenField(t *testing.T, path string, field goldenField) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	contents, err := json.MarshalIndent(field, "", "  ")
	require.NoError(t, err)
	contents = append(contents, '\n')
	require.NoError(t, os.WriteFile(path, contents, 0o644))
}

func readGoldenField(t *testing.T, path string) goldenField {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err, "fixture missing; regenerate explicitly with go test ./utils/pathfinding -run '^TestFrozenGoldenFields$' -update")
	var field goldenField
	require.NoError(t, json.Unmarshal(contents, &field))
	return field
}

func goldenFieldFailures(expected, actual goldenField) []string {
	failures := make([]string, 0)
	fail := func(path string, want, got any) {
		if want != got {
			failures = append(failures, fmt.Sprintf("%s: expected %v; actual %v", path, want, got))
		}
	}
	fail("name", expected.Name, actual.Name)
	fail("width", expected.Width, actual.Width)
	fail("height", expected.Height, actual.Height)
	fail("flee", expected.Flee, actual.Flee)
	failures = append(failures, matrixFailures("costs", expected.Costs, actual.Costs)...)
	failures = append(failures, sourceFailures(expected.Sources, actual.Sources)...)
	failures = append(failures, matrixFailures("distances", expected.Distances, actual.Distances)...)
	failures = append(failures, stepRecordFailures(expected.Steps, actual.Steps)...)
	return failures
}

func matrixFailures[T comparable](name string, expected, actual [][]T) []string {
	failures := make([]string, 0)
	if len(expected) != len(actual) {
		failures = append(failures, fmt.Sprintf("%s.len: expected %d; actual %d", name, len(expected), len(actual)))
	}
	rows := len(expected)
	if len(actual) < rows {
		rows = len(actual)
	}
	for y := 0; y < rows; y++ {
		if len(expected[y]) != len(actual[y]) {
			failures = append(failures, fmt.Sprintf("%s[%d].len: expected %d; actual %d", name, y, len(expected[y]), len(actual[y])))
		}
		columns := len(expected[y])
		if len(actual[y]) < columns {
			columns = len(actual[y])
		}
		for x := 0; x < columns; x++ {
			if expected[y][x] != actual[y][x] {
				failures = append(failures, fmt.Sprintf("%s[%d][%d]: expected %v; actual %v", name, y, x, expected[y][x], actual[y][x]))
			}
		}
	}
	return failures
}

func sourceFailures(expected, actual []goldenSource) []string {
	failures := make([]string, 0)
	if len(expected) != len(actual) {
		failures = append(failures, fmt.Sprintf("sources.len: expected %d; actual %d", len(expected), len(actual)))
	}
	count := len(expected)
	if len(actual) < count {
		count = len(actual)
	}
	for index := 0; index < count; index++ {
		prefix := fmt.Sprintf("sources[%d]", index)
		if expected[index].X != actual[index].X {
			failures = append(failures, fmt.Sprintf("%s.x: expected %d; actual %d", prefix, expected[index].X, actual[index].X))
		}
		if expected[index].Y != actual[index].Y {
			failures = append(failures, fmt.Sprintf("%s.y: expected %d; actual %d", prefix, expected[index].Y, actual[index].Y))
		}
		if expected[index].Bias != actual[index].Bias {
			failures = append(failures, fmt.Sprintf("%s.bias: expected %d; actual %d", prefix, expected[index].Bias, actual[index].Bias))
		}
	}
	return failures
}

func stepRecordFailures(expected, actual []goldenStep) []string {
	failures := make([]string, 0)
	if len(expected) != len(actual) {
		failures = append(failures, fmt.Sprintf("steps.len: expected %d; actual %d", len(expected), len(actual)))
	}
	count := len(expected)
	if len(actual) < count {
		count = len(actual)
	}
	for index := 0; index < count; index++ {
		prefix := fmt.Sprintf("steps[%d]", index)
		want := expected[index]
		got := actual[index]
		if want.X != got.X {
			failures = append(failures, fmt.Sprintf("%s.x: expected %d; actual %d", prefix, want.X, got.X))
		}
		if want.Y != got.Y {
			failures = append(failures, fmt.Sprintf("%s.y: expected %d; actual %d", prefix, want.Y, got.Y))
		}
		if want.Status != got.Status {
			failures = append(failures, fmt.Sprintf("%s.status: expected %s; actual %s", prefix, want.Status, got.Status))
		}
		if want.Direction != got.Direction {
			failures = append(failures, fmt.Sprintf("%s.direction: expected %s; actual %s", prefix, want.Direction, got.Direction))
		}
		if want.Distance != got.Distance {
			failures = append(failures, fmt.Sprintf("%s.distance: expected %d; actual %d", prefix, want.Distance, got.Distance))
		}
	}
	return failures
}
