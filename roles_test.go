package daedalus

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmptyRolesAssignAndConsumeNoDraw checks the role phase with a nil
// request: no Role is assigned and the RNG streams are left unchanged.
func TestEmptyRolesAssignAndConsumeNoDraw(t *testing.T) {
	rooms := []PlacedRoom{placedRoomAt(0, 0, 0), placedRoomAt(1, 3, 0)}
	backbone := []Connection{{FromRoomID: 0, ToRoomID: 1}}
	streams := newRNGStreams(Seed(91))
	wantStreams := streams

	roles, err := assignRoomRoles(context.Background(), rooms, backbone, nil)

	require.NoError(t, err)
	require.Len(t, roles, len(rooms))
	assert.Nil(t, roles[0])
	assert.Nil(t, roles[1])
	assert.Equal(t, wantStreams, streams)
}

func TestStartAlwaysReceivesRoomIDZero(t *testing.T) {
	rooms := []PlacedRoom{placedRoomAt(0, 7, 7), placedRoomAt(1, 0, 0)}
	requests := []RoomRoleRequest{{Role: RoomRoleStart, Count: 1}}

	roles, err := assignRoomRoles(context.Background(), rooms, []Connection{{FromRoomID: 0, ToRoomID: 1}}, requests)

	require.NoError(t, err)
	require.NotNil(t, roles[0])
	assert.Equal(t, RoomRoleStart, *roles[0])
	assert.Nil(t, roles[1])
}

func TestBossUsesWeightedTreePathSum(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 10, 0),
		placedRoomAt(2, 1, 0),
		placedRoomAt(3, 20, 0),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 3},
		{FromRoomID: 3, ToRoomID: 2},
	}
	requests := []RoomRoleRequest{
		{Role: RoomRoleStart, Count: 1},
		{Role: RoomRoleBoss, Count: 1},
	}

	roles, err := assignRoomRoles(context.Background(), rooms, backbone, requests)

	require.NoError(t, err)
	require.NotNil(t, roles[2])
	assert.Equal(t, RoomRoleBoss, *roles[2], "Room 1 is farther in a straight line, but Room 2 has the greater weighted path")
}

func TestRolesUseBoundingBoxCenters(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		{
			ID: 1, At: Cell{X: 1, Y: 0}, Shape: RoomShapeRectangle,
			Origin: Cell{X: 1, Y: 0}, Width: 19, Height: 1,
		},
		placedRoomAt(2, 9, 0),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 2},
	}
	requests := []RoomRoleRequest{
		{Role: RoomRoleStart, Count: 1},
		{Role: RoomRoleBoss, Count: 1},
	}

	roles, err := assignRoomRoles(context.Background(), rooms, backbone, requests)

	require.NoError(t, err)
	assertRoleAt(t, roles, 1, RoomRoleBoss)
	assert.Nil(t, roles[2], "the anchor metric would choose Room 2")
}

func TestRolesBreakDistanceTiesByAscendingRoomID(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, -2, 0),
		placedRoomAt(2, 2, 0),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 2},
	}
	requests := []RoomRoleRequest{
		{Role: RoomRoleStart, Count: 1},
		{Role: RoomRoleBoss, Count: 1},
	}

	roles, err := assignRoomRoles(context.Background(), rooms, backbone, requests)

	require.NoError(t, err)
	require.NotNil(t, roles[1])
	assert.Equal(t, RoomRoleBoss, *roles[1])
	assert.Nil(t, roles[2])
}

func TestTreasureChoosesNextFarthestWithoutRepeating(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 1, 0),
		placedRoomAt(2, 2, 0),
		placedRoomAt(3, 3, 0),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 1, ToRoomID: 2},
		{FromRoomID: 2, ToRoomID: 3},
	}
	requests := []RoomRoleRequest{
		{Role: RoomRoleStart, Count: 1},
		{Role: RoomRoleBoss, Count: 1},
		{Role: RoomRoleTreasure, Count: 2},
	}

	roles, err := assignRoomRoles(context.Background(), rooms, backbone, requests)

	require.NoError(t, err)
	assertRoleAt(t, roles, 0, RoomRoleStart)
	assertRoleAt(t, roles, 1, RoomRoleTreasure)
	assertRoleAt(t, roles, 2, RoomRoleTreasure)
	assertRoleAt(t, roles, 3, RoomRoleBoss)
}

