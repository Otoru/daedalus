package platform

import (
	"context"
	"math"
	"testing"
)

// --- the witness is the certificate, so it is re-checked independently ----

// TestEveryCertifiedEdgeCarriesAWitnessThatReplays re-integrates every phase
// of every edge of a graph and compares the result with the phase's own
// endpoints, the next phase's start, and the two nodes the edge joins.
//
// This is the invariant that makes a certificate worth something: the oracle
// says a manoeuvre exists AND hands over the trajectory, and the trajectory
// is checked by arithmetic that does not go through the code that produced
// it. An edge whose witness does not land where the edge claims is a lie,
// however convincing the verdict looks.
func TestEveryCertifiedEdgeCarriesAWitnessThatReplays(t *testing.T) {
	profile := testProfile()
	abilities := NewAbilitySet(AbilityDash, AbilityDoubleJump, AbilityWallJump)
	for _, fixture := range []struct {
		name string
		rows []string
	}{
		{name: "pit", rows: gapRoom(false)},
		{name: "one-way platform", rows: oneWayRoom()},
		{name: "intermediate platform", rows: crossingRoom(true)},
		{name: "shaft", rows: shaftRoom()},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			result := buildGraph(t, fixture.rows, profile, abilities)
			graph := &result.Graph
			if len(graph.Edges) == 0 {
				t.Fatalf("the fixture must produce edges to check")
			}
			for _, edge := range graph.Edges {
				if edge.Witness == nil {
					t.Fatalf("edge %d (%s) is certified without a witness", edge.ID, edge.Kind)
				}
				replayWitness(t, graph, edge, profile)
			}
		})
	}
}

func replayWitness(t *testing.T, graph *JumpGraph, edge MotionEdge, profile MovementProfile) {
	t.Helper()
	const tolerance = 1e-6
	phases := edge.Witness.Phases
	if len(phases) == 0 {
		t.Fatalf("edge %d has an empty witness", edge.ID)
	}
	for i, phase := range phases {
		if phase.Duration < 0 {
			t.Fatalf("edge %d phase %d runs backwards", edge.ID, i)
		}
		gotX := phase.Start.X + phase.Start.VX*phase.Duration + phase.AccelX*phase.Duration*phase.Duration/2
		gotY := phase.Start.Y + phase.Start.VY*phase.Duration + phase.AccelY*phase.Duration*phase.Duration/2
		if math.Abs(gotX-phase.End.X) > tolerance || math.Abs(gotY-phase.End.Y) > tolerance {
			t.Fatalf("edge %d (%s) phase %d does not integrate to its own end: got (%v, %v), stated (%v, %v)",
				edge.ID, edge.Kind, i, gotX, gotY, phase.End.X, phase.End.Y)
		}
		if i+1 < len(phases) {
			next := phases[i+1]
			if math.Abs(next.Start.X-phase.End.X) > tolerance || math.Abs(next.Start.Y-phase.End.Y) > tolerance {
				t.Fatalf("edge %d (%s) jumps in space between phases %d and %d", edge.ID, edge.Kind, i, i+1)
			}
		}
		if phase.Mode == MotionModeAirborne && phase.AccelY > 0 {
			t.Fatalf("edge %d phase %d accelerates a free body upward", edge.ID, i)
		}
	}
	from, to := graph.Nodes[edge.From], graph.Nodes[edge.To]
	start, end := phases[0].Start, phases[len(phases)-1].End
	if from.Mode == MotionModeGrounded {
		if math.Abs(start.X-from.Footing.Lo) > tolerance || math.Abs(start.Y-from.Height) > tolerance {
			t.Fatalf("edge %d starts at (%v, %v) but leaves node %d at (%v, %v)",
				edge.ID, start.X, start.Y, from.ID, from.Footing.Lo, from.Height)
		}
	}
	if to.Mode == MotionModeGrounded {
		if math.Abs(end.X-to.Footing.Lo) > tolerance || math.Abs(end.Y-to.Height) > tolerance {
			t.Fatalf("edge %d (%s) ends at (%v, %v) but claims node %d at (%v, %v)",
				edge.ID, edge.Kind, end.X, end.Y, to.ID, to.Footing.Lo, to.Height)
		}
	}
	if to.Mode == MotionModeWallCling {
		if math.Abs(end.Y-to.Height) > tolerance {
			t.Fatalf("edge %d ends clinging at %v but claims node %d at %v", edge.ID, end.Y, to.ID, to.Height)
		}
	}
}

