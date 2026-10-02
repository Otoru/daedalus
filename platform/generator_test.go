package platform

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Otoru/daedalus/core"
)

// synthConfig is the request the synthesis tests draw from. The vocabulary is
// deliberately small and the run short, so that a failure points at one beat
// rather than at a crowd of them.
func synthConfig(seed Seed) Config {
	return Config{
		Seed:    seed,
		Width:   70,
		Height:  44,
		Profile: DefaultProfile(),
		Beats: BeatConfig{
			Definitions: []BeatDefinition{
				{Kind: BeatKindRest, Difficulty: 0},
				{Kind: BeatKindTraverse, Difficulty: 40},
				{Kind: BeatKindGap, Difficulty: 120},
				{Kind: BeatKindClimb, Difficulty: 160},
				{Kind: BeatKindDescend, Difficulty: 90},
			},
			Spine: &BeatDistribution{
				Beats: []BeatWeight{
					{Kind: BeatKindRest, Weight: 2},
					{Kind: BeatKindTraverse, Weight: 3},
					{Kind: BeatKindGap, Weight: 3},
					{Kind: BeatKindClimb, Weight: 2},
					{Kind: BeatKindDescend, Weight: 2},
				},
				MinRunBeats: 3,
				MaxRunBeats: 5,
			},
		},
	}
}

// narrowLedgeConfig is a request the oracle has a real reason to refuse. The
// character is wide — a half width of 1.1 cells — and the vocabulary includes
// a precision beat whose platforms are two cells across. A two-cell ledge
// under a full-support policy leaves footing [x+1.15, x+0.85], which is
// empty: the platform exists and cannot be stood on. Whether a draw puts one
// of those at the end of a run is the draw's business, which is exactly what
// makes it a retry case rather than a configuration error.
func narrowLedgeConfig(seed Seed) Config {
	cfg := synthConfig(seed)
	cfg.Width, cfg.Height = 90, 60
	cfg.MaxRooms = 2
	cfg.Profile.BodyHalfWidth = 1.1
	cfg.Beats.Definitions = []BeatDefinition{
		{Kind: BeatKindRest, MinCells: 4, MaxCells: 5, Difficulty: 0},
		{Kind: BeatKindTraverse, MinCells: 4, MaxCells: 5, Difficulty: 40},
		{Kind: BeatKindPrecision, MinCells: 2, MaxCells: 2, Difficulty: 200},
		{Kind: BeatKindGap, MinCells: 4, MaxCells: 5, Difficulty: 120},
		{Kind: BeatKindClimb, MinCells: 4, MaxCells: 5, Difficulty: 160},
	}
	cfg.Beats.Spine = &BeatDistribution{
		Beats: []BeatWeight{
			{Kind: BeatKindRest, Weight: 1},
			{Kind: BeatKindTraverse, Weight: 2},
			{Kind: BeatKindPrecision, Weight: 1},
			{Kind: BeatKindGap, Weight: 2},
			{Kind: BeatKindClimb, Weight: 2},
		},
		MinRunBeats: 3,
		MaxRunBeats: 4,
	}
	return cfg
}

// TestSameConfigAndSeedProduceTheSameLayout is the determinism contract: the
// same question asked twice gives the same map, compared as bytes rather than
// as a Go value, so that a float that merely prints the same is not mistaken
// for a float that is the same.
func TestSameConfigAndSeedProduceTheSameLayout(t *testing.T) {
	config := synthConfig(11)

	first, err := Generate(context.Background(), NewM1Oracle(), config)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	second, err := Generate(context.Background(), NewM1Oracle(), config)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}

	left, right := first.Canonical(), second.Canonical()
	if !bytes.Equal(left, right) {
		t.Fatalf("the same config and seed produced different layouts: %d bytes against %d", len(left), len(right))
	}
	if len(left) == 0 {
		t.Fatal("the canonical encoding is empty, so the comparison above proved nothing")
	}
	if first.Attempts != second.Attempts {
		t.Errorf("the same request took %d attempts and then %d", first.Attempts, second.Attempts)
	}

	other := synthConfig(12)
	third, err := Generate(context.Background(), NewM1Oracle(), other)
	if err != nil {
		t.Fatalf("third generate: %v", err)
	}
	if bytes.Equal(left, third.Canonical()) {
		t.Fatal("two different seeds produced the same layout, so the seed is not reaching the draw")
	}
}

