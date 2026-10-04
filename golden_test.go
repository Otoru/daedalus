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
		MaxFootprintCells: 25, MinRoomGap: 1,
		Shapes: []RoomShapeWeight{
			shapeSpan(RoomShapeRectangle, 1, 1, 5, 1, 5),
			shapeSpan(RoomShapeL, 1, 1, 5, 1, 5),
			shapeSpan(RoomShapeT, 1, 1, 5, 1, 5),
			shapeSpan(RoomShapeCross, 1, 1, 5, 1, 5),
			shapeSpan(RoomShapeCircle, 1, 1, 5, 1, 5),
		},
	}

	return []goldenCase{
		{
			name:    "shapes_bounds_gap_catalog",
			fixture: "shapes_bounds_gap_catalog",
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
			fixture: "prim_tie_break_and_weights",
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
			fixture: "winding_bfs",
			config: Config{
				Width: 7, Height: 7, Seed: 0xF013,
				MinDistance: 1, MaxAttempts: 30, MaxRooms: 3,
				RoomGeometry: &RoomGeometry{
					MaxFootprintCells: 5,
					Shapes:            []RoomShapeWeight{shapeSpan(RoomShapeRectangle, 1, 1, 1, 1, 5)},
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
			name:    "corridors_keep_apart",
			fixture: "corridors_keep_apart",
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
			check: checkCorridorsStayApart,
		},
		{
			name:    "poisson_rejection_and_density",
			fixture: "poisson_rejection_and_density",
			config: Config{
				Width: 20, Height: 20, Seed: 123,
				// Four 1×1 Rooms are enough to show the half-open DensityRegion.
				// A Room of degree three can now be left: Corridors may touch
				// where both Cells are orthogonally adjacent to its footprint.
				MinDistance: 2, MaxAttempts: 12, MaxRooms: 4,
				RoomGeometry: &RoomGeometry{
					MaxFootprintCells: 1, MinRoomGap: 1,
					Shapes: []RoomShapeWeight{shapeSpan(RoomShapeRectangle, 1, 1, 1, 1, 1)},
				},
				DensityRegions: []DensityRegion{{
					Min: Cell{X: 0, Y: 0}, Max: Cell{X: 10, Y: 20}, MinDistance: 4,
				}},
			},
			generator: Generator{},
			check:     checkPoissonGolden,
		},
		{
			name:    "even_width_positive_side",
			fixture: "even_width_positive_side",
			config: Config{
				Width: 8, Height: 8, Seed: 0xF015,
				MinDistance: 1, MaxAttempts: 30, MaxRooms: 3,
				RoomGeometry: &RoomGeometry{
					MaxFootprintCells: 8, MinRoomGap: 1,
					Shapes: []RoomShapeWeight{shapeSpan(RoomShapeRectangle, 1, 1, 2, 1, 4)},
				},
				// Width 1 is mandatory in every catalog; the heavier weight on 2 keeps
				// both draws on the even width this fixture freezes.
				CorridorGeometry: &CorridorGeometry{Widths: []CorridorWidthWeight{
					{Width: 1, Weight: 1},
					{Width: 2, Weight: 4},
				}},
			},
			generator: Generator{
				Placer: goldenPlacer{
					goldenPlacement(RoomShapeRectangle, Cell{X: 0, Y: 0}, 1, 2),
					goldenPlacement(RoomShapeRectangle, Cell{X: 5, Y: 0}, 2, 4),
					goldenPlacement(RoomShapeRectangle, Cell{X: 5, Y: 6}, 2, 1),
				},
				Connector: goldenConnector{
					{FromRoomID: 0, ToRoomID: 1},
					{FromRoomID: 1, ToRoomID: 2},
				},
			},
			check: checkEvenWidthGolden,
		},
		{
			name:    "bend_width_square",
			fixture: "bend_width_square",
			config: Config{
				Width: 7, Height: 5, Seed: 0xF016,
				MinDistance: 1, MaxAttempts: 30, MaxRooms: 2,
				RoomGeometry: &RoomGeometry{
					MaxFootprintCells: 2, MinRoomGap: 1,
					Shapes: []RoomShapeWeight{shapeSpan(RoomShapeRectangle, 1, 1, 2, 1, 2)},
				},
				CorridorGeometry: &CorridorGeometry{Widths: []CorridorWidthWeight{
					{Width: 1, Weight: 1},
					{Width: 2, Weight: 1},
				}},
			},
			generator: Generator{
				Placer: goldenPlacer{
					goldenPlacement(RoomShapeRectangle, Cell{X: 0, Y: 3}, 1, 2),
					goldenPlacement(RoomShapeRectangle, Cell{X: 5, Y: 0}, 2, 1),
				},
				Connector: goldenConnector{{FromRoomID: 0, ToRoomID: 1}},
			},
			check: checkBendWidthGolden,
		},
		{
			name:    "degraded_width_tight_gap",
			fixture: "degraded_width_tight_gap",
			config: Config{
				Width: 12, Height: 12, Seed: 0xF017,
				MinDistance: 1, MaxAttempts: 30, MaxRooms: 6,
				RoomGeometry: &RoomGeometry{
					MaxFootprintCells: 9, MinRoomGap: 1,
					Shapes: []RoomShapeWeight{shapeSpan(RoomShapeRectangle, 1, 1, 5, 1, 3)},
				},
				// Width 1 is declared, so a 3 that cannot pass the one-Cell gap falls
				// to 1. Width 2 is absent and must not appear. The other edges leave
				// by doors the pinch halo does not cover.
				CorridorGeometry: &CorridorGeometry{Widths: []CorridorWidthWeight{
					{Width: 3, Weight: 1},
					{Width: 1, Weight: 1},
				}},
			},
			generator: Generator{
				Placer: goldenPlacer{
					goldenPlacement(RoomShapeRectangle, Cell{X: 0, Y: 0}, 3, 3),
					goldenPlacement(RoomShapeRectangle, Cell{X: 8, Y: 0}, 3, 3),
					goldenPlacement(RoomShapeRectangle, Cell{X: 3, Y: 6}, 5, 1),
					goldenPlacement(RoomShapeRectangle, Cell{X: 3, Y: 8}, 5, 1),
					goldenPlacement(RoomShapeRectangle, Cell{X: 0, Y: 6}, 1, 3),
					goldenPlacement(RoomShapeRectangle, Cell{X: 10, Y: 6}, 1, 3),
				},
				Connector: goldenConnector{
					{FromRoomID: 0, ToRoomID: 1},
					{FromRoomID: 4, ToRoomID: 5},
					{FromRoomID: 0, ToRoomID: 4},
					{FromRoomID: 2, ToRoomID: 1},
					{FromRoomID: 3, ToRoomID: 5},
				},
			},
			check: checkDegradedWidthGolden,
		},
		{
			name:    "mixed_corridor_widths",
			fixture: "mixed_corridor_widths",
			config: Config{
				Width: 13, Height: 3, Seed: 1,
				MinDistance: 1, MaxAttempts: 30, MaxRooms: 4,
				RoomGeometry: &RoomGeometry{
					MaxFootprintCells: 3, MinRoomGap: 1,
					Shapes: []RoomShapeWeight{shapeSpan(RoomShapeRectangle, 1, 1, 1, 1, 3)},
				},
				// Request order is not width order. Weights 1, 1, and 5 sort to
				// widths 1, 2, 3 before the ticket is drawn.
				CorridorGeometry: &CorridorGeometry{Widths: []CorridorWidthWeight{
					{Width: 3, Weight: 1},
					{Width: 1, Weight: 1},
					{Width: 2, Weight: 5},
				}},
			},
			generator: Generator{
				Placer: goldenPlacer{
					goldenPlacement(RoomShapeRectangle, Cell{X: 0, Y: 0}, 1, 3),
					goldenPlacement(RoomShapeRectangle, Cell{X: 4, Y: 0}, 1, 3),
					goldenPlacement(RoomShapeRectangle, Cell{X: 8, Y: 0}, 1, 3),
					goldenPlacement(RoomShapeRectangle, Cell{X: 12, Y: 0}, 1, 3),
				},
				Connector: goldenConnector{
					{FromRoomID: 0, ToRoomID: 1},
					{FromRoomID: 1, ToRoomID: 2},
					{FromRoomID: 2, ToRoomID: 3},
				},
			},
			check: checkMixedWidthGolden,
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

func checkCorridorsStayApart(t *testing.T, layout Layout) {
	t.Helper()
	require.GreaterOrEqual(t, len(layout.Corridors), 2)
	owners := make(map[Cell]CorridorID)
	for _, corridor := range layout.Corridors {
		for _, cell := range corridor.Cells {
			previous, shared := owners[cell]
			assert.False(t, shared, "Cell %v belongs to Corridor %d and Corridor %d", cell, previous, corridor.ID)
			owners[cell] = corridor.ID
		}
	}
	for _, corridor := range layout.Corridors {
		for _, cell := range corridor.Cells {
			checkCorridorNeighbors(t, corridor.ID, cell, owners)
		}
	}
	for _, state := range layout.Grid.Cells {
		assert.LessOrEqual(t, len(state.CorridorIDs), 1, "Cell %v", state.At)
	}
}

func checkCorridorNeighbors(t *testing.T, corridorID CorridorID, cell Cell, owners map[Cell]CorridorID) {
	t.Helper()
	for dy := int32(-1); dy <= 1; dy++ {
		for dx := int32(-1); dx <= 1; dx++ {
			neighbor := Cell{X: cell.X + dx, Y: cell.Y + dy}
			other, exists := owners[neighbor]
			if !exists || other == corridorID {
				continue
			}
			assert.Fail(t, "corridors touch",
				"Corridor %d at %v is within Chebyshev 1 of Corridor %d", corridorID, cell, other)
		}
	}
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

func checkEvenWidthGolden(t *testing.T, layout Layout) {
	t.Helper()
	require.Len(t, layout.Corridors, 2)
	horizontal := layout.Corridors[0]
	vertical := layout.Corridors[1]
	assert.Equal(t, uint32(2), layout.Doors[horizontal.FromDoorID].Span)
	assert.Equal(t, uint32(2), layout.Doors[horizontal.ToDoorID].Span)
	assert.Equal(t, uint32(2), layout.Doors[vertical.FromDoorID].Span)
	assert.Equal(t, uint32(2), layout.Doors[vertical.ToDoorID].Span)
	for _, cell := range horizontal.Centerline {
		assert.Contains(t, horizontal.Cells, Cell{X: cell.X, Y: cell.Y + 1},
			"even horizontal band keeps the extra Cell on +Y")
		assert.NotContains(t, horizontal.Cells, Cell{X: cell.X, Y: cell.Y - 1},
			"even horizontal band does not grow on -Y")
	}
	for _, cell := range vertical.Centerline {
		assert.Contains(t, vertical.Cells, Cell{X: cell.X + 1, Y: cell.Y},
			"even vertical band keeps the extra Cell on +X")
		assert.NotContains(t, vertical.Cells, Cell{X: cell.X - 1, Y: cell.Y},
			"even vertical band does not grow on -X")
	}
}

func checkBendWidthGolden(t *testing.T, layout Layout) {
	t.Helper()
	require.Len(t, layout.Corridors, 1)
	corridor := layout.Corridors[0]
	assert.Equal(t, uint32(2), layout.Doors[corridor.FromDoorID].Span)
	assert.Equal(t, uint32(2), layout.Doors[corridor.ToDoorID].Span)
	foundBend := false
	centerline := corridor.Centerline
	for index := 1; index < len(centerline)-1; index++ {
		incoming := Cell{
			X: centerline[index].X - centerline[index-1].X,
			Y: centerline[index].Y - centerline[index-1].Y,
		}
		outgoing := Cell{
			X: centerline[index+1].X - centerline[index].X,
			Y: centerline[index+1].Y - centerline[index].Y,
		}
		if incoming == outgoing {
			continue
		}
		foundBend = true
		corner := Cell{X: centerline[index].X + 1, Y: centerline[index].Y + 1}
		assert.Contains(t, corridor.Cells, corner,
			"bend fills the +X/+Y corner so the W×W block has no diagonal pinch")
		assert.Contains(t, corridor.Cells, Cell{X: corner.X, Y: centerline[index].Y})
		assert.Contains(t, corridor.Cells, Cell{X: centerline[index].X, Y: corner.Y})
	}
	assert.True(t, foundBend, "the fixture must bend or it does not guard the corner")
}

func checkDegradedWidthGolden(t *testing.T, layout Layout) {
	t.Helper()
	require.Len(t, layout.Corridors, 5)
	open := layout.Corridors[0]
	pinched := layout.Corridors[1]
	assert.Equal(t, RoomID(0), open.FromRoomID)
	assert.Equal(t, RoomID(1), open.ToRoomID)
	assert.Equal(t, uint32(3), layout.Doors[open.FromDoorID].Span)
	assert.Equal(t, uint32(3), layout.Doors[open.ToDoorID].Span)
	assert.Equal(t, RoomID(4), pinched.FromRoomID)
	assert.Equal(t, RoomID(5), pinched.ToRoomID)
	assert.Equal(t, uint32(3), layout.Rooms[4].Height)
	assert.Equal(t, uint32(3), layout.Rooms[5].Height)
	assert.Equal(t, uint32(1), layout.Doors[pinched.FromDoorID].Span,
		"width 3 does not fit the one-Cell gap, so the next declared width is 1")
	assert.Equal(t, uint32(1), layout.Doors[pinched.ToDoorID].Span)
	require.NotEmpty(t, pinched.Cells)
	for _, cell := range pinched.Cells {
		assert.Equal(t, int32(7), cell.Y, "the degraded corridor stays in the one-Cell gap")
	}
}

func checkMixedWidthGolden(t *testing.T, layout Layout) {
	t.Helper()
	require.Len(t, layout.Corridors, 3)
	// Seed 1 draws widths 1, then 3, then 2. That sequence is neither insertion
	// order nor "always the heaviest", so it pins both the sort and the tickets.
	drawn := []uint32{1, 3, 2}
	for index, corridor := range layout.Corridors {
		assert.Equal(t, drawn[index], layout.Doors[corridor.FromDoorID].Span, "corridor %d", index)
		assert.Equal(t, drawn[index], layout.Doors[corridor.ToDoorID].Span, "corridor %d", index)
	}
	symmetric := layout.Corridors[1]
	for _, cell := range symmetric.Centerline {
		assert.Contains(t, symmetric.Cells, Cell{X: cell.X, Y: cell.Y - 1})
		assert.Contains(t, symmetric.Cells, Cell{X: cell.X, Y: cell.Y + 1})
	}
	even := layout.Corridors[2]
	for _, cell := range even.Centerline {
		assert.Contains(t, even.Cells, Cell{X: cell.X, Y: cell.Y + 1},
			"the width drawn as 2 keeps the extra Cell on +Y")
		assert.NotContains(t, even.Cells, Cell{X: cell.X, Y: cell.Y - 1})
	}
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
