package platform

import (
	"context"
	"math"
	"testing"
)

// Every test in this file is a NEGATIVE case with its positive control beside
// it. A rejection nobody has watched happen proves nothing, so each fixture
// comes in two shapes that differ in exactly one thing — a ceiling, a
// platform's width, a refill rule, a tenth of a cell of jump velocity — and
// the test asserts that the single difference is what flips the verdict.

// nodeAt returns the graph node standing on a surface at a foot position.
func nodeAt(t *testing.T, graph *JumpGraph, x, height float64) MotionNode {
	t.Helper()
	best := -1
	distance := math.Inf(1)
	for i, node := range graph.Nodes {
		if node.Mode != MotionModeGrounded || math.Abs(node.Height-height) > 1e-9 {
			continue
		}
		if d := math.Abs(node.Footing.Lo - x); d < distance {
			best, distance = i, d
		}
	}
	if best < 0 {
		t.Fatalf("no grounded node at height %v; the graph has %d nodes", height, len(graph.Nodes))
	}
	return graph.Nodes[best]
}

func buildGraph(t *testing.T, rows []string, profile MovementProfile, abilities AbilitySet) GraphResult {
	t.Helper()
	result, err := NewM1Oracle().BuildGraph(context.Background(), GraphQuery{
		Grid:      roomFromRows(t, rows),
		Profile:   profile,
		Abilities: abilities,
		Budget:    testBudget(),
	})
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	return result
}

func checkEdge(t *testing.T, rows []string, profile MovementProfile, abilities AbilitySet, from, to MotionNode) EdgeResult {
	t.Helper()
	result, err := NewM1Oracle().CheckEdge(context.Background(), EdgeQuery{
		Grid:                roomFromRows(t, rows),
		Profile:             profile,
		Abilities:           abilities,
		From:                from,
		To:                  to,
		AnyArrivalResources: true,
		Budget:              testBudget(),
	})
	if err != nil {
		t.Fatalf("CheckEdge: %v", err)
	}
	if err := result.Judgement.Validate(); err != nil {
		t.Fatalf("CheckEdge produced a malformed judgement: %v", err)
	}
	if result.Judgement.Model != ModelM1 {
		t.Fatalf("judgement must name the model that produced it, got %q", result.Judgement.Model)
	}
	return result
}

// hasEdge reports whether the graph holds an edge between two nodes.
func hasEdge(graph *JumpGraph, from, to MotionNodeID) bool {
	for _, edge := range graph.Edges {
		if edge.From == from && edge.To == to {
			return true
		}
	}
	return false
}

// --- 1. the jump that grazes a ceiling -----------------------------------

// gapRoom is a four-cell pit between two platforms, with an optional ceiling
// over the pit. The pit reaches the room's floor, which is solid rock, so
// falling in is not a route.
func gapRoom(ceiling bool) []string {
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
		"................",
		"######....######",
	}
	if ceiling {
		rows[6] = "......####......"
	}
	return rows
}

func TestACeilingOverThePitRejectsTheJumpThatWouldOtherwiseClearIt(t *testing.T) {
	profile := testProfile()

	open := buildGraph(t, gapRoom(false), profile, 0)
	left := nodeAt(t, &open.Graph, 5.6, 1)
	right := nodeAt(t, &open.Graph, 10.4, 1)

	clear := checkEdge(t, gapRoom(false), profile, 0, left, right)
	if !clear.Judgement.Certified() {
		t.Fatalf("with no ceiling the pit is jumpable: got %s/%s (%s)",
			clear.Judgement.Verdict, clear.Judgement.Reason, clear.Judgement.Detail)
	}
	if clear.Edge.Witness == nil || len(clear.Edge.Witness.Phases) == 0 {
		t.Fatalf("a certified edge must carry its witness")
	}

	// The same two platforms, the same profile, one row of rock over the pit.
	// The nodes are taken from the ceiling room's own graph, because adding a
	// block adds a surface and every SurfaceID after it shifts.
	shut := buildGraph(t, gapRoom(true), profile, 0)
	blocked := checkEdge(t, gapRoom(true), profile, 0,
		nodeAt(t, &shut.Graph, 5.6, 1), nodeAt(t, &shut.Graph, 10.4, 1))
	if blocked.Judgement.Verdict != VerdictRejected {
		t.Fatalf("the ceiling must reject the jump, got %s/%s (%s)",
			blocked.Judgement.Verdict, blocked.Judgement.Reason, blocked.Judgement.Detail)
	}
	if blocked.Judgement.Reason != ReasonObstructed {
		t.Fatalf("the rejection is an obstruction, got %s", blocked.Judgement.Reason)
	}
	if blocked.Edge.Witness != nil {
		t.Fatalf("a rejected edge carries no witness")
	}
}

