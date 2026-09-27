package daedalus

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

var (
	errGeneratorNoRooms   = errors.New("generator received no valid Room")
	errGeneratorInvariant = errors.New("invariante interna do gerador violada")
	errInvalidConnection  = errors.New("Connector returned an invalid connection")
)

const weightedSelectionFirstTicket uint64 = 1

// Generator coordinates the phases that generate a Layout. Callers may supply
// Placer and Connector; nil fields select the built-in algorithms. The zero
// value is the default Generator and may be used simultaneously by multiple
// goroutines.
type Generator struct {
	Placer    Placer
	Connector Connector
}

// Generate creates a Layout using a Context with no explicit cancellation.
func (generator Generator) Generate(config Config) (Layout, error) {
	return generator.GenerateContext(context.Background(), config)
}

// GenerateContext creates a Layout while preserving cancellation and deadline
// from the supplied Context. The zero Layout is returned on every failure.
func (generator Generator) GenerateContext(ctx context.Context, config Config) (Layout, error) {
	// A nil Context means no cancellation.
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Layout{}, err
	}

	effective, err := normalizeConfig(config)
	if err != nil {
		return Layout{}, err
	}
	if err := ctx.Err(); err != nil {
		return Layout{}, err
	}

	placer := generator.Placer
	if placer == nil {
		placer = poissonDiskRoomsPlacer{}
	}
	placements, err := placer.Place(PlacementRequest{
		Context: ctx, Width: effective.width, Height: effective.height,
		MinDistance: effective.minDistance, DensityRegions: effective.densityRegions,
		RoomGeometry: effective.roomGeometry, MaxAttempts: effective.maxAttempts,
		MaxRooms: effective.maxRooms, Seed: effective.seed,
		geometryCombinations: effective.geometryCombinations,
	})
	if err != nil {
		if errors.Is(err, errPlacementRequestNotNormalized) {
			return Layout{}, errGeneratorInvariant
		}
		return Layout{}, err
	}
	if err := ctx.Err(); err != nil {
		return Layout{}, err
	}

	rooms, occupancy, err := validateAndMaterializePlacements(ctx, placements, effective)
	if err != nil {
		return Layout{}, err
	}

	connector := generator.Connector
	if connector == nil {
		connector = primRoomsConnector{}
	}
	backbone, err := connector.Connect(ConnectionRequest{
		Context: ctx, Rooms: rooms, Width: effective.width, Height: effective.height,
		ExtraEdgeCount: effective.extraEdgeCount, Seed: effective.seed,
	})
	if err != nil {
		return Layout{}, err
	}
	if err := validateConnections(rooms, backbone, effective.extraEdgeCount); err != nil {
		return Layout{}, fmt.Errorf("%w: %w", ErrInvalidPlugin, err)
	}
	if err := ctx.Err(); err != nil {
		return Layout{}, err
	}

	roles, connections, err := applyGeneratorTopologyOptions(ctx, rooms, backbone, effective)
	if err != nil {
		return Layout{}, err
	}
	if err := ctx.Err(); err != nil {
		return Layout{}, err
	}

	corridors, doors, err := routeCorridors(
		ctx, effective.width, effective.height, effective.corridorOrder, rooms, connections,
	)
	if err != nil {
		return Layout{}, err
	}
	if err := ctx.Err(); err != nil {
		return Layout{}, err
	}

	layout := materializeLayout(effective, rooms, occupancy, roles, corridors, doors)
	if err := resolvePlants(ctx, effective, &layout); err != nil {
		return Layout{}, err
	}
	return layout, nil
}

