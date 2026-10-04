package daedalus

import (
	"sort"

	"github.com/Otoru/daedalus/core"
)

const wideRouteIncomingSlots = routingDirectionCount

// widthClearance is the largest corridor width each free Cell can host.
// alongY is the width of a horizontal run (the band extends in Y). alongX is
// the width of a vertical run (the band extends in X). bend is the width of
// the W×W block centred on the Cell by the same side rule as the band.
type widthClearance struct {
	alongY []uint8
	alongX []uint8
	bend   []uint8
}

func newWidthClearance(occupancy *placementOccupancy) *widthClearance {
	cellCount := int(occupancy.width * occupancy.height)
	clearance := &widthClearance{
		alongY: make([]uint8, cellCount),
		alongX: make([]uint8, cellCount),
		bend:   make([]uint8, cellCount),
	}
	fillAlongY(clearance, occupancy)
	fillAlongX(clearance, occupancy)
	fillBend(clearance, occupancy)
	return clearance
}

func (clearance *widthClearance) admits(index int, direction Direction, bend bool, width uint32) bool {
	if clearance == nil || index < 0 {
		return false
	}
	if bend {
		return uint32(clearance.bend[index]) >= width
	}
	if direction == DirectionEast || direction == DirectionWest {
		return uint32(clearance.alongY[index]) >= width
	}
	return uint32(clearance.alongX[index]) >= width
}

func fillAlongY(clearance *widthClearance, occupancy *placementOccupancy) {
	width := int(occupancy.width)
	height := int(occupancy.height)
	for x := 0; x < width; x++ {
		y := 0
		for y < height {
			if !routingCellFree(occupancy, x, y) {
				y++
				continue
			}
			runStart := y
			for y < height && routingCellFree(occupancy, x, y) {
				y++
			}
			for cy := runStart; cy < y; cy++ {
				lowAvailable := cy - runStart
				highAvailable := y - cy - 1
				index, _ := occupancy.index(Cell{X: int32(x), Y: int32(cy)})
				clearance.alongY[index] = maxWidthForExtents(lowAvailable, highAvailable)
			}
		}
	}
}

func fillAlongX(clearance *widthClearance, occupancy *placementOccupancy) {
	width := int(occupancy.width)
	height := int(occupancy.height)
	for y := 0; y < height; y++ {
		x := 0
		for x < width {
			if !routingCellFree(occupancy, x, y) {
				x++
				continue
			}
			runStart := x
			for x < width && routingCellFree(occupancy, x, y) {
				x++
			}
			for cx := runStart; cx < x; cx++ {
				lowAvailable := cx - runStart
				highAvailable := x - cx - 1
				index, _ := occupancy.index(Cell{X: int32(cx), Y: int32(y)})
				clearance.alongX[index] = maxWidthForExtents(lowAvailable, highAvailable)
			}
		}
	}
}

func fillBend(clearance *widthClearance, occupancy *placementOccupancy) {
	width := int(occupancy.width)
	height := int(occupancy.height)
	stride := width + 1
	prefix := make([]int, (height+1)*stride)
	for y := 1; y <= height; y++ {
		for x := 1; x <= width; x++ {
			free := 0
			if routingCellFree(occupancy, x-1, y-1) {
				free = 1
			}
			north := prefix[(y-1)*stride+x]
			west := prefix[y*stride+(x-1)]
			northWest := prefix[(y-1)*stride+(x-1)]
			sum := north + west
			sum -= northWest
			sum += free
			prefix[y*stride+x] = sum
		}
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if !routingCellFree(occupancy, x, y) {
				continue
			}
			index, _ := occupancy.index(Cell{X: int32(x), Y: int32(y)})
			clearance.bend[index] = maxBendWidth(prefix, stride, width, height, x, y)
		}
	}
}

func maxBendWidth(prefix []int, stride, width, height, x, y int) uint8 {
	best := uint8(1)
	for candidate := 2; candidate <= maximumCorridorWidth; candidate++ {
		low := (candidate - 1) / 2
		high := candidate / 2
		x0 := x - low
		y0 := y - low
		x1 := x + high
		y1 := y + high
		if x0 < 0 || y0 < 0 || x1 >= width || y1 >= height {
			break
		}
		if prefixRect(prefix, stride, x0, y0, x1, y1) != candidate*candidate {
			break
		}
		best = uint8(candidate)
	}
	return best
}

