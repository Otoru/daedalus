package daedalus

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

// layoutInvariantFailures is the single oracle for the section 5.2 Layout
// invariants. Each diagnostic contains a field path, expected value, and actual
// value so a regression cannot hide behind a hash comparison.
//
// The helpers below append to the same failure list, in this order. A new
// check belongs at the end of its section: tests assert on the first path.
func layoutInvariantFailures(effective effectiveConfig, layout Layout) []string {
	state := &layoutInvariantCheck{
		effective: effective,
		layout:    layout,
		failures:  make([]string, 0),
	}
	state.checkLayoutShape()
	state.checkRoomFootprints()
	state.checkRoomPairGapAndDistance()
	state.checkCorridors()
	state.checkCorridorCountAndConnectivity()
	state.checkDoors()
	state.checkAssignedDoorOrder()
	state.checkGridCells()
	state.checkRoomRoles()
	return state.failures
}

// layoutInvariantCheck accumulates section 5.2 diagnostics and the indexes
// later sections need: footprint ownership, corridor cells, and door links.
type layoutInvariantCheck struct {
	effective effectiveConfig
	layout    Layout
	failures  []string

	roomByID          map[RoomID]Room
	roomIndexByID     map[RoomID]int
	roomCells         map[Cell]RoomID
	corridorCells     map[Cell][]CorridorID
	doorCorridors     map[DoorID][]CorridorID
	adjacency         [][]int
	expectedRoomDoors map[RoomID][]DoorID
	wantedGridCells   int
}

type roomFootprintBounds struct {
	minimumX int32
	maximumX int32
	minimumY int32
	maximumY int32
}

func (state *layoutInvariantCheck) fail(path string, expected, actual any) {
	state.failures = append(state.failures, fmt.Sprintf("%s: expected %v; actual %v", path, expected, actual))
}

func (state *layoutInvariantCheck) checkLayoutShape() {
	effective := state.effective
	layout := state.layout
	if layout.Seed != effective.seed {
		state.fail("Layout.Seed", effective.seed, layout.Seed)
	}
	if layout.Grid.Width != effective.width {
		state.fail("Layout.Grid.Width", effective.width, layout.Grid.Width)
	}
	if layout.Grid.Height != effective.height {
		state.fail("Layout.Grid.Height", effective.height, layout.Grid.Height)
	}
	if layout.Grid.CellSize != effective.cellSize {
		state.fail("Layout.Grid.CellSize", effective.cellSize, layout.Grid.CellSize)
	}
	state.wantedGridCells = int(uint64(effective.width) * uint64(effective.height))
	if len(layout.Grid.Cells) != state.wantedGridCells {
		state.fail("Layout.Grid.Cells.len", state.wantedGridCells, len(layout.Grid.Cells))
	}
	if len(layout.Rooms) == 0 {
		state.fail("Layout.Rooms.len", "at least 1", 0)
	}
	if uint32(len(layout.Rooms)) > effective.maxRooms {
		state.fail("Layout.Rooms.len", fmt.Sprintf("<= %d", effective.maxRooms), len(layout.Rooms))
	}
}

func (state *layoutInvariantCheck) checkRoomFootprints() {
	state.roomByID = make(map[RoomID]Room, len(state.layout.Rooms))
	state.roomIndexByID = make(map[RoomID]int, len(state.layout.Rooms))
	state.roomCells = make(map[Cell]RoomID)
	for roomIndex, room := range state.layout.Rooms {
		path := fmt.Sprintf("Layout.Rooms[%d]", roomIndex)
		state.checkRoomIdentity(path, roomIndex, room)
		if !state.roomMatchesCanonicalMask(path, room) {
			continue
		}
		if room.At != room.Cells[0] {
			state.fail(path+".At", room.Cells[0], room.At)
		}
		bounds := state.recordRoomFootprint(path, room)
		state.checkRoomBounds(path, room, bounds)
	}
}

