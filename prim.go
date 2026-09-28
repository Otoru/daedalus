package daedalus

import (
	"context"
	"fmt"
	"sort"
)

const (
	// primSingleRoomLimit is the largest Room count requiring no edge.
	primSingleRoomLimit = 1
	// primStartingRoomIndex corresponds to RoomID 0 in the canonical sequence.
	primStartingRoomIndex = 0
	// boundingBoxCenterDivisor converts the span between the first and last
	// bounding-box coordinates into an offset to the center.
	boundingBoxCenterDivisor = 2.0
	// boundingBoxCellAdjustment converts a Cell count into the span between the
	// centers of the first and last Cells.
	boundingBoxCellAdjustment = 1.0
	// primCancellationUpdateInterval limits the interval between Context checks
	// during best-key updates. Built-ins recheck Context at phase boundaries and
	// at least every 256 candidate attempts or Prim key updates.
	primCancellationUpdateInterval uint64 = 256
)

// primRoomsConnector implements the built-in, frozen prim_rooms_v1 algorithm.
// The type has no state: every buffer belongs to the Connect call.
type primRoomsConnector struct{}

var _ Connector = primRoomsConnector{}

type roomCenter struct {
	x float64
	y float64
}

type primEdgeCandidate struct {
	connection    Connection
	fromIndex     int
	toIndex       int
	squaredWeight float64
	destination   Cell
}

