package platform

import (
	"context"
	"math"
)

// This file assembles the directed jump graph and searches it.
//
// # Why the graph is directed, and why that is the genre
//
// Falling is free and climbing back is not. A→B existing says nothing about
// B→A, and the asymmetry is not an implementation detail to be smoothed over:
// it is where a metroidvania's one-way shortcut comes from. Every edge here is
// emitted in one sense only, by simulating a manoeuvre that starts at its tail
// and observing where it ends.
//
// # Nodes are refined, and every one of them has a point footing
//
// MotionNode's contract says a node is a SET of states and that every claim
// attached to it must hold for EVERY member. The cheapest way to honour a
// universal quantifier is to make the set a singleton, so this builder emits
// nodes whose Footing is a point sampled at the budget's LaunchResolution and
// whose Velocity is the point zero. Position, mode and resources are all
// carried, which is NodeDisciplineRefined; the subset of those nodes that
// MotionNode.IsRest accepts is exactly the coarse "one node per platform"
// graph, which is why the refinement contains the aggregation and not the
// other way round.
//
// # Why a landing is not yet a node
//
// An arc that lands somewhere is not the same thing as a character who is
// standing there. Between the two sits the braking distance, and a platform
// too short to stop on turns a pair of individually possible jumps into a
// pair that does not compose. The builder therefore converts a landing into a
// node only when the arriving speed can be shed inside the landing interval,
// and the edge carries the brake in its witness. See canStop.
//
// # Resources are part of a node's identity
//
// A dash spent is a different state from a dash in hand, so a landing that
// does not refill produces a different node from the one that does. With the
// default profile, which refills on the ground, every grounded node collapses
// to the full-resource state and the graph stays small. With a profile that
// refills only on wall contact, the spent states are real nodes and a route
// that needs a second dash before touching a wall simply is not in the graph.

// M1Oracle is the analytic movement oracle for the M1 profile: ground jump,
// fall, drop-through, static climb, coyote window, variable jump, double jump,
// dash and wall jump, each modelled as a finite sequence of
// constant-acceleration phases and checked against the geometry with
// continuous collision rather than sampled points.
//
// It is deterministic, pure and safe to share: it holds no state between
// calls, consumes no randomness, reads no clock and never lets map iteration
// reach its output.
//
// # What it guarantees and what it does not
//
// A VerdictCertified answer carries a Witness, and the witness is a real
// trajectory in the declared model: every phase respects the profile's
// accelerations, no phase puts the body inside solid geometry or a hazard,
// every landing is a strictly descending contact with footing for the whole
// body, and the arriving speed can be shed without leaving the platform.
//
// The converse does not hold. The enumeration of launch positions, launch
// velocities and mid-air event times is finite, so a manoeuvre it did not try
// is not refuted. VerdictRejected means this model excluded the manoeuvre;
// VerdictUnknown means the model did not decide it. Neither is a proof of
// impossibility, and neither should ever be shown to a player as one.
type M1Oracle struct{}

// NewM1Oracle returns the analytic oracle. It takes no configuration: every
// query carries its own profile, moveset and budget, which is what keeps one
// instance usable from several goroutines.
func NewM1Oracle() *M1Oracle { return &M1Oracle{} }

// compile-time proof that the analytic oracle satisfies the whole contract.
var _ Oracle = (*M1Oracle)(nil)

// Model returns ModelM1.
func (o *M1Oracle) Model() string { return ModelM1 }

// judge stamps a judgement with this oracle's provenance.
func (o *M1Oracle) judge(verdict Verdict, reason VerdictReason, profile MovementProfile, detail string, budget BudgetReport) Judgement {
	return judgeAs(verdict, reason, profile, detail, budget)
}

// judgeAs builds a judgement carrying the M1 model name and the profile's
// version. Provenance is not decoration: a certificate from the fake and one
// from this oracle are the same Go value apart from these two fields.
func judgeAs(verdict Verdict, reason VerdictReason, profile MovementProfile, detail string, budget BudgetReport) Judgement {
	return Judgement{
		Verdict:        verdict,
		Reason:         reason,
		Detail:         detail,
		Model:          ModelM1,
		ProfileVersion: profile.Version,
		Budget:         budget,
	}
}