func (state *layoutInvariantCheck) checkRoomIdentity(path string, roomIndex int, room Room) {
	if room.ID != RoomID(roomIndex) {
		state.fail(path+".ID", RoomID(roomIndex), room.ID)
	}
	if _, exists := state.roomByID[room.ID]; exists {
		state.fail(path+".ID", "unique", room.ID)
	}
	state.roomByID[room.ID] = room
	state.roomIndexByID[room.ID] = roomIndex
}

func (state *layoutInvariantCheck) roomMatchesCanonicalMask(path string, room Room) bool {
	offsets := RoomShapeOffsets(room.Shape, room.Width, room.Height)
	wantedCells := make([]Cell, len(offsets))
	for cellIndex, offset := range offsets {
		wantedCells[cellIndex] = Cell{X: room.Origin.X + offset.X, Y: room.Origin.Y + offset.Y}
	}
	if !reflect.DeepEqual(wantedCells, room.Cells) {
		state.fail(path+".Cells", wantedCells, room.Cells)
	}
	if len(room.Cells) == 0 {
		state.fail(path+".Cells.len", "at least 1", 0)
		return false
	}
	return true
}

func (state *layoutInvariantCheck) recordRoomFootprint(path string, room Room) roomFootprintBounds {
	bounds := roomFootprintBounds{
		minimumX: room.Cells[0].X,
		maximumX: room.Cells[0].X,
		minimumY: room.Cells[0].Y,
		maximumY: room.Cells[0].Y,
	}
	seenCells := make(map[Cell]struct{}, len(room.Cells))
	for cellIndex, cell := range room.Cells {
		cellPath := fmt.Sprintf("%s.Cells[%d]", path, cellIndex)
		if !cellInsideGrid(cell, state.effective.width, state.effective.height) {
			state.fail(cellPath, "inside the Grid", cell)
		}
		if _, exists := seenCells[cell]; exists {
			state.fail(cellPath, "unique Cell in footprint", cell)
		}
		seenCells[cell] = struct{}{}
		if owner, occupied := state.roomCells[cell]; occupied {
			state.fail(cellPath, "no overlap", fmt.Sprintf("also belongs to RoomID %d", owner))
		}
		state.roomCells[cell] = room.ID
		bounds.minimumX = min(bounds.minimumX, cell.X)
		bounds.maximumX = max(bounds.maximumX, cell.X)
		bounds.minimumY = min(bounds.minimumY, cell.Y)
		bounds.maximumY = max(bounds.maximumY, cell.Y)
	}
	return bounds
}

func (state *layoutInvariantCheck) checkRoomBounds(path string, room Room, bounds roomFootprintBounds) {
	if !cellsAreFourConnected(room.Cells) {
		state.fail(path+".Cells", "4-connected footprint", room.Cells)
	}
	actualOrigin := Cell{X: bounds.minimumX, Y: bounds.minimumY}
	if room.Origin != actualOrigin {
		state.fail(path+".Origin", actualOrigin, room.Origin)
	}
	actualWidth := uint32(int64(bounds.maximumX) - int64(bounds.minimumX) + 1)
	actualHeight := uint32(int64(bounds.maximumY) - int64(bounds.minimumY) + 1)
	if room.Width != actualWidth {
		state.fail(path+".Width", actualWidth, room.Width)
	}
	if room.Height != actualHeight {
		state.fail(path+".Height", actualHeight, room.Height)
	}
}

func (state *layoutInvariantCheck) checkRoomPairGapAndDistance() {
	effective := state.effective
	for firstIndex := 0; firstIndex < len(state.layout.Rooms); firstIndex++ {
		for secondIndex := firstIndex + 1; secondIndex < len(state.layout.Rooms); secondIndex++ {
			first := state.layout.Rooms[firstIndex]
			second := state.layout.Rooms[secondIndex]
			pairPath := fmt.Sprintf("Layout.Rooms[%d,%d]", firstIndex, secondIndex)
			if !footprintsRespectGap(first.Cells, second.Cells, effective.roomGeometry.MinRoomGap) {
				state.fail(pairPath+".MinRoomGap", fmt.Sprintf("> %d", effective.roomGeometry.MinRoomGap), "violated")
			}
			if !anchorsRespectDistance(first.At, second.At, effective.minDistance, effective.densityRegions) {
				required := max(
					localMinDistance(first.At, effective.minDistance, effective.densityRegions),
					localMinDistance(second.At, effective.minDistance, effective.densityRegions),
				)
				state.fail(pairPath+".MinDistance", fmt.Sprintf(">= %g", required), squaredCellDistance(first.At, second.At))
			}
		}
	}
}

