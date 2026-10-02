package platform

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/Otoru/daedalus/core"
)

func TestVerticalOpeningsSitInDifferentBands(t *testing.T) {
	spans := contactSpans(roomContact{axis0: 0, axis1: 12, sideA: TransitionSideRight}, true)
	if len(spans) != 2 {
		t.Fatalf("spans = %d, want a floor opening and a high opening", len(spans))
	}
	if spansOverlap(spans[0], spans[1]) {
		t.Fatalf("openings overlap: %+v %+v", spans[0], spans[1])
	}
	floor, ok := floorSpan(0, 12)
	if !ok || spans[0] != floor {
		t.Fatalf("first opening = %+v, want floor %+v", spans[0], floor)
	}
	// Y grows down, so the high opening starts at a smaller plane Y and a
	// larger Offset. The two landings are a whole number of bands apart.
	if spans[1].start >= spans[0].start {
		t.Fatalf("high opening start %d is not above the floor opening start %d", spans[1].start, spans[0].start)
	}
	if (spans[0].start-spans[1].start)%GateBandCells != 0 {
		t.Fatalf("opening starts %d and %d are not on the band quantum %d", spans[0].start, spans[1].start, GateBandCells)
	}
	if spanSolidGap(spans[0], spans[1]) < int32(OpeningExtent) {
		t.Fatalf("solid gap %d is thinner than a doorway of %d", spanSolidGap(spans[0], spans[1]), OpeningExtent)
	}
	// The shortest overlap that used to host two bands leaves one solid cell
	// between two holes of OpeningExtent. That is one torn opening.
	ragged := contactSpans(roomContact{axis0: 0, axis1: minOverlapCells, sideA: TransitionSideRight}, true)
	if len(ragged) != 1 {
		t.Fatalf("overlap of %d produced %d openings", minOverlapCells, len(ragged))
	}
}

func TestGenerateMacroMatchesTheCalibration(t *testing.T) {
	cfg := MacroConfig{
		Seed:   7,
		Width:  512,
		Height: 512,
		Rooms:  92,
		Steps: []ProgressionStep{
			{Name: "dash", Grants: NewAbilitySet(AbilityDash)},
			{Name: "claw", Grants: NewAbilitySet(AbilityWallJump)},
			{Name: "wings", Grants: NewAbilitySet(AbilityDoubleJump)},
		},
	}
	macro, err := GenerateMacro(context.Background(), cfg)
	if err != nil {
		t.Fatalf("GenerateMacro: %v", err)
	}
	if err := ValidatePlane(macro.Plane); err != nil {
		t.Fatalf("ValidatePlane: %v", err)
	}
	stats := TransitionStatistics(macro.Plane)
	if stats.ParallelPairs != 0 {
		t.Fatalf("parallel room pairs = %d; inflated degree %.4f over %d distinct pairs", stats.ParallelPairs, stats.MeanDegree, stats.DistinctPairs)
	}
	honest := 2 * float64(stats.DistinctPairs) / float64(stats.Rooms)
	if math.Abs(stats.MeanDegree-honest) > 1e-9 {
		t.Fatalf("mean degree = %.4f, honest degree = %.4f", stats.MeanDegree, honest)
	}
	if msg := exampleSideOpeningError(macro.Plane); msg != "" {
		t.Fatal(msg)
	}
	wantAsym := targetAsymmetricCount(stats.Pairs)
	if stats.AsymmetricPairs > wantAsym+1 || stats.ConditionalPairs < len(cfg.Steps) {
		t.Fatalf("asymmetry = %+v, want conditional gates and at most one extra one-way over %d", stats, wantAsym)
	}
	if stats.OneWayPairs > stats.ConditionalPairs {
		t.Fatalf("one-way pairs %d outnumber conditional pairs %d", stats.OneWayPairs, stats.ConditionalPairs)
	}
	widths := map[uint32]bool{}
	var multiOffset bool
	for _, room := range macro.Plane.Rooms {
		widths[room.Grid.Width] = true
		perSide := map[TransitionSide][]uint32{}
		for _, tr := range room.Transitions {
			perSide[tr.Side] = append(perSide[tr.Side], tr.Offset)
		}
		for _, offsets := range perSide {
			if len(offsets) >= 2 && offsets[0] != offsets[len(offsets)-1] {
				multiOffset = true
			}
		}
	}
	if len(widths) < 2 {
		t.Fatal("every room has the same width")
	}
	if stats.MultiOpeningSides > 0 && !multiOffset {
		t.Fatal("a side with two openings does not give them different offsets")
	}
	audit, err := AuditMacro(context.Background(), macro)
	if err != nil {
		t.Fatalf("AuditMacro: %v", err)
	}
	if !audit.Route.Exists || !audit.Grants.Obtainable || !audit.Locks.Held || !audit.Softlocks.Clear {
		t.Fatalf("audit = route %v grants %v locks %v softlock %v", audit.Route.Exists, audit.Grants.Obtainable, audit.Locks.Held, audit.Softlocks.Clear)
	}
	if audit.Route.Stages[0].GoalReachable {
		t.Fatal("the goal is reachable with the base moveset")
	}
	last := audit.Route.Stages[len(audit.Route.Stages)-1]
	if !last.GoalReachable || !last.ObjectiveIsGoal {
		t.Fatal("the final stage does not reach the goal")
	}
	again, err := GenerateMacro(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second GenerateMacro: %v", err)
	}
	if TransitionStatistics(again.Plane) != stats {
		t.Fatal("a second generate with the same seed changed the statistics")
	}
	if again.Plane.Goal != macro.Plane.Goal || again.Plane.Rooms[3].Origin != macro.Plane.Rooms[3].Origin {
		t.Fatal("a second generate with the same seed moved a room or the goal")
	}
}

