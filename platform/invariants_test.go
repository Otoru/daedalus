package platform

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
)

// qualityTol is the absolute slack on a replayed trajectory. It is wider than
// contactEpsilon, which is a root-finding cutoff, and narrower than any
// distance that would move a foot into another cell.
const qualityTol = 1e-6

// qualityConfig is the request the invariant and golden suites draw. It has
// two progression stages on purpose: a check that only walks the final
// moveset cannot see a gate the base moveset is still shut behind.
func qualityConfig(seed Seed) Config {
	return Config{
		Seed:     seed,
		Width:    96,
		Height:   64,
		MaxRooms: 4,
		Profile:  DefaultProfile(),
		Progression: ProgressionPlan{Steps: []ProgressionStep{{
			Name:   "dash",
			Grants: NewAbilitySet(AbilityDash),
		}}},
		Beats: BeatConfig{
			Definitions: []BeatDefinition{
				{Kind: BeatKindRest, MinCells: 4, MaxCells: 5, Difficulty: 0},
				{Kind: BeatKindTraverse, MinCells: 4, MaxCells: 5, Difficulty: 40},
				{Kind: BeatKindGap, MinCells: 4, MaxCells: 5, Difficulty: 120},
				{Kind: BeatKindDescend, MinCells: 3, MaxCells: 4, Difficulty: 90},
			},
			Spine: &BeatDistribution{
				Beats: []BeatWeight{
					{Kind: BeatKindRest, Weight: 1},
					{Kind: BeatKindTraverse, Weight: 2},
					{Kind: BeatKindGap, Weight: 2},
					{Kind: BeatKindDescend, Weight: 1},
				},
				MinRunBeats: 2,
				MaxRunBeats: 3,
			},
		},
	}
}

var (
	qualityOnce      sync.Once
	qualityCached    PlatformLayout
	qualityCachedErr error
)

// qualityCertified returns the seed-7 map. Callers must not mutate it: the
// value is cached and its slices are shared.
func qualityCertified(t *testing.T) PlatformLayout {
	t.Helper()
	qualityOnce.Do(func() {
		qualityCached, qualityCachedErr = Generate(context.Background(), NewM1Oracle(), qualityConfig(7))
	})
	if qualityCachedErr != nil {
		t.Fatalf("generate: %v", qualityCachedErr)
	}
	if !qualityCached.Judgement.Certified() {
		t.Fatalf("seed 7 is %s/%s (%s); the invariants need a certified map",
			qualityCached.Judgement.Verdict, qualityCached.Judgement.Reason, qualityCached.Judgement.Detail)
	}
	if len(qualityCached.JumpGraph.Edges) == 0 {
		t.Fatal("certified map has no jump edges, so a witness check would pass without looking at one")
	}
	if len(qualityCached.Plan.Stages()) < 2 {
		t.Fatal("plan has one moveset, so a final-only route check cannot be told from a per-stage check")
	}
	return qualityCached
}

// TestCertifiedEdgesReplay checks that every edge of the certified jump graph
// carries a witness whose phases integrate to the nodes the edge names.
// The room graph is authored and must not grow a witness of its own.
func TestCertifiedEdgesReplay(t *testing.T) {
	if msg := qualityWitnessError(qualityCertified(t)); msg != "" {
		t.Fatal(msg)
	}
}

// TestCertifiedEdgesDoNotCrossSolids re-solves each witness against the room
// grid. The solver here is not the oracle's sweep: a shared routine would
// stay green when the routine that certified the edge is the one that is wrong.
func TestCertifiedEdgesDoNotCrossSolids(t *testing.T) {
	if msg := qualitySolidError(qualityCertified(t)); msg != "" {
		t.Fatal(msg)
	}
}

// TestCertifiedMapsReachEachStageUnderItsMoveset walks the authored room graph
// once per cumulative moveset. The objective of an early stage is the next
// grant, then the goal; reaching the goal with the final moveset alone does
// not satisfy the test.
func TestCertifiedMapsReachEachStageUnderItsMoveset(t *testing.T) {
	if msg := qualityStageError(qualityCertified(t)); msg != "" {
		t.Fatal(msg)
	}
}

// TestNodeSpansAreUniversal requires every claim attached to a node to hold
// for the whole Footing and Velocity band. A witness that lands on one member
// of a wider band is the optimistic reading section 1.1 of the contract
// forbids.
func TestNodeSpansAreUniversal(t *testing.T) {
	if msg := qualitySpanError(qualityCertified(t)); msg != "" {
		t.Fatal(msg)
	}
}