func TestTheCeilingAlsoRemovesTheEdgeFromTheGraph(t *testing.T) {
	profile := testProfile()
	open := buildGraph(t, gapRoom(false), profile, 0)
	shut := buildGraph(t, gapRoom(true), profile, 0)

	leftOpen := nodeAt(t, &open.Graph, 5.6, 1)
	rightOpen := nodeAt(t, &open.Graph, 10.4, 1)
	if _, err := NewM1Oracle().FindRoute(context.Background(), RouteQuery{
		Graph: &open.Graph, From: leftOpen.ID, To: rightOpen.ID,
	}); err != nil {
		t.Fatalf("FindRoute: %v", err)
	}
	crossed := false
	for _, edge := range open.Graph.Edges {
		if open.Graph.Nodes[edge.From].Footing.Lo <= 5.6 && open.Graph.Nodes[edge.To].Footing.Lo >= 10.4 {
			crossed = true
		}
	}
	if !crossed {
		t.Fatalf("the open pit must be crossed by at least one edge")
	}
	for _, edge := range shut.Graph.Edges {
		if shut.Graph.Nodes[edge.From].Footing.Lo <= 5.6 && shut.Graph.Nodes[edge.To].Footing.Lo >= 10.4 {
			t.Fatalf("edge %d crosses a pit the ceiling seals: %s from %v to %v", edge.ID, edge.Kind,
				shut.Graph.Nodes[edge.From].Footing, shut.Graph.Nodes[edge.To].Footing)
		}
	}
}

// --- 2. two jumps that are each possible and do not compose --------------

// stepRoom is A, a landing platform, and C, all at the same height. width is
// the landing platform's width in cells.
func stepRoom(middleWidth int) []string {
	const height, columns = 12, 19
	rows := make([]string, height)
	for i := range rows {
		rows[i] = ""
		for c := 0; c < columns; c++ {
			rows[i] += "."
		}
	}
	floor := make([]byte, columns)
	for c := range floor {
		floor[c] = '.'
	}
	mark := func(from, to int) {
		for c := from; c <= to; c++ {
			floor[c] = '#'
		}
	}
	mark(0, 5)
	mark(9, 9+middleWidth-1)
	mark(13, 18)
	rows[height-1] = string(floor)
	return rows
}

// weakBrakeProfile is a character who cannot stop on a coin. Braking,
// GroundAccel and AirAccel are not interchangeable, and this is the one the
// composition of two jumps depends on.
func weakBrakeProfile() MovementProfile {
	profile := testProfile()
	profile.Braking = 20
	return profile
}

func TestTwoJumpsThatAreEachPossibleDoNotComposeOverAPlatformTooShortToStopOn(t *testing.T) {
	profile := weakBrakeProfile()

	narrow := buildGraph(t, stepRoom(1), profile, 0)
	a := nodeAt(t, &narrow.Graph, 5.6, 1)
	b := nodeAt(t, &narrow.Graph, 9.4, 1)
	c := nodeAt(t, &narrow.Graph, 13.4, 1)

	// The second jump exists on its own.
	second := checkEdge(t, stepRoom(1), profile, 0, nodeAt(t, &narrow.Graph, 9.6, 1), c)
	if !second.Judgement.Certified() {
		t.Fatalf("B to C must be possible on its own: %s/%s (%s)",
			second.Judgement.Verdict, second.Judgement.Reason, second.Judgement.Detail)
	}

	// The first one is not, because arriving at speed on a platform 0.2 cells
	// wide is not the same as standing on it.
	first := checkEdge(t, stepRoom(1), profile, 0, a, b)
	if first.Judgement.Verdict != VerdictRejected {
		t.Fatalf("A to B must be rejected: arriving is not standing. got %s/%s (%s)",
			first.Judgement.Verdict, first.Judgement.Reason, first.Judgement.Detail)
	}
	if first.Judgement.Reason != ReasonLandingUnsupported {
		t.Fatalf("the rejection is about the landing, got %s", first.Judgement.Reason)
	}

	if hasEdge(&narrow.Graph, a.ID, b.ID) {
		t.Fatalf("the graph must hold no edge into a platform the character cannot stop on")
	}
	route, err := NewM1Oracle().FindRoute(context.Background(), RouteQuery{
		Graph: &narrow.Graph, From: a.ID, To: c.ID,
	})
	if err != nil {
		t.Fatalf("FindRoute: %v", err)
	}
	if route.Judgement.Verdict != VerdictRejected || route.Judgement.Reason != ReasonDisconnected {
		t.Fatalf("A must not reach C: got %s/%s", route.Judgement.Verdict, route.Judgement.Reason)
	}
}

