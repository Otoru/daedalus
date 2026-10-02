package platform

import (
	"context"
	"math"
)

// FakeOracle is a deterministic stand-in for the real movement oracle, so
// that the rhythm, macro, wire and synthesis work can be written and tested
// before the analytic model exists. It ships in the package rather than in a
// test file precisely so that other packages can depend on it.
//
// # It is not sound, and that is the point
//
// The default behaviour is a naive envelope model. It uses the closed-form
// ballistic reach of the profile and NOTHING else: no continuous collision
// test, no body, no ceiling, no intermediate landing, no semi-solid rule, no
// braking distance on arrival. It will happily certify a jump straight through
// a wall.
//
// Every Judgement it produces therefore carries Model == ModelFake. A consumer
// that treats a certificate as binding compares the model name and refuses
// this one. Treating a fake certificate as a guarantee is the one failure this
// type is designed to make visible rather than convenient.
//
// # Overriding
//
// Each hook replaces one method. A nil hook uses the default. A test that
// needs a specific answer — a jump that must be rejected, a budget that must
// run out — sets the hook and leaves the rest alone.
//
// The zero FakeOracle is usable and uses DefaultProfile's shape only where a
// query does not carry its own profile, which in practice is never: every
// query carries one.
type FakeOracle struct {
	// SurfacesFunc replaces Surfaces.
	SurfacesFunc func(ctx context.Context, query SurfaceQuery) (SurfaceResult, error)
	// CheckEdgeFunc replaces CheckEdge.
	CheckEdgeFunc func(ctx context.Context, query EdgeQuery) (EdgeResult, error)
	// BuildGraphFunc replaces BuildGraph.
	BuildGraphFunc func(ctx context.Context, query GraphQuery) (GraphResult, error)
	// FindRouteFunc replaces FindRoute.
	FindRouteFunc func(ctx context.Context, query RouteQuery) (RouteResult, error)
	// MaxHeadroom caps how far above a surface the default derivation looks
	// for a ceiling, in cells. Zero requests 16.
	MaxHeadroom int32
}

// compile-time proof that the fake satisfies the whole contract.
var _ Oracle = (*FakeOracle)(nil)

// Model returns ModelFake.
func (f *FakeOracle) Model() string { return ModelFake }

func (f *FakeOracle) judgement(verdict Verdict, reason VerdictReason, profile MovementProfile, detail string, budget BudgetReport) Judgement {
	return Judgement{
		Verdict:        verdict,
		Reason:         reason,
		Detail:         detail,
		Model:          ModelFake,
		ProfileVersion: profile.Version,
		Budget:         budget,
	}
}

// Surfaces derives floor and semi-solid surfaces by scanning the grid for
// maximal horizontal runs of supporting cells with a free cell above.
//
// The derivation is crude on purpose: it emits exactly one interval per
// surface, it does not split an interval where the headroom changes, and it
// does not split one where a hazard begins. The real deriver must do all
// three. Walls, ceilings and ladders are not derived at all.
func (f *FakeOracle) Surfaces(ctx context.Context, query SurfaceQuery) (SurfaceResult, error) {
	if f.SurfacesFunc != nil {
		return f.SurfacesFunc(ctx, query)
	}
	if err := ctx.Err(); err != nil {
		return SurfaceResult{}, err
	}
	if err := validateGrid(query.Grid); err != nil {
		return SurfaceResult{}, err
	}

	profile := query.Profile
	var surfaces []Surface
	for row := int32(1); row < int32(query.Grid.Height); row++ {
		start := int32(-1)
		for column := int32(0); column <= int32(query.Grid.Width); column++ {
			support := false
			kind := CellKindEmpty
			if column < int32(query.Grid.Width) {
				below, _ := query.Grid.At(Cell{X: column, Y: row})
				above, _ := query.Grid.At(Cell{X: column, Y: row - 1})
				kind = below
				support = below.Supports() && !above.Blocks()
			}
			switch {
			case support && start < 0:
				start = column
			case !support && start >= 0:
				surfaces = append(surfaces, f.surfaceRun(query, profile, kind, row, start, column-1, SurfaceID(len(surfaces))))
				start = -1
			}
		}
	}
	return SurfaceResult{
		Surfaces: surfaces,
		Judgement: f.judgement(VerdictCertified, ReasonWitnessFound, profile,
			"fake derivation: one interval per run, no headroom or hazard fragmentation", BudgetReport{}),
	}, nil
}

