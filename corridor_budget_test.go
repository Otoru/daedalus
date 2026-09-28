package daedalus

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCircleCapacityIsPositiveAtWidthOneAndZeroAtWidthThree(t *testing.T) {
	for size := uint32(5); size <= 9; size += 2 {
		cells := shiftCells(RoomShapeOffsets(RoomShapeCircle, size, size), 8, 8)
		atOne := roomOpeningCapacity(cells, 64, 64, 1)
		atThree := roomOpeningCapacity(cells, 64, 64, 3)
		t.Logf("circle %d: width1=%d width3=%d", size, atOne, atThree)
		assert.Positive(t, atOne, "circle %d hosts a one-cell corridor", size)
		assert.Zero(t, atThree, "circle %d has no straight run of three", size)
	}
}

func TestCrossCapacityAtWidthThree(t *testing.T) {
	for size := uint32(3); size <= 9; size++ {
		if !ValidRoomShapeDimensions(RoomShapeCross, size, size) {
			continue
		}
		cells := shiftCells(RoomShapeOffsets(RoomShapeCross, size, size), 8, 8)
		atOne := roomOpeningCapacity(cells, 64, 64, 1)
		atThree := roomOpeningCapacity(cells, 64, 64, 3)
		t.Logf("cross %d: width1=%d width3=%d", size, atOne, atThree)
		assert.Positive(t, atOne, "cross %d", size)
	}
}

func TestPrimRefusesACircleOnTheWidestBudgetBeforeTheRouter(t *testing.T) {
	rooms := []PlacedRoom{
		rectanglePlacedRoom(0, 2, 2, 5, 5),
		circlePlacedRoom(1, 20, 2, 7),
	}
	var routed int
	_, err := (primRoomsConnector{}).Connect(ConnectionRequest{
		Context: context.Background(), Width: 64, Height: 64,
		MaxCorridorWidth: 3, Rooms: rooms,
		TryRoute: func(RoomID, RoomID) (bool, error) {
			routed++
			return true, nil
		},
	})
	require.ErrorIs(t, err, ErrUnconnectablePlacement)
	assert.Zero(t, routed, "the budget refuses the circle before TryRoute")

	routed = 0
	edges, err := (primRoomsConnector{}).Connect(ConnectionRequest{
		Context: context.Background(), Width: 64, Height: 64,
		MaxCorridorWidth: 1, Rooms: rooms,
		TryRoute: func(RoomID, RoomID) (bool, error) {
			routed++
			return true, nil
		},
	})
	require.NoError(t, err)
	require.Len(t, edges, 1)
	assert.Positive(t, routed)
}

func TestNarrowestDeclaredWidthIsTheDegradationFloor(t *testing.T) {
	assert.Equal(t, uint32(1), narrowestDeclaredCorridorWidth(nil))
	assert.Equal(t, uint32(1), narrowestDeclaredCorridorWidth([]CorridorWidthWeight{
		{Width: 3, Weight: 1},
		{Width: 1, Weight: 1},
	}))
	assert.Equal(t, uint32(3), narrowestDeclaredCorridorWidth([]CorridorWidthWeight{
		{Width: 3, Weight: 1},
	}))
}

func TestGeneratorBudgetsTheNarrowestDeclaredWidth(t *testing.T) {
	var budget uint32
	generator := Generator{Connector: ConnectorFunc(func(req ConnectionRequest) ([]Connection, error) {
		budget = req.MaxCorridorWidth
		return primRoomsConnector{}.Connect(req)
	})}
	config := rectangleAndCircleWidths(1)
	layout, err := generator.Generate(config)
	require.NoError(t, err)
	assert.Equal(t, uint32(1), budget)
	assert.Positive(t, countShape(layout, RoomShapeCircle))
	for _, corridor := range layout.Corridors {
		assert.Contains(t, []uint32{1, 3}, doorSpan(layout, corridor))
	}
}

