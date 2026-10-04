package platform

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// fixtureGrid builds an 8x6 room: a full floor along the bottom row and a
// three-cell ledge two cells above it, on the right.
//
//	row 0  . . . . . . . .
//	row 1  . . . . . . . .
//	row 2  . . . . . . . .
//	row 3  . . . . . # # #
//	row 4  . . . . . . . .
//	row 5  # # # # # # # #
func fixtureGrid() Grid {
	const width, height = 8, 6
	cells := make([]CellKind, width*height)
	for x := 0; x < width; x++ {
		cells[5*width+x] = CellKindSolid
	}
	for x := 5; x < width; x++ {
		cells[3*width+x] = CellKindSolid
	}
	return Grid{Width: width, Height: height, Cells: cells}
}

func TestFakeOracleSatisfiesTheContract(t *testing.T) {
	var oracle Oracle = &FakeOracle{}
	if oracle.Model() != ModelFake {
		t.Fatalf("Model() = %q, want %q", oracle.Model(), ModelFake)
	}
	// The narrow interfaces are the dependency a single-manoeuvre consumer
	// should be able to take.
	var _ EdgeChecker = oracle
	var _ GraphBuilder = oracle
	var _ RouteFinder = oracle
	var _ SurfaceDeriver = oracle
}

func TestFakeOracleDerivesSurfaces(t *testing.T) {
	fake := &FakeOracle{}
	profile := DefaultProfile()
	result, err := fake.Surfaces(context.Background(), SurfaceQuery{Grid: fixtureGrid(), Room: 3, Profile: profile})
	if err != nil {
		t.Fatalf("Surfaces() failed: %v", err)
	}
	if len(result.Surfaces) != 2 {
		t.Fatalf("derived %d surfaces, want the floor and the ledge", len(result.Surfaces))
	}
	ledge, floor := result.Surfaces[0], result.Surfaces[1]
	if ledge.At != 3 {
		t.Fatalf("the ledge sits at world height %v, want 3", ledge.At)
	}
	if ledge.Extent != (Span{Lo: 5, Hi: 8}) {
		t.Fatalf("the ledge spans %v, want [5, 8]", ledge.Extent)
	}
	if floor.At != 1 {
		t.Fatalf("the floor sits at world height %v, want 1", floor.At)
	}
	if floor.Extent != (Span{Lo: 0, Hi: 8}) {
		t.Fatalf("the floor spans %v, want [0, 8]", floor.Extent)
	}
	for _, surface := range result.Surfaces {
		if surface.Room != 3 {
			t.Fatalf("surface %d carries room %d, want the queried room", surface.ID, surface.Room)
		}
		if len(surface.Intervals) != 1 {
			t.Fatalf("surface %d has %d intervals; the fake emits one", surface.ID, len(surface.Intervals))
		}
		want := profile.Footing(surface.Extent)
		if surface.Intervals[0].Footing != want {
			t.Fatalf("surface %d footing %v, want %v", surface.ID, surface.Intervals[0].Footing, want)
		}
	}
	if result.Judgement.Model != ModelFake {
		t.Fatalf("the judgement names %q, want the fake model", result.Judgement.Model)
	}
}

func TestFakeOracleRejectsMalformedGeometry(t *testing.T) {
	fake := &FakeOracle{}
	profile := DefaultProfile()
	cases := []struct {
		name     string
		grid     Grid
		sentinel error
	}{
		{name: "zero width", grid: Grid{Height: 2, Cells: make([]CellKind, 0)}, sentinel: ErrInvalidGeometry},
		{
			name:     "cell slice disagrees",
			grid:     Grid{Width: 2, Height: 2, Cells: make([]CellKind, 3)},
			sentinel: ErrInvalidGeometry,
		},
		{
			name:     "unknown cell kind",
			grid:     Grid{Width: 1, Height: 1, Cells: []CellKind{CellKind(42)}},
			sentinel: ErrInvalidGeometry,
		},
		{
			name:     "side above the ceiling",
			grid:     Grid{Width: MaxRoomSide + 1, Height: 1, Cells: make([]CellKind, MaxRoomSide+1)},
			sentinel: ErrLimitExceeded,
		},
		{
			name:     "terrain layer that does not match",
			grid:     Grid{Width: 2, Height: 1, Cells: make([]CellKind, 2), Terrain: &TerrainLayer{Indices: []byte{0}}},
			sentinel: ErrInvalidGeometry,
		},
		{
			name: "terrain index past the palette",
			grid: Grid{
				Width: 1, Height: 1, Cells: make([]CellKind, 1),
				Terrain: &TerrainLayer{Indices: []byte{3}},
			},
			sentinel: ErrInvalidGeometry,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := fake.Surfaces(context.Background(), SurfaceQuery{Grid: tc.grid, Profile: profile})
			if err == nil {
				t.Fatal("malformed geometry was accepted")
			}
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("error %v does not wrap %v", err, tc.sentinel)
			}
		})
	}
}

