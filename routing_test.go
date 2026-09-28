package daedalus

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoutingReproducesWorkedLRoute checks the worked L-route between two 2×2
// Rectangles. From the Cell east of a Door at (2,1) to the Cell north of a Door
// at (5,3), XThenY yields [(3,1), (4,1), (5,1), (5,2)]: an orthogonal sequence
// that stays outside both footprints and meets the endpoint Doors.
func TestRoutingReproducesWorkedLRoute(t *testing.T) {
	rooms := []PlacedRoom{
		placedRectangle(t, 0, Cell{X: 1, Y: 1}, 2, 2),
		placedRectangle(t, 1, Cell{X: 5, Y: 3}, 2, 2),
	}
	occupancy := newPlacementOccupancy(8, 6)
	for roomIndex, room := range rooms {
		occupancy.mark(uint32(roomIndex), room.Cells)
	}

	route, ok := lRoute(
		Cell{X: 3, Y: 1},
		Cell{X: 5, Y: 2},
		CorridorOrderXThenY,
		occupancy,
		nil,
	)

	require.True(t, ok)
	assert.Equal(t, []Cell{{X: 3, Y: 1}, {X: 4, Y: 1}, {X: 5, Y: 1}, {X: 5, Y: 2}}, route)
}

func TestRoutingChoosesDoorPairWithShortestCorridor(t *testing.T) {
	rooms := []PlacedRoom{
		placedRectangle(t, 0, Cell{X: 1, Y: 2}, 1, 1),
		placedRectangle(t, 1, Cell{X: 5, Y: 2}, 1, 1),
	}

	corridors, doors, err := routeCorridors(
		context.Background(), 7, 5, CorridorOrderXThenY,
		rooms, []Connection{{FromRoomID: 0, ToRoomID: 1}},
	)

	require.NoError(t, err)
	require.Len(t, corridors, 1)
	require.Len(t, doors, 2)
	assert.Equal(t, []Cell{{X: 2, Y: 2}, {X: 3, Y: 2}, {X: 4, Y: 2}}, corridors[0].Cells)
	assert.Equal(t, Door{ID: 0, RoomID: 0, At: Cell{X: 1, Y: 2}, Direction: DirectionEast, Span: 1, CorridorIDs: []CorridorID{0}}, doors[0])
	assert.Equal(t, Door{ID: 1, RoomID: 1, At: Cell{X: 5, Y: 2}, Direction: DirectionWest, Span: 1, CorridorIDs: []CorridorID{0}}, doors[1])
}

// TestDeterministicBFSDetoursAroundRoomBlockingBothLRoutes checks that when
// both L-routes are blocked, deterministic BFS still finds an exterior route.
// The search expands North, East, South, West, uses a FIFO queue, marks a Cell
// on insertion, takes the parent from first discovery, and stores row-major.
func TestDeterministicBFSDetoursAroundRoomBlockingBothLRoutes(t *testing.T) {
	rooms := []PlacedRoom{
		placedRectangle(t, 0, Cell{X: 1, Y: 3}, 1, 1),
		placedRectangle(t, 1, Cell{X: 5, Y: 3}, 1, 1),
		placedRectangle(t, 2, Cell{X: 3, Y: 3}, 1, 1),
	}
	occupancy := newPlacementOccupancy(7, 7)
	for roomIndex, room := range rooms {
		occupancy.mark(uint32(roomIndex), room.Cells)
	}
	from := doorOpening{roomID: 0, at: rooms[0].At, direction: DirectionEast, outside: Cell{X: 2, Y: 3}}
	to := doorOpening{roomID: 1, at: rooms[1].At, direction: DirectionWest, outside: Cell{X: 4, Y: 3}}
	search := newRoutingSearch(context.Background(), occupancy)

	route, ok, err := routeOpeningPair(from, to, CorridorOrderXThenY, search, nil)

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []Cell{{X: 2, Y: 3}, {X: 2, Y: 2}, {X: 3, Y: 2}, {X: 4, Y: 2}, {X: 4, Y: 3}}, route)
}