func TestTreasureAndBossRespectRequestOrder(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 1, 0),
		placedRoomAt(2, 2, 0),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 1, ToRoomID: 2},
	}
	requests := []RoomRoleRequest{
		{Role: RoomRoleStart, Count: 1},
		{Role: RoomRoleTreasure, Count: 1},
		{Role: RoomRoleBoss, Count: 1},
	}

	roles, err := assignRoomRoles(context.Background(), rooms, backbone, requests)

	require.NoError(t, err)
	assertRoleAt(t, roles, 1, RoomRoleBoss)
	assertRoleAt(t, roles, 2, RoomRoleTreasure)
}

// TestBossDoesNotReuseATreasureRoom checks that a Room already holding a role
// stays taken. Treasure is requested first, so it claims the farthest Rooms
// from Start. Boss then runs and must leave those Rooms alone. Dropping the
// occupied-Room check would overwrite Room 9 with Boss.
func TestBossDoesNotReuseATreasureRoom(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 1, 0),
		placedRoomAt(2, 2, 0),
		placedRoomAt(3, 3, 0),
		placedRoomAt(4, 4, 0),
		placedRoomAt(5, 5, 0),
		placedRoomAt(6, 2, 1),
		placedRoomAt(7, 2, 2),
		placedRoomAt(8, 2, 3),
		placedRoomAt(9, 2, 4),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 1, ToRoomID: 2},
		{FromRoomID: 2, ToRoomID: 3},
		{FromRoomID: 3, ToRoomID: 4},
		{FromRoomID: 4, ToRoomID: 5},
		{FromRoomID: 2, ToRoomID: 6},
		{FromRoomID: 6, ToRoomID: 7},
		{FromRoomID: 7, ToRoomID: 8},
		{FromRoomID: 8, ToRoomID: 9},
	}
	requests := []RoomRoleRequest{
		{Role: RoomRoleStart, Count: 1},
		{Role: RoomRoleTreasure, Count: 3},
		{Role: RoomRoleBoss, Count: 1},
	}

	roles, err := assignRoomRoles(context.Background(), rooms, backbone, requests)

	require.NoError(t, err)
	assertRoleAt(t, roles, 0, RoomRoleStart)
	assertRoleAt(t, roles, 9, RoomRoleTreasure)
	assertRoleAt(t, roles, 5, RoomRoleTreasure)
	assertRoleAt(t, roles, 6, RoomRoleTreasure)
	assertRoleAt(t, roles, 8, RoomRoleBoss)
	assert.Equal(t, 3, countRole(roles, RoomRoleTreasure))
	assert.Equal(t, 1, countRole(roles, RoomRoleBoss))
}

// TestTreasureSpreadMaximizesDistanceFromAnchors checks farthest-point
// sampling. Start and Boss are the first anchors. Each Treasure is the
// unassigned Room whose minimum weighted path to any anchor is greatest, and
// that Room then becomes an anchor. On this tree the repeated farthest-from-Start
// rule would sit a Treasure on Room 8, adjacent to the Boss; dispersion does not.
func TestTreasureSpreadMaximizesDistanceFromAnchors(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 1, 0),
		placedRoomAt(2, 2, 0),
		placedRoomAt(3, 3, 0),
		placedRoomAt(4, 4, 0),
		placedRoomAt(5, 5, 0),
		placedRoomAt(6, 2, 1),
		placedRoomAt(7, 2, 2),
		placedRoomAt(8, 2, 3),
		placedRoomAt(9, 2, 4),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 1, ToRoomID: 2},
		{FromRoomID: 2, ToRoomID: 3},
		{FromRoomID: 3, ToRoomID: 4},
		{FromRoomID: 4, ToRoomID: 5},
		{FromRoomID: 2, ToRoomID: 6},
		{FromRoomID: 6, ToRoomID: 7},
		{FromRoomID: 7, ToRoomID: 8},
		{FromRoomID: 8, ToRoomID: 9},
	}
	requests := []RoomRoleRequest{
		{Role: RoomRoleStart, Count: 1},
		{Role: RoomRoleBoss, Count: 1},
		{Role: RoomRoleTreasure, Count: 3},
	}

	roles, err := assignRoomRoles(context.Background(), rooms, backbone, requests)

	require.NoError(t, err)
	assertRoleAt(t, roles, 0, RoomRoleStart)
	assertRoleAt(t, roles, 9, RoomRoleBoss)
	assertRoleAt(t, roles, 5, RoomRoleTreasure)
	assertRoleAt(t, roles, 6, RoomRoleTreasure)
	assertRoleAt(t, roles, 3, RoomRoleTreasure)
	assertTreasureRoomsUnshared(t, roles)
	assertTreasuresNotAdjacent(t, rooms, backbone, roles)
}

