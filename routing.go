package daedalus

import (
	"context"
	"fmt"
	"sort"
)

const (
	// routingForwardStep and routingBackwardStep are the unit offsets of a
	// 4-connected sequence along one Grid axis.
	routingForwardStep  int64 = 1
	routingBackwardStep int64 = -1
	// routingDirectionCount is the fixed number of v1 cardinal directions.
	routingDirectionCount = 4
	// routingFirstGeneration reserves zero for a Cell not yet visited.
	routingFirstGeneration uint32 = 1
	// routingNoParent marks the BFS root, which has no predecessor.
	routingNoParent int64 = -1
	// routingCancellationInterval limits work between Context checks.
	routingCancellationInterval uint64 = 256
	// routingEndpointCellCount includes the final external Cell in the Manhattan bound.
	routingEndpointCellCount int64 = 1
)

type doorOpening struct {
	roomID    RoomID
	at        Cell
	direction Direction
	outside   Cell
}

type routedConnection struct {
	from  doorOpening
	to    doorOpening
	cells []Cell
}

type openingPairCandidate struct {
	fromIndex  int
	toIndex    int
	lowerBound int64
}

type routingSearch struct {
	ctx          context.Context
	occupancy    *placementOccupancy
	generation   uint32
	visited      []uint32
	parents      []int64
	queue        []int64
	routeScratch []Cell
	bestScratch  []Cell
}

func newRoutingSearch(ctx context.Context, occupancy *placementOccupancy) *routingSearch {
	cellCount := occupancy.width * occupancy.height
	return &routingSearch{
		ctx: ctx, occupancy: occupancy,
		visited: make([]uint32, int(cellCount)),
		parents: make([]int64, int(cellCount)),
		queue:   make([]int64, 0, int(cellCount)),
	}
}

// routeCorridors routes Connections in received order. Every buffer and
// occupancy index belongs to the request; no Corridor Cell becomes an obstacle
// for a later edge.
func routeCorridors(
	ctx context.Context,
	width uint32,
	height uint32,
	order CorridorOrder,
	rooms []PlacedRoom,
	connections []Connection,
) ([]Corridor, []Door, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	occupancy := newPlacementOccupancy(width, height)
	openings := make([][]doorOpening, len(rooms))
	for roomIndex, room := range rooms {
		occupancy.mark(uint32(roomIndex), room.Cells)
	}
	for roomIndex, room := range rooms {
		openings[roomIndex] = enumerateDoorOpenings(room, uint32(roomIndex), occupancy)
	}

	corridors := make([]Corridor, 0, len(connections))
	doors := make([]Door, 0, len(connections)*2)
	search := newRoutingSearch(ctx, occupancy)
	for connectionIndex, connection := range connections {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		fromIndex := roomIndexByID(rooms, connection.FromRoomID)
		toIndex := roomIndexByID(rooms, connection.ToRoomID)
		if fromIndex == topologyNoRoomIndex || toIndex == topologyNoRoomIndex {
			return nil, nil, errTopologyUnknownRoom
		}

		best, found, err := selectRoutedConnection(
			openings[fromIndex], openings[toIndex], order, search,
		)
		if err != nil {
			return nil, nil, err
		}
		if !found {
			return nil, nil, fmt.Errorf(
				"%w: RoomID %d to RoomID %d",
				ErrUnroutableEdge, connection.FromRoomID, connection.ToRoomID,
			)
		}
		corridorID := CorridorID(connectionIndex)
		fromDoorID := getOrCreateDoor(&doors, best.from, corridorID)
		toDoorID := getOrCreateDoor(&doors, best.to, corridorID)
		corridors = append(corridors, Corridor{
			ID: corridorID, FromRoomID: connection.FromRoomID, ToRoomID: connection.ToRoomID,
			FromDoorID: fromDoorID, ToDoorID: toDoorID, Cells: best.cells,
		})
	}
	return corridors, doors, nil
}