func qualityWitnessError(layout PlatformLayout) string {
	for _, edge := range layout.RoomGraph.Edges {
		if edge.Witness != nil {
			return fmt.Sprintf("room-graph edge %d carries a witness; that graph is authored, and a witness here would make it look certified", edge.ID)
		}
	}
	graph := &layout.JumpGraph
	for index, node := range graph.Nodes {
		if node.ID != MotionNodeID(index) {
			return fmt.Sprintf("jump-graph node index %d carries id %d", index, node.ID)
		}
	}
	for index, edge := range graph.Edges {
		if edge.ID != MotionEdgeID(index) {
			return fmt.Sprintf("jump-graph edge index %d carries id %d", index, edge.ID)
		}
		if edge.Kind == MotionEdgeKindTransition {
			return fmt.Sprintf("jump-graph edge %d is a transition; crossing a room boundary is not a certified edge", edge.ID)
		}
		if int(edge.From) >= len(graph.Nodes) || int(edge.To) >= len(graph.Nodes) {
			return fmt.Sprintf("edge %d runs %d -> %d outside a graph of %d nodes", edge.ID, edge.From, edge.To, len(graph.Nodes))
		}
		if edge.Witness == nil {
			return fmt.Sprintf("edge %d (%s) is certified without a witness", edge.ID, edge.Kind)
		}
		if msg := qualityReplay(graph, edge, layout.Config.Profile); msg != "" {
			return msg
		}
	}
	return ""
}

func qualityReplay(graph *JumpGraph, edge MotionEdge, profile MovementProfile) string {
	phases := edge.Witness.Phases
	if len(phases) == 0 {
		return fmt.Sprintf("edge %d (%s) has an empty witness", edge.ID, edge.Kind)
	}
	var summed float64
	for i, phase := range phases {
		if phase.Duration < 0 {
			return fmt.Sprintf("edge %d phase %d runs backwards", edge.ID, i)
		}
		gotX := phase.Start.X + phase.Start.VX*phase.Duration + phase.AccelX*phase.Duration*phase.Duration/2
		gotY := phase.Start.Y + phase.Start.VY*phase.Duration + phase.AccelY*phase.Duration*phase.Duration/2
		gotVX := phase.Start.VX + phase.AccelX*phase.Duration
		gotVY := phase.Start.VY + phase.AccelY*phase.Duration
		if math.Abs(gotX-phase.End.X) > qualityTol || math.Abs(gotY-phase.End.Y) > qualityTol ||
			math.Abs(gotVX-phase.End.VX) > qualityTol || math.Abs(gotVY-phase.End.VY) > qualityTol {
			return fmt.Sprintf("edge %d (%s) phase %d does not integrate to its own end: got (%v, %v) vel (%v, %v), stated (%v, %v) vel (%v, %v)",
				edge.ID, edge.Kind, i, gotX, gotY, gotVX, gotVY, phase.End.X, phase.End.Y, phase.End.VX, phase.End.VY)
		}
		if i+1 < len(phases) {
			if msg := qualityBoundary(edge, i, phase, phases[i+1], profile); msg != "" {
				return msg
			}
		}
		summed += phase.Duration
	}
	if math.Abs(summed-edge.Duration) > qualityTol || math.Abs(summed-edge.Witness.Duration()) > qualityTol {
		return fmt.Sprintf("edge %d duration %v does not match its witness %v", edge.ID, edge.Duration, summed)
	}
	commands := edge.Witness.Commands
	if len(commands) == 0 {
		return fmt.Sprintf("edge %d (%s) witness has no commands, so the trajectory does not say what was held", edge.ID, edge.Kind)
	}
	if math.Abs(commands[0].At) > qualityTol {
		return fmt.Sprintf("edge %d witness commands start at %v, not at time zero", edge.ID, commands[0].At)
	}
	for i := 1; i < len(commands); i++ {
		if commands[i].At+qualityTol < commands[i-1].At {
			return fmt.Sprintf("edge %d witness commands go backwards at index %d", edge.ID, i)
		}
	}
	start := phases[0].Start
	end := phases[len(phases)-1].End
	from, to := graph.Nodes[edge.From], graph.Nodes[edge.To]
	if math.Abs(start.Y-from.Height) > qualityTol {
		return fmt.Sprintf("edge %d starts at height %v but leaves node %d at %v", edge.ID, start.Y, from.ID, from.Height)
	}
	if math.Abs(end.Y-to.Height) > qualityTol {
		return fmt.Sprintf("edge %d (%s) ends at height %v but claims node %d at %v", edge.ID, edge.Kind, end.Y, to.ID, to.Height)
	}
	// The node is the rest state. A grounded launch accelerates to its sample
	// speed before the first recorded phase, so the witness need not open at
	// the node's velocity; it must open at the node's feet and arrive at the
	// destination's feet and velocity.
	if !qualitySpanIsTheSample(from.Footing, start.X) {
		return fmt.Sprintf("edge %d leaves node %d whose footing %s is a band; the witness only establishes x=%v, and a claim on a span has to hold for every member",
			edge.ID, from.ID, from.Footing, start.X)
	}
	if !qualitySpanIsTheSample(to.Footing, end.X) || !qualitySpanIsTheSample(to.Velocity, end.VX) {
		return fmt.Sprintf("edge %d arrives at node %d footing %s velocity %s but the witness ends at x=%v vx=%v; one sample does not cover a band",
			edge.ID, to.ID, to.Footing, to.Velocity, end.X, end.VX)
	}
	return ""
}

