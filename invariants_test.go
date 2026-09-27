package daedalus

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

// layoutInvariantFailures é o oráculo único das invariantes de Layout da seção 5.2.
// Cada diagnóstico contém caminho de campo, valor esperado e valor real para que
// uma regressão não fique escondida atrás de uma comparação por hash.
func layoutInvariantFailures(effective effectiveConfig, layout Layout) []string {
	failures := make([]string, 0)
	fail := func(path string, expected, actual any) {
		failures = append(failures, fmt.Sprintf("%s: esperado %v; real %v", path, expected, actual))
	}

	if layout.Seed != effective.seed {
		fail("Layout.Seed", effective.seed, layout.Seed)
	}
	if layout.Grid.Width != effective.width {
		fail("Layout.Grid.Width", effective.width, layout.Grid.Width)
	}
	if layout.Grid.Height != effective.height {
		fail("Layout.Grid.Height", effective.height, layout.Grid.Height)
	}
	if layout.Grid.CellSize != effective.cellSize {
		fail("Layout.Grid.CellSize", effective.cellSize, layout.Grid.CellSize)
	}
	wantedGridCells := int(uint64(effective.width) * uint64(effective.height))
	if len(layout.Grid.Cells) != wantedGridCells {
		fail("Layout.Grid.Cells.len", wantedGridCells, len(layout.Grid.Cells))
	}
	if len(layout.Rooms) == 0 {
		fail("Layout.Rooms.len", "ao menos 1", 0)
	}
	if uint32(len(layout.Rooms)) > effective.maxRooms {
		fail("Layout.Rooms.len", fmt.Sprintf("<= %d", effective.maxRooms), len(layout.Rooms))
	}

	roomByID := make(map[RoomID]Room, len(layout.Rooms))
	roomIndexByID := make(map[RoomID]int, len(layout.Rooms))
	roomCells := make(map[Cell]RoomID)
	for roomIndex, room := range layout.Rooms {
		path := fmt.Sprintf("Layout.Rooms[%d]", roomIndex)
		if room.ID != RoomID(roomIndex) {
			fail(path+".ID", RoomID(roomIndex), room.ID)
		}
		if _, exists := roomByID[room.ID]; exists {
			fail(path+".ID", "único", room.ID)
		}
		roomByID[room.ID] = room
		roomIndexByID[room.ID] = roomIndex

		offsets := RoomShapeOffsets(room.Shape, room.Width, room.Height)
		wantedCells := make([]Cell, len(offsets))
		for cellIndex, offset := range offsets {
			wantedCells[cellIndex] = Cell{X: room.Origin.X + offset.X, Y: room.Origin.Y + offset.Y}
		}
		if !reflect.DeepEqual(wantedCells, room.Cells) {
			fail(path+".Cells", wantedCells, room.Cells)
		}
		if len(room.Cells) == 0 {
			fail(path+".Cells.len", "ao menos 1", 0)
			continue
		}
		if room.At != room.Cells[0] {
			fail(path+".At", room.Cells[0], room.At)
		}
		minimumX, maximumX := room.Cells[0].X, room.Cells[0].X
		minimumY, maximumY := room.Cells[0].Y, room.Cells[0].Y
		seenCells := make(map[Cell]struct{}, len(room.Cells))
		for cellIndex, cell := range room.Cells {
			cellPath := fmt.Sprintf("%s.Cells[%d]", path, cellIndex)
			if !cellInsideGrid(cell, effective.width, effective.height) {
				fail(cellPath, "dentro do Grid", cell)
			}
			if _, exists := seenCells[cell]; exists {
				fail(cellPath, "Cell única no footprint", cell)
			}
			seenCells[cell] = struct{}{}
			if owner, occupied := roomCells[cell]; occupied {
				fail(cellPath, "sem sobreposição", fmt.Sprintf("também pertence à RoomID %d", owner))
			}
			roomCells[cell] = room.ID
			minimumX = min(minimumX, cell.X)
			maximumX = max(maximumX, cell.X)
			minimumY = min(minimumY, cell.Y)
			maximumY = max(maximumY, cell.Y)
		}
		if !cellsAreFourConnected(room.Cells) {
			fail(path+".Cells", "footprint 4-conexo", room.Cells)
		}
		actualOrigin := Cell{X: minimumX, Y: minimumY}
		if room.Origin != actualOrigin {
			fail(path+".Origin", actualOrigin, room.Origin)
		}
		actualWidth := uint32(int64(maximumX) - int64(minimumX) + 1)
		actualHeight := uint32(int64(maximumY) - int64(minimumY) + 1)
		if room.Width != actualWidth {
			fail(path+".Width", actualWidth, room.Width)
		}
		if room.Height != actualHeight {
			fail(path+".Height", actualHeight, room.Height)
		}
	}

	for firstIndex := 0; firstIndex < len(layout.Rooms); firstIndex++ {
		for secondIndex := firstIndex + 1; secondIndex < len(layout.Rooms); secondIndex++ {
			first := layout.Rooms[firstIndex]
			second := layout.Rooms[secondIndex]
			pairPath := fmt.Sprintf("Layout.Rooms[%d,%d]", firstIndex, secondIndex)
			if !footprintsRespectGap(first.Cells, second.Cells, effective.roomGeometry.MinRoomGap) {
				fail(pairPath+".MinRoomGap", fmt.Sprintf("> %d", effective.roomGeometry.MinRoomGap), "violado")
			}
			if !anchorsRespectDistance(first.At, second.At, effective.minDistance, effective.densityRegions) {
				required := max(localMinDistance(first.At, effective.minDistance, effective.densityRegions), localMinDistance(second.At, effective.minDistance, effective.densityRegions))
				fail(pairPath+".MinDistance", fmt.Sprintf(">= %g", required), squaredCellDistance(first.At, second.At))
			}
		}
	}

	corridorCells := make(map[Cell][]CorridorID)
	doorCorridors := make(map[DoorID][]CorridorID)
	adjacency := make([][]int, len(layout.Rooms))
	for corridorIndex, corridor := range layout.Corridors {
		path := fmt.Sprintf("Layout.Corridors[%d]", corridorIndex)
		if corridor.ID != CorridorID(corridorIndex) {
			fail(path+".ID", CorridorID(corridorIndex), corridor.ID)
		}
		fromIndex, fromExists := roomIndexByID[corridor.FromRoomID]
		toIndex, toExists := roomIndexByID[corridor.ToRoomID]
		if !fromExists {
			fail(path+".FromRoomID", "RoomID existente", corridor.FromRoomID)
		}
		if !toExists {
			fail(path+".ToRoomID", "RoomID existente", corridor.ToRoomID)
		}
		if corridor.FromRoomID == corridor.ToRoomID {
			fail(path+".ToRoomID", "distinto de FromRoomID", corridor.ToRoomID)
		}
		if fromExists && toExists && corridor.FromRoomID != corridor.ToRoomID {
			adjacency[fromIndex] = append(adjacency[fromIndex], toIndex)
			adjacency[toIndex] = append(adjacency[toIndex], fromIndex)
		}
		fromDoor, fromDoorExists := doorAt(layout.Doors, corridor.FromDoorID)
		toDoor, toDoorExists := doorAt(layout.Doors, corridor.ToDoorID)
		if !fromDoorExists {
			fail(path+".FromDoorID", "DoorID existente", corridor.FromDoorID)
		} else if fromDoor.RoomID != corridor.FromRoomID {
			fail(path+".FromDoorID.RoomID", corridor.FromRoomID, fromDoor.RoomID)
		}
		if !toDoorExists {
			fail(path+".ToDoorID", "DoorID existente", corridor.ToDoorID)
		} else if toDoor.RoomID != corridor.ToRoomID {
			fail(path+".ToDoorID.RoomID", corridor.ToRoomID, toDoor.RoomID)
		}
		if corridor.FromDoorID == corridor.ToDoorID {
			fail(path+".ToDoorID", "distinto de FromDoorID", corridor.ToDoorID)
		}
		doorCorridors[corridor.FromDoorID] = append(doorCorridors[corridor.FromDoorID], corridor.ID)
		doorCorridors[corridor.ToDoorID] = append(doorCorridors[corridor.ToDoorID], corridor.ID)

		for cellIndex, cell := range corridor.Cells {
			cellPath := fmt.Sprintf("%s.Cells[%d]", path, cellIndex)
			if !cellInsideGrid(cell, effective.width, effective.height) {
				fail(cellPath, "dentro do Grid", cell)
			}
			if owner, occupied := roomCells[cell]; occupied {
				fail(cellPath, "externa aos footprints", fmt.Sprintf("RoomID %d", owner))
			}
			corridorCells[cell] = append(corridorCells[cell], corridor.ID)
			if cellIndex > 0 && manhattanDistance(corridor.Cells[cellIndex-1], cell) != 1 {
				fail(cellPath, "adjacente à Cell anterior", cell)
			}
		}
		if fromDoorExists && toDoorExists {
			fromOutside := addCell(fromDoor.At, fromDoor.Direction.Delta())
			toOutside := addCell(toDoor.At, toDoor.Direction.Delta())
			if len(corridor.Cells) == 0 {
				if fromOutside != toDoor.At || toOutside != fromDoor.At {
					fail(path+".Cells", "vazio apenas entre Doors adjacentes e opostas", corridor.Cells)
				}
			} else {
				if corridor.Cells[0] != fromOutside {
					fail(path+".Cells[0]", fromOutside, corridor.Cells[0])
				}
				lastIndex := len(corridor.Cells) - 1
				if corridor.Cells[lastIndex] != toOutside {
					fail(fmt.Sprintf("%s.Cells[%d]", path, lastIndex), toOutside, corridor.Cells[lastIndex])
				}
			}
		}
	}

	expectedCorridors := 0
	if len(layout.Rooms) > 1 {
		expectedCorridors = len(layout.Rooms) - 1
	}
	if len(layout.Corridors) < expectedCorridors {
		fail("Layout.Corridors.len", fmt.Sprintf(">= %d", expectedCorridors), len(layout.Corridors))
	}
	if effective.extraEdgeCount == 0 && len(layout.Corridors) != expectedCorridors {
		fail("Layout.Corridors.len", expectedCorridors, len(layout.Corridors))
	}
	if len(layout.Rooms) > 0 {
		visited := graphReachable(adjacency, 0)
		for roomIndex, reached := range visited {
			if !reached {
				fail(fmt.Sprintf("Layout.Rooms[%d]", roomIndex), "alcançável a partir da RoomID 0", "desconectada")
			}
		}
	}

	expectedRoomDoors := make(map[RoomID][]DoorID, len(layout.Rooms))
	seenDoors := make(map[doorInvariantKey]DoorID, len(layout.Doors))
	for doorIndex, door := range layout.Doors {
		path := fmt.Sprintf("Layout.Doors[%d]", doorIndex)
		if door.ID != DoorID(doorIndex) {
			fail(path+".ID", DoorID(doorIndex), door.ID)
		}
		room, roomExists := roomByID[door.RoomID]
		if !roomExists {
			fail(path+".RoomID", "RoomID existente", door.RoomID)
		} else if owner, occupied := roomCells[door.At]; !occupied || owner != door.RoomID {
			fail(path+".At", fmt.Sprintf("Cell da RoomID %d", door.RoomID), door.At)
		}
		if door.Direction < DirectionNorth || door.Direction > DirectionWest {
			fail(path+".Direction", "North, East, South ou West", door.Direction)
		} else {
			outside := addCell(door.At, door.Direction.Delta())
			if !cellInsideGrid(outside, effective.width, effective.height) {
				fail(path+".At+Direction", "dentro do Grid", outside)
			}
			if owner, occupied := roomCells[outside]; occupied && owner == door.RoomID {
				fail(path+".At+Direction", "fora do próprio footprint", outside)
			}
		}
		key := doorInvariantKey{roomID: door.RoomID, at: door.At, direction: door.Direction}
		if previous, exists := seenDoors[key]; exists {
			fail(path, "Door única por (RoomID, At, Direction)", fmt.Sprintf("duplica DoorID %d", previous))
		}
		seenDoors[key] = door.ID
		expectedRoomDoors[door.RoomID] = append(expectedRoomDoors[door.RoomID], door.ID)
		wantedCorridors := doorCorridors[door.ID]
		if !reflect.DeepEqual(wantedCorridors, door.CorridorIDs) {
			fail(path+".CorridorIDs", wantedCorridors, door.CorridorIDs)
		}
		if len(door.CorridorIDs) == 0 {
			fail(path+".CorridorIDs.len", "ao menos 1", 0)
		}
		if !strictlyIncreasingCorridorIDs(door.CorridorIDs) {
			fail(path+".CorridorIDs", "ordem numérica crescente", door.CorridorIDs)
		}
		_ = room
	}

	for roomIndex, room := range layout.Rooms {
		wantedDoorIDs := expectedRoomDoors[room.ID]
		sort.Slice(wantedDoorIDs, func(first, second int) bool {
			firstDoor, _ := doorAt(layout.Doors, wantedDoorIDs[first])
			secondDoor, _ := doorAt(layout.Doors, wantedDoorIDs[second])
			return doorLess(firstDoor, secondDoor)
		})
		if !reflect.DeepEqual(wantedDoorIDs, room.DoorIDs) {
			fail(fmt.Sprintf("Layout.Rooms[%d].DoorIDs", roomIndex), wantedDoorIDs, room.DoorIDs)
		}
	}

	gridLimit := min(len(layout.Grid.Cells), wantedGridCells)
	for cellIndex := 0; cellIndex < gridLimit; cellIndex++ {
		state := layout.Grid.Cells[cellIndex]
		y := cellIndex / int(effective.width)
		x := cellIndex - y*int(effective.width)
		wantedAt := Cell{X: int32(x), Y: int32(y)}
		path := fmt.Sprintf("Layout.Grid.Cells[%d]", cellIndex)
		if state.At != wantedAt {
			fail(path+".At", wantedAt, state.At)
		}
		roomID, isRoom := roomCells[wantedAt]
		corridorIDs, isCorridor := corridorCells[wantedAt]
		switch {
		case isRoom:
			if state.Kind != CellKindRoom {
				fail(path+".Kind", CellKindRoom, state.Kind)
			}
			if state.RoomID == nil {
				fail(path+".RoomID", roomID, nil)
			} else if *state.RoomID != roomID {
				fail(path+".RoomID", roomID, *state.RoomID)
			}
			if len(state.CorridorIDs) != 0 {
				fail(path+".CorridorIDs", []CorridorID(nil), state.CorridorIDs)
			}
		case isCorridor:
			if state.Kind != CellKindCorridor {
				fail(path+".Kind", CellKindCorridor, state.Kind)
			}
			if state.RoomID != nil {
				fail(path+".RoomID", nil, *state.RoomID)
			}
			if !reflect.DeepEqual(corridorIDs, state.CorridorIDs) {
				fail(path+".CorridorIDs", corridorIDs, state.CorridorIDs)
			}
			if !strictlyIncreasingCorridorIDs(state.CorridorIDs) {
				fail(path+".CorridorIDs", "ordem numérica crescente", state.CorridorIDs)
			}
		default:
			if state.Kind != CellKindEmpty {
				fail(path+".Kind", CellKindEmpty, state.Kind)
			}
			if state.RoomID != nil {
				fail(path+".RoomID", nil, *state.RoomID)
			}
			if len(state.CorridorIDs) != 0 {
				fail(path+".CorridorIDs", []CorridorID(nil), state.CorridorIDs)
			}
		}
	}

	requestedCounts := make(map[RoomRole]uint32, len(effective.roomRoleRequests))
	for _, request := range effective.roomRoleRequests {
		requestedCounts[request.Role] = request.Count
	}
	actualCounts := make(map[RoomRole]uint32)
	for roomIndex, room := range layout.Rooms {
		if room.Role == nil {
			continue
		}
		limit, requested := requestedCounts[*room.Role]
		if !requested {
			fail(fmt.Sprintf("Layout.Rooms[%d].Role", roomIndex), "papel solicitado", *room.Role)
			continue
		}
		actualCounts[*room.Role]++
		if actualCounts[*room.Role] > limit {
			fail(fmt.Sprintf("Layout.Rooms[%d].Role", roomIndex), fmt.Sprintf("Count <= %d", limit), actualCounts[*room.Role])
		}
	}
	if len(effective.roomRoleRequests) == 0 {
		for roomIndex, room := range layout.Rooms {
			if room.Role != nil {
				fail(fmt.Sprintf("Layout.Rooms[%d].Role", roomIndex), nil, *room.Role)
			}
		}
	}

	return failures
}

