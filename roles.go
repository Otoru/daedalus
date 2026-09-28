package daedalus

import (
	"context"
	"errors"
	"math"
	"sort"
)

const (
	// topologyFirstRoomIndex is RoomID 0's canonical position, the root of
	// thematic distances and the first Room accepted by placement.
	topologyFirstRoomIndex = 0
	// topologyNextRoomOffset advances to the next pair in the complete graph
	// without producing a self-edge.
	topologyNextRoomOffset = 1
	// topologyUndirectedEdgeDivisor removes the duplicate orientations when
	// sizing the complete simple graph.
	topologyUndirectedEdgeDivisor = 2
	// topologyNoExtraEdges completely disables the shortcut phase.
	topologyNoExtraEdges uint32 = 0
	// topologyNoRoomIndex represents the absence of a thematic candidate.
	topologyNoRoomIndex = -1
)

var (
	// errBossRoleWithoutStart signals a broken internal invariant: Config
	// validation must reject Boss without Start before this phase.
	errBossRoleWithoutStart = errors.New("assignment of the Boss role reached without a Start request")
	// errTopologyUnknownRoom signals a Connection with a nonexistent RoomID that
	// escaped Connector validation.
	errTopologyUnknownRoom = errors.New("topology contains unknown RoomID")
	// errTopologyDisconnected signals a backbone tree that escaped Connector
	// validation without reaching every Room.
	errTopologyDisconnected = errors.New("topologia de backbone desconectada")
)

type weightedRoomNeighbor struct {
	roomIndex int
	weight    float64
}

// applyTopologyOptions assigns roles on the backbone tree before any shortcut
// is added. Neither phase receives or consumes a random stream.
func applyTopologyOptions(
	ctx context.Context,
	rooms []PlacedRoom,
	backbone []Connection,
	requests []RoomRoleRequest,
	extraEdgeCount uint32,
	gridWidth uint32,
	gridHeight uint32,
	corridorWidth uint32,
) ([]*RoomRole, []Connection, error) {
	roles, err := assignRoomRoles(ctx, rooms, backbone, requests)
	if err != nil {
		return nil, nil, err
	}
	connections, err := addExtraConnections(
		ctx, rooms, backbone, extraEdgeCount, gridWidth, gridHeight, corridorWidth,
	)
	if err != nil {
		return nil, nil, err
	}
	return roles, connections, nil
}

// assignRoomRoles assigns roles in declared order, reserving RoomID 0 for Start
// when that request exists. Start receives RoomID 0 even when its entry appears
// after Boss or Treasure; the remaining entries preserve caller order and can
// never occupy that Room.
func assignRoomRoles(
	ctx context.Context,
	rooms []PlacedRoom,
	backbone []Connection,
	requests []RoomRoleRequest,
) ([]*RoomRole, error) {
	ctx = topologyContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	roles := make([]*RoomRole, len(rooms))
	if len(requests) == 0 || len(rooms) == 0 {
		return roles, nil
	}

	summary := summarizeRoleRequests(requests)
	if summary.hasBoss && !summary.hasStart {
		return nil, errBossRoleWithoutStart
	}

	startIndex := topologyFirstRoomIndex
	if rooms[startIndex].ID != RoomID(topologyFirstRoomIndex) {
		return nil, errTopologyUnknownRoom
	}
	if summary.hasStart {
		roles[startIndex] = roomRolePointer(RoomRoleStart)
	}

	// Treasure is the unassigned Room farthest from Start. Boss is the only role
	// that requires a Start request; Treasure alone is accepted. The distance
	// origin is then RoomID 0, the first accepted Room, the same Room Start would
	// occupy. Without that origin, farthest-from-Start would have no reference
	// and the assignment would be undefined.
	var distances []float64
	if summary.needsDistances {
		var err error
		distances, err = weightedTreeDistances(ctx, rooms, backbone, startIndex)
		if err != nil {
			return nil, err
		}
	}

	if err := assignNonStartRoles(ctx, rooms, roles, distances, requests); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return roles, nil
}

// roleRequestSummary records which thematic roles a request list asks for.
// Distances are required for Boss and for Treasure; Start is optional for
// Treasure and mandatory for Boss.
type roleRequestSummary struct {
	hasStart       bool
	hasBoss        bool
	needsDistances bool
}

func summarizeRoleRequests(requests []RoomRoleRequest) roleRequestSummary {
	var summary roleRequestSummary
	for _, request := range requests {
		switch request.Role {
		case RoomRoleStart:
			summary.hasStart = true
		case RoomRoleBoss:
			summary.hasBoss = true
			summary.needsDistances = true
		case RoomRoleTreasure:
			summary.needsDistances = true
		}
	}
	return summary
}