func TestCorridorOrderChangesBendWhenBothLRoutesAreValid(t *testing.T) {
	occupancy := newPlacementOccupancy(5, 5)
	search := newRoutingSearch(context.Background(), occupancy)
	from := doorOpening{outside: Cell{X: 1, Y: 1}}
	to := doorOpening{outside: Cell{X: 3, Y: 3}}

	xThenY, ok, err := routeOpeningPair(from, to, CorridorOrderXThenY, search, nil)
	require.NoError(t, err)
	require.True(t, ok)
	yThenX, ok, err := routeOpeningPair(from, to, CorridorOrderYThenX, search, nil)
	require.NoError(t, err)
	require.True(t, ok)

	assert.Equal(t, []Cell{{X: 1, Y: 1}, {X: 2, Y: 1}, {X: 3, Y: 1}, {X: 3, Y: 2}, {X: 3, Y: 3}}, xThenY)
	assert.Equal(t, []Cell{{X: 1, Y: 1}, {X: 1, Y: 2}, {X: 1, Y: 3}, {X: 2, Y: 3}, {X: 3, Y: 3}}, yThenX)
}

// TestUnroutableEdgeDiscardsPartialResult checks that a blocked pair returns
// the same ErrUnroutableEdge on every run and discards the partial Corridors
// and Doors.
func TestUnroutableEdgeDiscardsPartialResult(t *testing.T) {
	rooms := []PlacedRoom{
		placedRectangle(t, 0, Cell{X: 0, Y: 2}, 1, 1),
		placedRectangle(t, 1, Cell{X: 4, Y: 2}, 1, 1),
		placedRectangle(t, 2, Cell{X: 2, Y: 0}, 1, 5),
	}
	connections := []Connection{{FromRoomID: 0, ToRoomID: 1}}

	firstCorridors, firstDoors, firstErr := routeCorridors(
		context.Background(), 5, 5, CorridorOrderXThenY, rooms, connections,
	)
	secondCorridors, secondDoors, secondErr := routeCorridors(
		context.Background(), 5, 5, CorridorOrderXThenY, rooms, connections,
	)

	assert.ErrorIs(t, firstErr, ErrUnroutableEdge)
	assert.EqualError(t, secondErr, firstErr.Error())
	assert.Nil(t, firstCorridors)
	assert.Nil(t, firstDoors)
	assert.Nil(t, secondCorridors)
	assert.Nil(t, secondDoors)
}

// TestTwoEdgesDoNotReuseADoorWhoseBandIsOccupied checks that a second edge
// between the same Rooms cannot re-enter the first Corridor's opening: that
// exterior Cell belongs to the first band. The second edge may leave by a
// neighbouring opening and touch the first Corridor there, because both Cells
// are orthogonally adjacent to the Room.
func TestTwoEdgesDoNotReuseADoorWhoseBandIsOccupied(t *testing.T) {
	rooms := []PlacedRoom{
		placedRectangle(t, 0, Cell{X: 1, Y: 2}, 1, 1),
		placedRectangle(t, 1, Cell{X: 5, Y: 2}, 1, 1),
	}
	connections := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 1},
	}

	corridors, doors, err := routeCorridors(
		context.Background(), 7, 5, CorridorOrderXThenY, rooms, connections,
	)

	require.NoError(t, err)
	require.Len(t, corridors, 2)
	assert.NotEqual(t, corridors[0].FromDoorID, corridors[1].FromDoorID)
	assert.NotEqual(t, corridors[0].ToDoorID, corridors[1].ToDoorID)
	for _, door := range doors {
		assert.Len(t, door.CorridorIDs, 1)
	}
	assertContactOnlyBesideRooms(t, rooms, corridors)
}

// TestEmptyFacingCorridorsMayShareADoor checks the one geometry in which two
// Corridors can name the same Door without touching: both bands are empty
// because the Doors face each other, so there is no Cell for the halo to claim.
func TestEmptyFacingCorridorsMayShareADoor(t *testing.T) {
	rooms := []PlacedRoom{
		placedRectangle(t, 0, Cell{X: 1, Y: 1}, 1, 1),
		placedRectangle(t, 1, Cell{X: 2, Y: 1}, 1, 1),
	}
	corridors, doors, err := routeCorridors(
		context.Background(), 4, 3, CorridorOrderXThenY, rooms,
		[]Connection{{FromRoomID: 0, ToRoomID: 1}, {FromRoomID: 0, ToRoomID: 1}},
	)
	require.NoError(t, err)
	require.Len(t, corridors, 2)
	assert.Empty(t, corridors[0].Cells)
	assert.Empty(t, corridors[1].Cells)
	assert.Equal(t, corridors[0].FromDoorID, corridors[1].FromDoorID)
	assert.Equal(t, []CorridorID{0, 1}, doors[corridors[0].FromDoorID].CorridorIDs)
}

