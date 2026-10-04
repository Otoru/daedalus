package platform

import "math"

// addUpperGallery spends the otherwise unused top third on a broad wall
// outcrop. Keep it shallow enough to leave standing clearance below and let
// the motion oracle decide whether the resulting room remains traversable.
func addUpperGallery(room *Room, profile MovementProfile) bool {
	w, h := int32(room.Grid.Width), int32(room.Grid.Height)
	if w < 22 || h < 20 {
		return false
	}
	span := min(int32(12), w/3)
	for y := max(int32(4), shellClearance(profile)+2); y < h/3; y++ {
		for offset := int32(0); offset <= w-span-2; offset++ {
			x := int32(1) + offset
			if room.ID%2 != 0 {
				x = w - 1 - span - offset
			}
			if !landformSpace(room.Grid, x, y, span, 2, shellClearance(profile)) {
				continue
			}
			paintGallery(&room.Grid, x, y, span)
			return true
		}
	}
	return false
}

func paintGallery(grid *Grid, x, y, span int32) {
	for cx := x; cx < x+span; cx++ {
		setCell(grid, cx, y, CellKindSolid)
		if cx > x && cx < x+span-1 {
			setCell(grid, cx, y+1, CellKindSolid)
		}
	}
}

// addWallChimney places a climbable-by-wall-jump pocket above an ordinary
// foothold. Its five-cell air channel has opposing solid faces; short ledges
// elsewhere in the room cannot create that alternating manoeuvre. The motif
// is optional and never overwrites authored beats or border openings.
func addWallChimney(room *Room, profile MovementProfile) bool {
	w, h := int32(room.Grid.Width), int32(room.Grid.Height)
	if w < 18 || h < 22 || profile.WallJump == nil {
		return false
	}
	top, wallBottom, ledge := h-15, h-8, h-4
	for _, left := range []int32{3, w - 10} {
		right := left + 6
		if !chimneySpace(room.Grid, left, right, top, ledge) {
			continue
		}
		for y := top; y <= wallBottom; y++ {
			setCell(&room.Grid, left, y, CellKindSolid)
			setCell(&room.Grid, right, y, CellKindSolid)
		}
		for x := left; x <= right; x++ {
			setCell(&room.Grid, x, ledge, CellKindSolid)
		}
		return true
	}
	return false
}

func chimneySpace(grid Grid, left, right, top, ledge int32) bool {
	w := int32(grid.Width)
	for y := top - 2; y <= ledge; y++ {
		for x := left - 1; x <= right+1; x++ {
			if grid.Cells[y*w+x] != CellKindEmpty {
				return false
			}
		}
	}
	return true
}

// addLadderMotif couples a floor-to-gallery rope with a substantial landing.
// A short rope ending below an unrelated shelf is graph-climbable but visually
// and spatially useless as a route through the room.
func addLadderMotif(room *Room, profile MovementProfile) bool {
	w, h := int32(room.Grid.Width), int32(room.Grid.Height)
	if w < 18 || h < 18 || profile.Climb == nil {
		return false
	}
	for _, landingY := range []int32{h/2 - 1, h/2 + 1, h/2 + 3} {
		for offset := int32(0); offset < w-4; offset++ {
			x := w/2 + offset/2
			if offset%2 == 1 {
				x = w/2 - (offset+1)/2
			}
			for _, side := range []int32{1, -1} {
				if tryLadderMotif(room, profile, x, landingY, side) {
					return true
				}
			}
		}
	}
	return false
}

func tryLadderMotif(room *Room, profile MovementProfile, x, landingY, side int32) bool {
	w, h := int32(room.Grid.Width), int32(room.Grid.Height)
	deckX := x + 1
	if side < 0 {
		deckX = x - 8
	}
	if x < 2 || x >= w-2 || !landformSpace(room.Grid, deckX, landingY, 8, 2, shellClearance(profile)) {
		return false
	}
	if room.Grid.Cells[(h-1)*w+x] != CellKindSolid {
		return false
	}
	for y := landingY - 1; y < h-1; y++ {
		if room.Grid.Cells[y*w+x] != CellKindEmpty {
			return false
		}
	}
	paintGallery(&room.Grid, deckX, landingY, 8)
	for y := landingY - 1; y < h-1; y++ {
		setCell(&room.Grid, x, y, CellKindClimbable)
	}
	return true
}