// TestCanonicalCoversWhatItClaimsTo is what keeps the determinism test from
// being vacuous. An encoder that left the geometry out would make two
// different maps compare equal, and the test above would pass for the wrong
// reason. Each mutation below is a thing a consumer can observe, so each one
// has to change the bytes.
func TestCanonicalCoversWhatItClaimsTo(t *testing.T) {
	layout, err := Generate(context.Background(), NewM1Oracle(), synthConfig(11))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	base := layout.Canonical()
	if len(layout.JumpGraph.Edges) == 0 || len(layout.Plane.Rooms) == 0 {
		t.Fatal("the layout is too empty to mutate")
	}

	// Canonical reads through the slices it is given, so each case copies the
	// one slice it edits and leaves the original layout alone.
	cases := []struct {
		name   string
		mutate func(*PlatformLayout)
	}{
		{"one cell of one room", func(l *PlatformLayout) {
			rooms := append([]Room(nil), l.Plane.Rooms...)
			cells := append([]CellKind(nil), rooms[0].Grid.Cells...)
			for index, cell := range cells {
				if cell == CellKindEmpty {
					cells[index] = CellKindSolid
					break
				}
			}
			rooms[0].Grid.Cells = cells
			l.Plane.Rooms = rooms
		}},
		{"the room origin", func(l *PlatformLayout) {
			rooms := append([]Room(nil), l.Plane.Rooms...)
			rooms[0].Origin.X++
			l.Plane.Rooms = rooms
		}},
		{"the spawn", func(l *PlatformLayout) { l.Plane.Spawn.At.X++ }},
		{"the verdict", func(l *PlatformLayout) { l.Judgement.Verdict = VerdictRejected }},
		{"the model", func(l *PlatformLayout) { l.Judgement.Model = "something else" }},
		{"one edge's duration", func(l *PlatformLayout) {
			edges := append([]MotionEdge(nil), l.JumpGraph.Edges...)
			edges[0].Duration += 1e-12
			l.JumpGraph.Edges = edges
		}},
		{"one node's footing", func(l *PlatformLayout) {
			nodes := append([]MotionNode(nil), l.JumpGraph.Nodes...)
			nodes[0].Footing = nodes[0].Footing.Shift(1e-12)
			l.JumpGraph.Nodes = nodes
		}},
		{"the request seed", func(l *PlatformLayout) { l.Config.Seed++ }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			mutated := layout
			test.mutate(&mutated)
			if bytes.Equal(base, mutated.Canonical()) {
				t.Fatalf("changing %s did not change the canonical encoding, so it is not covered", test.name)
			}
		})
	}

	// A witness is part of the certificate, so it is part of the identity.
	var witnessed bool
	for _, edge := range layout.JumpGraph.Edges {
		if edge.Witness == nil || len(edge.Witness.Phases) == 0 {
			continue
		}
		witnessed = true
		break
	}
	if !witnessed {
		t.Fatal("no certified edge carries a witness, so the witness half of the encoding is untested")
	}
}

// TestTheSameSeedIsAlsoTheSameWithTheFake guards the other direction of
// provenance: the fake is a different model and must produce a layout that
// says so, while still being deterministic.
func TestTheSameSeedIsAlsoTheSameWithTheFake(t *testing.T) {
	config := synthConfig(5)
	first, err := Generate(context.Background(), &FakeOracle{}, config)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	second, err := Generate(context.Background(), &FakeOracle{}, config)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if !bytes.Equal(first.Canonical(), second.Canonical()) {
		t.Fatal("the fake oracle produced two different layouts for one seed")
	}
	if first.Judgement.Model != ModelFake {
		t.Errorf("a layout judged by the fake carries model %q, not %q", first.Judgement.Model, ModelFake)
	}
	if first.JumpGraph.Model != ModelFake {
		t.Errorf("the merged graph carries model %q, not %q", first.JumpGraph.Model, ModelFake)
	}
}

// TestAttemptSeedsAreTheAttemptStreamInOrder freezes the first rule of the
// consumption order: one Next per attempt, from a stream derived from the
// request seed with this package's own salt. A change to where the attempt
// seeds come from changes every map, so it has to be visible.
func TestAttemptSeedsAreTheAttemptStreamInOrder(t *testing.T) {
	config := narrowLedgeConfig(1)
	layout, err := Generate(context.Background(), NewM1Oracle(), config)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(layout.AttemptSeeds) == 0 {
		t.Fatal("no attempt seed was recorded")
	}
	stream := core.NewSplitMix64(config.Seed, synthAttemptSalt)
	for index, seed := range layout.AttemptSeeds {
		if want := Seed(stream.Next()); seed != want {
			t.Fatalf("attempt %d used seed %d; the attempt stream's value %d is %d", index, seed, index, want)
		}
	}
}