func TestWideningThePlatformRestoresTheComposition(t *testing.T) {
	profile := weakBrakeProfile()
	wide := buildGraph(t, stepRoom(3), profile, 0)
	a := nodeAt(t, &wide.Graph, 5.6, 1)
	c := nodeAt(t, &wide.Graph, 13.4, 1)

	route, err := NewM1Oracle().FindRoute(context.Background(), RouteQuery{
		Graph: &wide.Graph, From: a.ID, To: c.ID,
	})
	if err != nil {
		t.Fatalf("FindRoute: %v", err)
	}
	if !route.Judgement.Certified() {
		t.Fatalf("a platform wide enough to brake on restores the route: %s/%s",
			route.Judgement.Verdict, route.Judgement.Reason)
	}
	if len(route.Route.Edges) == 0 {
		t.Fatalf("a certified route between distinct nodes has edges")
	}
}

// --- 3. a dash spent with no way to recharge it --------------------------

// dashRoom is three platforms separated by gaps no ordinary jump crosses.
func dashRoom() []string {
	const height, columns = 12, 32
	rows := make([]string, height)
	for i := range rows {
		blank := make([]byte, columns)
		for c := range blank {
			blank[c] = '.'
		}
		rows[i] = string(blank)
	}
	floor := make([]byte, columns)
	for c := range floor {
		floor[c] = '.'
	}
	for _, run := range [][2]int{{0, 5}, {13, 18}, {26, 31}} {
		for c := run[0]; c <= run[1]; c++ {
			floor[c] = '#'
		}
	}
	rows[height-1] = string(floor)
	return rows
}

// wallRefillOnlyProfile recharges the dash on wall contact and nowhere else,
// which is a legitimate profile and the one that makes a spent dash a real
// state rather than a bookkeeping detail.
func wallRefillOnlyProfile() MovementProfile {
	profile := testProfile()
	dash := *profile.Dash
	dash.RefillOn = RefillOnWallCling
	profile.Dash = &dash
	jump := *profile.DoubleJump
	jump.RefillOn = RefillOnGround
	profile.DoubleJump = &jump
	return profile
}

func TestADashSpentWithNoRefillDoesNotCrossTheSecondGap(t *testing.T) {
	profile := wallRefillOnlyProfile()
	abilities := NewAbilitySet(AbilityDash)
	result := buildGraph(t, dashRoom(), profile, abilities)
	graph := &result.Graph

	a := nodeAt(t, graph, 5.6, 1)
	c := nodeAt(t, graph, 26.4, 1)

	// The first gap is crossed, and the edge says it costs the dash.
	crossed, spentNode := findFirstDashCrossing(t, graph)
	if !crossed {
		t.Fatalf("the dash must cross the first gap")
	}
	if got := graph.Nodes[spentNode].Resources.DashCharges; got != 0 {
		t.Fatalf("landing with no ground refill leaves the dash spent, got %d charges", got)
	}
	if graph.Nodes[spentNode].IsRest(profile) {
		t.Fatalf("a state with a spent dash is not a rest state, and collapsing it is how a graph lies")
	}

	// The second gap is not, because there is no wall to recharge against.
	// The builder seeds every platform with a rested state, so the middle
	// platform also carries a node that still has its dash; what must not
	// exist is an edge leaving the SPENT one.
	checkSpentDashCannotCrossSecondGap(t, graph)
	route, err := NewM1Oracle().FindRoute(context.Background(), RouteQuery{
		Graph: graph, From: a.ID, To: c.ID, Abilities: abilities,
	})
	if err != nil {
		t.Fatalf("FindRoute: %v", err)
	}
	if route.Judgement.Verdict != VerdictRejected || route.Judgement.Reason != ReasonDisconnected {
		t.Fatalf("A must not reach C on one dash: got %s/%s", route.Judgement.Verdict, route.Judgement.Reason)
	}

	// Asked directly, the oracle names the right reason.
	direct := checkEdge(t, dashRoom(), profile, abilities, graph.Nodes[spentNode], c)
	if direct.Judgement.Verdict != VerdictRejected || direct.Judgement.Reason != ReasonResourceExhausted {
		t.Fatalf("the rejection must name the spent charge: got %s/%s (%s)",
			direct.Judgement.Verdict, direct.Judgement.Reason, direct.Judgement.Detail)
	}
}

