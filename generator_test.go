package daedalus

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratorMaterializesMinimalLayout(t *testing.T) {
	layout, err := (Generator{}).Generate(Config{Width: 1, Height: 1, Seed: 7})

	require.NoError(t, err)
	require.Len(t, layout.Rooms, 1)
	require.Len(t, layout.Grid.Cells, 1)
	assert.Equal(t, Seed(7), layout.Seed)
	assert.Equal(t, uint32(1), layout.Grid.Width)
	assert.Equal(t, uint32(1), layout.Grid.Height)
	assert.Equal(t, 1.0, layout.Grid.CellSize)
	assert.Equal(t, RoomID(0), layout.Rooms[0].ID)
	assert.Equal(t, Cell{X: 0, Y: 0}, layout.Rooms[0].At)
	assert.Equal(t, []Cell{{X: 0, Y: 0}}, layout.Rooms[0].Cells)
	assert.Equal(t, CellState{
		At: Cell{X: 0, Y: 0}, Kind: CellKindRoom, RoomID: roomIDPointerForTest(0),
	}, layout.Grid.Cells[0])
	assert.Empty(t, layout.Corridors)
	assert.Empty(t, layout.Doors)
}

func TestGenerateContextPreservesCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	layout, err := (Generator{}).GenerateContext(ctx, Config{Width: 1, Height: 1})

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, Layout{}, layout)
}

func TestGeneratorSelectsPlantsCompatibleWithDoors(t *testing.T) {
	generator := Generator{
		Placer: fixedPlacer{placements: []RoomPlacement{
			rectanglePlacementForTest(Cell{X: 1, Y: 1}, 1, 1),
			rectanglePlacementForTest(Cell{X: 5, Y: 1}, 1, 1),
		}},
		Connector: fixedConnector{connections: []Connection{{FromRoomID: 0, ToRoomID: 1}}},
	}
	config := fixedGeometryConfigForTest(7, 3, 2)
	config.PlantCatalog = &PlantCatalog{
		Rooms: []RoomPlant{
			{ID: "west", Tags: []string{"cold"}, Weight: 1, DoorDirections: []Direction{DirectionWest}},
			{ID: "east", Tags: []string{"hot"}, Weight: 1, DoorDirections: []Direction{DirectionEast}},
		},
		Corridors: []CorridorPlant{{ID: "stone", Tags: []string{"damp"}, Weight: 1}},
	}

	layout, err := generator.Generate(config)

	require.NoError(t, err)
	require.Len(t, layout.Rooms, 2)
	require.Len(t, layout.Corridors, 1)
	assert.Equal(t, PlantID("east"), layout.Rooms[0].PlantID)
	assert.Equal(t, []string{"hot"}, layout.Rooms[0].Tags)
	assert.Equal(t, PlantID("west"), layout.Rooms[1].PlantID)
	assert.Equal(t, []string{"cold"}, layout.Rooms[1].Tags)
	assert.Equal(t, PlantID("stone"), layout.Corridors[0].PlantID)
	assert.Equal(t, []string{"damp"}, layout.Corridors[0].Tags)
}

func TestIncompatibleCatalogReturnsNoPartialLayout(t *testing.T) {
	generator := twoRoomGeneratorForTest()
	config := fixedGeometryConfigForTest(7, 3, 2)
	config.PlantCatalog = &PlantCatalog{
		Rooms: []RoomPlant{
			{ID: "north-only", Weight: 1, DoorDirections: []Direction{DirectionNorth}},
		},
		Corridors: []CorridorPlant{{ID: "stone", Weight: 1}},
	}

	layout, err := generator.Generate(config)

	assert.ErrorIs(t, err, ErrNoCompatiblePlant)
	assert.Equal(t, Layout{}, layout)
}