// TestTreasureMinDistanceTieBreaksBySmallerRoomID checks the tie-break on a
// constructed tie. Rooms 2 and 3 have the same minimum weighted distance to
// the anchors. The smaller RoomID wins. Reversing that comparison selects Room 3.
func TestTreasureMinDistanceTieBreaksBySmallerRoomID(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 0, 5),
		placedRoomAt(2, 5, 0),
		placedRoomAt(3, -5, 0),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 2},
		{FromRoomID: 0, ToRoomID: 3},
	}
	requests := []RoomRoleRequest{
		{Role: RoomRoleStart, Count: 1},
		{Role: RoomRoleBoss, Count: 1},
		{Role: RoomRoleTreasure, Count: 1},
	}

	roles, err := assignRoomRoles(context.Background(), rooms, backbone, requests)

	require.NoError(t, err)
	assertRoleAt(t, roles, 0, RoomRoleStart)
	assertRoleAt(t, roles, 1, RoomRoleBoss)
	assertRoleAt(t, roles, 2, RoomRoleTreasure)
	assert.Nil(t, roles[3])
}

// TestTreasureWithoutAnchorsUsesRoomZeroThenDisperses checks Treasure requested
// alone. The anchor set starts empty, so the first Treasure is the unassigned
// Room farthest from RoomID 0. The next Treasure is then farthest from that one.
func TestTreasureWithoutAnchorsUsesRoomZeroThenDisperses(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 1, 0),
		placedRoomAt(2, 3, 0),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 1, ToRoomID: 2},
	}
	requests := []RoomRoleRequest{{Role: RoomRoleTreasure, Count: 2}}

	roles, err := assignRoomRoles(context.Background(), rooms, backbone, requests)

	require.NoError(t, err)
	assertRoleAt(t, roles, 2, RoomRoleTreasure)
	assertRoleAt(t, roles, 0, RoomRoleTreasure)
	assert.Nil(t, roles[1])
}

// TestExcessTreasureAssignsOnlyFreeRooms checks a Count past the number of
// Rooms. Assignment stops when every Room already holds a role, returns no
// error, and does not reuse a Room. The surplus is left unplaced.
func TestExcessTreasureAssignsOnlyFreeRooms(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 1, 0),
		placedRoomAt(2, 3, 0),
	}
	requests := []RoomRoleRequest{{Role: RoomRoleTreasure, Count: 5}}

	roles, err := assignRoomRoles(context.Background(), rooms, []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 1, ToRoomID: 2},
	}, requests)

	require.NoError(t, err)
	assertRoleAt(t, roles, 0, RoomRoleTreasure)
	assertRoleAt(t, roles, 1, RoomRoleTreasure)
	assertRoleAt(t, roles, 2, RoomRoleTreasure)
	assertTreasureRoomsUnshared(t, roles)
}

