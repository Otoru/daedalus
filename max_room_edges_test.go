package daedalus

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaxRoomEdgesBelowTwoIsInvalidConfig(t *testing.T) {
	_, err := normalizeConfig(Config{Width: 8, Height: 8, MaxRoomEdges: 1})

	require.ErrorIs(t, err, ErrInvalidConfig)
	assert.NotErrorIs(t, err, ErrLimitExceeded)
	assert.ErrorContains(t, err, "two Rooms")
	assert.ErrorContains(t, err, "zero means unlimited")

	layout, generateErr := Generator{}.Generate(Config{Width: 8, Height: 8, Seed: 1, MaxRoomEdges: 1})
	require.ErrorIs(t, generateErr, ErrInvalidConfig)
	assert.Equal(t, Layout{}, layout)
}

func TestMaxRoomEdgesZeroIsUnlimitedAndTwoIsStored(t *testing.T) {
	zero, err := normalizeConfig(Config{Width: 8, Height: 8})
	require.NoError(t, err)
	assert.Zero(t, zero.maxRoomEdges)

	explicit, err := normalizeConfig(Config{Width: 8, Height: 8, MaxRoomEdges: 0})
	require.NoError(t, err)
	assert.Zero(t, explicit.maxRoomEdges)

	two, err := normalizeConfig(Config{Width: 8, Height: 8, MaxRoomEdges: 2})
	require.NoError(t, err)
	assert.Equal(t, uint32(2), two.maxRoomEdges)
}

func TestMaxRoomEdgesZeroMatchesTheOmittedField(t *testing.T) {
	base := Config{Width: 32, Height: 24, Seed: 9, MaxRooms: 10, ExtraEdgeCount: 2}
	withZero := base
	withZero.MaxRoomEdges = 0

	omitted, omittedErr := Generator{}.Generate(base)
	explicit, explicitErr := Generator{}.Generate(withZero)

	require.NoError(t, omittedErr)
	require.NoError(t, explicitErr)
	assert.Equal(t, omitted, explicit)
}

func TestMaxRoomEdgesLowersDegreeAcrossSeedsAndGrids(t *testing.T) {
	floors := []struct {
		width    uint32
		height   uint32
		maxRooms uint32
		seeds    []Seed
	}{
		{width: 40, height: 32, maxRooms: 12, seeds: []Seed{1, 7, 19}},
		{width: 48, height: 40, maxRooms: 16, seeds: []Seed{3, 11}},
		{width: 64, height: 48, maxRooms: 18, seeds: []Seed{5, 13}},
	}
	const ceiling uint32 = 3
	sawARoomAboveTheCeiling := false
	for _, floor := range floors {
		for _, seed := range floor.seeds {
			config := Config{
				Width: floor.width, Height: floor.height, Seed: seed,
				MaxRooms: floor.maxRooms, ExtraEdgeCount: 6,
			}
			uncapped, err := Generator{}.Generate(config)
			require.NoError(t, err, "seed %d %dx%d", seed, floor.width, floor.height)
			if maxCorridorDegree(uncapped) > int(ceiling) {
				sawARoomAboveTheCeiling = true
			}

			config.MaxRoomEdges = ceiling
			capped, err := Generator{}.Generate(config)
			require.NoError(t, err, "capped seed %d %dx%d", seed, floor.width, floor.height)
			assertRoomDegreesAtMost(t, capped, int(ceiling))
		}
	}
	assert.True(t, sawARoomAboveTheCeiling, "the ceiling never bound a Room, so the check did not prove a lower degree")
}

func TestMaxRoomEdgesDoesNotOverrideATighterPerimeter(t *testing.T) {
	config := Config{
		Width: 36, Height: 36, Seed: 4, MaxRooms: 8, MinDistance: 5,
		ExtraEdgeCount: 4, MaxRoomEdges: 6,
		RoomGeometry: &RoomGeometry{
			MaxFootprintCells: 1, MinRoomGap: 1,
			Shapes: []RoomShapeWeight{shapeSpan(RoomShapeRectangle, 1, 1, 1, 1, 1)},
		},
	}
	for _, seed := range []Seed{4, 15, 42} {
		config.Seed = seed
		layout, err := Generator{}.Generate(config)
		require.NoError(t, err, "seed %d", seed)
		require.GreaterOrEqual(t, len(layout.Rooms), 3, "seed %d", seed)
		width := narrowestDeclaredCorridorWidth(nil)
		for _, room := range layout.Rooms {
			geometric := roomOpeningCapacity(room.Cells, layout.Grid.Width, layout.Grid.Height, width)
			assert.Less(t, geometric, int(config.MaxRoomEdges), "seed %d room %d perimeter is the tighter ceiling", seed, room.ID)
			assert.LessOrEqual(t, corridorDegree(layout, room.ID), geometric, "seed %d room %d", seed, room.ID)
		}
	}
}

