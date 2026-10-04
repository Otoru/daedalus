package daedalus

import (
	"context"
	"fmt"
	"sort"
)

// LoopConnectorConfig selects the opt-in loop_rooms_v1 Connector.
//
// TargetRingRooms is a preference, not a promise: selection accepts the
// nearest size in the documented ±2 band. Zero selects the automatic target
// clamp. The loop mode is intentionally SDK-only and remains disabled when a
// Generator has a nil Connector. The measured 128×128/256 case costs about
// six times the default Prim generation, so callers should opt in knowingly.
type LoopConnectorConfig struct {
	TargetRingRooms uint32
}

// NewLoopConnector constructs the deterministic loop_rooms_v1 Connector.
// Three-or-more-Room requests require one ExtraEdgeCount unit because the
// closing edge is part of the main ring. One- and two-Room requests retain
// their degenerate tree and consume no cycle budget.
func NewLoopConnector(config LoopConnectorConfig) (Connector, error) {
	if config.TargetRingRooms == 1 || config.TargetRingRooms == 2 || config.TargetRingRooms > MaxRooms {
		return nil, fmt.Errorf("%w: TargetRingRooms must be zero or from 3 to %d", ErrInvalidConfig, MaxRooms)
	}
	return loopRoomsConnector{target: config.TargetRingRooms}, nil
}

type loopRoomsConnector struct{ target uint32 }

var _ Connector = loopRoomsConnector{}

type loopCenter struct{ x, y int64 }

// loopRing is the observable topological selection used by the later branch
// phase. Keeping the ordered edges here freezes the route probe and corridor
// ID order before branch growth is added.
type loopRing struct {
	Vertices []PlacedRoom
	Edges    []Connection
}

type loopRingCandidate struct {
	vertices       []int
	edges          []Connection
	closingSquared int64
	maxScaffold    int64
	totalScaffold  int64
}

// Connect selects the ring first, then grows every remaining Room through the
// router-aware branch phase. Ring edges are immutable once committed; only a
// branch edge may be replaced by a two-edge splice.
func (connector loopRoomsConnector) Connect(req ConnectionRequest) ([]Connection, error) {
	ctx := topologyContext(req.Context)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	n := len(req.Rooms)
	if n <= 1 {
		return []Connection{}, nil
	}
	if n == 2 {
		return []Connection{{FromRoomID: req.Rooms[0].ID, ToRoomID: req.Rooms[1].ID}}, nil
	}
	if req.ExtraEdgeCount == 0 {
		return nil, fmt.Errorf("%w: loop_rooms_v1 requires ExtraEdgeCount >= 1 for three or more Rooms", ErrInvalidConfig)
	}
	target := int(connector.target)
	if target == 0 {
		target = loopAutomaticTarget(n)
	}
	if target > n {
		target = n
	}
	width := req.MaxCorridorWidth
	if width == 0 {
		width = 1
	}
	probe := loopRouteProbe(ctx, req)
	ring, err := selectLoopRing(ctx, req, target, probe)
	if err != nil {
		return nil, err
	}
	if err := commitLoopRing(req, ring); err != nil {
		return nil, err
	}
	return loopGrowBranches(ctx, req, ring.Edges, ring.Vertices, width)
}

func loopRouteProbe(ctx context.Context, req ConnectionRequest) func([]Connection) bool {
	probe := func(edges []Connection) bool { return true }
	if req.TryRoute != nil {
		probe = func(edges []Connection) bool {
			session, err := newRouteSession(ctx, req.Width, req.Height, CorridorOrderXThenY, req.Rooms, nil, nil)
			if err != nil {
				return false
			}
			for _, edge := range edges {
				ok, routeErr := session.tryCommit(edge.FromRoomID, edge.ToRoomID)
				if routeErr != nil || !ok {
					return false
				}
			}
			return true
		}
	}
	return probe
}

func commitLoopRing(req ConnectionRequest, ring loopRing) error {
	if req.TryRoute != nil {
		for _, edge := range ring.Edges {
			ok, routeErr := req.TryRoute(edge.FromRoomID, edge.ToRoomID)
			if routeErr != nil {
				return routeErr
			}
			if !ok {
				return ErrUnconnectablePlacement
			}
		}
	}
	return nil
}