func TestGeneratorMaterializesGridAndCanonicalIDs(t *testing.T) {
	layout, err := twoRoomGeneratorForTest().Generate(fixedGeometryConfigForTest(7, 3, 2))

	require.NoError(t, err)
	require.Len(t, layout.Grid.Cells, 21)
	require.Len(t, layout.Rooms, 2)
	require.Len(t, layout.Corridors, 1)
	require.Len(t, layout.Doors, 2)
	for index, state := range layout.Grid.Cells {
		y := index / int(layout.Grid.Width)
		x := index - y*int(layout.Grid.Width)
		assert.Equal(t, Cell{X: int32(x), Y: int32(y)}, state.At)
	}
	for roomIndex, room := range layout.Rooms {
		assert.Equal(t, RoomID(roomIndex), room.ID)
		for _, cell := range room.Cells {
			state := layout.Grid.Cells[int(cell.Y)*int(layout.Grid.Width)+int(cell.X)]
			require.NotNil(t, state.RoomID)
			assert.Equal(t, CellKindRoom, state.Kind)
			assert.Equal(t, room.ID, *state.RoomID)
			assert.Empty(t, state.CorridorIDs)
		}
	}
	for corridorIndex, corridor := range layout.Corridors {
		assert.Equal(t, CorridorID(corridorIndex), corridor.ID)
		for _, cell := range corridor.Cells {
			state := layout.Grid.Cells[int(cell.Y)*int(layout.Grid.Width)+int(cell.X)]
			assert.Equal(t, CellKindCorridor, state.Kind)
			assert.Nil(t, state.RoomID)
			assert.Contains(t, state.CorridorIDs, corridor.ID)
		}
	}
}

func TestRoomDoorIDsAreSortedByCellAndDirection(t *testing.T) {
	generator := Generator{
		Placer: fixedPlacer{placements: []RoomPlacement{
			rectanglePlacementForTest(Cell{X: 3, Y: 3}, 1, 1),
			rectanglePlacementForTest(Cell{X: 3, Y: 6}, 1, 1),
			rectanglePlacementForTest(Cell{X: 6, Y: 3}, 1, 1),
		}},
		Connector: fixedConnector{connections: []Connection{
			{FromRoomID: 0, ToRoomID: 1},
			{FromRoomID: 0, ToRoomID: 2},
		}},
	}

	layout, err := generator.Generate(fixedGeometryConfigForTest(8, 8, 3))

	require.NoError(t, err)
	require.Len(t, layout.Rooms[0].DoorIDs, 2)
	first := layout.Doors[layout.Rooms[0].DoorIDs[0]]
	second := layout.Doors[layout.Rooms[0].DoorIDs[1]]
	// The south edge is routed first and its halo closes the east opening,
	// which would sit at Chebyshev distance 1. The second edge leaves north.
	// Both Doors are the Room's only Cell, so Direction order is North then South.
	assert.Equal(t, DirectionNorth, first.Direction)
	assert.Equal(t, DirectionSouth, second.Direction)
	assert.Equal(t, first.At, second.At)
}

func TestGeneratorRejectsInvalidPluginPlacements(t *testing.T) {
	valid := rectanglePlacementForTest(Cell{X: 1, Y: 1}, 2, 2)
	tests := []struct {
		name      string
		placement RoomPlacement
	}{
		{name: "duplicate mask", placement: RoomPlacement{
			Shape: RoomShapeRectangle, Origin: Cell{X: 1, Y: 1}, Width: 2, Height: 2,
			Cells: []Cell{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}},
		}},
		{name: "disconnected mask", placement: RoomPlacement{
			Shape: RoomShapeRectangle, Origin: Cell{X: 1, Y: 1}, Width: 2, Height: 2,
			Cells: []Cell{{X: 0, Y: 0}, {X: 1, Y: 1}},
		}},
		{name: "outside the Grid", placement: rectanglePlacementForTest(Cell{X: 7, Y: 7}, 2, 2)},
		{name: "incompatible with Shape", placement: RoomPlacement{
			Shape: RoomShapeL, Origin: valid.Origin, Width: valid.Width, Height: valid.Height,
			Cells: valid.Cells,
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			generator := Generator{
				Placer:    fixedPlacer{placements: []RoomPlacement{test.placement}},
				Connector: fixedConnector{},
			}
			config := Config{
				Width: 8, Height: 8, MinDistance: 1, MaxRooms: 1,
				RoomGeometry: &RoomGeometry{
					MaxFootprintCells: 4,
					Shapes: []RoomShapeWeight{
						shapeSpan(RoomShapeRectangle, 1, 1, 2, 1, 2),
						shapeSpan(RoomShapeL, 1, 1, 2, 1, 2),
					},
				},
			}

			layout, err := generator.Generate(config)

			assert.Error(t, err)
			assert.Equal(t, Layout{}, layout)
		})
	}
}