func TestCalibrationIsStableAcrossSeeds(t *testing.T) {
	var asym, oneWay, conditional, pairs, falls, returns int
	for seed := uint64(1); seed <= 8; seed++ {
		macro, err := GenerateMacro(context.Background(), MacroConfig{
			Seed: Seed(seed), Width: 512, Height: 512, Rooms: 92,
			Steps: []ProgressionStep{
				{Name: "dash", Grants: NewAbilitySet(AbilityDash)},
				{Name: "claw", Grants: NewAbilitySet(AbilityWallJump)},
				{Name: "wings", Grants: NewAbilitySet(AbilityDoubleJump)},
			},
		})
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		stats := TransitionStatistics(macro.Plane)
		wantAsym := targetAsymmetricCount(stats.Pairs)
		if stats.ParallelPairs != 0 || stats.ConditionalPairs != 3 || stats.AsymmetricPairs != wantAsym {
			t.Fatalf("seed %d stats = %+v, want 3 conditional and %d asymmetric", seed, stats, wantAsym)
		}
		if msg := exampleSideOpeningError(macro.Plane); msg != "" {
			t.Fatalf("seed %d: %s", seed, msg)
		}
		honest := 2 * float64(stats.DistinctPairs) / float64(stats.Rooms)
		if math.Abs(stats.MeanDegree-honest) > 1e-9 {
			t.Fatalf("seed %d mean degree %.4f, honest %.4f", seed, stats.MeanDegree, honest)
		}
		asym += stats.AsymmetricPairs
		oneWay += stats.OneWayPairs
		conditional += stats.ConditionalPairs
		pairs += stats.Pairs
		for _, room := range macro.Plane.Rooms {
			for _, tr := range room.Transitions {
				if tr.Outbound == nil {
					continue
				}
				switch tr.Outbound.Note {
				case "fall":
					falls++
				case "bot-return":
					returns++
				}
			}
		}
		audit, err := AuditMacro(context.Background(), macro)
		if err != nil {
			t.Fatalf("seed %d audit: %v", seed, err)
		}
		if !audit.Route.Exists || !audit.Grants.Obtainable || !audit.Locks.Held || !audit.Softlocks.Clear {
			t.Fatalf("seed %d audit failed", seed)
		}
	}
	rate := float64(asym) / float64(pairs)
	if math.Abs(rate-targetAsymmetricFraction) > 0.005 {
		t.Fatalf("asymmetric %d/%d = %.4f, target %.4f", asym, pairs, rate, targetAsymmetricFraction)
	}
	// A fall is emitted only when the drop has no way back. Extra shared walls
	// are real cycles, so a seed can have none. A bot-return still has to show
	// up: that is the one-way whose return is another pair.
	if returns == 0 || conditional <= oneWay {
		t.Fatalf("falls %d, bot-returns %d, conditional %d, one-way %d", falls, returns, conditional, oneWay)
	}
}

