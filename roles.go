package daedalus

import (
	"context"
	"errors"
	"fmt"
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
	maxRoomEdges uint32,
	tryRoute func(from, to RoomID) (bool, error),
) ([]*RoomRole, []Connection, error) {
	roles, err := assignRoomRolesWithConstraints(ctx, rooms, backbone, requests, gridWidth, gridHeight, corridorWidth, maxRoomEdges)
	if err != nil {
		return nil, nil, err
	}
	roleCeilings := roleCeilingsForAssignedRooms(rooms, roles, requests, gridWidth, gridHeight, corridorWidth, maxRoomEdges)
	connections, err := addExtraConnectionsWithRoleCeilings(
		ctx, rooms, backbone, extraEdgeCount, gridWidth, gridHeight, corridorWidth, maxRoomEdges, tryRoute,
		roleCeilings,
	)
	if err != nil {
		return nil, nil, err
	}
	if err := validateRoleDegrees(rooms, connections, roles, roleCeilings); err != nil {
		return nil, nil, err
	}
	return roles, connections, nil
}

func validateRoleDegrees(rooms []PlacedRoom, connections []Connection, roles []*RoomRole, ceilings []uint32) error {
	if len(ceilings) != len(rooms) {
		return nil
	}
	degrees := connectionDegrees(rooms, connections)
	for index, ceiling := range ceilings {
		if ceiling != 0 && roles[index] != nil && uint32(degrees[index]) > ceiling {
			return fmt.Errorf("%w: role=%d room=%d degree=%d ceiling=%d", errGeneratorInvariant, *roles[index], rooms[index].ID, degrees[index], ceiling)
		}
	}
	return nil
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
	return assignRoomRolesWithConstraints(ctx, rooms, backbone, requests, 0, 0, 0, 0)
}

func assignRoomRolesWithConstraints(
	ctx context.Context,
	rooms []PlacedRoom,
	backbone []Connection,
	requests []RoomRoleRequest,
	gridWidth, gridHeight, corridorWidth, globalCeiling uint32,
) ([]*RoomRole, error) {
	return assignRoomRolesWithDegreeConnections(ctx, rooms, backbone, backbone, requests, gridWidth, gridHeight, corridorWidth, globalCeiling)
}

func assignRoomRolesWithDegreeConnections(
	ctx context.Context,
	rooms []PlacedRoom,
	backbone []Connection,
	degreeConnections []Connection,
	requests []RoomRoleRequest,
	gridWidth, gridHeight, corridorWidth, globalCeiling uint32,
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
	degree := connectionDegrees(rooms, degreeConnections)
	roleCeilingsByRole := roleCeilingsByRequestedRole(rooms, requests, gridWidth, gridHeight, corridorWidth, globalCeiling)
	roleCeilings := roleCeilingsByRole[RoomRoleStart]

	startIndex := topologyFirstRoomIndex
	if rooms[startIndex].ID != RoomID(topologyFirstRoomIndex) {
		return nil, errTopologyUnknownRoom
	}
	if summary.hasStart {
		if len(roleCeilings) == len(rooms) && roleCeilings[startIndex] != 0 && uint32(degree[startIndex]) > roleCeilings[startIndex] {
			return nil, fmt.Errorf("%w: role=%d ordinal=1 ceiling=%d observed_degree=%d", ErrUnsatisfiedRoleConstraint, RoomRoleStart, roleCeilings[startIndex], degree[startIndex])
		}
		roles[startIndex] = roomRolePointer(RoomRoleStart)
	}

	// Boss is the only role that requires a Start request. Treasure alone is
	// accepted. Weighted distances are rooted at RoomID 0, the first accepted
	// Room, which is the Room Start occupies when it was requested. That root
	// is also the fallback origin for the first Treasure when no anchor exists.
	var distances []float64
	if summary.needsDistances {
		var err error
		distances, err = weightedTreeDistances(ctx, rooms, backbone, startIndex)
		if err != nil {
			return nil, err
		}
	}

	if err := assignNonStartRoles(ctx, rooms, backbone, roles, distances, requests, degree, roleCeilingsByRole); err != nil {
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
	backbone []Connection,
	roles []*RoomRole,
	distances []float64,
	requests []RoomRoleRequest,
	degree []int,
	roleCeilingsByRole map[RoomRole][]uint32,
) error {
	for requestIndex, request := range requests {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch request.Role {
		case RoomRoleStart:
			continue
		case RoomRoleBoss:
			roleCeilings := roleCeilingsByRole[request.Role]
			if !assignFarthestRoleConstrained(rooms, roles, distances, RoomRoleBoss, degree, roleCeilings, request.MaxRoomEdges != 0) && hasUnassignedRoleRoom(roles) && request.MaxRoomEdges != 0 {
				return roleConstraintError(request, uint32(requestIndex), degree, roleCeilings)
			}
		case RoomRoleTreasure:
			if err := assignSpreadTreasures(ctx, rooms, backbone, roles, distances, request.Count, degree, roleCeilingsByRole[request.Role], request); err != nil {
				return err
			}
		}
	}
	return nil
}

// assignSpreadTreasures places Count Treasure Rooms by farthest-point sampling.
// Anchors are the Rooms that already hold a role: Start and Boss when those
// requests preceded this one, plus every Treasure already placed. Each pick is
// the unassigned Room whose minimum weighted path distance to any anchor is
// greatest, and that Room joins the anchors. An empty anchor set has no such
// minimum, so the first Treasure uses the distances from RoomID 0, the same
// farthest-from-Start rule. A Count past the free Rooms stops once every Room
// is taken; the shortfall is not an error, and a taken Room is never reused.
func assignSpreadTreasures(
	ctx context.Context,
	rooms []PlacedRoom,
	backbone []Connection,
	roles []*RoomRole,
	originDistances []float64,
	count uint32,
	degree []int,
	roleCeilings []uint32,
	request RoomRoleRequest,
) error {
	for assigned := uint32(0); assigned < count; assigned++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		distances, err := treasureReferenceDistances(ctx, rooms, backbone, roles, originDistances)
		if err != nil {
			return err
		}
		if !assignFarthestRoleConstrained(rooms, roles, distances, RoomRoleTreasure, degree, roleCeilings, request.MaxRoomEdges != 0) {
			if request.MaxRoomEdges != 0 && hasUnassignedRoleRoom(roles) {
				return roleConstraintError(request, assigned, degree, roleCeilings)
			}
			break
		}
	}
	return nil
}