func TestGeneratorRejectsInvalidPluginConnections(t *testing.T) {
	placements := []RoomPlacement{
		rectanglePlacementForTest(Cell{X: 1, Y: 1}, 1, 1),
		rectanglePlacementForTest(Cell{X: 4, Y: 1}, 1, 1),
		rectanglePlacementForTest(Cell{X: 7, Y: 1}, 1, 1),
	}
	tests := []struct {
		name        string
		connections []Connection
	}{
		{name: "self edge", connections: []Connection{{FromRoomID: 0, ToRoomID: 0}}},
		{name: "duplicate edge", connections: []Connection{
			{FromRoomID: 0, ToRoomID: 1}, {FromRoomID: 1, ToRoomID: 0}, {FromRoomID: 1, ToRoomID: 2},
		}},
		{name: "unknown RoomID", connections: []Connection{
			{FromRoomID: 0, ToRoomID: 1}, {FromRoomID: 1, ToRoomID: 9},
		}},
		{name: "disconnected graph", connections: []Connection{{FromRoomID: 0, ToRoomID: 1}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			generator := Generator{
				Placer:    fixedPlacer{placements: placements},
				Connector: fixedConnector{connections: test.connections},
			}

			layout, err := generator.Generate(fixedGeometryConfigForTest(9, 3, 3))

			assert.Error(t, err)
			assert.Equal(t, Layout{}, layout)
		})
	}
}

func TestGeneratorClassifiesInvalidPluginOutput(t *testing.T) {
	t.Run("lying Placer", func(t *testing.T) {
		valid := rectanglePlacementForTest(Cell{X: 1, Y: 1}, 2, 2)
		generator := Generator{
			Placer: fixedPlacer{placements: []RoomPlacement{{
				Shape:  RoomShapeL,
				Origin: valid.Origin,
				Width:  valid.Width,
				Height: valid.Height,
				Cells:  valid.Cells,
			}}},
			Connector: fixedConnector{},
		}
		config := Config{
			Width: 8, Height: 8, MinDistance: 1, MaxRooms: 1,
			RoomGeometry: &RoomGeometry{
				MaxFootprintCells: 4,
				Shapes: []RoomShapeWeight{
					shapeSpan(RoomShapeRectangle, 1, 1, 2, 1, 2),
					shapeSpan(RoomShapeL, 1, 1, 2, 1, 2),
				},
			},
		}

		layout, err := generator.Generate(config)

		assert.ErrorIs(t, err, ErrInvalidPlugin)
		assert.ErrorContains(t, err, "invalid placement 0")
		assert.Equal(t, Layout{}, layout)
	})

	t.Run("lying Connector", func(t *testing.T) {
		generator := Generator{
			Placer: fixedPlacer{placements: []RoomPlacement{
				rectanglePlacementForTest(Cell{X: 1, Y: 1}, 1, 1),
				rectanglePlacementForTest(Cell{X: 4, Y: 1}, 1, 1),
				rectanglePlacementForTest(Cell{X: 7, Y: 1}, 1, 1),
			}},
			// Three Rooms need exactly two edges when ExtraEdgeCount is zero.
			// A short count would fail before the self-edge rule.
			Connector: fixedConnector{connections: []Connection{
				{FromRoomID: 0, ToRoomID: 0},
				{FromRoomID: 1, ToRoomID: 2},
			}},
		}

		layout, err := generator.Generate(fixedGeometryConfigForTest(9, 3, 3))

		assert.ErrorIs(t, err, ErrInvalidPlugin)
		assert.ErrorContains(t, err, "edge 0 is a self-edge")
		assert.Equal(t, Layout{}, layout)
	})

	t.Run("empty Placer", func(t *testing.T) {
		generator := Generator{Placer: fixedPlacer{}, Connector: fixedConnector{}}

		layout, err := generator.Generate(fixedGeometryConfigForTest(8, 1, 1))

		assert.ErrorIs(t, err, ErrInvalidPlugin)
		assert.ErrorContains(t, err, "generator received no valid Room")
		assert.Equal(t, Layout{}, layout)
	})

	t.Run("Connector edge count", func(t *testing.T) {
		generator := Generator{
			Placer: fixedPlacer{placements: []RoomPlacement{
				rectanglePlacementForTest(Cell{X: 1, Y: 1}, 1, 1),
				rectanglePlacementForTest(Cell{X: 4, Y: 1}, 1, 1),
				rectanglePlacementForTest(Cell{X: 7, Y: 1}, 1, 1),
			}},
			Connector: fixedConnector{connections: []Connection{{FromRoomID: 0, ToRoomID: 1}}},
		}

		layout, err := generator.Generate(fixedGeometryConfigForTest(9, 3, 3))

		assert.ErrorIs(t, err, ErrInvalidPlugin)
		assert.ErrorContains(t, err, "Connector must return between")
		assert.Equal(t, Layout{}, layout)
	})
}

