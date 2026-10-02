package platform

import (
	"errors"
	"math"
	"testing"
)

func TestSpanPredicates(t *testing.T) {
	cases := []struct {
		name     string
		span     Span
		empty    bool
		point    bool
		length   float64
		contains float64
		inside   bool
	}{
		{name: "ordinary", span: Span{Lo: 1, Hi: 3}, length: 2, contains: 2, inside: true},
		{name: "outside", span: Span{Lo: 1, Hi: 3}, length: 2, contains: 3.5},
		{name: "point", span: Point(2), point: true, contains: 2, inside: true},
		{name: "inverted", span: Span{Lo: 3, Hi: 1}, empty: true, contains: 2},
		{name: "nan low", span: Span{Lo: math.NaN(), Hi: 1}, empty: true, contains: 1},
		{name: "nan high", span: Span{Lo: 0, Hi: math.NaN()}, empty: true, contains: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.span.IsEmpty(); got != tc.empty {
				t.Fatalf("IsEmpty() = %v, want %v", got, tc.empty)
			}
			if got := tc.span.IsPoint(); got != tc.point {
				t.Fatalf("IsPoint() = %v, want %v", got, tc.point)
			}
			if got := tc.span.Length(); got != tc.length {
				t.Fatalf("Length() = %v, want %v", got, tc.length)
			}
			if got := tc.span.Contains(tc.contains); got != tc.inside {
				t.Fatalf("Contains(%v) = %v, want %v", tc.contains, got, tc.inside)
			}
		})
	}
}

func TestSpanIntersectAndOverlap(t *testing.T) {
	first := Span{Lo: 0, Hi: 4}
	second := Span{Lo: 3, Hi: 9}
	if !first.Overlaps(second) || !second.Overlaps(first) {
		t.Fatal("overlapping spans must report an overlap in both directions")
	}
	got, ok := first.Intersect(second)
	if !ok || got != (Span{Lo: 3, Hi: 4}) {
		t.Fatalf("Intersect() = %v, %v, want [3, 4], true", got, ok)
	}
	disjoint := Span{Lo: 10, Hi: 11}
	if first.Overlaps(disjoint) {
		t.Fatal("disjoint spans must not overlap")
	}
	if _, ok := first.Intersect(disjoint); ok {
		t.Fatal("disjoint spans must not intersect")
	}
	if got := first.Shift(2); got != (Span{Lo: 2, Hi: 6}) {
		t.Fatalf("Shift(2) = %v, want [2, 6]", got)
	}
}

// TestSpanContainsSpanIsUniversal locks the quantifier. A node's span is the
// set of states it stands for, and "contained" has to mean every member, not
// some member; the optimistic reading is how an abstract graph starts lying.
func TestSpanContainsSpanIsUniversal(t *testing.T) {
	outer := Span{Lo: 0, Hi: 10}
	if !outer.ContainsSpan(Span{Lo: 2, Hi: 8}) {
		t.Fatal("a span must contain a span wholly inside it")
	}
	if outer.ContainsSpan(Span{Lo: 8, Hi: 12}) {
		t.Fatal("a partially overlapping span must not count as contained")
	}
	if !outer.ContainsSpan(Span{Lo: 1, Hi: 0}) {
		t.Fatal("an empty span demands nothing and is contained")
	}
	if (Span{Lo: 1, Hi: 0}).ContainsSpan(Span{Lo: 1, Hi: 0}) {
		t.Fatal("an empty span contains nothing, not even another empty span")
	}
}

// TestWorldYAndGridRowAgree locks the one conversion between the grid frame,
// where Y grows downward, and the world frame, where it grows upward. Getting
// this wrong silently mirrors every map.
func TestWorldYAndGridRowAgree(t *testing.T) {
	const height = 6
	for row := int32(0); row < height; row++ {
		top := WorldY(row, height)
		if want := float64(height - row); top != want {
			t.Fatalf("WorldY(%d) = %v, want %v", row, top, want)
		}
		// A point strictly inside the row's band resolves back to the row.
		if got := GridRow(top-0.5, height); got != row {
			t.Fatalf("GridRow(%v) = %d, want %d", top-0.5, got, row)
		}
	}
	if got := WorldY(height, height); got != 0 {
		t.Fatalf("the bottom edge of the last row is %v, want 0", got)
	}
}