// --- the graph is directed -----------------------------------------------

// shelfRoom is a tall room with a floor and a shelf far above it, reachable
// going down and not coming back up.
func shelfRoom() []string {
	return []string{
		"..........",
		"..........",
		"####......",
		"..........",
		"..........",
		"..........",
		"..........",
		"..........",
		"..........",
		"..........",
		"..........",
		"##########",
	}
}

func TestFallingIsFreeAndClimbingBackIsNot(t *testing.T) {
	profile := testProfile()
	result := buildGraph(t, shelfRoom(), profile, 0)
	graph := &result.Graph

	shelf := nodeAt(t, graph, 3.6, 10)
	floor := nodeAt(t, graph, 3.6, 1)
	if shelf.Height != 10 || floor.Height != 1 {
		t.Fatalf("the fixture is a shelf at 10 over a floor at 1, got %v and %v", shelf.Height, floor.Height)
	}

	down, err := NewM1Oracle().FindRoute(context.Background(), RouteQuery{Graph: graph, From: shelf.ID, To: floor.ID})
	if err != nil {
		t.Fatalf("FindRoute: %v", err)
	}
	if !down.Judgement.Certified() {
		t.Fatalf("falling off a shelf is free: %s/%s", down.Judgement.Verdict, down.Judgement.Reason)
	}
	up, err := NewM1Oracle().FindRoute(context.Background(), RouteQuery{Graph: graph, From: floor.ID, To: shelf.ID})
	if err != nil {
		t.Fatalf("FindRoute: %v", err)
	}
	if up.Judgement.Verdict != VerdictRejected || up.Judgement.Reason != ReasonDisconnected {
		t.Fatalf("nine cells is far above the four-cell apex and must not be climbed: %s/%s",
			up.Judgement.Verdict, up.Judgement.Reason)
	}
	// Directedness is a property of the edge list, not only of the search.
	for _, edge := range graph.Edges {
		if graph.Nodes[edge.From].Height == 1 && graph.Nodes[edge.To].Height == 10 {
			t.Fatalf("edge %d (%s) climbs nine cells in one manoeuvre", edge.ID, edge.Kind)
		}
	}
}

// --- wall jump -----------------------------------------------------------

// shaftRoom is a four-cell-wide chimney with solid walls the whole way up
// and a floor at the bottom. Nothing in it is reachable above the four-cell
// apex without putting a hand on a wall.
func shaftRoom() []string {
	const height = 16
	rows := make([]string, height)
	for i := range rows {
		rows[i] = "##....##"
	}
	rows[height-1] = "########"
	return rows
}

// highestReachable returns the highest state a breadth-first walk of the
// graph reaches from a node under a moveset.
func highestReachable(graph *JumpGraph, from MotionNodeID, abilities AbilitySet) float64 {
	visited := make([]bool, len(graph.Nodes))
	visited[from] = true
	queue := []MotionNodeID{from}
	best := graph.Nodes[from].Height
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if h := graph.Nodes[current].Height; h > best {
			best = h
		}
		for _, edge := range graph.OutEdges(current) {
			if !edge.Reachable(abilities) || visited[edge.To] {
				continue
			}
			visited[edge.To] = true
			queue = append(queue, edge.To)
		}
	}
	return best
}