// assignNonStartRoles walks requests in caller order. Start was reserved on
// RoomID 0 before this walk, so Boss and Treasure can never occupy it.
func assignNonStartRoles(
	ctx context.Context,
	rooms []PlacedRoom,
	roles []*RoomRole,
	distances []float64,
	requests []RoomRoleRequest,
) error {
	for _, request := range requests {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch request.Role {
		case RoomRoleStart:
			continue
		case RoomRoleBoss:
			assignFarthestRole(rooms, roles, distances, RoomRoleBoss)
		case RoomRoleTreasure:
			for assigned := uint32(0); assigned < request.Count; assigned++ {
				if !assignFarthestRole(rooms, roles, distances, RoomRoleTreasure) {
					break
				}
			}
		}
	}
	return nil
}

func weightedTreeDistances(
	ctx context.Context,
	rooms []PlacedRoom,
	backbone []Connection,
	startIndex int,
) ([]float64, error) {
	centers := make([]roomCenter, len(rooms))
	for roomIndex, room := range rooms {
		centers[roomIndex] = boundingBoxCenter(room)
	}

	adjacency, err := weightedRoomAdjacency(rooms, backbone, centers)
	if err != nil {
		return nil, err
	}

	distances := make([]float64, len(rooms))
	visited := make([]bool, len(rooms))
	queue := make([]int, 0, len(rooms))
	visited[startIndex] = true
	queue = append(queue, startIndex)
	for queueIndex := topologyFirstRoomIndex; queueIndex < len(queue); queueIndex++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		currentIndex := queue[queueIndex]
		for _, neighbor := range adjacency[currentIndex] {
			if visited[neighbor.roomIndex] {
				continue
			}
			pathDistance := float64(distances[currentIndex])
			pathDistance += float64(neighbor.weight)
			distances[neighbor.roomIndex] = pathDistance
			visited[neighbor.roomIndex] = true
			queue = append(queue, neighbor.roomIndex)
		}
	}
	for _, reached := range visited {
		if !reached {
			return nil, errTopologyDisconnected
		}
	}
	return distances, nil
}

// weightedRoomAdjacency builds the undirected tree used to measure thematic
// distance. Edge weight is the Euclidean distance between bounding-box centers.
func weightedRoomAdjacency(
	rooms []PlacedRoom,
	backbone []Connection,
	centers []roomCenter,
) ([][]weightedRoomNeighbor, error) {
	adjacency := make([][]weightedRoomNeighbor, len(rooms))
	for _, connection := range backbone {
		fromIndex := roomIndexByID(rooms, connection.FromRoomID)
		toIndex := roomIndexByID(rooms, connection.ToRoomID)
		if fromIndex == topologyNoRoomIndex || toIndex == topologyNoRoomIndex {
			return nil, errTopologyUnknownRoom
		}
		squaredWeight := squaredCenterDistance(centers[fromIndex], centers[toIndex])
		weight := math.Sqrt(float64(squaredWeight))
		adjacency[fromIndex] = append(adjacency[fromIndex], weightedRoomNeighbor{roomIndex: toIndex, weight: weight})
		adjacency[toIndex] = append(adjacency[toIndex], weightedRoomNeighbor{roomIndex: fromIndex, weight: weight})
	}
	return adjacency, nil
}

func assignFarthestRole(
	rooms []PlacedRoom,
	roles []*RoomRole,
	distances []float64,
	role RoomRole,
) bool {
	selectedIndex := topologyNoRoomIndex
	for roomIndex, room := range rooms {
		if roles[roomIndex] != nil {
			continue
		}
		if selectedIndex == topologyNoRoomIndex ||
			distances[roomIndex] > distances[selectedIndex] ||
			(distances[roomIndex] == distances[selectedIndex] && room.ID < rooms[selectedIndex].ID) {
			selectedIndex = roomIndex
		}
	}
	if selectedIndex == topologyNoRoomIndex {
		return false
	}
	roles[selectedIndex] = roomRolePointer(role)
	return true
}

func roomRolePointer(role RoomRole) *RoomRole {
	assigned := role
	return &assigned
}