// TestUnknownCellKindIsRejectedNeverDefaulted is the explicit form of the
// wire risk: a semi-solid platform decoded as air leaves a map that still
// looks valid and has quietly lost a mechanic.
func TestUnknownCellKindIsRejectedNeverDefaulted(t *testing.T) {
	grid := Grid{Width: 1, Height: 1, Cells: []CellKind{CellKind(7)}}
	err := validateGrid(grid)
	if err == nil {
		t.Fatal("an unknown cell kind was accepted")
	}
	if !errors.Is(err, ErrInvalidGeometry) {
		t.Fatalf("error %v does not wrap ErrInvalidGeometry", err)
	}
	if kind, _ := grid.At(Cell{}); kind == CellKindEmpty {
		t.Fatal("lookup must not rewrite an unknown kind into air")
	}
}

func groundedNode(id MotionNodeID, surface SurfaceID, height float64, footing Span, profile MovementProfile) MotionNode {
	return MotionNode{
		ID:        id,
		Surface:   surface,
		Height:    height,
		Footing:   footing,
		Velocity:  Point(0),
		Mode:      MotionModeGrounded,
		Resources: profile.FullResources(),
	}
}

func TestFakeOracleCertifiesAJumpInsideTheEnvelope(t *testing.T) {
	profile := DefaultProfile()
	fake := &FakeOracle{}
	from := groundedNode(0, 0, 0, Span{Lo: 0, Hi: 1}, profile)
	to := groundedNode(1, 1, 2, Span{Lo: 4, Hi: 5}, profile)
	result, err := fake.CheckEdge(context.Background(), EdgeQuery{
		Grid: fixtureGrid(), Profile: profile, From: from, To: to,
	})
	if err != nil {
		t.Fatalf("CheckEdge() failed: %v", err)
	}
	if !result.Judgement.Certified() {
		t.Fatalf("judgement = %+v, want a certificate", result.Judgement)
	}
	if result.Edge.Kind != MotionEdgeKindJump {
		t.Fatalf("Kind = %s, want jump", result.Edge.Kind)
	}
	if result.Edge.Witness == nil {
		t.Fatal("a certified edge must carry a witness unless the query omits it")
	}
	if result.Edge.Requires != 0 {
		t.Fatalf("a plain jump requires %v, want the base moveset", result.Edge.Requires)
	}
	if err := result.Judgement.Validate(); err != nil {
		t.Fatalf("the fake produced a malformed judgement: %v", err)
	}
	if result.Judgement.Model != ModelFake {
		t.Fatal("every fake judgement must name the fake model, or a consumer cannot refuse it")
	}
}

func TestFakeOracleOmitsTheWitnessOnRequest(t *testing.T) {
	profile := DefaultProfile()
	fake := &FakeOracle{}
	result, err := fake.CheckEdge(context.Background(), EdgeQuery{
		Grid:    fixtureGrid(),
		Profile: profile,
		From:    groundedNode(0, 0, 0, Span{Lo: 0, Hi: 1}, profile),
		To:      groundedNode(1, 1, 0, Span{Lo: 4, Hi: 5}, profile),

		OmitWitness: true,
	})
	if err != nil {
		t.Fatalf("CheckEdge() failed: %v", err)
	}
	if !result.Judgement.Certified() {
		t.Fatalf("judgement = %+v, want a certificate", result.Judgement)
	}
	if result.Edge.Witness != nil {
		t.Fatal("the query asked for no witness")
	}
}

