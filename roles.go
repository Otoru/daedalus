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
type roleGridConstraints struct {
	Width         uint32
	Height        uint32
	CorridorWidth uint32
	MaxRoomEdges  uint32
}

type topologyOptionsRequest struct {
	Rooms          []PlacedRoom
	Backbone       []Connection
	Requests       []RoomRoleRequest
	ExtraEdgeCount uint32
	Constraints    roleGridConstraints
	TryRoute       func(from, to RoomID) (bool, error)
}

type extraConnectionsRequest struct {
	Rooms          []PlacedRoom
	Backbone       []Connection
	ExtraEdgeCount uint32
	Constraints    roleGridConstraints
	TryRoute       func(from, to RoomID) (bool, error)
	RoleCeilings   []uint32
}

type roleAssignmentState struct {
	rooms              []PlacedRoom
	backbone           []Connection
	roles              []*RoomRole
	distances          []float64
	degree             []int
	roleCeilingsByRole map[RoomRole][]uint32
}

func applyTopologyOptions(ctx context.Context, request topologyOptionsRequest) ([]*RoomRole, []Connection, error) {
	roles, err := assignRoomRolesWithConstraints(ctx, request.Rooms, request.Backbone, request.Requests, request.Constraints)
	if err != nil {
		return nil, nil, err
	}
	roleCeilings := roleCeilingsForAssignedRooms(request.Rooms, roles, request.Requests, request.Constraints)
	connections, err := addExtraConnectionsWithRoleCeilings(ctx, extraConnectionsRequest{
		Rooms: request.Rooms, Backbone: request.Backbone, ExtraEdgeCount: request.ExtraEdgeCount,
		Constraints: request.Constraints, TryRoute: request.TryRoute, RoleCeilings: roleCeilings,
	})
	if err != nil {
		return nil, nil, err
	}
	if err := validateRoleDegrees(request.Rooms, connections, roles, roleCeilings); err != nil {
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
	return assignRoomRolesWithConstraints(ctx, rooms, backbone, requests, roleGridConstraints{})
}

func assignRoomRolesWithConstraints(
	ctx context.Context,
	rooms []PlacedRoom,
	backbone []Connection,
	requests []RoomRoleRequest,
	constraints roleGridConstraints,
) ([]*RoomRole, error) {
	return assignRoomRolesWithDegreeConnections(ctx, rooms, backbone, backbone, requests, constraints)
}

func assignRoomRolesWithDegreeConnections(
	ctx context.Context,
	rooms []PlacedRoom,
	backbone []Connection,
	degreeConnections []Connection,
	requests []RoomRoleRequest,
	constraints roleGridConstraints,
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
	roleCeilingsByRole := roleCeilingsByRequestedRole(rooms, requests, constraints)
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

	state := roleAssignmentState{
		rooms: rooms, backbone: backbone, roles: roles, distances: distances,
		degree: degree, roleCeilingsByRole: roleCeilingsByRole,
	}
	if err := assignNonStartRoles(ctx, state, requests); err != nil {
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
func assignNonStartRoles(ctx context.Context, state roleAssignmentState, requests []RoomRoleRequest) error {
	for requestIndex, request := range requests {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch request.Role {
		case RoomRoleStart:
			continue
		case RoomRoleBoss:
			roleCeilings := state.roleCeilingsByRole[request.Role]
			if !assignFarthestRoleConstrained(state.rooms, state.roles, state.distances, RoomRoleBoss, state.degree, roleCeilings, request.MaxRoomEdges != 0) && hasUnassignedRoleRoom(state.roles) && request.MaxRoomEdges != 0 {
				return roleConstraintError(request, uint32(requestIndex), state.degree, roleCeilings)
			}
		case RoomRoleTreasure:
			if err := assignSpreadTreasures(ctx, state, request); err != nil {
				return err
			}
		}
	}
	return nil
}

// assignSpreadTreasures places Count Treasure Rooms by farthest-point sampling.
// Anchors are the Rooms that already hold a role: Start whenever requested,
// Boss when its request preceded this one, and every Treasure already placed. Each pick is
// the unassigned Room whose minimum weighted path distance to any anchor is
// greatest, and that Room joins the anchors. An empty anchor set has no such
// minimum, so the first Treasure uses the distances from RoomID 0, the same
// farthest-from-Start rule. A Count past the free Rooms stops once every Room
// is taken; the shortfall is not an error, and a taken Room is never reused.
func assignSpreadTreasures(ctx context.Context, state roleAssignmentState, request RoomRoleRequest) error {
	roleCeilings := state.roleCeilingsByRole[request.Role]
	for assigned := uint32(0); assigned < request.Count; assigned++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		distances, err := treasureReferenceDistances(ctx, state.rooms, state.backbone, state.roles, state.distances)
		if err != nil {
			return err
		}
		if !assignFarthestRoleConstrained(state.rooms, state.roles, distances, RoomRoleTreasure, state.degree, roleCeilings, request.MaxRoomEdges != 0) {
			if request.MaxRoomEdges != 0 && hasUnassignedRoleRoom(state.roles) {
				return roleConstraintError(request, assigned, state.degree, roleCeilings)
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

func roleCeilingsByRequestedRole(
	rooms []PlacedRoom,
	requests []RoomRoleRequest,
	constraints roleGridConstraints,
) map[RoomRole][]uint32 {
	result := make(map[RoomRole][]uint32, len(requests))
	for _, request := range requests {
		ceilings := make([]uint32, len(rooms))
		if request.MaxRoomEdges != 0 {
			for index, room := range rooms {
				ceilings[index] = cappedRoomEdgeCeiling(request.MaxRoomEdges, room, constraints)
			}
		}
		result[request.Role] = ceilings
	}
	return result
}

func roleCeilingsForAssignedRooms(
	rooms []PlacedRoom,
	roles []*RoomRole,
	requests []RoomRoleRequest,
	constraints roleGridConstraints,
) []uint32 {
	ceilings := make([]uint32, len(rooms))
	for index, role := range roles {
		if role == nil {
			continue
		}
		if ceiling, ok := assignedRoomEdgeCeiling(rooms[index], *role, requests, constraints); ok {
			ceilings[index] = ceiling
		}
	}
	return ceilings
}

func assignedRoomEdgeCeiling(
	room PlacedRoom,
	role RoomRole,
	requests []RoomRoleRequest,
	constraints roleGridConstraints,
) (uint32, bool) {
	for _, request := range requests {
		if request.Role != role || request.MaxRoomEdges == 0 {
			continue
		}
		return cappedRoomEdgeCeiling(request.MaxRoomEdges, room, constraints), true
	}
	return 0, false
}

func cappedRoomEdgeCeiling(requestMax uint32, room PlacedRoom, constraints roleGridConstraints) uint32 {
	ceiling := requestMax
	if constraints.MaxRoomEdges != 0 && constraints.MaxRoomEdges < ceiling {
		ceiling = constraints.MaxRoomEdges
	}
	geometry := roomOpeningCapacity(
		placedRoomFootprint(room), constraints.Width, constraints.Height, constraints.CorridorWidth,
	)
	if geometry < int(ceiling) {
		ceiling = uint32(geometry)
	}
	return ceiling
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
func addExtraConnections(ctx context.Context, request extraConnectionsRequest) ([]Connection, error) {
	request.RoleCeilings = nil
	return addExtraConnectionsWithRoleCeilings(ctx, request)
}

func addExtraConnectionsWithRoleCeilings(ctx context.Context, request extraConnectionsRequest) ([]Connection, error) {
	ctx = topologyContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request.ExtraEdgeCount == topologyNoExtraEdges {
		return request.Backbone, nil
	}
	candidates, err := sortedExtraEdgeCandidates(ctx, request.Rooms, request.Backbone)
	if err != nil {
		return nil, err
	}
	capacity, used, err := roomEdgeUsage(request)
	if err != nil {
		return nil, err
	}
	return selectExtraConnections(ctx, request, candidates, capacity, used)
}

func sortedExtraEdgeCandidates(ctx context.Context, rooms []PlacedRoom, backbone []Connection) ([]primEdgeCandidate, error) {
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
	return candidates, nil
}

func roomEdgeUsage(request extraConnectionsRequest) ([]int, []int, error) {
	width := request.Constraints.CorridorWidth
	if width == 0 {
		width = 1
	}
	capacity := make([]int, len(request.Rooms))
	used := make([]int, len(request.Rooms))
	for roomIndex, room := range request.Rooms {
		capacity[roomIndex] = roomOpeningBudget(
			placedRoomFootprint(room),
			request.Constraints.Width,
			request.Constraints.Height,
			width,
			request.Constraints.MaxRoomEdges,
		)
		if len(request.RoleCeilings) == len(request.Rooms) &&
			request.RoleCeilings[roomIndex] != 0 &&
			int(request.RoleCeilings[roomIndex]) < capacity[roomIndex] {
			capacity[roomIndex] = int(request.RoleCeilings[roomIndex])
		}
	}
	for _, connection := range request.Backbone {
		fromIndex := roomIndexByID(request.Rooms, connection.FromRoomID)
		toIndex := roomIndexByID(request.Rooms, connection.ToRoomID)
		if fromIndex == topologyNoRoomIndex || toIndex == topologyNoRoomIndex {
			return nil, nil, errTopologyUnknownRoom
		}
		used[fromIndex]++
		used[toIndex]++
	}
	return capacity, used, nil
}

func selectExtraConnections(
	ctx context.Context,
	request extraConnectionsRequest,
	candidates []primEdgeCandidate,
	capacity, used []int,
) ([]Connection, error) {
	connections := make([]Connection, 0, len(request.Backbone)+int(request.ExtraEdgeCount))
	connections = append(connections, request.Backbone...)
	added := 0
	for candidateIndex := range candidates {
		if uint64(added) >= uint64(request.ExtraEdgeCount) {
			break
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		accepted, err := tryAcceptExtraConnection(request, candidates[candidateIndex], capacity, used)
		if err != nil {
			return nil, err
		}
		if !accepted {
			continue
		}
		connections = append(connections, candidates[candidateIndex].connection)
		added++
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return connections, nil
}

func tryAcceptExtraConnection(
	request extraConnectionsRequest,
	candidate primEdgeCandidate,
	capacity, used []int,
) (bool, error) {
	fromIndex := roomIndexByID(request.Rooms, candidate.connection.FromRoomID)
	toIndex := roomIndexByID(request.Rooms, candidate.connection.ToRoomID)
	if fromIndex == topologyNoRoomIndex || toIndex == topologyNoRoomIndex {
		return false, errTopologyUnknownRoom
	}
	if used[fromIndex] >= capacity[fromIndex] || used[toIndex] >= capacity[toIndex] {
		return false, nil
	}
	if request.TryRoute != nil {
		ok, routeErr := request.TryRoute(candidate.connection.FromRoomID, candidate.connection.ToRoomID)
		if routeErr != nil {
			return false, routeErr
		}
		if !ok {
			return false, nil
		}
	}
	used[fromIndex]++
	used[toIndex]++
	return true, nil
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