// Connect chooses only the backbone tree among Rooms. ExtraEdgeCount shortcuts
// belong to Generator's later phase.
func (primRoomsConnector) Connect(req ConnectionRequest) ([]Connection, error) {
	roomCount := len(req.Rooms)
	edgeCapacity := roomCount - primSingleRoomLimit
	if edgeCapacity < 0 {
		edgeCapacity = 0
	}
	edges := make([]Connection, 0, edgeCapacity)
	if roomCount <= primSingleRoomLimit {
		return edges, nil
	}

	ctx := req.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	search := newPrimSearch(req, ctx)
	if err := search.updateKeys(primStartingRoomIndex); err != nil {
		return nil, err
	}
	for len(edges) < edgeCapacity {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		selected, feasible, selectErr := search.selectFeasibleEdge()
		if selectErr != nil {
			return nil, selectErr
		}
		if !feasible {
			if search.req.TryRoute != nil && search.req.RewireRoute == nil {
				return nil, search.unconnectableRoom()
			}
			spliced, spliceErr := search.spliceStranded(&edges)
			if spliceErr != nil {
				return nil, spliceErr
			}
			if !spliced {
				if search.req.TryRoute != nil {
					return nil, search.unconnectableRoom()
				}
				return nil, ErrUnconnectablePlacement
			}
			continue
		}
		if search.req.TryRoute != nil {
			ok, routeErr := search.req.TryRoute(selected.connection.FromRoomID, selected.connection.ToRoomID)
			if routeErr != nil {
				return nil, routeErr
			}
			if !ok {
				search.rejectPair(selected.fromIndex, selected.toIndex)
				continue
			}
		}
		edges = append(edges, selected.connection)
		search.used[selected.fromIndex]++
		search.used[selected.toIndex]++
		search.visited[selected.toIndex] = true
		search.hasKey[selected.toIndex] = false
		search.linkRooms(selected.fromIndex, selected.toIndex)
		if err := search.updateKeys(selected.toIndex); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return edges, nil
}

// primSearch holds the growing tree of one prim_rooms_v1 Connect call.
type primSearch struct {
	req        ConnectionRequest
	ctx        context.Context
	centers    []roomCenter
	visited    []bool
	bestKeys   []primEdgeCandidate
	hasKey     []bool
	keyUpdates uint64
	capacity   []int
	used       []int
	neighbors  [][]int
	// infeasible records undirected pairs that TryRoute refused. Index is
	// from*roomCount+to, both orientations, so the next candidate is chosen
	// without iterating a map.
	infeasible []bool
}

func newPrimSearch(req ConnectionRequest, ctx context.Context) *primSearch {
	roomCount := len(req.Rooms)
	centers := make([]roomCenter, roomCount)
	for index, room := range req.Rooms {
		centers[index] = boundingBoxCenter(room)
	}
	visited := make([]bool, roomCount)
	visited[primStartingRoomIndex] = true
	width := req.MaxCorridorWidth
	if width == 0 {
		width = 1
	}
	capacity := make([]int, roomCount)
	for index, room := range req.Rooms {
		capacity[index] = roomOpeningCapacity(placedRoomFootprint(room), req.Width, req.Height, width)
	}
	return &primSearch{
		req:       req,
		ctx:       ctx,
		centers:   centers,
		visited:   visited,
		bestKeys:  make([]primEdgeCandidate, roomCount),
		hasKey:    make([]bool, roomCount),
		capacity:  capacity,
		used:      make([]int, roomCount),
		neighbors: make([][]int, roomCount),
	}
}

func (search *primSearch) updateKeys(fromIndex int) error {
	if search.used[fromIndex] >= search.capacity[fromIndex] {
		return nil
	}
	from := search.req.Rooms[fromIndex]
	for toIndex, to := range search.req.Rooms {
		if search.visited[toIndex] {
			continue
		}
		if search.keyUpdates%primCancellationUpdateInterval == 0 {
			if err := search.ctx.Err(); err != nil {
				return err
			}
		}
		search.keyUpdates++

		if search.used[toIndex] >= search.capacity[toIndex] {
			continue
		}
		if search.pairRejected(fromIndex, toIndex) {
			continue
		}
		candidate := primEdgeCandidate{
			connection: Connection{
				FromRoomID: from.ID,
				ToRoomID:   to.ID,
			},
			fromIndex:     fromIndex,
			toIndex:       toIndex,
			squaredWeight: squaredCenterDistance(search.centers[fromIndex], search.centers[toIndex]),
			// Equal weights break ties by FromRoomID, then ToRoomID, then this
			// destination anchor (At) in canonical Y-then-X order.
			destination: to.At,
		}
		if !search.hasKey[toIndex] || primEdgeLess(candidate, search.bestKeys[toIndex]) {
			search.bestKeys[toIndex] = candidate
			search.hasKey[toIndex] = true
		}
	}
	return nil
}

// selectFeasibleEdge returns the cheapest edge into an unvisited Room whose
// both ends still have an opening left. A stored key whose source has since
// filled is recomputed from the Rooms that still have room.
func (search *primSearch) selectFeasibleEdge() (primEdgeCandidate, bool, error) {
	var selected primEdgeCandidate
	found := false
	for roomIndex := range search.req.Rooms {
		if search.visited[roomIndex] {
			continue
		}
		feasible, err := search.ensureFeasibleKey(roomIndex)
		if err != nil {
			return primEdgeCandidate{}, false, err
		}
		if !feasible {
			continue
		}
		candidate := search.bestKeys[roomIndex]
		if !found || primEdgeLess(candidate, selected) {
			selected = candidate
			found = true
		}
	}
	return selected, found, nil
}

func (search *primSearch) ensureFeasibleKey(toIndex int) (bool, error) {
	if search.capacity[toIndex] <= 0 {
		search.hasKey[toIndex] = false
		return false, nil
	}
	if search.hasKey[toIndex] {
		fromIndex := search.bestKeys[toIndex].fromIndex
		if search.visited[fromIndex] && search.used[fromIndex] < search.capacity[fromIndex] &&
			!search.pairRejected(fromIndex, toIndex) {
			return true, nil
		}
	}
	search.hasKey[toIndex] = false
	for fromIndex, from := range search.req.Rooms {
		if !search.visited[fromIndex] || search.used[fromIndex] >= search.capacity[fromIndex] {
			continue
		}
		if search.pairRejected(fromIndex, toIndex) {
			continue
		}
		if search.keyUpdates%primCancellationUpdateInterval == 0 {
			if err := search.ctx.Err(); err != nil {
				return false, err
			}
		}
		search.keyUpdates++
		candidate := primEdgeCandidate{
			connection:    Connection{FromRoomID: from.ID, ToRoomID: search.req.Rooms[toIndex].ID},
			fromIndex:     fromIndex,
			toIndex:       toIndex,
			squaredWeight: squaredCenterDistance(search.centers[fromIndex], search.centers[toIndex]),
			destination:   search.req.Rooms[toIndex].At,
		}
		if !search.hasKey[toIndex] || primEdgeLess(candidate, search.bestKeys[toIndex]) {
			search.bestKeys[toIndex] = candidate
			search.hasKey[toIndex] = true
		}
	}
	return search.hasKey[toIndex], nil
}

func (search *primSearch) pairRejected(from, to int) bool {
	if len(search.infeasible) == 0 {
		return false
	}
	roomCount := len(search.req.Rooms)
	return search.infeasible[from*roomCount+to]
}

func (search *primSearch) rejectPair(from, to int) {
	roomCount := len(search.req.Rooms)
	if search.infeasible == nil {
		search.infeasible = make([]bool, roomCount*roomCount)
	}
	search.infeasible[from*roomCount+to] = true
	search.infeasible[to*roomCount+from] = true
	search.hasKey[to] = false
}

func (search *primSearch) unconnectableRoom() error {
	for index, room := range search.req.Rooms {
		if search.visited[index] {
			continue
		}
		return fmt.Errorf("%w: RoomID %d has no routable edge", ErrUnconnectablePlacement, room.ID)
	}
	return ErrUnconnectablePlacement
}

// spliceStranded inserts one unvisited Room into an existing tree edge when
// every Room already in the tree is at its opening budget. The inserted Room
// must be able to host two openings. It is attached to the nearest in-tree
// Room, and that Room's tree neighbor which makes the cheaper Prim edge is
// the one moved onto the new Room. Degrees of the two Rooms that were already
// connected stay the same. If no such insertion exists, the placement cannot
// be connected under the separation rule. When RewireRoute is set, each
// candidate is reserved before it is kept, and a candidate that does not route
// is skipped.
func (search *primSearch) spliceStranded(edges *[]Connection) (bool, error) {
	if search.req.RewireRoute != nil {
		return search.spliceStrandedAgainstRouter(edges)
	}
	bestStranded := -1
	bestRoom := -1
	bestNeighbor := -1
	var bestEdge primEdgeCandidate
	found := false
	for stranded := range search.req.Rooms {
		if search.visited[stranded] || search.capacity[stranded] < 2 {
			continue
		}
		if err := search.ctx.Err(); err != nil {
			return false, err
		}
		nearest, nearestEdge, ok := search.nearestTreeRoom(stranded)
		if !ok {
			continue
		}
		neighbor, neighborOK := search.neighborToMove(stranded, nearest)
		if !neighborOK {
			continue
		}
		if !found || primEdgeLess(nearestEdge, bestEdge) {
			found = true
			bestStranded = stranded
			bestRoom = nearest
			bestNeighbor = neighbor
			bestEdge = nearestEdge
		}
	}
	if !found || !search.rewireTreeEdge(edges, bestRoom, bestNeighbor, bestStranded) {
		return false, nil
	}
	search.used[bestStranded] += 2
	search.visited[bestStranded] = true
	search.hasKey[bestStranded] = false
	if err := search.updateKeys(bestStranded); err != nil {
		return false, err
	}
	return true, nil
}

type primSplice struct {
	stranded int
	room     int
	neighbor int
	nearest  primEdgeCandidate
}

// spliceStrandedAgainstRouter tries splices in the same order as the
// opening-budget splice, cheapest nearest edge first. A splice whose two new
// corridors do not reserve is skipped. The first one that reserves is kept.
func (search *primSearch) spliceStrandedAgainstRouter(edges *[]Connection) (bool, error) {
	candidates := make([]primSplice, 0)
	for stranded := range search.req.Rooms {
		if search.visited[stranded] || search.capacity[stranded] < 2 {
			continue
		}
		if err := search.ctx.Err(); err != nil {
			return false, err
		}
		nearest, nearestEdge, ok := search.nearestTreeRoom(stranded)
		if !ok {
			continue
		}
		neighbor, neighborOK := search.neighborToMove(stranded, nearest)
		if !neighborOK {
			continue
		}
		candidates = append(candidates, primSplice{
			stranded: stranded, room: nearest, neighbor: neighbor, nearest: nearestEdge,
		})
	}
	sort.Slice(candidates, func(left, right int) bool {
		if primEdgeLess(candidates[left].nearest, candidates[right].nearest) {
			return true
		}
		if primEdgeLess(candidates[right].nearest, candidates[left].nearest) {
			return false
		}
		return candidates[left].stranded < candidates[right].stranded
	})
	for _, candidate := range candidates {
		roomID := search.req.Rooms[candidate.room].ID
		neighborID := search.req.Rooms[candidate.neighbor].ID
		strandedID := search.req.Rooms[candidate.stranded].ID
		if !search.treeEdgePresent(edges, roomID, neighborID) {
			continue
		}
		ok, err := search.req.RewireRoute(
			Connection{FromRoomID: roomID, ToRoomID: neighborID},
			Connection{FromRoomID: roomID, ToRoomID: strandedID},
			Connection{FromRoomID: neighborID, ToRoomID: strandedID},
		)
		if err != nil {
			return false, err
		}
		if !ok {
			continue
		}
		if !search.rewireTreeEdge(edges, candidate.room, candidate.neighbor, candidate.stranded) {
			return false, errGeneratorInvariant
		}
		search.used[candidate.stranded] += 2
		search.visited[candidate.stranded] = true
		search.hasKey[candidate.stranded] = false
		clear(search.infeasible)
		if err := search.updateKeys(candidate.stranded); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (search *primSearch) treeEdgePresent(edges *[]Connection, left, right RoomID) bool {
	for _, edge := range *edges {
		if connectionJoins(edge, left, right) {
			return true
		}
	}
	return false
}

func (search *primSearch) nearestTreeRoom(stranded int) (int, primEdgeCandidate, bool) {
	var best primEdgeCandidate
	found := false
	bestIndex := -1
	target := search.req.Rooms[stranded]
	for roomIndex, room := range search.req.Rooms {
		if !search.visited[roomIndex] {
			continue
		}
		candidate := primEdgeCandidate{
			connection:    Connection{FromRoomID: room.ID, ToRoomID: target.ID},
			fromIndex:     roomIndex,
			toIndex:       stranded,
			squaredWeight: squaredCenterDistance(search.centers[roomIndex], search.centers[stranded]),
			destination:   target.At,
		}
		if !found || primEdgeLess(candidate, best) {
			found = true
			best = candidate
			bestIndex = roomIndex
		}
	}
	return bestIndex, best, found
}

func (search *primSearch) neighborToMove(stranded, roomIndex int) (int, bool) {
	var best primEdgeCandidate
	found := false
	bestNeighbor := -1
	target := search.req.Rooms[stranded]
	for _, neighbor := range search.neighbors[roomIndex] {
		candidate := primEdgeCandidate{
			connection: Connection{
				FromRoomID: search.req.Rooms[neighbor].ID,
				ToRoomID:   target.ID,
			},
			fromIndex:     neighbor,
			toIndex:       stranded,
			squaredWeight: squaredCenterDistance(search.centers[neighbor], search.centers[stranded]),
			destination:   target.At,
		}
		if !found || primEdgeLess(candidate, best) {
			found = true
			best = candidate
			bestNeighbor = neighbor
		}
	}
	return bestNeighbor, found
}

func (search *primSearch) rewireTreeEdge(edges *[]Connection, roomIndex, neighbor, stranded int) bool {
	list := *edges
	roomID := search.req.Rooms[roomIndex].ID
	neighborID := search.req.Rooms[neighbor].ID
	strandedID := search.req.Rooms[stranded].ID
	edgeIndex := -1
	for index, edge := range list {
		if connectionJoins(edge, roomID, neighborID) {
			edgeIndex = index
			break
		}
	}
	if edgeIndex < 0 {
		return false
	}
	updated := make([]Connection, 0, len(list)+1)
	updated = append(updated, list[:edgeIndex]...)
	updated = append(updated,
		Connection{FromRoomID: roomID, ToRoomID: strandedID},
		Connection{FromRoomID: neighborID, ToRoomID: strandedID},
	)
	updated = append(updated, list[edgeIndex+1:]...)
	*edges = updated
	search.unlinkRooms(roomIndex, neighbor)
	search.linkRooms(roomIndex, stranded)
	search.linkRooms(neighbor, stranded)
	return true
}

func (search *primSearch) linkRooms(left, right int) {
	search.neighbors[left] = append(search.neighbors[left], right)
	search.neighbors[right] = append(search.neighbors[right], left)
}

func (search *primSearch) unlinkRooms(left, right int) {
	search.neighbors[left] = removeNeighbor(search.neighbors[left], right)
	search.neighbors[right] = removeNeighbor(search.neighbors[right], left)
}

func removeNeighbor(neighbors []int, target int) []int {
	for index, neighbor := range neighbors {
		if neighbor != target {
			continue
		}
		return append(neighbors[:index], neighbors[index+1:]...)
	}
	return neighbors
}

func connectionJoins(edge Connection, left, right RoomID) bool {
	return (edge.FromRoomID == left && edge.ToRoomID == right) ||
		(edge.FromRoomID == right && edge.ToRoomID == left)
}

// boundingBoxCenter returns the bounding-box center as the centroid of its
// Cells, that is, Origin + (dimension-1)/2. The alternative would be the area
// center, Origin + dimension/2, and the two produce different trees because the
// offset depends on each Room's dimension. A 1×1 Room weighs the same as the
// distance between anchors only with (dimension-1)/2, which makes the offset
// zero when the dimension is 1.
func boundingBoxCenter(room PlacedRoom) roomCenter {
	width := float64(room.Width)
	widthSpan := width - boundingBoxCellAdjustment
	halfWidthSpan := widthSpan / boundingBoxCenterDivisor
	originX := float64(room.Origin.X)
	centerX := originX + halfWidthSpan

	height := float64(room.Height)
	heightSpan := height - boundingBoxCellAdjustment
	halfHeightSpan := heightSpan / boundingBoxCenterDivisor
	originY := float64(room.Origin.Y)
	centerY := originY + halfHeightSpan

	return roomCenter{x: centerX, y: centerY}
}

// squaredCenterDistance keeps each product in its own statement. An expression
// of the form a*b+c may become a fused multiply-add on arm64 and must not on
// amd64, which would change the weight and every tie that depends on it.
func squaredCenterDistance(first, second roomCenter) float64 {
	deltaX := float64(first.x) - float64(second.x)
	deltaY := float64(first.y) - float64(second.y)
	deltaXSquared := float64(deltaX) * float64(deltaX)
	deltaYSquared := float64(deltaY) * float64(deltaY)
	return deltaXSquared + deltaYSquared
}

func primEdgeLess(first, second primEdgeCandidate) bool {
	// Edge weight is Euclidean. Because sqrt is strictly increasing for
	// non-negative values, comparing the square preserves exactly the same order
	// without introducing another rounding operation into the frozen path.
	if first.squaredWeight != second.squaredWeight {
		return first.squaredWeight < second.squaredWeight
	}
	if first.connection.FromRoomID != second.connection.FromRoomID {
		return first.connection.FromRoomID < second.connection.FromRoomID
	}
	if first.connection.ToRoomID != second.connection.ToRoomID {
		return first.connection.ToRoomID < second.connection.ToRoomID
	}

	// In a simple graph, FromRoomID and ToRoomID already identify the edge and
	// make this third level unreachable in Prim. The same comparator selects
	// discarded edges in the cycle phase, so Cell Y/X order stays.
	if first.destination.Y != second.destination.Y {
		return first.destination.Y < second.destination.Y
	}
	return first.destination.X < second.destination.X
}