// TestDegreeThreeRoomCorridorsTouchOnlyBesideTheWall checks that three
// Corridors can leave a 1×1 Room. Their first Cells sit on adjacent sides of
// the footprint and therefore touch diagonally. That contact is legal only
// because both Cells are orthogonally adjacent to the Room. Cells farther
// out still stay at least Chebyshev distance 2 apart, and no Cell is shared.
func TestDegreeThreeRoomCorridorsTouchOnlyBesideTheWall(t *testing.T) {
	rooms := []PlacedRoom{
		placedRectangle(t, 0, Cell{X: 3, Y: 3}, 1, 1),
		placedRectangle(t, 1, Cell{X: 3, Y: 6}, 1, 1),
		placedRectangle(t, 2, Cell{X: 3, Y: 0}, 1, 1),
		placedRectangle(t, 3, Cell{X: 6, Y: 3}, 1, 1),
	}

	corridors, _, err := routeCorridors(
		context.Background(), 7, 7, CorridorOrderXThenY, rooms,
		[]Connection{
			{FromRoomID: 0, ToRoomID: 1},
			{FromRoomID: 0, ToRoomID: 2},
			{FromRoomID: 0, ToRoomID: 3},
		},
	)

	require.NoError(t, err)
	require.Len(t, corridors, 3)
	assert.True(t, assertContactOnlyBesideRooms(t, rooms, corridors),
		"three exits from a 1×1 Room have to meet beside the wall")
}

func TestCorridorsKeepChebyshevSeparation(t *testing.T) {
	rooms := []PlacedRoom{
		placedRectangle(t, 0, Cell{X: 1, Y: 3}, 1, 1),
		placedRectangle(t, 1, Cell{X: 5, Y: 3}, 1, 1),
		placedRectangle(t, 2, Cell{X: 3, Y: 1}, 1, 1),
		placedRectangle(t, 3, Cell{X: 3, Y: 5}, 1, 1),
	}

	corridors, _, err := routeCorridors(
		context.Background(), 7, 7, CorridorOrderXThenY, rooms,
		[]Connection{{FromRoomID: 0, ToRoomID: 1}, {FromRoomID: 2, ToRoomID: 3}},
	)

	require.NoError(t, err)
	require.Len(t, corridors, 2)
	assert.NotContains(t, corridors[1].Cells, Cell{X: 3, Y: 3})
	assertCorridorChebyshevSeparation(t, corridors)
}

func TestAdjacentFacingRoomsProduceEmptyCorridor(t *testing.T) {
	rooms := []PlacedRoom{
		placedRectangle(t, 0, Cell{X: 1, Y: 1}, 1, 1),
		placedRectangle(t, 1, Cell{X: 2, Y: 1}, 1, 1),
	}

	corridors, doors, err := routeCorridors(
		context.Background(), 4, 3, CorridorOrderXThenY, rooms,
		[]Connection{{FromRoomID: 0, ToRoomID: 1}},
	)

	require.NoError(t, err)
	require.Len(t, corridors, 1)
	assert.Empty(t, corridors[0].Cells)
	assert.Equal(t, DirectionEast, doors[corridors[0].FromDoorID].Direction)
	assert.Equal(t, DirectionWest, doors[corridors[0].ToDoorID].Direction)
}

func TestAdjacentNonFacingRoomsDoNotProduceEmptyCorridor(t *testing.T) {
	rooms := []PlacedRoom{
		placedRectangle(t, 0, Cell{X: 1, Y: 1}, 1, 1),
		placedRectangle(t, 1, Cell{X: 2, Y: 1}, 1, 1),
	}
	occupancy := newPlacementOccupancy(4, 3)
	for roomIndex, room := range rooms {
		occupancy.mark(uint32(roomIndex), room.Cells)
	}
	search := newRoutingSearch(context.Background(), occupancy)
	from := doorOpening{roomID: 0, at: rooms[0].At, direction: DirectionNorth, outside: Cell{X: 1, Y: 0}}
	to := doorOpening{roomID: 1, at: rooms[1].At, direction: DirectionNorth, outside: Cell{X: 2, Y: 0}}

	route, ok, err := routeOpeningPair(from, to, CorridorOrderXThenY, search, nil)

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []Cell{{X: 1, Y: 0}, {X: 2, Y: 0}}, route)
}