// TestBudgetExhaustionIsUnknownAndKeepsThePartialGraph is the rule the whole
// package is built around, at the layout level: a ceiling that stopped a
// search is never a rejection and never an error, and the graph that was
// built before it stopped is real and comes back.
func TestALayoutBudgetExhaustionIsUnknownAndKeepsThePartialGraph(t *testing.T) {
	config := synthConfig(11)
	config.Budget = DefaultSearchBudget()
	config.Budget.MaxCollisionTests = 1

	layout, err := Generate(context.Background(), NewM1Oracle(), config)
	if err != nil {
		t.Fatalf("a budget that ran out must not be an error, got %v", err)
	}
	if layout.Judgement.Verdict != VerdictUnknown {
		t.Fatalf("a budget that ran out gave verdict %s, not unknown", layout.Judgement.Verdict)
	}
	if layout.Judgement.Reason != ReasonBudgetExhausted {
		t.Errorf("the reason is %s, not budget-exhausted", layout.Judgement.Reason)
	}
	if !layout.Judgement.Budget.Exhausted {
		t.Error("the judgement does not report an exhausted budget")
	}
	if len(layout.JumpGraph.Nodes) == 0 {
		t.Fatal("the partial graph is empty; a build that reached a ceiling still built something")
	}
	if len(layout.Plane.Rooms) == 0 {
		t.Fatal("the plane is empty, so there was nothing to be partial about")
	}
	if layout.Attempts != 1 {
		t.Errorf("an exhausted budget was retried: %d attempts. The next attempt runs under the same ceiling", layout.Attempts)
	}
	if err := layout.Judgement.Validate(); err != nil {
		t.Errorf("the judgement does not validate: %v", err)
	}
}

// TestARejectedDrawIsRetriedUntilItConverges is the retry contract. The first
// attempt is refused by the oracle, a later one is certified, and the test
// shows both halves: that attempt zero on its own really is a rejection, and
// that the layout returned is the certified one.
func TestARejectedDrawIsRetriedUntilItConverges(t *testing.T) {
	config := narrowLedgeConfig(2).Normalize()

	layout, err := Generate(context.Background(), NewM1Oracle(), config)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if layout.Judgement.Verdict != VerdictCertified {
		t.Fatalf("the retry did not converge: %s/%s %q", layout.Judgement.Verdict, layout.Judgement.Reason, layout.Judgement.Detail)
	}
	if layout.Attempts < 2 {
		t.Fatalf("the layout was certified on attempt %d, so no retry was exercised", layout.Attempts)
	}

	// The retry only means something if the draw it replaced was genuinely
	// refused. Replaying the first attempt's seed on its own shows that it
	// was, and names the reason.
	first, err := synthesizeOnce(context.Background(), NewM1Oracle(), config, layout.AttemptSeeds[0])
	if err != nil {
		t.Fatalf("replaying the first attempt: %v", err)
	}
	if first.Judgement.Verdict != VerdictRejected {
		t.Fatalf("the first attempt was %s, so the retry was not driven by a rejection", first.Judgement.Verdict)
	}
	if first.Judgement.Budget.Exhausted {
		t.Error("the first attempt was rejected with an exhausted budget, which is the one combination that must not happen")
	}

	// And the retry is deterministic: the same request converges the same way
	// every time, on the same attempt.
	again, err := Generate(context.Background(), NewM1Oracle(), config)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if again.Attempts != layout.Attempts {
		t.Errorf("the retry converged on attempt %d and then on attempt %d", layout.Attempts, again.Attempts)
	}
	if !bytes.Equal(layout.Canonical(), again.Canonical()) {
		t.Error("the converged layout differs between two runs of the same request")
	}
}

// TestARetryThatDoesNotConvergeIsHonest covers the other end. Every draw is
// refused, and the answer is the last rejection with the map still attached —
// not an error, not Unknown, and not silence. Unknown in particular would be
// a lie: this pass knows exactly what is wrong and can point at it.
func TestARetryThatDoesNotConvergeIsHonest(t *testing.T) {
	config := narrowLedgeConfig(1).Normalize()

	layout, err := Generate(context.Background(), NewM1Oracle(), config)
	if err != nil {
		t.Fatalf("a refused map is a verdict, not an error, got %v", err)
	}
	if layout.Judgement.Verdict != VerdictRejected {
		t.Fatalf("every draw was refused and the verdict is %s", layout.Judgement.Verdict)
	}
	if layout.Attempts != synthMaxAttempts {
		t.Errorf("the retry gave up after %d attempts, not %d", layout.Attempts, synthMaxAttempts)
	}
	if len(layout.AttemptSeeds) != synthMaxAttempts {
		t.Errorf("%d attempt seeds were recorded for %d attempts", len(layout.AttemptSeeds), synthMaxAttempts)
	}
	if layout.Judgement.Budget.Exhausted {
		t.Error("a rejection must never carry an exhausted budget: that pair is a rejection sold on an unfinished search")
	}
	if err := layout.Judgement.Validate(); err != nil {
		t.Errorf("the judgement does not validate: %v", err)
	}
	if !strings.Contains(layout.Judgement.Detail, "every one was rejected") {
		t.Errorf("the detail does not say the retries ran out: %q", layout.Judgement.Detail)
	}
	// The counterexample is still in the layout. An answer of "no" that threw
	// the map away would be unactionable.
	if len(layout.Plane.Rooms) == 0 || len(layout.JumpGraph.Nodes) == 0 {
		t.Fatal("the rejected layout carries no map to point at")
	}
	var refused int
	for _, room := range layout.Rooms {
		if room.Composition.Verdict == VerdictRejected {
			refused++
		}
	}
	if refused == 0 {
		t.Error("no room reports the composition that was refused")
	}
}