func enumerateDoorOpenings(
	room PlacedRoom,
	roomIndex uint32,
	occupancy *placementOccupancy,
) []doorOpening {
	openings := make([]doorOpening, 0)
	for _, cell := range room.Cells {
		for direction := DirectionNorth; direction < Direction(routingDirectionCount); direction++ {
			delta := direction.Delta()
			outsideX := int64(cell.X) + int64(delta.X)
			outsideY := int64(cell.Y) + int64(delta.Y)
			if outsideX < minCellCoordinate || outsideX > maxCellCoordinate ||
				outsideY < minCellCoordinate || outsideY > maxCellCoordinate {
				continue
			}
			outside := Cell{X: int32(outsideX), Y: int32(outsideY)}
			if _, inside := occupancy.index(outside); !inside {
				continue
			}
			owner, occupied := occupancy.ownerAt(outside)
			if occupied && owner == roomIndex {
				continue
			}
			openings = append(openings, doorOpening{
				roomID: room.ID, at: cell, direction: direction, outside: outside,
			})
		}
	}
	return openings
}

func selectRoutedConnection(
	fromOpenings []doorOpening,
	toOpenings []doorOpening,
	order CorridorOrder,
	search *routingSearch,
) (routedConnection, bool, error) {
	pairs, err := openingPairCandidates(search.ctx, fromOpenings, toOpenings)
	if err != nil {
		return routedConnection{}, false, err
	}
	sort.Slice(pairs, func(first, second int) bool {
		return pairs[first].lowerBound < pairs[second].lowerBound
	})

	var best routedConnection
	found := false
	for _, pair := range pairs {
		// Prune only when the lower bound is strictly greater than the best cost.
		// A pair whose bound equals that cost can still tie and win on the origin
		// Door (At.Y, At.X, Direction), so greater-or-equal must not stop the search.
		if found && !openingPairCanBeatBest(pair.lowerBound, len(best.cells)) {
			break
		}
		if err := search.ctx.Err(); err != nil {
			return routedConnection{}, false, err
		}
		from := fromOpenings[pair.fromIndex]
		to := toOpenings[pair.toIndex]
		cells, ok, err := routeOpeningPair(from, to, order, search, search.routeScratch)
		if err != nil {
			return routedConnection{}, false, err
		}
		search.routeScratch = cells[:0]
		if !ok {
			continue
		}
		candidate := routedConnection{from: from, to: to, cells: cells}
		if !found || routedConnectionLess(candidate, best) {
			search.bestScratch = append(search.bestScratch[:0], cells...)
			candidate.cells = search.bestScratch
			best = candidate
			found = true
		}
	}
	if found {
		best.cells = append([]Cell(nil), best.cells...)
	}
	return best, found, nil
}

// openingPairCandidates lists every door pair in received opening order.
// Cancellation is observed once per pair, before that pair's lower bound is recorded.
func openingPairCandidates(
	ctx context.Context,
	fromOpenings []doorOpening,
	toOpenings []doorOpening,
) ([]openingPairCandidate, error) {
	pairs := make([]openingPairCandidate, 0, len(fromOpenings)*len(toOpenings))
	for fromIndex, from := range fromOpenings {
		for toIndex, to := range toOpenings {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			pairs = append(pairs, openingPairCandidate{
				fromIndex:  fromIndex,
				toIndex:    toIndex,
				lowerBound: openingPairLowerBound(from, to),
			})
		}
	}
	return pairs, nil
}

func openingPairLowerBound(from, to doorOpening) int64 {
	if openingsFaceEachOther(from, to) {
		return 0
	}
	distanceX := absInt64(int64(from.outside.X) - int64(to.outside.X))
	distanceY := absInt64(int64(from.outside.Y) - int64(to.outside.Y))
	return distanceX + distanceY + routingEndpointCellCount
}

func openingPairCanBeatBest(lowerBound int64, bestCost int) bool {
	return lowerBound <= int64(bestCost)
}

func routeOpeningPair(
	from doorOpening,
	to doorOpening,
	order CorridorOrder,
	search *routingSearch,
	buffer []Cell,
) ([]Cell, bool, error) {
	if openingsFaceEachOther(from, to) {
		return buffer[:0], true, nil
	}
	if cells, ok := lRoute(from.outside, to.outside, order, search.occupancy, buffer); ok {
		return cells, true, nil
	}
	if cells, ok := lRoute(from.outside, to.outside, alternateCorridorOrder(order), search.occupancy, buffer); ok {
		return cells, true, nil
	}
	// Both L-routes are tried first. The fallback is one breadth-first search per
	// Door pair, from that pair's exterior Cell to the other, rather than one
	// multi-source search over every opening. Selection compares pairs, and the
	// tie-break is source Door, destination Door, then the route; one search per
	// pair keeps the parent from first discovery inside that pair.
	cells, ok, err := search.breadthFirstRoute(from.outside, to.outside, buffer)
	return cells, ok, err
}