// qualityBoundary requires position to be continuous across every phase
// boundary, and velocity to be continuous everywhere the witness does not
// record a substitution.
//
// Phase has no event field. MotionMode is the only type-level signal, and
// the only mode whose start is defined as a velocity replacement is
// MotionModeDashing (startDash writes VX, and VY when the dash suspends
// gravity, before that phase is recorded). The other writes — coyote jump,
// variable-jump cut, double-jump reset or impulse, dash exit, and the
// landing that zeroes VY — are not on the phase. They are accepted only when
// the velocity after the boundary is the value that profile event writes,
// and the component that event does not touch stays continuous.
//
// A grounded phase following a grounded phase is never one of those writes.
// A walk's accelerate/brake split and the brake after a landing live there,
// and a velocity jump between them is a bug. An apex is airborne followed
// by airborne with continuous velocity; a vertical jump there is accepted
// only when it equals a jump release or a double jump. The limit this cannot
// close: a corrupted apex whose new VY happens to equal that profile write
// is indistinguishable from the event, because the phase does not name it.
func qualityBoundary(edge MotionEdge, index int, prev, next Phase, profile MovementProfile) string {
	if math.Abs(next.Start.X-prev.End.X) > qualityTol || math.Abs(next.Start.Y-prev.End.Y) > qualityTol {
		return fmt.Sprintf("edge %d (%s) jumps in space between phases %d and %d", edge.ID, edge.Kind, index, index+1)
	}
	if math.Abs(next.Start.VX-prev.End.VX) <= qualityTol && math.Abs(next.Start.VY-prev.End.VY) <= qualityTol {
		return ""
	}
	if qualityVelocitySubstitution(prev, next, profile) {
		return ""
	}
	return fmt.Sprintf("edge %d (%s) jumps in velocity between phases %d (%s) and %d (%s): end vel (%v, %v), next starts (%v, %v). Position is continuous. This is not a dash entry, and the new velocity is not a jump, a jump release, a double jump, a dash exit or a landing written by the profile",
		edge.ID, edge.Kind, index, prev.Mode, index+1, next.Mode,
		prev.End.VX, prev.End.VY, next.Start.VX, next.Start.VY)
}

// qualityVelocitySubstitution reports whether the velocity gap between two
// phases is one of the profile's declared writes. A gap of any other size
// is refused, including on a mode pair that sometimes hosts an event.
func qualityVelocitySubstitution(prev, next Phase, profile MovementProfile) bool {
	vxHeld := math.Abs(next.Start.VX-prev.End.VX) <= qualityTol
	vyHeld := math.Abs(next.Start.VY-prev.End.VY) <= qualityTol
	switch {
	case next.Mode == MotionModeDashing:
		return qualityDashEntry(next, profile, vyHeld)
	case prev.Mode == MotionModeDashing:
		return qualityDashExit(prev, next, profile, vyHeld)
	case (prev.Mode == MotionModeCoyote || prev.Mode == MotionModeGrounded) && next.Mode == MotionModeAirborne:
		return vxHeld && math.Abs(next.Start.VY-profile.JumpVelocity) <= qualityTol
	case prev.Mode != MotionModeGrounded && next.Mode == MotionModeGrounded:
		return vxHeld && math.Abs(next.Start.VY) <= qualityTol
	case prev.Mode == MotionModeAirborne && next.Mode == MotionModeAirborne:
		return vxHeld && qualityAirSubstitution(prev, next, profile)
	default:
		return false
	}
}