func TestOpeningPairLowerBoundCountsExternalCells(t *testing.T) {
	tests := []struct {
		name string
		from doorOpening
		to   doorOpening
		want int64
	}{
		{
			name: "openings that face each other",
			from: doorOpening{at: Cell{X: 1, Y: 1}, direction: DirectionEast, outside: Cell{X: 2, Y: 1}},
			to:   doorOpening{at: Cell{X: 2, Y: 1}, direction: DirectionWest, outside: Cell{X: 1, Y: 1}},
			want: 0,
		},
		{
			name: "same external Cell",
			from: doorOpening{outside: Cell{X: 3, Y: 4}},
			to:   doorOpening{outside: Cell{X: 3, Y: 4}},
			want: 1,
		},
		{
			name: "Manhattan distance includes both endpoints",
			from: doorOpening{outside: Cell{X: 1, Y: 2}},
			to:   doorOpening{outside: Cell{X: 4, Y: 6}},
			want: 8,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, openingPairLowerBound(test.from, test.to))
		})
	}
}

func TestPruningKeepsPairThatCanStillTieOnCost(t *testing.T) {
	assert.True(t, openingPairCanBeatBest(7, 7))
	assert.False(t, openingPairCanBeatBest(8, 7))
}

func TestNonRectangularShapeDoorsLeaveBoundaryCells(t *testing.T) {
	tests := []struct {
		name   string
		shape  RoomShape
		width  uint32
		height uint32
	}{
		{name: "L", shape: RoomShapeL, width: 4, height: 4},
		{name: "Cross", shape: RoomShapeCross, width: 5, height: 5},
		{name: "Circle", shape: RoomShapeCircle, width: 5, height: 5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			room := placedShape(t, 0, test.shape, Cell{X: 2, Y: 2}, test.width, test.height)
			target := placedRectangle(t, 1, Cell{X: 10, Y: 10}, 1, 1)
			occupancy := newPlacementOccupancy(12, 12)
			occupancy.mark(0, room.Cells)
			occupancy.mark(1, target.Cells)

			openings := enumerateDoorOpenings(room, 0, occupancy)

			require.NotEmpty(t, openings)
			for _, opening := range openings {
				assert.Contains(t, room.Cells, opening.at)
				owner, occupied := occupancy.ownerAt(opening.outside)
				assert.False(t, occupied && owner == 0)
			}

			corridors, doors, err := routeCorridors(
				context.Background(), 12, 12, CorridorOrderXThenY,
				[]PlacedRoom{room, target}, []Connection{{FromRoomID: 0, ToRoomID: 1}},
			)
			require.NoError(t, err)
			require.Len(t, corridors, 1)
			selected := doors[corridors[0].FromDoorID]
			assert.Contains(t, room.Cells, selected.At)
			delta := selected.Direction.Delta()
			outside := Cell{X: selected.At.X + delta.X, Y: selected.At.Y + delta.Y}
			owner, occupied := occupancy.ownerAt(outside)
			assert.False(t, occupied && owner == 0)
		})
	}
}