func (f *FakeOracle) surfaceRun(query SurfaceQuery, profile MovementProfile, kind CellKind, row, from, to int32, id SurfaceID) Surface {
	extent := Span{Lo: float64(from), Hi: float64(to + 1)}
	surfaceKind := SurfaceKindFloor
	if kind == CellKindSemiSolid {
		surfaceKind = SurfaceKindSemiSolid
	}
	hazard := false
	headroom := f.headroomLimit()
	for column := from; column <= to; column++ {
		if cell, _ := query.Grid.At(Cell{X: column, Y: row - 1}); cell == CellKindHazard {
			hazard = true
		}
		free := int32(0)
		for probe := row - 1; probe >= 0 && free < f.headroomLimit(); probe-- {
			if cell, _ := query.Grid.At(Cell{X: column, Y: probe}); cell.Blocks() {
				break
			}
			free++
		}
		if free < headroom {
			headroom = free
		}
	}
	return Surface{
		ID:     id,
		Room:   query.Room,
		Kind:   surfaceKind,
		Extent: extent,
		At:     WorldY(row, query.Grid.Height),
		Intervals: []SurfaceInterval{{
			Footing:  profile.Footing(extent),
			Headroom: float64(headroom),
			Hazard:   hazard,
		}},
	}
}

func (f *FakeOracle) headroomLimit() int32 {
	if f.MaxHeadroom > 0 {
		return f.MaxHeadroom
	}
	return 16
}

// CheckEdge answers one manoeuvre with the naive ballistic envelope.
//
// It decides the horizontal span the two footings can demand, the span the
// manoeuvre can deliver, and whether they meet. It consults the moveset and
// the departure resources, so an edge needing a dash the character does not
// have, or a charge it already spent, is rejected for the right reason. It
// does not look at the geometry between the two states at all, which is why
// a certificate from it is worth nothing outside a test.
//
// Wall jumps, clings, climbs, drop-throughs and room transitions are outside
// the fake's model and answer VerdictUnknown with ReasonUnsupportedMoveset,
// which is also how the real oracle reports a manoeuvre it cannot analyse.
func (f *FakeOracle) CheckEdge(ctx context.Context, query EdgeQuery) (EdgeResult, error) {
	if f.CheckEdgeFunc != nil {
		return f.CheckEdgeFunc(ctx, query)
	}
	if err := ctx.Err(); err != nil {
		return EdgeResult{}, err
	}
	if err := query.Profile.Validate(); err != nil {
		return EdgeResult{}, err
	}
	profile := query.Profile
	budget := BudgetReport{CandidateEdges: 1}

	for _, kind := range f.candidateKinds(query) {
		if missing, ok := f.missingAbility(kind, query.Abilities); !ok {
			if query.Kind != MotionEdgeKindUnspecified {
				return EdgeResult{Judgement: f.judgement(VerdictRejected, ReasonAbilityMissing, profile,
					"the moveset lacks "+missing.String(), budget)}, nil
			}
			continue
		}
		if !f.hasResources(kind, query.From.Resources) {
			if query.Kind != MotionEdgeKindUnspecified {
				return EdgeResult{Judgement: f.judgement(VerdictRejected, ReasonResourceExhausted, profile,
					"no charge left for "+kind.String(), budget)}, nil
			}
			continue
		}
		reach, airtime, supported := f.envelope(kind, profile, query)
		if !supported {
			if query.Kind != MotionEdgeKindUnspecified {
				return EdgeResult{Judgement: f.judgement(VerdictUnknown, ReasonUnsupportedMoveset, profile,
					"the fake model does not analyse "+kind.String(), budget)}, nil
			}
			continue
		}
		if reach < 0 {
			continue
		}
		demand := Span{
			Lo: query.To.Footing.Lo - query.From.Footing.Hi,
			Hi: query.To.Footing.Hi - query.From.Footing.Lo,
		}
		if !demand.Overlaps(Span{Lo: -reach, Hi: reach}) {
			continue
		}
		edge := MotionEdge{
			From:     query.From.ID,
			To:       query.To.ID,
			Kind:     kind,
			Requires: f.requirement(kind),
			Duration: airtime,
		}
		if !query.OmitWitness {
			edge.Witness = f.witness(profile, query, kind, airtime)
		}
		return EdgeResult{
			Edge: edge,
			Judgement: f.judgement(VerdictCertified, ReasonWitnessFound, profile,
				"fake envelope only: no collision was tested", budget),
		}, nil
	}
	return EdgeResult{Judgement: f.judgement(VerdictRejected, ReasonOutOfEnvelope, profile,
		"no fake manoeuvre spans the gap", budget)}, nil
}