// qualityDashEntry checks the velocity startDash writes at the beginning of
// a dashing phase. That mode is the witness's only record of the event.
func qualityDashEntry(next Phase, profile MovementProfile, vyHeld bool) bool {
	dash := profile.Dash
	if dash == nil {
		return false
	}
	if dash.SuspendsGravity {
		if math.Abs(next.Start.VY) > qualityTol {
			return false
		}
	} else if !vyHeld {
		return false
	}
	speed := math.Abs(next.Start.VX)
	if math.Abs(speed-dash.Speed) <= qualityTol {
		return true
	}
	return dash.ShadowSpeed > 0 && math.Abs(speed-dash.ShadowSpeed) <= qualityTol
}

// qualityDashExit checks the horizontal velocity endDash writes. The
// following phase is airborne and does not itself declare the exit; the
// previous mode is what identifies it. Vertical velocity is left alone.
func qualityDashExit(prev, next Phase, profile MovementProfile, vyHeld bool) bool {
	if !vyHeld || profile.Dash == nil {
		return false
	}
	var want float64
	switch profile.Dash.Exit {
	case DashExitModeKeep:
		want = prev.End.VX
	case DashExitModeZero:
		want = 0
	case DashExitModeClampToRun:
		want = prev.End.VX
		if want > profile.MaxRunSpeed {
			want = profile.MaxRunSpeed
		}
		if want < -profile.MaxRunSpeed {
			want = -profile.MaxRunSpeed
		}
	default:
		return false
	}
	return math.Abs(next.Start.VX-want) <= qualityTol
}

// qualityAirSubstitution checks a vertical write that happens while the
// mode stays airborne: the variable-jump cut, or a double jump. The phase
// does not say which one fired. Horizontal velocity is the caller's job.
func qualityAirSubstitution(prev, next Phase, profile MovementProfile) bool {
	got := next.Start.VY
	if variable := profile.VariableJump; variable != nil && variable.Mode == VariableJumpModeCutVelocity && prev.End.VY > qualityTol {
		if math.Abs(got-prev.End.VY*variable.CutFactor) <= qualityTol {
			return true
		}
	}
	double := profile.DoubleJump
	if double == nil {
		return false
	}
	switch double.Mode {
	case DoubleJumpModeReset:
		return math.Abs(got-double.Velocity) <= qualityTol
	case DoubleJumpModeImpulse:
		return math.Abs(got-(prev.End.VY+double.Velocity)) <= qualityTol
	default:
		return false
	}
}

// qualitySpanIsTheSample reports whether value is every member of span, not
// merely some member. A point span passes when value sits on that point.
// Span.Contains would pass for a band that only contains the witness, which
// is the optimistic reading.
func qualitySpanIsTheSample(span Span, value float64) bool {
	if span.IsEmpty() {
		return false
	}
	return math.Abs(value-span.Lo) <= qualityTol && math.Abs(value-span.Hi) <= qualityTol
}

