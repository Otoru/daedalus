package daedalus

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var updateGoldens = flag.Bool("update", false, "rewrite golden fixtures after explicit review")

type goldenCase struct {
	name      string
	fixture   string
	config    Config
	generator Generator
	check     func(*testing.T, Layout)
}

type goldenPlacer []RoomPlacement

func (placements goldenPlacer) Place(PlacementRequest) ([]RoomPlacement, error) {
	result := make([]RoomPlacement, len(placements))
	for index, placement := range placements {
		result[index] = placement
		result[index].Cells = append([]Cell(nil), placement.Cells...)
	}
	return result, nil
}

type goldenConnector []Connection

func (connections goldenConnector) Connect(ConnectionRequest) ([]Connection, error) {
	return append([]Connection(nil), connections...), nil
}

// TestFrozenGoldenLayouts checks that named Seeds match the frozen fixtures
// exactly. A mismatch reports the field path, the expected and actual scalars,
// and the changed Cells, not only a hash. Running the suite on one machine does
// not prove the same bytes on native amd64 and arm64; that takes the same suite
// and the same fixtures on both runners.
func TestFrozenGoldenLayouts(t *testing.T) {
	for _, testCase := range goldenCases() {
		t.Run(testCase.name, func(t *testing.T) {
			layout, err := testCase.generator.Generate(testCase.config)
			require.NoError(t, err)
			testCase.check(t, layout)

			// The Layout is already in field order, with Grid.Cells stored row-major
			// (Y, then X). Keeping the whole value in the fixture freezes Empty
			// Cells and that order.
			normalized := layout
			fixturePath := filepath.Join("testdata", "golden", testCase.fixture+".json")
			if *updateGoldens {
				writeGoldenFixture(t, fixturePath, normalized)
			}

			expected := readGoldenFixture(t, fixturePath)
			if diagnostic := goldenDifference(expected, normalized); diagnostic != "" {
				assert.Fail(t, "Layout diverged from frozen fixture", diagnostic)
			}
		})
	}
}