func findFirstDashCrossing(t *testing.T, graph *JumpGraph) (bool, MotionNodeID) {
	crossed := false
	var spentNode MotionNodeID
	for _, edge := range graph.Edges {
		if edge.Kind != MotionEdgeKindDash {
			continue
		}
		if graph.Nodes[edge.From].Footing.Lo <= 5.6 && graph.Nodes[edge.To].Footing.Lo >= 13.4 {
			crossed = true
			spentNode = edge.To
			if !edge.Requires.Has(AbilityDash) {
				t.Fatalf("a dash edge must declare the ability it needs, got %s", edge.Requires)
			}
		}
	}
	return crossed, spentNode
}

func checkSpentDashCannotCrossSecondGap(t *testing.T, graph *JumpGraph) {
	for _, edge := range graph.Edges {
		if graph.Nodes[edge.From].Resources.DashCharges != 0 {
			continue
		}
		if graph.Nodes[edge.From].Footing.Lo >= 13.4 && graph.Nodes[edge.From].Footing.Lo <= 18.6 &&
			graph.Nodes[edge.To].Footing.Lo >= 26.4 {
			t.Fatalf("edge %d crosses the second gap with a dash that was already spent", edge.ID)
		}
	}
}

func TestRefillingTheDashOnTheGroundRestoresTheSecondGap(t *testing.T) {
	profile := testProfile() // the default refills on the ground
	abilities := NewAbilitySet(AbilityDash)
	result := buildGraph(t, dashRoom(), profile, abilities)
	graph := &result.Graph

	a := nodeAt(t, graph, 5.6, 1)
	c := nodeAt(t, graph, 26.4, 1)
	route, err := NewM1Oracle().FindRoute(context.Background(), RouteQuery{
		Graph: graph, From: a.ID, To: c.ID, Abilities: abilities,
	})
	if err != nil {
		t.Fatalf("FindRoute: %v", err)
	}
	if !route.Judgement.Certified() {
		t.Fatalf("with a ground refill the route exists: %s/%s", route.Judgement.Verdict, route.Judgement.Reason)
	}
	if !route.Route.Requires.Has(AbilityDash) {
		t.Fatalf("the route must declare the dash it uses, got %s", route.Route.Requires)
	}
}

// --- 4. the tangent contact at the apex ----------------------------------

// apexRoom is a floor with a one-way platform four cells above it: exactly
// the default profile's apex.
func apexRoom() []string {
	return []string{
		"........",
		"........",
		"........",
		"........",
		"........",
		"........",
		"........",
		"..====..",
		"........",
		"........",
		"........",
		"########",
	}
}