func TestGeneratorRejectsConnectorThatReturnsNoBackboneTree(t *testing.T) {
	generator := Generator{
		Placer: fixedPlacer{placements: []RoomPlacement{
			rectanglePlacementForTest(Cell{X: 1, Y: 1}, 1, 1),
			rectanglePlacementForTest(Cell{X: 4, Y: 1}, 1, 1),
			rectanglePlacementForTest(Cell{X: 7, Y: 1}, 1, 1),
		}},
		Connector: fixedConnector{connections: []Connection{
			{FromRoomID: 0, ToRoomID: 1},
			{FromRoomID: 1, ToRoomID: 2},
			{FromRoomID: 0, ToRoomID: 2},
		}},
	}

	layout, err := generator.Generate(fixedGeometryConfigForTest(9, 3, 3))

	assert.Error(t, err)
	assert.Equal(t, Layout{}, layout)
}

func TestGeneratorAcceptsConnectorCycleWithinShortcutBudget(t *testing.T) {
	generator := Generator{
		Placer: fixedPlacer{placements: []RoomPlacement{
			rectanglePlacementForTest(Cell{X: 1, Y: 3}, 1, 1),
			rectanglePlacementForTest(Cell{X: 4, Y: 3}, 1, 1),
			rectanglePlacementForTest(Cell{X: 7, Y: 3}, 1, 1),
		}},
		Connector: fixedConnector{connections: []Connection{
			{FromRoomID: 0, ToRoomID: 1},
			{FromRoomID: 1, ToRoomID: 2},
			{FromRoomID: 0, ToRoomID: 2},
		}},
	}
	// Height 3 leaves no row outside a Corridor's halo, so the shortcut has
	// nowhere to travel. The taller Grid keeps a free detour.
	config := fixedGeometryConfigForTest(9, 7, 3)
	config.ExtraEdgeCount = 1

	layout, err := generator.Generate(config)

	require.NoError(t, err)
	assert.Len(t, layout.Corridors, 3)
}