func (search *routingSearch) breadthFirstRoute(from, to Cell, buffer []Cell) ([]Cell, bool, error) {
	fromIndex, fromFree := search.freeIndex(from)
	toIndex, toFree := search.freeIndex(to)
	if !fromFree || !toFree {
		return buffer[:0], false, nil
	}

	search.nextGeneration()
	search.queue = search.queue[:0]
	search.queue = append(search.queue, fromIndex)
	search.visited[int(fromIndex)] = search.generation
	search.parents[int(fromIndex)] = routingNoParent
	var expansions uint64
	found := fromIndex == toIndex
	for queueIndex := 0; queueIndex < len(search.queue) && !found; queueIndex++ {
		if expansions%routingCancellationInterval == 0 {
			if err := search.ctx.Err(); err != nil {
				return buffer[:0], false, err
			}
		}
		expansions++
		if search.enqueueCardinalNeighbors(search.queue[queueIndex], toIndex) {
			found = true
		}
	}
	if !found {
		return buffer[:0], false, nil
	}
	return search.cellsFromParents(toIndex, buffer), true, nil
}

// enqueueCardinalNeighbors expands North, East, South, West into a FIFO queue.
// A Cell is marked on insertion, not on removal, so the parent is the first
// discovery and is never replaced. The index is row-major, Y*width+X, with no
// map, so the path does not depend on hash order.
func (search *routingSearch) enqueueCardinalNeighbors(currentIndex, toIndex int64) bool {
	current := search.cellAt(currentIndex)
	for direction := DirectionNorth; direction < Direction(routingDirectionCount); direction++ {
		delta := direction.Delta()
		neighborX := int64(current.X) + int64(delta.X)
		neighborY := int64(current.Y) + int64(delta.Y)
		if neighborX < minCellCoordinate || neighborX > maxCellCoordinate ||
			neighborY < minCellCoordinate || neighborY > maxCellCoordinate {
			continue
		}
		neighbor := Cell{X: int32(neighborX), Y: int32(neighborY)}
		neighborIndex, free := search.freeIndex(neighbor)
		if !free || search.visited[int(neighborIndex)] == search.generation {
			continue
		}
		// Mark on insertion. The first parent is never replaced.
		search.visited[int(neighborIndex)] = search.generation
		search.parents[int(neighborIndex)] = currentIndex
		search.queue = append(search.queue, neighborIndex)
		if neighborIndex == toIndex {
			return true
		}
	}
	return false
}

// cellsFromParents walks the first-discovery parents from the target back to the
// root, then reverses that walk so index 0 is the source Cell.
func (search *routingSearch) cellsFromParents(toIndex int64, buffer []Cell) []Cell {
	buffer = buffer[:0]
	for index := toIndex; index != routingNoParent; index = search.parents[int(index)] {
		buffer = append(buffer, search.cellAt(index))
	}
	for left, right := 0, len(buffer)-1; left < right; left, right = left+1, right-1 {
		buffer[left], buffer[right] = buffer[right], buffer[left]
	}
	return buffer
}

func (search *routingSearch) nextGeneration() {
	search.generation++
	if search.generation != 0 {
		return
	}
	clear(search.visited)
	search.generation = routingFirstGeneration
}

func (search *routingSearch) freeIndex(cell Cell) (int64, bool) {
	index, inside := search.occupancy.index(cell)
	if !inside {
		return 0, false
	}
	if _, occupied := search.occupancy.ownerAt(cell); occupied {
		return 0, false
	}
	return int64(index), true
}

func (search *routingSearch) cellAt(index int64) Cell {
	y := index / search.occupancy.width
	x := index - y*search.occupancy.width
	return Cell{X: int32(x), Y: int32(y)}
}

func openingsFaceEachOther(from, to doorOpening) bool {
	return from.outside == to.at && to.outside == from.at && from.direction.Opposite() == to.direction
}

func alternateCorridorOrder(order CorridorOrder) CorridorOrder {
	if order == CorridorOrderXThenY {
		return CorridorOrderYThenX
	}
	return CorridorOrderXThenY
}

