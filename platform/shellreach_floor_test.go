package platform

import (
	"math"
	"reflect"
	"testing"
)

// The optimized jump enumeration must preserve the original arc test's cells
// and order. The order matters because the seating pass is deterministic.
func TestShellJumpsMatchesArcEnumeration(t *testing.T) {
	profile := DefaultProfile()
	for pattern := 0; pattern < 3; pattern++ {
		grid := shellJumpTestGrid(pattern)
		for _, cur := range []Cell{{X: 1, Y: 1}, {X: 5, Y: 4}, {X: 10, Y: 8}} {
			assertShellJumpMatchesReference(t, grid, cur, profile, pattern)
		}
	}
}

func shellJumpTestGrid(pattern int) Grid {
	grid := Grid{Width: 12, Height: 10, Cells: make([]CellKind, 120)}
	for y := int32(0); y < 10; y++ {
		for x := int32(0); x < 12; x++ {
			if x == 0 || x == 11 || y == 0 || y == 9 ||
				(pattern != 0 && (x*7+y*11+int32(pattern))%13 == 0) {
				grid.Cells[y*12+x] = CellKindSolid
			}
		}
	}
	return grid
}

func assertShellJumpMatchesReference(t *testing.T, grid Grid, cur Cell, profile MovementProfile, pattern int) {
	t.Helper()
	got := shellJumps(grid, cur, profile, 0, shellClearance(profile))
	var want []Cell
	for peak := int32(0); peak <= int32(math.Floor(profile.ApexHeight())); peak++ {
		for dy := -peak; dy <= int32(grid.Height); dy++ {
			want = append(want, referenceShellJumpTargets(grid, cur, profile, shellClearance(profile), peak, dy)...)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pattern %d from %v: optimized jump cells differ from arc enumeration: got %d, want %d", pattern, cur, len(got), len(want))
	}
}

func referenceShellJumpTargets(grid Grid, cur Cell, profile MovementProfile, n, peak, dy int32) []Cell {
	reach := shellJumpReach(profile, float64(peak), float64(-dy))
	if reach < 0 {
		return nil
	}
	maxDx := int32(math.Floor(reach))
	var out []Cell
	for dx := -maxDx; dx <= maxDx; dx++ {
		if (dx == 0 && dy == 0) || math.Abs(float64(dx)) > reach {
			continue
		}
		tx, ty := cur.X+dx, cur.Y+dy
		if referenceShellArcClear(grid, cur.X, cur.Y, tx, ty, cur.Y-peak, n) {
			out = append(out, Cell{X: tx, Y: ty})
		}
	}
	return out
}

func referenceShellArcClear(grid Grid, x, y, tx, ty, top, n int32) bool {
	if ty < top {
		top = ty
	}
	for yy := y; yy >= top; yy-- {
		if !shellBodyFits(grid, x, yy, n) {
			return false
		}
	}
	step := int32(1)
	if tx < x {
		step = -1
	}
	for xx := x; xx != tx; xx += step {
		if !shellBodyFits(grid, xx, top, n) {
			return false
		}
	}
	dir := int32(1)
	if ty < top {
		dir = -1
	}
	for yy := top; ; yy += dir {
		if !shellBodyFits(grid, tx, yy, n) {
			return false
		}
		if yy == ty {
			break
		}
	}
	return true
}

// A tall empty room with a top opening must not claim the mouth is reachable
// from the floor under base moveset: falling from the mouth is not climbing.
func TestShellPlayableDoesNotCountFallAsClimb(t *testing.T) {
	profile := DefaultProfile()
	width, height := uint32(12), uint32(20)
	cells := make([]CellKind, width*height)
	for i := range cells {
		cells[i] = CellKindEmpty
	}
	// Border solid.
	for x := uint32(0); x < width; x++ {
		cells[x] = CellKindSolid
		cells[(height-1)*width+x] = CellKindSolid
	}
	for y := uint32(0); y < height; y++ {
		cells[y*width] = CellKindSolid
		cells[y*width+width-1] = CellKindSolid
	}
	// Floor footing under the interior.
	for x := uint32(1); x < width-1; x++ {
		cells[(height-2)*width+x] = CellKindSolid // support
		cells[(height-3)*width+x] = CellKindEmpty // stand
	}
	room := Room{
		ID:   0,
		Grid: Grid{Width: width, Height: height, Cells: cells},
		Transitions: []Transition{{
			ID: 1, Room: 0, Side: TransitionSideTop, Offset: 4, Extent: 3, To: 2,
		}},
	}
	// Carve the top opening border open.
	for i := uint32(0); i < 3; i++ {
		setCell(&room.Grid, int32(4+i), 0, CellKindEmpty)
	}
	if oneMouthReaches(room, room.Transitions[0], profile, 0) {
		t.Fatal("top opening must not be base-reachable from the floor by falling-from-mouth logic")
	}
	playable := shellPlayable(room.Grid, profile, 0)
	_, mouths := shellOpeningCells(room, room.Transitions[0])
	for _, m := range mouths {
		if playable[m] {
			t.Fatalf("mouth %v is in floor-rooted playable; fall is not climb", m)
		}
	}
	// Floor stands must still be playable.
	floor := Cell{X: 5, Y: int32(height) - 3}
	if !playable[floor] {
		t.Fatalf("floor stand %v missing from playable", floor)
	}
}

func TestGrowApproachReachesTopOpeningFromFloor(t *testing.T) {
	profile := DefaultProfile()
	width, height := uint32(12), uint32(16)
	cells := make([]CellKind, width*height)
	for i := range cells {
		cells[i] = CellKindEmpty
	}
	for x := uint32(0); x < width; x++ {
		cells[x] = CellKindSolid
		cells[(height-1)*width+x] = CellKindSolid
	}
	for y := uint32(0); y < height; y++ {
		cells[y*width] = CellKindSolid
		cells[y*width+width-1] = CellKindSolid
	}
	for x := uint32(1); x < width-1; x++ {
		cells[(height-2)*width+x] = CellKindSolid
	}
	room := Room{
		ID:   0,
		Grid: Grid{Width: width, Height: height, Cells: cells},
		Transitions: []Transition{{
			ID: 1, Room: 0, Side: TransitionSideTop, Offset: 4, Extent: 3, To: 2,
		}},
	}
	for i := uint32(0); i < 3; i++ {
		setCell(&room.Grid, int32(4+i), 0, CellKindEmpty)
	}
	if oneMouthReaches(room, room.Transitions[0], profile, 0) {
		t.Fatal("precondition: mouth starts unreachable")
	}
	if !growApproach(&room, room.Transitions[0], profile, 0) {
		t.Fatal("growApproach should build a climbable path from the floor to the top opening")
	}
	if !oneMouthReaches(room, room.Transitions[0], profile, 0) {
		t.Fatal("mouth still unreachable after growApproach")
	}
}
