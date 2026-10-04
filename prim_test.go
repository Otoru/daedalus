package daedalus

import (
	"context"
	"math"
	"math/rand"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrimMinimumRoomCount(t *testing.T) {
	connector := primRoomsConnector{}

	for _, testCase := range []struct {
		name  string
		rooms []PlacedRoom
		want  []Connection
	}{
		{name: "no Room", rooms: nil, want: []Connection{}},
		{name: "one Room", rooms: []PlacedRoom{placedRoomAt(0, 4, 7)}, want: []Connection{}},
		{
			name:  "two Rooms",
			rooms: []PlacedRoom{placedRoomAt(0, 1, 2), placedRoomAt(1, 8, 9)},
			want:  []Connection{{FromRoomID: 0, ToRoomID: 1}},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			edges, err := connector.Connect(ConnectionRequest{
				Context: context.Background(),
				Rooms:   testCase.rooms,
				Seed:    123,
			})

			require.NoError(t, err)
			assert.Equal(t, testCase.want, edges)
		})
	}
}

func TestPrimMinimumRoomCountIgnoresCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for _, rooms := range [][]PlacedRoom{nil, {placedRoomAt(0, 4, 7)}} {
		edges, err := (primRoomsConnector{}).Connect(ConnectionRequest{Context: ctx, Rooms: rooms})

		require.NoError(t, err)
		assert.Equal(t, []Connection{}, edges)
	}
}

func TestPrimResultIsTree(t *testing.T) {
	rooms := randomPlacedRooms(20, 91, 32, false)

	edges, err := (primRoomsConnector{}).Connect(ConnectionRequest{
		Context:        context.Background(),
		Rooms:          rooms,
		ExtraEdgeCount: 100,
	})

	require.NoError(t, err)
	assertConnectionsFormTree(t, rooms, edges)
}

func TestPrimHasMinimumWeightAgainstKruskal(t *testing.T) {
	for caseIndex := int64(0); caseIndex < 40; caseIndex++ {
		roomCount := 2 + int(caseIndex%23)
		rooms := randomPlacedRooms(roomCount, 1000+caseIndex, 48, false)

		edges, err := (primRoomsConnector{}).Connect(ConnectionRequest{Context: context.Background(), Rooms: rooms})
		require.NoError(t, err)

		assertConnectionsFormTree(t, rooms, edges)
		assert.True(t, openingsFit(rooms, edges), "random case %d exceeds the opening budget", caseIndex)
		unconstrained := referencePrim(rooms)
		if !openingsFit(rooms, unconstrained) {
			continue
		}
		wantWeight := kruskalWeight(rooms)
		gotWeight := connectionsWeight(rooms, edges)
		assert.InDelta(t, wantWeight, gotWeight, 1e-12, "random case %d", caseIndex)
	}
}

func TestPrimBreaksTiesByIDsInFrozenOrder(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 2, 0),
		placedRoomAt(2, 0, 2),
		placedRoomAt(3, 2, 2),
	}
	want := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 2},
		{FromRoomID: 1, ToRoomID: 3},
	}
	connector := primRoomsConnector{}

	for repetition := 0; repetition < 50; repetition++ {
		edges, err := connector.Connect(ConnectionRequest{Context: context.Background(), Rooms: rooms})
		require.NoError(t, err)
		assert.Equal(t, want, edges, "repetition %d", repetition)
	}
}

func TestPrimComparatorUsesDestinationCellAsLastTieBreak(t *testing.T) {
	first := primEdgeCandidate{
		connection:    Connection{FromRoomID: 1, ToRoomID: 2},
		squaredWeight: 9,
		destination: Cell{
			X: 8,
			Y: 3,
		},
	}
	second := first
	second.destination = Cell{X: 2, Y: 4}

	assert.True(t, primEdgeLess(first, second), "smaller Y must win before X")
	assert.False(t, primEdgeLess(second, first))
	second.destination = Cell{X: 9, Y: 3}
	assert.True(t, primEdgeLess(first, second), "with equal Y, smaller X must win")
}