// TestTreasureRoomsStayApartOnAFloorWithRoom checks a generated floor whose
// tree has enough Rooms to keep Treasures off one another. Seed 0 on 64×48
// places two Treasures on the same backbone edge under farthest-from-Start.
func TestTreasureRoomsStayApartOnAFloorWithRoom(t *testing.T) {
	layout, err := (Generator{}).Generate(Config{
		Width: 64, Height: 48, Seed: 0, MaxRooms: 16, ExtraEdgeCount: 0,
		RoomRoleRequests: []RoomRoleRequest{
			{Role: RoomRoleStart, Count: 1},
			{Role: RoomRoleBoss, Count: 1},
			{Role: RoomRoleTreasure, Count: 3},
		},
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(layout.Rooms), 12, "this floor has to have room to separate Treasures")

	roles := make([]*RoomRole, len(layout.Rooms))
	rooms := make([]PlacedRoom, len(layout.Rooms))
	backbone := make([]Connection, len(layout.Corridors))
	for index, room := range layout.Rooms {
		roles[index] = room.Role
		rooms[index] = PlacedRoom{
			ID: room.ID, At: room.At, Shape: room.Shape,
			Origin: room.Origin, Width: room.Width, Height: room.Height,
		}
	}
	for index, corridor := range layout.Corridors {
		backbone[index] = Connection{FromRoomID: corridor.FromRoomID, ToRoomID: corridor.ToRoomID}
	}
	assertRoleAt(t, roles, 0, RoomRoleStart)
	assertTreasureRoomsUnshared(t, roles)
	assert.Equal(t, 3, countRole(roles, RoomRoleTreasure))
	assert.Equal(t, 1, countRole(roles, RoomRoleBoss))
	assertTreasuresNotAdjacent(t, rooms, backbone, roles)
}

func TestFewerRoomsThanRolesAssignsWhatFitsWithoutDuplicates(t *testing.T) {
	rooms := []PlacedRoom{placedRoomAt(0, 0, 0), placedRoomAt(1, 1, 0)}
	requests := []RoomRoleRequest{
		{Role: RoomRoleStart, Count: 1},
		{Role: RoomRoleBoss, Count: 1},
		{Role: RoomRoleTreasure, Count: 8},
	}

	roles, err := assignRoomRoles(context.Background(), rooms, []Connection{{FromRoomID: 0, ToRoomID: 1}}, requests)

	require.NoError(t, err)
	assertRoleAt(t, roles, 0, RoomRoleStart)
	assertRoleAt(t, roles, 1, RoomRoleBoss)
}

func TestBossWithoutStartIsProgrammingError(t *testing.T) {
	rooms := []PlacedRoom{placedRoomAt(0, 0, 0), placedRoomAt(1, 1, 0)}

	roles, err := assignRoomRoles(context.Background(), rooms, []Connection{{FromRoomID: 0, ToRoomID: 1}}, []RoomRoleRequest{{Role: RoomRoleBoss, Count: 1}})

	assert.ErrorIs(t, err, errBossRoleWithoutStart)
	assert.Nil(t, roles)
}

func TestZeroShortcutsPreservesTreeAndSkipsPhaseWithoutDraw(t *testing.T) {
	rooms := []PlacedRoom{placedRoomAt(0, 0, 0), placedRoomAt(1, 1, 0), placedRoomAt(2, 2, 0)}
	backbone := []Connection{{FromRoomID: 0, ToRoomID: 1}, {FromRoomID: 1, ToRoomID: 2}}
	streams := newRNGStreams(Seed(92))
	wantStreams := streams

	connections, err := addExtraConnections(context.Background(), extraConnectionsRequest{
		Rooms: rooms, Backbone: backbone, ExtraEdgeCount: 0,
		Constraints: roleGridConstraints{CorridorWidth: 1},
	})

	require.NoError(t, err)
	assert.Equal(t, backbone, connections)
	assertConnectionsFormTree(t, rooms, connections)
	assert.Equal(t, wantStreams, streams)
}

func TestShortcutsAreFirstOrderedDiscardedEdges(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 2, 0),
		placedRoomAt(2, 0, 2),
		placedRoomAt(3, 2, 2),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 2},
		{FromRoomID: 1, ToRoomID: 3},
	}

	connections, err := addExtraConnections(context.Background(), extraConnectionsRequest{
		Rooms: rooms, Backbone: backbone, ExtraEdgeCount: 2,
		Constraints: roleGridConstraints{CorridorWidth: 1},
	})

	require.NoError(t, err)
	assert.Equal(t, []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 2},
		{FromRoomID: 1, ToRoomID: 3},
		{FromRoomID: 2, ToRoomID: 3},
	}, connections)
	assertGraphConnected(t, rooms, connections)
}

func TestShortcutsAboveAvailableAddAllWithoutError(t *testing.T) {
	rooms := []PlacedRoom{placedRoomAt(0, 0, 0), placedRoomAt(1, 1, 0), placedRoomAt(2, 2, 0)}
	backbone := []Connection{{FromRoomID: 0, ToRoomID: 1}, {FromRoomID: 1, ToRoomID: 2}}

	connections, err := addExtraConnections(context.Background(), extraConnectionsRequest{
		Rooms: rooms, Backbone: backbone, ExtraEdgeCount: 99,
		Constraints: roleGridConstraints{CorridorWidth: 1},
	})

	require.NoError(t, err)
	assert.Equal(t, append(append([]Connection(nil), backbone...), Connection{FromRoomID: 0, ToRoomID: 2}), connections)
}

