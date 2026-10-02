package platform

import "math"

// This file is the kinematic half of the M1 oracle: the phase machine that
// turns one planned manoeuvre into a trajectory.
//
// # Phases, not a formula
//
// There is no single closed form for "a jump". A jump is a finite SEQUENCE of
// constant-acceleration phases separated by events: the apex, where the rising
// gravity gives way to the falling one; the jump release, which cuts the
// vertical velocity; the mid-air jump, which replaces it; the dash, which
// fixes both components and may switch gravity off; the terminal-velocity
// clamp, which switches gravity off again; the horizontal speed clamp, where
// the air acceleration stops mattering. Section 4.6 of the design notes calls
// for exactly this and warns that "no physics engine" does not mean "one
// equation". Every phase this machine emits is also a Phase in the witness,
// so a consumer re-integrates the same segments the oracle reasoned about.
//
// # Coyote time is a phase, never an offset
//
// Leaving a ledge and jumping tau seconds later is modelled as a phase in
// MotionModeCoyote with the ordinary falling acceleration, followed by the
// ground jump applied to whatever state that phase ended in. The jump
// therefore starts g*tau^2/2 LOWER and V*tau further along. Folding the window
// into the envelope as "+V*Tc" keeps the gain and discards the loss, which
// manufactures edges a player cannot perform: the loss is quadratic in tau and
// the gain is only linear, so the two never cancel. tau is a planning variable
// of the search, sampled from the budget's TimeResolution, and never a
// constant of the profile.

// maxPhases bounds one manoeuvre's phase count. The real ceiling is far
// lower — a jump with a release, a mid-air jump, a dash and two clamps is
// eight — and the bound exists so a pathological profile cannot spin.
const maxPhases = 48

// launch is one candidate manoeuvre: where it starts, what it does and when.
// Every field is fixed before the simulation begins, so the trajectory is a
// pure function of the launch and the geometry.
type launch struct {
	// kind is the manoeuvre's edge kind, used for the edge it may produce.
	kind MotionEdgeKind
	// requires is the ability set the manoeuvre consumes.
	requires AbilitySet

	// x0, y0 is the departure foot position in the world frame.
	x0, y0 float64
	// vx, vy is the departure velocity.
	vx, vy float64
	// mode is the control regime the manoeuvre starts in.
	mode MotionMode

	// airHold is the direction held in the air: -1, 0 or +1. A non-zero value
	// applies AirAccel until MaxRunSpeed is reached.
	airHold int

	// jumpRelease is when the jump button is let go, for a profile with a
	// variable jump. Positive infinity holds it through the apex.
	jumpRelease float64
	// coyoteJumpAt is when the ground jump is taken after leaving a ledge. It
	// is NaN for every manoeuvre that is not a coyote jump, and it consumes no
	// air jump because the jump it takes is the grounded one.
	coyoteJumpAt float64
	// airJumpAt is when the mid-air jump happens, or NaN.
	airJumpAt float64
	// dashAt is when the dash burst starts, or NaN.
	dashAt float64
	// dashDir is the dash's horizontal sign.
	dashDir float64

	// exempt is the blocking run the arc leaves, for the overhang rule.
	exempt departure
	// ignoreSemi is the semi-solid surface a drop-through passes through, and
	// ignoreSemiActive says whether there is one. A drop-through ignores
	// exactly the platform it leaves and no other, for the whole arc: section
	// 4.5 requires the command to declare its scope, and ignoring every
	// semi-solid would turn one drop into a fall through the whole room.
	ignoreSemi       SurfaceID
	ignoreSemiActive bool

	// resources is the consumable state at departure.
	resources Resources
}

// newLaunch returns a launch with the optional events disabled.
func newLaunch() launch {
	return launch{
		jumpRelease:  math.Inf(1),
		coyoteJumpAt: math.NaN(),
		airJumpAt:    math.NaN(),
		dashAt:       math.NaN(),
	}
}

// stepper advances one launch through its phases. It is the single place the
// equations of motion live: the aiming solver and the collision-checked
// simulation drive the same stepper, so a manoeuvre can never be aimed with
// one model and verified with another.
type stepper struct {
	profile   MovementProfile
	abilities AbilitySet
	plan      launch

	t     float64
	state MotionState
	mode  MotionMode
	res   Resources

	released  bool
	coyoted   bool
	airJumped bool
	dashed    bool
	dashEnd   float64

	phases   []Phase
	commands []Command
	hold     Input
	failed   VerdictReason
}

