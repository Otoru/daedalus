package daedalus

import (
	"context"
	"fmt"
)

const (
	// routingForwardStep e routingBackwardStep são os deslocamentos unitários
	// de uma sequência 4-conexa sobre um eixo do Grid.
	routingForwardStep  int64 = 1
	routingBackwardStep int64 = -1
	// routingDirectionCount é a quantidade fixa de direções cardinais da v1.
	routingDirectionCount = 4
	// routingFirstGeneration reserva zero para uma Cell ainda não visitada.
	routingFirstGeneration uint32 = 1
	// routingNoParent marca a raiz da BFS, que não possui predecessor.
	routingNoParent int64 = -1
	// routingCancellationInterval limita o trabalho entre consultas ao Context.
	routingCancellationInterval uint64 = 256
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

// routeCorridors roteia as Connections na ordem recebida. Todos os buffers e
// índices de ocupação pertencem à solicitação; nenhuma Cell de Corridor vira
// obstáculo para uma aresta posterior.
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
				"%w: RoomID %d para RoomID %d",
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
	var best routedConnection
	found := false
	for _, from := range fromOpenings {
		for _, to := range toOpenings {
			if err := search.ctx.Err(); err != nil {
				return routedConnection{}, false, err
			}
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
				if len(best.cells) == 0 {
					// Zero é o custo mínimo possível. Como as aberturas são
					// enumeradas canonicamente, o primeiro par vazio também vence
					// todos os desempates de Doors da seção 9.1.
					return best, true, nil
				}
			}
		}
	}
	if found {
		best.cells = append([]Cell(nil), best.cells...)
	}
	return best, found, nil
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
	// A seção 9.2 chama o fallback de multi-origem/multi-destino, enquanto a
	// seleção da seção 9.1 compara cada par de Doors. A leitura conservadora,
	// explicitada pelo F08, executa uma BFS por par; cada chamada abaixo é o
	// caso degenerado de uma origem e um destino e preserva o desempate global.
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
		currentIndex := search.queue[queueIndex]
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
			// A marcação ocorre na inserção, conforme o comportamento congelado
			// da seção 10.2; assim o primeiro parent nunca é substituído.
			search.visited[int(neighborIndex)] = search.generation
			search.parents[int(neighborIndex)] = currentIndex
			search.queue = append(search.queue, neighborIndex)
			if neighborIndex == toIndex {
				found = true
				break
			}
		}
	}
	if !found {
		return buffer[:0], false, nil
	}

	buffer = buffer[:0]
	for index := toIndex; index != routingNoParent; index = search.parents[int(index)] {
		buffer = append(buffer, search.cellAt(index))
	}
	for left, right := 0, len(buffer)-1; left < right; left, right = left+1, right-1 {
		buffer[left], buffer[right] = buffer[right], buffer[left]
	}
	return buffer, true, nil
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

// lRoute materializa um cotovelo entre duas Cells externas, incluindo ambas
// as extremidades. O buffer pertence à solicitação e pode ser reutilizado na
// próxima tentativa depois que o chamador copiar uma rota vencedora.
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