func TestRolesAreComputedBeforeShortcuts(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 1, 0),
		placedRoomAt(2, 2, 0),
		placedRoomAt(3, 0, 1),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 1, ToRoomID: 2},
		{FromRoomID: 2, ToRoomID: 3},
	}
	requests := []RoomRoleRequest{{Role: RoomRoleStart, Count: 1}, {Role: RoomRoleBoss, Count: 1}}

	roles, connections, err := applyTopologyOptions(context.Background(), topologyOptionsRequest{
		Rooms: rooms, Backbone: backbone, Requests: requests, ExtraEdgeCount: 1,
		Constraints: roleGridConstraints{CorridorWidth: 1},
	})

	require.NoError(t, err)
	assertRoleAt(t, roles, 3, RoomRoleBoss)
	assert.Contains(t, connections, Connection{FromRoomID: 0, ToRoomID: 3}, "the shortcut shortens the path to the Boss only after assignment")
}

func TestEnabledRolesAndShortcutsConsumeNoDraw(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 1, 0),
		placedRoomAt(2, 2, 0),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 1, ToRoomID: 2},
	}
	requests := []RoomRoleRequest{{Role: RoomRoleStart, Count: 1}, {Role: RoomRoleBoss, Count: 1}}
	streams := newRNGStreams(Seed(93))
	wantStreams := streams

	roles, connections, err := applyTopologyOptions(context.Background(), topologyOptionsRequest{
		Rooms: rooms, Backbone: backbone, Requests: requests, ExtraEdgeCount: 1,
		Constraints: roleGridConstraints{CorridorWidth: 1},
	})

	require.NoError(t, err)
	assertRoleAt(t, roles, 2, RoomRoleBoss)
	assert.Len(t, connections, 3)
	assert.Equal(t, wantStreams, streams)
}

func TestRoleAndShortcutPhasesRespectCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rooms := []PlacedRoom{placedRoomAt(0, 0, 0), placedRoomAt(1, 1, 0)}
	backbone := []Connection{{FromRoomID: 0, ToRoomID: 1}}

	roles, roleErr := assignRoomRoles(ctx, rooms, backbone, []RoomRoleRequest{{Role: RoomRoleStart, Count: 1}})
	connections, edgeErr := addExtraConnections(ctx, extraConnectionsRequest{
		Rooms: rooms, Backbone: backbone, ExtraEdgeCount: 1,
		Constraints: roleGridConstraints{CorridorWidth: 1},
	})

	assert.ErrorIs(t, roleErr, context.Canceled)
	assert.Nil(t, roles)
	assert.ErrorIs(t, edgeErr, context.Canceled)
	assert.Nil(t, connections)
}

func assertTreasureRoomsUnshared(t *testing.T, roles []*RoomRole) {
	t.Helper()
	start, boss := 0, 0
	for index, role := range roles {
		if role == nil {
			continue
		}
		switch *role {
		case RoomRoleStart:
			start++
		case RoomRoleBoss:
			boss++
		case RoomRoleTreasure:
		default:
			t.Fatalf("Room %d holds an unknown role %d", index, *role)
		}
	}
	assert.LessOrEqual(t, start, 1)
	assert.LessOrEqual(t, boss, 1)
}

func assertTreasuresNotAdjacent(t *testing.T, rooms []PlacedRoom, backbone []Connection, roles []*RoomRole) {
	t.Helper()
	treasure := make([]bool, len(rooms))
	for index, role := range roles {
		if role != nil && *role == RoomRoleTreasure {
			treasure[roomIndexByID(rooms, rooms[index].ID)] = true
		}
	}
	for _, connection := range backbone {
		from := roomIndexByID(rooms, connection.FromRoomID)
		to := roomIndexByID(rooms, connection.ToRoomID)
		assert.False(t, treasure[from] && treasure[to],
			"Treasures %d and %d share an edge", connection.FromRoomID, connection.ToRoomID)
	}
}

func countRole(roles []*RoomRole, want RoomRole) int {
	count := 0
	for _, role := range roles {
		if role != nil && *role == want {
			count++
		}
	}
	return count
}

func assertRoleAt(t *testing.T, roles []*RoomRole, index int, want RoomRole) {
	t.Helper()
	require.NotNil(t, roles[index])
	assert.Equal(t, want, *roles[index])
}

func assertGraphConnected(t *testing.T, rooms []PlacedRoom, connections []Connection) {
	t.Helper()
	visited := make([]bool, len(rooms))
	visited[0] = true
	for changed := true; changed; {
		changed = false
		for _, connection := range connections {
			from := int(connection.FromRoomID)
			to := int(connection.ToRoomID)
			if visited[from] == visited[to] {
				continue
			}
			visited[from] = true
			visited[to] = true
			changed = true
		}
	}
	for roomIndex := range rooms {
		assert.True(t, visited[roomIndex], "Room %d must stay reachable", roomIndex)
	}
}