func assertLayoutInvariants(t *testing.T, effective effectiveConfig, layout Layout) {
	t.Helper()
	for _, failure := range layoutInvariantFailures(effective, layout) {
		assert.Fail(t, "invariante de Layout violada", failure)
	}
}

type doorInvariantKey struct {
	roomID    RoomID
	at        Cell
	direction Direction
}

func cellInsideGrid(cell Cell, width, height uint32) bool {
	return cell.X >= 0 && cell.Y >= 0 && int64(cell.X) < int64(width) && int64(cell.Y) < int64(height)
}

func addCell(first, second Cell) Cell {
	return Cell{X: first.X + second.X, Y: first.Y + second.Y}
}

func manhattanDistance(first, second Cell) int64 {
	deltaX := absInt64(int64(first.X) - int64(second.X))
	deltaY := absInt64(int64(first.Y) - int64(second.Y))
	return deltaX + deltaY
}

func squaredCellDistance(first, second Cell) int64 {
	deltaX := int64(first.X) - int64(second.X)
	deltaY := int64(first.Y) - int64(second.Y)
	deltaXSquared := deltaX * deltaX
	deltaYSquared := deltaY * deltaY
	return deltaXSquared + deltaYSquared
}

func cellsAreFourConnected(cells []Cell) bool {
	if len(cells) == 0 {
		return false
	}
	set := make(map[Cell]struct{}, len(cells))
	for _, cell := range cells {
		set[cell] = struct{}{}
	}
	visited := map[Cell]struct{}{cells[0]: {}}
	queue := []Cell{cells[0]}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for direction := DirectionNorth; direction <= DirectionWest; direction++ {
			neighbor := addCell(current, direction.Delta())
			if _, exists := set[neighbor]; !exists {
				continue
			}
			if _, reached := visited[neighbor]; reached {
				continue
			}
			visited[neighbor] = struct{}{}
			queue = append(queue, neighbor)
		}
	}
	return len(visited) == len(set)
}

func doorAt(doors []Door, id DoorID) (Door, bool) {
	index := int(id)
	if index < 0 || index >= len(doors) || doors[index].ID != id {
		return Door{}, false
	}
	return doors[index], true
}

func doorLess(first, second Door) bool {
	if first.At.Y != second.At.Y {
		return first.At.Y < second.At.Y
	}
	if first.At.X != second.At.X {
		return first.At.X < second.At.X
	}
	return first.Direction < second.Direction
}

func strictlyIncreasingCorridorIDs(ids []CorridorID) bool {
	for index := 1; index < len(ids); index++ {
		if ids[index] <= ids[index-1] {
			return false
		}
	}
	return true
}

func graphReachable(adjacency [][]int, start int) []bool {
	visited := make([]bool, len(adjacency))
	if start < 0 || start >= len(adjacency) {
		return visited
	}
	visited[start] = true
	queue := []int{start}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, neighbor := range adjacency[current] {
			if neighbor < 0 || neighbor >= len(visited) || visited[neighbor] {
				continue
			}
			visited[neighbor] = true
			queue = append(queue, neighbor)
		}
	}
	return visited
}