// TestTheStampNeverTouchesARoomBorder is the placement invariant that keeps
// the macro front's work intact. The border ring carries the openings the
// macro front punched and ValidatePlane paired; a stamp that wrote there
// could wall a transition shut and the map would still look valid.
func TestTheStampNeverTouchesARoomBorder(t *testing.T) {
	config := synthConfig(11).Normalize()
	layout, err := Generate(context.Background(), NewM1Oracle(), config)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// The macro front is deterministic in its own seed, so drawing it again
	// from the attempt that produced this layout gives the unstamped shells
	// the stamp started from.
	streams := newSynthStreams(layout.AttemptSeeds[layout.Attempts-1])
	shells, err := GenerateMacro(context.Background(), macroConfigFor(config, streams.macro))
	if err != nil {
		t.Fatalf("redrawing the shells: %v", err)
	}
	if len(shells.Plane.Rooms) != len(layout.Plane.Rooms) {
		t.Fatalf("the redrawn plane has %d rooms against %d", len(shells.Plane.Rooms), len(layout.Plane.Rooms))
	}

	var checked int
	for index, room := range layout.Plane.Rooms {
		shell := shells.Plane.Rooms[index]
		if shell.Grid.Width != room.Grid.Width || shell.Grid.Height != room.Grid.Height {
			t.Fatalf("room %d is %dx%d against the shell's %dx%d", room.ID, room.Grid.Width, room.Grid.Height, shell.Grid.Width, shell.Grid.Height)
		}
		for y := uint32(0); y < room.Grid.Height; y++ {
			for x := uint32(0); x < room.Grid.Width; x++ {
				if x != 0 && y != 0 && x+1 != room.Grid.Width && y+1 != room.Grid.Height {
					continue
				}
				checked++
				at := y*room.Grid.Width + x
				if room.Grid.Cells[at] != shell.Grid.Cells[at] {
					t.Fatalf("room %d border cell (%d,%d) is %s after the stamp and was %s before it", room.ID, x, y, room.Grid.Cells[at], shell.Grid.Cells[at])
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no border cell was compared")
	}
}

// TestEveryStampedBeatLiesInsideItsRoom is the other half of the same rule: a
// beat that did not fit is dropped and counted, never clipped. A clipped beat
// is geometry the oracle never saw, presented as if it had.
func TestEveryStampedBeatLiesInsideItsRoom(t *testing.T) {
	layout, err := Generate(context.Background(), &FakeOracle{}, synthConfig(23))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var stamped, dropped int
	for _, room := range layout.Rooms {
		grid := layout.Plane.Rooms[room.Room].Grid
		dropped += room.Dropped
		for _, placement := range room.Placements {
			stamped++
			if placement.Origin.X < 1 || placement.Origin.Y < 1 {
				t.Errorf("room %d: beat %d starts at %v, on or outside the border", room.Room, placement.Beat, placement.Origin)
			}
			if placement.Origin.X+int32(placement.Width) > int32(grid.Width)-1 {
				t.Errorf("room %d: beat %d ends at x=%d in a grid %d wide", room.Room, placement.Beat, placement.Origin.X+int32(placement.Width), grid.Width)
			}
			if placement.Origin.Y+int32(placement.Height) > int32(grid.Height) {
				t.Errorf("room %d: beat %d ends at y=%d in a grid %d tall", room.Room, placement.Beat, placement.Origin.Y+int32(placement.Height), grid.Height)
			}
		}
	}
	if stamped == 0 {
		t.Fatal("no beat was stamped, so the bounds above were never tested")
	}
	t.Logf("%d beats stamped, %d dropped", stamped, dropped)
}

// TestAStampedRunChainsAtMatchingHeights is what makes the composition check
// meaningful rather than vacuous: beat i's arrival platform and beat i+1's
// departure platform are the same ledge, at the same height and touching. If
// they were not, every run would be refused for a reason that has nothing to
// do with the moveset.
//
// Every assertion reads the GRID, not the BeatPlacement that claims to
// describe it. A record and the cells it describes are two different things,
// and a placement pass that drifted apart from its own bookkeeping would pass
// a test written against the bookkeeping alone — the oracle reads the grid,
// so the test has to as well.
func TestAStampedRunChainsAtMatchingHeights(t *testing.T) {
	layout, err := Generate(context.Background(), &FakeOracle{}, synthConfig(23))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// standsOn asserts that a character whose feet are at (x, height) in this
	// grid has a solid cell under them and air at knee level.
	standsOn := func(t *testing.T, grid Grid, room RoomID, label string, x, height float64) {
		t.Helper()
		column := int32(x)
		row := int32(grid.Height) - int32(height)
		under, ok := grid.At(Cell{X: column, Y: row})
		if !ok {
			t.Errorf("room %d: the %s platform's support cell (%d,%d) is outside a %dx%d grid", room, label, column, row, grid.Width, grid.Height)
			return
		}
		if !under.Supports() {
			t.Errorf("room %d: the %s platform claims feet at height %v, and the cell under them (%d,%d) is %s", room, label, height, column, row, under)
		}
		above, ok := grid.At(Cell{X: column, Y: row - 1})
		if ok && above.Blocks() {
			t.Errorf("room %d: the %s platform claims feet at height %v, and the cell they occupy (%d,%d) is %s", room, label, height, column, row-1, above)
		}
	}

	var chained int
	for _, room := range layout.Rooms {
		grid := layout.Plane.Rooms[room.Room].Grid
		for index, placement := range room.Placements {
			standsOn(t, grid, room.Room, "departure", placement.DepartureX, placement.DepartureHeight)
			standsOn(t, grid, room.Room, "arrival", placement.ArrivalX, placement.ArrivalHeight)
			if index == 0 {
				continue
			}
			previous := room.Placements[index-1]
			if previous.ArrivalHeight != placement.DepartureHeight {
				t.Errorf("room %d: beat %d arrives at height %v and beat %d departs from %v",
					room.Room, previous.Beat, previous.ArrivalHeight, placement.Beat, placement.DepartureHeight)
			}
			if want := previous.Origin.X + int32(previous.Width); placement.Origin.X != want {
				t.Errorf("room %d: beat %d ends at x=%d and beat %d starts at x=%d",
					room.Room, previous.Beat, want, placement.Beat, placement.Origin.X)
			}
			chained++
		}
	}
	if chained == 0 {
		t.Fatal("no room stamped two beats, so nothing was chained and the composition check is vacuous")
	}
}

// TestTheMergedGraphRenumbersWithoutCollision checks the one thing a
// concatenation of graphs can get wrong. Ids have to stay dense and in order,
// and every reference has to follow its target into the new numbering.
func TestTheMergedGraphRenumbersWithoutCollision(t *testing.T) {
	layout, err := Generate(context.Background(), &FakeOracle{}, synthConfig(23))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	graph := layout.JumpGraph
	if len(graph.Nodes) == 0 || len(graph.Edges) == 0 {
		t.Fatal("the merged graph is empty")
	}
	for index, node := range graph.Nodes {
		if node.ID != MotionNodeID(index) {
			t.Fatalf("node %d carries id %d; Nodes must be dense and ascending", index, node.ID)
		}
		if int(node.Surface) >= len(graph.Surfaces) {
			t.Fatalf("node %d points at surface %d of %d", index, node.Surface, len(graph.Surfaces))
		}
	}
	for index, surface := range graph.Surfaces {
		if surface.ID != SurfaceID(index) {
			t.Fatalf("surface %d carries id %d", index, surface.ID)
		}
	}
	for index, edge := range graph.Edges {
		if edge.ID != MotionEdgeID(index) {
			t.Fatalf("edge %d carries id %d", index, edge.ID)
		}
		if int(edge.From) >= len(graph.Nodes) || int(edge.To) >= len(graph.Nodes) {
			t.Fatalf("edge %d runs %d->%d in a graph of %d nodes", index, edge.From, edge.To, len(graph.Nodes))
		}
	}

	// Each room's declared range has to be the room's own, and the ranges
	// have to tile the graph without a gap.
	//
	// The assertions that actually bite are the CONTAINMENT ones below, not
	// the bounds checks above. The oracle builds each room from zero, so a
	// reference that was never shifted still lands inside the first room's
	// range and stays a legal index: "in range" cannot tell a renumbered
	// reference from an un-renumbered one. Requiring every edge to join two
	// nodes of its OWN room, and every node to name a surface of its own
	// room, can — and it is true by construction, because the oracle builds
	// one room at a time and emits no edge across rooms.
	var nextNode MotionNodeID
	var nextEdge MotionEdgeID
	var nextSurface SurfaceID
	var crossRoom int
	for _, room := range layout.Rooms {
		if room.NodeBase != nextNode || room.EdgeBase != nextEdge || room.SurfaceBase != nextSurface {
			t.Fatalf("room %d declares bases %d/%d/%d where the running totals are %d/%d/%d",
				room.Room, room.NodeBase, room.EdgeBase, room.SurfaceBase, nextNode, nextEdge, nextSurface)
		}
		nodeEnd := room.NodeBase + MotionNodeID(room.NodeCount)
		surfaceEnd := room.SurfaceBase + SurfaceID(room.SurfaceCount)

		for offset := uint32(0); offset < room.SurfaceCount; offset++ {
			if got := graph.Surfaces[uint32(room.SurfaceBase)+offset].Room; got != room.Room {
				t.Fatalf("surface %d is inside room %d's range and names room %d", uint32(room.SurfaceBase)+offset, room.Room, got)
			}
		}
		for offset := uint32(0); offset < room.NodeCount; offset++ {
			node := graph.Nodes[uint32(room.NodeBase)+offset]
			if node.Surface < room.SurfaceBase || node.Surface >= surfaceEnd {
				t.Fatalf("node %d belongs to room %d and names surface %d, outside that room's range [%d, %d)",
					node.ID, room.Room, node.Surface, room.SurfaceBase, surfaceEnd)
			}
		}
		for offset := uint32(0); offset < room.EdgeCount; offset++ {
			edge := graph.Edges[uint32(room.EdgeBase)+offset]
			if edge.From < room.NodeBase || edge.From >= nodeEnd || edge.To < room.NodeBase || edge.To >= nodeEnd {
				t.Fatalf("edge %d belongs to room %d and runs %d->%d, outside that room's node range [%d, %d)",
					edge.ID, room.Room, edge.From, edge.To, room.NodeBase, nodeEnd)
			}
			crossRoom++
		}

		nextNode = nodeEnd
		nextEdge += MotionEdgeID(room.EdgeCount)
		nextSurface = surfaceEnd
	}
	if int(nextNode) != len(graph.Nodes) || int(nextEdge) != len(graph.Edges) || int(nextSurface) != len(graph.Surfaces) {
		t.Fatalf("the room ranges cover %d/%d/%d of %d/%d/%d", nextNode, nextEdge, nextSurface, len(graph.Nodes), len(graph.Edges), len(graph.Surfaces))
	}
	if crossRoom == 0 {
		t.Fatal("no edge was checked for containment")
	}
	if len(layout.Rooms) < 2 || layout.Rooms[1].NodeBase == 0 {
		t.Fatal("the map has no second room with a non-zero base, so a missing offset would be invisible here")
	}
}

// TestTheCertifiedGraphCarriesNoTransitionEdge records the decision that the
// two graphs are not merged. The oracle does not model a room crossing, so no
// edge in the certified graph may claim one; the room-level graph is where
// the crossings live, and it is a separate field for that reason.
func TestTheCertifiedGraphCarriesNoTransitionEdge(t *testing.T) {
	layout, err := Generate(context.Background(), &FakeOracle{}, synthConfig(23))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, edge := range layout.JumpGraph.Edges {
		if edge.Kind == MotionEdgeKindTransition {
			t.Fatalf("edge %d is a transition edge in the certified graph; nothing certified it", edge.ID)
		}
	}
	if len(layout.RoomGraph.Nodes) != len(layout.Plane.Rooms) {
		t.Errorf("the room graph has %d nodes for %d rooms", len(layout.RoomGraph.Nodes), len(layout.Plane.Rooms))
	}
}

// TestAGatedRoomIsAuthoredForTheMovesetThatReachesIt checks the progression
// seam. A room behind the dash gate is built for a character that has the
// dash; a room in no locked region is built for the base moveset.
func TestAGatedRoomIsAuthoredForTheMovesetThatReachesIt(t *testing.T) {
	config := synthConfig(29)
	config.Width, config.Height = 220, 140
	config.Progression = ProgressionPlan{Steps: []ProgressionStep{
		{Name: "dash", Grants: NewAbilitySet(AbilityDash)},
		{Name: "double jump", Grants: NewAbilitySet(AbilityDoubleJump)},
	}}

	layout, err := Generate(context.Background(), &FakeOracle{}, config)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(layout.Locks) == 0 {
		t.Fatal("the map has no locked region, so there is no gating to check")
	}

	authored := make(map[RoomID]AbilitySet, len(layout.Rooms))
	for _, room := range layout.Rooms {
		authored[room.Room] = room.Abilities
	}
	stages := layout.Plan.Stages()
	var gated int
	for _, lock := range layout.Locks {
		want := stages[lock.Step+1]
		for _, room := range lock.Rooms {
			gated++
			if !authored[room].Contains(want) {
				t.Errorf("room %d is behind step %d's gate and was authored for %s, which does not include %s",
					room, lock.Step, authored[room], want)
			}
		}
	}
	if gated == 0 {
		t.Fatal("no room sits behind a gate")
	}
	var base int
	for _, room := range layout.Rooms {
		if room.Abilities == layout.Plan.Base {
			base++
		}
	}
	if base == 0 {
		t.Error("every room was authored for a later stage; the spawn side of the map should be base")
	}
}

// TestANilOracleIsAnErrorAndNotAVerdict keeps the line between the two. A
// missing oracle is a malformed question: nobody was asked, so there is no
// answer to report, and inventing a default would decide whose certificate
// the caller is holding.
func TestANilOracleIsAnErrorAndNotAVerdict(t *testing.T) {
	_, err := Generate(context.Background(), nil, synthConfig(1))
	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("a nil oracle gave %v, not ErrInvalidQuery", err)
	}
}

// TestAMalformedRequestFailsBeforeAnyDraw checks that the question is judged
// before the map is. Each case below is about the request, so each one is an
// error and none of them costs an attempt.
func TestAMalformedRequestFailsBeforeAnyDraw(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		sentine error
	}{
		{"zero width", func(c *Config) { c.Width = 0 }, ErrInvalidConfig},
		{"no beats", func(c *Config) { c.Beats = BeatConfig{} }, ErrInvalidBeats},
		{"unparameterised grant", func(c *Config) {
			c.Profile.Dash = nil
			c.Progression = ProgressionPlan{Steps: []ProgressionStep{{Name: "dash", Grants: NewAbilitySet(AbilityDash)}}}
		}, ErrInvalidProgression},
		{"plane smaller than one room", func(c *Config) { c.Width, c.Height = 4, 4 }, ErrInvalidConfig},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			config := synthConfig(1)
			test.mutate(&config)
			_, err := Generate(context.Background(), NewM1Oracle(), config)
			if !errors.Is(err, test.sentine) {
				t.Fatalf("got %v, want %v", err, test.sentine)
			}
		})
	}
}