// newStepper starts a manoeuvre. It returns a VALUE, not a pointer: the
// aiming solver runs it hundreds of thousands of times per room without
// recording anything, and a heap allocation per probe is the difference
// between a room that builds in milliseconds and one that does not.
func newStepper(profile MovementProfile, abilities AbilitySet, plan launch, record bool) stepper {
	s := stepper{
		profile:   profile,
		abilities: abilities,
		plan:      plan,
		state:     MotionState{X: plan.x0, Y: plan.y0, VX: plan.vx, VY: plan.vy},
		mode:      plan.mode,
		res:       plan.resources,
		dashEnd:   math.NaN(),
	}
	if record {
		s.phases = make([]Phase, 0, 8)
		s.commands = make([]Command, 0, 6)
	}
	s.hold = s.initialHold()
	s.emitCommand(0)
	return s
}

// initialHold returns the controller state the manoeuvre starts with.
func (s *stepper) initialHold() Input {
	var hold Input
	switch {
	case s.plan.airHold > 0:
		hold |= InputRight
	case s.plan.airHold < 0:
		hold |= InputLeft
	case s.plan.vx > 0:
		hold |= InputRight
	case s.plan.vx < 0:
		hold |= InputLeft
	}
	if s.plan.vy > 0 || s.plan.mode == MotionModeAirborne && s.plan.vy > 0 {
		hold |= InputJump
	}
	if s.plan.mode == MotionModeDashing {
		hold |= InputDash
	}
	return hold
}

// emitCommand records the controller state at a time, collapsing a repeat.
func (s *stepper) emitCommand(at float64) {
	if s.commands == nil {
		return
	}
	if n := len(s.commands); n > 0 {
		if s.commands[n-1].Hold == s.hold {
			return
		}
		if at-s.commands[n-1].At <= timeEpsilon {
			s.commands[n-1].Hold = s.hold
			return
		}
	}
	s.commands = append(s.commands, Command{At: at, Hold: s.hold})
}

// tick is one controller step, used to separate a release from the press that
// follows it so a witness is replayable by a real controller.
func (s *stepper) tick() float64 {
	if s.profile.ControlRate > 0 {
		return 1 / s.profile.ControlRate
	}
	return timeEpsilon
}

// gravity returns the vertical acceleration the current state is under.
func (s *stepper) gravity() float64 {
	if s.mode == MotionModeDashing && s.profile.Dash != nil && s.profile.Dash.SuspendsGravity {
		return 0
	}
	if s.state.VY <= -s.profile.MaxFallSpeed+contactEpsilon {
		return 0
	}
	if s.state.VY > 0 {
		if s.released && s.profile.VariableJump != nil &&
			s.profile.VariableJump.Mode == VariableJumpModeSwapGravity &&
			s.profile.VariableJump.ReleaseGravity > 0 {
			return -s.profile.VariableJump.ReleaseGravity
		}
		return -s.profile.GravityUp
	}
	return -s.profile.GravityDown
}

// horizontal returns the horizontal acceleration the current state is under.
func (s *stepper) horizontal() float64 {
	if s.mode == MotionModeDashing || s.plan.airHold == 0 || s.profile.AirAccel <= 0 {
		return 0
	}
	target := float64(float64(s.plan.airHold) * s.profile.MaxRunSpeed)
	if math.Abs(s.state.VX-target) <= contactEpsilon {
		return 0
	}
	if s.state.VX < target {
		return s.profile.AirAccel
	}
	return -s.profile.AirAccel
}

// boundary returns the shortest duration after which the current phase's
// constant accelerations stop being the right ones.
func (s *stepper) boundary(ax, ay float64) float64 {
	best := math.Inf(1)
	consider := func(dt float64) {
		if dt > timeEpsilon && dt < best {
			best = dt
		}
	}
	// Scheduled events.
	consider(s.plan.jumpRelease - s.t)
	consider(s.plan.coyoteJumpAt - s.t)
	consider(s.plan.airJumpAt - s.t)
	consider(s.plan.dashAt - s.t)
	consider(s.dashEnd - s.t)
	// The apex, where the rising gravity gives way to the falling one.
	if ay < 0 && s.state.VY > 0 {
		consider(s.state.VY / -ay)
	}
	// The terminal-velocity clamp.
	if ay < 0 && s.state.VY > -s.profile.MaxFallSpeed {
		consider((s.state.VY + s.profile.MaxFallSpeed) / -ay)
	}
	// The horizontal speed clamp.
	if ax != 0 {
		target := float64(float64(s.plan.airHold) * s.profile.MaxRunSpeed)
		consider(float64(target-s.state.VX) / ax)
	}
	return best
}

// next returns the arc of the current phase, bounded by limit.
func (s *stepper) next(limit float64) (arc, bool) {
	if s.failed != ReasonUnspecified || len(s.phases) >= maxPhases {
		return arc{}, false
	}
	if s.t >= limit-timeEpsilon {
		return arc{}, false
	}
	ax, ay := s.horizontal(), s.gravity()
	span := math.Min(s.boundary(ax, ay), limit-s.t)
	if math.IsInf(span, 1) || span <= timeEpsilon {
		return arc{}, false
	}
	return arc{
		x0: s.state.X, y0: s.state.Y,
		vx: s.state.VX, vy: s.state.VY,
		ax: ax, ay: ay,
		span: span,
	}, true
}