func TestLayoutsFromDistinctRequestsDoNotShareSlices(t *testing.T) {
	generator := twoRoomGeneratorForTest()
	config := fixedGeometryConfigForTest(7, 3, 2)
	config.PlantCatalog = &PlantCatalog{
		Rooms: []RoomPlant{{
			ID: "room", Tags: []string{"original"}, Weight: 1,
			DoorDirections: []Direction{DirectionNorth, DirectionEast, DirectionSouth, DirectionWest},
		}},
		Corridors: []CorridorPlant{{ID: "corridor", Tags: []string{"stone"}, Weight: 1}},
	}
	first, firstErr := generator.Generate(config)
	second, secondErr := generator.Generate(config)
	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	require.Equal(t, first, second)

	first.Rooms[0].Cells[0] = Cell{X: 99, Y: 99}
	first.Rooms[0].Tags[0] = "changed"
	first.Rooms[0].DoorIDs[0] = DoorID(99)
	first.Corridors[0].Cells[0] = Cell{X: 98, Y: 98}
	first.Corridors[0].Tags[0] = "changed"
	first.Doors[0].CorridorIDs[0] = CorridorID(99)
	first.Grid.Cells[0].CorridorIDs = append(first.Grid.Cells[0].CorridorIDs, CorridorID(99))

	assert.Equal(t, Cell{X: 1, Y: 1}, second.Rooms[0].Cells[0])
	assert.Equal(t, []string{"original"}, second.Rooms[0].Tags)
	assert.NotEqual(t, DoorID(99), second.Rooms[0].DoorIDs[0])
	assert.NotEqual(t, Cell{X: 98, Y: 98}, second.Corridors[0].Cells[0])
	assert.Equal(t, []string{"stone"}, second.Corridors[0].Tags)
	assert.NotEqual(t, CorridorID(99), second.Doors[0].CorridorIDs[0])
	assert.Empty(t, second.Grid.Cells[0].CorridorIDs)
}

func TestPluginErrorsDoNotPublishLayout(t *testing.T) {
	pluginErr := errors.New("controlled plugin failure")
	tests := []Generator{
		{Placer: fixedPlacer{err: pluginErr}},
		{Placer: fixedPlacer{placements: []RoomPlacement{rectanglePlacementForTest(Cell{}, 1, 1)}}, Connector: fixedConnector{err: pluginErr}},
	}
	for _, generator := range tests {
		layout, err := generator.Generate(fixedGeometryConfigForTest(1, 1, 1))
		assert.ErrorIs(t, err, pluginErr)
		assert.NotErrorIs(t, err, ErrInvalidPlugin)
		assert.Equal(t, Layout{}, layout)
	}
}

func TestDefaultGeneratorProducesDynamicFootprintsAndConnectedTree(t *testing.T) {
	config := Config{Width: 32, Height: 32, Seed: 123, MaxRooms: 24}

	layout, err := (Generator{}).Generate(config)

	require.NoError(t, err)
	require.NotEmpty(t, layout.Rooms)
	assert.Len(t, layout.Corridors, len(layout.Rooms)-1)
	for _, room := range layout.Rooms {
		assert.Greater(t, len(room.Cells), 1)
		assert.Equal(t, room.Cells[0], room.At)
		assert.Equal(t, RoomShapeOffsets(room.Shape, room.Width, room.Height), localOffsetsForTest(room))
	}
	assertLayoutGraphConnectedForTest(t, layout)
}

func TestCatalogOrderDoesNotChangeWeightedSelection(t *testing.T) {
	firstConfig := fixedGeometryConfigForTest(7, 3, 2)
	firstConfig.Seed = 91
	firstConfig.PlantCatalog = &PlantCatalog{
		Rooms: []RoomPlant{
			{ID: "zeta", Weight: 3, DoorDirections: allDirectionsForTest()},
			{ID: "alpha", Weight: 1, DoorDirections: allDirectionsForTest()},
		},
		Corridors: []CorridorPlant{{ID: "zeta-corridor", Weight: 2}, {ID: "alpha-corridor", Weight: 1}},
	}
	secondConfig := firstConfig
	secondCatalog := *firstConfig.PlantCatalog
	secondCatalog.Rooms = []RoomPlant{firstConfig.PlantCatalog.Rooms[1], firstConfig.PlantCatalog.Rooms[0]}
	secondCatalog.Corridors = []CorridorPlant{firstConfig.PlantCatalog.Corridors[1], firstConfig.PlantCatalog.Corridors[0]}
	secondConfig.PlantCatalog = &secondCatalog

	first, firstErr := twoRoomGeneratorForTest().Generate(firstConfig)
	second, secondErr := twoRoomGeneratorForTest().Generate(secondConfig)

	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	assert.Equal(t, first, second)
}

