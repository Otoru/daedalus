package daedalus

// openingSeparation is the Chebyshev distance at which two doorway bands are
// allowed to sit. Distance 1 is contact, diagonal included, and is forbidden
// everywhere, including beside a Room. Distance 2 is the first legal gap.
const openingSeparation int32 = 2

// doorwayWindowLimit is the largest conflict span, measured in candidate
// steps along the boundary, that the circular packing DP will carry. A wider
// span falls back to the exact search, which stays on the small candidate
// counts that produce a wide span.
const doorwayWindowLimit = 10

// boundarySlot is one footprint Cell and the cardinal direction that steps
// out of the footprint. Slots are traced clockwise, interior on the right.
type boundarySlot struct {
	cell Cell
	out  Direction
}

// doorway is one opening of the budget width. span is the footprint Cells it
// claims, in trace order; outside is the band's first step out of the Room.
type doorway struct {
	out     Direction
	span    []Cell
	outside []Cell
}

// narrowestDeclaredCorridorWidth is the width the Connector budgets for.
// Degradation walks the declared list downward and can always reach this
// width, so a Room that can host it is reachable. Budgeting the widest
// declared width refuses a circle, and a cross smaller than 6: those
// perimeters have no straight run of three Cells facing one direction, so
// their capacity at width 3 is zero even though a one-Cell corridor fits.
// A nil geometry has no declared widths and budgets one Cell, the same
// width the router uses when it never draws.
func narrowestDeclaredCorridorWidth(widths []CorridorWidthWeight) uint32 {
	var narrowest uint32
	for _, candidate := range widths {
		if candidate.Width == 0 {
			continue
		}
		if narrowest == 0 || candidate.Width < narrowest {
			narrowest = candidate.Width
		}
	}
	if narrowest == 0 {
		return 1
	}
	return narrowest
}

// placedRoomFootprint is the Room's occupied Cells. A Connector test may
// omit Cells and still name a rectangle by Origin, Width, and Height; that
// rectangle is the footprint, in row-major order. A Room with neither is empty.
func placedRoomFootprint(room PlacedRoom) []Cell {
	if len(room.Cells) > 0 {
		return room.Cells
	}
	if room.Width == 0 || room.Height == 0 {
		return nil
	}
	count := int(room.Width) * int(room.Height)
	cells := make([]Cell, 0, count)
	for y := uint32(0); y < room.Height; y++ {
		for x := uint32(0); x < room.Width; x++ {
			cells = append(cells, Cell{
				X: room.Origin.X + int32(x),
				Y: room.Origin.Y + int32(y),
			})
		}
	}
	return cells
}

// roomOpeningCapacity is how many openings of corridorWidth fit on the
// footprint at once. Each opening claims that many contiguous boundary Cells
// on one straight side. Between two openings on that side there are at least
// two unused boundary Cells. Openings on adjacent sides also conflict when
// any of their outside Cells are within Chebyshev distance 1, so a corner is
// not free real estate. A zero width budgets one Cell. A zero Grid dimension
// means the footprint is not clipped by a border, which is a Connector call
// that did not name a Grid.
func roomOpeningCapacity(cells []Cell, gridWidth, gridHeight, corridorWidth uint32) int {
	if corridorWidth == 0 {
		corridorWidth = 1
	}
	if len(cells) == 0 || corridorWidth > maximumCorridorWidth {
		return 0
	}
	slots := traceBoundarySlots(cells)
	if len(slots) == 0 {
		return 0
	}
	occupied := footprintSet(cells)
	openings := doorwayOpenings(slots, corridorWidth, gridWidth, gridHeight, occupied)
	return maximumCompatibleOpenings(openings)
}

func footprintSet(cells []Cell) map[Cell]struct{} {
	occupied := make(map[Cell]struct{}, len(cells))
	for _, cell := range cells {
		occupied[cell] = struct{}{}
	}
	return occupied
}

func traceBoundarySlots(cells []Cell) []boundarySlot {
	occupied := footprintSet(cells)
	start := cells[0]
	for _, cell := range cells[1:] {
		if cell.Y < start.Y || (cell.Y == start.Y && cell.X < start.X) {
			start = cell
		}
	}
	has := func(cell Cell) bool {
		_, ok := occupied[cell]
		return ok
	}
	current := start
	out := DirectionNorth
	limit := len(cells)*4 + 1
	slots := make([]boundarySlot, 0, len(cells)*2)
	for len(slots) < limit {
		slots = append(slots, boundarySlot{cell: current, out: out})
		nextCell, nextOut := nextBoundarySlot(current, out, has)
		if nextCell == start && nextOut == DirectionNorth {
			break
		}
		current = nextCell
		out = nextOut
	}
	return slots
}