func prefixRect(prefix []int, stride, x0, y0, x1, y1 int) int {
	x0++
	y0++
	x1++
	y1++
	total := prefix[y1*stride+x1]
	north := prefix[(y0-1)*stride+x1]
	west := prefix[y1*stride+(x0-1)]
	northWest := prefix[(y0-1)*stride+(x0-1)]
	sum := total - north
	sum -= west
	sum += northWest
	return sum
}

func maxWidthForExtents(lowAvailable, highAvailable int) uint8 {
	best := uint8(1)
	for candidate := 2; candidate <= maximumCorridorWidth; candidate++ {
		low := (candidate - 1) / 2
		high := candidate / 2
		if low > lowAvailable || high > highAvailable {
			break
		}
		best = uint8(candidate)
	}
	return best
}

func routingCellFree(occupancy *placementOccupancy, x, y int) bool {
	_, occupied := occupancy.ownerAt(Cell{X: int32(x), Y: int32(y)})
	return !occupied
}

// drawCorridorWidth picks one width. Candidates are sorted by Width ascending
// before the draw, matching the Plant catalog's weighted selection, so the
// order of Widths in the request cannot change the result.
func drawCorridorWidth(widths []CorridorWidthWeight, stream *core.SplitMix64) uint32 {
	ordered := append([]CorridorWidthWeight(nil), widths...)
	sort.Slice(ordered, func(first, second int) bool {
		return ordered[first].Width < ordered[second].Width
	})
	var total uint64
	for _, candidate := range ordered {
		total += uint64(candidate.Weight)
	}
	draw := stream.UniformInt(weightedSelectionFirstTicket, total)
	var cumulative uint64
	for _, candidate := range ordered {
		cumulative += uint64(candidate.Weight)
		if draw <= cumulative {
			return candidate.Width
		}
	}
	return ordered[len(ordered)-1].Width
}

// routeDegraded tries the drawn width, then each narrower width the caller
// declared, in descending order. It never tries a width that is absent from
// that list. The caller has already drawn once; this loop consumes no further
// randomness. Nothing declared at or below the draw yields an unroutable edge.
func routeDegraded(
	fromOpenings []doorOpening,
	toOpenings []doorOpening,
	order CorridorOrder,
	drawn uint32,
	declared []CorridorWidthWeight,
	search *routingSearch,
) (routedConnection, uint32, bool, error) {
	for _, width := range degradationWidths(declared, drawn) {
		if err := search.ctx.Err(); err != nil {
			return routedConnection{}, 0, false, err
		}
		best, found, err := selectRoutedConnection(fromOpenings, toOpenings, order, search, width)
		if err != nil {
			return routedConnection{}, 0, false, err
		}
		if found {
			return best, width, true, nil
		}
	}
	return routedConnection{}, 0, false, nil
}

// degradationWidths lists declared widths that are at most the drawn width,
// descending and duplicate-free. The result is a slice, never a map, so the
// walk order does not depend on hash iteration.
func degradationWidths(declared []CorridorWidthWeight, drawn uint32) []uint32 {
	if drawn == 0 {
		return nil
	}
	widths := make([]uint32, 0, len(declared))
	for _, candidate := range declared {
		if candidate.Width >= 1 && candidate.Width <= drawn {
			widths = append(widths, candidate.Width)
		}
	}
	sort.Slice(widths, func(first, second int) bool {
		return widths[first] > widths[second]
	})
	if len(widths) == 0 {
		return nil
	}
	unique := make([]uint32, 0, len(widths))
	unique = append(unique, widths[0])
	for _, width := range widths[1:] {
		if width == unique[len(unique)-1] {
			continue
		}
		unique = append(unique, width)
	}
	return unique
}