// TestACancelledContextStopsWithoutAVerdict checks rule 4 of the oracle
// contract at the layout level: cancellation is an error, never a map.
func TestACancelledContextStopsWithoutAVerdict(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Generate(ctx, NewM1Oracle(), synthConfig(1))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled context gave %v", err)
	}
}

// TestSynthesisSaltsAreItsOwn is the rule from the plan: the dungeon's seven
// salts are private to the root because they are another generator's
// vocabulary, and borrowing one would tie a platform map's identity to a
// dungeon's. The seven are repeated here as literals because this package
// cannot see them, which is the point.
func TestSynthesisSaltsAreItsOwn(t *testing.T) {
	dungeon := []uint64{
		0xA0B1C2D3E4F56789, 0x1F2E3D4C5B6A7988, 0x9E3779B97F4A7C15,
		0x6C8E9CF570932BD5, 0xD1B54A32D192ED03, 0xC3D4E5F60718293A,
		0x7A6B5C4D3E2F1A09,
	}
	platform := map[string]uint64{
		"synthAttempt":   synthAttemptSalt,
		"synthMacro":     synthMacroSalt,
		"synthRhythm":    synthRhythmSalt,
		"synthPlacement": synthPlacementSalt,
		"spineShape":     spineShapeSalt,
		"beatChoice":     beatChoiceSalt,
		"geometrySpan":   geometrySpanSalt,
		"rooms":          saltRooms,
	}
	seen := make(map[uint64]string, len(platform))
	for name, salt := range platform {
		if other, clash := seen[salt]; clash {
			t.Errorf("%s and %s are the same salt %#016x, so they are one stream", name, other, salt)
		}
		seen[salt] = name
		for _, forbidden := range dungeon {
			if salt == forbidden {
				t.Errorf("%s reuses the dungeon salt %#016x", name, forbidden)
			}
		}
	}
}