// nextBoundarySlot steps clockwise around the footprint. The forward Cell is
// the next Cell along the wall; the diagonal Cell is one step further
// outward. The four occupancy pairs are the straight run, the convex corner,
// the concave corner, and a diagonal-only step.
func nextBoundarySlot(cell Cell, out Direction, has func(Cell) bool) (Cell, Direction) {
	forward := turnRight(out)
	outDelta := out.Delta()
	forwardDelta := forward.Delta()
	ahead := Cell{X: cell.X + forwardDelta.X, Y: cell.Y + forwardDelta.Y}
	diag := Cell{X: ahead.X + outDelta.X, Y: ahead.Y + outDelta.Y}
	switch {
	case has(ahead) && !has(diag):
		return ahead, out
	case !has(ahead) && !has(diag):
		return cell, forward
	case has(ahead) && has(diag):
		return diag, forward.Opposite()
	default:
		return diag, out.Opposite()
	}
}

func turnRight(direction Direction) Direction {
	switch direction {
	case DirectionNorth:
		return DirectionEast
	case DirectionEast:
		return DirectionSouth
	case DirectionSouth:
		return DirectionWest
	default:
		return DirectionNorth
	}
}

func doorwayOpenings(
	slots []boundarySlot,
	width uint32,
	gridWidth uint32,
	gridHeight uint32,
	occupied map[Cell]struct{},
) []doorway {
	span := int(width)
	if span > len(slots) {
		return nil
	}
	openings := make([]doorway, 0)
	for start := 0; start+span <= len(slots); start++ {
		direction := slots[start].out
		spanCells := make([]Cell, 0, span)
		outside := make([]Cell, 0, span)
		fits := true
		for offset := 0; offset < span; offset++ {
			slot := slots[start+offset]
			if slot.out != direction {
				fits = false
				break
			}
			if offset > 0 && !boundarySlotsAdjacent(slots[start+offset-1], slot) {
				fits = false
				break
			}
			step := direction.Delta()
			cell := Cell{X: slot.cell.X + step.X, Y: slot.cell.Y + step.Y}
			if _, inside := occupied[cell]; inside || !openingCellInGrid(cell, gridWidth, gridHeight) {
				fits = false
				break
			}
			spanCells = append(spanCells, slot.cell)
			outside = append(outside, cell)
		}
		if fits {
			openings = append(openings, doorway{out: direction, span: spanCells, outside: outside})
		}
	}
	return openings
}

func boundarySlotsAdjacent(first, second boundarySlot) bool {
	dx := first.cell.X - second.cell.X
	if dx < 0 {
		dx = -dx
	}
	dy := first.cell.Y - second.cell.Y
	if dy < 0 {
		dy = -dy
	}
	return dx+dy == 1
}

func openingCellInGrid(cell Cell, width, height uint32) bool {
	if width == 0 || height == 0 {
		return true
	}
	if cell.X < 0 || cell.Y < 0 {
		return false
	}
	return uint32(cell.X) < width && uint32(cell.Y) < height
}

func doorwaysConflict(first, second doorway) bool {
	if sameWallSlackTooSmall(first, second) {
		return true
	}
	for _, left := range first.outside {
		for _, right := range second.outside {
			if chebyshevDistance(left, right) < openingSeparation {
				return true
			}
		}
	}
	return false
}

// openingSlackCells is the unused boundary between two openings on one wall.
// Two Cells are required so the bands stay at Chebyshev distance 2 after the
// width of each opening is taken into account at a corner as well as mid-wall.
const openingSlackCells int32 = 2

func sameWallSlackTooSmall(first, second doorway) bool {
	if first.out != second.out || len(first.span) == 0 || len(second.span) == 0 {
		return false
	}
	if first.out == DirectionNorth || first.out == DirectionSouth {
		if first.span[0].Y != second.span[0].Y {
			return false
		}
		return spanGapTooSmall(first.span, second.span, true)
	}
	if first.span[0].X != second.span[0].X {
		return false
	}
	return spanGapTooSmall(first.span, second.span, false)
}

func spanGapTooSmall(first, second []Cell, horizontal bool) bool {
	firstLow, firstHigh := spanAxis(first, horizontal)
	secondLow, secondHigh := spanAxis(second, horizontal)
	return axisGap(firstLow, firstHigh, secondLow, secondHigh) < openingSlackCells
}

func spanAxis(cells []Cell, horizontal bool) (int32, int32) {
	low := cells[0].X
	high := cells[0].X
	if !horizontal {
		low = cells[0].Y
		high = cells[0].Y
	}
	for _, cell := range cells[1:] {
		value := cell.X
		if !horizontal {
			value = cell.Y
		}
		if value < low {
			low = value
		}
		if value > high {
			high = value
		}
	}
	return low, high
}