// candidateKinds returns the manoeuvres the fake will try, in a fixed order so
// that an unrestricted query is deterministic.
func (f *FakeOracle) candidateKinds(query EdgeQuery) []MotionEdgeKind {
	if query.Kind != MotionEdgeKindUnspecified {
		return []MotionEdgeKind{query.Kind}
	}
	return []MotionEdgeKind{
		MotionEdgeKindWalk,
		MotionEdgeKindFall,
		MotionEdgeKindJump,
		MotionEdgeKindDoubleJump,
		MotionEdgeKindDash,
	}
}

func (f *FakeOracle) requirement(kind MotionEdgeKind) AbilitySet {
	switch kind {
	case MotionEdgeKindDash:
		return NewAbilitySet(AbilityDash)
	case MotionEdgeKindDoubleJump:
		return NewAbilitySet(AbilityDoubleJump)
	case MotionEdgeKindWallJump, MotionEdgeKindWallCling:
		return NewAbilitySet(AbilityWallJump)
	case MotionEdgeKindClimb:
		return NewAbilitySet(AbilityClimb)
	}
	return 0
}

// missingAbility reports whether abilities covers the kind, and names one
// ability that is missing when it does not.
func (f *FakeOracle) missingAbility(kind MotionEdgeKind, abilities AbilitySet) (Ability, bool) {
	required := f.requirement(kind)
	if abilities.Contains(required) {
		return 0, true
	}
	for _, ability := range required.Abilities() {
		if !abilities.Has(ability) {
			return ability, false
		}
	}
	return 0, false
}

func (f *FakeOracle) hasResources(kind MotionEdgeKind, resources Resources) bool {
	switch kind {
	case MotionEdgeKindDash:
		return resources.DashCharges > 0 && resources.DashCooldown <= 0
	case MotionEdgeKindDoubleJump:
		return resources.AirJumps > 0
	}
	return true
}

// envelope returns the horizontal reach, in cells, and the airtime, in
// seconds, of one manoeuvre between the query's two heights. A negative reach
// means the manoeuvre cannot gain the required height at all. The third result
// reports whether the fake models the manoeuvre.
func (f *FakeOracle) envelope(kind MotionEdgeKind, profile MovementProfile, query EdgeQuery) (float64, float64, bool) {
	rise := query.To.Height - query.From.Height
	switch kind {
	case MotionEdgeKindWalk:
		if query.From.Surface != query.To.Surface || rise != 0 {
			return -1, 0, true
		}
		span := math.Abs(query.To.Footing.Lo - query.From.Footing.Hi)
		if profile.MaxRunSpeed <= 0 {
			return -1, 0, true
		}
		return math.Inf(1), span / profile.MaxRunSpeed, true
	case MotionEdgeKindFall:
		if rise >= 0 {
			return -1, 0, true
		}
		airtime := math.Sqrt(2 * -rise / profile.GravityDown)
		return profile.MaxRunSpeed * airtime, airtime, true
	case MotionEdgeKindJump:
		return f.ballistic(profile, profile.JumpVelocity, rise)
	case MotionEdgeKindDoubleJump:
		if profile.DoubleJump == nil {
			return -1, 0, false
		}
		// Crude: a reset-mode second jump at the apex doubles the reachable
		// height and adds one more airtime. An impulse-mode one does not, and
		// the fake does not try to tell them apart.
		reach, airtime, ok := f.ballistic(profile, profile.JumpVelocity, rise-profile.ApexHeight())
		if !ok || reach < 0 {
			return reach, airtime, ok
		}
		return reach + profile.MaxRunSpeed*profile.TimeToApex(), airtime + profile.TimeToApex(), true
	case MotionEdgeKindDash:
		if profile.Dash == nil {
			return -1, 0, false
		}
		if rise > 0 {
			return -1, 0, true
		}
		reach := profile.Dash.Speed * profile.Dash.Duration
		airtime := profile.Dash.Duration
		if rise < 0 {
			fall := math.Sqrt(2 * -rise / profile.GravityDown)
			reach += profile.MaxRunSpeed * fall
			airtime += fall
		}
		return reach, airtime, true
	}
	return -1, 0, false
}