func validateAndMaterializePlacements(
	ctx context.Context,
	placements []RoomPlacement,
	effective effectiveConfig,
) ([]PlacedRoom, *placementOccupancy, error) {
	if len(placements) == 0 {
		return nil, nil, fmt.Errorf("%w: %w", ErrInvalidPlugin, errGeneratorNoRooms)
	}
	if uint64(len(placements)) > uint64(effective.maxRooms) {
		return nil, nil, fmt.Errorf(
			"%w: %w: Room count exceeds MaxRooms", ErrInvalidPlugin, errGeneratorInvariant,
		)
	}

	occupancy := newPlacementOccupancy(effective.width, effective.height)
	accepted := make([]acceptedPlacement, 0, len(placements))
	rooms := make([]PlacedRoom, 0, len(placements))
	acceptance := placementAcceptance{
		gridWidth:         effective.width,
		gridHeight:        effective.height,
		maxFootprintCells: effective.roomGeometry.MaxFootprintCells,
		minRoomGap:        effective.roomGeometry.MinRoomGap,
		minDistance:       effective.minDistance,
		densityRegions:    effective.densityRegions,
		occupancy:         occupancy,
	}
	for index, placement := range placements {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		acceptance.accepted = accepted
		footprint, err := validatePlacementAndMaterialize(placement, acceptance)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: invalid placement %d: %w", ErrInvalidPlugin, index, err)
		}
		roomID := RoomID(index)
		roomCells := footprint
		rooms = append(rooms, PlacedRoom{
			ID: roomID, At: roomCells[0], Shape: placement.Shape,
			Origin: placement.Origin, Width: placement.Width, Height: placement.Height,
			Cells: roomCells,
		})
		accepted = append(accepted, acceptedPlacement{anchor: roomCells[0], footprint: roomCells})
		occupancy.mark(uint32(index), roomCells)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return rooms, occupancy, nil
}

func validateConnections(rooms []PlacedRoom, connections []Connection, extraEdgeCount uint32) error {
	roomCount := len(rooms)
	minimumEdges := roomCount - topologyNextRoomOffset
	maximumEdges := uint64(minimumEdges) + uint64(extraEdgeCount)
	if len(connections) < minimumEdges || uint64(len(connections)) > maximumEdges {
		return fmt.Errorf(
			"%w: Connector must return between %d and %d edges",
			errInvalidConnection, minimumEdges, maximumEdges,
		)
	}
	seen := make(map[[2]RoomID]struct{}, len(connections))
	adjacency := make([][]int, roomCount)
	for index, connection := range connections {
		if err := validateConnectionEdge(roomCount, index, connection, seen); err != nil {
			return err
		}
		from := int(connection.FromRoomID)
		to := int(connection.ToRoomID)
		adjacency[from] = append(adjacency[from], to)
		adjacency[to] = append(adjacency[to], from)
	}
	return validateConnectorReachesEveryRoom(adjacency)
}

// validateConnectionEdge rejects an unknown Room, a self-edge, or a duplicate
// undirected pair, in that order.
func validateConnectionEdge(
	roomCount int,
	index int,
	connection Connection,
	seen map[[2]RoomID]struct{},
) error {
	from := int(connection.FromRoomID)
	to := int(connection.ToRoomID)
	if from < 0 || from >= roomCount || to < 0 || to >= roomCount {
		return fmt.Errorf("%w: edge %d references unknown RoomID", errInvalidConnection, index)
	}
	if from == to {
		return fmt.Errorf("%w: edge %d is a self-edge", errInvalidConnection, index)
	}
	first := connection.FromRoomID
	second := connection.ToRoomID
	if second < first {
		first, second = second, first
	}
	key := [2]RoomID{first, second}
	if _, exists := seen[key]; exists {
		return fmt.Errorf("%w: edge %d is duplicated", errInvalidConnection, index)
	}
	seen[key] = struct{}{}
	return nil
}

// validateConnectorReachesEveryRoom reports a connector graph that cannot reach
// every Room from Room 0.
func validateConnectorReachesEveryRoom(adjacency [][]int) error {
	roomCount := len(adjacency)
	visited := make([]bool, roomCount)
	queue := make([]int, 0, roomCount)
	visited[0] = true
	queue = append(queue, 0)
	for queueIndex := 0; queueIndex < len(queue); queueIndex++ {
		for _, neighbor := range adjacency[queue[queueIndex]] {
			if visited[neighbor] {
				continue
			}
			visited[neighbor] = true
			queue = append(queue, neighbor)
		}
	}
	for _, reached := range visited {
		if !reached {
			return fmt.Errorf("%w: disconnected graph", errInvalidConnection)
		}
	}
	return nil
}