func loopGrowBranches(ctx context.Context, req ConnectionRequest, ringEdges []Connection, ringRooms []PlacedRoom, corridorWidth uint32) ([]Connection, error) {
	roomCount := len(req.Rooms)
	capacity := make([]int, roomCount)
	for index, room := range req.Rooms {
		capacity[index] = roomOpeningBudget(placedRoomFootprint(room), req.Width, req.Height, corridorWidth, req.MaxRoomEdges)
	}
	visited := make([]bool, roomCount)
	degree := make([]int, roomCount)
	ringPairs := make([]bool, roomCount*roomCount)
	result := append([]Connection(nil), ringEdges...)
	if err := initializeLoopRing(req.Rooms, ringRooms, ringEdges, visited, degree, ringPairs); err != nil {
		return nil, err
	}
	rejected := make([]bool, roomCount*roomCount)
	for len(result) < roomCount {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		selected, fromIndex, toIndex, found := loopBestBranch(req.Rooms, visited, degree, capacity, rejected, nil)
		if found {
			if req.TryRoute != nil {
				ok, err := req.TryRoute(selected.FromRoomID, selected.ToRoomID)
				if err != nil {
					return nil, err
				}
				if !ok {
					rejected[fromIndex*roomCount+toIndex] = true
					rejected[toIndex*roomCount+fromIndex] = true
					continue
				}
			}
			result = append(result, selected)
			visited[toIndex] = true
			degree[fromIndex]++
			degree[toIndex]++
			continue
		}
		spliced, updated, spliceResult := loopSpliceBranch(ctx, req, result, visited, degree, capacity, ringPairs)
		if spliceResult != nil {
			return nil, spliceResult
		}
		if !spliced {
			return nil, ErrUnconnectablePlacement
		}
		result = updated
	}
	return result, nil
}

func initializeLoopRing(rooms []PlacedRoom, ringRooms []PlacedRoom, ringEdges []Connection, visited []bool, degree []int, ringPairs []bool) error {
	roomCount := len(rooms)
	for _, room := range ringRooms {
		index := roomIndexByID(rooms, room.ID)
		if index >= 0 {
			visited[index] = true
		}
	}
	for _, edge := range ringEdges {
		from := roomIndexByID(rooms, edge.FromRoomID)
		to := roomIndexByID(rooms, edge.ToRoomID)
		if from < 0 || to < 0 {
			return errTopologyUnknownRoom
		}
		degree[from]++
		degree[to]++
		ringPairs[from*roomCount+to] = true
		ringPairs[to*roomCount+from] = true
	}
	return nil
}

func loopBestBranch(rooms []PlacedRoom, visited []bool, degree, capacity []int, rejected []bool, centers []loopCenter) (Connection, int, int, bool) {
	if centers == nil {
		centers = loopRoomCenters(rooms)
	}
	roomCount := len(rooms)
	var selected Connection
	fromIndex, toIndex := -1, -1
	var selectedWeight int64
	found := false
	for from := range rooms {
		if !visited[from] || degree[from] >= capacity[from] {
			continue
		}
		for to := range rooms {
			if !loopBranchAllowed(visited, degree, capacity, rejected, roomCount, from, to) {
				continue
			}
			candidate := Connection{FromRoomID: rooms[from].ID, ToRoomID: rooms[to].ID}
			weight := loopSquaredDistance(centers, from, to)
			if loopBranchBetter(found, weight, selectedWeight, candidate, selected) {
				selected, fromIndex, toIndex, selectedWeight, found = candidate, from, to, weight, true
			}
		}
	}
	return selected, fromIndex, toIndex, found
}

func loopRoomCenters(rooms []PlacedRoom) []loopCenter {
	centers := make([]loopCenter, len(rooms))
	for index, room := range rooms {
		centers[index] = loopDoubledCenter(room)
	}
	return centers
}

func loopBranchBetter(found bool, weight, selectedWeight int64, candidate, selected Connection) bool {
	return !found || weight < selectedWeight || (weight == selectedWeight && loopConnectionLess(candidate, selected))
}

func loopBranchAllowed(visited []bool, degree, capacity []int, rejected []bool, roomCount, from, to int) bool {
	return !visited[to] && degree[to] < capacity[to] && !rejected[from*roomCount+to]
}

func loopSpliceBranch(ctx context.Context, req ConnectionRequest, edges []Connection, visited []bool, degree, capacity []int, ringPairs []bool) (bool, []Connection, error) {
	state := loopSpliceState{ctx: ctx, req: req, edges: edges, visited: visited, degree: degree, capacity: capacity, ringPairs: ringPairs}
	for stranded := range req.Rooms {
		if visited[stranded] || capacity[stranded] < 2 {
			continue
		}
		for edgeIndex, old := range edges {
			updated, ok, err := state.trySplice(edgeIndex, old, stranded)
			if err != nil {
				return false, nil, err
			}
			if ok {
				return true, updated, nil
			}
		}
	}
	return false, nil, nil
}