func TestFakeOracleRejectsBeyondTheEnvelope(t *testing.T) {
	profile := DefaultProfile()
	fake := &FakeOracle{}
	cases := []struct {
		name   string
		to     MotionNode
		reason VerdictReason
	}{
		{
			name:   "too far horizontally",
			to:     groundedNode(1, 1, 0, Span{Lo: 40, Hi: 41}, profile),
			reason: ReasonOutOfEnvelope,
		},
		{
			name:   "above the apex",
			to:     groundedNode(1, 1, profile.ApexHeight()+1, Span{Lo: 1, Hi: 2}, profile),
			reason: ReasonOutOfEnvelope,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := fake.CheckEdge(context.Background(), EdgeQuery{
				Grid:    fixtureGrid(),
				Profile: profile,
				From:    groundedNode(0, 0, 0, Span{Lo: 0, Hi: 1}, profile),
				To:      tc.to,
			})
			if err != nil {
				t.Fatalf("CheckEdge() failed: %v", err)
			}
			if result.Judgement.Verdict != VerdictRejected || result.Judgement.Reason != tc.reason {
				t.Fatalf("judgement = %+v, want a rejection for %s", result.Judgement, tc.reason)
			}
			if result.Edge.Witness != nil {
				t.Fatal("a rejection must not carry a witness")
			}
		})
	}
}

// TestFakeOracleRejectsAMissingAbility and the test after it are the two
// resource-aware rejections the contract has to be able to express, because
// an edge that ignores them is exactly the false route the design warns about.
func TestFakeOracleRejectsAMissingAbility(t *testing.T) {
	profile := DefaultProfile()
	fake := &FakeOracle{}
	result, err := fake.CheckEdge(context.Background(), EdgeQuery{
		Grid:      fixtureGrid(),
		Profile:   profile,
		Abilities: 0,
		Kind:      MotionEdgeKindDash,
		From:      groundedNode(0, 0, 0, Span{Lo: 0, Hi: 1}, profile),
		To:        groundedNode(1, 1, 0, Span{Lo: 2, Hi: 3}, profile),
	})
	if err != nil {
		t.Fatalf("CheckEdge() failed: %v", err)
	}
	if result.Judgement.Verdict != VerdictRejected || result.Judgement.Reason != ReasonAbilityMissing {
		t.Fatalf("judgement = %+v, want a rejection for a missing ability", result.Judgement)
	}

	// With the ability the same query is answered.
	result, err = fake.CheckEdge(context.Background(), EdgeQuery{
		Grid:      fixtureGrid(),
		Profile:   profile,
		Abilities: NewAbilitySet(AbilityDash),
		Kind:      MotionEdgeKindDash,
		From:      groundedNode(0, 0, 0, Span{Lo: 0, Hi: 1}, profile),
		To:        groundedNode(1, 1, 0, Span{Lo: 2, Hi: 3}, profile),
	})
	if err != nil {
		t.Fatalf("CheckEdge() failed: %v", err)
	}
	if !result.Judgement.Certified() {
		t.Fatalf("judgement = %+v, want a certificate once the ability is held", result.Judgement)
	}
	if result.Edge.Requires != NewAbilitySet(AbilityDash) {
		t.Fatalf("the edge requires %v, want dash; without this field progression needs a second graph", result.Edge.Requires)
	}
}