func qualitySpanError(layout PlatformLayout) string {
	graph := &layout.JumpGraph
	body := layout.Config.Profile.BodyHeight
	for _, node := range graph.Nodes {
		if node.Footing.IsEmpty() || node.Velocity.IsEmpty() {
			return fmt.Sprintf("node %d has an empty span (footing %s, velocity %s); an empty band does not name the states the node stands for",
				node.ID, node.Footing, node.Velocity)
		}
		// A band is a universal claim. Span.Contains would accept it as soon
		// as the witness's one sample sits inside, which is the optimistic
		// reading: the sample has to be the whole band.
		if node.Footing.Length() > qualityTol {
			return fmt.Sprintf("node %d footing %s is a band; a claim on a span has to hold for every member, and one sample does not",
				node.ID, node.Footing)
		}
		if node.Velocity.Length() > qualityTol {
			return fmt.Sprintf("node %d velocity %s is a band; a claim on a span has to hold for every member, and one sample does not",
				node.ID, node.Velocity)
		}
		surface, ok := graph.Surface(node.Surface)
		if !ok {
			return fmt.Sprintf("node %d names surface %d, which is not in the graph", node.ID, node.Surface)
		}
		if int(node.Interval) >= len(surface.Intervals) {
			return fmt.Sprintf("node %d names interval %d of surface %d, which has %d",
				node.ID, node.Interval, surface.ID, len(surface.Intervals))
		}
		interval := surface.Intervals[node.Interval]
		if surface.Kind.Supports() {
			// ContainsSpan is the universal reading. Overlaps would accept a
			// footing that sticks out of the interval and only shares a point.
			if !interval.Footing.ContainsSpan(node.Footing) {
				return fmt.Sprintf("node %d footing %s is not contained in interval footing %s; overlap would accept a band the interval does not stand for",
					node.ID, node.Footing, interval.Footing)
			}
			if math.Abs(node.Height-surface.At) > qualityTol {
				return fmt.Sprintf("node %d height %v is not its surface height %v", node.ID, node.Height, surface.At)
			}
			if node.Mode == MotionModeGrounded && interval.Headroom+qualityTol < body {
				return fmt.Sprintf("node %d stands in headroom %v, below the body %v, and that headroom belongs to the whole interval",
					node.ID, interval.Headroom, body)
			}
		}
	}
	for _, edge := range graph.Edges {
		if edge.Witness == nil || len(edge.Witness.Phases) == 0 {
			continue
		}
		if int(edge.From) >= len(graph.Nodes) || int(edge.To) >= len(graph.Nodes) {
			return fmt.Sprintf("edge %d runs %d -> %d outside the node list", edge.ID, edge.From, edge.To)
		}
		start := edge.Witness.Phases[0].Start
		end := edge.Witness.Phases[len(edge.Witness.Phases)-1].End
		from, to := graph.Nodes[edge.From], graph.Nodes[edge.To]
		if !qualitySpanIsTheSample(from.Footing, start.X) {
			return fmt.Sprintf("edge %d leaves node %d whose footing %s is a band; the witness only establishes x=%v, and a claim on a span has to hold for every member",
				edge.ID, from.ID, from.Footing, start.X)
		}
		if !qualitySpanIsTheSample(to.Footing, end.X) || !qualitySpanIsTheSample(to.Velocity, end.VX) {
			return fmt.Sprintf("edge %d arrives at node %d footing %s velocity %s but the witness ends at x=%v vx=%v; one sample does not cover a band",
				edge.ID, to.ID, to.Footing, to.Velocity, end.X, end.VX)
		}
	}
	return ""
}

func qualityStageError(layout PlatformLayout) string {
	stages := layout.Plan.Stages()
	if len(stages) < 2 {
		return "plan has one moveset, so checking the goal under the final moveset cannot see an earlier gate"
	}
	if len(layout.Grants) != len(layout.Plan.Steps) {
		return fmt.Sprintf("plan has %d steps and %d grants", len(layout.Plan.Steps), len(layout.Grants))
	}
	graph := &layout.RoomGraph
	if len(graph.Nodes) != len(layout.Plane.Rooms) {
		return fmt.Sprintf("room graph has %d nodes for %d rooms", len(graph.Nodes), len(layout.Plane.Rooms))
	}
	spawn := int(layout.Plane.Spawn.Room)
	goal := int(layout.Plane.Goal.Room)
	if spawn < 0 || spawn >= len(graph.Nodes) || goal < 0 || goal >= len(graph.Nodes) {
		return fmt.Sprintf("spawn %d or goal %d is outside the room graph", spawn, goal)
	}
	final := stages[len(stages)-1]
	if !qualityRoomReachable(graph, spawn, goal, final) {
		return fmt.Sprintf("final moveset %s does not reach goal room %d from spawn %d", final, goal, spawn)
	}
	for i, moveset := range stages {
		objective := goal
		what := "goal"
		if i < len(layout.Grants) {
			objective = int(layout.Grants[i].Room)
			what = "grant"
		}
		if objective < 0 || objective >= len(graph.Nodes) {
			return fmt.Sprintf("stage %d %s room %d is outside the room graph", i, what, objective)
		}
		if qualityRoomReachable(graph, spawn, objective, moveset) {
			continue
		}
		note := ""
		if qualityRoomReachable(graph, spawn, objective, final) {
			note = fmt.Sprintf("; the final moveset %s does reach it, so checking only the complete moveset would miss this", final)
		}
		return fmt.Sprintf("stage %d moveset %s does not reach %s room %d from spawn %d%s",
			i, moveset, what, objective, spawn, note)
	}
	return ""
}

