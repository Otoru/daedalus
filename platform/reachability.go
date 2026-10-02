package platform

import (
	"context"
	"math"
)

// This file is the search half of the M1 oracle: how a manoeuvre is proposed,
// how it is flown against the geometry, and how the result becomes a verdict.
//
// # Candidates are aimed, not swept
//
// The vertical motion of a manoeuvre does not depend on its launch velocity,
// so the flight time to a destination height is fixed before the horizontal
// question is asked. Section 4.3's witness construction then inverts the
// horizontal displacement directly: for every family whose horizontal
// behaviour is affine in the launch velocity — which is every family that
// does not hold a direction in the air — two evaluations of the phase machine
// give the exact launch velocity that lands on a chosen point. A coarse sweep
// is added on top, because an aimed candidate can be obstructed while a
// neighbouring one is not, and because an arc that lands somewhere ELSE than
// it was aimed at is still a real edge.
//
// # Sound but incomplete, and the three-valued verdict is how that is said
//
// The enumeration is finite: launch positions come from the budget's
// LaunchResolution, mid-air event times from its TimeResolution, and both the
// flight and the candidate count are capped. A manoeuvre the enumeration
// missed is NOT rejected. Running out of budget, running past the fall
// horizon and meeting a mechanic the model does not analyse all produce
// VerdictUnknown. VerdictRejected is reserved for the cases the model
// positively excludes: outside the envelope, obstructed on every candidate,
// no footing to land on, a resource already spent, an ability not held.

// searchLimits are the constants the enumeration uses on top of the budget.
const (
	// maxEventSamples caps how many mid-air event times one family tries.
	// Finer sampling finds more manoeuvres and what it misses is Unknown.
	maxEventSamples = 6
	// maxReleaseSamples caps the variable jump's release times.
	maxReleaseSamples = 4
	// sweepSamples is the number of launch velocities tried per family on top
	// of the aimed ones.
	sweepSamples = 9
	// aimPoints is how many positions of a destination footing are aimed at:
	// the two ends and the middle.
	aimPoints = 3
)

// spend meters one question against its budget. It is the only thing that may
// end a search early, and it ends it with VerdictUnknown: a ceiling is a
// statement about this oracle, never about the geometry.
type spend struct {
	budget    SearchBudget
	report    BudgetReport
	exhausted bool
}

func newSpend(budget SearchBudget) *spend {
	return &spend{budget: effectiveBudget(budget)}
}

func (s *spend) charge(counter *uint64, ceiling uint64) bool {
	if s.exhausted {
		return false
	}
	*counter++
	if ceiling != 0 && *counter > ceiling {
		s.exhausted = true
		s.report.Exhausted = true
		return false
	}
	return true
}

func (s *spend) candidate() bool {
	return s.charge(&s.report.CandidateEdges, s.budget.MaxCandidateEdges)
}

func (s *spend) collision() bool {
	return s.charge(&s.report.CollisionTests, s.budget.MaxCollisionTests)
}

func (s *spend) expansion() bool {
	return s.charge(&s.report.ExpandedNodes, s.budget.MaxExpandedNodes)
}

// flightResult is what one candidate manoeuvre did.
type flightResult struct {
	// kind is what ended the arc.
	kind contactKind
	// site is the landing, meaningful when kind is contactLanding.
	site landingSite
	// wall is the side a cling candidate touched, meaningful when the arc was
	// blocked by a vertical face.
	wall WallSide
	// wallX is the foot position of that cling.
	wallX float64
	// state is the state the arc ended in.
	state MotionState
	// resources is the consumable state at the end.
	resources Resources
	// witness is the recorded trajectory, or nil when recording was off.
	witness *Witness
	// duration is the arc's total time.
	duration float64
	// reason is why the arc produced no landing.
	reason VerdictReason
	// truncated reports that a ceiling, not the geometry, ended the arc.
	truncated bool
}

// fly runs one candidate manoeuvre against the geometry, phase by phase,
// stopping at the first event. An intermediate landing ENDS the arc: a
// parabola never crosses a platform in silence, because the platform is a
// state change and the state after it is a different node.
func (g *geometry) fly(abilities AbilitySet, plan launch, ceiling, horizon float64, record bool, spent *spend) flightResult {
	s := newStepper(g.profile, abilities, plan, record)
	for {
		a, ok := s.next(ceiling)
		if !ok {
			if s.failed != ReasonUnspecified {
				return flightResult{kind: contactNone, reason: s.failed, state: s.state, resources: s.res, duration: s.t}
			}
			return flightResult{kind: contactNone, reason: ReasonBudgetExhausted, truncated: true, state: s.state, resources: s.res, duration: s.t}
		}
		// The fall horizon is a search parameter, so it clips the phase rather
		// than being noticed after the fact: without it the candidate set of a
		// tall room is unbounded downward, and an arc that fell past it would
		// be certified against geometry the budget said not to look at.
		full := a.span
		if clipped, ok := clipToHorizon(a, horizon); ok {
			a.span = clipped
		}
		met := g.sweep(a, plan.exempt, plan.ignoreSemi, plan.ignoreSemiActive, s.mode, abilities, spent)
		if spent.exhausted {
			return flightResult{kind: contactNone, reason: ReasonBudgetExhausted, truncated: true, state: s.state, resources: s.res, duration: s.t}
		}
		if met.kind != contactNone && met.at <= a.span+contactEpsilon {
			s.advance(a, math.Min(met.at, a.span), false)
			result := flightResult{
				kind:      met.kind,
				site:      met.site,
				wall:      met.wallSide,
				wallX:     met.wallFace,
				state:     s.state,
				resources: s.res,
				witness:   s.witness(),
				duration:  s.t,
			}
			switch met.kind {
			case contactLanding:
				result.reason = ReasonWitnessFound
			case contactHazard:
				result.reason = ReasonObstructed
			default:
				result.reason = ReasonObstructed
			}
			return result
		}
		s.advance(a, a.span, a.span >= full-timeEpsilon)
		if s.failed != ReasonUnspecified {
			return flightResult{kind: contactNone, reason: s.failed, state: s.state, resources: s.res, duration: s.t}
		}
		if s.state.Y <= horizon+contactEpsilon {
			return flightResult{kind: contactNone, reason: ReasonBudgetExhausted, truncated: true, state: s.state, resources: s.res, duration: s.t}
		}
	}
}