func TestCellKindSemantics(t *testing.T) {
	cases := []struct {
		kind     CellKind
		known    bool
		blocks   bool
		supports bool
		name     string
	}{
		{kind: CellKindEmpty, known: true, name: "empty"},
		{kind: CellKindSolid, known: true, blocks: true, supports: true, name: "solid"},
		{kind: CellKindSemiSolid, known: true, supports: true, name: "semi-solid"},
		{kind: CellKindClimbable, known: true, name: "climbable"},
		{kind: CellKindHazard, known: true, name: "hazard"},
		{kind: CellKind(99), name: "cellkind(99)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.kind.IsKnown(); got != tc.known {
				t.Fatalf("IsKnown() = %v, want %v", got, tc.known)
			}
			if got := tc.kind.Blocks(); got != tc.blocks {
				t.Fatalf("Blocks() = %v, want %v", got, tc.blocks)
			}
			if got := tc.kind.Supports(); got != tc.supports {
				t.Fatalf("Supports() = %v, want %v", got, tc.supports)
			}
			if got := tc.kind.String(); got != tc.name {
				t.Fatalf("String() = %q, want %q", got, tc.name)
			}
		})
	}
}

// TestSemiSolidIsNotSolid is the one cell distinction a wire converter is most
// likely to lose. A semi-solid that blocks is a wall; a semi-solid that does
// not support is air.
func TestSemiSolidIsNotSolid(t *testing.T) {
	if CellKindSemiSolid.Blocks() {
		t.Fatal("a semi-solid platform must not block motion from below")
	}
	if !CellKindSemiSolid.Supports() {
		t.Fatal("a semi-solid platform must support a standing character")
	}
}

func TestGridLookupFailsClosed(t *testing.T) {
	grid := Grid{Width: 2, Height: 2, Cells: []CellKind{
		CellKindEmpty, CellKindSolid,
		CellKindSemiSolid, CellKindHazard,
	}}
	if kind, ok := grid.At(Cell{X: 1, Y: 0}); !ok || kind != CellKindSolid {
		t.Fatalf("At(1,0) = %v, %v, want solid, true", kind, ok)
	}
	for _, at := range []Cell{{X: -1}, {Y: -1}, {X: 2}, {Y: 2}} {
		if kind, ok := grid.At(at); ok || kind != CellKindEmpty {
			t.Fatalf("At(%v) = %v, %v, want empty, false", at, kind, ok)
		}
	}
	short := Grid{Width: 2, Height: 2, Cells: []CellKind{CellKindSolid}}
	if _, ok := short.At(Cell{X: 1, Y: 1}); ok {
		t.Fatal("a short cell slice must not report a hit")
	}
	if got := grid.CellCount(); got != 4 {
		t.Fatalf("CellCount() = %d, want 4", got)
	}
}

func TestGridTerrainLookupFailsClosed(t *testing.T) {
	layer := &TerrainLayer{
		Palette: []TerrainDefinition{{ID: "spikes", Lethal: true}},
		Indices: []byte{0, 1, 2, 0},
	}
	grid := Grid{Width: 2, Height: 2, Cells: make([]CellKind, 4), Terrain: layer}
	if definition, ok := grid.TerrainAt(Cell{X: 1, Y: 0}); !ok || definition.ID != "spikes" {
		t.Fatalf("TerrainAt(1,0) = %v, %v, want spikes, true", definition, ok)
	}
	if _, ok := grid.TerrainAt(Cell{X: 0, Y: 0}); ok {
		t.Fatal("index zero means no terrain")
	}
	if _, ok := grid.TerrainAt(Cell{X: 0, Y: 1}); ok {
		t.Fatal("an out-of-range palette index must report no terrain")
	}
	if _, ok := (Grid{Width: 1, Height: 1}).TerrainAt(Cell{}); ok {
		t.Fatal("a nil layer must report no terrain")
	}
}