func TestOptimizedPrimMatchesNaivePseudocode(t *testing.T) {
	for caseIndex := int64(0); caseIndex < 80; caseIndex++ {
		roomCount := 2 + int(caseIndex%31)
		manyTies := caseIndex%2 == 0
		rooms := randomPlacedRooms(roomCount, 9000+caseIndex, 7, manyTies)

		got, err := (primRoomsConnector{}).Connect(ConnectionRequest{Context: context.Background(), Rooms: rooms})
		require.NoError(t, err)
		want := referencePrim(rooms)
		if openingsFit(rooms, want) {
			assert.Equal(t, want, got, "random case %d", caseIndex)
			continue
		}
		assertConnectionsFormTree(t, rooms, got)
		assert.True(t, openingsFit(rooms, got), "random case %d", caseIndex)
	}
}

func TestPrimUsesBoundingBoxCenterForMultiCellRooms(t *testing.T) {
	rooms := []PlacedRoom{
		{
			ID: 0, At: Cell{X: 0, Y: 0}, Shape: RoomShapeRectangle,
			Origin: Cell{X: 0, Y: 0}, Width: 21, Height: 1,
		},
		placedRoomAt(1, 10, 0),
		placedRoomAt(2, 1, 0),
	}

	edges, err := (primRoomsConnector{}).Connect(ConnectionRequest{Context: context.Background(), Rooms: rooms})

	require.NoError(t, err)
	assert.Equal(t, []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 2},
	}, edges, "the anchor metric would choose 0→2 and then 2→1")
}

func TestPrimSeedChangesNeitherResultNorStream(t *testing.T) {
	rooms := randomPlacedRooms(16, 77, 20, false)
	connector := primRoomsConnector{}

	first, firstErr := connector.Connect(ConnectionRequest{Context: context.Background(), Rooms: rooms, Seed: 1})
	second, secondErr := connector.Connect(ConnectionRequest{Context: context.Background(), Rooms: rooms, Seed: ^Seed(0)})

	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	assert.Equal(t, first, second)
}

func TestCanceledPrimReturnsNoPartialResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	edges, err := (primRoomsConnector{}).Connect(ConnectionRequest{
		Context: ctx,
		Rooms:   randomPlacedRooms(64, 55, 40, false),
	})

	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, edges)
}

func TestConcurrentPrimCallsAreIdentical(t *testing.T) {
	rooms := randomPlacedRooms(64, 8080, 30, true)
	req := ConnectionRequest{Context: context.Background(), Rooms: rooms, Seed: 44}
	connector := primRoomsConnector{}
	baseline, err := connector.Connect(req)
	require.NoError(t, err)

	const workers = 4
	results := make([][]Connection, workers)
	errors := make([]error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for worker := range workers {
		go func() {
			defer group.Done()
			results[worker], errors[worker] = connector.Connect(req)
		}()
	}
	group.Wait()

	for worker := range workers {
		require.NoError(t, errors[worker])
		assert.Equal(t, baseline, results[worker])
	}
}

func TestPrimRefusesAnEdgePastEitherRoomsOpeningBudget(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 1, 0),
		placedRoomAt(1, 0, 0),
		placedRoomAt(2, 2, 0),
		placedRoomAt(3, 1, 1),
		placedRoomAt(4, 1, 2),
	}
	edges, err := (primRoomsConnector{}).Connect(ConnectionRequest{
		Context: context.Background(), Width: 3, Height: 3, MaxCorridorWidth: 1, Rooms: rooms,
	})

	require.NoError(t, err)
	assertConnectionsFormTree(t, rooms, edges)
	assert.True(t, openingsFitOnGrid(rooms, edges, 3, 3, 1))
	unconstrained := referencePrim(rooms)
	assert.False(t, openingsFitOnGrid(rooms, unconstrained, 3, 3, 1),
		"the unconstrained tree overfills the hub, which is the case capacity has to change")
}

func TestPrimReportsAPlacementThatCannotBeSeparated(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 2, 0),
		placedRoomAt(2, 0, 2),
		placedRoomAt(3, 2, 2),
	}
	edges, err := (primRoomsConnector{}).Connect(ConnectionRequest{
		Context: context.Background(), Width: 3, Height: 3, MaxCorridorWidth: 1, Rooms: rooms,
	})

	require.ErrorIs(t, err, ErrUnconnectablePlacement)
	assert.ErrorContains(t, err, "placement cannot be connected under the separation rule")
	assert.Nil(t, edges)
}