// clipToHorizon returns the first time an arc's feet descend through a height,
// so a phase never runs past the fall horizon.
func clipToHorizon(a arc, horizon float64) (float64, bool) {
	best := math.Inf(1)
	found := roots(a.y0-horizon, a.vy, a.ay, a.span)
	for i := 0; i < found.n; i++ {
		if t := found.at[i]; t > timeEpsilon && a.vyAt(t) < 0 && t < best {
			best = t
		}
	}
	if math.IsInf(best, 1) {
		return 0, false
	}
	return best, true
}

// nodeRecord is the builder's view of one MotionNode: the node itself plus
// the geometric facts the enumeration needs and the contract does not carry.
type nodeRecord struct {
	node MotionNode
	// walkable is the contiguous span of foot positions the node can reach on
	// foot without leaving the ground. It bounds both the run-up available
	// for a launch and the braking room available on arrival.
	walkable Span
	// ledgeLo and ledgeHi report that the node sits at an end of the walkable
	// span, which is the only place a fall or a sideways departure can start.
	ledgeLo bool
	ledgeHi bool
	// exempt is the blocking run the node stands on, for the overhang rule.
	exempt departure
	// site is the landing site the node occupies, for a grounded node.
	site landingSite
	// semi reports that the supporting surface is a one-way platform, which
	// is the only kind a drop-through can leave downward.
	semi bool
	// wall is the side of the body the wall is on, for a cling node.
	wall WallSide
	// climb reports a node attached to a ladder or rope.
	climb bool
}

// footX returns the node's single foot position. Every node this oracle
// builds has a point footing, which is what makes the universal reading of
// MotionNode's spans trivially true rather than merely hoped for.
func (r nodeRecord) footX() float64 { return r.node.Footing.Lo }

// launchCeiling is the speed the character can actually leave this node with,
// in one direction, given the run-up the surface offers.
func (r nodeRecord) launchCeiling(profile MovementProfile, dir float64) float64 {
	if r.node.Mode != MotionModeGrounded {
		return profile.MaxRunSpeed
	}
	available := r.footX() - r.walkable.Lo
	if dir < 0 {
		available = r.walkable.Hi - r.footX()
	}
	if available < 0 {
		available = 0
	}
	return math.Min(profile.MaxRunSpeed, math.Sqrt(2*profile.GroundAccel*available))
}

// canStop reports whether a body arriving at x with velocity vx can brake to
// rest without leaving the span.
//
// This is where "two jumps that are each possible do not compose" is caught.
// A landing is not a state; a landing the character can stand still in is.
// A short platform reached at speed fails here even though the arc that
// reached it was certified, and the edge is therefore never emitted.
func canStop(profile MovementProfile, span Span, x, vx float64) bool {
	if span.IsEmpty() {
		return false
	}
	stop := x + math.Copysign(profile.StoppingDistance(math.Abs(vx)), vx)
	if vx == 0 {
		stop = x
	}
	return span.Contains(x) && span.Contains(stop)
}

// groundResources returns the consumable state after a safe landing, applying
// the profile's refill rules and the free-wait rule.
//
// The free-wait rule: on static geometry a character standing on a safe
// surface may wait as long as it likes, so a profile that refills on cooldown
// is always full again by the time it leaves. That is a statement about this
// model — nothing in it moves, nothing chases — and it is why a profile that
// refills ONLY on cooldown or ONLY on ground never produces an exhausted
// ground state, while one that refills only on wall contact does.
func groundResources(profile MovementProfile, arriving Resources, elapsed float64) Resources {
	out := arriving
	out.DashCooldown = math.Max(0, out.DashCooldown-elapsed)
	out.WallJumpsSinceGround = 0
	out.LastWall = WallSideNone
	if profile.WallJump != nil {
		out.ClingRemaining = profile.WallJump.Stamina
	}
	if profile.DoubleJump != nil && profile.DoubleJump.RefillOn.Has(RefillOnGround) {
		out.AirJumps = profile.DoubleJump.Charges
	}
	if profile.Dash != nil {
		if profile.Dash.RefillOn.Has(RefillOnGround) {
			out.DashCharges = profile.Dash.Charges
			out.DashCooldown = 0
		}
		if profile.Dash.RefillOn.Has(RefillOnCooldown) {
			out.DashCharges = profile.Dash.Charges
			out.DashCooldown = 0
		}
	}
	return out
}