// TestACanonicalBeatFitsInTheDefaultRoom is the composition check that used to
// pass without ever placing a beat. The widest canonical beat is two platforms
// of the traverse span around the gap, and the default room has to hold it
// even at the largest leading pad the placer draws.
func TestACanonicalBeatFitsInTheDefaultRoom(t *testing.T) {
	cfg := (MacroConfig{Seed: 1, Width: 512, Height: 512, Rooms: 4}).Normalize()
	_, span := canonicalCells(BeatKindTraverse)
	beat := stampBeat(BeatKindTraverse, span, 0)
	wantWidth := span + platformGap + span
	if beat.Width != wantWidth {
		t.Fatalf("canonical beat width = %d, want %d", beat.Width, wantWidth)
	}
	room := Room{Grid: shellGrid(cfg.MinWidth, cfg.MinHeight)}
	rhythm := Rhythm{Beats: []RealizedBeat{{Kind: BeatKindTraverse, Grid: beat}}}
	_, fits, err := layoutRun(room, rhythm, []int{0}, int32(synthMaxLeadingPad), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !fits {
		t.Fatalf("a canonical beat %d cells wide does not fit in the default room %dx%d with leading pad %d", beat.Width, cfg.MinWidth, cfg.MinHeight, synthMaxLeadingPad)
	}
}

// TestValidateRejectsAPlaneTooSmallForADecentMap is the 70×44 plane that used
// to come back as two or three rooms. The floor has to name the arithmetic.
func TestValidateRejectsAPlaneTooSmallForADecentMap(t *testing.T) {
	cfg := MacroConfig{Seed: 1, Width: 70, Height: 44, Rooms: 4}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("a 70×44 plane was accepted")
	}
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("error = %v, want invalid configuration", err)
	}
	msg := err.Error()
	for _, piece := range []string{"70", "44", "26", "4"} {
		if !strings.Contains(msg, piece) {
			t.Fatalf("error %q does not document the floor arithmetic (missing %q)", msg, piece)
		}
	}
}

func TestGenerateMacroRejectsACanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := GenerateMacro(ctx, MacroConfig{Seed: 1, Width: 64, Height: 64, Rooms: 4})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestValidatePlaneRejectsOverlapUnpairedAndDisagreeingExit(t *testing.T) {
	base := twoRoomPlane()
	base.Rooms[1].Origin = base.Rooms[0].Origin
	err := ValidatePlane(base)
	if !errors.Is(err, ErrInvalidGeometry) {
		t.Fatalf("overlap error = %v", err)
	}

	unpaired := twoRoomPlane()
	unpaired.Rooms[0].Transitions[0].To = 9
	err = ValidatePlane(unpaired)
	if !errors.Is(err, ErrInvalidGeometry) {
		t.Fatalf("unpaired error = %v", err)
	}

	badExit := twoRoomPlane()
	badExit.Rooms[0].Transitions[0].Exit = core.DirectionWest
	err = ValidatePlane(badExit)
	if !errors.Is(err, ErrInvalidGeometry) {
		t.Fatalf("exit error = %v", err)
	}
}

func TestOneCellHoleIsNotABypass(t *testing.T) {
	plane := twoRoomPlane()
	// A single empty cell is shorter than the default body.
	setCell(&plane.Rooms[0].Grid, int32(plane.Rooms[0].Grid.Width)-1, 1, CellKindEmpty)
	setCell(&plane.Rooms[1].Grid, 0, 1, CellKindEmpty)
	edges, err := GeometricBypasses(plane, DefaultProfile())
	if err != nil {
		t.Fatalf("GeometricBypasses: %v", err)
	}
	if len(edges) != 0 {
		t.Fatalf("a 1-cell hole produced %d bypasses", len(edges))
	}
}