func TestTheShaftIsClimbedOnlyWithTheWallJumpAndTheEdgesSaySo(t *testing.T) {
	profile := testProfile()
	wall := NewAbilitySet(AbilityWallJump)

	without := buildGraph(t, shaftRoom(), profile, 0)
	withWall := buildGraph(t, shaftRoom(), profile, wall)

	for _, node := range without.Graph.Nodes {
		if node.Mode == MotionModeWallCling {
			t.Fatalf("a character without the ability does not hold on to walls")
		}
	}
	clings, jumps := 0, 0
	for _, node := range withWall.Graph.Nodes {
		if node.Mode == MotionModeWallCling {
			clings++
		}
	}
	for _, edge := range withWall.Graph.Edges {
		if edge.Kind == MotionEdgeKindWallJump {
			jumps++
			if !edge.Requires.Has(AbilityWallJump) {
				t.Fatalf("a wall jump must declare the ability it needs, got %s", edge.Requires)
			}
			if withWall.Graph.Nodes[edge.From].Mode != MotionModeWallCling {
				t.Fatalf("edge %d is a wall jump that does not leave a wall", edge.ID)
			}
		}
	}
	if clings == 0 || jumps == 0 {
		t.Fatalf("the shaft must produce wall clings and wall jumps, got %d and %d", clings, jumps)
	}

	// The floor is at world height 1 and a single jump reaches 5. Anything
	// the chimney gives above that came from a wall.
	floor := nodeAt(t, &withWall.Graph, 2.4, 1)
	bare := nodeAt(t, &without.Graph, 2.4, 1)
	plainCeiling := 1 + profile.ApexHeight()
	if got := highestReachable(&without.Graph, bare.ID, 0); got > plainCeiling+1e-9 {
		t.Fatalf("without a wall the chimney tops out at %v, reached %v", plainCeiling, got)
	}
	climbed := highestReachable(&withWall.Graph, floor.ID, wall)
	if climbed <= plainCeiling+1 {
		t.Fatalf("the wall jump must climb well past %v, reached %v", plainCeiling, climbed)
	}

	// The same graph, filtered to the base moveset, loses the height. That is
	// the whole of progression gating: one graph, a predicate per stage.
	if got := highestReachable(&withWall.Graph, floor.ID, 0); got > plainCeiling+1e-9 {
		t.Fatalf("filtering the moveset must take the height back, reached %v", got)
	}

	// And the route to the highest cling is certified with the ability and
	// disconnected without it.
	target := MotionNodeID(0)
	best := -1.0
	for _, node := range withWall.Graph.Nodes {
		if node.Mode == MotionModeWallCling && node.Height > best && node.Height <= climbed+1e-9 {
			best, target = node.Height, node.ID
		}
	}
	with, err := NewM1Oracle().FindRoute(context.Background(), RouteQuery{
		Graph: &withWall.Graph, From: floor.ID, To: target, Abilities: wall,
	})
	if err != nil {
		t.Fatalf("FindRoute: %v", err)
	}
	if !with.Judgement.Certified() {
		t.Fatalf("the chimney is climbable with wall jumps: %s/%s", with.Judgement.Verdict, with.Judgement.Reason)
	}
	if !with.Route.Requires.Has(AbilityWallJump) {
		t.Fatalf("the route must declare the ability it leans on, got %s", with.Route.Requires)
	}
	gated, err := NewM1Oracle().FindRoute(context.Background(), RouteQuery{
		Graph: &withWall.Graph, From: floor.ID, To: target, Abilities: 0,
	})
	if err != nil {
		t.Fatalf("FindRoute: %v", err)
	}
	if gated.Judgement.Verdict != VerdictRejected || gated.Judgement.Reason != ReasonDisconnected {
		t.Fatalf("without the ability the same graph must not route: %s/%s",
			gated.Judgement.Verdict, gated.Judgement.Reason)
	}
}

// --- static climbing -----------------------------------------------------

func ladderRoom() []string {
	return []string{
		"..........",
		"..........",
		"..........",
		"..........",
		"....H.....",
		"....H.....",
		"....H.....",
		"....H.....",
		"....H.....",
		"....H.....",
		"....H.....",
		"##########",
	}
}