// clingResources returns the consumable state after attaching to a wall.
func clingResources(profile MovementProfile, arriving Resources, side WallSide, elapsed float64) Resources {
	out := arriving
	out.DashCooldown = math.Max(0, out.DashCooldown-elapsed)
	out.LastWall = side
	if profile.WallJump != nil {
		out.ClingRemaining = profile.WallJump.Stamina
	}
	if profile.DoubleJump != nil && profile.DoubleJump.RefillOn.Has(RefillOnWallCling) {
		out.AirJumps = profile.DoubleJump.Charges
	}
	if profile.Dash != nil && profile.Dash.RefillOn.Has(RefillOnWallCling) {
		out.DashCharges = profile.Dash.Charges
		out.DashCooldown = 0
	}
	return out
}

// climbResources returns the consumable state after grabbing a climbable.
func climbResources(profile MovementProfile, arriving Resources, elapsed float64) Resources {
	out := arriving
	out.DashCooldown = math.Max(0, out.DashCooldown-elapsed)
	if profile.DoubleJump != nil && profile.DoubleJump.RefillOn.Has(RefillOnClimb) {
		out.AirJumps = profile.DoubleJump.Charges
	}
	if profile.Dash != nil && profile.Dash.RefillOn.Has(RefillOnClimb) {
		out.DashCharges = profile.Dash.Charges
		out.DashCooldown = 0
	}
	return out
}

// family is one shape of manoeuvre: a base launch plus the event schedule it
// varies. Families are generated in a fixed order so the candidate stream is
// reproducible.
type family struct {
	kind     MotionEdgeKind
	requires AbilitySet
	base     launch
	// events is the schedule dimension to vary, or nil for a single plan.
	events []float64
	// apply writes one event time into a copy of the base plan.
	apply func(plan *launch, at float64)
	// aimable reports whether the horizontal displacement is affine in the
	// launch velocity, which is what lets a destination be aimed at exactly.
	aimable bool
}

// enumerate produces the candidate manoeuvres leaving a node, in a canonical
// order, and hands each to visit. It stops when visit returns false or when
// the budget runs out.
func (g *geometry) enumerate(record nodeRecord, abilities AbilitySet, budget SearchBudget, spent *spend, visit func(launch) bool) {
	profile := g.profile
	ceiling := flightCeiling(profile, budget)
	for _, f := range g.families(record, abilities, budget) {
		times := f.events
		if times == nil {
			times = []float64{math.NaN()}
		}
		for _, at := range times {
			base := f.base
			if f.apply != nil && !math.IsNaN(at) {
				f.apply(&base, at)
			}
			for _, u := range g.launchVelocities(record, abilities, f, base, ceiling, budget) {
				plan := base
				plan.vx = u
				plan.kind = f.kind
				plan.requires = f.requires
				if spent.exhausted || !visit(plan) {
					return
				}
			}
			if f.aimable {
				continue
			}
		}
	}
}

// families lists the manoeuvre shapes available from a node under a moveset.
func (g *geometry) families(record nodeRecord, abilities AbilitySet, budget SearchBudget) []family {
	profile := g.profile
	var out []family

	base := newLaunch()
	base.x0 = record.footX()
	base.y0 = record.node.Height
	base.resources = record.node.Resources
	base.exempt = record.exempt

	ledge := record.ledgeLo || record.ledgeHi

	switch record.node.Mode {
	case MotionModeGrounded:
		jump := base
		jump.vy = profile.JumpVelocity
		jump.mode = MotionModeAirborne
		out = append(out, family{kind: MotionEdgeKindJump, base: jump, aimable: true})

		if profile.VariableJump != nil && profile.VariableJump.Mode != VariableJumpModeNone &&
			profile.VariableJump.Mode != VariableJumpModeUnspecified {
			out = append(out, family{
				kind:    MotionEdgeKindJump,
				base:    jump,
				events:  releaseTimes(profile, budget),
				apply:   func(plan *launch, at float64) { plan.jumpRelease = at },
				aimable: true,
			})
		}

		if ledge {
			fall := base
			fall.mode = MotionModeAirborne
			out = append(out, family{kind: MotionEdgeKindFall, base: fall, aimable: true})

			if profile.Coyote != nil && profile.Coyote.Window > 0 &&
				profile.Coyote.AppliesTo.Has(CoyoteFromLedge) {
				coyote := base
				coyote.mode = MotionModeCoyote
				out = append(out, family{
					kind:    MotionEdgeKindJump,
					base:    coyote,
					events:  eventTimes(budget, profile.Coyote.Window),
					apply:   func(plan *launch, at float64) { plan.coyoteJumpAt = at },
					aimable: true,
				})
			}
		}

		if record.semi {
			drop := base
			drop.mode = MotionModeAirborne
			drop.ignoreSemi = record.node.Surface
			drop.ignoreSemiActive = true
			out = append(out, family{kind: MotionEdgeKindDropThrough, base: drop, aimable: true})
		}

	case MotionModeWallCling:
		if profile.WallJump == nil || !abilities.Has(AbilityWallJump) {
			return nil
		}
		away := 1.0
		if record.wall == WallSideRight {
			away = -1
		}
		leap := base
		leap.mode = MotionModeAirborne
		leap.vy = profile.WallJump.ImpulseY
		leap.vx = away * profile.WallJump.ImpulseX
		out = append(out, family{
			kind:     MotionEdgeKindWallJump,
			requires: NewAbilitySet(AbilityWallJump),
			base:     leap,
		})
		drop := base
		drop.mode = MotionModeAirborne
		drop.vx = away * profile.WallJump.ImpulseX * 0.25
		out = append(out, family{kind: MotionEdgeKindFall, base: drop})

	case MotionModeClimbing:
		if profile.Climb == nil || !abilities.Has(AbilityClimb) {
			return nil
		}
		if !profile.Climb.CanJumpOff {
			return nil
		}
		leap := base
		leap.mode = MotionModeAirborne
		leap.vy = profile.JumpVelocity
		out = append(out, family{
			kind:     MotionEdgeKindClimb,
			requires: NewAbilitySet(AbilityClimb),
			base:     leap,
			aimable:  true,
		})
	}

	// Mid-air extensions. At most one per arc: combining a mid-air jump with a
	// dash is a real manoeuvre and this build does not enumerate it, which
	// makes the analysis more incomplete and never less sound.
	airborne := make([]family, 0, 4)
	for _, f := range out {
		if f.base.mode != MotionModeAirborne && f.base.mode != MotionModeCoyote {
			continue
		}
		if f.events != nil {
			continue
		}
		if profile.DoubleJump != nil && abilities.Has(AbilityDoubleJump) && record.node.Resources.AirJumps > 0 {
			airborne = append(airborne, family{
				kind:     MotionEdgeKindDoubleJump,
				requires: NewAbilitySet(AbilityDoubleJump),
				base:     f.base,
				events:   eventTimes(budget, flightCeiling(profile, budget)/2),
				apply:    func(plan *launch, at float64) { plan.airJumpAt = at },
				aimable:  true,
			})
		}
		if profile.Dash != nil && abilities.Has(AbilityDash) &&
			record.node.Resources.DashCharges > 0 && record.node.Resources.DashCooldown <= contactEpsilon {
			requires := NewAbilitySet(AbilityDash)
			if abilities.Has(AbilityShadowDash) && profile.Dash.ShadowSpeed > 0 {
				requires = requires.With(AbilityShadowDash)
			}
			for _, dir := range []float64{1, -1} {
				if !profile.Dash.Directions.Has(DashDirectionHorizontal) {
					continue
				}
				burst := f.base
				burst.dashDir = dir
				airborne = append(airborne, family{
					kind:     MotionEdgeKindDash,
					requires: requires,
					base:     burst,
					events:   eventTimes(budget, flightCeiling(profile, budget)/2),
					apply:    func(plan *launch, at float64) { plan.dashAt = at },
					aimable:  false,
				})
			}
		}
	}
	return append(out, airborne...)
}

