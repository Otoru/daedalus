package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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

// TestExampleAnchorsAreStandingCells is the anchor invariant. Spawn and goal
// are chosen on the empty shell, then the rhythm stamps over them. A certified
// example whose anchor is solid fails here, naming the file and the anchor.
func TestExampleAnchorsAreStandingCells(t *testing.T) {
	for _, name := range []string{"01-minimo", "02-travessia"} {
		t.Run(name, func(t *testing.T) {
			layout := exampleLayout(t, name)
			if msg := exampleAnchorError(layout); msg != "" {
				t.Fatalf("%s is %s/%s and %s", name, layout.Judgement.Verdict, layout.Judgement.Reason, msg)
			}
		})
	}
}

// TestExampleOpeningsAreReachable is the opening invariant. A transition the
// stamp sealed off from the playable area fails here, naming the room and the
// opening. Both ends of a pair are checked: an edge whose far mouth is sealed
// is the same lie.
func TestExampleOpeningsAreReachable(t *testing.T) {
	layout := exampleLayout(t, "01-minimo")
	if msg := exampleOpeningError(layout); msg != "" {
		t.Fatalf("01-minimo is %s/%s and %s", layout.Judgement.Verdict, layout.Judgement.Reason, msg)
	}
}

// TestExampleSideOpeningsAreDistinctDoorways is the doorway invariant. Two
// openings on the same side of a room are one ragged hole when the solid run
// between them is narrower than the doorway, and they are a parallel edge
// when both lead to the same room. Either one fails the map. The solid run
// has to be at least OpeningExtent cells: that is the width already chosen
// for a doorway, so a thinner tooth is not a second gate.
func TestExampleSideOpeningsAreDistinctDoorways(t *testing.T) {
	layout := exampleLayout(t, "01-minimo")
	if msg := exampleSideOpeningError(layout.Plane); msg != "" {
		t.Fatalf("01-minimo is %s/%s and %s", layout.Judgement.Verdict, layout.Judgement.Reason, msg)
	}
}

type exampleHeld struct {
	layout PlatformLayout
	err    error
}

var (
	exampleMu    sync.Mutex
	exampleCache = map[string]exampleHeld{}
)

func exampleLayout(t *testing.T, name string) PlatformLayout {
	t.Helper()
	exampleMu.Lock()
	defer exampleMu.Unlock()
	if held, ok := exampleCache[name]; ok {
		if held.err != nil {
			t.Fatalf("generate %s: %v", name, held.err)
		}
		return held.layout
	}
	layout, err := Generate(context.Background(), NewM1Oracle(), exampleConfig(t, name))
	exampleCache[name] = exampleHeld{layout: layout, err: err}
	if err != nil {
		t.Fatalf("generate %s: %v", name, err)
	}
	return layout
}

func exampleConfig(t *testing.T, name string) Config {
	t.Helper()
	path := filepath.Join("..", ".research", "exemplos-plataforma", name+".json")
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var file struct {
		Config struct {
			Seed            string `json:"seed"`
			Width           uint32 `json:"width"`
			Height          uint32 `json:"height"`
			MaxRooms        uint32 `json:"max_rooms"`
			BeatDefinitions []struct {
				Kind       int    `json:"kind"`
				MinCells   uint32 `json:"min_cells"`
				MaxCells   uint32 `json:"max_cells"`
				Difficulty uint8  `json:"difficulty"`
				Requires   uint32 `json:"requires"`
			} `json:"beat_definitions"`
			Spine    *exampleDistribution `json:"spine"`
			Branches *exampleDistribution `json:"branches"`
		} `json:"config"`
	}
	if err := json.Unmarshal(payload, &file); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	seed, err := strconv.ParseUint(file.Config.Seed, 10, 64)
	if err != nil {
		t.Fatalf("seed %q: %v", file.Config.Seed, err)
	}
	config := Config{
		Seed:     Seed(seed),
		Width:    file.Config.Width,
		Height:   file.Config.Height,
		MaxRooms: file.Config.MaxRooms,
		Profile:  DefaultProfile(),
	}
	for _, definition := range file.Config.BeatDefinitions {
		config.Beats.Definitions = append(config.Beats.Definitions, BeatDefinition{
			Kind:       BeatKind(definition.Kind),
			MinCells:   definition.MinCells,
			MaxCells:   definition.MaxCells,
			Difficulty: definition.Difficulty,
			Requires:   AbilitySet(definition.Requires),
		})
	}
	config.Beats.Spine = definitionFromExample(file.Config.Spine)
	config.Beats.Branches = definitionFromExample(file.Config.Branches)
	return config
}

