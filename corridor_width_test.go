package daedalus

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvenCorridorWidthLandsOnThePositiveSide(t *testing.T) {
	t.Run("horizontal travel adds the extra Cell on +Y", func(t *testing.T) {
		rooms := []PlacedRoom{
			placedRectangle(t, 0, Cell{X: 1, Y: 2}, 1, 2),
			placedRectangle(t, 1, Cell{X: 6, Y: 2}, 1, 2),
		}
		corridor := routeFixedWidth(t, 8, 6, rooms, 2)

		require.NotEmpty(t, corridor.Centerline)
		for _, cell := range corridor.Centerline {
			assert.Equal(t, int32(2), cell.Y)
			assert.Contains(t, corridor.Cells, Cell{X: cell.X, Y: cell.Y + 1})
			assert.NotContains(t, corridor.Cells, Cell{X: cell.X, Y: cell.Y - 1})
		}
	})

	t.Run("vertical travel adds the extra Cell on +X", func(t *testing.T) {
		rooms := []PlacedRoom{
			placedRectangle(t, 0, Cell{X: 2, Y: 1}, 2, 1),
			placedRectangle(t, 1, Cell{X: 2, Y: 6}, 2, 1),
		}
		corridor := routeFixedWidth(t, 8, 8, rooms, 2)

		require.NotEmpty(t, corridor.Centerline)
		for _, cell := range corridor.Centerline {
			assert.Equal(t, int32(2), cell.X)
			assert.Contains(t, corridor.Cells, Cell{X: cell.X + 1, Y: cell.Y})
			assert.NotContains(t, corridor.Cells, Cell{X: cell.X - 1, Y: cell.Y})
		}
	})
}

func TestBendFillsSquareAndStaysFourConnected(t *testing.T) {
	// East, then north into a south-facing Door. The +X/+Y corner of the bend
	// sits off both arms: a strip-only dilation leaves a diagonal pinch there.
	rooms := []PlacedRoom{
		placedRectangle(t, 0, Cell{X: 1, Y: 4}, 1, 2),
		placedRectangle(t, 1, Cell{X: 6, Y: 1}, 2, 1),
	}
	corridor := routeFixedWidth(t, 10, 8, rooms, 2)

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
		assert.Contains(t, corridor.Cells, corner, "bend %v left a diagonal pinch", centerline[index])
		assert.Contains(t, corridor.Cells, Cell{X: corner.X, Y: centerline[index].Y})
		assert.Contains(t, corridor.Cells, Cell{X: centerline[index].X, Y: corner.Y})
	}
	assert.True(t, foundBend, "the fixture must bend or it does not guard the corner")
	assertBandFourConnected(t, corridor.Cells)
}

func TestDegradationWalksDeclaredWidthsOnly(t *testing.T) {
	rooms := []PlacedRoom{
		placedRectangle(t, 0, Cell{X: 1, Y: 2}, 1, 2),
		placedRectangle(t, 1, Cell{X: 6, Y: 2}, 1, 2),
	}
	openings := routedOpenings(t, 8, 6, rooms)

	t.Run("an undeclared narrower width is not invented", func(t *testing.T) {
		_, width, found, err := routeDegraded(
			openings[0], openings[1], CorridorOrderXThenY, 4,
			[]CorridorWidthWeight{{Width: 4, Weight: 2}},
			newClearedSearch(t, 8, 6, rooms),
		)
		require.NoError(t, err)
		assert.False(t, found, "width 4 does not fit and 3, 2, 1 were not declared")
		assert.Equal(t, uint32(0), width)
	})

	t.Run("a drawn 4 falls straight to the next declared width", func(t *testing.T) {
		_, width, found, err := routeDegraded(
			openings[0], openings[1], CorridorOrderXThenY, 4,
			[]CorridorWidthWeight{{Width: 1, Weight: 1}, {Width: 4, Weight: 2}},
			newClearedSearch(t, 8, 6, rooms),
		)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, uint32(1), width, "declared [1, 4] skips 3 and 2")
	})

	t.Run("the widest declared width that fits wins and draws once", func(t *testing.T) {
		widths := []CorridorWidthWeight{{Width: 2, Weight: 1}, {Width: 4, Weight: 2}}
		streams := newRNGStreams(Seed(11))
		before := streams.corridorWidth
		corridors, doors, err := routeCorridorsWithWidths(
			context.Background(), 8, 6, CorridorOrderXThenY, rooms,
			[]Connection{{FromRoomID: 0, ToRoomID: 1}},
			widths, &streams.corridorWidth,
		)
		require.NoError(t, err)
		require.Len(t, corridors, 1)
		assert.Equal(t, uint32(2), doors[corridors[0].FromDoorID].Span)
		assert.Equal(t, uint32(2), doors[corridors[0].ToDoorID].Span)
		assert.NotEqual(t, before, streams.corridorWidth)
		once := newRNGStreams(Seed(11))
		_ = drawCorridorWidth(widths, &once.corridorWidth)
		assert.Equal(t, once.corridorWidth, streams.corridorWidth,
			"walking the declared list must not draw again")
	})
}