// eventTimes returns the mid-air event times a family tries, sampled from the
// budget's TimeResolution and capped so one family cannot dominate the search.
func eventTimes(budget SearchBudget, window float64) []float64 {
	step := budget.TimeResolution
	if step <= 0 || window <= 0 {
		return nil
	}
	count := int(math.Floor(window / step))
	if count < 1 {
		count = 1
	}
	stride := 1
	if count > maxEventSamples {
		stride = (count + maxEventSamples - 1) / maxEventSamples
	}
	var out []float64
	for i := 1; i <= count && len(out) < maxEventSamples; i += stride {
		out = append(out, float64(i)*step)
	}
	return out
}

// releaseTimes returns the jump-release times a variable jump tries, between
// the profile's minimum hold and the apex.
func releaseTimes(profile MovementProfile, budget SearchBudget) []float64 {
	apex := profile.TimeToApex()
	minimum := 0.0
	if profile.VariableJump != nil {
		minimum = profile.VariableJump.MinHoldTime
	}
	if apex <= minimum {
		return nil
	}
	var out []float64
	for i := 1; i <= maxReleaseSamples; i++ {
		at := minimum + (apex-minimum)*float64(i)/float64(maxReleaseSamples+1)
		if at > budget.TimeResolution {
			out = append(out, at)
		}
	}
	return out
}