func TestAbilitySetOperations(t *testing.T) {
	set := NewAbilitySet(AbilityDash, AbilityWallJump)
	if !set.Has(AbilityDash) || !set.Has(AbilityWallJump) {
		t.Fatalf("%v is missing a declared ability", set)
	}
	if set.Has(AbilityDoubleJump) {
		t.Fatalf("%v holds an ability it was never given", set)
	}
	if got := set.Count(); got != 2 {
		t.Fatalf("Count() = %d, want 2", got)
	}
	abilities := set.Abilities()
	if len(abilities) != 2 || abilities[0] != AbilityDash || abilities[1] != AbilityWallJump {
		t.Fatalf("Abilities() = %v, want ascending [dash wall-jump]", abilities)
	}
	if got := set.String(); got != "dash,wall-jump" {
		t.Fatalf("String() = %q, want %q", got, "dash,wall-jump")
	}
	if got := AbilitySet(0).String(); got != "base" {
		t.Fatalf("the empty set is the base moveset, got %q", got)
	}
	if got := set.Without(AbilityDash); got != NewAbilitySet(AbilityWallJump) {
		t.Fatalf("Without(dash) = %v, want wall-jump alone", got)
	}
	if got := set.Union(NewAbilitySet(AbilityClimb)).Count(); got != 3 {
		t.Fatalf("Union() lost an ability, count = %d", got)
	}
}

// TestAbilitySetIgnoresUnknownAbilities keeps a corrupt value from setting a
// bit nothing can ever clear or read.
func TestAbilitySetIgnoresUnknownAbilities(t *testing.T) {
	outside := Ability(abilityCount + 3)
	if outside.IsKnown() {
		t.Fatal("an ability past the vocabulary must not be known")
	}
	if got := NewAbilitySet(outside); got != 0 {
		t.Fatalf("an unknown ability must not enter the set, got %v", got)
	}
	if AbilitySet(0).Has(outside) {
		t.Fatal("an unknown ability is never held")
	}
}

func TestAbilitySetContainsIsTheGatePredicate(t *testing.T) {
	held := NewAbilitySet(AbilityDash, AbilityDoubleJump)
	if !held.Contains(NewAbilitySet(AbilityDash)) {
		t.Fatal("a held ability must satisfy a gate that asks for it")
	}
	if !held.Contains(0) {
		t.Fatal("every moveset satisfies the base requirement")
	}
	if held.Contains(NewAbilitySet(AbilityDash, AbilityWallJump)) {
		t.Fatal("a gate asking for an ability that is not held must not open")
	}
}

func TestProgressionPlanStages(t *testing.T) {
	plan := ProgressionPlan{
		Steps: []ProgressionStep{
			{Name: "dash", Grants: NewAbilitySet(AbilityDash)},
			{Name: "claw", Grants: NewAbilitySet(AbilityWallJump)},
		},
	}
	stages := plan.Stages()
	if len(stages) != 3 {
		t.Fatalf("Stages() returned %d entries, want one more than the steps", len(stages))
	}
	if stages[0] != 0 {
		t.Fatalf("the first stage is the base moveset, got %v", stages[0])
	}
	if stages[1] != NewAbilitySet(AbilityDash) {
		t.Fatalf("stage 1 = %v, want dash", stages[1])
	}
	if stages[2] != NewAbilitySet(AbilityDash, AbilityWallJump) {
		t.Fatalf("stage 2 = %v, want dash and wall-jump", stages[2])
	}
	if plan.Final() != stages[2] {
		t.Fatalf("Final() = %v, want the last stage", plan.Final())
	}
	// The chain is monotone by construction, which is the property the macro
	// layer leans on when it reasons about one stage at a time.
	for index := 1; index < len(stages); index++ {
		if !stages[index].Contains(stages[index-1]) {
			t.Fatalf("stage %d lost an ability held at stage %d", index, index-1)
		}
	}
}

func TestTransitionSenses(t *testing.T) {
	both := Transition{
		Outbound: &Traversal{},
		Inbound:  &Traversal{Requires: NewAbilitySet(AbilityWallJump)},
	}
	if both.IsOneWay() {
		t.Fatal("a transition with both senses is not one-way, however they differ")
	}
	if !both.IsConditional() {
		t.Fatal("two senses with different requirements is the conditional shortcut")
	}

	oneWay := Transition{Outbound: &Traversal{}}
	if !oneWay.IsOneWay() {
		t.Fatal("a transition with one sense is one-way")
	}
	if oneWay.IsConditional() {
		t.Fatal("a one-way transition has no second sense to differ from")
	}

	symmetric := Transition{Outbound: &Traversal{}, Inbound: &Traversal{}}
	if symmetric.IsOneWay() || symmetric.IsConditional() {
		t.Fatal("two identical senses is an ordinary two-way opening")
	}
}