// ballistic returns the reach and airtime of a jump of initial velocity
// launch that must gain rise cells, using GravityUp throughout. The real
// model uses GravityUp for the rise and GravityDown for the fall; the fake
// does not, which is one more reason its answers do not transfer.
func (f *FakeOracle) ballistic(profile MovementProfile, launch, rise float64) (float64, float64, bool) {
	discriminant := launch*launch - 2*profile.GravityUp*rise
	if discriminant <= 0 {
		return -1, 0, true
	}
	airtime := (launch + math.Sqrt(discriminant)) / profile.GravityUp
	if airtime <= 0 {
		return -1, 0, true
	}
	return profile.MaxRunSpeed * airtime, airtime, true
}

func (f *FakeOracle) witness(profile MovementProfile, query EdgeQuery, kind MotionEdgeKind, airtime float64) *Witness {
	launch := 0.0
	accelY := -profile.GravityUp
	switch kind {
	case MotionEdgeKindJump, MotionEdgeKindDoubleJump:
		launch = profile.JumpVelocity
	case MotionEdgeKindFall:
		accelY = -profile.GravityDown
	case MotionEdgeKindWalk, MotionEdgeKindDash:
		accelY = 0
	}
	start := MotionState{X: query.From.Footing.Lo, Y: query.From.Height, VX: 0, VY: launch}
	end := MotionState{X: query.To.Footing.Lo, Y: query.To.Height, VX: 0, VY: launch + accelY*airtime}
	hold := InputRight
	if end.X < start.X {
		hold = InputLeft
	}
	if kind == MotionEdgeKindJump || kind == MotionEdgeKindDoubleJump {
		hold |= InputJump
	}
	if kind == MotionEdgeKindDash {
		hold |= InputDash
	}
	return &Witness{
		Phases: []Phase{{
			Mode:     MotionModeAirborne,
			Start:    start,
			End:      end,
			Duration: airtime,
			AccelY:   accelY,
		}},
		Commands:    []Command{{At: 0, Hold: hold}},
		ControlRate: profile.ControlRate,
	}
}