type loopSpliceState struct {
	ctx              context.Context
	req              ConnectionRequest
	edges            []Connection
	visited          []bool
	degree, capacity []int
	ringPairs        []bool
}

func (state *loopSpliceState) trySplice(edgeIndex int, old Connection, stranded int) ([]Connection, bool, error) {
	roomCount := len(state.req.Rooms)
	from := roomIndexByID(state.req.Rooms, old.FromRoomID)
	to := roomIndexByID(state.req.Rooms, old.ToRoomID)
	if from < 0 || to < 0 || state.ringPairs[from*roomCount+to] || state.degree[from] >= state.capacity[from] || state.degree[to] >= state.capacity[to] {
		return nil, false, nil
	}
	if err := state.ctx.Err(); err != nil {
		return nil, false, err
	}
	first := Connection{FromRoomID: state.req.Rooms[from].ID, ToRoomID: state.req.Rooms[stranded].ID}
	second := Connection{FromRoomID: state.req.Rooms[to].ID, ToRoomID: state.req.Rooms[stranded].ID}
	if state.req.RewireRoute != nil {
		ok, err := state.req.RewireRoute(old, first, second)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			return nil, false, nil
		}
	}
	updated := make([]Connection, 0, len(state.edges)+1)
	updated = append(updated, state.edges[:edgeIndex]...)
	updated = append(updated, first, second)
	updated = append(updated, state.edges[edgeIndex+1:]...)
	state.visited[stranded] = true
	state.degree[stranded] = 2
	return updated, true, nil
}

func loopAutomaticTarget(roomCount int) int {
	target := loopCeilSqrt(roomCount)
	if target < 6 {
		return 6
	}
	if target > 12 {
		return 12
	}
	return target
}

func loopCeilSqrt(value int) int {
	root := 0
	for root*root < value {
		root++
	}
	return root
}

func selectLoopRing(ctx context.Context, req ConnectionRequest, target int, probe func([]Connection) bool) (loopRing, error) {
	n := len(req.Rooms)
	if n < 3 {
		return loopRing{}, fmt.Errorf("%w: a ring needs at least three Rooms", ErrUnconnectablePlacement)
	}
	centers := make([]loopCenter, n)
	for index, room := range req.Rooms {
		centers[index] = loopDoubledCenter(room)
	}
	neighbors, treePairs, err := loopScaffold(ctx, req.Rooms, centers)
	if err != nil {
		return loopRing{}, err
	}
	width := req.MaxCorridorWidth
	if width == 0 {
		width = 1
	}
	capacity := make([]int, n)
	for index, room := range req.Rooms {
		capacity[index] = roomOpeningBudget(placedRoomFootprint(room), req.Width, req.Height, width, req.MaxRoomEdges)
	}
	candidates, err := loopCandidates(ctx, req.Rooms, centers, neighbors, treePairs, capacity, target)
	if err != nil {
		return loopRing{}, err
	}
	for _, candidate := range candidates {
		if probe == nil || probe(candidate.edges) {
			vertices := make([]PlacedRoom, len(candidate.vertices))
			for index, roomIndex := range candidate.vertices {
				vertices[index] = req.Rooms[roomIndex]
			}
			return loopRing{Vertices: vertices, Edges: candidate.edges}, nil
		}
	}
	return loopRing{}, ErrUnconnectablePlacement
}

func loopDoubledCenter(room PlacedRoom) loopCenter {
	x := int64(room.Origin.X) * 2
	x += int64(room.Width)
	x -= 1
	y := int64(room.Origin.Y) * 2
	y += int64(room.Height)
	y -= 1
	return loopCenter{x: x, y: y}
}

func loopSquaredDistance(centers []loopCenter, first, second int) int64 {
	deltaX := centers[first].x - centers[second].x
	deltaY := centers[first].y - centers[second].y
	deltaXSquared := deltaX * deltaX
	deltaYSquared := deltaY * deltaY
	return deltaXSquared + deltaYSquared
}

