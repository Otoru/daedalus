package platform

import (
	"fmt"
	"math"
	"sort"
)

// seatShell is the pass that runs after the rhythm has stamped beat geometry
// into the rooms. The macro front punches openings and drops spawn and goal
// on the empty shell; the stamp then writes the interior and can wall a mouth
// shut or land an anchor inside a solid. Both failures are that one order.
// This pass re-decides both against the stamped grids, under the profile.
//
// An opening stays where it is when both mouths are already reachable. Otherwise
// it slides along the shared wall to the first span both rooms can reach, and
// if no span works it grows jumpable footings from the playable component to
// the mouth that is already paired. Spawn and goal stay on their cells when
// those cells are standing positions inside the room's playable component;
// otherwise they move to the canonical standing cell of that component (Y then
// X).
//
// When a room has no standing cell, or a mouth still cannot be reached after
// the footing is grown, the result is a rejection, not a panic and not an
// invented cell. The anchor stays in its room: spawn and goal are the roots of
// the progression graph, and moving them to another room would make that graph
// a lie of the same kind as a sealed opening. Generate retries a rejected draw.
func seatShell(plane *Plane, profile MovementProfile, stages []AbilitySet) (VerdictReason, string, bool) {
	openReason, openDetail, openRejected := reseatOpenings(plane, profile, stages)
	anchorReason, anchorDetail, anchorRejected := reseatAnchors(plane, profile, stages)
	if openRejected {
		return openReason, openDetail, true
	}
	if anchorRejected {
		return anchorReason, anchorDetail, true
	}
	return 0, "", false
}