func TestALadderIsClimbedOnlyWithTheAbilityAndIsStatic(t *testing.T) {
	profile := testProfile()
	withClimb := buildGraph(t, ladderRoom(), profile, NewAbilitySet(AbilityClimb))
	withoutClimb := buildGraph(t, ladderRoom(), profile, 0)

	climbing := 0
	for _, node := range withClimb.Graph.Nodes {
		if node.Mode == MotionModeClimbing {
			climbing++
		}
	}
	if climbing == 0 {
		t.Fatalf("the ladder must produce climbing states")
	}
	for _, node := range withoutClimb.Graph.Nodes {
		if node.Mode == MotionModeClimbing {
			t.Fatalf("a character without the ability does not use ladders")
		}
	}
	up, down := 0, 0
	for _, edge := range withClimb.Graph.Edges {
		if edge.Kind != MotionEdgeKindClimb {
			continue
		}
		if !edge.Requires.Has(AbilityClimb) {
			t.Fatalf("a climb must declare the ability it needs, got %s", edge.Requires)
		}
		if withClimb.Graph.Nodes[edge.To].Height > withClimb.Graph.Nodes[edge.From].Height {
			up++
		} else {
			down++
		}
	}
	if up == 0 || down == 0 {
		t.Fatalf("a static ladder is traversable both ways, got %d up and %d down", up, down)
	}
	// A ladder does not block anything: the cells stay crossable.
	geom := mustGeometry(t, ladderRoom(), profile)
	for row, runs := range geom.blocking {
		if row == 11 {
			continue
		}
		if len(runs) != 0 {
			t.Fatalf("row %d treats a ladder as a block: %v", row, runs)
		}
	}
}

// --- surfaces, discipline and query hygiene ------------------------------

func TestSurfaceIdentifiersAreStableAndTheDerivationIsExhaustive(t *testing.T) {
	profile := testProfile()
	oracle := NewM1Oracle()
	first, err := oracle.Surfaces(context.Background(), SurfaceQuery{Grid: roomFromRows(t, shaftRoom()), Profile: profile})
	if err != nil {
		t.Fatalf("Surfaces: %v", err)
	}
	second, err := oracle.Surfaces(context.Background(), SurfaceQuery{Grid: roomFromRows(t, shaftRoom()), Profile: profile})
	if err != nil {
		t.Fatalf("Surfaces: %v", err)
	}
	if len(first.Surfaces) != len(second.Surfaces) {
		t.Fatalf("two derivations of one room must agree")
	}
	for i := range first.Surfaces {
		if first.Surfaces[i].ID != SurfaceID(i) {
			t.Fatalf("surface %d carries id %d; ids are positions", i, first.Surfaces[i].ID)
		}
		if first.Surfaces[i].Kind != second.Surfaces[i].Kind || first.Surfaces[i].At != second.Surfaces[i].At {
			t.Fatalf("surface %d differs between derivations", i)
		}
		if first.Surfaces[i].Kind == SurfaceKindUnspecified {
			t.Fatalf("surface %d has no kind", i)
		}
	}
	kinds := map[SurfaceKind]int{}
	for _, surface := range first.Surfaces {
		kinds[surface.Kind]++
	}
	for _, want := range []SurfaceKind{SurfaceKindFloor, SurfaceKindWallLeft, SurfaceKindWallRight} {
		if kinds[want] == 0 {
			t.Fatalf("a chimney has %s surfaces and the derivation found none", want)
		}
	}
	if !first.Judgement.Certified() {
		t.Fatalf("an exhaustive derivation certifies")
	}

	// A ceiling is a solid bottom edge with air under it, which the chimney
	// has none of. The room with a slab over its pit has two.
	overhang, err := oracle.Surfaces(context.Background(), SurfaceQuery{Grid: roomFromRows(t, gapRoom(true)), Profile: profile})
	if err != nil {
		t.Fatalf("Surfaces: %v", err)
	}
	ceilings := 0
	for _, surface := range overhang.Surfaces {
		if surface.Kind == SurfaceKindCeiling {
			ceilings++
		}
	}
	if ceilings == 0 {
		t.Fatalf("the slab over the pit has an underside and the derivation must name it")
	}
}