func TestFakeOracleRejectsASpentCharge(t *testing.T) {
	profile := DefaultProfile()
	fake := &FakeOracle{}
	spent := groundedNode(0, 0, 0, Span{Lo: 0, Hi: 1}, profile)
	spent.Resources.DashCharges = 0
	result, err := fake.CheckEdge(context.Background(), EdgeQuery{
		Grid:      fixtureGrid(),
		Profile:   profile,
		Abilities: NewAbilitySet(AbilityDash),
		Kind:      MotionEdgeKindDash,
		From:      spent,
		To:        groundedNode(1, 1, 0, Span{Lo: 2, Hi: 3}, profile),
	})
	if err != nil {
		t.Fatalf("CheckEdge() failed: %v", err)
	}
	if result.Judgement.Verdict != VerdictRejected || result.Judgement.Reason != ReasonResourceExhausted {
		t.Fatalf("judgement = %+v, want a rejection for a spent charge", result.Judgement)
	}

	// The same holds for an air jump, which is the other half of the "two
	// jumps that do not compose" problem.
	noAirJump := groundedNode(0, 0, 0, Span{Lo: 0, Hi: 1}, profile)
	noAirJump.Resources.AirJumps = 0
	result, err = fake.CheckEdge(context.Background(), EdgeQuery{
		Grid:      fixtureGrid(),
		Profile:   profile,
		Abilities: NewAbilitySet(AbilityDoubleJump),
		Kind:      MotionEdgeKindDoubleJump,
		From:      noAirJump,
		To:        groundedNode(1, 1, 5, Span{Lo: 1, Hi: 2}, profile),
	})
	if err != nil {
		t.Fatalf("CheckEdge() failed: %v", err)
	}
	if result.Judgement.Reason != ReasonResourceExhausted {
		t.Fatalf("judgement = %+v, want a rejection for a spent air jump", result.Judgement)
	}
}

// TestFakeOracleReportsUnsupportedManoeuvresAsUnknown is the shape of the
// three-valued verdict that matters most: not knowing is not rejecting.
func TestFakeOracleReportsUnsupportedManoeuvresAsUnknown(t *testing.T) {
	profile := DefaultProfile()
	fake := &FakeOracle{}
	for _, kind := range []MotionEdgeKind{
		MotionEdgeKindWallJump, MotionEdgeKindClimb, MotionEdgeKindDropThrough, MotionEdgeKindTransition,
	} {
		t.Run(kind.String(), func(t *testing.T) {
			result, err := fake.CheckEdge(context.Background(), EdgeQuery{
				Grid:      fixtureGrid(),
				Profile:   profile,
				Abilities: profile.Abilities(),
				Kind:      kind,
				From:      groundedNode(0, 0, 0, Span{Lo: 0, Hi: 1}, profile),
				To:        groundedNode(1, 1, 0, Span{Lo: 2, Hi: 3}, profile),
			})
			if err != nil {
				t.Fatalf("CheckEdge() failed: %v", err)
			}
			if result.Judgement.Verdict != VerdictUnknown {
				t.Fatalf("judgement = %+v, want unknown for a manoeuvre outside the model", result.Judgement)
			}
			if result.Judgement.Reason != ReasonUnsupportedMoveset {
				t.Fatalf("reason = %s, want unsupported-moveset", result.Judgement.Reason)
			}
		})
	}
}

func TestFakeOracleBuildsAGraphAndRoutesOverIt(t *testing.T) {
	profile := DefaultProfile()
	fake := &FakeOracle{}
	built, err := fake.BuildGraph(context.Background(), GraphQuery{
		Grid: fixtureGrid(), Room: 0, Profile: profile, Abilities: profile.Abilities(),
	})
	if err != nil {
		t.Fatalf("BuildGraph() failed: %v", err)
	}
	if !built.Judgement.Certified() {
		t.Fatalf("judgement = %+v, want a complete build", built.Judgement)
	}
	graph := built.Graph
	if len(graph.Nodes) != 2 {
		t.Fatalf("the graph has %d nodes, want one per surface interval", len(graph.Nodes))
	}
	if len(graph.Edges) == 0 {
		t.Fatal("the floor and the ledge are within the envelope of each other")
	}
	if graph.Model != ModelFake || graph.ProfileVersion != profile.Version {
		t.Fatalf("the graph does not name its provenance: %q / %q", graph.Model, graph.ProfileVersion)
	}
	if graph.Discipline != NodeDisciplineRefined {
		t.Fatalf("Discipline = %s, want the refined default", graph.Discipline)
	}
	checkFakeGraphNodesAndEdges(t, graph, profile)

	route, err := fake.FindRoute(context.Background(), RouteQuery{
		Graph: &graph, From: 1, To: 0, Abilities: profile.Abilities(),
	})
	if err != nil {
		t.Fatalf("FindRoute() failed: %v", err)
	}
	if !route.Judgement.Certified() {
		t.Fatalf("judgement = %+v, want a route from the floor to the ledge", route.Judgement)
	}
	if len(route.Route.Edges) == 0 {
		t.Fatal("a certified non-trivial route must list its edges")
	}
	if route.Route.From != 1 || route.Route.To != 0 {
		t.Fatalf("route runs %d -> %d, want 1 -> 0", route.Route.From, route.Route.To)
	}
}