func TestTransitionSideGeometry(t *testing.T) {
	cases := []struct {
		side     TransitionSide
		cardinal bool
		vertical bool
		name     string
	}{
		{side: TransitionSideUnspecified, name: "unspecified"},
		{side: TransitionSideLeft, cardinal: true, vertical: true, name: "left"},
		{side: TransitionSideRight, cardinal: true, vertical: true, name: "right"},
		{side: TransitionSideTop, cardinal: true, name: "top"},
		{side: TransitionSideBottom, cardinal: true, name: "bot"},
		{side: TransitionSideDoor, name: "door"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.side.IsCardinal(); got != tc.cardinal {
				t.Fatalf("IsCardinal() = %v, want %v", got, tc.cardinal)
			}
			if got := tc.side.IsVertical(); got != tc.vertical {
				t.Fatalf("IsVertical() = %v, want %v", got, tc.vertical)
			}
			if got := tc.side.String(); got != tc.name {
				t.Fatalf("String() = %q, want %q", got, tc.name)
			}
		})
	}
}

func TestMotionNodeIsRest(t *testing.T) {
	profile := DefaultProfile()
	rest := MotionNode{
		Mode:      MotionModeGrounded,
		Velocity:  Point(0),
		Resources: profile.FullResources(),
	}
	if !rest.IsRest(profile) {
		t.Fatal("a grounded, still node holding full resources is a rest node")
	}

	moving := rest
	moving.Velocity = Span{Lo: 4, Hi: 8}
	if moving.IsRest(profile) {
		t.Fatal("a node that cannot be standing still is not a rest node")
	}

	spent := rest
	spent.Resources.DashCharges = 0
	if spent.IsRest(profile) {
		t.Fatal("a node with a spent dash is not a rest node, which is exactly the case aggregation gets wrong")
	}

	airborne := rest
	airborne.Mode = MotionModeAirborne
	if airborne.IsRest(profile) {
		t.Fatal("an airborne node is not a rest node")
	}
}

func TestJumpGraphLookups(t *testing.T) {
	graph := &JumpGraph{
		Surfaces: []Surface{{ID: 0}, {ID: 1}},
		Nodes:    []MotionNode{{ID: 0}, {ID: 1}, {ID: 2}},
		Edges: []MotionEdge{
			{ID: 0, From: 0, To: 1, Kind: MotionEdgeKindJump, Passage: 7},
			{ID: 1, From: 1, To: 0, Kind: MotionEdgeKindFall, Passage: 7, Requires: NewAbilitySet(AbilityDash)},
			{ID: 2, From: 0, To: 2, Kind: MotionEdgeKindWalk},
		},
	}
	if node, ok := graph.Node(1); !ok || node.ID != 1 {
		t.Fatalf("Node(1) = %v, %v", node, ok)
	}
	if _, ok := graph.Node(9); ok {
		t.Fatal("a node outside the graph must not be found")
	}
	if edge, ok := graph.Edge(2); !ok || edge.Kind != MotionEdgeKindWalk {
		t.Fatalf("Edge(2) = %v, %v", edge, ok)
	}
	if _, ok := graph.Edge(9); ok {
		t.Fatal("an edge outside the graph must not be found")
	}
	if surface, ok := graph.Surface(1); !ok || surface.ID != 1 {
		t.Fatalf("Surface(1) = %v, %v", surface, ok)
	}
	out := graph.OutEdges(0)
	if len(out) != 2 || out[0].ID != 0 || out[1].ID != 2 {
		t.Fatalf("OutEdges(0) = %v, want edges 0 and 2 in ascending order", out)
	}
	var nilGraph *JumpGraph
	if nilGraph.OutEdges(0) != nil {
		t.Fatal("a nil graph has no out edges")
	}
}