func TestRoleRequiredTagsFilterRoomPlant(t *testing.T) {
	config := fixedGeometryConfigForTest(7, 3, 2)
	config.RoomRoleRequests = []RoomRoleRequest{{
		Role: RoomRoleStart, Count: 1, RequiredTags: []string{"start"},
	}}
	config.PlantCatalog = &PlantCatalog{
		Rooms: []RoomPlant{
			{ID: "common", Weight: 100, DoorDirections: allDirectionsForTest()},
			{ID: "entrance", Tags: []string{"start"}, Weight: 1, DoorDirections: allDirectionsForTest()},
		},
		Corridors: []CorridorPlant{{ID: "corridor", Weight: 1}},
	}

	layout, err := twoRoomGeneratorForTest().Generate(config)

	require.NoError(t, err)
	require.NotNil(t, layout.Rooms[0].Role)
	assert.Equal(t, RoomRoleStart, *layout.Rooms[0].Role)
	assert.Equal(t, PlantID("entrance"), layout.Rooms[0].PlantID)
	assert.Contains(t, layout.Rooms[0].Tags, "start")
}

func TestLimitsFailBeforeInvokingPlugins(t *testing.T) {
	geometryOverLimit := fixedGeometryConfigForTest(1, 1, 1)
	geometryOverLimit.RoomGeometry.MaxFootprintCells = MaxFootprintCells + 1
	tests := []Config{
		{Width: 257, Height: 1},
		{Width: 1, Height: 1, MaxRooms: MaxRooms + 1},
		geometryOverLimit,
	}
	for _, config := range tests {
		generator := Generator{Placer: panicPlacer{}}

		layout, err := generator.Generate(config)

		assert.ErrorIs(t, err, ErrLimitExceeded)
		assert.Equal(t, Layout{}, layout)
	}
}

func TestSharedGeneratorIsDeterministicUnderConcurrentCalls(t *testing.T) {
	generator := Generator{}
	config := Config{Width: 24, Height: 24, Seed: 456, MaxRooms: 20}
	baseline, baselineErr := generator.Generate(config)
	require.NoError(t, baselineErr)

	const concurrentRequests = 8
	results := make([]Layout, concurrentRequests)
	errs := make([]error, concurrentRequests)
	var wait sync.WaitGroup
	wait.Add(concurrentRequests)
	for index := range results {
		go func() {
			defer wait.Done()
			results[index], errs[index] = generator.Generate(config)
		}()
	}
	wait.Wait()

	for index := range results {
		require.NoError(t, errs[index])
		assert.Equal(t, baseline, results[index])
	}
}

func TestCancellationOfOneRequestDoesNotAffectAnother(t *testing.T) {
	generator := Generator{}
	config := Config{Width: 24, Height: 24, Seed: 789, MaxRooms: 20}
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()

	var canceledLayout Layout
	var canceledErr error
	var successfulLayout Layout
	var successfulErr error
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		canceledLayout, canceledErr = generator.GenerateContext(canceledContext, config)
	}()
	go func() {
		defer wait.Done()
		successfulLayout, successfulErr = generator.Generate(config)
	}()
	wait.Wait()

	assert.ErrorIs(t, canceledErr, context.Canceled)
	assert.Equal(t, Layout{}, canceledLayout)
	require.NoError(t, successfulErr)
	assert.NotEmpty(t, successfulLayout.Rooms)
}