// Surfaces derives the surface index of one room: floors and one-way platform
// tops first in row-major order, then the two wall orientations, then
// ceilings, then climbables. The order fixes every SurfaceID.
//
// A supporting run is split into intervals wherever the headroom or the
// hazard state changes along it, computed over real foot positions rather
// than over columns, because both conditions are properties of the whole
// body. A low ceiling therefore fragments one edge into several intervals,
// and an interval whose Footing is empty is a surface that cannot be stood on
// at all under the full-support policy — it is kept rather than dropped, so a
// consumer can see that the geometry exists and is unusable.
func (o *M1Oracle) Surfaces(ctx context.Context, query SurfaceQuery) (SurfaceResult, error) {
	if err := ctx.Err(); err != nil {
		return SurfaceResult{}, err
	}
	geom, err := newGeometry(query.Grid, query.Room, query.Profile)
	if err != nil {
		return SurfaceResult{}, err
	}
	return SurfaceResult{
		Surfaces: geom.surfaces,
		Judgement: o.judge(VerdictCertified, ReasonWitnessFound, query.Profile,
			"surfaces derived exhaustively from the grid", BudgetReport{}),
	}, nil
}

// nodeKey identifies a state exactly. It is comparable so the builder can
// intern states without ever letting the map's iteration order escape: nodes
// are emitted in creation order, which is a deterministic function of the
// expansion order.
type nodeKey struct {
	surface  SurfaceID
	interval uint32
	sample   uint32
	mode     MotionMode
	res      Resources
}

// edgeKey collapses duplicate candidates: two manoeuvres of the same kind and
// the same requirement between the same pair of states are one edge, and the
// shorter one wins.
type edgeKey struct {
	from     MotionNodeID
	to       MotionNodeID
	kind     MotionEdgeKind
	requires AbilitySet
}

// builder accumulates the graph.
type builder struct {
	geom      *geometry
	profile   MovementProfile
	abilities AbilitySet
	budget    SearchBudget
	spent     *spend
	witness   bool

	// samples holds the sampled foot positions of every surface interval,
	// indexed by surface then interval.
	samples [][][]float64

	nodes   []MotionNode
	records []nodeRecord
	index   map[nodeKey]MotionNodeID

	edges     []MotionEdge
	edgeIndex map[edgeKey]MotionEdgeID
}

// BuildGraph derives the whole directed jump graph of one room under one
// moveset.
//
// The build is a closed expansion: every occupiable interval is seeded with a
// rest state, every state is expanded once by simulating the manoeuvres its
// mode and resources allow, and the states the arcs end in are interned and
// expanded in turn. Wall clings and climbs are discovered, never seeded,
// because the only way to be on a wall is to have reached it.
//
// The graph comes back even when the judgement is VerdictUnknown. A build
// that hit a ceiling is a real partial graph: every edge in it still carries a
// witness and is still certified, and what is missing is only the knowledge of
// what else exists.
func (o *M1Oracle) BuildGraph(ctx context.Context, query GraphQuery) (GraphResult, error) {
	if err := ctx.Err(); err != nil {
		return GraphResult{}, err
	}
	budget := effectiveBudget(query.Budget)
	if err := budget.Validate(); err != nil {
		return GraphResult{}, queryError("budget: %v", err)
	}
	discipline := query.Discipline
	if discipline == NodeDisciplineUnspecified {
		discipline = NodeDisciplineRefined
	}
	if discipline == NodeDisciplineRestOnly {
		return GraphResult{Judgement: o.judge(VerdictUnknown, ReasonUnsupportedGeometry, query.Profile,
			"this oracle builds refined nodes; a rest-only graph would have to prove the reset property it does not assume", BudgetReport{})}, nil
	}
	geom, err := newGeometry(query.Grid, query.Room, query.Profile)
	if err != nil {
		return GraphResult{}, err
	}

	b := &builder{
		geom:      geom,
		profile:   query.Profile,
		abilities: query.Abilities,
		budget:    budget,
		spent:     newSpend(budget),
		witness:   !query.OmitWitness,
		index:     make(map[nodeKey]MotionNodeID),
		edgeIndex: make(map[edgeKey]MotionEdgeID),
	}
	b.sampleSurfaces()
	b.seed()
	truncated := b.expand(ctx)
	if err := ctx.Err(); err != nil {
		return GraphResult{}, err
	}
	b.walkEdges()
	b.climbEdges()

	graph := JumpGraph{
		Model:          ModelM1,
		ProfileVersion: query.Profile.Version,
		Abilities:      query.Abilities,
		Discipline:     NodeDisciplineRefined,
		Surfaces:       geom.surfaces,
		Nodes:          b.nodes,
		Edges:          b.edges,
	}
	if len(graph.Nodes) > MaxMotionNodes {
		return GraphResult{}, limitError("graph holds %d nodes, above the ceiling of %d", len(graph.Nodes), MaxMotionNodes)
	}
	if len(graph.Edges) > MaxMotionEdges {
		return GraphResult{}, limitError("graph holds %d edges, above the ceiling of %d", len(graph.Edges), MaxMotionEdges)
	}

	judgement := o.judge(VerdictCertified, ReasonWitnessFound, query.Profile,
		"every manoeuvre the model enumerates was decided", b.spent.report)
	if truncated || b.spent.exhausted {
		b.spent.report.Exhausted = true
		judgement = o.judge(VerdictUnknown, ReasonBudgetExhausted, query.Profile,
			"a search ceiling stopped the build; the graph is real but partial", b.spent.report)
	}
	return GraphResult{Graph: graph, Judgement: judgement}, nil
}