// TestJumpGraphPassageExpressesTheConditionalShortcut is the cheap-to-express
// requirement made concrete: one passage, two directed edges, different
// requirements, no second piece of geometry.
func TestJumpGraphPassageExpressesTheConditionalShortcut(t *testing.T) {
	graph := &JumpGraph{Edges: []MotionEdge{
		{ID: 0, From: 0, To: 1, Passage: 7},
		{ID: 1, From: 1, To: 0, Passage: 7, Requires: NewAbilitySet(AbilityDoubleJump)},
		{ID: 2, From: 0, To: 2, Passage: 0},
	}}
	senses := graph.Passage(7)
	if len(senses) != 2 {
		t.Fatalf("Passage(7) returned %d edges, want both senses", len(senses))
	}
	if senses[0].Requires == senses[1].Requires {
		t.Fatal("the two senses were supposed to differ in what they require")
	}
	if graph.Passage(0) != nil {
		t.Fatal("the zero passage groups nothing")
	}

	base := AbilitySet(0)
	if !senses[0].Reachable(base) {
		t.Fatal("the downhill sense must be usable by the base moveset")
	}
	if senses[1].Reachable(base) {
		t.Fatal("the uphill sense must stay shut until the ability is held")
	}
	if !senses[1].Reachable(NewAbilitySet(AbilityDoubleJump)) {
		t.Fatal("the uphill sense must open once the ability is held")
	}
}

func TestWitnessDuration(t *testing.T) {
	witness := &Witness{Phases: []Phase{{Duration: 0.25}, {Duration: 0.5}}}
	if got := witness.Duration(); math.Abs(got-0.75) > 1e-12 {
		t.Fatalf("Duration() = %v, want 0.75", got)
	}
	var missing *Witness
	if got := missing.Duration(); got != 0 {
		t.Fatalf("a missing witness lasts %v, want 0", got)
	}
}

// TestVerdictReasonBelongsToOneVerdict is the guard that keeps a judgement
// from reporting an exhausted budget as a rejection. Every reason is listed,
// so a reason added later without a verdict fails here.
func TestVerdictReasonBelongsToOneVerdict(t *testing.T) {
	cases := []struct {
		reason VerdictReason
		want   Verdict
		name   string
	}{
		{reason: ReasonUnspecified, want: VerdictUnknown, name: "unspecified"},
		{reason: ReasonWitnessFound, want: VerdictCertified, name: "witness-found"},
		{reason: ReasonTrivial, want: VerdictCertified, name: "trivial"},
		{reason: ReasonOutOfEnvelope, want: VerdictRejected, name: "out-of-envelope"},
		{reason: ReasonObstructed, want: VerdictRejected, name: "obstructed"},
		{reason: ReasonLandingUnsupported, want: VerdictRejected, name: "landing-unsupported"},
		{reason: ReasonResourceExhausted, want: VerdictRejected, name: "resource-exhausted"},
		{reason: ReasonAbilityMissing, want: VerdictRejected, name: "ability-missing"},
		{reason: ReasonDisconnected, want: VerdictRejected, name: "disconnected"},
		{reason: ReasonBudgetExhausted, want: VerdictUnknown, name: "budget-exhausted"},
		{reason: ReasonUnsupportedMoveset, want: VerdictUnknown, name: "unsupported-moveset"},
		{reason: ReasonUnsupportedGeometry, want: VerdictUnknown, name: "unsupported-geometry"},
		{reason: ReasonNotImplemented, want: VerdictUnknown, name: "not-implemented"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.reason.Verdict(); got != tc.want {
				t.Fatalf("Verdict() = %s, want %s", got, tc.want)
			}
			if got := tc.reason.String(); got != tc.name {
				t.Fatalf("String() = %q, want %q", got, tc.name)
			}
		})
	}
	// A budget that ran out is never a rejection: that is the whole reason the
	// verdict has three values.
	if ReasonBudgetExhausted.Verdict() == VerdictRejected {
		t.Fatal("an exhausted budget must never be reported as a rejection")
	}
}

func TestJudgementValidate(t *testing.T) {
	valid := Judgement{Verdict: VerdictCertified, Reason: ReasonWitnessFound, Model: ModelM1}
	if err := valid.Validate(); err != nil {
		t.Fatalf("a well-formed judgement was rejected: %v", err)
	}
	if !valid.Certified() {
		t.Fatal("Certified() must agree with the verdict")
	}

	cases := []struct {
		name      string
		judgement Judgement
	}{
		{name: "no reason", judgement: Judgement{Verdict: VerdictCertified, Model: ModelM1}},
		{name: "reason from another verdict", judgement: Judgement{Verdict: VerdictRejected, Reason: ReasonBudgetExhausted, Model: ModelM1}},
		{name: "no model", judgement: Judgement{Verdict: VerdictCertified, Reason: ReasonWitnessFound}},
		{
			name: "exhausted budget with a decided verdict",
			judgement: Judgement{
				Verdict: VerdictRejected, Reason: ReasonDisconnected, Model: ModelM1,
				Budget: BudgetReport{Exhausted: true},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.judgement.Validate()
			if err == nil {
				t.Fatal("a malformed judgement was accepted")
			}
			if !errors.Is(err, ErrInvalidJudgement) {
				t.Fatalf("error %v does not wrap ErrInvalidJudgement", err)
			}
		})
	}
}