type exampleDistribution struct {
	MinRunBeats uint32 `json:"min_run_beats"`
	MaxRunBeats uint32 `json:"max_run_beats"`
	Beats       []struct {
		Kind   int    `json:"kind"`
		Weight uint32 `json:"weight"`
	} `json:"beats"`
}

func definitionFromExample(source *exampleDistribution) *BeatDistribution {
	if source == nil {
		return nil
	}
	out := &BeatDistribution{MinRunBeats: source.MinRunBeats, MaxRunBeats: source.MaxRunBeats}
	for _, beat := range source.Beats {
		out.Beats = append(out.Beats, BeatWeight{Kind: BeatKind(beat.Kind), Weight: beat.Weight})
	}
	return out
}

// exampleAnchorError reports spawn or goal cells that are not standing
// positions under layout.Config.Profile. The column is ceil(BodyHeight) cells
// tall starting at the standing cell: that is the space above the support,
// and it is the same count the oracle uses for the body.
func exampleAnchorError(layout PlatformLayout) string {
	profile := layout.Config.Profile
	var parts []string
	for _, anchor := range []struct {
		name string
		at   Anchor
	}{{"spawn", layout.Plane.Spawn}, {"goal", layout.Plane.Goal}} {
		if msg := exampleStandError(layout.Plane, anchor.at, anchor.name, profile); msg != "" {
			parts = append(parts, msg)
		}
	}
	return joinParts(parts)
}

func exampleStandError(plane Plane, anchor Anchor, name string, profile MovementProfile) string {
	if int(anchor.Room) >= len(plane.Rooms) {
		return fmt.Sprintf("%s names room %d, which is not in the plane", name, anchor.Room)
	}
	room := plane.Rooms[anchor.Room]
	kind, ok := room.Grid.At(anchor.At)
	if !ok {
		return fmt.Sprintf("%s room %d cell (%d,%d) is outside the room", name, anchor.Room, anchor.At.X, anchor.At.Y)
	}
	if kind != CellKindEmpty && kind != CellKindClimbable {
		return fmt.Sprintf("%s room %d cell (%d,%d) is %s, not a traversable cell (empty or climbable)", name, anchor.Room, anchor.At.X, anchor.At.Y, kind)
	}
	below, ok := room.Grid.At(Cell{X: anchor.At.X, Y: anchor.At.Y + 1})
	if !ok || (below != CellKindSolid && below != CellKindSemiSolid && below != CellKindClimbable) {
		got := "outside"
		if ok {
			got = below.String()
		}
		return fmt.Sprintf("%s room %d cell (%d,%d) is not supported from below (cell beneath is %s)", name, anchor.Room, anchor.At.X, anchor.At.Y, got)
	}
	n := exampleClearance(profile)
	for i := int32(1); i < n; i++ {
		at := Cell{X: anchor.At.X, Y: anchor.At.Y - i}
		above, ok := room.Grid.At(at)
		if !ok || above.Blocks() {
			got := "outside"
			if ok {
				got = above.String()
			}
			return fmt.Sprintf("%s room %d cell (%d,%d) does not leave %d body cells (ceil(BodyHeight)=%g) free above the support; cell (%d,%d) is %s",
				name, anchor.Room, anchor.At.X, anchor.At.Y, n, profile.BodyHeight, at.X, at.Y, got)
		}
	}
	return ""
}

func exampleClearance(profile MovementProfile) int32 {
	n := int32(math.Ceil(profile.BodyHeight))
	if n < 1 {
		return 1
	}
	return n
}