func TestPrimKeepsTheUnconstrainedTreeWhenEveryRoomHasSpareOpenings(t *testing.T) {
	rooms := []PlacedRoom{
		rectanglePlacedRoom(0, 0, 0, 5, 5),
		rectanglePlacedRoom(1, 20, 0, 5, 5),
		rectanglePlacedRoom(2, 0, 20, 5, 5),
		rectanglePlacedRoom(3, 20, 20, 5, 5),
	}
	edges, err := (primRoomsConnector{}).Connect(ConnectionRequest{
		Context: context.Background(), Width: 40, Height: 40, MaxCorridorWidth: 1, Rooms: rooms,
	})

	require.NoError(t, err)
	assert.Equal(t, referencePrim(rooms), edges)
}

func TestPrimSkipsAnUnroutableEdgeAndStillSpans(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 2, 0),
		placedRoomAt(2, 0, 2),
	}
	var attempts []Connection
	edges, err := (primRoomsConnector{}).Connect(ConnectionRequest{
		Context: context.Background(),
		Rooms:   rooms,
		TryRoute: func(from, to RoomID) (bool, error) {
			attempts = append(attempts, Connection{FromRoomID: from, ToRoomID: to})
			if connectionJoins(Connection{FromRoomID: from, ToRoomID: to}, 0, 1) {
				return false, nil
			}
			return true, nil
		},
	})

	require.NoError(t, err)
	assert.Equal(t, []Connection{
		{FromRoomID: 0, ToRoomID: 2},
		{FromRoomID: 2, ToRoomID: 1},
	}, edges)
	assert.NotContains(t, edges, Connection{FromRoomID: 0, ToRoomID: 1})
	assert.Equal(t, []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 2},
		{FromRoomID: 2, ToRoomID: 1},
	}, attempts, "the rejected pair is the cheaper candidate, and it is not a failure")
}

func TestPrimNamesARoomWithNoRoutableEdge(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 4, 0),
		placedRoomAt(2, 0, 4),
	}
	edges, err := (primRoomsConnector{}).Connect(ConnectionRequest{
		Context: context.Background(),
		Rooms:   rooms,
		TryRoute: func(RoomID, RoomID) (bool, error) {
			return false, nil
		},
	})

	require.ErrorIs(t, err, ErrUnconnectablePlacement)
	assert.NotErrorIs(t, err, ErrUnroutableEdge)
	assert.Nil(t, edges)
}

func TestPrimTryRouteCancellationReturnsNoEdges(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 3, 0),
	}
	edges, err := (primRoomsConnector{}).Connect(ConnectionRequest{
		Context: context.Background(),
		Rooms:   rooms,
		TryRoute: func(RoomID, RoomID) (bool, error) {
			return false, context.Canceled
		},
	})

	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, edges)
}

func TestExtraEdgePhaseSkipsAnUnroutableShortcut(t *testing.T) {
	// 3×3 rooms have spare openings, so the skip is the refused route and not
	// the opening budget. Centers keep 2→3 as the shortest discarded edge and
	// 0→3 as the next one under the Prim tie-break.
	rooms := []PlacedRoom{
		rectanglePlacedRoom(0, 0, 0, 3, 3),
		rectanglePlacedRoom(1, 10, 0, 3, 3),
		rectanglePlacedRoom(2, 0, 10, 3, 3),
		rectanglePlacedRoom(3, 10, 10, 3, 3),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 2},
		{FromRoomID: 1, ToRoomID: 3},
	}
	var attempts []Connection
	connections, err := addExtraConnections(context.Background(), extraConnectionsRequest{
		Rooms: rooms, Backbone: backbone, ExtraEdgeCount: 1,
		Constraints: roleGridConstraints{CorridorWidth: 1},
		TryRoute: func(from, to RoomID) (bool, error) {
			attempts = append(attempts, Connection{FromRoomID: from, ToRoomID: to})
			if from == 2 && to == 3 {
				return false, nil
			}
			return true, nil
		},
	})

	require.NoError(t, err)
	assert.Equal(t, []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 2},
		{FromRoomID: 1, ToRoomID: 3},
		{FromRoomID: 0, ToRoomID: 3},
	}, connections)
	assert.Equal(t, []Connection{
		{FromRoomID: 2, ToRoomID: 3},
		{FromRoomID: 0, ToRoomID: 3},
	}, attempts)
}