func TestARestOnlyGraphIsRefusedRatherThanFaked(t *testing.T) {
	// The aggregated discipline is only legal when every node is a rest
	// state, and proving that needs a reset property this oracle does not
	// assume. Saying so is better than returning a smaller graph that lies.
	result, err := NewM1Oracle().BuildGraph(context.Background(), GraphQuery{
		Grid:       roomFromRows(t, gapRoom(false)),
		Profile:    testProfile(),
		Discipline: NodeDisciplineRestOnly,
		Budget:     testBudget(),
	})
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	if result.Judgement.Verdict != VerdictUnknown || result.Judgement.Reason != ReasonUnsupportedGeometry {
		t.Fatalf("want unknown/unsupported-geometry, got %s/%s", result.Judgement.Verdict, result.Judgement.Reason)
	}
}

func TestTheGraphDeclaresItsProvenanceAndItsDiscipline(t *testing.T) {
	profile := testProfile()
	result := buildGraph(t, gapRoom(false), profile, NewAbilitySet(AbilityDash))
	if result.Graph.Model != ModelM1 {
		t.Fatalf("the graph must name the model that built it, got %q", result.Graph.Model)
	}
	if result.Graph.ProfileVersion != profile.Version {
		t.Fatalf("the graph must name the profile it is relative to, got %q", result.Graph.ProfileVersion)
	}
	if result.Graph.Discipline != NodeDisciplineRefined {
		t.Fatalf("this builder emits refined nodes, got %s", result.Graph.Discipline)
	}
	if result.Graph.Abilities != NewAbilitySet(AbilityDash) {
		t.Fatalf("the graph must record the moveset it was built for, got %s", result.Graph.Abilities)
	}
	for _, edge := range result.Graph.Edges {
		if !result.Graph.Abilities.Contains(edge.Requires) {
			t.Fatalf("edge %d needs %s, beyond the graph's %s", edge.ID, edge.Requires, result.Graph.Abilities)
		}
	}
	for i, node := range result.Graph.Nodes {
		if node.ID != MotionNodeID(i) {
			t.Fatalf("node %d carries id %d; ids are positions", i, node.ID)
		}
		if !node.Footing.IsPoint() {
			t.Fatalf("node %d has a wide footing %v; a claim over a continuum cannot be proved", i, node.Footing)
		}
		if node.Mode == MotionModeUnspecified {
			t.Fatalf("node %d has no mode", i)
		}
	}
}

func TestAMalformedQueryIsAnErrorAndNotAVerdict(t *testing.T) {
	oracle := NewM1Oracle()
	profile := testProfile()
	grid := roomFromRows(t, gapRoom(false))

	if _, err := oracle.CheckEdge(context.Background(), EdgeQuery{
		Grid:    grid,
		Profile: profile,
		From:    MotionNode{Surface: 99, Mode: MotionModeGrounded},
		To:      MotionNode{Surface: 0, Mode: MotionModeGrounded},
	}); err == nil {
		t.Fatalf("a node outside the surface table is a malformed question")
	}
	if _, err := oracle.FindRoute(context.Background(), RouteQuery{Graph: nil}); err == nil {
		t.Fatalf("a route query without a graph is malformed")
	}
	graph := buildGraph(t, gapRoom(false), profile, 0).Graph
	if _, err := oracle.FindRoute(context.Background(), RouteQuery{Graph: &graph, From: 0, To: 9999}); err == nil {
		t.Fatalf("a route to a node outside the graph is malformed")
	}
	broken := grid
	broken.Cells = broken.Cells[:len(broken.Cells)-1]
	if _, err := oracle.BuildGraph(context.Background(), GraphQuery{Grid: broken, Profile: profile}); err == nil {
		t.Fatalf("a cell slice that disagrees with the dimensions is malformed")
	}
	bad := profile
	bad.JumpVelocity = 0
	if _, err := oracle.BuildGraph(context.Background(), GraphQuery{Grid: grid, Profile: bad}); err == nil {
		t.Fatalf("a profile that cannot describe a character is malformed")
	}
}