// exampleOpeningError reports a transition whose mouth is not reachable from
// the room's playable component under the profile's base locomotion plus the
// abilities that room was authored for. Both ends of each pair are visited.
func exampleOpeningError(layout PlatformLayout) string {
	profile := layout.Config.Profile
	abilities := make([]AbilitySet, len(layout.Plane.Rooms))
	for _, room := range layout.Rooms {
		if int(room.Room) < len(abilities) {
			abilities[room.Room] = room.Abilities
		}
	}
	playable := make([]map[Cell]bool, len(layout.Plane.Rooms))
	for i := range layout.Plane.Rooms {
		set := abilities[i]
		if len(layout.Rooms) == 0 {
			set = layout.Config.Progression.Final()
		}
		playable[i] = examplePlayable(layout.Plane.Rooms[i].Grid, profile, set)
	}
	var parts []string
	for _, room := range layout.Plane.Rooms {
		for _, transition := range room.Transitions {
			if transition.Side == TransitionSideDoor {
				continue
			}
			if exampleOpeningReached(room, transition, playable[room.ID], profile) {
				continue
			}
			parts = append(parts, fmt.Sprintf("room %d opening %d side %s offset %d extent %d is not reachable from the playable area",
				room.ID, transition.ID, transition.Side, transition.Offset, transition.Extent))
		}
	}
	return joinParts(parts)
}

// exampleSideOpeningError reports openings that share a side and are not two
// doorways. A doorway is distinct only when both are true: the other opening
// leads to a different room, and the solid run between the air spans is at
// least OpeningExtent cells wide. Same room is a parallel edge (the player
// arrives in the same place through either hole). A thinner wall is one hole
// with a tooth, including a gap of zero, which is a single torn opening.
func exampleSideOpeningError(plane Plane) string {
	index := map[TransitionID]Transition{}
	for _, room := range plane.Rooms {
		for _, transition := range room.Transitions {
			index[transition.ID] = transition
		}
	}
	var parts []string
	for _, room := range plane.Rooms {
		bySide := map[TransitionSide][]Transition{}
		for _, transition := range room.Transitions {
			if !transition.Side.IsCardinal() {
				continue
			}
			bySide[transition.Side] = append(bySide[transition.Side], transition)
		}
		sides := make([]TransitionSide, 0, len(bySide))
		for side := range bySide {
			sides = append(sides, side)
		}
		sort.Slice(sides, func(i, j int) bool { return sides[i] < sides[j] })
		for _, side := range sides {
			group := bySide[side]
			sort.Slice(group, func(i, j int) bool {
				if group[i].Offset != group[j].Offset {
					return group[i].Offset < group[j].Offset
				}
				return group[i].ID < group[j].ID
			})
			for i := 0; i < len(group); i++ {
				for j := i + 1; j < len(group); j++ {
					a, b := group[i], group[j]
					if a.Offset > b.Offset {
						a, b = b, a
					}
					partnerA, okA := index[a.To]
					partnerB, okB := index[b.To]
					if !okA || !okB {
						parts = append(parts, fmt.Sprintf("room %d side %s openings %d and %d have no partner", room.ID, side, a.ID, b.ID))
						continue
					}
					gap := int(b.Offset) - int(a.Offset+a.Extent)
					sameRoom := partnerA.Room == partnerB.Room
					tooClose := gap < OpeningExtent
					if !sameRoom && !tooClose {
						continue
					}
					parts = append(parts, fmt.Sprintf("room %d side %s openings %d and %d offsets %d and %d extents %d and %d partners %d and %d solid gap %d",
						room.ID, side, a.ID, b.ID, a.Offset, b.Offset, a.Extent, b.Extent, partnerA.Room, partnerB.Room, gap))
				}
			}
		}
	}
	return joinParts(parts)
}

func exampleOpeningReached(room Room, transition Transition, playable map[Cell]bool, profile MovementProfile) bool {
	borders, mouths := exampleOpeningCells(room, transition)
	if len(borders) == 0 || len(mouths) == 0 {
		return false
	}
	for _, border := range borders {
		kind, ok := room.Grid.At(border)
		if !ok || kind.Blocks() {
			return false
		}
	}
	n := exampleClearance(profile)
	for _, mouth := range mouths {
		if !exampleBodyFits(room.Grid, mouth.X, mouth.Y, n) {
			continue
		}
		if playable[mouth] {
			return true
		}
	}
	return false
}