// sampleSurfaces lays the launch-position grid over every surface interval.
func (b *builder) sampleSurfaces() {
	b.samples = make([][][]float64, len(b.geom.surfaces))
	for s, surface := range b.geom.surfaces {
		b.samples[s] = make([][]float64, len(surface.Intervals))
		for i, interval := range surface.Intervals {
			b.samples[s][i] = samplePositions(interval.Footing, b.budget.LaunchResolution)
		}
	}
}

// samplePositions returns the foot positions sampled along a footing span:
// always both ends, and interior points no further apart than the resolution.
func samplePositions(footing Span, resolution float64) []float64 {
	if footing.IsEmpty() {
		return nil
	}
	if footing.Length() <= contactEpsilon {
		return []float64{footing.Lo}
	}
	if resolution <= 0 {
		resolution = footing.Length()
	}
	count := int(math.Ceil(footing.Length()/resolution)) + 1
	if count < 2 {
		count = 2
	}
	out := make([]float64, count)
	for i := range out {
		out[i] = footing.Lo + footing.Length()*float64(i)/float64(count-1)
	}
	return out
}

// occupiable reports whether an interval can hold a standing body.
func (b *builder) occupiable(surface Surface, index int) bool {
	if index < 0 || index >= len(surface.Intervals) {
		return false
	}
	interval := surface.Intervals[index]
	return !interval.Footing.IsEmpty() && !interval.Hazard && interval.Headroom >= b.profile.BodyHeight
}

// seed creates the rest state of every interval a body can stand on, and the
// resting state of every ladder rung. Ladders are seeded rather than
// discovered so that they are expanded like any other state: a character who
// can jump off a rope needs the rope to be a place the search has visited.
func (b *builder) seed() {
	full := b.profile.FullResources()
	for s := range b.geom.surfaces {
		b.seedSurface(s, full)
	}
}

func (b *builder) seedSurface(s int, full Resources) {
	surface := b.geom.surfaces[s]
	mode := MotionModeGrounded
	if surface.Kind == SurfaceKindClimbable {
		if b.profile.Climb == nil || !b.abilities.Has(AbilityClimb) {
			return
		}
		mode = MotionModeClimbing
	} else if !surface.Kind.Supports() {
		return
	}
	for i := range surface.Intervals {
		if mode == MotionModeClimbing && surface.Intervals[i].Footing.IsEmpty() || mode == MotionModeGrounded && !b.occupiable(surface, i) {
			continue
		}
		for sample := range b.samples[s][i] {
			b.intern(nodeKey{surface: surface.ID, interval: uint32(i), sample: uint32(sample), mode: mode, res: full})
		}
	}
}

// intern returns the node for a state, creating it the first time it is seen.
func (b *builder) intern(key nodeKey) MotionNodeID {
	if id, ok := b.index[key]; ok {
		return id
	}
	surface := b.geom.surfaces[key.surface]
	x := b.samples[key.surface][key.interval][key.sample]
	height := surface.At
	footing := Point(x)
	if key.mode == MotionModeWallCling || key.mode == MotionModeClimbing {
		height = x
		footing = Point(surface.At)
		if key.mode == MotionModeWallCling {
			footing = Point(b.clingFoot(surface))
		}
	}
	id := MotionNodeID(len(b.nodes))
	node := MotionNode{
		ID:        id,
		Surface:   key.surface,
		Interval:  key.interval,
		Height:    height,
		Footing:   footing,
		Velocity:  Point(0),
		Mode:      key.mode,
		Resources: key.res,
	}
	record := nodeRecord{node: node, exempt: b.geom.departureOf(key.surface)}
	switch key.mode {
	case MotionModeWallCling:
		record.wall = WallSideRight
		if surface.Kind == SurfaceKindWallRight {
			record.wall = WallSideLeft
		}
		record.walkable = surface.Intervals[key.interval].Footing
	case MotionModeClimbing:
		record.climb = true
		record.walkable = surface.Intervals[key.interval].Footing
	default:
		record.semi = surface.Kind == SurfaceKindSemiSolid
		record.walkable = b.geom.walkableSpan(surface, key.interval)
		record.ledgeLo = x <= record.walkable.Lo+contactEpsilon
		record.ledgeHi = x >= record.walkable.Hi-contactEpsilon
		record.site = landingSite{
			surface:  key.surface,
			interval: key.interval,
			at:       surface.At,
			footing:  surface.Intervals[key.interval].Footing,
			semi:     record.semi,
		}
	}
	b.index[key] = id
	b.nodes = append(b.nodes, node)
	b.records = append(b.records, record)
	return id
}