// treasureReferenceDistances is the distance of every Room to the nearest
// assigned role. With nothing assigned yet, it returns the distances from
// RoomID 0 so the first Treasure stays defined.
func treasureReferenceDistances(
	ctx context.Context,
	rooms []PlacedRoom,
	backbone []Connection,
	roles []*RoomRole,
	originDistances []float64,
) ([]float64, error) {
	anchors := assignedRoleIndexes(roles)
	if len(anchors) == 0 {
		return originDistances, nil
	}
	return minimumWeightedTreeDistances(ctx, rooms, backbone, anchors)
}

func assignedRoleIndexes(roles []*RoomRole) []int {
	indexes := make([]int, 0)
	for roomIndex, role := range roles {
		if role != nil {
			indexes = append(indexes, roomIndex)
		}
	}
	return indexes
}

// minimumWeightedTreeDistances is the minimum, over the anchors, of the
// weighted backbone path from that anchor. Each anchor is measured with
// weightedTreeDistances, so Treasure and Boss share one distance. Anchors are
// visited in index order. The minimum itself does not depend on that order.
func minimumWeightedTreeDistances(
	ctx context.Context,
	rooms []PlacedRoom,
	backbone []Connection,
	anchors []int,
) ([]float64, error) {
	var minimum []float64
	for _, anchorIndex := range anchors {
		distances, err := weightedTreeDistances(ctx, rooms, backbone, anchorIndex)
		if err != nil {
			return nil, err
		}
		if minimum == nil {
			minimum = distances
			continue
		}
		for roomIndex := range minimum {
			if distances[roomIndex] < minimum[roomIndex] {
				minimum[roomIndex] = distances[roomIndex]
			}
		}
	}
	return minimum, nil
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

func assignFarthestRoleConstrained(rooms []PlacedRoom, roles []*RoomRole, distances []float64, role RoomRole, degree []int, ceilings []uint32, constrained bool) bool {
	selectedIndex := topologyNoRoomIndex
	for roomIndex, room := range rooms {
		if roles[roomIndex] != nil {
			continue
		}
		if constrained && ceilings[roomIndex] != 0 && uint32(degree[roomIndex]) > ceilings[roomIndex] {
			continue
		}
		if selectedIndex == topologyNoRoomIndex || distances[roomIndex] > distances[selectedIndex] || (distances[roomIndex] == distances[selectedIndex] && room.ID < rooms[selectedIndex].ID) {
			selectedIndex = roomIndex
		}
	}
	if selectedIndex == topologyNoRoomIndex {
		return false
	}
	roles[selectedIndex] = roomRolePointer(role)
	return true
}

func hasUnassignedRoleRoom(roles []*RoomRole) bool {
	for _, role := range roles {
		if role == nil {
			return true
		}
	}
	return false
}

func connectionDegrees(rooms []PlacedRoom, connections []Connection) []int {
	degrees := make([]int, len(rooms))
	for _, connection := range connections {
		from := roomIndexByID(rooms, connection.FromRoomID)
		to := roomIndexByID(rooms, connection.ToRoomID)
		if from >= 0 {
			degrees[from]++
		}
		if to >= 0 {
			degrees[to]++
		}
	}
	return degrees
}

func roleCeilingsByRequestedRole(rooms []PlacedRoom, requests []RoomRoleRequest, gridWidth, gridHeight, corridorWidth, globalCeiling uint32) map[RoomRole][]uint32 {
	result := make(map[RoomRole][]uint32, len(requests))
	for _, request := range requests {
		ceilings := make([]uint32, len(rooms))
		if request.MaxRoomEdges != 0 {
			for index, room := range rooms {
				ceiling := request.MaxRoomEdges
				if globalCeiling != 0 && globalCeiling < ceiling {
					ceiling = globalCeiling
				}
				geometry := roomOpeningCapacity(placedRoomFootprint(room), gridWidth, gridHeight, corridorWidth)
				if geometry < int(ceiling) {
					ceiling = uint32(geometry)
				}
				ceilings[index] = ceiling
			}
		}
		result[request.Role] = ceilings
	}
	return result
}

func roleCeilingsForAssignedRooms(rooms []PlacedRoom, roles []*RoomRole, requests []RoomRoleRequest, gridWidth, gridHeight, corridorWidth, globalCeiling uint32) []uint32 {
	ceilings := make([]uint32, len(rooms))
	for index, role := range roles {
		if role == nil {
			continue
		}
		for _, request := range requests {
			if request.Role != *role || request.MaxRoomEdges == 0 {
				continue
			}
			ceiling := request.MaxRoomEdges
			if globalCeiling != 0 && globalCeiling < ceiling {
				ceiling = globalCeiling
			}
			geometry := roomOpeningCapacity(placedRoomFootprint(rooms[index]), gridWidth, gridHeight, corridorWidth)
			if geometry < int(ceiling) {
				ceiling = uint32(geometry)
			}
			ceilings[index] = ceiling
		}
	}
	return ceilings
}

func roleConstraintError(request RoomRoleRequest, ordinal uint32, degrees []int, ceilings []uint32) error {
	minimum := int(^uint(0) >> 1)
	for index, degree := range degrees {
		if ceilings[index] != 0 && degree < minimum {
			minimum = degree
		}
	}
	return fmt.Errorf("%w: role=%d ordinal=%d ceiling=%d smallest_observed_degree=%d", ErrUnsatisfiedRoleConstraint, request.Role, ordinal+1, request.MaxRoomEdges, minimum)
}

func roomRolePointer(role RoomRole) *RoomRole {
	assigned := role
	return &assigned
}

// addExtraConnections adds the shortest complete-graph edges not belonging to
// the backbone. With a zero count, it returns immediately without forming,
// sorting, or traversing the discarded set. maxRoomEdges is the caller ceiling:
// zero leaves the geometric opening budget unchanged, and a positive value
// skips a shortcut that would put either Room past that many Corridors.
func addExtraConnections(
	ctx context.Context,
	rooms []PlacedRoom,
	backbone []Connection,
	extraEdgeCount uint32,
	gridWidth uint32,
	gridHeight uint32,
	corridorWidth uint32,
	maxRoomEdges uint32,
	tryRoute func(from, to RoomID) (bool, error),
) ([]Connection, error) {
	return addExtraConnectionsWithRoleCeilings(ctx, rooms, backbone, extraEdgeCount, gridWidth, gridHeight, corridorWidth, maxRoomEdges, tryRoute, nil)
}

func addExtraConnectionsWithRoleCeilings(
	ctx context.Context,
	rooms []PlacedRoom,
	backbone []Connection,
	extraEdgeCount uint32,
	gridWidth uint32,
	gridHeight uint32,
	corridorWidth uint32,
	maxRoomEdges uint32,
	tryRoute func(from, to RoomID) (bool, error),
	roleCeilings []uint32,
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
		capacity[roomIndex] = roomOpeningBudget(placedRoomFootprint(room), gridWidth, gridHeight, width, maxRoomEdges)
		if len(roleCeilings) == len(rooms) && roleCeilings[roomIndex] != 0 && int(roleCeilings[roomIndex]) < capacity[roomIndex] {
			capacity[roomIndex] = int(roleCeilings[roomIndex])
		}
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
		if tryRoute != nil {
			ok, routeErr := tryRoute(candidate.connection.FromRoomID, candidate.connection.ToRoomID)
			if routeErr != nil {
				return nil, routeErr
			}
			if !ok {
				continue
			}
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