func (state *layoutInvariantCheck) checkCorridors() {
	state.corridorCells = make(map[Cell][]CorridorID)
	state.doorCorridors = make(map[DoorID][]CorridorID)
	state.adjacency = make([][]int, len(state.layout.Rooms))
	for corridorIndex, corridor := range state.layout.Corridors {
		path := fmt.Sprintf("Layout.Corridors[%d]", corridorIndex)
		state.checkCorridorEndpoints(path, corridorIndex, corridor)
		fromDoor, toDoor, fromOK, toOK := state.checkCorridorDoors(path, corridor)
		state.checkCorridorCells(path, corridor)
		state.checkCorridorDoorGeometry(path, corridor, fromDoor, toDoor, fromOK, toOK)
	}
}

func (state *layoutInvariantCheck) checkCorridorEndpoints(path string, corridorIndex int, corridor Corridor) {
	if corridor.ID != CorridorID(corridorIndex) {
		state.fail(path+".ID", CorridorID(corridorIndex), corridor.ID)
	}
	fromIndex, fromExists := state.roomIndexByID[corridor.FromRoomID]
	toIndex, toExists := state.roomIndexByID[corridor.ToRoomID]
	if !fromExists {
		state.fail(path+".FromRoomID", "existing RoomID", corridor.FromRoomID)
	}
	if !toExists {
		state.fail(path+".ToRoomID", "existing RoomID", corridor.ToRoomID)
	}
	if corridor.FromRoomID == corridor.ToRoomID {
		state.fail(path+".ToRoomID", "distinct from FromRoomID", corridor.ToRoomID)
	}
	if fromExists && toExists && corridor.FromRoomID != corridor.ToRoomID {
		state.adjacency[fromIndex] = append(state.adjacency[fromIndex], toIndex)
		state.adjacency[toIndex] = append(state.adjacency[toIndex], fromIndex)
	}
}

func (state *layoutInvariantCheck) checkCorridorDoors(path string, corridor Corridor) (Door, Door, bool, bool) {
	fromDoor, fromDoorExists := doorAt(state.layout.Doors, corridor.FromDoorID)
	toDoor, toDoorExists := doorAt(state.layout.Doors, corridor.ToDoorID)
	if !fromDoorExists {
		state.fail(path+".FromDoorID", "existing DoorID", corridor.FromDoorID)
	} else if fromDoor.RoomID != corridor.FromRoomID {
		state.fail(path+".FromDoorID.RoomID", corridor.FromRoomID, fromDoor.RoomID)
	}
	if !toDoorExists {
		state.fail(path+".ToDoorID", "existing DoorID", corridor.ToDoorID)
	} else if toDoor.RoomID != corridor.ToRoomID {
		state.fail(path+".ToDoorID.RoomID", corridor.ToRoomID, toDoor.RoomID)
	}
	if corridor.FromDoorID == corridor.ToDoorID {
		state.fail(path+".ToDoorID", "distinct from FromDoorID", corridor.ToDoorID)
	}
	state.doorCorridors[corridor.FromDoorID] = append(state.doorCorridors[corridor.FromDoorID], corridor.ID)
	state.doorCorridors[corridor.ToDoorID] = append(state.doorCorridors[corridor.ToDoorID], corridor.ID)
	return fromDoor, toDoor, fromDoorExists, toDoorExists
}