func TestBeatKindVocabularyIsClosed(t *testing.T) {
	if BeatKindUnspecified.IsKnown() {
		t.Fatal("the zero beat kind must not be a declared kind")
	}
	if BeatKind(99).IsKnown() {
		t.Fatal("a value past the vocabulary must not be a declared kind")
	}
	demanding := map[BeatKind]bool{
		BeatKindTraverse: true, BeatKindGap: true, BeatKindClimb: true,
		BeatKindDescend: true, BeatKindShaft: true, BeatKindHazard: true,
		BeatKindPrecision: true, BeatKindGate: true, BeatKindSecret: true,
	}
	for kind := BeatKindRest; kind <= BeatKindCheckpoint; kind++ {
		if !kind.IsKnown() {
			t.Fatalf("%s is inside the declared range and must be known", kind)
		}
		if got := kind.IsDemanding(); got != demanding[kind] {
			t.Fatalf("%s.IsDemanding() = %v, want %v", kind, got, demanding[kind])
		}
		if kind.String() == "unspecified" {
			t.Fatalf("beat kind %d has no name", int(kind))
		}
	}
	if BeatKindUnspecified.IsDemanding() {
		t.Fatal("an unspecified beat demands nothing because it is not a beat")
	}
}

func TestStringersNameEveryDeclaredValue(t *testing.T) {
	cases := []struct {
		got  string
		want string
	}{
		{got: Verdict(99).String(), want: "unknown"},
		{got: VerdictCertified.String(), want: "certified"},
		{got: VerdictRejected.String(), want: "rejected"},
		{got: MotionModeUnspecified.String(), want: "unspecified"},
		{got: MotionModeCoyote.String(), want: "coyote"},
		{got: MotionModeWallCling.String(), want: "wall-cling"},
		{got: MotionEdgeKindUnspecified.String(), want: "unspecified"},
		{got: MotionEdgeKindDropThrough.String(), want: "drop-through"},
		{got: MotionEdgeKindTransition.String(), want: "transition"},
		{got: SurfaceKindUnspecified.String(), want: "unspecified"},
		{got: SurfaceKindSemiSolid.String(), want: "semi-solid"},
		{got: NodeDisciplineRefined.String(), want: "refined"},
		{got: NodeDisciplineRestOnly.String(), want: "rest-only"},
		{got: NodeDisciplineUnspecified.String(), want: "unspecified"},
		{got: WallSideNone.String(), want: "none"},
		{got: WallSideLeft.String(), want: "left"},
		{got: DoubleJumpModeUnspecified.String(), want: "unspecified"},
		{got: DoubleJumpModeReset.String(), want: "reset"},
		{got: DoubleJumpModeImpulse.String(), want: "impulse"},
		{got: VariableJumpModeSwapGravity.String(), want: "swap-gravity"},
		{got: DashExitModeClampToRun.String(), want: "clamp-to-run"},
		{got: Ability(99).String(), want: "ability(99)"},
		{got: Span{Lo: 1, Hi: 0}.String(), want: "empty"},
		{got: Span{Lo: 0.5, Hi: 2}.String(), want: "[0.5, 2]"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("String() = %q, want %q", tc.got, tc.want)
		}
	}
}

func TestSurfaceKindSupports(t *testing.T) {
	for _, kind := range []SurfaceKind{SurfaceKindFloor, SurfaceKindSemiSolid} {
		if !kind.Supports() {
			t.Fatalf("%s must support a standing character", kind)
		}
	}
	for _, kind := range []SurfaceKind{SurfaceKindUnspecified, SurfaceKindWallLeft, SurfaceKindWallRight, SurfaceKindCeiling, SurfaceKindClimbable} {
		if kind.Supports() {
			t.Fatalf("%s must not support a standing character", kind)
		}
	}
}