// BuildGraph derives the surfaces, places one rest node on each interval, and
// asks CheckEdge about every ordered pair. That is O(n^2) candidates and is
// fine for the small fixtures a fake is used with.
//
// It respects the budget: when the candidate ceiling is reached the build
// stops, the partial graph is returned, and the judgement is VerdictUnknown
// with ReasonBudgetExhausted. Every edge in a partial graph is still an edge
// the fake certified; what is missing is the knowledge of what else exists.
func (f *FakeOracle) BuildGraph(ctx context.Context, query GraphQuery) (GraphResult, error) {
	if f.BuildGraphFunc != nil {
		return f.BuildGraphFunc(ctx, query)
	}
	if err := ctx.Err(); err != nil {
		return GraphResult{}, err
	}
	if err := query.Profile.Validate(); err != nil {
		return GraphResult{}, err
	}
	budget := effectiveBudget(query.Budget)
	profile := query.Profile

	derived, err := f.Surfaces(ctx, SurfaceQuery{Grid: query.Grid, Room: query.Room, Profile: profile})
	if err != nil {
		return GraphResult{}, err
	}

	discipline := query.Discipline
	if discipline == NodeDisciplineUnspecified {
		discipline = NodeDisciplineRefined
	}
	graph := JumpGraph{
		Model:          ModelFake,
		ProfileVersion: profile.Version,
		Abilities:      query.Abilities,
		Discipline:     discipline,
		Surfaces:       derived.Surfaces,
	}
	for _, surface := range derived.Surfaces {
		for index, interval := range surface.Intervals {
			if interval.Footing.IsEmpty() {
				continue
			}
			if len(graph.Nodes) >= MaxMotionNodes {
				return GraphResult{Graph: graph, Judgement: f.judgement(VerdictUnknown, ReasonBudgetExhausted, profile,
					"node ceiling reached", BudgetReport{Exhausted: true})}, nil
			}
			graph.Nodes = append(graph.Nodes, MotionNode{
				ID:        MotionNodeID(len(graph.Nodes)),
				Surface:   surface.ID,
				Interval:  uint32(index),
				Height:    surface.At,
				Footing:   interval.Footing,
				Velocity:  Point(0),
				Mode:      MotionModeGrounded,
				Resources: profile.FullResources(),
			})
		}
	}

	report := BudgetReport{}
	for _, from := range graph.Nodes {
		for _, to := range graph.Nodes {
			if from.ID == to.ID {
				continue
			}
			if err := ctx.Err(); err != nil {
				return GraphResult{}, err
			}
			if budget.MaxCandidateEdges > 0 && report.CandidateEdges >= budget.MaxCandidateEdges {
				report.Exhausted = true
				return GraphResult{Graph: graph, Judgement: f.judgement(VerdictUnknown, ReasonBudgetExhausted, profile,
					"candidate ceiling reached", report)}, nil
			}
			report.CandidateEdges++
			result, err := f.CheckEdge(ctx, EdgeQuery{
				Grid:        query.Grid,
				Profile:     profile,
				Abilities:   query.Abilities,
				From:        from,
				To:          to,
				Budget:      budget,
				OmitWitness: query.OmitWitness,
			})
			if err != nil {
				return GraphResult{}, err
			}
			if !result.Judgement.Certified() {
				continue
			}
			if len(graph.Edges) >= MaxMotionEdges {
				report.Exhausted = true
				return GraphResult{Graph: graph, Judgement: f.judgement(VerdictUnknown, ReasonBudgetExhausted, profile,
					"edge ceiling reached", report)}, nil
			}
			edge := result.Edge
			edge.ID = MotionEdgeID(len(graph.Edges))
			edge.From = from.ID
			edge.To = to.ID
			graph.Edges = append(graph.Edges, edge)
		}
	}
	return GraphResult{
		Graph: graph,
		Judgement: f.judgement(VerdictCertified, ReasonWitnessFound, profile,
			"fake envelope only: no collision was tested", report),
	}, nil
}