func loopScaffold(ctx context.Context, rooms []PlacedRoom, centers []loopCenter) ([][]int, []bool, error) {
	n := len(rooms)
	neighbors := make([][]int, n)
	treePairs := make([]bool, n*n)
	visited := make([]bool, n)
	visited[0] = true
	for accepted := 1; accepted < n; accepted++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		bestFrom, bestTo, found := loopScaffoldEdge(rooms, centers, visited)
		if !found {
			return nil, nil, ErrUnconnectablePlacement
		}
		visited[bestTo] = true
		neighbors[bestFrom] = append(neighbors[bestFrom], bestTo)
		neighbors[bestTo] = append(neighbors[bestTo], bestFrom)
		treePairs[bestFrom*n+bestTo] = true
		treePairs[bestTo*n+bestFrom] = true
	}
	for index := range neighbors {
		sort.Ints(neighbors[index])
	}
	return neighbors, treePairs, nil
}

func loopScaffoldEdge(rooms []PlacedRoom, centers []loopCenter, visited []bool) (bestFrom, bestTo int, found bool) {
	bestFrom, bestTo = -1, -1
	var bestWeight int64
	found = false
	for from := range rooms {
		if !visited[from] {
			continue
		}
		for to := range rooms {
			if visited[to] {
				continue
			}
			weight := loopSquaredDistance(centers, from, to)
			candidate := Connection{FromRoomID: rooms[from].ID, ToRoomID: rooms[to].ID}
			if loopScaffoldBetter(rooms, candidate, weight, bestFrom, bestTo, bestWeight, found) {
				bestFrom, bestTo, bestWeight, found = from, to, weight, true
			}
		}
	}
	return bestFrom, bestTo, found
}

func loopScaffoldBetter(rooms []PlacedRoom, candidate Connection, weight int64, bestFrom, bestTo int, bestWeight int64, found bool) bool {
	if !found || weight < bestWeight {
		return true
	}
	if weight != bestWeight {
		return false
	}
	best := Connection{FromRoomID: rooms[bestFrom].ID, ToRoomID: rooms[bestTo].ID}
	return loopConnectionLess(candidate, best)
}

func loopCandidates(ctx context.Context, rooms []PlacedRoom, centers []loopCenter, neighbors [][]int, treePairs []bool, capacity []int, target int) ([]loopRingCandidate, error) {
	candidates, err := collectLoopCandidates(ctx, rooms, centers, neighbors, treePairs, capacity)
	if err != nil {
		return nil, err
	}
	return closestLoopBand(rooms, candidates, target), nil
}

func collectLoopCandidates(ctx context.Context, rooms []PlacedRoom, centers []loopCenter, neighbors [][]int, treePairs []bool, capacity []int) ([]loopRingCandidate, error) {
	n := len(rooms)
	candidates := make([]loopRingCandidate, 0)
	for first := 0; first < n; first++ {
		for second := first + 1; second < n; second++ {
			if treePairs[first*n+second] {
				continue
			}
			candidate, ok, err := loopCandidateForPair(ctx, rooms, centers, neighbors, capacity, first, second)
			if err != nil {
				return nil, err
			}
			if ok {
				candidates = append(candidates, candidate)
			}
		}
	}
	return candidates, nil
}

func closestLoopBand(rooms []PlacedRoom, candidates []loopRingCandidate, target int) []loopRingCandidate {
	n := len(rooms)
	for radius := 0; radius <= n; radius++ {
		lower, upper := target-radius, target+radius
		if lower < 3 {
			lower = 3
		}
		if upper > n {
			upper = n
		}
		band := make([]loopRingCandidate, 0, len(candidates))
		for _, candidate := range candidates {
			if len(candidate.vertices) >= lower && len(candidate.vertices) <= upper {
				band = append(band, candidate)
			}
		}
		if len(band) == 0 {
			continue
		}
		sort.SliceStable(band, func(left, right int) bool { return loopCandidateLess(rooms, band[left], band[right], target) })
		return band
	}
	return nil
}