func goldenCases() []goldenCase {
	unitGeometry := unitRectangleGeometry(0)
	allDirections := []Direction{DirectionNorth, DirectionEast, DirectionSouth, DirectionWest}
	plantCatalog := &PlantCatalog{
		Rooms: []RoomPlant{
			{ID: "sala_a", Tags: []string{"tematica"}, Weight: 1, DoorDirections: allDirections},
			{ID: "sala_b", Tags: []string{"tematica"}, Weight: 1, DoorDirections: allDirections},
		},
		Corridors: []CorridorPlant{
			{ID: "corredor_a", Tags: []string{"pedra"}, Weight: 1},
			{ID: "corredor_b", Tags: []string{"pedra"}, Weight: 1},
		},
	}

	shapePlacements := goldenPlacer{
		goldenPlacement(RoomShapeRectangle, Cell{X: 0, Y: 0}, 3, 2),
		goldenPlacement(RoomShapeL, Cell{X: 8, Y: 0}, 4, 3),
		goldenPlacement(RoomShapeT, Cell{X: 19, Y: 0}, 5, 3),
		goldenPlacement(RoomShapeCross, Cell{X: 0, Y: 13}, 5, 5),
		goldenPlacement(RoomShapeCircle, Cell{X: 19, Y: 13}, 5, 5),
	}
	shapeGeometry := &RoomGeometry{
		MinWidth: 1, MaxWidth: 5, MinHeight: 1, MaxHeight: 5,
		MaxFootprintCells: 25, MinRoomGap: 1,
		Shapes: []RoomShapeWeight{
			{Shape: RoomShapeRectangle, Weight: 1},
			{Shape: RoomShapeL, Weight: 1},
			{Shape: RoomShapeT, Weight: 1},
			{Shape: RoomShapeCross, Weight: 1},
			{Shape: RoomShapeCircle, Weight: 1},
		},
	}

	return []goldenCase{
		{
			name:    "shapes_bounds_gap_catalog",
			fixture: "formas_limites_gap_catalogo",
			config: Config{
				Width: 24, Height: 18, Seed: 0xF011,
				MinDistance: 2, MaxAttempts: 30, MaxRooms: 5,
				RoomGeometry: shapeGeometry, PlantCatalog: plantCatalog,
				RoomRoleRequests: []RoomRoleRequest{
					{Role: RoomRoleStart, Count: 1},
					{Role: RoomRoleBoss, Count: 1},
					{Role: RoomRoleTreasure, Count: 1},
				},
			},
			generator: Generator{Placer: shapePlacements},
			check:     checkShapeGolden,
		},
		{
			name:    "prim_tie_break_and_weights",
			fixture: "desempate_prim_e_pesos",
			config: Config{
				Width: 11, Height: 11, Seed: 0xF012,
				MinDistance: 1, MaxAttempts: 30, MaxRooms: 3,
				RoomGeometry: &unitGeometry, PlantCatalog: plantCatalog,
			},
			generator: Generator{Placer: goldenPlacer{
				goldenPlacement(RoomShapeRectangle, Cell{X: 5, Y: 5}, 1, 1),
				goldenPlacement(RoomShapeRectangle, Cell{X: 3, Y: 5}, 1, 1),
				goldenPlacement(RoomShapeRectangle, Cell{X: 7, Y: 5}, 1, 1),
			}},
			check: checkTieGolden,
		},
		{
			name:    "winding_bfs",
			fixture: "bfs_serpenteante",
			config: Config{
				Width: 7, Height: 7, Seed: 0xF013,
				MinDistance: 1, MaxAttempts: 30, MaxRooms: 3,
				RoomGeometry: &RoomGeometry{
					MinWidth: 1, MaxWidth: 1, MinHeight: 1, MaxHeight: 5,
					MaxFootprintCells: 5,
					Shapes:            []RoomShapeWeight{{Shape: RoomShapeRectangle, Weight: 1}},
				},
			},
			generator: Generator{
				Placer: goldenPlacer{
					goldenPlacement(RoomShapeRectangle, Cell{X: 1, Y: 3}, 1, 1),
					goldenPlacement(RoomShapeRectangle, Cell{X: 5, Y: 3}, 1, 1),
					goldenPlacement(RoomShapeRectangle, Cell{X: 3, Y: 1}, 1, 5),
				},
				Connector: goldenConnector{
					{FromRoomID: 0, ToRoomID: 1},
					{FromRoomID: 0, ToRoomID: 2},
				},
			},
			check: checkBFSGolden,
		},
		{
			name:    "corridors_share_cell",
			fixture: "corridors_compartilham_cell",
			config: Config{
				Width: 7, Height: 7, Seed: 0xF014,
				MinDistance: 1, MaxAttempts: 30, MaxRooms: 4,
				RoomGeometry: &unitGeometry,
			},
			generator: Generator{
				Placer: goldenPlacer{
					goldenPlacement(RoomShapeRectangle, Cell{X: 1, Y: 3}, 1, 1),
					goldenPlacement(RoomShapeRectangle, Cell{X: 5, Y: 3}, 1, 1),
					goldenPlacement(RoomShapeRectangle, Cell{X: 3, Y: 1}, 1, 1),
					goldenPlacement(RoomShapeRectangle, Cell{X: 3, Y: 5}, 1, 1),
				},
				Connector: goldenConnector{
					{FromRoomID: 0, ToRoomID: 1},
					{FromRoomID: 2, ToRoomID: 3},
					{FromRoomID: 0, ToRoomID: 2},
				},
			},
			check: checkSharedCellGolden,
		},
		{
			name:    "poisson_rejection_and_density",
			fixture: "poisson_rejeicao_e_densidade",
			config: Config{
				Width: 20, Height: 20, Seed: 123,
				MinDistance: 2, MaxAttempts: 12, MaxRooms: 20,
				RoomGeometry: &RoomGeometry{
					MinWidth: 1, MaxWidth: 1, MinHeight: 1, MaxHeight: 1,
					MaxFootprintCells: 1, MinRoomGap: 1,
					Shapes: []RoomShapeWeight{{Shape: RoomShapeRectangle, Weight: 1}},
				},
				DensityRegions: []DensityRegion{{
					Min: Cell{X: 0, Y: 0}, Max: Cell{X: 10, Y: 20}, MinDistance: 4,
				}},
			},
			generator: Generator{},
			check:     checkPoissonGolden,
		},
	}
}

func goldenPlacement(shape RoomShape, origin Cell, width, height uint32) RoomPlacement {
	return RoomPlacement{
		Shape: shape, Origin: origin, Width: width, Height: height,
		Cells: RoomShapeOffsets(shape, width, height),
	}
}