// addTerraces builds relief around the authored run: grounded foundations,
// broad wall outcrops and a few shaped stepping stones. Seating and the motion
// oracle validate the resulting geometry afterwards.
func addTerraces(room *Room, profile MovementProfile) {
	clearance := shellClearance(profile)
	rise := int32(math.Floor(profile.ApexHeight())) - 1
	if rise <= clearance || rise < 2 {
		return
	}
	before := append([]CellKind(nil), room.Grid.Cells...)
	groundTerraces(room, before, clearance, rise)
	shapeTerraces(room, clearance, rise)
}

func groundTerraces(room *Room, before []CellKind, clearance, rise int32) {
	w, h := int32(room.Grid.Width), int32(room.Grid.Height)
	// Ground nearby parts of the authored run without burying other geometry.
	for x := int32(2); x < w-2; x++ {
		for y := max(clearance+2, h-rise-3); y < h-2; y++ {
			if before[y*w+x] != CellKindSolid || before[(y-1)*w+x] != CellKindEmpty {
				continue
			}
			if groundColumnClear(before, w, h, x, y) {
				for cy := y + 1; cy < h-1; cy++ {
					setCell(&room.Grid, x, cy, CellKindSolid)
				}
			}
			break
		}
	}
}

func groundColumnClear(before []CellKind, w, h, x, y int32) bool {
	if before[(h-1)*w+x] != CellKindSolid {
		return false
	}
	for cy := y + 1; cy < h-1; cy++ {
		if before[cy*w+x] != CellKindEmpty {
			return false
		}
	}
	return true
}

func shapeTerraces(room *Room, clearance, rise int32) {
	w, h := int32(room.Grid.Width), int32(room.Grid.Height)
	// Alternate broad outcrops, leaving an open central traversal channel.
	for level, y := int32(0), h-2-rise*2; y > clearance+2; level, y = level+1, y-rise*2 {
		left := (level+int32(room.ID))%2 == 0
		span := min(w/3, 7+(int32(room.ID)+level*3)%6)
		depth := int32(2) + (int32(room.ID)+level)%3
		x := int32(1)
		if !left {
			x = w - 1 - span
		}
		paintOutcrop(room, x, y, span, depth, clearance, left)
		// The stepping stone rises alongside an overhang and has a shaped body.
		bridgeX := x + span + 2
		if !left {
			bridgeX = x - 7
		}
		bridgeY := y + rise
		paintSteppingStone(room, bridgeX, bridgeY, clearance)
	}
}

func paintOutcrop(room *Room, x, y, span, depth, clearance int32, left bool) {
	if !landformSpace(room.Grid, x, y, span, depth, clearance) {
		return
	}
	for row := int32(0); row < depth; row++ {
		lo, hi := x, x+span-row
		if !left {
			lo, hi = x+row, x+span
		}
		for cx := lo; cx < hi; cx++ {
			setCell(&room.Grid, cx, y+row, CellKindSolid)
		}
	}
}

func paintSteppingStone(room *Room, x, y, clearance int32) {
	if !landformSpace(room.Grid, x, y, 5, 2, clearance) {
		return
	}
	for cx := x; cx < x+5; cx++ {
		setCell(&room.Grid, cx, y, CellKindSolid)
	}
	for cx := x + 1; cx < x+4; cx++ {
		setCell(&room.Grid, cx, y+1, CellKindSolid)
	}
}

func landformSpace(grid Grid, x, y, span, depth, clearance int32) bool {
	w, h := int32(grid.Width), int32(grid.Height)
	if span < 3 || x < 1 || x+span > w-1 || y-clearance < 1 || y+depth >= h-1 {
		return false
	}
	// A new ceiling can invalidate a standing node below it just as easily
	// as it can hit one above. Preserve body-height air on both sides.
	for cy := y - clearance; cy <= min(h-2, y+depth+clearance); cy++ {
		for cx := x; cx < x+span; cx++ {
			if grid.Cells[cy*w+cx] != CellKindEmpty {
				return false
			}
		}
	}
	return true
}