// TestCorridorKeepsExternalCellsAndFourConnectedSequence checks that between
// multi-Cell Rooms the Corridor stays outside every footprint, each step is
// orthogonal, and the endpoint Doors sit on the two Rooms.
func TestCorridorKeepsExternalCellsAndFourConnectedSequence(t *testing.T) {
	rooms := []PlacedRoom{
		placedRectangle(t, 0, Cell{X: 1, Y: 1}, 2, 2),
		placedRectangle(t, 1, Cell{X: 8, Y: 6}, 2, 2),
		placedRectangle(t, 2, Cell{X: 5, Y: 3}, 2, 2),
	}
	occupancy := newPlacementOccupancy(12, 10)
	for roomIndex, room := range rooms {
		occupancy.mark(uint32(roomIndex), room.Cells)
	}

	corridors, doors, err := routeCorridors(
		context.Background(), 12, 10, CorridorOrderXThenY, rooms,
		[]Connection{{FromRoomID: 0, ToRoomID: 1}},
	)

	require.NoError(t, err)
	require.Len(t, corridors, 1)
	corridor := corridors[0]
	assert.NotEqual(t, corridor.FromRoomID, corridor.ToRoomID)
	require.NotEmpty(t, corridor.Cells)
	for index, cell := range corridor.Cells {
		_, occupied := occupancy.ownerAt(cell)
		assert.False(t, occupied, "Cell %v belongs to a footprint", cell)
		if index == 0 {
			continue
		}
		previous := corridor.Cells[index-1]
		distance := absInt64(int64(cell.X)-int64(previous.X)) + absInt64(int64(cell.Y)-int64(previous.Y))
		assert.Equal(t, int64(1), distance)
	}
	fromDoor := doors[corridor.FromDoorID]
	toDoor := doors[corridor.ToDoorID]
	assert.Contains(t, rooms[0].Cells, fromDoor.At)
	assert.Contains(t, rooms[1].Cells, toDoor.At)
}

func TestBFSRepeatsSameRouteOnEveryRun(t *testing.T) {
	rooms := []PlacedRoom{
		placedRectangle(t, 0, Cell{X: 1, Y: 3}, 1, 1),
		placedRectangle(t, 1, Cell{X: 5, Y: 3}, 1, 1),
		placedRectangle(t, 2, Cell{X: 3, Y: 3}, 1, 1),
	}
	occupancy := newPlacementOccupancy(7, 7)
	for roomIndex, room := range rooms {
		occupancy.mark(uint32(roomIndex), room.Cells)
	}
	search := newRoutingSearch(context.Background(), occupancy)
	from := doorOpening{roomID: 0, at: rooms[0].At, direction: DirectionEast, outside: Cell{X: 2, Y: 3}}
	to := doorOpening{roomID: 1, at: rooms[1].At, direction: DirectionWest, outside: Cell{X: 4, Y: 3}}
	want := []Cell{{X: 2, Y: 3}, {X: 2, Y: 2}, {X: 3, Y: 2}, {X: 4, Y: 2}, {X: 4, Y: 3}}

	for execution := 0; execution < 32; execution++ {
		route, ok, err := routeOpeningPair(from, to, CorridorOrderXThenY, search, nil)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, want, route)
	}
}

func TestCanceledRoutingReturnsNoPartialResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rooms := []PlacedRoom{
		placedRectangle(t, 0, Cell{X: 1, Y: 1}, 1, 1),
		placedRectangle(t, 1, Cell{X: 3, Y: 1}, 1, 1),
	}

	corridors, doors, err := routeCorridors(
		ctx, 5, 3, CorridorOrderXThenY, rooms,
		[]Connection{{FromRoomID: 0, ToRoomID: 1}},
	)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, corridors)
	assert.Nil(t, doors)
}

func placedRectangle(t testing.TB, id RoomID, origin Cell, width, height uint32) PlacedRoom {
	t.Helper()
	return placedShape(t, id, RoomShapeRectangle, origin, width, height)
}

func placedShape(t testing.TB, id RoomID, shape RoomShape, origin Cell, width, height uint32) PlacedRoom {
	t.Helper()
	placement := RoomPlacement{
		Shape:  shape,
		Origin: origin,
		Width:  width,
		Height: height,
		Cells:  RoomShapeOffsets(shape, width, height),
	}
	footprint, ok := absoluteFootprint(placement)
	require.True(t, ok)
	return PlacedRoom{
		ID: id, At: footprint[0], Shape: placement.Shape, Origin: origin,
		Width: width, Height: height, Cells: footprint,
	}
}