// addExtraConnections adds the shortest complete-graph edges not belonging to
// the backbone. With a zero count, it returns immediately without forming,
// sorting, or traversing the discarded set.
func addExtraConnections(
	ctx context.Context,
	rooms []PlacedRoom,
	backbone []Connection,
	extraEdgeCount uint32,
	gridWidth uint32,
	gridHeight uint32,
	corridorWidth uint32,
) ([]Connection, error) {
	ctx = topologyContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if extraEdgeCount == topologyNoExtraEdges {
		return backbone, nil
	}

	backbonePairs, err := backbonePairFlags(rooms, backbone)
	if err != nil {
		return nil, err
	}

	centers := make([]roomCenter, len(rooms))
	for roomIndex, room := range rooms {
		centers[roomIndex] = boundingBoxCenter(room)
	}
	candidates, err := nonBackboneEdgeCandidates(ctx, rooms, centers, backbonePairs)
	if err != nil {
		return nil, err
	}
	sort.Slice(candidates, func(first, second int) bool {
		return primEdgeLess(candidates[first], candidates[second])
	})

	width := corridorWidth
	if width == 0 {
		width = 1
	}
	capacity := make([]int, len(rooms))
	used := make([]int, len(rooms))
	for roomIndex, room := range rooms {
		capacity[roomIndex] = roomOpeningCapacity(placedRoomFootprint(room), gridWidth, gridHeight, width)
	}
	for _, connection := range backbone {
		fromIndex := roomIndexByID(rooms, connection.FromRoomID)
		toIndex := roomIndexByID(rooms, connection.ToRoomID)
		if fromIndex == topologyNoRoomIndex || toIndex == topologyNoRoomIndex {
			return nil, errTopologyUnknownRoom
		}
		used[fromIndex]++
		used[toIndex]++
	}

	connections := make([]Connection, 0, len(backbone)+int(extraEdgeCount))
	connections = append(connections, backbone...)
	added := 0
	for candidateIndex := range candidates {
		if uint64(added) >= uint64(extraEdgeCount) {
			break
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		candidate := candidates[candidateIndex]
		fromIndex := roomIndexByID(rooms, candidate.connection.FromRoomID)
		toIndex := roomIndexByID(rooms, candidate.connection.ToRoomID)
		if fromIndex == topologyNoRoomIndex || toIndex == topologyNoRoomIndex {
			return nil, errTopologyUnknownRoom
		}
		if used[fromIndex] >= capacity[fromIndex] || used[toIndex] >= capacity[toIndex] {
			continue
		}
		connections = append(connections, candidate.connection)
		used[fromIndex]++
		used[toIndex]++
		added++
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return connections, nil
}

// backbonePairFlags records both orientations of every backbone edge so the
// shortcut scan can skip them without a second search.
func backbonePairFlags(rooms []PlacedRoom, backbone []Connection) ([]bool, error) {
	roomCount := len(rooms)
	pairs := make([]bool, roomCount*roomCount)
	for _, connection := range backbone {
		fromIndex := roomIndexByID(rooms, connection.FromRoomID)
		toIndex := roomIndexByID(rooms, connection.ToRoomID)
		if fromIndex == topologyNoRoomIndex || toIndex == topologyNoRoomIndex {
			return nil, errTopologyUnknownRoom
		}
		pairs[fromIndex*roomCount+toIndex] = true
		pairs[toIndex*roomCount+fromIndex] = true
	}
	return pairs, nil
}

// nonBackboneEdgeCandidates lists complete-graph edges absent from the
// backbone, oriented from the lower RoomID to the higher one. The shortest of
// these, under the same Prim tie-break, are appended after roles have been
// assigned, and only up to ExtraEdgeCount.
func nonBackboneEdgeCandidates(
	ctx context.Context,
	rooms []PlacedRoom,
	centers []roomCenter,
	backbonePairs []bool,
) ([]primEdgeCandidate, error) {
	roomCount := len(rooms)
	maximumCandidateCount := roomCount * (roomCount - topologyNextRoomOffset) / topologyUndirectedEdgeDivisor
	candidates := make([]primEdgeCandidate, 0, maximumCandidateCount)
	for firstIndex := topologyFirstRoomIndex; firstIndex < roomCount; firstIndex++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for secondIndex := firstIndex + topologyNextRoomOffset; secondIndex < roomCount; secondIndex++ {
			if backbonePairs[firstIndex*roomCount+secondIndex] {
				continue
			}
			fromIndex := firstIndex
			toIndex := secondIndex
			if rooms[toIndex].ID < rooms[fromIndex].ID {
				fromIndex, toIndex = toIndex, fromIndex
			}
			candidates = append(candidates, primEdgeCandidate{
				connection: Connection{
					FromRoomID: rooms[fromIndex].ID,
					ToRoomID:   rooms[toIndex].ID,
				},
				toIndex:       toIndex,
				squaredWeight: squaredCenterDistance(centers[fromIndex], centers[toIndex]),
				// A discarded edge's canonical orientation is from lower to higher
				// RoomID; this is therefore the destination Cell used by Prim's
				// frozen third tie-break.
				destination: rooms[toIndex].At,
			})
		}
	}
	return candidates, nil
}

func roomIndexByID(rooms []PlacedRoom, id RoomID) int {
	for roomIndex, room := range rooms {
		if room.ID == id {
			return roomIndex
		}
	}
	return topologyNoRoomIndex
}

func topologyContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