// TestNoVerdictIsSoldOnAnUnfinishedSearch sweeps several seeds and asserts the
// two rules that cannot be allowed to drift: every judgement validates, and an
// exhausted budget only ever accompanies Unknown.
func TestNoVerdictIsSoldOnAnUnfinishedSearch(t *testing.T) {
	for seed := Seed(1); seed <= 8; seed++ {
		layout, err := Generate(context.Background(), &FakeOracle{}, synthConfig(seed))
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if err := layout.Judgement.Validate(); err != nil {
			t.Errorf("seed %d: the layout judgement does not validate: %v", seed, err)
		}
		if layout.Judgement.Budget.Exhausted && layout.Judgement.Verdict != VerdictUnknown {
			t.Errorf("seed %d: verdict %s with an exhausted budget", seed, layout.Judgement.Verdict)
		}
		for _, room := range layout.Rooms {
			for label, part := range map[string]Judgement{"build": room.Build, "composition": room.Composition} {
				if err := part.Validate(); err != nil {
					t.Errorf("seed %d room %d: the %s judgement does not validate: %v", seed, room.Room, label, err)
				}
				if part.Budget.Exhausted && part.Verdict != VerdictUnknown {
					t.Errorf("seed %d room %d: the %s verdict is %s with an exhausted budget", seed, room.Room, label, part.Verdict)
				}
			}
		}
	}
}