func TestGeometricHoleFailsTheLockAndNotTheAuthoredRoute(t *testing.T) {
	plane := twoRoomPlane()
	// Three cells of air beside the gated opening. The body fits; no
	// transition covers them.
	for _, y := range []int32{1, 2, 3} {
		setCell(&plane.Rooms[0].Grid, int32(plane.Rooms[0].Grid.Width)-1, y, CellKindEmpty)
		setCell(&plane.Rooms[1].Grid, 0, y, CellKindEmpty)
	}
	graph := linkGraph([]MotionEdge{
		{From: 0, To: 1, Kind: MotionEdgeKindTransition, Requires: NewAbilitySet(AbilityWallJump), Passage: 1, Transition: 0},
		{From: 1, To: 0, Kind: MotionEdgeKindTransition, Passage: 1, Transition: 1},
	})
	plan := ProgressionPlan{Steps: []ProgressionStep{{Name: "claw", Grants: NewAbilitySet(AbilityWallJump)}}}
	grants := []AbilityGrant{{Step: 0, Room: 0, Grants: NewAbilitySet(AbilityWallJump)}}
	locks := []RegionLock{{Step: 0, Ability: AbilityWallJump, Gate: 0, Rooms: []RoomID{1}}}
	route, err := AuditRoute(context.Background(), graph, 0, 1, plan, grants)
	if err != nil {
		t.Fatalf("AuditRoute: %v", err)
	}
	if !route.Exists {
		t.Fatal("authored route does not exist")
	}
	if route.Stages[0].GoalReachable {
		t.Fatal("authored graph reaches the goal without the ability")
	}
	lock, err := AuditLocks(context.Background(), plane, graph, 0, plan, locks, DefaultProfile())
	if err != nil {
		t.Fatalf("AuditLocks: %v", err)
	}
	if lock.Held || len(lock.Locks) != 1 || !lock.Locks[0].ViaGeometric || lock.Locks[0].AuthoredLeak {
		t.Fatalf("lock = %+v, want a geometric leak and no authored leak", lock.Locks)
	}
}

func TestAbilityBehindItsOwnGateIsNotObtainable(t *testing.T) {
	graph := linkGraph([]MotionEdge{
		{From: 0, To: 1, Kind: MotionEdgeKindTransition, Requires: NewAbilitySet(AbilityDash), Passage: 1, Transition: 0},
		{From: 1, To: 0, Kind: MotionEdgeKindTransition, Passage: 1, Transition: 1},
		{From: 0, To: 2, Kind: MotionEdgeKindTransition, Passage: 2, Transition: 2},
		{From: 2, To: 0, Kind: MotionEdgeKindTransition, Passage: 2, Transition: 3},
	})
	plan := ProgressionPlan{Steps: []ProgressionStep{{Name: "dash", Grants: NewAbilitySet(AbilityDash)}}}
	grants := []AbilityGrant{{Step: 0, Room: 1, Grants: NewAbilitySet(AbilityDash)}}
	report, err := AuditGrants(context.Background(), graph, 0, plan, grants)
	if err != nil {
		t.Fatalf("AuditGrants: %v", err)
	}
	if report.Obtainable || len(report.Grants) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if report.Grants[0].Obtainable || !report.Grants[0].OnceHeld || report.Grants[0].Ability != AbilityDash {
		t.Fatalf("finding = %+v, want dash unreachable until it is pretended owned", report.Grants[0])
	}
}