func (state *layoutInvariantCheck) checkCorridorCells(path string, corridor Corridor) {
	for cellIndex, cell := range corridor.Cells {
		cellPath := fmt.Sprintf("%s.Cells[%d]", path, cellIndex)
		if !cellInsideGrid(cell, state.effective.width, state.effective.height) {
			state.fail(cellPath, "inside the Grid", cell)
		}
		if owner, occupied := state.roomCells[cell]; occupied {
			state.fail(cellPath, "outside the footprints", fmt.Sprintf("RoomID %d", owner))
		}
		state.corridorCells[cell] = append(state.corridorCells[cell], corridor.ID)
		if cellIndex > 0 && manhattanDistance(corridor.Cells[cellIndex-1], cell) != 1 {
			state.fail(cellPath, "adjacent to previous Cell", cell)
		}
	}
}

func (state *layoutInvariantCheck) checkCorridorDoorGeometry(path string, corridor Corridor, fromDoor, toDoor Door, fromOK, toOK bool) {
	if !fromOK || !toOK {
		return
	}
	fromOutside := addCell(fromDoor.At, fromDoor.Direction.Delta())
	toOutside := addCell(toDoor.At, toDoor.Direction.Delta())
	if len(corridor.Cells) == 0 {
		if fromOutside != toDoor.At || toOutside != fromDoor.At {
			state.fail(path+".Cells", "empty only between adjacent opposite Doors", corridor.Cells)
		}
		return
	}
	if corridor.Cells[0] != fromOutside {
		state.fail(path+".Cells[0]", fromOutside, corridor.Cells[0])
	}
	lastIndex := len(corridor.Cells) - 1
	if corridor.Cells[lastIndex] != toOutside {
		state.fail(fmt.Sprintf("%s.Cells[%d]", path, lastIndex), toOutside, corridor.Cells[lastIndex])
	}
}

func (state *layoutInvariantCheck) checkCorridorCountAndConnectivity() {
	expectedCorridors := 0
	if len(state.layout.Rooms) > 1 {
		expectedCorridors = len(state.layout.Rooms) - 1
	}
	if len(state.layout.Corridors) < expectedCorridors {
		state.fail("Layout.Corridors.len", fmt.Sprintf(">= %d", expectedCorridors), len(state.layout.Corridors))
	}
	if state.effective.extraEdgeCount == 0 && len(state.layout.Corridors) != expectedCorridors {
		state.fail("Layout.Corridors.len", expectedCorridors, len(state.layout.Corridors))
	}
	if len(state.layout.Rooms) == 0 {
		return
	}
	visited := graphReachable(state.adjacency, 0)
	for roomIndex, reached := range visited {
		if !reached {
			state.fail(fmt.Sprintf("Layout.Rooms[%d]", roomIndex), "reachable from RoomID 0", "disconnected")
		}
	}
}

func (state *layoutInvariantCheck) checkDoors() {
	state.expectedRoomDoors = make(map[RoomID][]DoorID, len(state.layout.Rooms))
	seenDoors := make(map[doorInvariantKey]DoorID, len(state.layout.Doors))
	for doorIndex, door := range state.layout.Doors {
		path := fmt.Sprintf("Layout.Doors[%d]", doorIndex)
		state.checkDoorPlacement(path, doorIndex, door)
		state.rememberDoor(path, door, seenDoors)
	}
}

func (state *layoutInvariantCheck) checkDoorPlacement(path string, doorIndex int, door Door) {
	if door.ID != DoorID(doorIndex) {
		state.fail(path+".ID", DoorID(doorIndex), door.ID)
	}
	_, roomExists := state.roomByID[door.RoomID]
	if !roomExists {
		state.fail(path+".RoomID", "existing RoomID", door.RoomID)
	} else if owner, occupied := state.roomCells[door.At]; !occupied || owner != door.RoomID {
		state.fail(path+".At", fmt.Sprintf("Cell of RoomID %d", door.RoomID), door.At)
	}
	state.checkDoorDirection(path, door)
}

func (state *layoutInvariantCheck) checkDoorDirection(path string, door Door) {
	if door.Direction < DirectionNorth || door.Direction > DirectionWest {
		state.fail(path+".Direction", "North, East, South, or West", door.Direction)
		return
	}
	outside := addCell(door.At, door.Direction.Delta())
	if !cellInsideGrid(outside, state.effective.width, state.effective.height) {
		state.fail(path+".At+Direction", "inside the Grid", outside)
	}
	if owner, occupied := state.roomCells[outside]; occupied && owner == door.RoomID {
		state.fail(path+".At+Direction", "outside its own footprint", outside)
	}
}

