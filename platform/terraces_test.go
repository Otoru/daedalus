package platform

import (
	"context"
	"reflect"
	"testing"
)

func TestTerracedSynthesis(t *testing.T) {
	for _, seed := range []Seed{9, 7, 11} {
		config := terracedTestConfig(seed)
		layout, err := Generate(context.Background(), NewM1Oracle(), config)
		if err != nil {
			t.Fatal(err)
		}
		if layout.Judgement.Verdict != VerdictCertified {
			t.Fatalf("seed %d: %s: %s", seed, layout.Judgement.Verdict, layout.Judgement.Detail)
		}
		terraces, support, interior := terracedRoomMetrics(t, seed, layout, config.Profile)
		placed, dropped := 0, 0
		for _, room := range layout.Rooms {
			placed += len(room.Placements)
			dropped += room.Dropped
		}
		crossings, alternations := terracedWallChains(&layout.JumpGraph)
		t.Logf("seed=%d rooms=%d terraceCells=%d support=%.1f%% placed=%d dropped=%d wallCrossings=%d alternations=%d", seed, len(layout.Rooms), terraces, 100*float64(support)/float64(interior), placed, dropped, crossings, alternations)
		if crossings < 8 {
			t.Fatalf("seed %d has no repeated wall-to-wall traversal", seed)
		}
		if alternations == 0 {
			t.Fatalf("seed %d has no alternating wall-jump chain", seed)
		}
		if float64(support)/float64(interior) < 0.08 {
			t.Fatal("interior remains sparse")
		}
	}
}

func terracedTestConfig(seed Seed) Config {
	config := synthConfig(seed)
	config.Progression.Steps = []ProgressionStep{{Name: "Dash", Grants: NewAbilitySet(AbilityDash)}, {Name: "Wall", Grants: NewAbilitySet(AbilityWallJump)}, {Name: "Double", Grants: NewAbilitySet(AbilityDoubleJump)}}
	config.Beats.Definitions = append(config.Beats.Definitions, BeatDefinition{Kind: BeatKindShaft, Difficulty: 180, Requires: NewAbilitySet(AbilityWallJump)})
	config.Beats.Spine.Beats = append(config.Beats.Spine.Beats, BeatWeight{Kind: BeatKindShaft, Weight: 3})
	config.Beats.Spine.MinRunBeats, config.Beats.Spine.MaxRunBeats = 4, 6
	for i := range config.Beats.Spine.Beats {
		config.Beats.Spine.Beats[i].Weight = 2
	}
	config.Beats.Spine.Beats[len(config.Beats.Spine.Beats)-1].Weight = 3
	return config
}

func terracedRoomMetrics(t *testing.T, seed Seed, layout PlatformLayout, profile MovementProfile) (terraces, support, interior int) {
	t.Helper()
	for _, room := range layout.Plane.Rooms {
		playable := shellPlayable(room.Grid, profile, layout.Rooms[room.ID].Abilities)
		count, reached, roomSupport, roomInterior := terracedOneRoomMetrics(room.Grid, playable)
		support += roomSupport
		interior += roomInterior
		if count == 0 {
			t.Errorf("seed %d room %d has no terraces", seed, room.ID)
		}
		if reached == 0 {
			t.Errorf("seed %d room %d terraces are unreachable", seed, room.ID)
		}
		terraces += count
	}
	return terraces, support, interior
}

func terracedOneRoomMetrics(grid Grid, playable map[Cell]bool) (count, reached, support, interior int) {
	for y := int32(1); y < int32(grid.Height)-1; y++ {
		for x := int32(1); x < int32(grid.Width)-1; x++ {
			kind, _ := grid.At(Cell{X: x, Y: y})
			interior++
			if kind.Supports() {
				support++
			}
			if terracedCell(grid, x, y) {
				count++
				if playable[Cell{X: x, Y: y - 1}] {
					reached++
				}
			}
		}
	}
	return count, reached, support, interior
}

func terracedCell(grid Grid, x, y int32) bool {
	kind, _ := grid.At(Cell{X: x, Y: y})
	below, _ := grid.At(Cell{X: x, Y: y + 1})
	above, _ := grid.At(Cell{X: x, Y: y - 1})
	return kind == CellKindSolid && below == CellKindSolid && above == CellKindEmpty
}