func checkShapeGolden(t *testing.T, layout Layout) {
	t.Helper()
	require.Len(t, layout.Rooms, 5)
	for shape := RoomShapeRectangle; shape <= RoomShapeCircle; shape++ {
		assert.Equal(t, shape, layout.Rooms[int(shape)].Shape)
	}
	assert.Contains(t, layout.Rooms[0].Cells, Cell{X: 0, Y: 0})
	assert.Contains(t, layout.Rooms[2].Cells, Cell{X: 23, Y: 0})
	assert.Contains(t, layout.Rooms[3].Cells, Cell{X: 0, Y: 15})
	assert.Contains(t, layout.Rooms[4].Cells, Cell{X: 23, Y: 15})
	for _, room := range layout.Rooms {
		assert.NotEmpty(t, room.PlantID)
		for _, doorID := range room.DoorIDs {
			door := layout.Doors[int(doorID)]
			assert.Contains(t, room.Cells, door.At)
		}
	}
	for _, corridor := range layout.Corridors {
		assert.NotEmpty(t, corridor.PlantID)
	}
	for firstIndex, first := range layout.Rooms {
		for secondIndex := firstIndex + 1; secondIndex < len(layout.Rooms); secondIndex++ {
			assert.True(t, footprintsRespectGap(first.Cells, layout.Rooms[secondIndex].Cells, 1))
		}
	}
}

func checkTieGolden(t *testing.T, layout Layout) {
	t.Helper()
	require.Len(t, layout.Corridors, 2)
	assert.Equal(t, RoomID(0), layout.Corridors[0].FromRoomID)
	assert.Equal(t, RoomID(1), layout.Corridors[0].ToRoomID)
	assert.Equal(t, RoomID(0), layout.Corridors[1].FromRoomID)
	assert.Equal(t, RoomID(2), layout.Corridors[1].ToRoomID)
	assert.NotEmpty(t, layout.Rooms[0].PlantID)
}

func checkBFSGolden(t *testing.T, layout Layout) {
	t.Helper()
	require.NotEmpty(t, layout.Corridors)
	assert.GreaterOrEqual(t, corridorTurnCount(layout.Corridors[0].Cells), 2,
		"blocking both L-routes must trigger BFS")
	assert.Contains(t, layout.Corridors[0].Cells, Cell{X: 3, Y: 0})
}

func checkSharedCellGolden(t *testing.T, layout Layout) {
	t.Helper()
	foundShared := false
	for _, state := range layout.Grid.Cells {
		if len(state.CorridorIDs) > 1 {
			foundShared = true
		}
	}
	assert.True(t, foundShared, "at least one Cell must belong to multiple Corridors")
}

func checkPoissonGolden(t *testing.T, layout Layout) {
	t.Helper()
	require.Greater(t, len(layout.Rooms), 1)
	foundDenseRegion := false
	for _, room := range layout.Rooms {
		if room.At.X >= 0 && room.At.X < 10 {
			foundDenseRegion = true
		}
	}
	assert.True(t, foundDenseRegion, "DensityRegion is half-open [Min, Max): an anchor with X in [0, 10) materializes the region")
}

func corridorTurnCount(cells []Cell) int {
	turns := 0
	var previousDelta Cell
	for index := 1; index < len(cells); index++ {
		delta := Cell{X: cells[index].X - cells[index-1].X, Y: cells[index].Y - cells[index-1].Y}
		if index > 1 && delta != previousDelta {
			turns++
		}
		previousDelta = delta
	}
	return turns
}

func writeGoldenFixture(t *testing.T, path string, layout Layout) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	contents, err := json.MarshalIndent(layout, "", "  ")
	require.NoError(t, err)
	contents = append(contents, '\n')
	require.NoError(t, os.WriteFile(path, contents, 0o644))
}

func readGoldenFixture(t *testing.T, path string) Layout {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err, "fixture missing; regenerate explicitly with go test ./ -run TestFrozenGoldenLayouts -update")
	var layout Layout
	require.NoError(t, json.Unmarshal(contents, &layout))
	return layout
}

// TestGoldenDiagnosticPointsToFirstChangedCell checks that the diagnostic names
// the first divergent Cell path, the expected and actual scalars, and the
// changed Cells.
func TestGoldenDiagnosticPointsToFirstChangedCell(t *testing.T) {
	expected := Layout{Rooms: []Room{{ID: 0, Cells: []Cell{{X: 2, Y: 3}}}}}
	actual := Layout{Rooms: []Room{{ID: 0, Cells: []Cell{{X: 9, Y: 3}}}}}

	diagnostic := goldenDifference(expected, actual)

	assert.Contains(t, diagnostic, "first divergent path: Layout.Rooms[0].Cells[0].X")
	assert.Contains(t, diagnostic, "scalar expected=2 actual=9")
	assert.Contains(t, diagnostic, "changed Cells: Rooms[0].Cells: expected=[{2 3}] actual=[{9 3}]")
}