// qualityRoomReachable is a flood of the authored room graph: transition and
// fall edges whose Requires the moveset contains. It does not call AuditRoute
// and it does not read MacroAudit, both of which are produced by the same
// pass that built the map.
func qualityRoomReachable(graph *JumpGraph, spawn, objective int, moveset AbilitySet) bool {
	reached := make([]bool, len(graph.Nodes))
	if spawn < 0 || spawn >= len(reached) {
		return false
	}
	reached[spawn] = true
	queue := []int{spawn}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, edge := range graph.Edges {
			if int(edge.From) != cur || int(edge.To) >= len(reached) || reached[edge.To] {
				continue
			}
			if edge.Kind != MotionEdgeKindTransition && edge.Kind != MotionEdgeKindFall {
				continue
			}
			if !moveset.Contains(edge.Requires) {
				continue
			}
			reached[edge.To] = true
			queue = append(queue, int(edge.To))
		}
	}
	return objective >= 0 && objective < len(reached) && reached[objective]
}

type qualityRun struct {
	row    int32
	x0, x1 int32
}

func qualitySolidError(layout PlatformLayout) string {
	graph := &layout.JumpGraph
	profile := layout.Config.Profile
	blocks := make(map[RoomID][]qualityRun, len(layout.Plane.Rooms))
	grids := make(map[RoomID]Grid, len(layout.Plane.Rooms))
	for _, room := range layout.Plane.Rooms {
		grids[room.ID] = room.Grid
		blocks[room.ID] = qualityBlockingRuns(room.Grid)
	}
	for _, edge := range graph.Edges {
		if edge.Witness == nil {
			return fmt.Sprintf("edge %d (%s) is certified without a witness", edge.ID, edge.Kind)
		}
		if int(edge.From) >= len(graph.Nodes) || int(edge.To) >= len(graph.Nodes) {
			return fmt.Sprintf("edge %d runs %d -> %d outside the node list", edge.ID, edge.From, edge.To)
		}
		fromSurface := graph.Nodes[edge.From].Surface
		toSurface := graph.Nodes[edge.To].Surface
		if int(fromSurface) >= len(graph.Surfaces) || int(toSurface) >= len(graph.Surfaces) {
			return fmt.Sprintf("edge %d names a surface the graph does not have", edge.ID)
		}
		fromRoom := graph.Surfaces[fromSurface].Room
		toRoom := graph.Surfaces[toSurface].Room
		if fromRoom != toRoom {
			return fmt.Sprintf("edge %d runs from room %d to room %d; a certified edge stays inside one room", edge.ID, fromRoom, toRoom)
		}
		grid, ok := grids[fromRoom]
		if !ok {
			return fmt.Sprintf("edge %d names room %d, which is not in the plane", edge.ID, fromRoom)
		}
		if msg := qualityEdgeHitsSolid(edge, graph.Nodes[edge.From], grid, blocks[fromRoom], profile); msg != "" {
			return msg
		}
	}
	return ""
}

func qualityBlockingRuns(grid Grid) []qualityRun {
	var runs []qualityRun
	width := int32(grid.Width)
	for row := int32(0); row < int32(grid.Height); row++ {
		start := int32(-1)
		for x := int32(0); x < width; x++ {
			kind, inside := grid.At(Cell{X: x, Y: row})
			solid := inside && kind.Blocks()
			if solid && start < 0 {
				start = x
			}
			if !solid && start >= 0 {
				runs = append(runs, qualityRun{row: row, x0: start, x1: x - 1})
				start = -1
			}
		}
		if start >= 0 {
			runs = append(runs, qualityRun{row: row, x0: start, x1: width - 1})
		}
	}
	return runs
}

func qualityEdgeHitsSolid(edge MotionEdge, from MotionNode, grid Grid, runs []qualityRun, profile MovementProfile) string {
	r := profile.BodyHalfWidth
	h := profile.BodyHeight
	inset := r + profile.Margin
	departure, hasDeparture := qualityDepartureRun(from, grid, runs)
	for phaseIndex, phase := range edge.Witness.Phases {
		if phase.Duration <= timeEpsilon {
			continue
		}
		if msg := qualityPhaseHitsSolid(edge, phaseIndex, phase, grid, runs, departure, hasDeparture, r, h, inset); msg != "" {
			return msg
		}
	}
	return ""
}