func terracedWallChains(graph *JumpGraph) (crossings, alternations int) {
	outgoing := make(map[MotionNodeID][]MotionEdge)
	for _, edge := range graph.Edges {
		if edge.Kind == MotionEdgeKindWallJump {
			outgoing[edge.From] = append(outgoing[edge.From], edge)
		}
	}
	for _, edge := range graph.Edges {
		if edge.Kind != MotionEdgeKindWallJump {
			continue
		}
		from, to := graph.Nodes[edge.From], graph.Nodes[edge.To]
		if !terracedWallCrossing(from, to) {
			continue
		}
		crossings++
		for _, next := range outgoing[edge.To] {
			landing := graph.Nodes[next.To]
			if landing.Mode == MotionModeWallCling && landing.Height > to.Height &&
				landing.Footing.Lo-from.Footing.Lo < 1 && from.Footing.Lo-landing.Footing.Lo < 1 {
				alternations++
				break
			}
		}
	}
	return crossings, alternations
}

func terracedWallCrossing(from, to MotionNode) bool {
	return from.Mode == MotionModeWallCling && to.Mode == MotionModeWallCling && to.Height > from.Height &&
		(to.Footing.Lo-from.Footing.Lo > 3 || from.Footing.Lo-to.Footing.Lo > 3)
}

func TestCompactRunSharesLandingInsteadOfDroppingNextBeat(t *testing.T) {
	config := synthConfig(9)
	config.Beats.Spine = &BeatDistribution{Beats: []BeatWeight{{Kind: BeatKindRest, Weight: 1}}, MinRunBeats: 4, MaxRunBeats: 4}
	rhythm, err := GenerateRhythm(context.Background(), &FakeOracle{}, config, 0)
	if err != nil {
		t.Fatal(err)
	}
	run := mainPathBeats(rhythm)[:2]
	first, second := rhythm.Beats[run[0]].Grid, rhythm.Beats[run[1]].Grid
	width := first.Width + second.Width - min(departureSpan(first.Width), departureSpan(second.Width)) + 2
	room := Room{Grid: Grid{Width: width, Height: 40}}
	placements, fits, err := layoutRun(room, rhythm, run, 0, 0)
	if err != nil || !fits || len(placements) != 2 {
		t.Fatalf("compact run dropped a beat: fits=%v err=%v", fits, err)
	}
	if placements[1].Origin.X >= placements[0].Origin.X+int32(first.Width) {
		t.Fatal("landing not shared")
	}
}

func TestTerracesPreserveExistingGeometryAndAreDeterministic(t *testing.T) {
	grid := Grid{Width: 32, Height: 24, Cells: make([]CellKind, 32*24)}
	for i := range grid.Cells {
		grid.Cells[i] = CellKindEmpty
	}
	for x := int32(0); x < 32; x++ {
		setCell(&grid, x, 23, CellKindSolid)
	}
	setCell(&grid, 8, 14, CellKindHazard)
	setCell(&grid, 12, 11, CellKindClimbable)
	before := append([]CellKind(nil), grid.Cells...)
	room := Room{ID: 3, Grid: grid}
	other := Room{ID: 3, Grid: Grid{Width: 32, Height: 24, Cells: append([]CellKind(nil), before...)}}
	addTerraces(&room, DefaultProfile())
	addTerraces(&other, DefaultProfile())
	if !reflect.DeepEqual(room, other) {
		t.Fatal("terraces are not deterministic")
	}
	added := 0
	for i, kind := range before {
		if kind != CellKindEmpty && room.Grid.Cells[i] != kind {
			t.Fatalf("overwrote geometry at %d", i)
		}
		if kind == CellKindEmpty && room.Grid.Cells[i] == CellKindSolid {
			added++
		}
	}
	if added == 0 {
		t.Fatal("no terraces added")
	}
}