func TestGoldenDiagnosticPrefersEarlierFieldOverLaterSliceLength(t *testing.T) {
	expected := Layout{Rooms: []Room{{ID: 0}}, Corridors: []Corridor{{ID: 0}, {ID: 1}}}
	actual := Layout{Rooms: []Room{{ID: 4}}, Corridors: []Corridor{{ID: 0}}}

	diagnostic := goldenDifference(expected, actual)

	assert.Contains(t, diagnostic, "first divergent path: Layout.Rooms[0].ID")
	assert.Contains(t, diagnostic, "scalar expected=0 actual=4")
}

func goldenDifference(expected, actual Layout) string {
	path, expectedScalar, actualScalar, found := firstGoldenDifference(
		"Layout", reflect.ValueOf(expected), reflect.ValueOf(actual),
	)
	if !found {
		return ""
	}

	missingIDs, extraIDs := goldenIDDifferences(expected, actual)
	cellChanges := goldenCellDifferences(expected, actual)
	return strings.Join([]string{
		"first divergent path: " + path,
		fmt.Sprintf("scalar expected=%v actual=%v", expectedScalar, actualScalar),
		"missing IDs: " + missingIDs,
		"extra IDs: " + extraIDs,
		"changed Cells: " + cellChanges,
	}, "\n")
}

// firstGoldenDifference walks Layout in field declaration order, then slice
// index order, and stops at the first scalar, nil, or length mismatch.
func firstGoldenDifference(path string, expected, actual reflect.Value) (string, any, any, bool) {
	if expected.Type() != actual.Type() {
		return path + ".type", expected.Type(), actual.Type(), true
	}
	switch expected.Kind() {
	case reflect.Pointer:
		return firstPointerGoldenDifference(path, expected, actual)
	case reflect.Struct:
		return firstStructGoldenDifference(path, expected, actual)
	case reflect.Slice:
		return firstSliceGoldenDifference(path, expected, actual)
	default:
		if !reflect.DeepEqual(expected.Interface(), actual.Interface()) {
			return path, expected.Interface(), actual.Interface(), true
		}
		return "", nil, nil, false
	}
}

func firstPointerGoldenDifference(path string, expected, actual reflect.Value) (string, any, any, bool) {
	if expected.IsNil() != actual.IsNil() {
		return path, goldenValue(expected), goldenValue(actual), true
	}
	if expected.IsNil() {
		return "", nil, nil, false
	}
	return firstGoldenDifference(path, expected.Elem(), actual.Elem())
}

func firstStructGoldenDifference(path string, expected, actual reflect.Value) (string, any, any, bool) {
	for fieldIndex := 0; fieldIndex < expected.NumField(); fieldIndex++ {
		fieldPath := path + "." + expected.Type().Field(fieldIndex).Name
		if differingPath, want, got, found := firstGoldenDifference(
			fieldPath, expected.Field(fieldIndex), actual.Field(fieldIndex),
		); found {
			return differingPath, want, got, true
		}
	}
	return "", nil, nil, false
}

func firstSliceGoldenDifference(path string, expected, actual reflect.Value) (string, any, any, bool) {
	if expected.IsNil() != actual.IsNil() {
		return path + ".nil", expected.IsNil(), actual.IsNil(), true
	}
	sharedLength := expected.Len()
	if actual.Len() < sharedLength {
		sharedLength = actual.Len()
	}
	for index := 0; index < sharedLength; index++ {
		itemPath := fmt.Sprintf("%s[%d]", path, index)
		if differingPath, want, got, found := firstGoldenDifference(
			itemPath, expected.Index(index), actual.Index(index),
		); found {
			return differingPath, want, got, true
		}
	}
	if expected.Len() != actual.Len() {
		return path + ".len", expected.Len(), actual.Len(), true
	}
	return "", nil, nil, false
}

func goldenValue(value reflect.Value) any {
	if value.IsNil() {
		return nil
	}
	return value.Interface()
}