// qualityDepartureRun is the solid run the edge leaves. Coyote time keeps the
// body over that run after the feet have dropped below its top, and the
// exemption has to cover the whole edge, not only the phase that still starts
// on the surface. The run's interior stays solid.
func qualityDepartureRun(from MotionNode, grid Grid, runs []qualityRun) (qualityRun, bool) {
	if from.Mode != MotionModeGrounded {
		return qualityRun{}, false
	}
	for _, run := range runs {
		top := WorldY(run.row, grid.Height)
		if math.Abs(top-from.Height) > qualityTol {
			continue
		}
		lo, hi := float64(run.x0), float64(run.x1)+1
		if from.Footing.Lo >= lo-qualityTol && from.Footing.Hi <= hi+qualityTol {
			return run, true
		}
	}
	return qualityRun{}, false
}

func qualityPhaseHitsSolid(edge MotionEdge, phaseIndex int, phase Phase, grid Grid, runs []qualityRun, departure qualityRun, hasDeparture bool, r, h, inset float64) string {
	xLo, xHi := qualityAxisRange(phase.Start.X, phase.Start.VX, phase.AccelX, phase.Duration)
	yLo, yHi := qualityAxisRange(phase.Start.Y, phase.Start.VY, phase.AccelY, phase.Duration)
	bodyX := Span{Lo: xLo - r, Hi: xHi + r}
	bodyY := Span{Lo: yLo, Hi: yHi + h}
	if msg := qualityLeavesRoom(edge, phaseIndex, phase, grid, r, h); msg != "" {
		return msg
	}
	for _, run := range runs {
		top := WorldY(run.row, grid.Height)
		bottom := WorldY(run.row+1, grid.Height)
		cellX := Span{Lo: float64(run.x0), Hi: float64(run.x1) + 1}
		if bodyX.Hi < cellX.Lo || bodyX.Lo > cellX.Hi || bodyY.Hi < bottom || bodyY.Lo > top {
			continue
		}
		boxX := Span{Lo: cellX.Lo - r, Hi: cellX.Hi + r}
		boxY := Span{Lo: bottom - h, Hi: top}
		// Leaving a ledge drops the feet through the platform they just stood
		// on. Only that run is shrunk, and only down to the footing inset, so
		// the interior of the platform still counts as solid.
		sameRun := hasDeparture && run.row == departure.row && run.x0 == departure.x0 && run.x1 == departure.x1
		if sameRun || (math.Abs(top-phase.Start.Y) <= qualityTol && phase.Start.X >= cellX.Lo-qualityTol && phase.Start.X <= cellX.Hi+qualityTol) {
			boxX = Span{Lo: cellX.Lo + inset, Hi: cellX.Hi - inset}
			if boxX.Hi-boxX.Lo <= contactEpsilon {
				continue
			}
		}
		xs := qualityInside(phase.Start.X, phase.Start.VX, phase.AccelX, boxX.Lo, boxX.Hi, phase.Duration)
		if len(xs) == 0 {
			continue
		}
		ys := qualityInside(phase.Start.Y, phase.Start.VY, phase.AccelY, boxY.Lo, boxY.Hi, phase.Duration)
		hit, ok := qualityFirstOverlap(xs, ys)
		if !ok {
			continue
		}
		state := qualityStateAt(phase, hit)
		return fmt.Sprintf("edge %d (%s) phase %d puts the body inside solid room row %d cols [%d,%d] at t=%v (feet %v, %v)",
			edge.ID, edge.Kind, phaseIndex, run.row, run.x0, run.x1, hit, state.X, state.Y)
	}
	return ""
}

func qualityLeavesRoom(edge MotionEdge, phaseIndex int, phase Phase, grid Grid, r, h float64) string {
	width := float64(grid.Width)
	height := float64(grid.Height)
	spans := []struct {
		name string
		hits []Span
	}{
		{"left of the room", qualityInside(phase.Start.X, phase.Start.VX, phase.AccelX, math.Inf(-1), r, phase.Duration)},
		{"right of the room", qualityInside(phase.Start.X, phase.Start.VX, phase.AccelX, width-r, math.Inf(1), phase.Duration)},
		{"below the room", qualityInside(phase.Start.Y, phase.Start.VY, phase.AccelY, math.Inf(-1), 0, phase.Duration)},
		{"above the room", qualityInside(phase.Start.Y+h, phase.Start.VY, phase.AccelY, height, math.Inf(1), phase.Duration)},
	}
	for _, side := range spans {
		hit, ok := qualityFirstOverlap(side.hits, []Span{{Lo: 0, Hi: phase.Duration}})
		if !ok {
			continue
		}
		state := qualityStateAt(phase, hit)
		return fmt.Sprintf("edge %d (%s) phase %d leaves the room (%s) at t=%v (feet %v, %v)",
			edge.ID, edge.Kind, phaseIndex, side.name, hit, state.X, state.Y)
	}
	return ""
}