func reseatOpenings(plane *Plane, profile MovementProfile, stages []AbilitySet) (VerdictReason, string, bool) {
	index := map[TransitionID]struct{ room, at int }{}
	for ri := range plane.Rooms {
		for ti := range plane.Rooms[ri].Transitions {
			index[plane.Rooms[ri].Transitions[ti].ID] = struct{ room, at int }{ri, ti}
		}
	}
	seen := map[TransitionID]bool{}
	var ordered []TransitionID
	for ri := range plane.Rooms {
		for _, t := range plane.Rooms[ri].Transitions {
			if t.Side == TransitionSideDoor || seen[t.ID] {
				continue
			}
			partner, ok := index[t.To]
			if !ok {
				return ReasonDisconnected, fmt.Sprintf("opening %d in room %d has no partner", t.ID, t.Room), true
			}
			seen[t.ID] = true
			seen[plane.Rooms[partner.room].Transitions[partner.at].ID] = true
			id := t.ID
			if plane.Rooms[partner.room].Transitions[partner.at].ID < id {
				id = plane.Rooms[partner.room].Transitions[partner.at].ID
			}
			ordered = append(ordered, id)
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	for _, id := range ordered {
		loc := index[id]
		t := plane.Rooms[loc.room].Transitions[loc.at]
		partnerLoc := index[t.To]
		if detail, rejected := slideOpening(plane, loc, partnerLoc, profile, stages); rejected {
			return ReasonDisconnected, detail, true
		}
	}
	// A later pair's footing can sit on an earlier mouth's approach. Rebuild
	// any mouth that the rest of the pass walled off, and stop if a pass
	// stops changing the count. Eight passes is the room diameter in practice;
	// a pair that still oscillates is rejected rather than certified.
	var last int
	for pass := 0; pass < 8; pass++ {
		var broken []TransitionID
		for _, id := range ordered {
			loc := index[id]
			t := plane.Rooms[loc.room].Transitions[loc.at]
			partnerLoc := index[t.To]
			if mouthsReach(plane, t, plane.Rooms[partnerLoc.room].Transitions[partnerLoc.at], profile, stages) {
				continue
			}
			broken = append(broken, id)
		}
		if len(broken) == 0 {
			assignIndices(plane.Rooms)
			return 0, "", false
		}
		if pass > 0 && len(broken) >= last {
			loc := index[broken[0]]
			t := plane.Rooms[loc.room].Transitions[loc.at]
			return ReasonDisconnected, openingSealed(t.Room, t), true
		}
		last = len(broken)
		for _, id := range broken {
			loc := index[id]
			t := plane.Rooms[loc.room].Transitions[loc.at]
			partnerLoc := index[t.To]
			growApproaches(plane, loc, partnerLoc, profile, stages)
		}
	}
	loc := index[ordered[0]]
	t := plane.Rooms[loc.room].Transitions[loc.at]
	return ReasonDisconnected, openingSealed(t.Room, t), true
}

func slideOpening(plane *Plane, loc, partner struct{ room, at int }, profile MovementProfile, stages []AbilitySet) (string, bool) {
	roomA := &plane.Rooms[loc.room]
	roomB := &plane.Rooms[partner.room]
	tA := roomA.Transitions[loc.at]
	tB := roomB.Transitions[partner.at]
	if mouthsReach(plane, tA, tB, profile, stages) {
		return "", false
	}
	span, ok := openingSpan(*roomA, tA)
	if !ok {
		return fmt.Sprintf("room %d opening %d side %s offset %d extent %d is not reachable from the playable area",
			roomA.ID, tA.ID, tA.Side, tA.Offset, tA.Extent), true
	}
	savedA := append([]CellKind(nil), roomA.Grid.Cells...)
	savedB := append([]CellKind(nil), roomB.Grid.Cells...)
	offA, offB := tA.Offset, tB.Offset
	restore := func() {
		copy(roomA.Grid.Cells, savedA)
		copy(roomB.Grid.Cells, savedB)
		roomA.Transitions[loc.at].Offset = offA
		roomB.Transitions[partner.at].Offset = offB
	}
	lo, hi, ok := sharedAxis(*roomA, *roomB, tA.Side)
	if !ok {
		restore()
		return openingSealed(roomA.ID, tA), true
	}
	// The first pass takes a mouth the stamp already left open. The second cuts
	// a footing at each candidate: the reachable doorway may sit somewhere
	// other than the span the macro punched, and only after that footing exists.
	for _, grow := range []bool{false, true} {
		for start := lo; start+span.extent <= hi; start++ {
			candidate := axisSpan{start: start, extent: span.extent}
			nextA, okA := offsetFromSpan(*roomA, tA.Side, candidate)
			nextB, okB := offsetFromSpan(*roomB, tB.Side, candidate)
			if !okA || !okB {
				continue
			}
			if openingHitsCorner(*roomA, tA.Side, nextA, tA.Extent) || openingHitsCorner(*roomB, tB.Side, nextB, tB.Extent) {
				continue
			}
			if crowdsSibling(*roomA, tA.ID, tA.Side, nextA, tA.Extent) || crowdsSibling(*roomB, tB.ID, tB.Side, nextB, tB.Extent) {
				continue
			}
			restore()
			sealOpening(roomA, roomA.Transitions[loc.at])
			sealOpening(roomB, roomB.Transitions[partner.at])
			roomA.Transitions[loc.at].Offset = nextA
			roomB.Transitions[partner.at].Offset = nextB
			punchOpening(roomA, roomA.Transitions[loc.at])
			punchOpening(roomB, roomB.Transitions[partner.at])
			if grow && !growApproaches(plane, loc, partner, profile, stages) {
				continue
			}
			if mouthsReach(plane, roomA.Transitions[loc.at], roomB.Transitions[partner.at], profile, stages) {
				return "", false
			}
		}
		restore()
	}
	return openingSealed(roomA.ID, roomA.Transitions[loc.at]) + "; " + openingSealed(roomB.ID, roomB.Transitions[partner.at]), true
}

// growApproaches cuts a jumpable approach from each room's playable component
// to its mouth. Sliding only moves the opening along the wall; a mouth that
// stays above the jump, or behind a stamped solid, is still sealed. Each step
// is a real footing: empty body column, solid support, no further apart than
// the profile's apex. Border cells are left alone.
func growApproaches(plane *Plane, loc, partner struct{ room, at int }, profile MovementProfile, stages []AbilitySet) bool {
	okA := growApproach(&plane.Rooms[loc.room], plane.Rooms[loc.room].Transitions[loc.at], profile, stageOf(stages, plane.Rooms[loc.room].ID))
	okB := growApproach(&plane.Rooms[partner.room], plane.Rooms[partner.room].Transitions[partner.at], profile, stageOf(stages, plane.Rooms[partner.room].ID))
	return okA && okB
}

func growApproach(room *Room, t Transition, profile MovementProfile, abilities AbilitySet) bool {
	if oneMouthReaches(*room, t, profile, abilities) {
		return true
	}
	limit := int(room.Grid.Width + room.Grid.Height + 2)
	for i := 0; i < limit; i++ {
		if oneMouthReaches(*room, t, profile, abilities) {
			return true
		}
		mouths := mouthCells(*room, t)
		if len(mouths) == 0 {
			return false
		}
		for _, mouth := range mouths {
			clearColumn(room, mouth.X, mouth.Y, profile)
		}
		if oneMouthReaches(*room, t, profile, abilities) {
			return true
		}
		playable := shellPlayable(room.Grid, profile, abilities)
		origin, ok := nearestStand(room.Grid, profile, playable, mouths)
		if !ok {
			return false
		}
		mouth := closestCell(mouths, origin)
		if !placeStep(room, origin, mouth, profile) {
			return false
		}
	}
	return oneMouthReaches(*room, t, profile, abilities)
}

func mouthCells(room Room, t Transition) []Cell {
	_, mouths := shellOpeningCells(room, t)
	return mouths
}

func clearColumn(room *Room, x, y int32, profile MovementProfile) {
	n := shellClearance(profile)
	for i := int32(0); i < n; i++ {
		cy := y - i
		if !interiorCell(room, x, cy) {
			continue
		}
		kind, ok := room.Grid.At(Cell{X: x, Y: cy})
		if ok && kind.Blocks() {
			setCell(&room.Grid, x, cy, CellKindEmpty)
		}
	}
}

func interiorCell(room *Room, x, y int32) bool {
	return x >= 1 && y >= 1 && x+1 < int32(room.Grid.Width) && y+1 < int32(room.Grid.Height)
}

func nearestStand(grid Grid, profile MovementProfile, playable map[Cell]bool, mouths []Cell) (Cell, bool) {
	var best Cell
	found := false
	var bestDist int32
	for y := int32(0); y < int32(grid.Height); y++ {
		for x := int32(0); x < int32(grid.Width); x++ {
			if !playable[Cell{X: x, Y: y}] || !shellStandable(grid, x, y, profile) {
				continue
			}
			dist := int32(1 << 30)
			for _, mouth := range mouths {
				d := absInt32(x-mouth.X) + absInt32(y-mouth.Y)
				if d < dist {
					dist = d
				}
			}
			if !found || dist < bestDist || (dist == bestDist && (y > best.Y || (y == best.Y && x < best.X))) {
				best, bestDist, found = Cell{X: x, Y: y}, dist, true
			}
		}
	}
	return best, found
}

func closestCell(mouths []Cell, origin Cell) Cell {
	best := mouths[0]
	bestDist := absInt32(best.X-origin.X) + absInt32(best.Y-origin.Y)
	for _, mouth := range mouths[1:] {
		dist := absInt32(mouth.X-origin.X) + absInt32(mouth.Y-origin.Y)
		if dist < bestDist {
			best, bestDist = mouth, dist
		}
	}
	return best
}

func placeStep(room *Room, origin, mouth Cell, profile MovementProfile) bool {
	rise := int32(math.Floor(profile.ApexHeight()))
	if rise < 1 {
		rise = 1
	}
	dx := signInt32(mouth.X - origin.X)
	dy := signInt32(mouth.Y - origin.Y)
	nx, ny := origin.X, origin.Y
	if absInt32(mouth.Y-origin.Y) >= absInt32(mouth.X-origin.X) && dy != 0 {
		step := rise
		if absInt32(mouth.Y-origin.Y) < step {
			step = absInt32(mouth.Y - origin.Y)
		}
		ny = origin.Y + dy*step
	} else if dx != 0 {
		nx = origin.X + dx
	} else {
		return false
	}
	if nx == mouth.X && ny == mouth.Y {
		// The mouth is the opening, not a footing. Step aside onto a neighbour.
		if mouth.X+1 < int32(room.Grid.Width)-1 {
			nx = mouth.X + 1
		} else if mouth.X-1 > 0 {
			nx = mouth.X - 1
		}
		ny = mouth.Y
	}
	if !clearJumpColumn(room, origin, Cell{X: nx, Y: ny}, profile) {
		return false
	}
	return makeStand(room, nx, ny, profile)
}

func clearJumpColumn(room *Room, from, to Cell, profile MovementProfile) bool {
	n := shellClearance(profile)
	x0, x1 := from.X, to.X
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	y0, y1 := from.Y, to.Y
	if y1 < y0 {
		y0, y1 = y1, y0
	}
	y0 -= n - 1
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			if !interiorCell(room, x, y) {
				continue
			}
			kind, ok := room.Grid.At(Cell{X: x, Y: y})
			if ok && kind.Blocks() {
				setCell(&room.Grid, x, y, CellKindEmpty)
			}
		}
	}
	return true
}