// advance integrates dt of the arc, records the phase and, when the arc ran to
// its own end, applies whatever event ended it.
func (s *stepper) advance(a arc, dt float64, complete bool) {
	if dt < 0 {
		dt = 0
	}
	start := s.state
	s.state = MotionState{X: a.xAt(dt), Y: a.yAt(dt), VX: a.vxAt(dt), VY: a.vyAt(dt)}
	s.t += dt
	if s.phases != nil {
		s.phases = append(s.phases, Phase{
			Mode:     s.mode,
			Start:    start,
			End:      s.state,
			Duration: dt,
			AccelX:   a.ax,
			AccelY:   a.ay,
		})
	}
	if s.state.VY < -s.profile.MaxFallSpeed {
		s.state.VY = -s.profile.MaxFallSpeed
	}
	if !complete {
		return
	}
	s.applyEvents()
}

// applyEvents fires every scheduled event that falls on the current time.
func (s *stepper) applyEvents() {
	near := func(at float64) bool {
		return !math.IsNaN(at) && !math.IsInf(at, 1) && math.Abs(s.t-at) <= timeEpsilon*8
	}
	if !s.released && near(s.plan.jumpRelease) {
		s.release()
	}
	if !s.coyoted && near(s.plan.coyoteJumpAt) {
		s.coyoteJump()
	}
	if !s.airJumped && near(s.plan.airJumpAt) {
		s.airJump()
	}
	if !s.dashed && near(s.plan.dashAt) {
		s.startDash()
	}
	if s.mode == MotionModeDashing && near(s.dashEnd) {
		s.endDash()
	}
}

// release applies the variable jump's cut.
func (s *stepper) release() {
	s.released = true
	s.hold &^= InputJump
	s.emitCommand(s.t)
	variable := s.profile.VariableJump
	if variable == nil || variable.Mode != VariableJumpModeCutVelocity {
		return
	}
	if s.state.VY > 0 {
		s.state.VY *= variable.CutFactor
	}
}

// coyoteJump applies the grounded jump taken inside the coyote window. It
// replaces the vertical velocity from WHEREVER the fall had reached, which is
// what makes the window a trade and not a bonus.
func (s *stepper) coyoteJump() {
	s.coyoted = true
	s.mode = MotionModeAirborne
	s.state.VY = s.profile.JumpVelocity
	s.hold |= InputJump
	s.emitCommand(s.t)
}

// airJump applies the mid-air jump, consuming a charge.
func (s *stepper) airJump() {
	s.airJumped = true
	profile := s.profile.DoubleJump
	if profile == nil || s.res.AirJumps == 0 {
		s.failed = ReasonResourceExhausted
		return
	}
	s.res.AirJumps--
	switch profile.Mode {
	case DoubleJumpModeImpulse:
		s.state.VY += profile.Velocity
	default:
		s.state.VY = profile.Velocity
	}
	s.mode = MotionModeAirborne
	if s.hold&InputJump != 0 {
		s.hold &^= InputJump
		s.emitCommand(math.Max(0, s.t-s.tick()))
	}
	s.hold |= InputJump
	s.emitCommand(s.t)
}

// startDash begins the burst, consuming a charge and fixing the velocity.
func (s *stepper) startDash() {
	s.dashed = true
	profile := s.profile.Dash
	if profile == nil {
		s.failed = ReasonUnsupportedMoveset
		return
	}
	if s.res.DashCharges == 0 || s.res.DashCooldown > contactEpsilon {
		s.failed = ReasonResourceExhausted
		return
	}
	s.res.DashCharges--
	s.res.DashCooldown = profile.Cooldown
	speed := profile.Speed
	if s.abilities.Has(AbilityShadowDash) && profile.ShadowSpeed > 0 {
		speed = profile.ShadowSpeed
	}
	s.mode = MotionModeDashing
	s.state.VX = s.plan.dashDir * speed
	if profile.SuspendsGravity {
		s.state.VY = 0
	}
	s.dashEnd = s.t + profile.Duration
	s.hold |= InputDash
	s.emitCommand(s.t)
}

// endDash leaves the burst and applies the declared exit velocity.
func (s *stepper) endDash() {
	profile := s.profile.Dash
	s.mode = MotionModeAirborne
	s.dashEnd = math.NaN()
	switch profile.Exit {
	case DashExitModeZero:
		s.state.VX = 0
	case DashExitModeClampToRun:
		s.state.VX = math.Max(-s.profile.MaxRunSpeed, math.Min(s.profile.MaxRunSpeed, s.state.VX))
	}
	s.hold &^= InputDash
	s.emitCommand(s.t)
}