// TestRoomSizeFollowsTheBeatVocabulary records the decision in macroConfigFor:
// the room is sized to the beats, not the beats to the room. A caller that
// declares wider beats gets wider rooms, which is what makes a declared
// MaxCells a declaration rather than a suggestion.
func TestRoomSizeFollowsTheBeatVocabulary(t *testing.T) {
	narrow := synthConfig(1)
	for index := range narrow.Beats.Definitions {
		narrow.Beats.Definitions[index].MinCells = 3
		narrow.Beats.Definitions[index].MaxCells = 4
	}
	wide := synthConfig(1)
	for index := range wide.Beats.Definitions {
		wide.Beats.Definitions[index].MinCells = 10
		wide.Beats.Definitions[index].MaxCells = 12
	}

	narrowMacro := macroConfigFor(narrow, 1)
	wideMacro := macroConfigFor(wide, 1)
	if !(wideMacro.MaxWidth > narrowMacro.MaxWidth) {
		t.Fatalf("a vocabulary of 10..12 cell beats gave rooms up to %d wide and one of 3..4 gave %d", wideMacro.MaxWidth, narrowMacro.MaxWidth)
	}
	if narrowMacro.MinWidth < widestBeatCells(narrow) {
		t.Errorf("the smallest room is %d wide and the widest beat is %d, so a room may hold no beat at all", narrowMacro.MinWidth, widestBeatCells(narrow))
	}
	if wideMacro.MaxWidth < synthBeatsPerRoom*widestBeatCells(wide) {
		t.Errorf("the largest room is %d wide, below %d beats of %d", wideMacro.MaxWidth, synthBeatsPerRoom, widestBeatCells(wide))
	}
}