func (state *layoutInvariantCheck) rememberDoor(path string, door Door, seenDoors map[doorInvariantKey]DoorID) {
	key := doorInvariantKey{roomID: door.RoomID, at: door.At, direction: door.Direction}
	if previous, exists := seenDoors[key]; exists {
		state.fail(path, "unique Door by (RoomID, At, Direction)", fmt.Sprintf("duplicates DoorID %d", previous))
	}
	seenDoors[key] = door.ID
	state.expectedRoomDoors[door.RoomID] = append(state.expectedRoomDoors[door.RoomID], door.ID)
	wantedCorridors := state.doorCorridors[door.ID]
	if !reflect.DeepEqual(wantedCorridors, door.CorridorIDs) {
		state.fail(path+".CorridorIDs", wantedCorridors, door.CorridorIDs)
	}
	if len(door.CorridorIDs) == 0 {
		state.fail(path+".CorridorIDs.len", "at least 1", 0)
	}
	if !strictlyIncreasingCorridorIDs(door.CorridorIDs) {
		state.fail(path+".CorridorIDs", "ascending numeric order", door.CorridorIDs)
	}
}

func (state *layoutInvariantCheck) checkAssignedDoorOrder() {
	for roomIndex, room := range state.layout.Rooms {
		wantedDoorIDs := state.expectedRoomDoors[room.ID]
		sort.Slice(wantedDoorIDs, func(first, second int) bool {
			firstDoor, _ := doorAt(state.layout.Doors, wantedDoorIDs[first])
			secondDoor, _ := doorAt(state.layout.Doors, wantedDoorIDs[second])
			return doorLess(firstDoor, secondDoor)
		})
		if !reflect.DeepEqual(wantedDoorIDs, room.DoorIDs) {
			state.fail(fmt.Sprintf("Layout.Rooms[%d].DoorIDs", roomIndex), wantedDoorIDs, room.DoorIDs)
		}
	}
}

func (state *layoutInvariantCheck) checkGridCells() {
	gridLimit := min(len(state.layout.Grid.Cells), state.wantedGridCells)
	for cellIndex := 0; cellIndex < gridLimit; cellIndex++ {
		state.checkGridCell(cellIndex)
	}
}

func (state *layoutInvariantCheck) checkGridCell(cellIndex int) {
	cell := state.layout.Grid.Cells[cellIndex]
	y := cellIndex / int(state.effective.width)
	x := cellIndex - y*int(state.effective.width)
	wantedAt := Cell{X: int32(x), Y: int32(y)}
	path := fmt.Sprintf("Layout.Grid.Cells[%d]", cellIndex)
	if cell.At != wantedAt {
		state.fail(path+".At", wantedAt, cell.At)
	}
	roomID, isRoom := state.roomCells[wantedAt]
	corridorIDs, isCorridor := state.corridorCells[wantedAt]
	switch {
	case isRoom:
		state.checkGridRoomCell(path, cell, roomID)
	case isCorridor:
		state.checkGridCorridorCell(path, cell, corridorIDs)
	default:
		state.checkGridEmptyCell(path, cell)
	}
}

func (state *layoutInvariantCheck) checkGridRoomCell(path string, cell CellState, roomID RoomID) {
	if cell.Kind != CellKindRoom {
		state.fail(path+".Kind", CellKindRoom, cell.Kind)
	}
	if cell.RoomID == nil {
		state.fail(path+".RoomID", roomID, nil)
	} else if *cell.RoomID != roomID {
		state.fail(path+".RoomID", roomID, *cell.RoomID)
	}
	if len(cell.CorridorIDs) != 0 {
		state.fail(path+".CorridorIDs", []CorridorID(nil), cell.CorridorIDs)
	}
}

