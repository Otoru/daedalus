package daedalus

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewLoopConnectorValidatesTarget(t *testing.T) {
	for _, target := range []uint32{1, 2, MaxRooms + 1} {
		_, err := NewLoopConnector(LoopConnectorConfig{TargetRingRooms: target})
		require.ErrorIs(t, err, ErrInvalidConfig)
	}
	connector, err := NewLoopConnector(LoopConnectorConfig{TargetRingRooms: 6})
	require.NoError(t, err)
	require.NotNil(t, connector)
}

func TestLoopRingAutomaticTargetAndCanonicalOrdering(t *testing.T) {
	require.Equal(t, 6, loopAutomaticTarget(16))
	require.Equal(t, 8, loopAutomaticTarget(64))
	require.Equal(t, 12, loopAutomaticTarget(144))
	require.Equal(t, 12, loopAutomaticTarget(256))

	rooms := loopTestRooms(6)
	ring, err := selectLoopRing(context.Background(), ConnectionRequest{Rooms: rooms, Width: 64, Height: 64, MaxCorridorWidth: 1}, 6, nil)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(ring.Vertices), 3)
	require.LessOrEqual(t, len(ring.Vertices), 6)
	containsRoot := false
	for _, room := range ring.Vertices {
		containsRoot = containsRoot || room.ID == 0
	}
	require.True(t, containsRoot)
	require.Len(t, ring.Edges, len(ring.Vertices))
	for _, edge := range ring.Edges {
		require.NotEqual(t, edge.FromRoomID, edge.ToRoomID)
	}
}

func TestLoopRingRouterProbeRejectsCandidateBeforeCommit(t *testing.T) {
	rooms := loopTestRooms(16)
	probes := 0
	ring, err := selectLoopRing(context.Background(), ConnectionRequest{Rooms: rooms, Width: 64, Height: 64, MaxCorridorWidth: 1}, 6, func(edges []Connection) bool {
		probes++
		return probes > 1
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, probes, 2)
	require.NotEmpty(t, ring.Edges)
}

func TestLoopRingRouterProbeCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := selectLoopRing(ctx, ConnectionRequest{Rooms: loopTestRooms(8), Width: 64, Height: 64, MaxCorridorWidth: 1}, 6, nil)
	require.ErrorIs(t, err, context.Canceled)
}

func TestLoopConnectorRequiresCycleBudget(t *testing.T) {
	connector, err := NewLoopConnector(LoopConnectorConfig{})
	require.NoError(t, err)
	_, err = connector.Connect(ConnectionRequest{Rooms: loopTestRooms(3), ExtraEdgeCount: 0})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestLoopConnectorDegenerateRoomsDoNotConsumeCycleBudget(t *testing.T) {
	connector, err := NewLoopConnector(LoopConnectorConfig{})
	require.NoError(t, err)
	for _, count := range []int{0, 1, 2} {
		edges, connectErr := connector.Connect(ConnectionRequest{Rooms: loopTestRooms(count), ExtraEdgeCount: 0})
		require.NoError(t, connectErr)
		require.Len(t, edges, loopMaxInt(0, count-1))
	}
}

func TestLoopConnectorGeneratesReachableLayoutThroughGenerator(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		target uint32
		config Config
	}{
		{name: "automatic", target: 0, config: Config{Width: 48, Height: 40, Seed: 0, MinDistance: 6, MaxRooms: 28, ExtraEdgeCount: 1}},
		{name: "explicit", target: 12, config: Config{Width: 96, Height: 96, Seed: 0, MinDistance: 7, MaxRooms: 128, ExtraEdgeCount: 1}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			connector, err := NewLoopConnector(LoopConnectorConfig{TargetRingRooms: testCase.target})
			require.NoError(t, err)
			layout, err := (Generator{Connector: connector}).Generate(testCase.config)
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(layout.Rooms), 3)
			require.Equal(t, len(layout.Rooms), len(layout.Corridors), "one ring cycle plus branches must have E=V")
			assertLayoutGraphConnected(t, layout)
		})
	}
}

func assertLayoutGraphConnected(t *testing.T, layout Layout) {
	t.Helper()
	visited := make([]bool, len(layout.Rooms))
	visited[0] = true
	for changed := true; changed; {
		changed = false
		for _, corridor := range layout.Corridors {
			from := int(corridor.FromRoomID)
			to := int(corridor.ToRoomID)
			if visited[from] && !visited[to] {
				visited[to] = true
				changed = true
			}
			if visited[to] && !visited[from] {
				visited[from] = true
				changed = true
			}
		}
	}
	for roomIndex, reachable := range visited {
		require.True(t, reachable, "Room %d is unreachable", roomIndex)
	}
}

func loopTestRooms(count int) []PlacedRoom {
	rooms := make([]PlacedRoom, count)
	for index := range rooms {
		column := index % 4
		row := index / 4
		rooms[index] = PlacedRoom{ID: RoomID(index), Origin: Cell{X: int32(column * 10), Y: int32(row * 10)}, Width: 9, Height: 9}
	}
	return rooms
}

func loopMaxInt(first, second int) int {
	if first > second {
		return first
	}
	return second
}