func TestNilCorridorGeometryConsumesNoWidthDraw(t *testing.T) {
	rooms := []PlacedRoom{
		placedRectangle(t, 0, Cell{X: 1, Y: 2}, 1, 1),
		placedRectangle(t, 1, Cell{X: 5, Y: 2}, 1, 1),
	}
	streams := newRNGStreams(Seed(99))
	widthBefore := streams.corridorWidth
	plantBefore := streams.corridorPlant
	placementBefore := streams.placement

	corridors, doors, err := routeCorridorsWithWidths(
		context.Background(), 7, 5, CorridorOrderXThenY, rooms,
		[]Connection{{FromRoomID: 0, ToRoomID: 1}},
		nil, &streams.corridorWidth,
	)

	require.NoError(t, err)
	require.Len(t, corridors, 1)
	assert.Equal(t, corridors[0].Cells, corridors[0].Centerline)
	assert.Equal(t, uint32(1), doors[0].Span)
	assert.Equal(t, widthBefore, streams.corridorWidth)
	assert.Equal(t, plantBefore, streams.corridorPlant)
	assert.Equal(t, placementBefore, streams.placement)

	legacy, _, legacyErr := routeCorridors(
		context.Background(), 7, 5, CorridorOrderXThenY, rooms,
		[]Connection{{FromRoomID: 0, ToRoomID: 1}},
	)
	require.NoError(t, legacyErr)
	assert.Equal(t, legacy[0].Cells, corridors[0].Cells)
}

func TestWideBFSDetoursWhenBothLRoutesAreBlocked(t *testing.T) {
	rooms := []PlacedRoom{
		placedRectangle(t, 0, Cell{X: 1, Y: 4}, 1, 3),
		placedRectangle(t, 1, Cell{X: 7, Y: 4}, 1, 3),
		placedRectangle(t, 2, Cell{X: 4, Y: 3}, 1, 5),
	}
	streams := newRNGStreams(Seed(3))
	corridors, doors, err := routeCorridorsWithWidths(
		context.Background(), 10, 9, CorridorOrderXThenY, rooms,
		[]Connection{{FromRoomID: 0, ToRoomID: 1}},
		[]CorridorWidthWeight{{Width: 2, Weight: 1}}, &streams.corridorWidth,
	)
	require.NoError(t, err)
	require.Len(t, corridors, 1)
	assert.Equal(t, uint32(2), doors[corridors[0].FromDoorID].Span)

	wentAround := false
	for _, cell := range corridors[0].Centerline {
		assert.False(t, cell.X == 4 && cell.Y >= 3, "centerline crossed the wall at %v", cell)
		if cell.Y < 3 {
			wentAround = true
		}
	}
	assert.True(t, wentAround, "width 2 must take the open detour rather than degrade")
	assertBandFourConnected(t, corridors[0].Cells)
}

func TestWidthOrderDoesNotChangeTheDraw(t *testing.T) {
	rooms := []PlacedRoom{
		placedRectangle(t, 0, Cell{X: 1, Y: 2}, 1, 3),
		placedRectangle(t, 1, Cell{X: 6, Y: 2}, 1, 3),
	}
	ascending := []CorridorWidthWeight{{Width: 1, Weight: 1}, {Width: 3, Weight: 1}}
	descending := []CorridorWidthWeight{{Width: 3, Weight: 1}, {Width: 1, Weight: 1}}
	first := routeWidths(t, 8, 8, rooms, ascending, 21)
	second := routeWidths(t, 8, 8, rooms, descending, 21)
	assert.Equal(t, first.Centerline, second.Centerline)
	assert.Equal(t, first.Cells, second.Cells)
}