// clingFoot returns the foot x of a body held against a wall face.
func (b *builder) clingFoot(surface Surface) float64 {
	inset := b.profile.BodyHalfWidth + b.profile.Margin
	if surface.Kind == SurfaceKindWallRight {
		return surface.At + inset
	}
	return surface.At - inset
}

// expand simulates every state's manoeuvres until no new state appears.
// It reports whether a ceiling stopped it.
func (b *builder) expand(ctx context.Context) bool {
	ceiling := flightCeiling(b.profile, b.budget)
	truncated := false
	for cursor := 0; cursor < len(b.records); cursor++ {
		if err := ctx.Err(); err != nil {
			return true
		}
		if !b.spent.expansion() {
			return true
		}
		record := b.records[cursor]
		horizon := record.node.Height - b.budget.FallHorizon
		b.geom.enumerate(record, b.abilities, b.budget, b.spent, func(plan launch) bool {
			return b.expandCandidate(record, plan, ceiling, horizon, &truncated)
		})
		if b.spent.exhausted {
			return true
		}
	}
	return truncated
}

func (b *builder) expandCandidate(record nodeRecord, plan launch, ceiling, horizon float64, truncated *bool) bool {
	if !b.abilities.Contains(plan.requires) {
		return true
	}
	if !b.spent.candidate() {
		*truncated = true
		return false
	}
	// Candidates are flown without recording; successful ones are replayed.
	result := b.geom.fly(b.abilities, plan, ceiling, horizon, false, b.spent)
	if result.truncated {
		*truncated = true
	}
	if b.spent.exhausted {
		*truncated = true
		return false
	}
	b.record(record, plan, result)
	return true
}

// record turns one flown candidate into an edge, when it ended somewhere the
// character can actually be.
func (b *builder) record(from nodeRecord, plan launch, result flightResult) {
	switch result.kind {
	case contactLanding:
		b.recordLanding(from, plan, result)
	case contactBlocked:
		b.recordCling(from, plan, result)
	}
}

// replay re-flies a candidate with recording on, to attach the witness of an
// edge that is about to be emitted. It runs against an unmetered budget
// because the candidate has already been charged once and the outcome is
// known to be the same.
func (b *builder) replay(plan launch, horizon float64) *Witness {
	if !b.witness {
		return nil
	}
	free := &spend{budget: b.budget}
	free.budget.MaxCandidateEdges = 0
	free.budget.MaxCollisionTests = 0
	free.budget.MaxExpandedNodes = 0
	return b.geom.fly(b.abilities, plan, flightCeiling(b.profile, b.budget), horizon, true, free).witness
}

// recordLanding emits the edge of an arc that came down on a platform.
func (b *builder) recordLanding(from nodeRecord, plan launch, result flightResult) {
	site := result.site
	if !canStop(b.profile, site.footing, result.state.X, result.state.VX) {
		return
	}
	if !math.IsInf(b.profile.MaxSafeFallHeight, 1) && plan.y0-result.state.Y > b.profile.MaxSafeFallHeight {
		return
	}
	sample, ok := b.nearestSample(site.surface, site.interval, result.state.X)
	if !ok {
		return
	}
	resources := groundResources(b.profile, result.resources, result.duration)
	to := b.intern(nodeKey{
		surface:  site.surface,
		interval: site.interval,
		sample:   uint32(sample),
		mode:     MotionModeGrounded,
		res:      resources,
	})
	target := b.samples[site.surface][site.interval][sample]
	approach, extra := groundApproach(b.profile, result.state, target)
	result.witness = b.replay(plan, plan.y0-b.budget.FallHorizon)
	b.emit(from.node.ID, to, plan, result, extra, approach)
}