func TestATangentContactAtTheApexIsNotALanding(t *testing.T) {
	// The one-way platform sits at world height 5 and the floor at 1. The
	// default profile's apex is exactly 4, so the feet reach the platform
	// with zero vertical velocity and stay there for an instant. Accepting
	// that contact would certify a jump with no margin at all.
	profile := testProfile()
	if got := profile.ApexHeight(); math.Abs(got-4) > 1e-12 {
		t.Fatalf("this fixture is built on an apex of exactly 4, got %v", got)
	}

	plan := newLaunch()
	plan.x0, plan.y0 = 4, 1
	plan.vy = profile.JumpVelocity
	plan.mode = MotionModeAirborne
	ceiling := flightCeiling(profile, testBudget())
	if _, ok := descentTime(profile, 0, plan, 5, ceiling); ok {
		t.Fatalf("reaching a height exactly at the apex is not a descent through it")
	}
	// A hair more jump velocity and the same height IS crossed descending.
	raised := profile
	raised.JumpVelocity = 20.5
	plan.vy = raised.JumpVelocity
	if _, ok := descentTime(raised, 0, plan, 5, ceiling); !ok {
		t.Fatalf("an apex above the target must produce a descending crossing")
	}

	graph := buildGraph(t, apexRoom(), profile, 0)
	for _, edge := range graph.Graph.Edges {
		if math.Abs(graph.Graph.Nodes[edge.From].Height-1) > 1e-9 {
			continue
		}
		if math.Abs(graph.Graph.Nodes[edge.To].Height-5) < 1e-9 {
			t.Fatalf("edge %d lands on a platform the jump only touches tangentially", edge.ID)
		}
	}
}

func TestAnApexAboveThePlatformLandsOnIt(t *testing.T) {
	profile := testProfile()
	profile.JumpVelocity = 20.5
	if profile.DoubleJump != nil {
		jump := *profile.DoubleJump
		jump.Velocity = 20.5
		profile.DoubleJump = &jump
	}
	graph := buildGraph(t, apexRoom(), profile, 0)
	landed := false
	for _, edge := range graph.Graph.Edges {
		if math.Abs(graph.Graph.Nodes[edge.From].Height-1) > 1e-9 {
			continue
		}
		if math.Abs(graph.Graph.Nodes[edge.To].Height-5) < 1e-9 {
			landed = true
		}
	}
	if !landed {
		t.Fatalf("an apex of %v clears a platform at 4 above and must land on it", profile.ApexHeight())
	}
}

// --- 5. a one-way platform is not a block on the way up ------------------

func oneWayRoom() []string {
	return []string{
		"..........",
		"..........",
		"..........",
		"..........",
		"..........",
		"..........",
		"..........",
		"..........",
		"..........",
		"==========",
		"..........",
		"##########",
	}
}

func TestASemiSolidIsCrossedFromBelowAndLandedOnFromAbove(t *testing.T) {
	// The one-way platform is two cells above the floor. Expanded as a solid
	// block it would forbid the rise entirely, because the body's forbidden
	// band begins 1.65 cells below its underside and the feet enter that band
	// almost immediately.
	profile := testProfile()
	graph := buildGraph(t, oneWayRoom(), profile, 0)

	floor := nodeAt(t, &graph.Graph, 4, 1)
	above := nodeAt(t, &graph.Graph, 4, 3)
	if above.Height != 3 {
		t.Fatalf("the one-way platform stands at world height 3, got %v", above.Height)
	}
	result := checkEdge(t, oneWayRoom(), profile, 0, floor, above)
	if !result.Judgement.Certified() {
		t.Fatalf("a jump from under a one-way platform must land on it: %s/%s (%s)",
			result.Judgement.Verdict, result.Judgement.Reason, result.Judgement.Detail)
	}
	rising := false
	for _, phase := range result.Edge.Witness.Phases {
		if phase.Start.Y < 3 && phase.End.Y > 3 {
			rising = true
		}
	}
	if !rising {
		t.Fatalf("the witness must show the body passing up THROUGH the platform: %v", result.Edge.Witness.Phases)
	}

	// Coming back down the same platform is a landing, and leaving it
	// downward needs the declared drop-through.
	drop := false
	for _, edge := range graph.Graph.Edges {
		if edge.Kind == MotionEdgeKindDropThrough && graph.Graph.Nodes[edge.From].Height == 3 {
			drop = true
		}
	}
	if !drop {
		t.Fatalf("a one-way platform must offer a drop-through downward")
	}
}

// --- 6. a parabola never crosses a platform in silence -------------------

// crossingRoom puts a wide one-way platform between two floors, at a height
// every arc that would reach the far floor must descend through.
func crossingRoom(middle bool) []string {
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
		"................",
		"######....######",
	}
	if middle {
		rows[9] = ".......======..."
	}
	return rows
}

