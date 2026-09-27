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

// applyTopologyOptions fixes the normative section 8.1 order: roles are
// projected onto the backbone tree before any shortcut is added. Neither phase
// receives or consumes a random stream.
func applyTopologyOptions(
	ctx context.Context,
	rooms []PlacedRoom,
	backbone []Connection,
	requests []RoomRoleRequest,
	extraEdgeCount uint32,
) ([]*RoomRole, []Connection, error) {
	roles, err := assignRoomRoles(ctx, rooms, backbone, requests)
	if err != nil {
		return nil, nil, err
	}
	connections, err := addExtraConnections(ctx, rooms, backbone, extraEdgeCount)
	if err != nil {
		return nil, nil, err
	}
	return roles, connections, nil
}

// assignRoomRoles assigns roles in declared order, reserving RoomID 0 for Start
// when that request exists. Early reservation is the conservative reading of
// section 8.1: Start receives RoomID 0 even when its entry appears after Boss or
// Treasure; the remaining entries preserve caller order and can never occupy
// that Room.
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

	hasStart := false
	hasBoss := false
	needsDistances := false
	for _, request := range requests {
		switch request.Role {
		case RoomRoleStart:
			hasStart = true
		case RoomRoleBoss:
			hasBoss = true
			needsDistances = true
		case RoomRoleTreasure:
			needsDistances = true
		}
	}
	if hasBoss && !hasStart {
		return nil, errBossRoleWithoutStart
	}

	startIndex := topologyFirstRoomIndex
	if rooms[startIndex].ID != RoomID(topologyFirstRoomIndex) {
		return nil, errTopologyUnknownRoom
	}
	if hasStart {
		roles[startIndex] = roomRolePointer(RoomRoleStart)
	}

	// Section 8.1 measures Treasure as "farthest from Start" but requires Start
	// only for Boss; Config validation accepts Treasure alone. In that case the
	// origin remains RoomID 0, the first accepted Room, exactly the Room Start
	// would occupy. This conservative reading becomes a frozen contract: without
	// it, "farthest from Start" would have no reference and assignment would be
	// undefined.
	var distances []float64
	if needsDistances {
		var err error
		distances, err = weightedTreeDistances(ctx, rooms, backbone, startIndex)
		if err != nil {
			return nil, err
		}
	}

	for _, request := range requests {
		if err := ctx.Err(); err != nil {
			return nil, err
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return roles, nil
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
// sorting, or traversing the discarded set, as specified by section 8.1.
func addExtraConnections(
	ctx context.Context,
	rooms []PlacedRoom,
	backbone []Connection,
	extraEdgeCount uint32,
) ([]Connection, error) {
	ctx = topologyContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if extraEdgeCount == topologyNoExtraEdges {
		return backbone, nil
	}

	roomCount := len(rooms)
	backbonePairs := make([]bool, roomCount*roomCount)
	for _, connection := range backbone {
		fromIndex := roomIndexByID(rooms, connection.FromRoomID)
		toIndex := roomIndexByID(rooms, connection.ToRoomID)
		if fromIndex == topologyNoRoomIndex || toIndex == topologyNoRoomIndex {
			return nil, errTopologyUnknownRoom
		}
		backbonePairs[fromIndex*roomCount+toIndex] = true
		backbonePairs[toIndex*roomCount+fromIndex] = true
	}

	centers := make([]roomCenter, roomCount)
	for roomIndex, room := range rooms {
		centers[roomIndex] = boundingBoxCenter(room)
	}
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
	sort.Slice(candidates, func(first, second int) bool {
		return primEdgeLess(candidates[first], candidates[second])
	})

	selectedCount := len(candidates)
	if uint64(selectedCount) > uint64(extraEdgeCount) {
		selectedCount = int(extraEdgeCount)
	}
	connections := make([]Connection, 0, len(backbone)+selectedCount)
	connections = append(connections, backbone...)
	for candidateIndex := topologyFirstRoomIndex; candidateIndex < selectedCount; candidateIndex++ {
		connections = append(connections, candidates[candidateIndex].connection)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return connections, nil
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