func axisGap(firstLow, firstHigh, secondLow, secondHigh int32) int32 {
	leftHigh := firstHigh
	rightLow := secondLow
	if secondLow < firstLow {
		leftHigh = secondHigh
		rightLow = firstLow
	}
	gap := rightLow - leftHigh - 1
	if gap < 0 {
		return 0
	}
	return gap
}

// openingConflictWordBits is the width of the packed conflict word. A wider
// candidate list keeps the unpacked search; v1 shapes at the default sizes
// stay inside one word (a 9×9 mask has at most 36 openings).
const openingConflictWordBits = 64

func maximumCompatibleOpenings(openings []doorway) int {
	count := len(openings)
	if count == 0 {
		return 0
	}
	if count > openingConflictWordBits {
		return maximumCompatibleOpeningsWide(openings)
	}
	conflictBits := make([]uint64, count)
	window := 0
	for left := 0; left < count; left++ {
		for right := left + 1; right < count; right++ {
			if !doorwaysConflict(openings[left], openings[right]) {
				continue
			}
			conflictBits[left] |= 1 << uint(right)
			conflictBits[right] |= 1 << uint(left)
			span := right - left
			wrap := count - span
			if wrap < span {
				span = wrap
			}
			if span > window {
				window = span
			}
		}
	}
	if window == 0 {
		return count
	}
	if window <= doorwayWindowLimit && count >= window*2 {
		return circularDoorwayMIS(count, window, conflictBits)
	}
	return exactDoorwayMIS(count, func(left, right int) bool {
		return conflictBits[left]&(1<<uint(right)) != 0
	})
}

func maximumCompatibleOpeningsWide(openings []doorway) int {
	count := len(openings)
	conflict := func(left, right int) bool {
		return doorwaysConflict(openings[left], openings[right])
	}
	window := 0
	for left := 0; left < count; left++ {
		for right := left + 1; right < count; right++ {
			if !conflict(left, right) {
				continue
			}
			span := right - left
			wrap := count - span
			if wrap < span {
				span = wrap
			}
			if span > window {
				window = span
			}
		}
	}
	if window == 0 {
		return count
	}
	if window <= doorwayWindowLimit && count >= window*2 {
		return circularDoorwayMISWide(count, window, conflict)
	}
	return exactDoorwayMIS(count, conflict)
}

// circularDoorwayMIS is the maximum independent set on a circular conflict
// graph whose edges reach at most window steps along the boundary. The
// geometric doorway test is resolved once into conflictBits; every later
// transition is a shift of that word. Re-testing Cells inside the state loop
// is what made Typical spend most of its time here.
func circularDoorwayMIS(count, window int, conflictBits []uint64) int {
	states := 1 << window
	maskLimit := states - 1
	shiftConflict := make([]int, count)
	for pos := 0; pos < count; pos++ {
		bits := 0
		for shift := 0; shift < window; shift++ {
			previous := pos - 1 - shift
			if previous < 0 {
				break
			}
			if conflictBits[pos]&(uint64(1)<<uint(previous)) != 0 {
				bits |= 1 << uint(shift)
			}
		}
		shiftConflict[pos] = bits
	}

	scoreA := make([]int, states)
	scoreB := make([]int, states)
	// stamp records the generation that last wrote each mask of the buffer
	// currently being filled. Generations only increase, so a stale write from
	// the other buffer cannot be mistaken for this step.
	stamp := make([]int, states)
	liveA := make([]int, 0, states)
	liveB := make([]int, 0, states)
	generation := 0
	best := 0

	for prefix := 0; prefix < states; prefix++ {
		if !prefixConsistentBits(prefix, window, conflictBits) {
			continue
		}
		start := encodeRecentMask(prefix, window)
		scoreA[start] = 0
		liveA = append(liveA[:0], start)

		curScore, nextScore := scoreA, scoreB
		curLive, nextLive := liveA, liveB
		for pos := window; pos < count; pos++ {
			generation++
			nextLive = nextLive[:0]
			for _, mask := range curLive {
				base := curScore[mask]
				skipped := (mask << 1) & maskLimit
				if stamp[skipped] != generation {
					stamp[skipped] = generation
					nextScore[skipped] = base
					nextLive = append(nextLive, skipped)
				} else if base > nextScore[skipped] {
					nextScore[skipped] = base
				}
				if mask&shiftConflict[pos] == 0 {
					taken := skipped | 1
					value := base + 1
					if stamp[taken] != generation {
						stamp[taken] = generation
						nextScore[taken] = value
						nextLive = append(nextLive, taken)
					} else if value > nextScore[taken] {
						nextScore[taken] = value
					}
				}
			}
			curScore, nextScore = nextScore, curScore
			curLive, nextLive = nextLive, curLive
		}

		prefixCount := bitCount(prefix)
		for _, mask := range curLive {
			if !suffixAgreesBits(mask, prefix, count, window, conflictBits) {
				continue
			}
			total := curScore[mask] + prefixCount
			if total > best {
				best = total
			}
		}
	}
	return best
}

