package daedalus

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoteamentoReproduzExemploDoApêndiceA(t *testing.T) {
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

func TestRoteamentoEscolheParDeDoorsComMenorCorredor(t *testing.T) {
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
	assert.Equal(t, Door{ID: 0, RoomID: 0, At: Cell{X: 1, Y: 2}, Direction: DirectionEast, CorridorIDs: []CorridorID{0}}, doors[0])
	assert.Equal(t, Door{ID: 1, RoomID: 1, At: Cell{X: 5, Y: 2}, Direction: DirectionWest, CorridorIDs: []CorridorID{0}}, doors[1])
}

func TestBFSDeterministicaDesviaDeRoomQueBloqueiaAsDuasRotasEmL(t *testing.T) {
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

func TestCorridorOrderMudaCotoveloQuandoAmbasRotasEmLSãoVálidas(t *testing.T) {
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

func TestArestaSemRotaDescartaResultadoParcial(t *testing.T) {
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

func TestDuasArestasReutilizamDoorEOrdenamCorridorIDs(t *testing.T) {
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
	require.Len(t, doors, 2)
	assert.Equal(t, corridors[0].FromDoorID, corridors[1].FromDoorID)
	assert.Equal(t, corridors[0].ToDoorID, corridors[1].ToDoorID)
	assert.Equal(t, []CorridorID{0, 1}, doors[corridors[0].FromDoorID].CorridorIDs)
	assert.Equal(t, []CorridorID{0, 1}, doors[corridors[0].ToDoorID].CorridorIDs)
}

func TestCorridorsPodemCompartilharCell(t *testing.T) {
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
	assert.Contains(t, corridors[0].Cells, Cell{X: 3, Y: 3})
	assert.Contains(t, corridors[1].Cells, Cell{X: 3, Y: 3})
}

func TestRoomsAdjacentesQueSeEncaramProduzemCorridorVazio(t *testing.T) {
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

func TestRoomsAdjacentesQueNãoSeEncaramNãoProduzemCorridorVazio(t *testing.T) {
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

func TestDoorsDeFormasNãoRetangularesSaemDeCellsDeBorda(t *testing.T) {
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

func TestCorridorMantémCellsExternasESequênciaQuatroConexa(t *testing.T) {
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
		assert.False(t, occupied, "Cell %v pertence a footprint", cell)
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

func TestBFSRepeteMesmaRotaEmTodasAsExecuções(t *testing.T) {
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

func TestRoteamentoCanceladoNãoDevolveResultadoParcial(t *testing.T) {
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

func BenchmarkRoteamento256Rooms(b *testing.B) {
	const (
		roomsPerAxis = 16
		roomSpacing  = 16
		gridSize     = 256
	)
	rooms := make([]PlacedRoom, 0, roomsPerAxis*roomsPerAxis)
	for row := 0; row < roomsPerAxis; row++ {
		for columnOffset := 0; columnOffset < roomsPerAxis; columnOffset++ {
			column := columnOffset
			if row%2 != 0 {
				column = roomsPerAxis - 1 - columnOffset
			}
			origin := Cell{X: int32(1 + column*roomSpacing), Y: int32(1 + row*roomSpacing)}
			rooms = append(rooms, placedRectangle(b, RoomID(len(rooms)), origin, 1, 1))
		}
	}
	connections := make([]Connection, 0, len(rooms)-1)
	for roomIndex := 1; roomIndex < len(rooms); roomIndex++ {
		connections = append(connections, Connection{
			FromRoomID: RoomID(roomIndex - 1), ToRoomID: RoomID(roomIndex),
		})
	}

	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		corridors, doors, err := routeCorridors(
			context.Background(), gridSize, gridSize, CorridorOrderXThenY, rooms, connections,
		)
		if err != nil || len(corridors) != len(connections) || len(doors) == 0 {
			b.Fatalf("roteamento 256 Rooms inválido: corridors=%d doors=%d err=%v", len(corridors), len(doors), err)
		}
	}
}