func checkFakeGraphNodesAndEdges(t *testing.T, graph JumpGraph, profile MovementProfile) {
	t.Helper()
	for index, node := range graph.Nodes {
		if node.ID != MotionNodeID(index) {
			t.Fatalf("node %d carries ID %d; IDs are indices", index, node.ID)
		}
		if !node.IsRest(profile) {
			t.Fatalf("node %d is not a rest node, but the fake only builds those", index)
		}
	}
	for index, edge := range graph.Edges {
		if edge.ID != MotionEdgeID(index) {
			t.Fatalf("edge %d carries ID %d; IDs are indices", index, edge.ID)
		}
		if _, ok := graph.Node(edge.From); !ok {
			t.Fatalf("edge %d leaves a node that does not exist", index)
		}
		if _, ok := graph.Node(edge.To); !ok {
			t.Fatalf("edge %d enters a node that does not exist", index)
		}
	}
}

func TestFakeOracleRoutesTrivially(t *testing.T) {
	fake := &FakeOracle{}
	graph := &JumpGraph{Nodes: []MotionNode{{ID: 0}}}
	result, err := fake.FindRoute(context.Background(), RouteQuery{Graph: graph, From: 0, To: 0})
	if err != nil {
		t.Fatalf("FindRoute() failed: %v", err)
	}
	if result.Judgement.Reason != ReasonTrivial || !result.Judgement.Certified() {
		t.Fatalf("judgement = %+v, want a trivial certificate", result.Judgement)
	}
	if len(result.Route.Edges) != 0 {
		t.Fatal("the trivial route traverses nothing")
	}
}

// TestFakeOracleRouteIsFilteredByTheMoveset is the gating check in miniature:
// one graph, two stages, two different answers, and no second graph.
func TestFakeOracleRouteIsFilteredByTheMoveset(t *testing.T) {
	fake := &FakeOracle{}
	graph := &JumpGraph{
		Nodes: []MotionNode{{ID: 0}, {ID: 1}},
		Edges: []MotionEdge{{ID: 0, From: 0, To: 1, Requires: NewAbilitySet(AbilityDash)}},
	}
	locked, err := fake.FindRoute(context.Background(), RouteQuery{Graph: graph, From: 0, To: 1})
	if err != nil {
		t.Fatalf("FindRoute() failed: %v", err)
	}
	if locked.Judgement.Verdict != VerdictRejected || locked.Judgement.Reason != ReasonDisconnected {
		t.Fatalf("judgement = %+v, want the gate to hold before the ability", locked.Judgement)
	}
	opened, err := fake.FindRoute(context.Background(), RouteQuery{
		Graph: graph, From: 0, To: 1, Abilities: NewAbilitySet(AbilityDash),
	})
	if err != nil {
		t.Fatalf("FindRoute() failed: %v", err)
	}
	if !opened.Judgement.Certified() {
		t.Fatalf("judgement = %+v, want the gate to open once the ability is held", opened.Judgement)
	}
	if opened.Route.Requires != NewAbilitySet(AbilityDash) {
		t.Fatalf("the route requires %v, want dash", opened.Route.Requires)
	}
}