func prefixConsistentBits(prefix, window int, conflictBits []uint64) bool {
	windowMask := (uint64(1) << uint(window)) - 1
	for left := 0; left < window; left++ {
		if prefix&(1<<uint(left)) == 0 {
			continue
		}
		if uint64(prefix)&conflictBits[left]&windowMask != 0 {
			return false
		}
	}
	return true
}

func suffixAgreesBits(mask, prefix, count, window int, conflictBits []uint64) bool {
	prefixBits := uint64(prefix)
	for shift := 0; shift < window; shift++ {
		if mask&(1<<uint(shift)) == 0 {
			continue
		}
		pos := count - 1 - shift
		if prefixBits&conflictBits[pos] != 0 {
			return false
		}
	}
	return true
}

func circularDoorwayMISWide(count, window int, conflict func(int, int) bool) int {
	states := 1 << window
	best := 0
	current := make([]int, states)
	next := make([]int, states)
	for prefix := 0; prefix < states; prefix++ {
		if !prefixConsistent(prefix, window, conflict) {
			continue
		}
		for index := range current {
			current[index] = -1
		}
		current[encodeRecentMask(prefix, window)] = 0
		for pos := window; pos < count; pos++ {
			for index := range next {
				next[index] = -1
			}
			maskLimit := states - 1
			for mask := 0; mask < states; mask++ {
				base := current[mask]
				if base < 0 {
					continue
				}
				skipped := (mask << 1) & maskLimit
				if base > next[skipped] {
					next[skipped] = base
				}
				if doorwayCanTake(mask, pos, window, conflict) {
					taken := skipped | 1
					value := base + 1
					if value > next[taken] {
						next[taken] = value
					}
				}
			}
			current, next = next, current
		}
		prefixCount := bitCount(prefix)
		for mask := 0; mask < states; mask++ {
			if current[mask] < 0 {
				continue
			}
			if !suffixAgrees(mask, prefix, count, window, conflict) {
				continue
			}
			total := current[mask] + prefixCount
			if total > best {
				best = total
			}
		}
	}
	return best
}

func prefixConsistent(prefix, window int, conflict func(int, int) bool) bool {
	for left := 0; left < window; left++ {
		if prefix&(1<<left) == 0 {
			continue
		}
		for right := left + 1; right < window; right++ {
			if prefix&(1<<right) == 0 {
				continue
			}
			if conflict(left, right) {
				return false
			}
		}
	}
	return true
}

func encodeRecentMask(prefix, window int) int {
	mask := 0
	for pos := 0; pos < window; pos++ {
		if prefix&(1<<pos) == 0 {
			continue
		}
		bit := window - 1 - pos
		mask |= 1 << bit
	}
	return mask
}

func doorwayCanTake(mask, pos, window int, conflict func(int, int) bool) bool {
	for shift := 0; shift < window; shift++ {
		if mask&(1<<shift) == 0 {
			continue
		}
		previous := pos - 1 - shift
		if previous >= 0 && conflict(previous, pos) {
			return false
		}
	}
	return true
}

func suffixAgrees(mask, prefix, count, window int, conflict func(int, int) bool) bool {
	for shift := 0; shift < window; shift++ {
		if mask&(1<<shift) == 0 {
			continue
		}
		pos := count - 1 - shift
		for earlier := 0; earlier < window; earlier++ {
			if prefix&(1<<earlier) == 0 {
				continue
			}
			if pos != earlier && conflict(earlier, pos) {
				return false
			}
		}
	}
	return true
}

func bitCount(value int) int {
	count := 0
	for value != 0 {
		count += value & 1
		value >>= 1
	}
	return count
}

func exactDoorwayMIS(count int, conflict func(int, int) bool) int {
	conflicts := make([][]int, count)
	for left := 0; left < count; left++ {
		for right := left + 1; right < count; right++ {
			if !conflict(left, right) {
				continue
			}
			conflicts[left] = append(conflicts[left], right)
			conflicts[right] = append(conflicts[right], left)
		}
	}
	best := 0
	used := make([]bool, count)
	var search func(index, taken int)
	search = func(index, taken int) {
		if taken+(count-index) <= best {
			return
		}
		if index == count {
			best = taken
			return
		}
		search(index+1, taken)
		for _, other := range conflicts[index] {
			if other < index && used[other] {
				return
			}
		}
		used[index] = true
		search(index+1, taken+1)
		used[index] = false
	}
	search(0, 0)
	return best
}