func TestPlantedPitIsASoftlockWhileTheGoalStaysReachable(t *testing.T) {
	graph := linkGraph([]MotionEdge{
		{From: 0, To: 1, Kind: MotionEdgeKindTransition, Passage: 1, Transition: 0},
		{From: 1, To: 0, Kind: MotionEdgeKindTransition, Passage: 1, Transition: 1},
		{From: 1, To: 2, Kind: MotionEdgeKindTransition, Passage: 2, Transition: 2},
		{From: 2, To: 1, Kind: MotionEdgeKindTransition, Passage: 2, Transition: 3},
		{From: 1, To: 3, Kind: MotionEdgeKindFall, Passage: 3, Transition: 4},
	})
	plan := ProgressionPlan{}
	route, err := AuditRoute(context.Background(), graph, 0, 2, plan, nil)
	if err != nil {
		t.Fatalf("AuditRoute: %v", err)
	}
	if !route.Exists || !route.Stages[0].GoalReachable {
		t.Fatal("the goal route is missing; this fixture is supposed to separate the route from the trap")
	}
	soft, err := AuditSoftlocks(context.Background(), graph, 0, 2, plan)
	if err != nil {
		t.Fatalf("AuditSoftlocks: %v", err)
	}
	if soft.Clear {
		t.Fatal("softlock audit cleared a planted pit")
	}
	var pit *HazardFinding
	for i := range soft.Hazards {
		if soft.Hazards[i].To == 3 && soft.Hazards[i].Trapped() {
			pit = &soft.Hazards[i]
		}
	}
	if pit == nil || pit.CanWin || pit.CanReturn || pit.Mandatory {
		t.Fatalf("pit = %+v, want an optional trap that neither wins nor returns", pit)
	}
}

func TestDoorPairsValidateWithoutASharedWall(t *testing.T) {
	plane := twoRoomPlane()
	// Move the rooms apart and replace the wall pair with a door pair.
	plane.Rooms[1].Origin = Cell{X: 20, Y: 0}
	plane.Width = 40
	for i := range plane.Rooms {
		for ti := range plane.Rooms[i].Transitions {
			plane.Rooms[i].Transitions[ti].Side = TransitionSideDoor
		}
	}
	plane.Rooms[0].Transitions[0].Exit = core.DirectionEast
	plane.Rooms[1].Transitions[0].Exit = core.DirectionWest
	if err := ValidatePlane(plane); err != nil {
		t.Fatalf("door pair: %v", err)
	}
}

func twoRoomPlane() Plane {
	left := Room{ID: 0, Origin: Cell{X: 0, Y: 0}, Grid: shellGrid(8, 8)}
	right := Room{ID: 1, Origin: Cell{X: 8, Y: 0}, Grid: shellGrid(8, 8)}
	// Offset 1, extent 3: air at plane Y [4, 7), landing on the bottom cell.
	left.Transitions = []Transition{{
		ID: 0, Room: 0, Side: TransitionSideRight, Index: 1, Offset: 1, Extent: OpeningExtent,
		Exit: core.DirectionEast, To: 1,
		Outbound: &Traversal{Requires: NewAbilitySet(AbilityWallJump)},
		Inbound:  &Traversal{},
	}}
	right.Transitions = []Transition{{
		ID: 1, Room: 1, Side: TransitionSideLeft, Index: 1, Offset: 1, Extent: OpeningExtent,
		Exit: core.DirectionWest, To: 0,
		Outbound: &Traversal{},
		Inbound:  &Traversal{Requires: NewAbilitySet(AbilityWallJump)},
	}}
	plane := Plane{Width: 16, Height: 8, Rooms: []Room{left, right}, Spawn: Anchor{Room: 0, At: Cell{X: 4, Y: 6}}, Goal: Anchor{Room: 1, At: Cell{X: 4, Y: 6}}}
	for i := range plane.Rooms {
		for _, tr := range plane.Rooms[i].Transitions {
			punchOpening(&plane.Rooms[i], tr)
		}
	}
	return plane
}

func linkGraph(edges []MotionEdge) *JumpGraph {
	maxNode := 0
	for _, edge := range edges {
		if int(edge.From) > maxNode {
			maxNode = int(edge.From)
		}
		if int(edge.To) > maxNode {
			maxNode = int(edge.To)
		}
	}
	profile := DefaultProfile()
	full := profile.FullResources()
	nodes := make([]MotionNode, maxNode+1)
	for i := range nodes {
		nodes[i] = MotionNode{
			ID: MotionNodeID(i), Mode: MotionModeGrounded, Velocity: Span{Lo: 0, Hi: 0},
			Resources: full, Height: 1, Footing: Point(1),
		}
	}
	for i := range edges {
		edges[i].ID = MotionEdgeID(i)
	}
	return &JumpGraph{
		Model: "daedalus/platform/macro", ProfileVersion: profile.Version,
		Discipline: NodeDisciplineRestOnly, Nodes: nodes, Edges: edges,
	}
}