func TestFakeOracleRejectsAMalformedRouteQuery(t *testing.T) {
	fake := &FakeOracle{}
	graph := &JumpGraph{Nodes: []MotionNode{{ID: 0}}}
	cases := []struct {
		name  string
		query RouteQuery
	}{
		{name: "nil graph", query: RouteQuery{From: 0, To: 0}},
		{name: "unknown origin", query: RouteQuery{Graph: graph, From: 9, To: 0}},
		{name: "unknown goal", query: RouteQuery{Graph: graph, From: 0, To: 9}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := fake.FindRoute(context.Background(), tc.query)
			if !errors.Is(err, ErrInvalidQuery) {
				t.Fatalf("error %v does not wrap ErrInvalidQuery", err)
			}
		})
	}
}

// TestBudgetExhaustionIsUnknownAndKeepsThePartialGraph is the contract that
// buys the whole three-valued design: running out of search is not a
// rejection, and the work already done is still correct.
func TestBudgetExhaustionIsUnknownAndKeepsThePartialGraph(t *testing.T) {
	profile := DefaultProfile()
	fake := &FakeOracle{}
	budget := DefaultSearchBudget()
	budget.MaxCandidateEdges = 1
	built, err := fake.BuildGraph(context.Background(), GraphQuery{
		Grid: fixtureGrid(), Profile: profile, Abilities: profile.Abilities(), Budget: budget,
	})
	if err != nil {
		t.Fatalf("BuildGraph() failed: %v", err)
	}
	if built.Judgement.Verdict != VerdictUnknown {
		t.Fatalf("judgement = %+v, want unknown", built.Judgement)
	}
	if built.Judgement.Reason != ReasonBudgetExhausted {
		t.Fatalf("reason = %s, want budget-exhausted", built.Judgement.Reason)
	}
	if !built.Judgement.Budget.Exhausted {
		t.Fatal("the report must say the search was cut short")
	}
	if err := built.Judgement.Validate(); err != nil {
		t.Fatalf("the judgement is internally inconsistent: %v", err)
	}
	if len(built.Graph.Nodes) != 2 {
		t.Fatalf("the partial graph lost its nodes: %d", len(built.Graph.Nodes))
	}

	routeBudget := DefaultSearchBudget()
	routeBudget.MaxExpandedNodes = 0
	routeBudget.MaxCandidateEdges = 0
	routeBudget.MaxCollisionTests = 1
	// A zero expansion ceiling means unlimited, so cut it to one instead.
	routeBudget.MaxExpandedNodes = 1
	graph := &JumpGraph{
		Nodes: []MotionNode{{ID: 0}, {ID: 1}, {ID: 2}},
		Edges: []MotionEdge{{ID: 0, From: 0, To: 1}, {ID: 1, From: 1, To: 2}},
	}
	result, err := fake.FindRoute(context.Background(), RouteQuery{Graph: graph, From: 0, To: 2, Budget: routeBudget})
	if err != nil {
		t.Fatalf("FindRoute() failed: %v", err)
	}
	if result.Judgement.Verdict != VerdictUnknown || result.Judgement.Reason != ReasonBudgetExhausted {
		t.Fatalf("judgement = %+v, want unknown for an exhausted search", result.Judgement)
	}
}