// recordCling emits the edge of an arc that ended against a wall the moveset
// can hold on to. Without the wall-jump ability the arc simply hit rock and
// there is no edge.
func (b *builder) recordCling(from nodeRecord, plan launch, result flightResult) {
	if result.wall == WallSideNone || !b.abilities.Has(AbilityWallJump) || b.profile.WallJump == nil {
		return
	}
	surface, interval, ok := b.geom.wallAt(result.wall, result.wallX, result.state.Y)
	if !ok {
		return
	}
	sample, ok := b.sampleBelow(surface, interval, result.state.Y)
	if !ok {
		return
	}
	resources := clingResources(b.profile, result.resources, result.wall, result.duration)
	to := b.intern(nodeKey{
		surface:  surface,
		interval: interval,
		sample:   uint32(sample),
		mode:     MotionModeWallCling,
		res:      resources,
	})
	// An edge is named by the manoeuvre that STARTED it. A leap off one wall
	// that ends against the next one is a wall jump; an ordinary jump or fall
	// that ends against a wall is an attachment.
	cling := plan
	if cling.kind != MotionEdgeKindWallJump {
		cling.kind = MotionEdgeKindWallCling
	}
	cling.requires = plan.requires.With(AbilityWallJump)
	slide, extra := b.slide(result, b.samples[surface][interval][sample], b.nodes[to].Footing.Lo)
	result.witness = b.replay(plan, plan.y0-b.budget.FallHorizon)
	b.emit(from.node.ID, to, cling, result, extra, slide)
}

// sampleBelow returns the sampled height of a wall interval at or below a
// contact. A cling slides DOWN the face at the profile's slide speed and
// never up it, so snapping upward would invent motion the character has no
// way to perform.
func (b *builder) sampleBelow(surface SurfaceID, interval uint32, at float64) (int, bool) {
	if int(surface) >= len(b.samples) || int(interval) >= len(b.samples[surface]) {
		return 0, false
	}
	list := b.samples[surface][interval]
	best := -1
	for i, value := range list {
		if value > at+contactEpsilon {
			continue
		}
		if best < 0 || value > list[best] {
			best = i
		}
	}
	if best < 0 {
		return 0, false
	}
	return best, true
}

// slide returns the phase that lowers a cling from the contact height to the
// node it was snapped to, and how long it takes.
func (b *builder) slide(result flightResult, target, footX float64) ([]Phase, float64) {
	drop := result.state.Y - target
	if drop <= contactEpsilon {
		return nil, 0
	}
	speed := 0.0
	if b.profile.WallJump != nil {
		speed = b.profile.WallJump.SlideSpeed
	}
	if speed <= 0 {
		return nil, 0
	}
	duration := drop / speed
	return []Phase{{
		Mode:     MotionModeWallCling,
		Start:    MotionState{X: footX, Y: result.state.Y, VY: -speed},
		End:      MotionState{X: footX, Y: target, VY: -speed},
		Duration: duration,
	}}, duration
}

// nearestSample returns the sampled position of an interval closest to a
// coordinate. Snapping is justified by the walk edges: every sample of one
// walkable run is reachable from every other on foot, and the braking rule
// has already proved the character can stop inside the interval.
func (b *builder) nearestSample(surface SurfaceID, interval uint32, at float64) (int, bool) {
	if int(surface) >= len(b.samples) || int(interval) >= len(b.samples[surface]) {
		return 0, false
	}
	list := b.samples[surface][interval]
	if len(list) == 0 {
		return 0, false
	}
	best, distance := 0, math.Inf(1)
	for i, value := range list {
		if d := math.Abs(value - at); d < distance {
			best, distance = i, d
		}
	}
	return best, true
}

// groundApproach returns the phases that brake the arriving body to rest and
// reposition it on the node it was snapped to, and how long that takes. It is
// part of the witness because the edge claims the character ends at the node,
// not merely somewhere on the platform.
func groundApproach(profile MovementProfile, landed MotionState, target float64) ([]Phase, float64) {
	var phases []Phase
	elapsed := 0.0
	state := MotionState{X: landed.X, Y: landed.Y, VX: landed.VX}
	if math.Abs(state.VX) > contactEpsilon && profile.Braking > 0 {
		duration := math.Abs(state.VX) / profile.Braking
		stop := state.X + math.Copysign(profile.StoppingDistance(math.Abs(state.VX)), state.VX)
		end := MotionState{X: stop, Y: state.Y}
		phases = append(phases, Phase{
			Mode:     MotionModeGrounded,
			Start:    state,
			End:      end,
			Duration: duration,
			AccelX:   -math.Copysign(profile.Braking, state.VX),
		})
		elapsed += duration
		state = end
	}
	if math.Abs(target-state.X) > contactEpsilon {
		walk := walkWitness(profile, state.X, target, state.Y, walkDuration(profile, target-state.X))
		phases = append(phases, walk.Phases...)
		elapsed += walkDuration(profile, target-state.X)
	}
	return phases, elapsed
}