func TestWallChimneyCreatesOpposingFaces(t *testing.T) {
	grid := Grid{Width: 32, Height: 26, Cells: make([]CellKind, 32*26)}
	for x := int32(0); x < 32; x++ {
		setCell(&grid, x, 25, CellKindSolid)
	}
	for y := int32(0); y < 26; y++ {
		setCell(&grid, 0, y, CellKindSolid)
		setCell(&grid, 31, y, CellKindSolid)
	}
	room := Room{Grid: grid}
	if !addWallChimney(&room, DefaultProfile()) {
		t.Fatal("empty tall room has no wall-jump chimney")
	}
	left, right := chimneyWallsAt(room.Grid, 11)
	if left < 0 || right-left < 4 || right-left > 7 {
		t.Fatalf("expected opposed walls around a traversable shaft, got %d and %d", left, right)
	}
	if at, kind, blocked := chimneyBlockedCell(room.Grid, left, right); blocked {
		t.Fatalf("shaft blocked at %d,%d: %v", at.X, at.Y, kind)
	}
	result, err := NewM1Oracle().BuildGraph(context.Background(), GraphQuery{
		Grid: room.Grid, Profile: DefaultProfile(), Abilities: NewAbilitySet(AbilityWallJump),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Judgement.Certified() {
		t.Fatalf("chimney graph: %s (%s)", result.Judgement.Verdict, result.Judgement.Detail)
	}
	upwardCrossings := chimneyCrossings(result.Graph)
	if upwardCrossings < 2 {
		t.Fatalf("expected repeated upward wall-to-wall jumps, got %d", upwardCrossings)
	}
}

func chimneyWallsAt(grid Grid, y int32) (left, right int32) {
	left, right = -1, -1
	for x := int32(1); x < int32(grid.Width)-1; x++ {
		kind, _ := grid.At(Cell{X: x, Y: y})
		if kind != CellKindSolid {
			continue
		}
		if left < 0 {
			left = x
		} else {
			return left, x
		}
	}
	return left, right
}

func chimneyBlockedCell(grid Grid, left, right int32) (Cell, CellKind, bool) {
	for x := left + 1; x < right; x++ {
		for y := int32(11); y < 19; y++ {
			at := Cell{X: x, Y: y}
			kind, _ := grid.At(at)
			if kind != CellKindEmpty {
				return at, kind, true
			}
		}
	}
	return Cell{}, CellKindEmpty, false
}

func chimneyCrossings(graph JumpGraph) int {
	count := 0
	for _, edge := range graph.Edges {
		if edge.Kind != MotionEdgeKindWallJump {
			continue
		}
		if terracedWallCrossing(graph.Nodes[edge.From], graph.Nodes[edge.To]) {
			count++
		}
	}
	return count
}

func TestOutcropKeepsHeadroomAboveExistingPlatform(t *testing.T) {
	grid := Grid{Width: 24, Height: 20, Cells: make([]CellKind, 24*20)}
	for x := int32(3); x < 12; x++ {
		setCell(&grid, x, 12, CellKindSolid)
	}
	if landformSpace(grid, 3, 9, 9, 2, shellClearance(DefaultProfile())) {
		t.Fatal("outcrop would remove the standing clearance of an existing platform")
	}
}

func TestLadderMotifHasCertifiedClimb(t *testing.T) {
	grid := Grid{Width: 32, Height: 26, Cells: make([]CellKind, 32*26)}
	for x := int32(0); x < 32; x++ {
		setCell(&grid, x, 25, CellKindSolid)
	}
	for y := int32(0); y < 26; y++ {
		setCell(&grid, 0, y, CellKindSolid)
		setCell(&grid, 31, y, CellKindSolid)
	}
	room := Room{Grid: grid}
	if !addLadderMotif(&room, DefaultProfile()) {
		t.Fatal("empty tall room has no ladder")
	}
	// A climb is level geometry only when it actually reaches a landing.
	landings := ladderLandingCount(room.Grid)
	if landings == 0 {
		t.Fatal("ladder ends without a landing in the upper half of the room")
	}
	result, err := NewM1Oracle().BuildGraph(context.Background(), GraphQuery{
		Grid: room.Grid, Profile: DefaultProfile(), Abilities: NewAbilitySet(AbilityClimb),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Judgement.Certified() {
		t.Fatalf("ladder graph: %s (%s)", result.Judgement.Verdict, result.Judgement.Detail)
	}
	up, down := climbDirections(result.Graph)
	if up < 2 || down < 2 {
		t.Fatalf("ladder needs a usable two-way climb, got %d up and %d down", up, down)
	}
}

func ladderLandingCount(grid Grid) int {
	count := 0
	for y := int32(3); y < int32(grid.Height)/2; y++ {
		for x := int32(2); x < int32(grid.Width)-2; x++ {
			kind, _ := grid.At(Cell{X: x, Y: y})
			left, _ := grid.At(Cell{X: x - 1, Y: y})
			right, _ := grid.At(Cell{X: x + 1, Y: y})
			if kind == CellKindClimbable && (left == CellKindSolid || right == CellKindSolid) {
				count++
			}
		}
	}
	return count
}

func climbDirections(graph JumpGraph) (up, down int) {
	for _, edge := range graph.Edges {
		if edge.Kind != MotionEdgeKindClimb {
			continue
		}
		from, to := graph.Nodes[edge.From], graph.Nodes[edge.To]
		if to.Height > from.Height {
			up++
		} else if to.Height < from.Height {
			down++
		}
	}
	return up, down
}

func TestGeneratedClimbRoomsHaveUsableRopes(t *testing.T) {
	config := synthConfig(4)
	config.Progression.Base = NewAbilitySet(AbilityClimb)
	layout, err := Generate(context.Background(), NewM1Oracle(), config)
	if err != nil {
		t.Fatal(err)
	}
	if !layout.Judgement.Certified() {
		t.Fatalf("generated climb map: %s (%s)", layout.Judgement.Verdict, layout.Judgement.Detail)
	}
	climbs := 0
	ladders := 0
	for _, room := range layout.Plane.Rooms {
		t.Logf("room %d ladder longest=%d", room.ID, longestClimbRun(room.Grid))
		for _, kind := range room.Grid.Cells {
			if kind == CellKindClimbable {
				ladders++
			}
		}
	}
	for _, edge := range layout.JumpGraph.Edges {
		if edge.Kind == MotionEdgeKindClimb {
			climbs++
		}
	}
	if climbs == 0 {
		t.Fatalf("generated ladders have no usable climb edges (cells=%d, abilities=%s, rooms=%d)", ladders, layout.JumpGraph.Abilities, len(layout.Rooms))
	}
}

func longestClimbRun(grid Grid) int {
	longest := 0
	for x := int32(0); x < int32(grid.Width); x++ {
		run := 0
		for y := int32(0); y < int32(grid.Height); y++ {
			kind, _ := grid.At(Cell{X: x, Y: y})
			if kind == CellKindClimbable {
				run++
				if run > longest {
					longest = run
				}
			} else {
				run = 0
			}
		}
	}
	return longest
}

func TestGeneratedClimbRoomsHaveUpperGallery(t *testing.T) {
	config := synthConfig(9)
	config.Progression.Base = NewAbilitySet(AbilityClimb)
	layout, err := Generate(context.Background(), NewM1Oracle(), config)
	if err != nil {
		t.Fatal(err)
	}
	if !layout.Judgement.Certified() {
		t.Fatalf("map not certified: %s", layout.Judgement.Detail)
	}
	roomsWithGallery := 0
	for _, room := range layout.Plane.Rooms {
		playable := shellPlayable(room.Grid, config.Profile, layout.Rooms[room.ID].Abilities)
		if hasUpperGallery(room.Grid, playable) {
			roomsWithGallery++
		}
	}
	if roomsWithGallery < len(layout.Plane.Rooms)/2 {
		t.Fatalf("only %d/%d rooms have a readable upper gallery", roomsWithGallery, len(layout.Plane.Rooms))
	}
}

func hasUpperGallery(grid Grid, playable map[Cell]bool) bool {
	for y := int32(3); y < int32(grid.Height)/3; y++ {
		if upperGalleryRow(grid, playable, y) {
			return true
		}
	}
	return false
}

func upperGalleryRow(grid Grid, playable map[Cell]bool, y int32) bool {
	run := 0
	reachable := false
	for x := int32(1); x < int32(grid.Width)-1; x++ {
		kind, _ := grid.At(Cell{X: x, Y: y})
		above, _ := grid.At(Cell{X: x, Y: y - 1})
		if kind == CellKindSolid && above == CellKindEmpty {
			run++
			reachable = reachable || playable[Cell{X: x, Y: y - 1}]
			if run >= 7 && reachable {
				return true
			}
		} else {
			run = 0
			reachable = false
		}
	}
	return false
}