// FindRoute runs a breadth-first search over the graph, visiting edges in
// ascending MotionEdgeID order so the route is deterministic. An edge the
// moveset cannot use is skipped, which is the whole of the progression filter.
func (f *FakeOracle) FindRoute(ctx context.Context, query RouteQuery) (RouteResult, error) {
	if f.FindRouteFunc != nil {
		return f.FindRouteFunc(ctx, query)
	}
	if err := ctx.Err(); err != nil {
		return RouteResult{}, err
	}
	if query.Graph == nil {
		return RouteResult{}, queryError("RouteQuery.Graph is nil")
	}
	if _, ok := query.Graph.Node(query.From); !ok {
		return RouteResult{}, queryError("RouteQuery.From %d is not a node of the graph", query.From)
	}
	if _, ok := query.Graph.Node(query.To); !ok {
		return RouteResult{}, queryError("RouteQuery.To %d is not a node of the graph", query.To)
	}
	profile := MovementProfile{Version: query.Graph.ProfileVersion}
	budget := effectiveBudget(query.Budget)

	if query.From == query.To {
		return RouteResult{
			Route:     Route{From: query.From, To: query.To},
			Judgement: f.judgement(VerdictCertified, ReasonTrivial, profile, "origin and goal are the same node", BudgetReport{}),
		}, nil
	}

	const unvisited = -1
	cameFrom := make([]int, len(query.Graph.Nodes))
	for index := range cameFrom {
		cameFrom[index] = unvisited
	}
	report := BudgetReport{}
	queue := []MotionNodeID{query.From}
	visited := make([]bool, len(query.Graph.Nodes))
	visited[query.From] = true

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return RouteResult{}, err
		}
		if budget.MaxExpandedNodes > 0 && report.ExpandedNodes >= budget.MaxExpandedNodes {
			report.Exhausted = true
			return RouteResult{Judgement: f.judgement(VerdictUnknown, ReasonBudgetExhausted, profile,
				"expansion ceiling reached", report)}, nil
		}
		current := queue[0]
		queue = queue[1:]
		report.ExpandedNodes++

		for _, edge := range query.Graph.Edges {
			if edge.From != current || !edge.Reachable(query.Abilities) {
				continue
			}
			if visited[edge.To] {
				continue
			}
			visited[edge.To] = true
			cameFrom[edge.To] = int(edge.ID)
			if edge.To == query.To {
				return RouteResult{
					Route: f.rebuild(query, cameFrom),
					Judgement: f.judgement(VerdictCertified, ReasonWitnessFound, profile,
						"fake envelope only: every edge on this route is unverified", report),
				}, nil
			}
			queue = append(queue, edge.To)
		}
	}
	return RouteResult{Judgement: f.judgement(VerdictRejected, ReasonDisconnected, profile,
		"no route under this moveset in this graph", report)}, nil
}

func (f *FakeOracle) rebuild(query RouteQuery, cameFrom []int) Route {
	var reversed []MotionEdgeID
	node := query.To
	for node != query.From {
		edgeIndex := cameFrom[node]
		if edgeIndex < 0 {
			break
		}
		edge := query.Graph.Edges[edgeIndex]
		reversed = append(reversed, edge.ID)
		node = edge.From
	}
	route := Route{From: query.From, To: query.To, Edges: make([]MotionEdgeID, 0, len(reversed))}
	for index := len(reversed) - 1; index >= 0; index-- {
		edge := query.Graph.Edges[reversed[index]]
		route.Edges = append(route.Edges, edge.ID)
		route.Requires = route.Requires.Union(edge.Requires)
		route.Duration += edge.Duration
	}
	return route
}

// validateGrid checks that a grid describes space. It is the geometry gate
// every oracle entry point passes through.
func validateGrid(grid Grid) error {
	if grid.Width == 0 || grid.Height == 0 {
		return geometryError("grid dimensions %dx%d include a zero", grid.Width, grid.Height)
	}
	if grid.Width > MaxRoomSide || grid.Height > MaxRoomSide {
		return limitError("grid dimensions %dx%d exceed the per-axis ceiling of %d", grid.Width, grid.Height, MaxRoomSide)
	}
	if grid.CellCount() > MaxRoomCells {
		return limitError("grid holds %d cells, above the ceiling of %d", grid.CellCount(), MaxRoomCells)
	}
	if uint64(len(grid.Cells)) != grid.CellCount() {
		return geometryError("grid holds %d cells for dimensions %dx%d", len(grid.Cells), grid.Width, grid.Height)
	}
	for index, kind := range grid.Cells {
		if !kind.IsKnown() {
			return geometryError("cell %d has unknown kind %d; an unknown kind is rejected, never defaulted to air", index, int(kind))
		}
	}
	if grid.Terrain == nil {
		return nil
	}
	if uint64(len(grid.Terrain.Indices)) != grid.CellCount() {
		return geometryError("terrain layer holds %d indices for dimensions %dx%d", len(grid.Terrain.Indices), grid.Width, grid.Height)
	}
	if len(grid.Terrain.Palette) > MaxTerrainKinds {
		return limitError("terrain palette holds %d entries, above the ceiling of %d", len(grid.Terrain.Palette), MaxTerrainKinds)
	}
	for index, selector := range grid.Terrain.Indices {
		if int(selector) > len(grid.Terrain.Palette) {
			return geometryError("terrain index %d selects palette entry %d of %d", index, selector, len(grid.Terrain.Palette))
		}
	}
	return nil
}