// launchVelocities returns the launch velocities one family tries from one
// node: the velocities that aim at every destination footing within reach,
// plus a coarse sweep, all clipped to what the surface's run-up can build.
func (g *geometry) launchVelocities(record nodeRecord, abilities AbilitySet, f family, base launch, ceiling float64, budget SearchBudget) []float64 {
	profile := g.profile
	right := record.launchCeiling(profile, 1)
	left := record.launchCeiling(profile, -1)
	if record.node.Mode != MotionModeGrounded {
		right, left = profile.MaxRunSpeed, profile.MaxRunSpeed
	}
	if f.kind == MotionEdgeKindWallJump || f.base.vx != 0 {
		// The launch velocity is fixed by the manoeuvre itself.
		return []float64{f.base.vx}
	}

	admit := func(u float64) bool {
		if u >= 0 {
			return u <= right+contactEpsilon
		}
		return -u <= left+contactEpsilon
	}

	out := make([]float64, 0, sweepSamples+2*aimPoints)
	seen := func(u float64) bool {
		for _, had := range out {
			if math.Abs(had-u) <= 1e-6 {
				return true
			}
		}
		return false
	}
	add := func(u float64) {
		if !admit(u) || math.IsNaN(u) || math.IsInf(u, 0) || seen(u) {
			return
		}
		out = append(out, u)
	}

	if f.aimable && base.airHold == 0 {
		// The vertical motion does not depend on the launch velocity, so the
		// flight time to a height — and with it the horizontal offset and the
		// slope of the displacement in u — is computed ONCE PER ROW and
		// reused by every landing site at that height. Then the window the
		// run-up can actually deliver prunes the sites before any of them is
		// aimed at.
		zero, one := base, base
		zero.vx, one.vx = 0, 1
		for row := range g.landings {
			sites := g.landings[row]
			if len(sites) == 0 {
				continue
			}
			at := sites[0].at
			if at-base.y0 > profile.ApexHeight()+profile.BodyHeight || base.y0-at > budget.FallHorizon {
				continue
			}
			flight, ok := descentTime(profile, abilities, zero, at, ceiling)
			if !ok {
				continue
			}
			offset := horizontalReach(profile, abilities, zero, flight)
			slope := horizontalReach(profile, abilities, one, flight) - offset
			if math.Abs(slope) <= contactEpsilon {
				continue
			}
			window := Span{
				Lo: base.x0 + offset - left*slope,
				Hi: base.x0 + offset + right*slope,
			}
			if window.Lo > window.Hi {
				window.Lo, window.Hi = window.Hi, window.Lo
			}
			for _, site := range sites {
				if !window.Overlaps(site.footing) {
					continue
				}
				for point := 0; point < aimPoints; point++ {
					target := site.footing.Lo + site.footing.Length()*float64(point)/float64(aimPoints-1)
					add((target - base.x0 - offset) / slope)
				}
			}
		}
	}

	limit := math.Max(right, left)
	for i := 0; i < sweepSamples; i++ {
		u := -limit + 2*limit*float64(i)/float64(sweepSamples-1)
		add(u)
	}
	return out
}

// --- the edge question ----------------------------------------------------

// CheckEdge answers whether one manoeuvre connects two states.
//
// # What a wide node means
//
// A MotionNode is a set of states, and the two endpoints are quantified
// differently on purpose. The departure is UNIVERSAL: the answer must hold
// from every foot position and every velocity the From node stands for,
// because the next edge in a route will be justified the same way. The
// arrival is EXISTENTIAL: reaching any state of To is reaching To.
//
// A From whose footing is a single point is decided exactly. A From whose
// footing is wider cannot be proved by a finite sample, so a wide From can
// only ever be REJECTED here — by one sampled position that fails, which is a
// genuine counterexample to the universal claim — or answered Unknown. It is
// never certified. Every node this oracle builds has a point footing, so the
// restriction costs the graph nothing and costs a hand-written query only an
// honest answer.
func (o *M1Oracle) CheckEdge(ctx context.Context, query EdgeQuery) (EdgeResult, error) {
	if err := ctx.Err(); err != nil {
		return EdgeResult{}, err
	}
	budget := effectiveBudget(query.Budget)
	if err := budget.Validate(); err != nil {
		return EdgeResult{}, queryError("budget: %v", err)
	}
	geom, err := newGeometry(query.Grid, query.From.Surface.room(), query.Profile)
	if err != nil {
		return EdgeResult{}, err
	}
	from, err := geom.describe(query.From)
	if err != nil {
		return EdgeResult{}, err
	}
	if _, err := geom.describe(query.To); err != nil {
		return EdgeResult{}, err
	}
	spent := newSpend(budget)

	samples := from.samples(budget)
	certified := 0
	var best EdgeResult
	var worst Judgement
	decided := false

	for _, position := range samples {
		probe := from
		probe.node.Footing = Point(position)
		result := geom.decideEdge(ctx, probe, query, budget, spent)
		if result.Judgement.Verdict == VerdictRejected {
			result.Judgement.Budget = spent.report
			return result, nil
		}
		if result.Judgement.Certified() {
			certified++
			if !decided {
				best = result
				decided = true
			}
			continue
		}
		worst = result.Judgement
	}

	switch {
	case certified == len(samples) && len(samples) == 1:
		best.Judgement.Budget = spent.report
		return best, nil
	case certified == len(samples):
		return EdgeResult{Judgement: o.judge(VerdictUnknown, ReasonUnsupportedGeometry, query.Profile,
			"every sampled departure position certified, but a node whose footing spans more than a point cannot be proved by a finite sample", spent.report)}, nil
	default:
		if worst.Reason == ReasonUnspecified {
			worst = o.judge(VerdictUnknown, ReasonBudgetExhausted, query.Profile, "search ceiling reached", spent.report)
		}
		worst.Budget = spent.report
		return EdgeResult{Judgement: worst}, nil
	}
}

// samples returns the departure positions a node's footing is tested at.
func (r nodeRecord) samples(budget SearchBudget) []float64 {
	footing := r.node.Footing
	if footing.IsEmpty() {
		return nil
	}
	if footing.Length() <= contactEpsilon {
		return []float64{footing.Lo}
	}
	step := budget.LaunchResolution
	count := int(math.Ceil(footing.Length()/step)) + 1
	if count < 2 {
		count = 2
	}
	out := make([]float64, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, footing.Lo+footing.Length()*float64(i)/float64(count-1))
	}
	return out
}

// decideEdge answers one point-footed departure. retry is false on the
// diagnostic re-run described in spentCharges.
func (g *geometry) decideEdge(ctx context.Context, from nodeRecord, query EdgeQuery, budget SearchBudget, spent *spend) EdgeResult {
	return g.decide(ctx, from, query, budget, spent, true)
}