func TestExtraEdgePhaseSkipsAShortcutThatOverfillsARoom(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 3, 0),
		placedRoomAt(2, 0, 3),
		placedRoomAt(3, 6, 0),
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
	assert.NotContains(t, connections, Connection{FromRoomID: 0, ToRoomID: 3})
	assert.True(t, openingsFit(rooms, connections))
	assert.GreaterOrEqual(t, len(connections), len(backbone))
}

func rectanglePlacedRoom(id RoomID, x, y int32, width, height uint32) PlacedRoom {
	return PlacedRoom{
		ID: id, At: Cell{X: x, Y: y}, Shape: RoomShapeRectangle,
		Origin: Cell{X: x, Y: y}, Width: width, Height: height,
		Cells: rectangleCells(x, y, width, height),
	}
}

func openingsFit(rooms []PlacedRoom, edges []Connection) bool {
	return openingsFitOnGrid(rooms, edges, 0, 0, 1)
}

func openingsFitOnGrid(rooms []PlacedRoom, edges []Connection, gridWidth, gridHeight, corridorWidth uint32) bool {
	degree := make([]int, len(rooms))
	for _, edge := range edges {
		from := -1
		to := -1
		for index, room := range rooms {
			if room.ID == edge.FromRoomID {
				from = index
			}
			if room.ID == edge.ToRoomID {
				to = index
			}
		}
		if from < 0 || to < 0 {
			return false
		}
		degree[from]++
		degree[to]++
	}
	for index, room := range rooms {
		capacity := roomOpeningCapacity(placedRoomFootprint(room), gridWidth, gridHeight, corridorWidth)
		if degree[index] > capacity {
			return false
		}
	}
	return true
}

func placedRoomAt(id RoomID, x, y int32) PlacedRoom {
	at := Cell{X: x, Y: y}
	return PlacedRoom{
		ID: id, At: at, Shape: RoomShapeRectangle, Origin: at,
		Width: 1, Height: 1, Cells: []Cell{at},
	}
}

func randomPlacedRooms(count int, seed int64, coordinateRange int32, manyTies bool) []PlacedRoom {
	random := rand.New(rand.NewSource(seed))
	rooms := make([]PlacedRoom, count)
	for index := range rooms {
		x := int32(random.Int31n(coordinateRange))
		y := int32(random.Int31n(coordinateRange))
		width := uint32(1 + random.Int31n(7))
		height := uint32(1 + random.Int31n(7))
		if manyTies {
			x %= 4
			y %= 4
			width = 1
			height = 1
		}
		at := Cell{X: x, Y: y}
		rooms[index] = PlacedRoom{
			ID: RoomID(index), At: at, Shape: RoomShapeRectangle,
			Origin: at, Width: width, Height: height,
		}
	}
	return rooms
}

type referenceEdge struct {
	connection  Connection
	fromIndex   int
	toIndex     int
	weight      float64
	destination Cell
}

func referencePrim(rooms []PlacedRoom) []Connection {
	if len(rooms) <= 1 {
		return []Connection{}
	}
	visited := make([]bool, len(rooms))
	visited[0] = true
	edges := make([]Connection, 0, len(rooms)-1)
	for len(edges) < len(rooms)-1 {
		best := nextReferencePrimEdge(rooms, visited)
		edges = append(edges, best.connection)
		visited[best.toIndex] = true
	}
	return edges
}