func loopCandidateForPair(ctx context.Context, rooms []PlacedRoom, centers []loopCenter, neighbors [][]int, capacity []int, first, second int) (loopRingCandidate, bool, error) {
	path, err := loopTreePath(ctx, neighbors, first, second)
	if err != nil {
		return loopRingCandidate{}, false, err
	}
	if len(path) < 3 || !loopPathEligible(path, capacity, len(rooms)) {
		return loopRingCandidate{}, false, nil
	}
	maxScaffold, totalScaffold := int64(0), int64(0)
	pathEdges := make([]Connection, 0, len(path))
	for index := 0; index+1 < len(path); index++ {
		weight := loopSquaredDistance(centers, path[index], path[index+1])
		totalScaffold += weight
		if weight > maxScaffold {
			maxScaffold = weight
		}
		pathEdges = append(pathEdges, Connection{FromRoomID: rooms[path[index]].ID, ToRoomID: rooms[path[index+1]].ID})
	}
	sort.SliceStable(pathEdges, func(left, right int) bool {
		leftWeight := loopEdgeWeight(rooms, centers, pathEdges[left])
		rightWeight := loopEdgeWeight(rooms, centers, pathEdges[right])
		if leftWeight != rightWeight {
			return leftWeight > rightWeight
		}
		return loopConnectionLess(pathEdges[left], pathEdges[right])
	})
	pathEdges = append(pathEdges, Connection{FromRoomID: rooms[first].ID, ToRoomID: rooms[second].ID})
	return loopRingCandidate{vertices: path, edges: pathEdges, closingSquared: loopSquaredDistance(centers, first, second), maxScaffold: maxScaffold, totalScaffold: totalScaffold}, true, nil
}

func loopPathEligible(path []int, capacity []int, roomCount int) bool {
	hasRoot := false
	for _, vertex := range path {
		if vertex == 0 {
			hasRoot = true
		}
		if capacity[vertex] < 2 {
			return false
		}
	}
	if !hasRoot {
		return false
	}
	if len(path) == roomCount {
		return true
	}
	for _, vertex := range path {
		if capacity[vertex] >= 3 {
			return true
		}
	}
	return false
}

func loopCandidateLess(rooms []PlacedRoom, first, second loopRingCandidate, target int) bool {
	if first.closingSquared != second.closingSquared {
		return first.closingSquared < second.closingSquared
	}
	firstDistance := len(first.vertices) - target
	if firstDistance < 0 {
		firstDistance = -firstDistance
	}
	secondDistance := len(second.vertices) - target
	if secondDistance < 0 {
		secondDistance = -secondDistance
	}
	if firstDistance != secondDistance {
		return firstDistance < secondDistance
	}
	if first.maxScaffold != second.maxScaffold {
		return first.maxScaffold < second.maxScaffold
	}
	if first.totalScaffold != second.totalScaffold {
		return first.totalScaffold < second.totalScaffold
	}
	return loopConnectionLess(first.edges[len(first.edges)-1], second.edges[len(second.edges)-1])
}

func loopTreePath(ctx context.Context, neighbors [][]int, from, to int) ([]int, error) {
	parent := make([]int, len(neighbors))
	for index := range parent {
		parent[index] = -1
	}
	parent[from] = from
	queue := []int{from}
	for head := 0; head < len(queue); head++ {
		if head%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		current := queue[head]
		if current == to {
			break
		}
		queue = appendUnvisitedLoopNeighbors(queue, parent, neighbors[current], current)
	}
	if parent[to] < 0 {
		return nil, nil
	}
	path := loopParentPath(parent, from, to)
	if req := ctx.Err(); req != nil {
		return nil, req
	}
	return path, nil
}

func appendUnvisitedLoopNeighbors(queue, parent, neighbors []int, current int) []int {
	for _, next := range neighbors {
		if parent[next] >= 0 {
			continue
		}
		parent[next] = current
		queue = append(queue, next)
	}
	return queue
}

func loopParentPath(parent []int, from, to int) []int {
	path := []int{to}
	for path[len(path)-1] != from {
		path = append(path, parent[path[len(path)-1]])
	}
	for left, right := 0, len(path)-1; left < right; left, right = left+1, right-1 {
		path[left], path[right] = path[right], path[left]
	}
	return path
}

func loopEdgeWeight(rooms []PlacedRoom, centers []loopCenter, edge Connection) int64 {
	return loopSquaredDistance(centers, roomIndexByID(rooms, edge.FromRoomID), roomIndexByID(rooms, edge.ToRoomID))
}

func loopConnectionLess(first, second Connection) bool {
	firstLow, firstHigh := first.FromRoomID, first.ToRoomID
	if firstLow > firstHigh {
		firstLow, firstHigh = firstHigh, firstLow
	}
	secondLow, secondHigh := second.FromRoomID, second.ToRoomID
	if secondLow > secondHigh {
		secondLow, secondHigh = secondHigh, secondLow
	}
	if firstLow != secondLow {
		return firstLow < secondLow
	}
	return firstHigh < secondHigh
}