func applyGeneratorTopologyOptions(
	ctx context.Context,
	rooms []PlacedRoom,
	connections []Connection,
	effective effectiveConfig,
) ([]*RoomRole, []Connection, error) {
	minimumEdges := len(rooms) - topologyNextRoomOffset
	existingExtraEdges := len(connections) - minimumEdges
	if existingExtraEdges == 0 {
		return applyTopologyOptions(
			ctx, rooms, connections, effective.roomRoleRequests, effective.extraEdgeCount,
		)
	}

	// A custom Connector may return up to n-1+ExtraEdgeCount edges. Roles are
	// computed on a tree, before shortcuts. The plugin's creation order is kept:
	// the first edges that reach every Room form that tree, and edges that close
	// a cycle already consume the shortcut budget.
	roleBackbone := connectorSpanningTree(len(rooms), connections)
	roles, err := assignRoomRoles(ctx, rooms, roleBackbone, effective.roomRoleRequests)
	if err != nil {
		return nil, nil, err
	}
	remainingExtraEdges := effective.extraEdgeCount - uint32(existingExtraEdges)
	finalConnections, err := addExtraConnections(ctx, rooms, connections, remainingExtraEdges)
	if err != nil {
		return nil, nil, err
	}
	return roles, finalConnections, nil
}

func connectorSpanningTree(roomCount int, connections []Connection) []Connection {
	parents := make([]int, roomCount)
	for index := range parents {
		parents[index] = index
	}
	var find func(int) int
	find = func(index int) int {
		if parents[index] != index {
			parents[index] = find(parents[index])
		}
		return parents[index]
	}
	tree := make([]Connection, 0, roomCount-topologyNextRoomOffset)
	for _, connection := range connections {
		fromRoot := find(int(connection.FromRoomID))
		toRoot := find(int(connection.ToRoomID))
		if fromRoot == toRoot {
			continue
		}
		parents[toRoot] = fromRoot
		tree = append(tree, connection)
	}
	return tree
}

func materializeLayout(
	effective effectiveConfig,
	placed []PlacedRoom,
	occupancy *placementOccupancy,
	roles []*RoomRole,
	corridors []Corridor,
	doors []Door,
) Layout {
	rooms := make([]Room, len(placed))
	for index, room := range placed {
		rooms[index] = Room{
			ID: room.ID, At: room.At, Shape: room.Shape, Origin: room.Origin,
			Width: room.Width, Height: room.Height, Cells: room.Cells,
			Role: roles[index],
		}
	}
	for _, door := range doors {
		roomIndex := int(door.RoomID)
		rooms[roomIndex].DoorIDs = append(rooms[roomIndex].DoorIDs, door.ID)
	}
	for roomIndex := range rooms {
		sort.Slice(rooms[roomIndex].DoorIDs, func(first, second int) bool {
			firstDoor := doors[int(rooms[roomIndex].DoorIDs[first])]
			secondDoor := doors[int(rooms[roomIndex].DoorIDs[second])]
			if firstDoor.At.Y != secondDoor.At.Y {
				return firstDoor.At.Y < secondDoor.At.Y
			}
			if firstDoor.At.X != secondDoor.At.X {
				return firstDoor.At.X < secondDoor.At.X
			}
			return firstDoor.Direction < secondDoor.Direction
		})
	}

	cellCount := int(uint64(effective.width) * uint64(effective.height))
	cells := make([]CellState, cellCount)
	for index := range cells {
		y := index / int(effective.width)
		x := index - y*int(effective.width)
		cells[index].At = Cell{X: int32(x), Y: int32(y)}
		if owner, occupied := occupancy.ownerAt(cells[index].At); occupied {
			roomID := RoomID(owner)
			cells[index].Kind = CellKindRoom
			cells[index].RoomID = &roomID
		}
	}
	for _, corridor := range corridors {
		for _, cell := range corridor.Cells {
			index, _ := occupancy.index(cell)
			cells[index].Kind = CellKindCorridor
			cells[index].RoomID = nil
			cells[index].CorridorIDs = append(cells[index].CorridorIDs, corridor.ID)
		}
	}

	return Layout{
		Seed:  effective.seed,
		Grid:  Grid{Width: effective.width, Height: effective.height, CellSize: effective.cellSize, Cells: cells},
		Rooms: rooms, Corridors: corridors, Doors: doors,
	}
}

