package platform

import (
	"math"
)

// Shell reachability is the seating pass's model of "can the body get there".
// It is deliberately parallel to the invariant in invariants_test.go: seating
// decides with this copy, and the test does not call it. A bug that reported
// every cell reachable would otherwise hide a sealed opening from both sides.

func shellClearance(profile MovementProfile) int32 {
	n := int32(math.Ceil(profile.BodyHeight))
	if n < 1 {
		return 1
	}
	return n
}

func shellOpeningReached(room Room, transition Transition, playable map[Cell]bool, profile MovementProfile) bool {
	borders, mouths := shellOpeningCells(room, transition)
	if len(borders) == 0 || len(mouths) == 0 {
		return false
	}
	for _, border := range borders {
		kind, ok := room.Grid.At(border)
		if !ok || kind.Blocks() {
			return false
		}
	}
	n := shellClearance(profile)
	for _, mouth := range mouths {
		if !shellBodyFits(room.Grid, mouth.X, mouth.Y, n) {
			continue
		}
		if playable[mouth] {
			return true
		}
	}
	return false
}

func shellOpeningCells(room Room, transition Transition) (borders, mouths []Cell) {
	extent := int32(transition.Extent)
	if extent <= 0 {
		return nil, nil
	}
	switch transition.Side {
	case TransitionSideBottom:
		y := int32(room.Grid.Height) - 1
		for i := int32(0); i < extent; i++ {
			x := int32(transition.Offset) + i
			borders = append(borders, Cell{X: x, Y: y})
			mouths = append(mouths, Cell{X: x, Y: y - 1})
		}
	case TransitionSideTop:
		for i := int32(0); i < extent; i++ {
			x := int32(transition.Offset) + i
			borders = append(borders, Cell{X: x, Y: 0})
			mouths = append(mouths, Cell{X: x, Y: 1})
		}
	case TransitionSideLeft:
		y0 := int32(room.Grid.Height) - int32(transition.Offset) - extent
		for i := int32(0); i < extent; i++ {
			borders = append(borders, Cell{X: 0, Y: y0 + i})
			mouths = append(mouths, Cell{X: 1, Y: y0 + i})
		}
	case TransitionSideRight:
		y0 := int32(room.Grid.Height) - int32(transition.Offset) - extent
		x := int32(room.Grid.Width) - 1
		for i := int32(0); i < extent; i++ {
			borders = append(borders, Cell{X: x, Y: y0 + i})
			mouths = append(mouths, Cell{X: x - 1, Y: y0 + i})
		}
	}
	return borders, mouths
}

func shellPlayable(grid Grid, profile MovementProfile, abilities AbilitySet) map[Cell]bool {
	// Movement is directed: a body can fall from a high ledge onto the floor,
	// but the floor cannot climb that fall in reverse. Seeding the search from
	// every standable cell and keeping the largest component therefore marks a
	// high opening "reachable" when the search started on the ledge and fell,
	// even though a character who spawns on the floor can never get there with
	// the same moveset. Root the playable set at the lowest footing (largest
	// Y) and flood only along directed shellMoves from there.
	n := shellClearance(profile)
	roots := shellFloorRoots(grid, profile)
	if len(roots) == 0 {
		return map[Cell]bool{}
	}
	return shellFlood(grid, profile, abilities, n, roots)
}

func shellFloorRoots(grid Grid, profile MovementProfile) []Cell {
	width := int32(grid.Width)
	height := int32(grid.Height)
	floorY := int32(-1)
	var roots []Cell
	for y := int32(0); y < height; y++ {
		for x := int32(0); x < width; x++ {
			if !shellStandable(grid, x, y, profile) {
				continue
			}
			if y > floorY {
				floorY = y
				roots = roots[:0]
				roots = append(roots, Cell{X: x, Y: y})
				continue
			}
			if y == floorY {
				roots = append(roots, Cell{X: x, Y: y})
			}
		}
	}
	return roots
}