func (state *layoutInvariantCheck) checkGridCorridorCell(path string, cell CellState, corridorIDs []CorridorID) {
	if cell.Kind != CellKindCorridor {
		state.fail(path+".Kind", CellKindCorridor, cell.Kind)
	}
	if cell.RoomID != nil {
		state.fail(path+".RoomID", nil, *cell.RoomID)
	}
	if !reflect.DeepEqual(corridorIDs, cell.CorridorIDs) {
		state.fail(path+".CorridorIDs", corridorIDs, cell.CorridorIDs)
	}
	if !strictlyIncreasingCorridorIDs(cell.CorridorIDs) {
		state.fail(path+".CorridorIDs", "ascending numeric order", cell.CorridorIDs)
	}
}

func (state *layoutInvariantCheck) checkGridEmptyCell(path string, cell CellState) {
	if cell.Kind != CellKindEmpty {
		state.fail(path+".Kind", CellKindEmpty, cell.Kind)
	}
	if cell.RoomID != nil {
		state.fail(path+".RoomID", nil, *cell.RoomID)
	}
	if len(cell.CorridorIDs) != 0 {
		state.fail(path+".CorridorIDs", []CorridorID(nil), cell.CorridorIDs)
	}
}

func (state *layoutInvariantCheck) checkRoomRoles() {
	requestedCounts := make(map[RoomRole]uint32, len(state.effective.roomRoleRequests))
	for _, request := range state.effective.roomRoleRequests {
		requestedCounts[request.Role] = request.Count
	}
	actualCounts := make(map[RoomRole]uint32)
	for roomIndex, room := range state.layout.Rooms {
		if room.Role == nil {
			continue
		}
		limit, requested := requestedCounts[*room.Role]
		if !requested {
			state.fail(fmt.Sprintf("Layout.Rooms[%d].Role", roomIndex), "requested role", *room.Role)
			continue
		}
		actualCounts[*room.Role]++
		if actualCounts[*room.Role] > limit {
			state.fail(fmt.Sprintf("Layout.Rooms[%d].Role", roomIndex), fmt.Sprintf("Count <= %d", limit), actualCounts[*room.Role])
		}
	}
	if len(state.effective.roomRoleRequests) != 0 {
		return
	}
	for roomIndex, room := range state.layout.Rooms {
		if room.Role != nil {
			state.fail(fmt.Sprintf("Layout.Rooms[%d].Role", roomIndex), nil, *room.Role)
		}
	}
}

func assertLayoutInvariants(t *testing.T, effective effectiveConfig, layout Layout) {
	t.Helper()
	for _, failure := range layoutInvariantFailures(effective, layout) {
		assert.Fail(t, "Layout invariant violated", failure)
	}
}

// TestLayoutInvariantFailuresKeepFirstDiagnostic freezes the order in which
// layoutInvariantFailures appends. Later corruption must not leapfrog an
// earlier field: callers assert on the first divergent path.
func TestLayoutInvariantFailuresKeepFirstDiagnostic(t *testing.T) {
	config := Config{Width: 24, Height: 24, Seed: 5010, MaxRooms: 12}
	effective, err := normalizeConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	layout, err := (Generator{}).Generate(config)
	if err != nil {
		t.Fatal(err)
	}
	layout.Seed = 1
	layout.Grid.Width = 1
	if len(layout.Rooms) > 0 {
		layout.Rooms[0].ID = 99
		if len(layout.Rooms[0].Cells) > 0 {
			layout.Rooms[0].Cells[0] = Cell{X: -5, Y: -5}
		}
	}
	if len(layout.Grid.Cells) > 0 {
		layout.Grid.Cells[0].At = Cell{X: 99, Y: 99}
	}

	failures := layoutInvariantFailures(effective, layout)
	if len(failures) == 0 {
		t.Fatal("expected invariant failures")
	}
	const firstDiagnostic = "Layout.Seed: expected 5010; actual 1"
	if failures[0] != firstDiagnostic {
		t.Fatalf("first diagnostic = %q, want %q", failures[0], firstDiagnostic)
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
