package daedalus

import "context"

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
	// during best-key updates, as specified by section 11.
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
		selected := search.selectNextEdge()
		edges = append(edges, selected.connection)
		search.visited[selected.toIndex] = true
		search.hasKey[selected.toIndex] = false
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
}

func newPrimSearch(req ConnectionRequest, ctx context.Context) *primSearch {
	roomCount := len(req.Rooms)
	centers := make([]roomCenter, roomCount)
	for index, room := range req.Rooms {
		centers[index] = boundingBoxCenter(room)
	}
	visited := make([]bool, roomCount)
	visited[primStartingRoomIndex] = true
	return &primSearch{
		req:      req,
		ctx:      ctx,
		centers:  centers,
		visited:  visited,
		bestKeys: make([]primEdgeCandidate, roomCount),
		hasKey:   make([]bool, roomCount),
	}
}

func (search *primSearch) updateKeys(fromIndex int) error {
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

		candidate := primEdgeCandidate{
			connection: Connection{
				FromRoomID: from.ID,
				ToRoomID:   to.ID,
			},
			toIndex:       toIndex,
			squaredWeight: squaredCenterDistance(search.centers[fromIndex], search.centers[toIndex]),
			// Section 8's "destination Cell" is interpreted as the destination
			// Room's At anchor, the Cell supplied by the PlacedRoom contract for
			// canonical tie-breaks.
			destination: to.At,
		}
		if !search.hasKey[toIndex] || primEdgeLess(candidate, search.bestKeys[toIndex]) {
			search.bestKeys[toIndex] = candidate
			search.hasKey[toIndex] = true
		}
	}
	return nil
}

func (search *primSearch) selectNextEdge() primEdgeCandidate {
	var selected primEdgeCandidate
	hasSelected := false
	for roomIndex := range search.req.Rooms {
		if search.visited[roomIndex] || !search.hasKey[roomIndex] {
			continue
		}
		candidate := search.bestKeys[roomIndex]
		if !hasSelected || primEdgeLess(candidate, selected) {
			selected = candidate
			hasSelected = true
		}
	}
	return selected
}

// boundingBoxCenter returns the bounding-box center as the centroid of its
// Cells, that is, Origin + (dimension-1)/2. The alternative would be the area
// center, Origin + dimension/2, and the two produce different trees because the
// offset depends on each Room's dimension. Section 8 resolves the ambiguity by
// stating that, for 1×1 Rooms, the weight "matches the distance between
// anchors": that is true only with (dimension-1)/2, which makes the offset zero
// when the dimension is 1.
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

func squaredCenterDistance(first, second roomCenter) float64 {
	deltaX := float64(first.x) - float64(second.x)
	deltaY := float64(first.y) - float64(second.y)
	deltaXSquared := float64(deltaX) * float64(deltaX)
	deltaYSquared := float64(deltaY) * float64(deltaY)
	return deltaXSquared + deltaYSquared
}

func primEdgeLess(first, second primEdgeCandidate) bool {
	// Section 8 defines Euclidean weight. Because sqrt is strictly increasing for
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
	// make this third level unreachable in Prim. Section 8 freezes it because the
	// same comparator is reused to select discarded edges in the cycle phase;
	// therefore we preserve Cell Y/X order.
	if first.destination.Y != second.destination.Y {
		return first.destination.Y < second.destination.Y
	}
	return first.destination.X < second.destination.X
}