// spentCharges reports whether the departure state has used up a mid-air
// charge the moveset and the profile would otherwise provide.
func spentCharges(profile MovementProfile, abilities AbilitySet, res Resources) bool {
	if profile.Dash != nil && abilities.Has(AbilityDash) &&
		(res.DashCharges == 0 || res.DashCooldown > contactEpsilon) {
		return true
	}
	return profile.DoubleJump != nil && abilities.Has(AbilityDoubleJump) && res.AirJumps == 0
}

func (g *geometry) decide(ctx context.Context, from nodeRecord, query EdgeQuery, budget SearchBudget, spent *spend, diagnose bool) EdgeResult {
	profile := query.Profile
	ceiling := flightCeiling(profile, budget)
	horizon := from.node.Height - budget.FallHorizon

	if query.From.Surface == query.To.Surface && query.From.Interval == query.To.Interval &&
		query.To.Footing.Contains(from.footX()) && query.From.Mode == query.To.Mode {
		return EdgeResult{Judgement: judgeAs(VerdictCertified, ReasonTrivial, profile, "the two states are the same", spent.report)}
	}

	// Walking is not a flight and is answered first.
	if walk, ok := g.walkEdge(from, query, budget); ok {
		return walk
	}

	// The rejection reasons are distinguished by how far a candidate got, not
	// by which one failed last. Reaching the destination and failing to stop
	// on it is a different fact from never reaching it, and a consumer that
	// has to tell a short platform from a wall reads this field.
	landedSomewhere := false
	landedOnTarget := false
	stopFailed := false
	arrivalMismatch := false
	obstructed := false
	truncated := false
	abilityShort := false

	var answer EdgeResult
	found := false

	g.enumerate(from, query.Abilities, budget, spent, func(plan launch) bool {
		if err := ctx.Err(); err != nil {
			return false
		}
		if query.Kind != MotionEdgeKindUnspecified && plan.kind != query.Kind {
			return true
		}
		if !query.Abilities.Contains(plan.requires) {
			abilityShort = true
			return true
		}
		if !spent.candidate() {
			truncated = true
			return false
		}
		result := g.fly(query.Abilities, plan, ceiling, horizon, !query.OmitWitness, spent)
		switch result.reason {
		case ReasonBudgetExhausted:
			truncated = true
		case ReasonObstructed:
			obstructed = true
		}
		if result.kind != contactLanding {
			if arrival, ok := g.clingArrival(from, plan, result, query); ok {
				if matchArrival(arrival, query, profile) {
					answer = EdgeResult{
						Edge:      edgeFrom(plan, result, query),
						Judgement: judgeAs(VerdictCertified, ReasonWitnessFound, profile, "wall contact certified", spent.report),
					}
					found = true
					return false
				}
			}
			return true
		}
		landedSomewhere = true
		onTarget := result.site.surface == query.To.Surface && result.site.interval == query.To.Interval
		if onTarget {
			landedOnTarget = true
		}
		arrival, ok := g.landingArrival(plan, result, query.Profile)
		if !ok {
			if onTarget {
				stopFailed = true
			}
			return true
		}
		if !matchArrival(arrival, query, profile) {
			if onTarget {
				arrivalMismatch = true
			}
			return true
		}
		answer = EdgeResult{
			Edge:      edgeFrom(plan, result, query),
			Judgement: judgeAs(VerdictCertified, ReasonWitnessFound, profile, plan.kind.String()+" certified", spent.report),
		}
		found = true
		return false
	})

	if found {
		return answer
	}
	// Naming the right reason for a rejection is worth one re-run. When the
	// departure has already spent a charge, the honest answer depends on
	// whether the charge was what was missing, and the only way to know is to
	// ask the same question of a rested state. ReasonResourceExhausted is
	// reported only when that rested state would have made it.
	if diagnose && !truncated && spentCharges(profile, query.Abilities, from.node.Resources) {
		rested := from
		rested.node.Resources = profile.FullResources()
		probe := query
		probe.From.Resources = rested.node.Resources
		if g.decide(ctx, rested, probe, budget, newSpend(budget), false).Judgement.Certified() {
			return EdgeResult{Judgement: judgeAs(VerdictRejected, ReasonResourceExhausted, profile,
				"the same manoeuvre is certified from a rested state, so what is missing is a charge the departure has already spent", spent.report)}
		}
	}
	switch {
	case truncated:
		return EdgeResult{Judgement: judgeAs(VerdictUnknown, ReasonBudgetExhausted, profile,
			"the search ceiling or the fall horizon ended the enumeration before the question was decided", spent.report)}
	case stopFailed:
		return EdgeResult{Judgement: judgeAs(VerdictRejected, ReasonLandingUnsupported, profile,
			"candidates landed on the destination but could not shed their arrival speed inside its footing; arriving is not standing", spent.report)}
	case arrivalMismatch:
		return EdgeResult{Judgement: judgeAs(VerdictRejected, ReasonLandingUnsupported, profile,
			"candidates came to rest on the destination surface in a state the query does not accept", spent.report)}
	case landedOnTarget:
		return EdgeResult{Judgement: judgeAs(VerdictRejected, ReasonLandingUnsupported, profile,
			"the destination offers no footing the body can land on", spent.report)}
	case abilityShort && !landedSomewhere:
		return EdgeResult{Judgement: judgeAs(VerdictRejected, ReasonAbilityMissing, profile,
			"the manoeuvre needs an ability the moveset does not hold", spent.report)}
	case obstructed:
		return EdgeResult{Judgement: judgeAs(VerdictRejected, ReasonObstructed, profile,
			"every candidate aimed at the destination collided before reaching it", spent.report)}
	default:
		return EdgeResult{Judgement: judgeAs(VerdictRejected, ReasonOutOfEnvelope, profile,
			"no candidate manoeuvre reached the destination", spent.report)}
	}
}