// TestAWiderVocabularyPlacesMoreBeatsPerRoom is the same decision observed end
// to end rather than in the arithmetic: the rooms really do grow, and the runs
// really do land in them.
func TestAWiderVocabularyPlacesMoreBeatsPerRoom(t *testing.T) {
	config := synthConfig(23)
	config.Width, config.Height = 160, 100
	layout, err := Generate(context.Background(), &FakeOracle{}, config)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var placed, rooms int
	for _, room := range layout.Rooms {
		placed += len(room.Placements)
		if len(room.Placements) >= 2 {
			rooms++
		}
	}
	if rooms == 0 {
		t.Fatalf("no room holds two beats; %d beats were placed across %d rooms", placed, len(layout.Rooms))
	}
	t.Logf("%d of %d rooms hold two or more beats, %d beats in total", rooms, len(layout.Rooms), placed)
}

// TestStampingNeverErasesWhatIsAlreadyThere pins rule 2 of the placement
// contract directly, because the generator cannot pin it: it lays beats in
// disjoint columns, so there the rule holds for free and a test written
// against a generated map would pass whether the rule existed or not.
//
// Here two stamps deliberately overlap. The second one's air must leave the
// first one's platform standing, and the border ring must survive both.
func TestStampingNeverErasesWhatIsAlreadyThere(t *testing.T) {
	destination := shellGrid(10, 8)
	solid := Grid{Width: 2, Height: 2, Cells: []CellKind{CellKindSolid, CellKindSolid, CellKindSolid, CellKindSolid}}
	air := Grid{Width: 4, Height: 4, Cells: make([]CellKind, 16)}

	stampInterior(&destination, solid, 3, 3)
	before := append([]CellKind(nil), destination.Cells...)

	// A block of pure air laid straight over the platform just stamped.
	stampInterior(&destination, air, 2, 2)
	for index, kind := range destination.Cells {
		if kind != before[index] {
			t.Fatalf("cell %d went from %s to %s: stamping air erased what was there", index, before[index], kind)
		}
	}

	// And a stamp that runs off every edge leaves the border ring intact.
	wide := Grid{Width: 14, Height: 12, Cells: make([]CellKind, 14*12)}
	for index := range wide.Cells {
		wide.Cells[index] = CellKindSolid
	}
	shell := shellGrid(10, 8)
	stampInterior(&shell, wide, -2, -2)
	for y := uint32(0); y < shell.Height; y++ {
		for x := uint32(0); x < shell.Width; x++ {
			onBorder := x == 0 || y == 0 || x+1 == shell.Width || y+1 == shell.Height
			kind := shell.Cells[y*shell.Width+x]
			if onBorder && kind != CellKindSolid {
				t.Fatalf("border cell (%d,%d) is %s", x, y, kind)
			}
			if !onBorder && kind != CellKindSolid {
				t.Fatalf("interior cell (%d,%d) is %s; the overhanging stamp should still fill the interior", x, y, kind)
			}
		}
	}
}