func goldenIDDifferences(expected, actual Layout) (string, string) {
	missing := make([]string, 0)
	extra := make([]string, 0)
	collectIDDifferences("RoomID", roomIDs(expected.Rooms), roomIDs(actual.Rooms), &missing, &extra)
	collectIDDifferences("CorridorID", corridorIDs(expected.Corridors), corridorIDs(actual.Corridors), &missing, &extra)
	collectIDDifferences("DoorID", doorIDs(expected.Doors), doorIDs(actual.Doors), &missing, &extra)
	if len(missing) == 0 {
		missing = append(missing, "none")
	}
	if len(extra) == 0 {
		extra = append(extra, "none")
	}
	return strings.Join(missing, ", "), strings.Join(extra, ", ")
}

func collectIDDifferences(kind string, expected, actual []uint32, missing, extra *[]string) {
	expectedIndex := 0
	actualIndex := 0
	for expectedIndex < len(expected) && actualIndex < len(actual) {
		switch {
		case expected[expectedIndex] < actual[actualIndex]:
			*missing = append(*missing, fmt.Sprintf("%s=%d", kind, expected[expectedIndex]))
			expectedIndex++
		case actual[actualIndex] < expected[expectedIndex]:
			*extra = append(*extra, fmt.Sprintf("%s=%d", kind, actual[actualIndex]))
			actualIndex++
		default:
			expectedIndex++
			actualIndex++
		}
	}
	for ; expectedIndex < len(expected); expectedIndex++ {
		*missing = append(*missing, fmt.Sprintf("%s=%d", kind, expected[expectedIndex]))
	}
	for ; actualIndex < len(actual); actualIndex++ {
		*extra = append(*extra, fmt.Sprintf("%s=%d", kind, actual[actualIndex]))
	}
}

func roomIDs(rooms []Room) []uint32 {
	ids := make([]uint32, len(rooms))
	for index, room := range rooms {
		ids[index] = uint32(room.ID)
	}
	return ids
}

func corridorIDs(corridors []Corridor) []uint32 {
	ids := make([]uint32, len(corridors))
	for index, corridor := range corridors {
		ids[index] = uint32(corridor.ID)
	}
	return ids
}

func doorIDs(doors []Door) []uint32 {
	ids := make([]uint32, len(doors))
	for index, door := range doors {
		ids[index] = uint32(door.ID)
	}
	return ids
}

func goldenCellDifferences(expected, actual Layout) string {
	for roomIndex := 0; roomIndex < len(expected.Rooms) && roomIndex < len(actual.Rooms); roomIndex++ {
		if !reflect.DeepEqual(expected.Rooms[roomIndex].Cells, actual.Rooms[roomIndex].Cells) {
			return fmt.Sprintf(
				"Rooms[%d].Cells: expected=%v actual=%v",
				roomIndex, expected.Rooms[roomIndex].Cells, actual.Rooms[roomIndex].Cells,
			)
		}
	}
	for corridorIndex := 0; corridorIndex < len(expected.Corridors) && corridorIndex < len(actual.Corridors); corridorIndex++ {
		if !reflect.DeepEqual(expected.Corridors[corridorIndex].Cells, actual.Corridors[corridorIndex].Cells) {
			return fmt.Sprintf(
				"Corridors[%d].Cells: expected=%v actual=%v",
				corridorIndex, expected.Corridors[corridorIndex].Cells, actual.Corridors[corridorIndex].Cells,
			)
		}
	}
	sharedGridLength := len(expected.Grid.Cells)
	if len(actual.Grid.Cells) < sharedGridLength {
		sharedGridLength = len(actual.Grid.Cells)
	}
	for cellIndex := 0; cellIndex < sharedGridLength; cellIndex++ {
		if !reflect.DeepEqual(expected.Grid.Cells[cellIndex], actual.Grid.Cells[cellIndex]) {
			return fmt.Sprintf(
				"Grid.Cells[%d]: expected=%s actual=%s",
				cellIndex,
				formatGoldenCellState(expected.Grid.Cells[cellIndex]),
				formatGoldenCellState(actual.Grid.Cells[cellIndex]),
			)
		}
	}
	if len(expected.Grid.Cells) != len(actual.Grid.Cells) {
		return fmt.Sprintf(
			"Grid.Cells.len: expected=%d actual=%d",
			len(expected.Grid.Cells), len(actual.Grid.Cells),
		)
	}
	return "none"
}

func formatGoldenCellState(state CellState) string {
	roomID := "nil"
	if state.RoomID != nil {
		roomID = fmt.Sprintf("%d", *state.RoomID)
	}
	return fmt.Sprintf(
		"{At:%v Kind:%d RoomID:%s CorridorIDs:%v}",
		state.At, state.Kind, roomID, state.CorridorIDs,
	)
}