func nextReferencePrimEdge(rooms []PlacedRoom, visited []bool) referenceEdge {
	var best referenceEdge
	hasBest := false
	for fromIndex, from := range rooms {
		if !visited[fromIndex] {
			continue
		}
		for toIndex, to := range rooms {
			if visited[toIndex] {
				continue
			}
			candidate := referenceEdge{
				connection:  Connection{FromRoomID: from.ID, ToRoomID: to.ID},
				fromIndex:   fromIndex,
				toIndex:     toIndex,
				weight:      referenceWeight(from, to),
				destination: to.At,
			}
			if !hasBest || referenceEdgeLess(candidate, best) {
				best = candidate
				hasBest = true
			}
		}
	}
	return best
}

func referenceEdgeLess(first, second referenceEdge) bool {
	if first.weight != second.weight {
		return first.weight < second.weight
	}
	if first.connection.FromRoomID != second.connection.FromRoomID {
		return first.connection.FromRoomID < second.connection.FromRoomID
	}
	if first.connection.ToRoomID != second.connection.ToRoomID {
		return first.connection.ToRoomID < second.connection.ToRoomID
	}
	if first.destination.Y != second.destination.Y {
		return first.destination.Y < second.destination.Y
	}
	return first.destination.X < second.destination.X
}

func referenceWeight(first, second PlacedRoom) float64 {
	firstX := float64(first.Origin.X) + float64(first.Width-1)/2
	firstY := float64(first.Origin.Y) + float64(first.Height-1)/2
	secondX := float64(second.Origin.X) + float64(second.Width-1)/2
	secondY := float64(second.Origin.Y) + float64(second.Height-1)/2
	deltaX := firstX - secondX
	deltaY := firstY - secondY
	return deltaX*deltaX + deltaY*deltaY
}

func assertConnectionsFormTree(t *testing.T, rooms []PlacedRoom, edges []Connection) {
	t.Helper()
	require.Len(t, edges, len(rooms)-1)
	parent := make([]int, len(rooms))
	for index := range parent {
		parent[index] = index
	}
	seen := make(map[[2]RoomID]struct{}, len(edges))
	for _, edge := range edges {
		assert.NotEqual(t, edge.FromRoomID, edge.ToRoomID)
		pair := [2]RoomID{edge.FromRoomID, edge.ToRoomID}
		if pair[0] > pair[1] {
			pair[0], pair[1] = pair[1], pair[0]
		}
		_, duplicate := seen[pair]
		assert.False(t, duplicate, "duplicate edge: %v", pair)
		seen[pair] = struct{}{}
		firstRoot := findRoot(parent, int(edge.FromRoomID))
		secondRoot := findRoot(parent, int(edge.ToRoomID))
		assert.NotEqual(t, firstRoot, secondRoot, "an edge must not close a cycle")
		parent[firstRoot] = secondRoot
	}
	wantRoot := findRoot(parent, 0)
	for index := range rooms {
		assert.Equal(t, wantRoot, findRoot(parent, index), "Room %d must be reachable", index)
	}
}

func kruskalWeight(rooms []PlacedRoom) float64 {
	edges := make([]referenceEdge, 0, len(rooms)*(len(rooms)-1)/2)
	for firstIndex := range rooms {
		for secondIndex := firstIndex + 1; secondIndex < len(rooms); secondIndex++ {
			edges = append(edges, referenceEdge{
				fromIndex: firstIndex,
				toIndex:   secondIndex,
				weight:    referenceWeight(rooms[firstIndex], rooms[secondIndex]),
			})
		}
	}
	sort.Slice(edges, func(first, second int) bool {
		return edges[first].weight < edges[second].weight
	})
	parent := make([]int, len(rooms))
	for index := range parent {
		parent[index] = index
	}
	var total float64
	for _, edge := range edges {
		firstRoot := findRoot(parent, edge.fromIndex)
		secondRoot := findRoot(parent, edge.toIndex)
		if firstRoot == secondRoot {
			continue
		}
		parent[firstRoot] = secondRoot
		total += math.Sqrt(edge.weight)
	}
	return total
}

func connectionsWeight(rooms []PlacedRoom, edges []Connection) float64 {
	var total float64
	for _, edge := range edges {
		total += math.Sqrt(referenceWeight(rooms[int(edge.FromRoomID)], rooms[int(edge.ToRoomID)]))
	}
	return total
}

func findRoot(parent []int, node int) int {
	for parent[node] != node {
		node = parent[node]
	}
	return node
}