// emit adds an edge, keeping the shorter of two manoeuvres that are otherwise
// the same.
func (b *builder) emit(from, to MotionNodeID, plan launch, result flightResult, extra float64, approach []Phase) {
	if from == to {
		return
	}
	key := edgeKey{from: from, to: to, kind: plan.kind, requires: plan.requires}
	duration := result.duration + extra
	if id, ok := b.edgeIndex[key]; ok {
		if duration >= b.edges[id].Duration {
			return
		}
		b.edges[id].Duration = duration
		b.edges[id].Witness = b.assemble(result, approach)
		return
	}
	id := MotionEdgeID(len(b.edges))
	b.edgeIndex[key] = id
	b.edges = append(b.edges, MotionEdge{
		ID:       id,
		From:     from,
		To:       to,
		Kind:     plan.kind,
		Requires: plan.requires,
		Duration: duration,
		Witness:  b.assemble(result, approach),
	})
}

// assemble appends the approach phases to a flight's witness.
func (b *builder) assemble(result flightResult, approach []Phase) *Witness {
	if !b.witness || result.witness == nil {
		return nil
	}
	if len(approach) == 0 {
		return result.witness
	}
	phases := make([]Phase, 0, len(result.witness.Phases)+len(approach))
	phases = append(phases, result.witness.Phases...)
	phases = append(phases, approach...)
	return &Witness{
		Phases:      phases,
		Commands:    result.witness.Commands,
		ControlRate: result.witness.ControlRate,
	}
}

// walkEdges connects the grounded nodes a character can reach on foot: those
// at the same height whose foot positions are adjacent in the sorted order and
// close enough that the body crosses the gap between them.
//
// The gap tolerance is one launch sample plus twice the footing inset, which
// is exactly the distance between the last foot position of one supporting run
// and the first of the run that touches it. Two runs with a real hole between
// them are further apart than that and stay unconnected.
func (b *builder) walkEdges() {
	inset := b.profile.BodyHalfWidth + b.profile.Margin
	tolerance := b.budget.LaunchResolution + 2*inset + contactEpsilon
	order := b.groundedByHeight()
	for _, group := range order {
		for i := 0; i+1 < len(group); i++ {
			left, right := group[i], group[i+1]
			a, bNode := b.nodes[left], b.nodes[right]
			if a.Resources != bNode.Resources {
				continue
			}
			gap := bNode.Footing.Lo - a.Footing.Lo
			if gap > tolerance {
				continue
			}
			if !b.geom.walkClear(a.Footing.Lo, bNode.Footing.Lo, a.Height) {
				continue
			}
			duration := walkDuration(b.profile, gap)
			b.walkPair(left, right, duration)
			b.walkPair(right, left, duration)
		}
	}
}

// walkPair adds one sense of a walk.
func (b *builder) walkPair(from, to MotionNodeID, duration float64) {
	key := edgeKey{from: from, to: to, kind: MotionEdgeKindWalk}
	if _, ok := b.edgeIndex[key]; ok {
		return
	}
	id := MotionEdgeID(len(b.edges))
	b.edgeIndex[key] = id
	edge := MotionEdge{ID: id, From: from, To: to, Kind: MotionEdgeKindWalk, Duration: duration}
	if b.witness {
		a, c := b.nodes[from], b.nodes[to]
		edge.Witness = walkWitness(b.profile, a.Footing.Lo, c.Footing.Lo, a.Height, duration)
	}
	b.edges = append(b.edges, edge)
}

// groundedByHeight groups the grounded nodes by world height, each group
// sorted by foot position. Grouping is done over a slice and an insertion
// sort, never over a map, so the order is the same on every machine.
func (b *builder) groundedByHeight() [][]MotionNodeID {
	type bucket struct {
		height float64
		nodes  []MotionNodeID
	}
	var buckets []bucket
	for _, node := range b.nodes {
		if node.Mode != MotionModeGrounded {
			continue
		}
		placed := false
		for i := range buckets {
			if math.Abs(buckets[i].height-node.Height) <= contactEpsilon {
				buckets[i].nodes = append(buckets[i].nodes, node.ID)
				placed = true
				break
			}
		}
		if !placed {
			buckets = append(buckets, bucket{height: node.Height, nodes: []MotionNodeID{node.ID}})
		}
	}
	out := make([][]MotionNodeID, 0, len(buckets))
	for _, group := range buckets {
		out = append(out, b.sortFootings(group.nodes))
	}
	return out
}

func (b *builder) sortFootings(ids []MotionNodeID) []MotionNodeID {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && b.nodes[ids[j]].Footing.Lo < b.nodes[ids[j-1]].Footing.Lo; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
	return ids
}