// witness assembles the recorded trajectory, or nil when recording was off.
func (s *stepper) witness() *Witness {
	if s.phases == nil {
		return nil
	}
	return &Witness{Phases: s.phases, Commands: s.commands, ControlRate: s.profile.ControlRate}
}

// flightCeiling returns the longest time a manoeuvre is allowed to last: the
// time to rise to the apex and then fall the whole fall horizon, plus the dash
// burst. Beyond it the search stops looking, which is a budget decision and
// therefore never a rejection.
func flightCeiling(profile MovementProfile, budget SearchBudget) float64 {
	rise := 0.0
	if profile.GravityUp > 0 {
		rise = 2 * profile.JumpVelocity / profile.GravityUp
	}
	drop := budget.FallHorizon + profile.ApexHeight() + 1
	fall := 0.0
	if profile.GravityDown > 0 {
		fall = math.Sqrt(2 * drop / profile.GravityDown)
	}
	if profile.MaxFallSpeed > 0 && !math.IsInf(profile.MaxFallSpeed, 1) {
		fall = math.Max(fall, drop/profile.MaxFallSpeed+profile.MaxFallSpeed/math.Max(profile.GravityDown, 1))
	}
	burst := 0.0
	if profile.Dash != nil {
		burst = profile.Dash.Duration
	}
	return rise + fall + burst + 1
}

// descentTime returns the first time the feet descend through a target height
// under a launch, ignoring all geometry. It drives candidate aiming: with the
// vertical motion decoupled from the horizontal one, the flight time to a
// height fixes the horizontal budget, and section 4.3's witness construction
// then solves for the launch velocity directly instead of sweeping it.
//
// A contact that is tangent at the apex is NOT a descent. The crossing must
// have strictly negative vertical velocity, which is the same rule the
// collision sweep applies, so an aimed candidate and the simulation that
// verifies it agree about the boundary case rather than disagreeing by a
// rounding error.
func descentTime(profile MovementProfile, abilities AbilitySet, plan launch, target, ceiling float64) (float64, bool) {
	s := newStepper(profile, abilities, plan, false)
	for {
		a, ok := s.next(ceiling)
		if !ok {
			return 0, false
		}
		found := roots(a.y0-target, a.vy, a.ay, a.span)
		for i := 0; i < found.n; i++ {
			t := found.at[i]
			if t <= timeEpsilon || a.vyAt(t) >= -contactEpsilon {
				continue
			}
			return s.t + t, true
		}
		s.advance(a, a.span, true)
		if s.failed != ReasonUnspecified {
			return 0, false
		}
	}
}

// horizontalReach returns the horizontal displacement a launch produces after
// a given flight time with a given launch velocity, under the same phase
// machine. It is used to invert the aiming for manoeuvres whose horizontal
// motion is not simply u*t — a dash fixes the velocity for its burst, and air
// control accelerates — rather than assuming a constant velocity that the
// profile does not have.
func horizontalReach(profile MovementProfile, abilities AbilitySet, plan launch, flight float64) float64 {
	s := newStepper(profile, abilities, plan, false)
	for {
		a, ok := s.next(flight)
		if !ok {
			return s.state.X - plan.x0
		}
		if s.t+a.span >= flight-timeEpsilon {
			return a.xAt(flight-s.t) - plan.x0
		}
		s.advance(a, a.span, true)
		if s.failed != ReasonUnspecified {
			return s.state.X - plan.x0
		}
	}
}

// walkDuration returns the time to walk between two foot positions on one
// surface: accelerate to the run speed, cruise, then brake to rest. A short
// hop never reaches the run speed, and the triangular profile is used instead.
func walkDuration(profile MovementProfile, distance float64) float64 {
	distance = math.Abs(distance)
	if distance <= contactEpsilon {
		return 0
	}
	accel := profile.GroundAccel
	brake := profile.Braking
	if accel <= 0 || brake <= 0 {
		return math.Inf(1)
	}
	peak := math.Sqrt(2 * distance / (1/accel + 1/brake))
	if peak <= profile.MaxRunSpeed {
		return peak/accel + peak/brake
	}
	v := profile.MaxRunSpeed
	ramp := v*v/(2*accel) + v*v/(2*brake)
	return v/accel + v/brake + (distance-ramp)/v
}

// runUpDistance returns the ground distance needed to reach a launch speed
// from rest. A launch velocity the character cannot build up to on the surface
// it departs from is not available, which is the other half of section 4.3's
// warning: a safe interval is not a free choice of launch position.
func runUpDistance(profile MovementProfile, speed float64) float64 {
	speed = math.Abs(speed)
	if speed <= contactEpsilon {
		return 0
	}
	if profile.GroundAccel <= 0 {
		return math.Inf(1)
	}
	return speed * speed / (2 * profile.GroundAccel)
}