func makeStand(room *Room, x, y int32, profile MovementProfile) bool {
	if !interiorCell(room, x, y) {
		return false
	}
	clearColumn(room, x, y, profile)
	setCell(&room.Grid, x, y, CellKindEmpty)
	supportY := y + 1
	if interiorCell(room, x, supportY) {
		setCell(&room.Grid, x, supportY, CellKindSolid)
		return true
	}
	kind, ok := room.Grid.At(Cell{X: x, Y: supportY})
	return ok && (kind == CellKindSolid || kind == CellKindSemiSolid || kind == CellKindClimbable)
}

func absInt32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func signInt32(v int32) int32 {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	default:
		return 0
	}
}

func openingSealed(room RoomID, t Transition) string {
	return fmt.Sprintf("room %d opening %d side %s offset %d extent %d is not reachable from the playable area",
		room, t.ID, t.Side, t.Offset, t.Extent)
}

func mouthsReach(plane *Plane, a, b Transition, profile MovementProfile, stages []AbilitySet) bool {
	return oneMouthReaches(plane.Rooms[a.Room], a, profile, stageOf(stages, a.Room)) &&
		oneMouthReaches(plane.Rooms[b.Room], b, profile, stageOf(stages, b.Room))
}

func oneMouthReaches(room Room, t Transition, profile MovementProfile, abilities AbilitySet) bool {
	playable := shellPlayable(room.Grid, profile, abilities)
	return shellOpeningReached(room, t, playable, profile)
}