func routedConnectionLess(first, second routedConnection) bool {
	if len(first.cells) != len(second.cells) {
		return len(first.cells) < len(second.cells)
	}
	if comparison := compareDoorOpening(first.from, second.from); comparison != 0 {
		return comparison < 0
	}
	if comparison := compareDoorOpening(first.to, second.to); comparison != 0 {
		return comparison < 0
	}
	return routeLexicographicallyLess(first.cells, second.cells)
}

func compareDoorOpening(first, second doorOpening) int {
	if first.at.Y != second.at.Y {
		return compareInt32(first.at.Y, second.at.Y)
	}
	if first.at.X != second.at.X {
		return compareInt32(first.at.X, second.at.X)
	}
	if first.direction < second.direction {
		return -1
	}
	if first.direction > second.direction {
		return 1
	}
	return 0
}

func compareInt32(first, second int32) int {
	if first < second {
		return -1
	}
	if first > second {
		return 1
	}
	return 0
}

func routeLexicographicallyLess(first, second []Cell) bool {
	for index := range first {
		if first[index].Y != second[index].Y {
			return first[index].Y < second[index].Y
		}
		if first[index].X != second[index].X {
			return first[index].X < second[index].X
		}
	}
	return false
}

func getOrCreateDoor(doors *[]Door, opening doorOpening, corridorID CorridorID) DoorID {
	for doorIndex := range *doors {
		door := &(*doors)[doorIndex]
		if door.RoomID == opening.roomID && door.At == opening.at && door.Direction == opening.direction {
			door.CorridorIDs = append(door.CorridorIDs, corridorID)
			return door.ID
		}
	}
	doorID := DoorID(len(*doors))
	*doors = append(*doors, Door{
		ID: doorID, RoomID: opening.roomID, At: opening.at,
		Direction: opening.direction, CorridorIDs: []CorridorID{corridorID},
	})
	return doorID
}

// lRoute materializes a bend between two external Cells, including both
// endpoints. The buffer belongs to the request and may be reused on the next
// attempt after the caller copies a winning route.
func lRoute(
	from Cell,
	to Cell,
	order CorridorOrder,
	occupancy *placementOccupancy,
	buffer []Cell,
) ([]Cell, bool) {
	buffer = buffer[:0]
	currentX := int64(from.X)
	currentY := int64(from.Y)
	targetX := int64(to.X)
	targetY := int64(to.Y)

	var ok bool
	buffer, ok = appendFreeRoutingCell(buffer, currentX, currentY, occupancy)
	if !ok {
		return buffer[:0], false
	}
	if order == CorridorOrderXThenY {
		buffer, currentX, ok = appendRoutingAxis(buffer, currentX, targetX, currentY, true, occupancy)
		if ok {
			buffer, _, ok = appendRoutingAxis(buffer, currentY, targetY, currentX, false, occupancy)
		}
	} else {
		buffer, currentY, ok = appendRoutingAxis(buffer, currentY, targetY, currentX, false, occupancy)
		if ok {
			buffer, _, ok = appendRoutingAxis(buffer, currentX, targetX, currentY, true, occupancy)
		}
	}
	if !ok {
		return buffer[:0], false
	}
	return buffer, true
}

func appendRoutingAxis(
	buffer []Cell,
	current int64,
	target int64,
	fixed int64,
	horizontal bool,
	occupancy *placementOccupancy,
) ([]Cell, int64, bool) {
	step := routingForwardStep
	if target < current {
		step = routingBackwardStep
	}
	for current != target {
		current += step
		x := fixed
		y := current
		if horizontal {
			x = current
			y = fixed
		}
		var ok bool
		buffer, ok = appendFreeRoutingCell(buffer, x, y, occupancy)
		if !ok {
			return buffer, current, false
		}
	}
	return buffer, current, true
}

func appendFreeRoutingCell(
	buffer []Cell,
	x int64,
	y int64,
	occupancy *placementOccupancy,
) ([]Cell, bool) {
	if x < minCellCoordinate || x > maxCellCoordinate || y < minCellCoordinate || y > maxCellCoordinate {
		return buffer, false
	}
	cell := Cell{X: int32(x), Y: int32(y)}
	if _, inside := occupancy.index(cell); !inside {
		return buffer, false
	}
	if _, occupied := occupancy.ownerAt(cell); occupied {
		return buffer, false
	}
	return append(buffer, cell), true
}