// arrival is the state a candidate ended in, already converted to a node-like
// shape so it can be compared with the query's destination.
type arrival struct {
	surface   SurfaceID
	interval  uint32
	height    float64
	footing   Span
	velocity  Span
	mode      MotionMode
	resources Resources
}

// landingArrival turns a landing into an arrival, enforcing the braking rule.
func (g *geometry) landingArrival(plan launch, result flightResult, profile MovementProfile) (arrival, bool) {
	site := result.site
	if !canStop(profile, site.footing, result.state.X, result.state.VX) {
		return arrival{}, false
	}
	if !math.IsInf(profile.MaxSafeFallHeight, 1) {
		if plan.y0-result.state.Y > profile.MaxSafeFallHeight {
			return arrival{}, false
		}
	}
	return arrival{
		surface:   site.surface,
		interval:  site.interval,
		height:    site.at,
		footing:   site.footing,
		velocity:  Point(0),
		mode:      MotionModeGrounded,
		resources: groundResources(profile, result.resources, result.duration),
	}, true
}

// clingArrival turns a wall contact into an arrival when the moveset can hold
// one. A character without the wall-jump ability simply hits the wall.
func (g *geometry) clingArrival(from nodeRecord, plan launch, result flightResult, query EdgeQuery) (arrival, bool) {
	if result.wall == WallSideNone || !query.Abilities.Has(AbilityWallJump) || query.Profile.WallJump == nil {
		return arrival{}, false
	}
	surface, interval, ok := g.wallAt(result.wall, result.wallX, result.state.Y)
	if !ok {
		return arrival{}, false
	}
	return arrival{
		surface:   surface,
		interval:  interval,
		height:    result.state.Y,
		footing:   Point(result.wallX),
		velocity:  Point(0),
		mode:      MotionModeWallCling,
		resources: clingResources(query.Profile, result.resources, result.wall, result.duration),
	}, true
}

// wallAt finds the wall surface a cling belongs to.
//
// The two vocabularies are mirror images and are easy to cross. WallSide
// names the side of the CHARACTER the wall is on; SurfaceKind names the
// direction the wall's outward normal points. A wall on the character's left
// therefore faces right, and the foot position sits one inset to the right of
// its face.
func (g *geometry) wallAt(side WallSide, x, y float64) (SurfaceID, uint32, bool) {
	inset := g.profile.BodyHalfWidth + g.profile.Margin
	want := SurfaceKindWallRight
	face := x - inset
	if side == WallSideRight {
		want = SurfaceKindWallLeft
		face = x + inset
	}
	for _, surface := range g.surfaces {
		if surface.Kind != want || math.Abs(surface.At-face) > 0.5 {
			continue
		}
		for index, interval := range surface.Intervals {
			if interval.Footing.Contains(y) {
				return surface.ID, uint32(index), true
			}
		}
	}
	return 0, 0, false
}

// matchArrival reports whether an arrival satisfies the query's destination.
func matchArrival(got arrival, query EdgeQuery, profile MovementProfile) bool {
	to := query.To
	if got.surface != to.Surface || got.interval != to.Interval {
		return false
	}
	if math.Abs(got.height-to.Height) > contactEpsilon+profile.Margin {
		return false
	}
	if to.Mode != MotionModeUnspecified && to.Mode != got.mode {
		return false
	}
	if !to.Footing.IsEmpty() && !to.Footing.Overlaps(got.footing) {
		return false
	}
	if !to.Velocity.IsEmpty() && !to.Velocity.Overlaps(got.velocity) {
		return false
	}
	if !query.AnyArrivalResources && to.Resources != got.resources {
		return false
	}
	return true
}

// edgeFrom assembles the MotionEdge a certified candidate produced.
func edgeFrom(plan launch, result flightResult, query EdgeQuery) MotionEdge {
	edge := MotionEdge{
		From:     query.From.ID,
		To:       query.To.ID,
		Kind:     plan.kind,
		Requires: plan.requires,
		Duration: result.duration,
		Witness:  result.witness,
	}
	if query.OmitWitness {
		edge.Witness = nil
	}
	return edge
}

// walkEdge answers the one manoeuvre that is not a flight: moving along a
// supporting surface. It is separate because walking has no arc — the body
// never leaves the ground — and because its cost is the braking profile, not
// a parabola.
func (g *geometry) walkEdge(from nodeRecord, query EdgeQuery, budget SearchBudget) (EdgeResult, bool) {
	to := query.To
	if from.node.Mode != MotionModeGrounded || to.Mode != MotionModeGrounded {
		return EdgeResult{}, false
	}
	if math.Abs(to.Height-from.node.Height) > contactEpsilon {
		return EdgeResult{}, false
	}
	target := to.Footing.Lo
	if to.Footing.IsEmpty() {
		return EdgeResult{}, false
	}
	if !from.walkable.Contains(target) {
		return EdgeResult{}, false
	}
	if !g.walkClear(from.footX(), target, from.node.Height) {
		return EdgeResult{}, false
	}
	if query.Kind != MotionEdgeKindUnspecified && query.Kind != MotionEdgeKindWalk {
		return EdgeResult{}, false
	}
	resources := groundResources(query.Profile, from.node.Resources, walkDuration(query.Profile, target-from.footX()))
	if !query.AnyArrivalResources && to.Resources != resources {
		return EdgeResult{}, false
	}
	duration := walkDuration(query.Profile, target-from.footX())
	edge := MotionEdge{
		From:     query.From.ID,
		To:       to.ID,
		Kind:     MotionEdgeKindWalk,
		Duration: duration,
	}
	if !query.OmitWitness {
		edge.Witness = walkWitness(query.Profile, from.footX(), target, from.node.Height, duration)
	}
	return EdgeResult{
		Edge:      edge,
		Judgement: judgeAs(VerdictCertified, ReasonWitnessFound, query.Profile, "walk certified", BudgetReport{}),
	}, true
}