func TestATrivialRouteIsCertifiedWithoutEdges(t *testing.T) {
	graph := buildGraph(t, gapRoom(false), testProfile(), 0).Graph
	result, err := NewM1Oracle().FindRoute(context.Background(), RouteQuery{Graph: &graph, From: 3, To: 3})
	if err != nil {
		t.Fatalf("FindRoute: %v", err)
	}
	if !result.Judgement.Certified() || result.Judgement.Reason != ReasonTrivial {
		t.Fatalf("want certified/trivial, got %s/%s", result.Judgement.Verdict, result.Judgement.Reason)
	}
	if len(result.Route.Edges) != 0 {
		t.Fatalf("the trivial route has no edges")
	}
}

func TestAHazardIsNeverStoodOnAndNeverFlownThrough(t *testing.T) {
	profile := testProfile()
	rows := []string{
		"................",
		"................",
		"................",
		"................",
		"................",
		"................",
		"................",
		"................",
		"................",
		"................",
		"......^^^^......",
		"######^^^^######",
	}
	result := buildGraph(t, rows, profile, 0)
	graph := &result.Graph
	for _, node := range graph.Nodes {
		if node.Mode != MotionModeGrounded {
			continue
		}
		if node.Footing.Lo > 6 && node.Footing.Lo < 10 {
			t.Fatalf("node %d stands on spikes at %v", node.ID, node.Footing)
		}
	}
	for _, edge := range graph.Edges {
		if edge.Witness == nil {
			continue
		}
		for _, phase := range edge.Witness.Phases {
			for _, state := range []MotionState{phase.Start, phase.End} {
				if state.Y < 2.5 && state.X > 6.4 && state.X < 9.6 {
					t.Fatalf("edge %d (%s) passes through the spikes at (%v, %v)", edge.ID, edge.Kind, state.X, state.Y)
				}
			}
		}
	}
}

func TestAGraphBuiltForMoreAbilitiesContainsTheOneBuiltForFewer(t *testing.T) {
	// Grant-only progression makes the stage movesets a chain under
	// inclusion. Whether the REACHABLE sets grow with it is a hypothesis
	// about the geometry, not a law, and this is the cheap check that it
	// holds for one fixture rather than an assumption nobody looked at.
	profile := testProfile()
	base := buildGraph(t, shaftRoom(), profile, 0)
	richer := buildGraph(t, shaftRoom(), profile, NewAbilitySet(AbilityWallJump))

	reach := func(result GraphResult, abilities AbilitySet) map[string]bool {
		graph := &result.Graph
		start := nodeAt(t, graph, 2.4, 1)
		seen := map[string]bool{}
		queue := []MotionNodeID{start.ID}
		visited := make([]bool, len(graph.Nodes))
		visited[start.ID] = true
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			node := graph.Nodes[current]
			seen[node.Mode.String()+Point(node.Height).String()+node.Footing.String()] = true
			for _, edge := range graph.OutEdges(current) {
				if !edge.Reachable(abilities) || visited[edge.To] {
					continue
				}
				visited[edge.To] = true
				queue = append(queue, edge.To)
			}
		}
		return seen
	}
	small := reach(base, 0)
	large := reach(richer, NewAbilitySet(AbilityWallJump))
	for key := range small {
		if !large[key] {
			t.Fatalf("the wall jump removed the reachable state %s; monotonicity is a hypothesis and this fixture breaks it", key)
		}
	}
	if len(large) <= len(small) {
		t.Fatalf("the wall jump must open states: %d with, %d without", len(large), len(small))
	}
}