func TestCancellationDuringPlacementValidationPreventsNextPhase(t *testing.T) {
	ctx := &countingCancelContext{cancelAt: 4}
	connector := &recordingConnector{}
	generator := Generator{
		Placer: fixedPlacer{placements: []RoomPlacement{
			rectanglePlacementForTest(Cell{X: 1, Y: 1}, 1, 1),
		}},
		Connector: connector,
	}

	layout, err := generator.GenerateContext(ctx, fixedGeometryConfigForTest(3, 3, 1))

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, Layout{}, layout)
	assert.False(t, connector.called)
}

type fixedPlacer struct {
	placements []RoomPlacement
	err        error
}

func (placer fixedPlacer) Place(PlacementRequest) ([]RoomPlacement, error) {
	return placer.placements, placer.err
}

type fixedConnector struct {
	connections []Connection
	err         error
}

type panicPlacer struct{}

func (panicPlacer) Place(PlacementRequest) ([]RoomPlacement, error) {
	panic("Placer must not be called")
}

type recordingConnector struct {
	called bool
}

func (connector *recordingConnector) Connect(ConnectionRequest) ([]Connection, error) {
	connector.called = true
	return nil, nil
}

type countingCancelContext struct {
	calls    int
	cancelAt int
}

func (ctx *countingCancelContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (ctx *countingCancelContext) Done() <-chan struct{}       { return nil }
func (ctx *countingCancelContext) Value(any) any               { return nil }
func (ctx *countingCancelContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.cancelAt {
		return context.Canceled
	}
	return nil
}

func (connector fixedConnector) Connect(ConnectionRequest) ([]Connection, error) {
	return connector.connections, connector.err
}

func fixedGeometryConfigForTest(width, height, maxRooms uint32) Config {
	return Config{
		Width: width, Height: height, MinDistance: 1, MaxRooms: maxRooms,
		RoomGeometry: &RoomGeometry{
			MaxFootprintCells: 1,
			Shapes:            []RoomShapeWeight{shapeSpan(RoomShapeRectangle, 1, 1, 1, 1, 1)},
		},
	}
}

func twoRoomGeneratorForTest() Generator {
	return Generator{
		Placer: fixedPlacer{placements: []RoomPlacement{
			rectanglePlacementForTest(Cell{X: 1, Y: 1}, 1, 1),
			rectanglePlacementForTest(Cell{X: 5, Y: 1}, 1, 1),
		}},
		Connector: fixedConnector{connections: []Connection{{FromRoomID: 0, ToRoomID: 1}}},
	}
}

func rectanglePlacementForTest(origin Cell, width, height uint32) RoomPlacement {
	return RoomPlacement{
		Shape: RoomShapeRectangle, Origin: origin, Width: width, Height: height,
		Cells: RoomShapeOffsets(RoomShapeRectangle, width, height),
	}
}

func allDirectionsForTest() []Direction {
	return []Direction{DirectionNorth, DirectionEast, DirectionSouth, DirectionWest}
}

func localOffsetsForTest(room Room) []Cell {
	offsets := make([]Cell, len(room.Cells))
	for index, cell := range room.Cells {
		offsets[index] = Cell{X: cell.X - room.Origin.X, Y: cell.Y - room.Origin.Y}
	}
	return offsets
}

func assertLayoutGraphConnectedForTest(t *testing.T, layout Layout) {
	t.Helper()
	adjacency := make([][]RoomID, len(layout.Rooms))
	for _, corridor := range layout.Corridors {
		adjacency[corridor.FromRoomID] = append(adjacency[corridor.FromRoomID], corridor.ToRoomID)
		adjacency[corridor.ToRoomID] = append(adjacency[corridor.ToRoomID], corridor.FromRoomID)
	}
	visited := make([]bool, len(layout.Rooms))
	queue := []RoomID{0}
	visited[0] = true
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, neighbor := range adjacency[current] {
			if !visited[neighbor] {
				visited[neighbor] = true
				queue = append(queue, neighbor)
			}
		}
	}
	for roomID, reached := range visited {
		assert.True(t, reached, "RoomID %d was not reached", roomID)
	}
}

func roomIDPointerForTest(id RoomID) *RoomID {
	return &id
}