func TestAnIntermediateLandingEndsTheArcInsteadOfBeingCrossedInSilence(t *testing.T) {
	profile := testProfile()

	withoutMiddle := buildGraph(t, crossingRoom(false), profile, 0)
	left := nodeAt(t, &withoutMiddle.Graph, 5.6, 1)
	right := nodeAt(t, &withoutMiddle.Graph, 10.4, 1)
	if !checkEdge(t, crossingRoom(false), profile, 0, left, right).Judgement.Certified() {
		t.Fatalf("the bare pit is jumpable, which is what makes the next assertion mean something")
	}

	withMiddle := buildGraph(t, crossingRoom(true), profile, 0)
	leftTwo := nodeAt(t, &withMiddle.Graph, 5.6, 1)
	rightTwo := nodeAt(t, &withMiddle.Graph, 10.4, 1)
	middle := nodeAt(t, &withMiddle.Graph, 7.4, 3)
	if middle.Height != 3 {
		t.Fatalf("the intermediate platform stands at world height 3, got %v", middle.Height)
	}

	direct := checkEdge(t, crossingRoom(true), profile, 0, leftTwo, rightTwo)
	if direct.Judgement.Certified() {
		t.Fatalf("an arc that descends through the middle platform lands on it; it must not reach the far floor in one manoeuvre")
	}
	onto := checkEdge(t, crossingRoom(true), profile, 0, leftTwo, middle)
	if !onto.Judgement.Certified() {
		t.Fatalf("the arc must land on the platform it crosses: %s/%s (%s)",
			onto.Judgement.Verdict, onto.Judgement.Reason, onto.Judgement.Detail)
	}
	if hasEdge(&withMiddle.Graph, leftTwo.ID, rightTwo.ID) {
		t.Fatalf("the graph must not hold the silent crossing either")
	}
	// Two manoeuvres still get there, which is the point: the arc was split,
	// not forbidden.
	route, err := NewM1Oracle().FindRoute(context.Background(), RouteQuery{
		Graph: &withMiddle.Graph, From: leftTwo.ID, To: rightTwo.ID,
	})
	if err != nil {
		t.Fatalf("FindRoute: %v", err)
	}
	if !route.Judgement.Certified() {
		t.Fatalf("the far floor is still reachable in two manoeuvres: %s/%s",
			route.Judgement.Verdict, route.Judgement.Reason)
	}
	if len(route.Route.Edges) < 2 {
		t.Fatalf("the route must take at least two manoeuvres, got %d", len(route.Route.Edges))
	}
}

// --- budget, abilities and the three-valued verdict ----------------------

func TestABudgetCeilingIsUnknownAndNeverARejection(t *testing.T) {
	profile := testProfile()
	budget := testBudget()
	budget.MaxCandidateEdges = 3

	result, err := NewM1Oracle().CheckEdge(context.Background(), EdgeQuery{
		Grid:        roomFromRows(t, gapRoom(true)),
		Profile:     profile,
		From:        MotionNode{Surface: 0, Interval: 0, Height: 1, Footing: Point(5.6), Velocity: Point(0), Mode: MotionModeGrounded, Resources: profile.FullResources()},
		To:          MotionNode{Surface: 1, Interval: 0, Height: 1, Footing: Point(10.4), Velocity: Point(0), Mode: MotionModeGrounded, Resources: profile.FullResources()},
		Budget:      budget,
		OmitWitness: true,
	})
	if err != nil {
		t.Fatalf("CheckEdge: %v", err)
	}
	if result.Judgement.Verdict != VerdictUnknown {
		t.Fatalf("a ceiling never rejects: got %s/%s", result.Judgement.Verdict, result.Judgement.Reason)
	}
	if result.Judgement.Reason != ReasonBudgetExhausted {
		t.Fatalf("the reason must be the budget, got %s", result.Judgement.Reason)
	}
	if !result.Judgement.Budget.Exhausted {
		t.Fatalf("the report must say the budget ran out")
	}
	if err := result.Judgement.Validate(); err != nil {
		t.Fatalf("judgement must be internally consistent: %v", err)
	}
}