func TestCorridorWidthValidation(t *testing.T) {
	base := Config{Width: 8, Height: 8}
	t.Run("width above 64 is invalid config", func(t *testing.T) {
		config := base
		config.CorridorGeometry = &CorridorGeometry{Widths: []CorridorWidthWeight{{Width: 65, Weight: 1}}}
		_, err := normalizeConfig(config)
		assert.ErrorIs(t, err, ErrInvalidConfig)
		assert.NotErrorIs(t, err, ErrLimitExceeded)
	})
	t.Run("width 64 is accepted", func(t *testing.T) {
		config := base
		config.CorridorGeometry = &CorridorGeometry{Widths: []CorridorWidthWeight{
			{Width: 1, Weight: 1},
			{Width: 64, Weight: 1},
		}}
		effective, err := normalizeConfig(config)
		require.NoError(t, err)
		assert.Equal(t, []CorridorWidthWeight{
			{Width: 1, Weight: 1},
			{Width: 64, Weight: 1},
		}, effective.corridorWidths)
	})
	t.Run("nil geometry draws nothing", func(t *testing.T) {
		effective, err := normalizeConfig(base)
		require.NoError(t, err)
		assert.Nil(t, effective.corridorWidths)
		assert.Equal(t, uint32(1), effective.roomGeometry.MinRoomGap)
	})
	t.Run("empty widths", func(t *testing.T) {
		config := base
		config.CorridorGeometry = &CorridorGeometry{}
		_, err := normalizeConfig(config)
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})
	t.Run("duplicate width", func(t *testing.T) {
		config := base
		config.CorridorGeometry = &CorridorGeometry{Widths: []CorridorWidthWeight{
			{Width: 2, Weight: 1},
			{Width: 2, Weight: 3},
		}}
		_, err := normalizeConfig(config)
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})
	t.Run("zero weight", func(t *testing.T) {
		config := base
		config.CorridorGeometry = &CorridorGeometry{Widths: []CorridorWidthWeight{{Width: 2, Weight: 0}}}
		_, err := normalizeConfig(config)
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})
	t.Run("widths are sorted by Width", func(t *testing.T) {
		config := base
		config.CorridorGeometry = &CorridorGeometry{Widths: []CorridorWidthWeight{
			{Width: 3, Weight: 2},
			{Width: 1, Weight: 4},
		}}
		effective, err := normalizeConfig(config)
		require.NoError(t, err)
		assert.Equal(t, []CorridorWidthWeight{
			{Width: 1, Weight: 4},
			{Width: 3, Weight: 2},
		}, effective.corridorWidths)
	})
}

func routedOpenings(t *testing.T, width, height uint32, rooms []PlacedRoom) [][]doorOpening {
	t.Helper()
	occupancy := newPlacementOccupancy(width, height)
	openings := make([][]doorOpening, len(rooms))
	for roomIndex, room := range rooms {
		occupancy.mark(uint32(roomIndex), room.Cells)
	}
	for roomIndex, room := range rooms {
		openings[roomIndex] = enumerateDoorOpenings(room, uint32(roomIndex), occupancy)
	}
	return openings
}

func newClearedSearch(t *testing.T, width, height uint32, rooms []PlacedRoom) *routingSearch {
	t.Helper()
	occupancy := newPlacementOccupancy(width, height)
	for roomIndex, room := range rooms {
		occupancy.mark(uint32(roomIndex), room.Cells)
	}
	search := newRoutingSearch(context.Background(), occupancy)
	search.clearance = newWidthClearance(occupancy)
	return search
}

func routeFixedWidth(t *testing.T, width, height uint32, rooms []PlacedRoom, corridorWidth uint32) Corridor {
	t.Helper()
	return routeWidths(t, width, height, rooms, []CorridorWidthWeight{{Width: corridorWidth, Weight: 1}}, 1)
}

func routeWidths(t *testing.T, width, height uint32, rooms []PlacedRoom, widths []CorridorWidthWeight, seed Seed) Corridor {
	t.Helper()
	streams := newRNGStreams(seed)
	corridors, doors, err := routeCorridorsWithWidths(
		context.Background(), width, height, CorridorOrderXThenY, rooms,
		[]Connection{{FromRoomID: 0, ToRoomID: 1}},
		widths, &streams.corridorWidth,
	)
	require.NoError(t, err)
	require.Len(t, corridors, 1)
	require.NotEmpty(t, doors)
	return corridors[0]
}

func assertBandFourConnected(t *testing.T, cells []Cell) {
	t.Helper()
	require.NotEmpty(t, cells)
	index := make(map[Cell]struct{}, len(cells))
	for _, cell := range cells {
		index[cell] = struct{}{}
	}
	queue := []Cell{cells[0]}
	seen := map[Cell]struct{}{cells[0]: {}}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, delta := range []Cell{{X: 0, Y: -1}, {X: 1, Y: 0}, {X: 0, Y: 1}, {X: -1, Y: 0}} {
			next := Cell{X: current.X + delta.X, Y: current.Y + delta.Y}
			if _, ok := index[next]; !ok {
				continue
			}
			if _, ok := seen[next]; ok {
				continue
			}
			seen[next] = struct{}{}
			queue = append(queue, next)
		}
	}
	assert.Equal(t, len(cells), len(seen), "band is not 4-connected")
}