func qualityStateAt(phase Phase, t float64) MotionState {
	return MotionState{
		X:  phase.Start.X + phase.Start.VX*t + 0.5*phase.AccelX*t*t,
		Y:  phase.Start.Y + phase.Start.VY*t + 0.5*phase.AccelY*t*t,
		VX: phase.Start.VX + phase.AccelX*t,
		VY: phase.Start.VY + phase.AccelY*t,
	}
}

func qualityAxisRange(p0, v, a, span float64) (float64, float64) {
	end := p0 + v*span + 0.5*a*span*span
	lo, hi := p0, p0
	if end < lo {
		lo = end
	}
	if end > hi {
		hi = end
	}
	if a != 0 {
		if vertex := -v / a; vertex > 0 && vertex < span {
			at := p0 + v*vertex + 0.5*a*vertex*vertex
			if at < lo {
				lo = at
			}
			if at > hi {
				hi = at
			}
		}
	}
	return lo, hi
}

// qualityInside returns the sub-intervals of [0, span] on which the quadratic
// p(t) = p0 + v t + a t^2/2 lies strictly between lo and hi. Roots cut the
// interval; a midpoint decides each piece. This is not insideTimes: the
// invariant has to be able to disagree with the oracle's solver.
func qualityInside(p0, v, a, lo, hi, span float64) []Span {
	cuts := make([]float64, 0, 6)
	cuts = append(cuts, 0, span)
	cuts = append(cuts, qualityRoots(p0-lo, v, a, span)...)
	cuts = append(cuts, qualityRoots(p0-hi, v, a, span)...)
	for i := 1; i < len(cuts); i++ {
		for j := i; j > 0 && cuts[j] < cuts[j-1]; j-- {
			cuts[j], cuts[j-1] = cuts[j-1], cuts[j]
		}
	}
	var out []Span
	for i := 0; i+1 < len(cuts); i++ {
		from, to := cuts[i], cuts[i+1]
		if to-from <= contactEpsilon {
			continue
		}
		mid := (from + to) / 2
		value := p0 + v*mid + 0.5*a*mid*mid
		// contactEpsilon is the oracle's own cutoff between a legal contact
		// and a penetration (collision.go). qualityTol is 1e-6, a million
		// times a double's ulp and a thousand times this cutoff, so using it
		// here would accept a foot that has actually entered the solid.
		// A foot 1 ulp under a surface stays outside the open interval.
		if value > lo+contactEpsilon && value < hi-contactEpsilon {
			out = append(out, Span{Lo: from, Hi: to})
		}
	}
	return out
}

func qualityRoots(c, v, a, span float64) []float64 {
	var out []float64
	add := func(t float64) {
		if t > 0 && t < span && !math.IsNaN(t) && !math.IsInf(t, 0) {
			out = append(out, t)
		}
	}
	if math.IsNaN(c) || math.IsInf(c, 0) || math.IsNaN(v) || math.IsNaN(a) {
		return nil
	}
	if a == 0 {
		if v != 0 {
			add(-c / v)
		}
		return out
	}
	discriminant := v*v - 2*a*c
	if discriminant < 0 {
		return nil
	}
	root := math.Sqrt(discriminant)
	add((-v + root) / a)
	add((-v - root) / a)
	return out
}

func qualityFirstOverlap(a, b []Span) (float64, bool) {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		lo := math.Max(a[i].Lo, b[j].Lo)
		hi := math.Min(a[i].Hi, b[j].Hi)
		if hi-lo > contactEpsilon && hi > timeEpsilon {
			if lo <= timeEpsilon {
				lo = timeEpsilon
			}
			return lo, true
		}
		if a[i].Hi < b[j].Hi {
			i++
		} else {
			j++
		}
	}
	return 0, false
}