func shellFlood(grid Grid, profile MovementProfile, abilities AbilitySet, n int32, roots []Cell) map[Cell]bool {
	comp := map[Cell]bool{}
	queue := make([]Cell, 0, len(roots))
	for _, root := range roots {
		comp[root] = true
		queue = append(queue, root)
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range shellMoves(grid, cur, profile, abilities, n) {
			if comp[next] {
				continue
			}
			comp[next] = true
			queue = append(queue, next)
		}
	}
	return comp
}

func shellStandable(grid Grid, x, y int32, profile MovementProfile) bool {
	kind, ok := grid.At(Cell{X: x, Y: y})
	if !ok || (kind != CellKindEmpty && kind != CellKindClimbable) {
		return false
	}
	below, ok := grid.At(Cell{X: x, Y: y + 1})
	if !ok || (below != CellKindSolid && below != CellKindSemiSolid && below != CellKindClimbable) {
		return false
	}
	n := shellClearance(profile)
	for i := int32(1); i < n; i++ {
		above, ok := grid.At(Cell{X: x, Y: y - i})
		if !ok || above.Blocks() {
			return false
		}
	}
	return true
}

func shellBodyFits(grid Grid, x, y, n int32) bool {
	for i := int32(0); i < n; i++ {
		kind, ok := grid.At(Cell{X: x, Y: y - i})
		if !ok || kind.Blocks() {
			return false
		}
	}
	return true
}

func shellMoves(grid Grid, cur Cell, profile MovementProfile, abilities AbilitySet, n int32) []Cell {
	var out []Cell
	if shellStandable(grid, cur.X, cur.Y, profile) {
		for _, dx := range []int32{-1, 1} {
			if shellBodyFits(grid, cur.X+dx, cur.Y, n) {
				out = append(out, Cell{X: cur.X + dx, Y: cur.Y})
			}
		}
		out = append(out, shellJumps(grid, cur, profile, abilities, n)...)
		if abilities.Has(AbilityDash) && profile.Dash != nil {
			out = append(out, shellDash(grid, cur, profile, n)...)
		}
	}
	if shellBodyFits(grid, cur.X, cur.Y+1, n) {
		out = append(out, Cell{X: cur.X, Y: cur.Y + 1})
	}
	out = append(out, shellClimbMoves(grid, cur, profile, abilities, n)...)
	if abilities.Has(AbilityWallJump) && profile.WallJump != nil && shellBodyFits(grid, cur.X, cur.Y, n) {
		if shellBesideWall(grid, cur) {
			out = append(out, shellJumps(grid, cur, profile, abilities, n)...)
		}
	}
	return out
}

func shellClimbMoves(grid Grid, cur Cell, profile MovementProfile, abilities AbilitySet, n int32) []Cell {
	if !abilities.Has(AbilityClimb) || profile.Climb == nil {
		return nil
	}
	kind, ok := grid.At(cur)
	if !ok || kind != CellKindClimbable {
		return nil
	}
	var out []Cell
	for _, dy := range []int32{-1, 1} {
		next := Cell{X: cur.X, Y: cur.Y + dy}
		nk, nok := grid.At(next)
		if nok && nk == CellKindClimbable && shellBodyFits(grid, next.X, next.Y, n) {
			out = append(out, next)
		}
	}
	return out
}

func shellBesideWall(grid Grid, cur Cell) bool {
	for _, dx := range []int32{-1, 1} {
		kind, ok := grid.At(Cell{X: cur.X + dx, Y: cur.Y})
		if ok && kind.Blocks() {
			return true
		}
	}
	return false
}

func shellDash(grid Grid, cur Cell, profile MovementProfile, n int32) []Cell {
	distance := int32(math.Floor(profile.Dash.Speed * profile.Dash.Duration))
	var out []Cell
	for _, dx := range []int32{-1, 1} {
		for step := int32(1); step <= distance; step++ {
			x := cur.X + dx*step
			if !shellBodyFits(grid, x, cur.Y, n) {
				break
			}
			out = append(out, Cell{X: x, Y: cur.Y})
		}
	}
	return out
}