// walkClear reports whether the body fits along the whole walk.
func (g *geometry) walkClear(from, to, height float64) bool {
	step := g.profile.BodyHalfWidth
	if step <= 0 {
		step = 0.25
	}
	count := int(math.Ceil(math.Abs(to-from)/step)) + 1
	for i := 0; i <= count; i++ {
		x := from + (to-from)*float64(i)/float64(count)
		if !g.bodyFree(x, height) {
			return false
		}
	}
	return true
}

// walkWitness builds the two-phase trajectory of a walk: accelerate, then
// brake. It is a witness like any other, so an invariant test can replay it.
func walkWitness(profile MovementProfile, from, to, height, duration float64) *Witness {
	direction := 1.0
	if to < from {
		direction = -1
	}
	half := duration / 2
	peak := direction * math.Abs(to-from) / math.Max(duration, contactEpsilon) * 2
	middle := MotionState{X: (from + to) / 2, Y: height, VX: peak}
	hold := InputRight
	if direction < 0 {
		hold = InputLeft
	}
	return &Witness{
		Phases: []Phase{
			{
				Mode:     MotionModeGrounded,
				Start:    MotionState{X: from, Y: height},
				End:      middle,
				Duration: half,
				AccelX:   peak / math.Max(half, contactEpsilon),
			},
			{
				Mode:     MotionModeGrounded,
				Start:    middle,
				End:      MotionState{X: to, Y: height},
				Duration: half,
				AccelX:   -peak / math.Max(half, contactEpsilon),
			},
		},
		Commands:    []Command{{At: 0, Hold: hold}, {At: half, Hold: 0}},
		ControlRate: profile.ControlRate,
	}
}

// describe resolves a caller-supplied MotionNode against the geometry.
func (g *geometry) describe(node MotionNode) (nodeRecord, error) {
	if int(node.Surface) >= len(g.surfaces) {
		return nodeRecord{}, queryError("node %d names surface %d of %d", node.ID, node.Surface, len(g.surfaces))
	}
	surface := g.surfaces[node.Surface]
	if int(node.Interval) >= len(surface.Intervals) {
		return nodeRecord{}, queryError("node %d names interval %d of surface %d, which has %d",
			node.ID, node.Interval, node.Surface, len(surface.Intervals))
	}
	if node.Mode == MotionModeUnspecified {
		return nodeRecord{}, queryError("node %d has an unspecified mode", node.ID)
	}
	record := nodeRecord{node: node, exempt: g.departureOf(node.Surface)}
	switch node.Mode {
	case MotionModeWallCling:
		record.wall = WallSideRight
		if surface.Kind == SurfaceKindWallRight {
			record.wall = WallSideLeft
		}
		record.walkable = surface.Intervals[node.Interval].Footing
	case MotionModeClimbing:
		record.climb = true
		record.walkable = surface.Intervals[node.Interval].Footing
	default:
		record.semi = surface.Kind == SurfaceKindSemiSolid
		record.walkable = g.walkableSpan(surface, node.Interval)
		record.ledgeLo = node.Footing.Lo <= record.walkable.Lo+contactEpsilon
		record.ledgeHi = node.Footing.Hi >= record.walkable.Hi-contactEpsilon
		record.site = landingSite{
			surface:  node.Surface,
			interval: node.Interval,
			at:       surface.At,
			footing:  surface.Intervals[node.Interval].Footing,
			semi:     record.semi,
		}
	}
	return record, nil
}

// walkableSpan returns the contiguous run of occupiable intervals that
// contains one interval. Walking never crosses an interval too low for the
// body, so the run-up and the braking room both stop there.
func (g *geometry) walkableSpan(surface Surface, index uint32) Span {
	occupiable := func(i int) bool {
		if i < 0 || i >= len(surface.Intervals) {
			return false
		}
		interval := surface.Intervals[i]
		return !interval.Footing.IsEmpty() && !interval.Hazard && interval.Headroom >= g.profile.BodyHeight
	}
	if !occupiable(int(index)) {
		return Span{Lo: 1, Hi: 0}
	}
	lo := int(index)
	for occupiable(lo - 1) {
		lo--
	}
	hi := int(index)
	for occupiable(hi + 1) {
		hi++
	}
	return Span{Lo: surface.Intervals[lo].Footing.Lo, Hi: surface.Intervals[hi].Footing.Hi}
}

// room is a helper that keeps CheckEdge's geometry construction readable; a
// SurfaceID carries no room of its own, and an EdgeQuery has no room field,
// so every single-manoeuvre question is answered in room zero.
func (SurfaceID) room() RoomID { return 0 }