func TestFakeOracleIsDeterministic(t *testing.T) {
	profile := DefaultProfile()
	fake := &FakeOracle{}
	query := GraphQuery{Grid: fixtureGrid(), Profile: profile, Abilities: profile.Abilities()}
	first, err := fake.BuildGraph(context.Background(), query)
	if err != nil {
		t.Fatalf("BuildGraph() failed: %v", err)
	}
	second, err := fake.BuildGraph(context.Background(), query)
	if err != nil {
		t.Fatalf("BuildGraph() failed: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("two identical queries produced two different graphs")
	}
	// A second oracle value answers the same way: nothing is carried between
	// calls.
	third, err := (&FakeOracle{}).BuildGraph(context.Background(), query)
	if err != nil {
		t.Fatalf("BuildGraph() failed: %v", err)
	}
	if !reflect.DeepEqual(first, third) {
		t.Fatal("a fresh oracle produced a different graph")
	}
}

func TestFakeOracleHonoursCancellation(t *testing.T) {
	profile := DefaultProfile()
	fake := &FakeOracle{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := fake.Surfaces(ctx, SurfaceQuery{Grid: fixtureGrid(), Profile: profile}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Surfaces() returned %v, want context.Canceled", err)
	}
	if _, err := fake.CheckEdge(ctx, EdgeQuery{Grid: fixtureGrid(), Profile: profile}); !errors.Is(err, context.Canceled) {
		t.Fatalf("CheckEdge() returned %v, want context.Canceled", err)
	}
	if _, err := fake.BuildGraph(ctx, GraphQuery{Grid: fixtureGrid(), Profile: profile}); !errors.Is(err, context.Canceled) {
		t.Fatalf("BuildGraph() returned %v, want context.Canceled", err)
	}
	graph := &JumpGraph{Nodes: []MotionNode{{ID: 0}}}
	if _, err := fake.FindRoute(ctx, RouteQuery{Graph: graph}); !errors.Is(err, context.Canceled) {
		t.Fatalf("FindRoute() returned %v, want context.Canceled", err)
	}
}

func TestFakeOracleRejectsAnInvalidProfile(t *testing.T) {
	fake := &FakeOracle{}
	broken := DefaultProfile()
	broken.GravityUp = 0
	if _, err := fake.CheckEdge(context.Background(), EdgeQuery{Grid: fixtureGrid(), Profile: broken}); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("CheckEdge() returned %v, want ErrInvalidProfile", err)
	}
	if _, err := fake.BuildGraph(context.Background(), GraphQuery{Grid: fixtureGrid(), Profile: broken}); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("BuildGraph() returned %v, want ErrInvalidProfile", err)
	}
}

// TestFakeOracleHooksReplaceOneMethodEach is what makes the fake useful for a
// negative test: a consumer can force the exact answer it wants to handle.
func TestFakeOracleHooksReplaceOneMethodEach(t *testing.T) {
	sentinel := errors.New("hook")
	fake := &FakeOracle{
		SurfacesFunc: func(context.Context, SurfaceQuery) (SurfaceResult, error) { return SurfaceResult{}, sentinel },
		CheckEdgeFunc: func(context.Context, EdgeQuery) (EdgeResult, error) {
			return EdgeResult{Judgement: Judgement{
				Verdict: VerdictRejected, Reason: ReasonObstructed, Model: ModelFake,
			}}, nil
		},
		BuildGraphFunc: func(context.Context, GraphQuery) (GraphResult, error) { return GraphResult{}, sentinel },
		FindRouteFunc:  func(context.Context, RouteQuery) (RouteResult, error) { return RouteResult{}, sentinel },
	}
	if _, err := fake.Surfaces(context.Background(), SurfaceQuery{}); !errors.Is(err, sentinel) {
		t.Fatal("SurfacesFunc was not used")
	}
	result, err := fake.CheckEdge(context.Background(), EdgeQuery{})
	if err != nil {
		t.Fatalf("CheckEdge() failed: %v", err)
	}
	if result.Judgement.Reason != ReasonObstructed {
		t.Fatal("CheckEdgeFunc was not used")
	}
	if _, err := fake.BuildGraph(context.Background(), GraphQuery{}); !errors.Is(err, sentinel) {
		t.Fatal("BuildGraphFunc was not used")
	}
	if _, err := fake.FindRoute(context.Background(), RouteQuery{}); !errors.Is(err, sentinel) {
		t.Fatal("FindRouteFunc was not used")
	}
}

func TestEffectiveBudgetFillsTheZeroValue(t *testing.T) {
	if got := effectiveBudget(SearchBudget{}); got != DefaultSearchBudget() {
		t.Fatalf("effectiveBudget(zero) = %+v, want the default", got)
	}
	explicit := SearchBudget{MaxExpandedNodes: 3, FallHorizon: 1, LaunchResolution: 1, TimeResolution: 1}
	if got := effectiveBudget(explicit); got != explicit {
		t.Fatalf("effectiveBudget overwrote an explicit budget: %+v", got)
	}
}