func exampleOpeningCells(room Room, transition Transition) (borders, mouths []Cell) {
	extent := int32(transition.Extent)
	if extent <= 0 {
		return nil, nil
	}
	switch transition.Side {
	case TransitionSideBottom:
		y := int32(room.Grid.Height) - 1
		for i := int32(0); i < extent; i++ {
			x := int32(transition.Offset) + i
			borders = append(borders, Cell{X: x, Y: y})
			mouths = append(mouths, Cell{X: x, Y: y - 1})
		}
	case TransitionSideTop:
		for i := int32(0); i < extent; i++ {
			x := int32(transition.Offset) + i
			borders = append(borders, Cell{X: x, Y: 0})
			mouths = append(mouths, Cell{X: x, Y: 1})
		}
	case TransitionSideLeft:
		y0 := int32(room.Grid.Height) - int32(transition.Offset) - extent
		for i := int32(0); i < extent; i++ {
			borders = append(borders, Cell{X: 0, Y: y0 + i})
			mouths = append(mouths, Cell{X: 1, Y: y0 + i})
		}
	case TransitionSideRight:
		y0 := int32(room.Grid.Height) - int32(transition.Offset) - extent
		x := int32(room.Grid.Width) - 1
		for i := int32(0); i < extent; i++ {
			borders = append(borders, Cell{X: x, Y: y0 + i})
			mouths = append(mouths, Cell{X: x - 1, Y: y0 + i})
		}
	}
	return borders, mouths
}

func examplePlayable(grid Grid, profile MovementProfile, abilities AbilitySet) map[Cell]bool {
	n := exampleClearance(profile)
	visitedStand := map[Cell]bool{}
	var best map[Cell]bool
	bestStands := -1
	var bestMin Cell
	width := int32(grid.Width)
	height := int32(grid.Height)
	for y := int32(0); y < height; y++ {
		for x := int32(0); x < width; x++ {
			start := Cell{X: x, Y: y}
			if visitedStand[start] || !exampleStandable(grid, x, y, profile) {
				continue
			}
			comp := map[Cell]bool{}
			stands := 0
			minCell := start
			queue := []Cell{start}
			comp[start] = true
			visitedStand[start] = true
			stands++
			for len(queue) > 0 {
				cur := queue[0]
				queue = queue[1:]
				for _, next := range exampleMoves(grid, cur, profile, abilities, n) {
					if comp[next] {
						continue
					}
					comp[next] = true
					queue = append(queue, next)
					if next.Y < minCell.Y || (next.Y == minCell.Y && next.X < minCell.X) {
						minCell = next
					}
					if exampleStandable(grid, next.X, next.Y, profile) {
						visitedStand[next] = true
						stands++
					}
				}
			}
			if best == nil || stands > bestStands || (stands == bestStands && (minCell.Y < bestMin.Y || (minCell.Y == bestMin.Y && minCell.X < bestMin.X))) {
				best = comp
				bestStands = stands
				bestMin = minCell
			}
		}
	}
	if best == nil {
		return map[Cell]bool{}
	}
	return best
}

func exampleStandable(grid Grid, x, y int32, profile MovementProfile) bool {
	kind, ok := grid.At(Cell{X: x, Y: y})
	if !ok || (kind != CellKindEmpty && kind != CellKindClimbable) {
		return false
	}
	below, ok := grid.At(Cell{X: x, Y: y + 1})
	if !ok || (below != CellKindSolid && below != CellKindSemiSolid && below != CellKindClimbable) {
		return false
	}
	n := exampleClearance(profile)
	for i := int32(1); i < n; i++ {
		above, ok := grid.At(Cell{X: x, Y: y - i})
		if !ok || above.Blocks() {
			return false
		}
	}
	return true
}

func exampleBodyFits(grid Grid, x, y, n int32) bool {
	for i := int32(0); i < n; i++ {
		kind, ok := grid.At(Cell{X: x, Y: y - i})
		if !ok || kind.Blocks() {
			return false
		}
	}
	return true
}