// --- cost -----------------------------------------------------------------

// exampleRoom is a room of the shape the generator will actually produce:
// 48 by 24, with a floor, five platforms, a one-way platform, a chimney, a
// ladder and a spike pit.
func exampleRoom() []string {
	return []string{
		"################################################",
		"#..............................................#",
		"#..............................................#",
		"#.....####.....................####............#",
		"#..............................................#",
		"#...................H..........................#",
		"#..........=====....H.................####.....#",
		"#...................H..........................#",
		"#...................H..........................#",
		"#####...............H..........................#",
		"#...................H.......####...............#",
		"#...................H..........................#",
		"#...........####....H..........................#",
		"#...................H..........................#",
		"#..............................................#",
		"#.......................................####...#",
		"#..............................................#",
		"#...####.......................................#",
		"#..............................................#",
		"#..............................................#",
		"#.........................####.................#",
		"#..............................................#",
		"#.............^^^^^^...........................#",
		"################################################",
	}
}

// TestTheCostOfBuildingAGraphIsReported measures one build of the example
// room and prints what it spent. The numbers are in the delivery report; the
// assertion is only that the build completes inside the default budget, so
// the test does not fail on a slower machine.
func TestTheCostOfBuildingAGraphIsReported(t *testing.T) {
	profile := testProfile()
	abilities := NewAbilitySet(AbilityDash, AbilityDoubleJump, AbilityWallJump, AbilityClimb)
	budget := DefaultSearchBudget()
	budget.LaunchResolution = 1.0
	budget.TimeResolution = 0.04

	result, err := NewM1Oracle().BuildGraph(context.Background(), GraphQuery{
		Grid:      roomFromRows(t, exampleRoom()),
		Profile:   profile,
		Abilities: abilities,
		Budget:    budget,
	})
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	t.Logf("surfaces=%d nodes=%d edges=%d", len(result.Graph.Surfaces), len(result.Graph.Nodes), len(result.Graph.Edges))
	t.Logf("verdict=%s/%s budget=%+v", result.Judgement.Verdict, result.Judgement.Reason, result.Judgement.Budget)
	if !result.Judgement.Certified() {
		t.Fatalf("the example room must fit inside the default budget: %s/%s",
			result.Judgement.Verdict, result.Judgement.Reason)
	}
	for _, edge := range result.Graph.Edges {
		replayWitness(t, &result.Graph, edge, profile)
	}
}

// BenchmarkBuildGraphExampleRoom is the wall-clock figure quoted in the
// delivery report. It is a benchmark and not an assertion, because a time
// limit in a test makes the suite depend on the machine.
func BenchmarkBuildGraphExampleRoom(b *testing.B) {
	profile := DefaultProfile()
	abilities := NewAbilitySet(AbilityDash, AbilityDoubleJump, AbilityWallJump, AbilityClimb)
	budget := DefaultSearchBudget()
	budget.LaunchResolution = 1.0
	budget.TimeResolution = 0.04
	rows := exampleRoom()
	width := len(rows[0])
	cells := make([]CellKind, 0, width*len(rows))
	for _, row := range rows {
		for _, symbol := range row {
			switch symbol {
			case '#':
				cells = append(cells, CellKindSolid)
			case '=':
				cells = append(cells, CellKindSemiSolid)
			case '^':
				cells = append(cells, CellKindHazard)
			case 'H':
				cells = append(cells, CellKindClimbable)
			default:
				cells = append(cells, CellKindEmpty)
			}
		}
	}
	grid := Grid{Width: uint32(width), Height: uint32(len(rows)), Cells: cells}
	query := GraphQuery{Grid: grid, Profile: profile, Abilities: abilities, Budget: budget}
	oracle := NewM1Oracle()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := oracle.BuildGraph(context.Background(), query); err != nil {
			b.Fatalf("BuildGraph: %v", err)
		}
	}
}