func TestABuildThatRanOutStillReturnsItsPartialGraph(t *testing.T) {
	profile := testProfile()
	budget := testBudget()
	budget.MaxCandidateEdges = 40

	result, err := NewM1Oracle().BuildGraph(context.Background(), GraphQuery{
		Grid:    roomFromRows(t, gapRoom(false)),
		Profile: profile,
		Budget:  budget,
	})
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	if result.Judgement.Verdict != VerdictUnknown || result.Judgement.Reason != ReasonBudgetExhausted {
		t.Fatalf("a truncated build is unknown, got %s/%s", result.Judgement.Verdict, result.Judgement.Reason)
	}
	if len(result.Graph.Nodes) == 0 {
		t.Fatalf("the partial graph is real and must come back")
	}
	for _, edge := range result.Graph.Edges {
		if edge.Kind == MotionEdgeKindUnspecified {
			t.Fatalf("every edge of a partial graph is still a real edge")
		}
	}
}

func TestAWideDepartureFootingIsNeverCertified(t *testing.T) {
	// The departure is quantified universally, and a continuum cannot be
	// covered by a finite sample. A node wider than a point can therefore be
	// rejected by a counterexample but never certified.
	profile := testProfile()
	graph := buildGraph(t, gapRoom(false), profile, 0)
	to := nodeAt(t, &graph.Graph, 10.4, 1)
	from := nodeAt(t, &graph.Graph, 5.6, 1)
	from.Footing = Span{Lo: 4.0, Hi: 5.6}

	result := checkEdge(t, gapRoom(false), profile, 0, from, to)
	if result.Judgement.Verdict == VerdictCertified {
		t.Fatalf("a wide departure must not be certified: %s/%s", result.Judgement.Verdict, result.Judgement.Reason)
	}
}

func TestAMissingAbilityIsRejectedAndNamed(t *testing.T) {
	profile := testProfile()
	graph := buildGraph(t, dashRoom(), profile, NewAbilitySet(AbilityDash))
	a := nodeAt(t, &graph.Graph, 5.6, 1)
	b := nodeAt(t, &graph.Graph, 13.4, 1)

	with := checkEdge(t, dashRoom(), profile, NewAbilitySet(AbilityDash), a, b)
	if !with.Judgement.Certified() {
		t.Fatalf("the dash crosses the gap: %s/%s (%s)", with.Judgement.Verdict, with.Judgement.Reason, with.Judgement.Detail)
	}
	if !with.Edge.Requires.Has(AbilityDash) {
		t.Fatalf("the edge must name the ability it needs, got %s", with.Edge.Requires)
	}
	without := checkEdge(t, dashRoom(), profile, 0, a, b)
	if without.Judgement.Verdict != VerdictRejected {
		t.Fatalf("without the dash the gap is not crossed: %s/%s", without.Judgement.Verdict, without.Judgement.Reason)
	}
}

func TestTheOracleIsDeterministic(t *testing.T) {
	profile := testProfile()
	first := buildGraph(t, crossingRoom(true), profile, NewAbilitySet(AbilityDash, AbilityDoubleJump))
	second := buildGraph(t, crossingRoom(true), profile, NewAbilitySet(AbilityDash, AbilityDoubleJump))
	if len(first.Graph.Nodes) != len(second.Graph.Nodes) || len(first.Graph.Edges) != len(second.Graph.Edges) {
		t.Fatalf("two builds of one room must agree: %d/%d nodes, %d/%d edges",
			len(first.Graph.Nodes), len(second.Graph.Nodes), len(first.Graph.Edges), len(second.Graph.Edges))
	}
	for i := range first.Graph.Edges {
		a, b := first.Graph.Edges[i], second.Graph.Edges[i]
		if a.From != b.From || a.To != b.To || a.Kind != b.Kind || a.Requires != b.Requires {
			t.Fatalf("edge %d differs between builds: %+v vs %+v", i, a, b)
		}
	}
}

func TestCancellationIsAnErrorAndNeverAVerdict(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	profile := testProfile()
	if _, err := NewM1Oracle().BuildGraph(ctx, GraphQuery{
		Grid: roomFromRows(t, gapRoom(false)), Profile: profile, Budget: testBudget(),
	}); err == nil {
		t.Fatalf("a cancelled build returns an error, not a verdict")
	}
}

func TestAnUnknownCellKindIsRejectedAndNeverDefaulted(t *testing.T) {
	grid := roomFromRows(t, gapRoom(false))
	grid.Cells[0] = CellKind(99)
	if _, err := NewM1Oracle().Surfaces(context.Background(), SurfaceQuery{Grid: grid, Profile: testProfile()}); err == nil {
		t.Fatalf("an unknown kind must be an error, never air")
	}
}