func TestMaxRoomEdgesOfTwoGeneratesAChain(t *testing.T) {
	layout, err := Generator{}.Generate(Config{
		Width: 28, Height: 24, Seed: 7, MaxRooms: 6, ExtraEdgeCount: 0, MaxRoomEdges: 2,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(layout.Rooms), 3)
	assert.Equal(t, len(layout.Rooms)-1, len(layout.Corridors))
	endpoints := 0
	for _, room := range layout.Rooms {
		degree := corridorDegree(layout, room.ID)
		assert.LessOrEqual(t, degree, 2)
		if degree == 1 {
			endpoints++
		} else {
			assert.Equal(t, 2, degree)
		}
	}
	assert.Equal(t, 2, endpoints, "a chain has two ends")
}

func TestPrimHonorsACallerCeilingBelowThePerimeter(t *testing.T) {
	rooms := starPlacedRooms()
	uncapped, err := (primRoomsConnector{}).Connect(ConnectionRequest{
		Context: context.Background(), Rooms: rooms, Width: 80, Height: 80,
	})
	require.NoError(t, err)
	assert.Greater(t, maxConnectionDegree(rooms, uncapped), 2)

	capped, err := (primRoomsConnector{}).Connect(ConnectionRequest{
		Context: context.Background(), Rooms: rooms, Width: 80, Height: 80, MaxRoomEdges: 2,
	})
	require.NoError(t, err)
	assert.Equal(t, len(rooms)-1, len(capped))
	assert.LessOrEqual(t, maxConnectionDegree(rooms, capped), 2)
}

func TestPrimKeepsGeometryWhenTheCallerCeilingIsWider(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 10, 10),
		placedRoomAt(1, 10, 0),
		placedRoomAt(2, 10, 20),
		placedRoomAt(3, 0, 10),
		placedRoomAt(4, 20, 10),
	}
	edges, err := (primRoomsConnector{}).Connect(ConnectionRequest{
		Context: context.Background(), Rooms: rooms, Width: 40, Height: 40, MaxRoomEdges: 6,
	})
	require.NoError(t, err)
	assert.Equal(t, len(rooms)-1, len(edges))
	assert.LessOrEqual(t, maxConnectionDegree(rooms, edges), 2, "a 1×1 Room hosts two openings, and a ceiling of six must not raise that")
}

func TestExtraEdgePhaseHonorsACallerCeiling(t *testing.T) {
	rooms := []PlacedRoom{
		rectanglePlacedRoom(0, 0, 0, 5, 5),
		rectanglePlacedRoom(1, 12, 0, 5, 5),
		rectanglePlacedRoom(2, 24, 0, 5, 5),
		rectanglePlacedRoom(3, 36, 0, 5, 5),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 1, ToRoomID: 2},
		{FromRoomID: 2, ToRoomID: 3},
	}
	uncapped, err := addExtraConnections(context.Background(), rooms, backbone, 10, 80, 80, 1, 0, nil)
	require.NoError(t, err)
	assert.Greater(t, maxConnectionDegree(rooms, uncapped), 2)

	capped, err := addExtraConnections(context.Background(), rooms, backbone, 10, 80, 80, 1, 2, nil)
	require.NoError(t, err)
	assert.LessOrEqual(t, maxConnectionDegree(rooms, capped), 2)
	assert.Less(t, len(capped), len(uncapped))
}

func starPlacedRooms() []PlacedRoom {
	return []PlacedRoom{
		rectanglePlacedRoom(0, 30, 30, 5, 5),
		rectanglePlacedRoom(1, 30, 8, 5, 5),
		rectanglePlacedRoom(2, 30, 52, 5, 5),
		rectanglePlacedRoom(3, 8, 30, 5, 5),
		rectanglePlacedRoom(4, 52, 30, 5, 5),
	}
}

func corridorDegree(layout Layout, id RoomID) int {
	degree := 0
	for _, corridor := range layout.Corridors {
		if corridor.FromRoomID == id || corridor.ToRoomID == id {
			degree++
		}
	}
	return degree
}

func maxCorridorDegree(layout Layout) int {
	maxDegree := 0
	for _, room := range layout.Rooms {
		if degree := corridorDegree(layout, room.ID); degree > maxDegree {
			maxDegree = degree
		}
	}
	return maxDegree
}

func assertRoomDegreesAtMost(t *testing.T, layout Layout, ceiling int) {
	t.Helper()
	for _, room := range layout.Rooms {
		assert.LessOrEqual(t, corridorDegree(layout, room.ID), ceiling, "room %d", room.ID)
	}
}

func maxConnectionDegree(rooms []PlacedRoom, edges []Connection) int {
	degree := make([]int, len(rooms))
	indexByID := make(map[RoomID]int, len(rooms))
	for index, room := range rooms {
		indexByID[room.ID] = index
	}
	for _, edge := range edges {
		degree[indexByID[edge.FromRoomID]]++
		degree[indexByID[edge.ToRoomID]]++
	}
	maxDegree := 0
	for _, count := range degree {
		if count > maxDegree {
			maxDegree = count
		}
	}
	return maxDegree
}