func stageOf(stages []AbilitySet, room RoomID) AbilitySet {
	if int(room) < 0 || int(room) >= len(stages) {
		return 0
	}
	return stages[room]
}

func sharedAxis(a, b Room, side TransitionSide) (lo, hi int32, ok bool) {
	if side.IsVertical() {
		lo = max(a.Origin.Y, b.Origin.Y)
		hi = min(a.Origin.Y+int32(a.Grid.Height), b.Origin.Y+int32(b.Grid.Height))
	} else {
		lo = max(a.Origin.X, b.Origin.X)
		hi = min(a.Origin.X+int32(a.Grid.Width), b.Origin.X+int32(b.Grid.Width))
	}
	return lo, hi, hi > lo
}

func openingHitsCorner(room Room, side TransitionSide, offset, extent uint32) bool {
	switch side {
	case TransitionSideTop, TransitionSideBottom:
		if offset == 0 || offset+extent >= room.Grid.Width {
			return true
		}
	case TransitionSideLeft, TransitionSideRight:
		if offset == 0 || offset+extent >= room.Grid.Height {
			return true
		}
	}
	return false
}

// crowdsSibling reports whether a candidate opening shares its side with
// another opening and the solid run between them is narrower than a doorway.
// Overlap is the extreme: the two air spans are one hole. A tooth thinner
// than OpeningExtent is the same hole with a chip in the middle.
func crowdsSibling(room Room, self TransitionID, side TransitionSide, offset, extent uint32) bool {
	end := offset + extent
	for _, t := range room.Transitions {
		if t.ID == self || t.Side != side {
			continue
		}
		tEnd := t.Offset + t.Extent
		var gap int
		switch {
		case offset >= tEnd:
			gap = int(offset - tEnd)
		case t.Offset >= end:
			gap = int(t.Offset - end)
		default:
			return true
		}
		if gap < OpeningExtent {
			return true
		}
	}
	return false
}

func sealOpening(room *Room, t Transition) {
	borders, _ := shellOpeningCells(*room, t)
	for _, cell := range borders {
		setCell(&room.Grid, cell.X, cell.Y, CellKindSolid)
	}
}

func reseatAnchors(plane *Plane, profile MovementProfile, stages []AbilitySet) (VerdictReason, string, bool) {
	for _, spec := range []struct {
		name   string
		anchor *Anchor
	}{
		{"spawn", &plane.Spawn},
		{"goal", &plane.Goal},
	} {
		if int(spec.anchor.Room) >= len(plane.Rooms) {
			return ReasonLandingUnsupported, fmt.Sprintf("%s names room %d, which is not in the plane", spec.name, spec.anchor.Room), true
		}
		room := &plane.Rooms[spec.anchor.Room]
		abilities := stageOf(stages, spec.anchor.Room)
		playable := shellPlayable(room.Grid, profile, abilities)
		if shellStandable(room.Grid, spec.anchor.At.X, spec.anchor.At.Y, profile) && playable[spec.anchor.At] {
			continue
		}
		cell, ok := canonicalStand(room.Grid, profile, playable)
		if !ok {
			return ReasonLandingUnsupported, fmt.Sprintf("%s room %d has no standing cell under the profile (body height %g)", spec.name, spec.anchor.Room, profile.BodyHeight), true
		}
		spec.anchor.At = cell
	}
	return 0, "", false
}

// canonicalStand is the first standing cell of the playable component in
// canonical cell order, Y then X. There is no second room: the caller rejects
// when this returns false.
func canonicalStand(grid Grid, profile MovementProfile, playable map[Cell]bool) (Cell, bool) {
	for y := int32(0); y < int32(grid.Height); y++ {
		for x := int32(0); x < int32(grid.Width); x++ {
			cell := Cell{X: x, Y: y}
			if playable[cell] && shellStandable(grid, x, y, profile) {
				return cell, true
			}
		}
	}
	return Cell{}, false
}