func resolvePlants(ctx context.Context, effective effectiveConfig, layout *Layout) error {
	if effective.plantCatalog == nil {
		return nil
	}
	streams := newRNGStreams(effective.seed)
	for roomIndex := range layout.Rooms {
		if err := ctx.Err(); err != nil {
			return err
		}
		room := &layout.Rooms[roomIndex]
		requiredDirections := roomDoorDirections(*room, layout.Doors)
		requiredTags := requiredTagsForRole(room.Role, effective.roomRoleRequests)
		candidates := make([]RoomPlant, 0, len(effective.plantCatalog.Rooms))
		for _, plant := range effective.plantCatalog.Rooms {
			if roomPlantCompatible(plant, requiredDirections, requiredTags) {
				candidates = append(candidates, plant)
			}
		}
		if len(candidates) == 0 {
			return fmt.Errorf("%w: RoomID %d", ErrNoCompatiblePlant, room.ID)
		}
		sort.Slice(candidates, func(first, second int) bool {
			return candidates[first].ID < candidates[second].ID
		})
		selected := selectWeightedRoomPlant(candidates, &streams.roomPlant)
		room.PlantID = selected.ID
		room.Tags = append([]string(nil), selected.Tags...)
	}
	for corridorIndex := range layout.Corridors {
		if err := ctx.Err(); err != nil {
			return err
		}
		candidates := append([]CorridorPlant(nil), effective.plantCatalog.Corridors...)
		sort.Slice(candidates, func(first, second int) bool {
			return candidates[first].ID < candidates[second].ID
		})
		selected := selectWeightedCorridorPlant(candidates, &streams.corridorPlant)
		layout.Corridors[corridorIndex].PlantID = selected.ID
		layout.Corridors[corridorIndex].Tags = append([]string(nil), selected.Tags...)
	}
	return nil
}

func roomDoorDirections(room Room, doors []Door) [routingDirectionCount]bool {
	var required [routingDirectionCount]bool
	for _, doorID := range room.DoorIDs {
		direction := doors[int(doorID)].Direction
		required[int(direction)] = true
	}
	return required
}

func requiredTagsForRole(role *RoomRole, requests []RoomRoleRequest) []string {
	if role == nil {
		return nil
	}
	for _, request := range requests {
		if request.Role == *role {
			return request.RequiredTags
		}
	}
	return nil
}

func roomPlantCompatible(
	plant RoomPlant,
	requiredDirections [routingDirectionCount]bool,
	requiredTags []string,
) bool {
	var supported [routingDirectionCount]bool
	for _, direction := range plant.DoorDirections {
		supported[int(direction)] = true
	}
	for direction, required := range requiredDirections {
		if required && !supported[direction] {
			return false
		}
	}
	for _, requiredTag := range requiredTags {
		found := false
		for _, plantTag := range plant.Tags {
			if plantTag == requiredTag {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func selectWeightedRoomPlant(candidates []RoomPlant, stream *splitMix64) RoomPlant {
	var total uint64
	for _, candidate := range candidates {
		total += uint64(candidate.Weight)
	}
	draw := stream.uniformInt(weightedSelectionFirstTicket, total)
	var cumulative uint64
	for _, candidate := range candidates {
		cumulative += uint64(candidate.Weight)
		if draw <= cumulative {
			return candidate
		}
	}
	return candidates[len(candidates)-1]
}

func selectWeightedCorridorPlant(candidates []CorridorPlant, stream *splitMix64) CorridorPlant {
	var total uint64
	for _, candidate := range candidates {
		total += uint64(candidate.Weight)
	}
	draw := stream.uniformInt(weightedSelectionFirstTicket, total)
	var cumulative uint64
	for _, candidate := range candidates {
		cumulative += uint64(candidate.Weight)
		if draw <= cumulative {
			return candidate
		}
	}
	return candidates[len(candidates)-1]
}