func (search *routingSearch) routeOpeningPairWide(
	from doorOpening,
	to doorOpening,
	order CorridorOrder,
	width uint32,
	buffer []Cell,
) ([]Cell, bool, error) {
	if !spanFits(from, width, search.occupancy) || !spanFits(to, width, search.occupancy) {
		return buffer[:0], false, nil
	}
	if openingsFaceEachOther(from, to) {
		if spansFace(from, to, width, search.occupancy) {
			return buffer[:0], true, nil
		}
		return buffer[:0], false, nil
	}
	if cells, ok := lRoute(from.outside, to.outside, order, search.occupancy, buffer); ok {
		if search.centerlineAdmits(cells, from.direction, to.direction, width) {
			return cells, true, nil
		}
	}
	if cells, ok := lRoute(from.outside, to.outside, alternateCorridorOrder(order), search.occupancy, buffer); ok {
		if search.centerlineAdmits(cells, from.direction, to.direction, width) {
			return cells, true, nil
		}
	}
	return search.breadthFirstWide(from, to, width, buffer)
}

func (search *routingSearch) centerlineAdmits(cells []Cell, fromDir, toDir Direction, width uint32) bool {
	if len(cells) == 0 {
		return true
	}
	for index := range cells {
		incoming, outgoing := centerlineDirections(cells, index, fromDir, toDir)
		cellIndex, inside := search.occupancy.index(cells[index])
		if !inside {
			return false
		}
		if !search.clearance.admits(cellIndex, incoming, incoming != outgoing, width) {
			return false
		}
	}
	return true
}

func (search *routingSearch) breadthFirstWide(from, to doorOpening, width uint32, buffer []Cell) ([]Cell, bool, error) {
	fromIndex, fromFree := search.freeIndex(from.outside)
	toIndex, toFree := search.freeIndex(to.outside)
	if !fromFree || !toFree {
		return buffer[:0], false, nil
	}
	if fromIndex == toIndex {
		if !search.centerlineAdmits([]Cell{from.outside}, from.direction, to.direction, width) {
			return buffer[:0], false, nil
		}
		buffer = append(buffer[:0], from.outside)
		return buffer, true, nil
	}

	search.ensureWideBuffers()
	search.nextWideGeneration()
	search.wideQueue = search.wideQueue[:0]
	startSlot := wideSlot(fromIndex, from.direction)
	search.wideQueue = append(search.wideQueue, startSlot)
	search.wideVisited[startSlot] = search.wideGeneration
	search.wideParents[startSlot] = routingNoParent

	depart := to.direction.Opposite()
	foundSlot := routingNoParent
	var expansions uint64
	for queueIndex := 0; queueIndex < len(search.wideQueue) && foundSlot == routingNoParent; queueIndex++ {
		if expansions%routingCancellationInterval == 0 {
			if err := search.ctx.Err(); err != nil {
				return buffer[:0], false, err
			}
		}
		expansions++
		currentSlot := search.wideQueue[queueIndex]
		currentIndex := wideCellIndex(currentSlot)
		incoming := Direction(int(currentSlot) % routingDirectionCount)
		if !search.wideExpand(currentIndex, incoming, toIndex, depart, width) {
			continue
		}
		foundSlot = search.wideQueue[len(search.wideQueue)-1]
	}
	if foundSlot == routingNoParent {
		return buffer[:0], false, nil
	}
	return search.cellsFromWideParents(foundSlot, buffer), true, nil
}