func shellJumps(grid Grid, cur Cell, profile MovementProfile, abilities AbilitySet, n int32) []Cell {
	maxPeak := profile.ApexHeight()
	if abilities.Has(AbilityDoubleJump) && profile.DoubleJump != nil {
		maxPeak += profile.ApexHeight()
	}
	peakCells := int32(math.Floor(maxPeak))
	var out []Cell
	for peak := int32(0); peak <= peakCells; peak++ {
		top := cur.Y - peak
		if !shellJumpAscentClear(grid, cur, top, n) {
			continue
		}
		left, right := shellJumpRoof(grid, cur.X, top, n)
		fallLimits := shellJumpFallLimits(grid, left, right, top, n)
		for dy := -peak; dy <= int32(grid.Height); dy++ {
			out = append(out, shellJumpTargetsCached(cur, profile, peak, dy, left, right, fallLimits)...)
		}
	}
	return out
}

func shellJumpTargetsCached(cur Cell, profile MovementProfile, peak, dy, left, right int32, fallLimits []int32) []Cell {
	reach := shellJumpReach(profile, float64(peak), float64(-dy))
	if reach < 0 {
		return nil
	}
	maxDx := int32(math.Floor(reach))
	var out []Cell
	for dx := max(-maxDx, left-cur.X); dx <= min(maxDx, right-cur.X); dx++ {
		if (dx == 0 && dy == 0) || math.Abs(float64(dx)) > reach {
			continue
		}
		tx, ty := cur.X+dx, cur.Y+dy
		if ty <= fallLimits[tx-left] {
			out = append(out, Cell{X: tx, Y: ty})
		}
	}
	return out
}

func shellJumpAscentClear(grid Grid, cur Cell, top, n int32) bool {
	for y := cur.Y; y >= top; y-- {
		if !shellBodyFits(grid, cur.X, y, n) {
			return false
		}
	}
	return true
}

func shellJumpRoof(grid Grid, x, top, n int32) (int32, int32) {
	left, right := x, x
	for left > 0 && shellBodyFits(grid, left-1, top, n) {
		left--
	}
	for right+1 < int32(grid.Width) && shellBodyFits(grid, right+1, top, n) {
		right++
	}
	return left, right
}

func shellJumpFallLimits(grid Grid, left, right, top, n int32) []int32 {
	limits := make([]int32, right-left+1)
	for x := left; x <= right; x++ {
		last := top
		for y := top + 1; y < int32(grid.Height) && shellBodyFits(grid, x, y, n); y++ {
			last = y
		}
		limits[x-left] = last
	}
	return limits
}

func shellJumpReach(profile MovementProfile, peak, rise float64) float64 {
	// Every product is rounded on its own. A fused multiply-subtract here would
	// make the reach depend on GOARCH, which the assembly guard forbids.
	apex := profile.ApexHeight()
	limit := float64(apex + apex)
	if rise > peak+1e-9 || peak > limit+1e-9 {
		return -1
	}
	gUp := profile.GravityUp
	gDown := profile.GravityDown
	if gUp <= 0 || gDown <= 0 {
		return -1
	}
	velocity := profile.JumpVelocity
	square := float64(velocity * velocity)
	twiceG := float64(2 * gUp)
	scaled := float64(twiceG * peak)
	disc := float64(square - scaled)
	if disc < 0 {
		return -1
	}
	root := math.Sqrt(disc)
	tUp := float64(float64(velocity-root) / gUp)
	fall := float64(peak - rise)
	tDown := 0.0
	if fall > 1e-9 {
		twoFall := float64(2 * fall)
		quot := float64(twoFall / gDown)
		tDown = math.Sqrt(quot)
	}
	return float64(profile.MaxRunSpeed * float64(tUp+tDown))
}