func TestReleasingACorridorKeepsTheSurvivorsHalo(t *testing.T) {
	widths := &CorridorGeometry{Widths: []CorridorWidthWeight{
		{Width: 1, Weight: 1},
		{Width: 3, Weight: 1},
	}}
	for _, seed := range []Seed{22, 191} {
		config := Config{
			Width: 48, Height: 40, Seed: seed, MaxRooms: 28, ExtraEdgeCount: 3,
			CorridorGeometry: widths,
		}
		effective, err := normalizeConfig(config)
		require.NoError(t, err)
		layout, err := (Generator{}).Generate(config)
		require.NoError(t, err, "seed %d", seed)
		for _, failure := range layoutInvariantFailures(effective, layout) {
			t.Errorf("seed %d: %s", seed, failure)
		}
	}
}

func TestShippedDebugExampleGeneratesMixedWidths(t *testing.T) {
	config := Config{
		Width: 64, Height: 64, CellSize: 1, Seed: 4242,
		MinDistance: 6, MaxAttempts: 30, MaxRooms: 128,
		CorridorGeometry: &CorridorGeometry{Widths: []CorridorWidthWeight{
			{Width: 1, Weight: 5},
			{Width: 2, Weight: 2},
			{Width: 3, Weight: 1},
		}},
	}
	layout, err := (Generator{}).Generate(config)
	require.NoError(t, err)
	spans := map[uint32]int{}
	for _, corridor := range layout.Corridors {
		span := doorSpan(layout, corridor)
		assert.Contains(t, []uint32{1, 2, 3}, span)
		spans[span]++
	}
	assert.Positive(t, spans[1])
	assert.Positive(t, spans[2]+spans[3], "the shipped example still shows a corridor wider than one cell")
}

func TestRectangleAndCircleGenerateWhenWidthOneIsDeclared(t *testing.T) {
	layout, err := (Generator{}).Generate(rectangleAndCircleWidths(1))
	require.NoError(t, err)
	require.Positive(t, countShape(layout, RoomShapeCircle))
	for _, corridor := range layout.Corridors {
		assert.Contains(t, []uint32{1, 3}, doorSpan(layout, corridor))
	}
}

func rectangleAndCircleWidths(seed Seed) Config {
	return Config{
		Width: 64, Height: 64, Seed: seed, MaxRooms: 48,
		RoomGeometry: &RoomGeometry{
			MaxFootprintCells: 81, MinRoomGap: 1,
			Shapes: []RoomShapeWeight{
				shapeSpan(RoomShapeRectangle, 4, 3, 9, 3, 9),
				shapeSpan(RoomShapeCircle, 2, 5, 9, 5, 9),
			},
		},
		CorridorGeometry: &CorridorGeometry{Widths: []CorridorWidthWeight{
			{Width: 1, Weight: 5},
			{Width: 3, Weight: 2},
		}},
	}
}

func countShape(layout Layout, shape RoomShape) int {
	count := 0
	for _, room := range layout.Rooms {
		if room.Shape == shape {
			count++
		}
	}
	return count
}

func circlePlacedRoom(id RoomID, x, y int32, size uint32) PlacedRoom {
	local := RoomShapeOffsets(RoomShapeCircle, size, size)
	cells := shiftCells(local, x, y)
	return PlacedRoom{
		ID: id, At: cells[0], Shape: RoomShapeCircle,
		Origin: Cell{X: x, Y: y}, Width: size, Height: size, Cells: cells,
	}
}

func shiftCells(cells []Cell, x, y int32) []Cell {
	shifted := make([]Cell, len(cells))
	for index, cell := range cells {
		shifted[index] = Cell{X: cell.X + x, Y: cell.Y + y}
	}
	return shifted
}

func doorSpan(layout Layout, corridor Corridor) uint32 {
	for _, door := range layout.Doors {
		if door.ID == corridor.FromDoorID {
			return door.Span
		}
	}
	return 0
}