// assertContactOnlyBesideRooms fails if two Corridors share a Cell or touch
// away from a Room wall. It returns whether any legal wall contact occurred.
func assertContactOnlyBesideRooms(t *testing.T, rooms []PlacedRoom, corridors []Corridor) bool {
	t.Helper()
	besideRoom := cellsBesidePlacedRooms(rooms)
	owners := corridorCellOwners(t, corridors)
	touched := false
	for _, corridor := range corridors {
		for _, cell := range corridor.Cells {
			for dy := int32(-1); dy <= 1; dy++ {
				for dx := int32(-1); dx <= 1; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					neighbor := Cell{X: cell.X + dx, Y: cell.Y + dy}
					other, exists := owners[neighbor]
					if !exists || other == corridor.ID {
						continue
					}
					touched = true
					if !besideRoom[cell] || !besideRoom[neighbor] {
						t.Fatalf("Corridor %d at %v touches Corridor %d at %v away from a Room wall",
							corridor.ID, cell, other, neighbor)
					}
				}
			}
		}
	}
	return touched
}

func corridorCellOwners(t *testing.T, corridors []Corridor) map[Cell]CorridorID {
	t.Helper()
	owners := make(map[Cell]CorridorID)
	for _, corridor := range corridors {
		for _, cell := range corridor.Cells {
			if previous, exists := owners[cell]; exists {
				t.Fatalf("Cell %v belongs to Corridor %d and Corridor %d", cell, previous, corridor.ID)
			}
			owners[cell] = corridor.ID
		}
	}
	return owners
}

func cellsBesidePlacedRooms(rooms []PlacedRoom) map[Cell]bool {
	footprints := make(map[Cell]struct{})
	for _, room := range rooms {
		for _, cell := range room.Cells {
			footprints[cell] = struct{}{}
		}
	}
	beside := make(map[Cell]bool)
	for cell := range footprints {
		for direction := DirectionNorth; direction < Direction(routingDirectionCount); direction++ {
			delta := direction.Delta()
			neighbor := Cell{X: cell.X + delta.X, Y: cell.Y + delta.Y}
			if _, isFootprint := footprints[neighbor]; isFootprint {
				continue
			}
			beside[neighbor] = true
		}
	}
	return beside
}

func assertCorridorChebyshevSeparation(t *testing.T, corridors []Corridor) {
	t.Helper()
	owners := make(map[Cell]CorridorID, len(corridors))
	for _, corridor := range corridors {
		for _, cell := range corridor.Cells {
			if previous, exists := owners[cell]; exists {
				t.Fatalf("Cell %v belongs to Corridor %d and Corridor %d", cell, previous, corridor.ID)
			}
			owners[cell] = corridor.ID
		}
	}
	for _, corridor := range corridors {
		for _, cell := range corridor.Cells {
			for dy := int32(-1); dy <= 1; dy++ {
				for dx := int32(-1); dx <= 1; dx++ {
					neighbor := Cell{X: cell.X + dx, Y: cell.Y + dy}
					other, exists := owners[neighbor]
					if exists && other != corridor.ID {
						t.Fatalf("Corridor %d at %v touches Corridor %d at %v", corridor.ID, cell, other, neighbor)
					}
				}
			}
		}
	}
}

func BenchmarkRouting256Rooms(b *testing.B) {
	config := Config{
		Width: 256, Height: 256, Seed: 17,
		MinDistance: 6, MaxAttempts: 30, MaxRooms: 256,
	}
	layout, err := (Generator{}).Generate(config)
	require.NoError(b, err)
	require.Len(b, layout.Rooms, 256)

	rooms := make([]PlacedRoom, len(layout.Rooms))
	for roomIndex, room := range layout.Rooms {
		rooms[roomIndex] = PlacedRoom{
			ID: room.ID, At: room.At, Shape: room.Shape, Origin: room.Origin,
			Width: room.Width, Height: room.Height, Cells: room.Cells,
		}
	}
	connections := make([]Connection, 0, len(layout.Corridors))
	for _, corridor := range layout.Corridors {
		connections = append(connections, Connection{
			FromRoomID: corridor.FromRoomID, ToRoomID: corridor.ToRoomID,
		})
	}

	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		corridors, doors, routeErr := routeCorridors(
			context.Background(), config.Width, config.Height, config.CorridorOrder, rooms, connections,
		)
		if routeErr != nil || len(corridors) != len(connections) || len(doors) == 0 {
			b.Fatalf("invalid routing of 256 Rooms: corridors=%d doors=%d err=%v", len(corridors), len(doors), routeErr)
		}
	}
}