// wideExpand enqueues the cardinal neighbours of currentIndex. It reports
// whether the target was inserted. Neighbours expand North, East, South, West.
// A state is marked on insertion, so the parent is the first discovery.
func (search *routingSearch) wideExpand(currentIndex int64, incoming Direction, toIndex int64, depart Direction, width uint32) bool {
	current := search.cellAt(currentIndex)
	currentSlot := wideSlot(currentIndex, incoming)
	for direction := DirectionNorth; direction < Direction(routingDirectionCount); direction++ {
		if !search.clearance.admits(int(currentIndex), direction, incoming != direction, width) {
			continue
		}
		delta := direction.Delta()
		neighborX := int64(current.X) + int64(delta.X)
		neighborY := int64(current.Y) + int64(delta.Y)
		if neighborX < minCellCoordinate || neighborX > maxCellCoordinate ||
			neighborY < minCellCoordinate || neighborY > maxCellCoordinate {
			continue
		}
		neighbor := Cell{X: int32(neighborX), Y: int32(neighborY)}
		neighborIndex, free := search.freeIndex(neighbor)
		if !free {
			continue
		}
		arrivingBend := neighborIndex == toIndex && direction != depart
		if !search.clearance.admits(int(neighborIndex), direction, arrivingBend, width) {
			continue
		}
		slot := wideSlot(neighborIndex, direction)
		if search.wideVisited[slot] == search.wideGeneration {
			continue
		}
		search.wideVisited[slot] = search.wideGeneration
		search.wideParents[slot] = currentSlot
		search.wideQueue = append(search.wideQueue, slot)
		if neighborIndex == toIndex {
			return true
		}
	}
	return false
}

func (search *routingSearch) cellsFromWideParents(slot int64, buffer []Cell) []Cell {
	buffer = buffer[:0]
	for slot != routingNoParent {
		buffer = append(buffer, search.cellAt(wideCellIndex(slot)))
		slot = search.wideParents[slot]
	}
	for left, right := 0, len(buffer)-1; left < right; left, right = left+1, right-1 {
		buffer[left], buffer[right] = buffer[right], buffer[left]
	}
	return buffer
}

func (search *routingSearch) ensureWideBuffers() {
	cellCount := int(search.occupancy.width * search.occupancy.height)
	slots := cellCount * wideRouteIncomingSlots
	if len(search.wideVisited) == slots {
		return
	}
	search.wideVisited = make([]uint32, slots)
	search.wideParents = make([]int64, slots)
	search.wideQueue = make([]int64, 0, cellCount)
}

func (search *routingSearch) nextWideGeneration() {
	search.wideGeneration++
	if search.wideGeneration != 0 {
		return
	}
	clear(search.wideVisited)
	search.wideGeneration = routingFirstGeneration
}

func wideSlot(cellIndex int64, incoming Direction) int64 {
	scaled := cellIndex * int64(wideRouteIncomingSlots)
	return scaled + int64(incoming)
}

func wideCellIndex(slot int64) int64 {
	return slot / int64(wideRouteIncomingSlots)
}

func spanFits(opening doorOpening, width uint32, occupancy *placementOccupancy) bool {
	owner, occupied := occupancy.ownerAt(opening.at)
	if !occupied {
		return false
	}
	for _, cell := range spanCells(opening, width) {
		cellOwner, cellOccupied := occupancy.ownerAt(cell)
		if !cellOccupied || cellOwner != owner {
			return false
		}
		outside := stepCell(cell, opening.direction)
		if _, inside := occupancy.index(outside); !inside {
			return false
		}
		outOwner, outOccupied := occupancy.ownerAt(outside)
		if outOccupied && outOwner == owner {
			return false
		}
	}
	return true
}

func spansFace(from, to doorOpening, width uint32, occupancy *placementOccupancy) bool {
	toOwner, toOccupied := occupancy.ownerAt(to.at)
	if !toOccupied {
		return false
	}
	toCells := spanCells(to, width)
	for _, cell := range spanCells(from, width) {
		outside := stepCell(cell, from.direction)
		if !cellListContains(toCells, outside) {
			return false
		}
		owner, occupied := occupancy.ownerAt(outside)
		if !occupied || owner != toOwner {
			return false
		}
	}
	return true
}

func spanCells(opening doorOpening, width uint32) []Cell {
	low, high := widthExtents(width)
	cells := make([]Cell, 0, width)
	alongX := opening.direction == DirectionNorth || opening.direction == DirectionSouth
	for offset := -low; offset <= high; offset++ {
		cell := opening.at
		if alongX {
			cell.X += offset
		} else {
			cell.Y += offset
		}
		cells = append(cells, cell)
	}
	return cells
}

func widthExtents(width uint32) (int32, int32) {
	low := int32((width - 1) / 2)
	high := int32(width / 2)
	return low, high
}