func exampleMoves(grid Grid, cur Cell, profile MovementProfile, abilities AbilitySet, n int32) []Cell {
	var out []Cell
	if exampleStandable(grid, cur.X, cur.Y, profile) {
		for _, dx := range []int32{-1, 1} {
			if exampleBodyFits(grid, cur.X+dx, cur.Y, n) {
				out = append(out, Cell{X: cur.X + dx, Y: cur.Y})
			}
		}
		out = append(out, exampleJumps(grid, cur, profile, abilities, n)...)
		if abilities.Has(AbilityDash) && profile.Dash != nil {
			out = append(out, exampleDash(grid, cur, profile, n)...)
		}
	}
	if exampleBodyFits(grid, cur.X, cur.Y+1, n) {
		out = append(out, Cell{X: cur.X, Y: cur.Y + 1})
	}
	if abilities.Has(AbilityClimb) && profile.Climb != nil {
		kind, ok := grid.At(cur)
		if ok && kind == CellKindClimbable {
			for _, dy := range []int32{-1, 1} {
				next := Cell{X: cur.X, Y: cur.Y + dy}
				nk, nok := grid.At(next)
				if nok && nk == CellKindClimbable && exampleBodyFits(grid, next.X, next.Y, n) {
					out = append(out, next)
				}
			}
		}
	}
	if abilities.Has(AbilityWallJump) && profile.WallJump != nil && exampleBodyFits(grid, cur.X, cur.Y, n) {
		if exampleBesideWall(grid, cur) {
			out = append(out, exampleJumps(grid, cur, profile, abilities, n)...)
		}
	}
	return out
}

func exampleBesideWall(grid Grid, cur Cell) bool {
	for _, dx := range []int32{-1, 1} {
		kind, ok := grid.At(Cell{X: cur.X + dx, Y: cur.Y})
		if ok && kind.Blocks() {
			return true
		}
	}
	return false
}

func exampleDash(grid Grid, cur Cell, profile MovementProfile, n int32) []Cell {
	distance := int32(math.Floor(profile.Dash.Speed * profile.Dash.Duration))
	var out []Cell
	for _, dx := range []int32{-1, 1} {
		for step := int32(1); step <= distance; step++ {
			x := cur.X + dx*step
			if !exampleBodyFits(grid, x, cur.Y, n) {
				break
			}
			out = append(out, Cell{X: x, Y: cur.Y})
		}
	}
	return out
}

func exampleJumps(grid Grid, cur Cell, profile MovementProfile, abilities AbilitySet, n int32) []Cell {
	maxPeak := profile.ApexHeight()
	if abilities.Has(AbilityDoubleJump) && profile.DoubleJump != nil {
		maxPeak += profile.ApexHeight()
	}
	peakCells := int32(math.Floor(maxPeak))
	var out []Cell
	for peak := int32(0); peak <= peakCells; peak++ {
		for dy := -peak; dy <= int32(grid.Height); dy++ {
			rise := float64(-dy)
			reach := exampleJumpReach(profile, float64(peak), rise)
			if reach < 0 {
				continue
			}
			maxDx := int32(math.Floor(reach))
			for dx := -maxDx; dx <= maxDx; dx++ {
				if dx == 0 && dy == 0 {
					continue
				}
				if math.Abs(float64(dx)) > reach {
					continue
				}
				tx, ty := cur.X+dx, cur.Y+dy
				if !exampleArcClear(grid, cur.X, cur.Y, tx, ty, cur.Y-peak, n) {
					continue
				}
				out = append(out, Cell{X: tx, Y: ty})
			}
		}
	}
	return out
}

func exampleJumpReach(profile MovementProfile, peak, rise float64) float64 {
	return shellJumpReach(profile, peak, rise)
}

func exampleArcClear(grid Grid, x, y, tx, ty, top, n int32) bool {
	if ty < top {
		top = ty
	}
	for yy := y; yy >= top; yy-- {
		if !exampleBodyFits(grid, x, yy, n) {
			return false
		}
	}
	step := int32(1)
	if tx < x {
		step = -1
	}
	for xx := x; xx != tx; xx += step {
		if !exampleBodyFits(grid, xx, top, n) {
			return false
		}
	}
	dir := int32(1)
	if ty < top {
		dir = -1
	}
	for yy := top; ; yy += dir {
		if !exampleBodyFits(grid, tx, yy, n) {
			return false
		}
		if yy == ty {
			break
		}
	}
	return true
}

func joinParts(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, part := range parts[1:] {
		out += "; " + part
	}
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