// climbEdges connects ladders and ropes: a grounded node beside one attaches
// to it, consecutive heights on one climbable move into each other, and a
// climbable that reaches a supporting surface lets the character step off.
//
// Climbing is static. A rope that swings is a dynamic body and is not this,
// and a profile that asks for one is answered VerdictUnknown rather than
// approximated.
func (b *builder) climbEdges() {
	if b.profile.Climb == nil || !b.abilities.Has(AbilityClimb) {
		return
	}
	requires := NewAbilitySet(AbilityClimb)
	capture := b.profile.Climb.CaptureHalfWidth
	full := b.profile.FullResources()

	for s := range b.geom.surfaces {
		surface := b.geom.surfaces[s]
		if surface.Kind != SurfaceKindClimbable {
			continue
		}
		for i := range surface.Intervals {
			if surface.Intervals[i].Footing.IsEmpty() {
				continue
			}
			b.climbInterval(s, i, surface, full, capture, requires)
		}
	}
}

func (b *builder) climbInterval(s, i int, surface Surface, full Resources, capture float64, requires AbilitySet) {
	ladder := make([]MotionNodeID, 0, len(b.samples[s][i]))
	for sample := range b.samples[s][i] {
		id, ok := b.index[nodeKey{surface: surface.ID, interval: uint32(i), sample: uint32(sample), mode: MotionModeClimbing, res: full}]
		if ok {
			ladder = append(ladder, id)
		}
	}
	for k := 0; k+1 < len(ladder); k++ {
		lower, upper := ladder[k], ladder[k+1]
		rise := math.Abs(b.nodes[upper].Height - b.nodes[lower].Height)
		duration := math.Inf(1)
		if b.profile.Climb.Speed > 0 {
			duration = rise / b.profile.Climb.Speed
		}
		b.climbPair(lower, upper, requires, duration)
		b.climbPair(upper, lower, requires, duration)
	}
	for _, id := range ladder {
		b.attachClimb(id, surface, capture, requires)
	}
}

// attachClimb connects one ladder node to every grounded node within the
// capture corridor at the same height.
func (b *builder) attachClimb(id MotionNodeID, ladder Surface, capture float64, requires AbilitySet) {
	height := b.nodes[id].Height
	for _, node := range b.nodes {
		if node.Mode != MotionModeGrounded {
			continue
		}
		if math.Abs(node.Height-height) > contactEpsilon {
			continue
		}
		if math.Abs(node.Footing.Lo-ladder.At) > capture+b.profile.BodyHalfWidth {
			continue
		}
		duration := walkDuration(b.profile, node.Footing.Lo-ladder.At)
		b.climbPair(node.ID, id, requires, duration)
		b.climbPair(id, node.ID, requires, duration)
	}
}

// climbPair adds one sense of a climb edge.
func (b *builder) climbPair(from, to MotionNodeID, requires AbilitySet, duration float64) {
	if from == to || math.IsInf(duration, 1) {
		return
	}
	key := edgeKey{from: from, to: to, kind: MotionEdgeKindClimb, requires: requires}
	if _, ok := b.edgeIndex[key]; ok {
		return
	}
	id := MotionEdgeID(len(b.edges))
	b.edgeIndex[key] = id
	b.edges = append(b.edges, MotionEdge{
		ID:       id,
		From:     from,
		To:       to,
		Kind:     MotionEdgeKindClimb,
		Requires: requires,
		Duration: duration,
		Witness:  b.climbWitness(from, to, duration),
	})
}

// climbWitness records a climb. Moving along the rope is one phase at the
// profile's constant climb speed; stepping on or off it is the ordinary
// accelerate-and-brake pair, recorded in the climbing mode so a consumer can
// see which part of the move is on the rope.
func (b *builder) climbWitness(from, to MotionNodeID, duration float64) *Witness {
	if !b.witness || duration <= 0 {
		return nil
	}
	a, c := b.nodes[from], b.nodes[to]
	if math.Abs(c.Height-a.Height) <= contactEpsilon {
		walk := walkWitness(b.profile, a.Footing.Lo, c.Footing.Lo, a.Height, duration)
		for i := range walk.Phases {
			walk.Phases[i].Mode = MotionModeClimbing
		}
		return walk
	}
	speed := (c.Height - a.Height) / duration
	hold := InputUp
	if speed < 0 {
		hold = InputDown
	}
	return &Witness{
		Phases: []Phase{{
			Mode:     MotionModeClimbing,
			Start:    MotionState{X: a.Footing.Lo, Y: a.Height, VY: speed},
			End:      MotionState{X: c.Footing.Lo, Y: c.Height, VY: speed},
			Duration: duration,
		}},
		Commands:    []Command{{At: 0, Hold: hold}, {At: duration, Hold: 0}},
		ControlRate: b.profile.ControlRate,
	}
}