func stepCell(cell Cell, direction Direction) Cell {
	delta := direction.Delta()
	return Cell{X: cell.X + delta.X, Y: cell.Y + delta.Y}
}

func cellListContains(cells []Cell, target Cell) bool {
	for _, cell := range cells {
		if cell == target {
			return true
		}
	}
	return false
}

func getOrCreateSpannedDoor(doors *[]Door, opening doorOpening, width uint32, corridorID CorridorID) DoorID {
	cells := spanCells(opening, width)
	at := cells[0]
	for _, cell := range cells[1:] {
		if cell.Y < at.Y || (cell.Y == at.Y && cell.X < at.X) {
			at = cell
		}
	}
	for doorIndex := range *doors {
		door := &(*doors)[doorIndex]
		if door.RoomID == opening.roomID && door.At == at && door.Direction == opening.direction && door.Span == width {
			door.CorridorIDs = append(door.CorridorIDs, corridorID)
			return door.ID
		}
	}
	doorID := DoorID(len(*doors))
	*doors = append(*doors, Door{
		ID: doorID, RoomID: opening.roomID, At: at,
		Direction: opening.direction, Span: width, CorridorIDs: []CorridorID{corridorID},
	})
	return doorID
}

// occupyBand dilates the centerline into the occupied region. Odd W is
// symmetric, (W-1)/2 Cells each side. Even W: the extra Cell goes to the +X
// side for vertical travel and the +Y side for horizontal travel. At every
// bend the full W×W block is filled, centred by that same rule.
func occupyBand(centerline []Cell, fromDir, toDir Direction, width uint32) []Cell {
	if len(centerline) == 0 || width == 0 {
		return nil
	}
	low, high := widthExtents(width)
	stamped := make([]Cell, 0, len(centerline)*int(width))
	for index := range centerline {
		incoming, outgoing := centerlineDirections(centerline, index, fromDir, toDir)
		stamped = appendBandCell(stamped, centerline[index], incoming, outgoing, low, high)
	}
	return uniqueRowMajor(stamped)
}

func appendBandCell(stamped []Cell, cell Cell, incoming, outgoing Direction, low, high int32) []Cell {
	if incoming != outgoing {
		for dy := -low; dy <= high; dy++ {
			for dx := -low; dx <= high; dx++ {
				stamped = append(stamped, Cell{X: cell.X + dx, Y: cell.Y + dy})
			}
		}
		return stamped
	}
	if incoming == DirectionEast || incoming == DirectionWest {
		for dy := -low; dy <= high; dy++ {
			stamped = append(stamped, Cell{X: cell.X, Y: cell.Y + dy})
		}
		return stamped
	}
	for dx := -low; dx <= high; dx++ {
		stamped = append(stamped, Cell{X: cell.X + dx, Y: cell.Y})
	}
	return stamped
}

func centerlineDirections(cells []Cell, index int, fromDir, toDir Direction) (Direction, Direction) {
	var incoming Direction
	if index == 0 {
		incoming = fromDir
	} else {
		incoming = stepBetween(cells[index-1], cells[index])
	}
	var outgoing Direction
	if index == len(cells)-1 {
		outgoing = toDir.Opposite()
	} else {
		outgoing = stepBetween(cells[index], cells[index+1])
	}
	return incoming, outgoing
}

func stepBetween(from, to Cell) Direction {
	if to.X > from.X {
		return DirectionEast
	}
	if to.X < from.X {
		return DirectionWest
	}
	if to.Y > from.Y {
		return DirectionSouth
	}
	return DirectionNorth
}

func uniqueRowMajor(cells []Cell) []Cell {
	if len(cells) == 0 {
		return nil
	}
	sort.Slice(cells, func(first, second int) bool {
		if cells[first].Y != cells[second].Y {
			return cells[first].Y < cells[second].Y
		}
		return cells[first].X < cells[second].X
	})
	unique := make([]Cell, 0, len(cells))
	for _, cell := range cells {
		if len(unique) > 0 {
			previous := unique[len(unique)-1]
			if cell.X == previous.X && cell.Y == previous.Y {
				continue
			}
		}
		unique = append(unique, cell)
	}
	return unique
}