// FindRoute searches a built graph for a path under a moveset.
//
// The search is a breadth-first expansion over the edge list in ascending
// MotionEdgeID order, so the route returned for a given graph and moveset is
// always the same one. Edges the moveset cannot use are skipped by
// MotionEdge.Reachable, which is the whole of progression filtering: one
// graph answers every stage of a plan.
//
// A graph with no path gives VerdictRejected with ReasonDisconnected, and that
// rejection is about THIS graph. It says nothing about geometry the graph does
// not contain, which is why a caller comparing stages must compare graphs
// built from the same geometry.
func (o *M1Oracle) FindRoute(ctx context.Context, query RouteQuery) (RouteResult, error) {
	if err := validateRouteQuery(ctx, query); err != nil {
		return RouteResult{}, err
	}
	graph := query.Graph
	budget := effectiveBudget(query.Budget)
	spent := newSpend(budget)
	profile := MovementProfile{Version: graph.ProfileVersion}

	if query.From == query.To {
		return RouteResult{
			Route:     Route{From: query.From, To: query.To},
			Judgement: o.judge(VerdictCertified, ReasonTrivial, profile, "the route is the empty path", spent.report),
		}, nil
	}

	adjacency := routeAdjacency(graph)

	const unvisited = -1
	cameFrom := make([]int, len(graph.Nodes))
	for i := range cameFrom {
		cameFrom[i] = unvisited
	}
	queue := []MotionNodeID{query.From}
	cameFrom[query.From] = int(query.From)
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return RouteResult{}, err
		}
		current := queue[0]
		queue = queue[1:]
		if !spent.expansion() {
			return RouteResult{Judgement: o.judge(VerdictUnknown, ReasonBudgetExhausted, profile,
				"the route search ran out of expansions", spent.report)}, nil
		}
		if routeStep(graph, query, adjacency[current], cameFrom, &queue) {
			return RouteResult{
				Route:     rebuildRoute(graph, cameFrom, query.From, query.To),
				Judgement: o.judge(VerdictCertified, ReasonWitnessFound, profile, "route found", spent.report),
			}, nil
		}
	}
	return RouteResult{Judgement: o.judge(VerdictRejected, ReasonDisconnected, profile,
		"the graph holds no route between the two nodes under this moveset", spent.report)}, nil
}

func validateRouteQuery(ctx context.Context, query RouteQuery) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if query.Graph == nil {
		return queryError("route query has no graph")
	}
	graph := query.Graph
	if int(query.From) >= len(graph.Nodes) {
		return queryError("route starts at node %d of %d", query.From, len(graph.Nodes))
	}
	if int(query.To) >= len(graph.Nodes) {
		return queryError("route ends at node %d of %d", query.To, len(graph.Nodes))
	}
	// The moveset must be comparable with the graph's own.
	if !graph.Abilities.Contains(query.Abilities) && !query.Abilities.Contains(graph.Abilities) {
		return queryError("route moveset %s is not comparable with the graph's %s", query.Abilities, graph.Abilities)
	}
	return nil
}

func routeAdjacency(graph *JumpGraph) [][]MotionEdgeID {
	adjacency := make([][]MotionEdgeID, len(graph.Nodes))
	for _, edge := range graph.Edges {
		if int(edge.From) < len(adjacency) {
			adjacency[edge.From] = append(adjacency[edge.From], edge.ID)
		}
	}
	return adjacency
}

func routeStep(graph *JumpGraph, query RouteQuery, edges []MotionEdgeID, cameFrom []int, queue *[]MotionNodeID) bool {
	const unvisited = -1
	for _, id := range edges {
		edge := graph.Edges[id]
		if !edge.Reachable(query.Abilities) || cameFrom[edge.To] != unvisited {
			continue
		}
		cameFrom[edge.To] = int(id)
		if edge.To == query.To {
			return true
		}
		*queue = append(*queue, edge.To)
	}
	return false
}

// rebuildRoute walks the predecessor edges back to the start.
func rebuildRoute(graph *JumpGraph, cameFrom []int, from, to MotionNodeID) Route {
	var reversed []MotionEdgeID
	route := Route{From: from, To: to}
	for node := to; node != from; {
		id := MotionEdgeID(cameFrom[node])
		edge := graph.Edges[id]
		reversed = append(reversed, id)
		route.Requires = route.Requires.Union(edge.Requires)
		route.Duration += edge.Duration
		node = edge.From
	}
	route.Edges = make([]MotionEdgeID, 0, len(reversed))
	for i := len(reversed) - 1; i >= 0; i-- {
		route.Edges = append(route.Edges, reversed[i])
	}
	return route
}
