package platform

import (
	"math"
	"testing"
)

// The numbers in this file are the ones the design notes verified against
// independent numerical integration. Where a test quotes a figure to many
// digits, that figure comes from that check and not from running this code
// and writing down what it printed.

// singleGravityProfile is the worked example of section 4.2: apex 4 cells in
// 0.4 s with one gravity for the rise and the fall, a run of 8 cells per
// second and no terminal velocity in the way.
func singleGravityProfile() MovementProfile {
	profile := testProfile()
	profile.GravityDown = profile.GravityUp
	profile.MaxFallSpeed = 1000
	return profile
}

func TestTheBallisticNumbersAreTheVerifiedOnes(t *testing.T) {
	profile := singleGravityProfile()
	if got := profile.GravityUp; got != 50 {
		t.Fatalf("g = 2H/Ta^2 = 50, got %v", got)
	}
	if got := profile.JumpVelocity; got != 20 {
		t.Fatalf("J = 2H/Ta = 20, got %v", got)
	}

	plan := newLaunch()
	plan.y0 = 0
	plan.vy = profile.JumpVelocity
	plan.mode = MotionModeAirborne
	ceiling := flightCeiling(profile, testBudget())

	cases := []struct {
		name  string
		delta float64
		want  float64
	}{
		{name: "same height", delta: 0, want: 0.8},
		{name: "two cells up", delta: 2, want: 0.6828427124746191},
		{name: "three cells down", delta: -3, want: 0.9291502622129182},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := descentTime(profile, 0, plan, c.delta, ceiling)
			if !ok {
				t.Fatalf("the phase machine must reach %v", c.delta)
			}
			if math.Abs(got-c.want) > 1e-9 {
				t.Fatalf("t_land: want %v, got %v", c.want, got)
			}
			if reach := horizontalReach(profile, 0, withLaunchSpeed(plan, profile.MaxRunSpeed), got); math.Abs(reach-profile.MaxRunSpeed*c.want) > 1e-9 {
				t.Fatalf("V*t: want %v, got %v", profile.MaxRunSpeed*c.want, reach)
			}
		})
	}
}

func withLaunchSpeed(plan launch, u float64) launch {
	plan.vx = u
	return plan
}

func TestReachingTheApexHeightIsNotADescentThroughIt(t *testing.T) {
	// D = J^2 - 2*g*dy is zero at dy = H. Section 4.2 is explicit that the
	// tangent contact must not be accepted implicitly, and this is where the
	// whole package refuses it.
	profile := singleGravityProfile()
	plan := newLaunch()
	plan.vy = profile.JumpVelocity
	plan.mode = MotionModeAirborne
	ceiling := flightCeiling(profile, testBudget())

	if _, ok := descentTime(profile, 0, plan, profile.ApexHeight(), ceiling); ok {
		t.Fatalf("dy = H is tangent and must not be a landing")
	}
	if _, ok := descentTime(profile, 0, plan, profile.ApexHeight()+0.001, ceiling); ok {
		t.Fatalf("dy above H is not reached at all")
	}
	if _, ok := descentTime(profile, 0, plan, profile.ApexHeight()-0.001, ceiling); !ok {
		t.Fatalf("dy just below H is reached descending")
	}
}

func TestTheCoyoteWindowTradesHeightForDistanceInThePhaseMachine(t *testing.T) {
	// The table of section 2.7, reproduced by running the phases rather than
	// by evaluating the closed form the profile already exposes. The point is
	// that the SIMULATION loses the height, not merely the documentation.
	profile := singleGravityProfile()
	ceiling := flightCeiling(profile, testBudget())
	for _, tau := range []float64{0, 0.02, 0.04, 0.08, 0.16} {
		plan := newLaunch()
		plan.y0 = 4
		plan.vx = profile.MaxRunSpeed
		plan.mode = MotionModeCoyote
		plan.coyoteJumpAt = tau
		if tau == 0 {
			plan.mode = MotionModeAirborne
			plan.vy = profile.JumpVelocity
		}

		apex := apexOf(t, profile, plan, ceiling)
		wantLoss := profile.CoyoteHeightLoss(tau)
		if gotLoss := 8 - apex; math.Abs(gotLoss-wantLoss) > 1e-9 {
			t.Fatalf("tau=%v: height loss is g*tau^2/2 = %v, got %v", tau, wantLoss, gotLoss)
		}
		gain := profile.CoyoteHorizontalGain(tau)
		if math.Abs(gain-profile.MaxRunSpeed*tau) > 1e-12 {
			t.Fatalf("tau=%v: the horizontal gain is linear", tau)
		}
	}
	// Doubling tau doubles the gain and QUADRUPLES the loss. That is the whole
	// reason the window cannot be folded into an envelope offset.
	if got, want := profile.CoyoteHeightLoss(0.16)/profile.CoyoteHeightLoss(0.08), 4.0; math.Abs(got-want) > 1e-9 {
		t.Fatalf("the loss is quadratic in tau, ratio %v", got)
	}
	if got, want := profile.CoyoteHorizontalGain(0.16)/profile.CoyoteHorizontalGain(0.08), 2.0; math.Abs(got-want) > 1e-9 {
		t.Fatalf("the gain is linear in tau, ratio %v", got)
	}
}

// apexOf runs a plan and returns the highest the feet reach.
func apexOf(t *testing.T, profile MovementProfile, plan launch, ceiling float64) float64 {
	t.Helper()
	s := newStepper(profile, 0, plan, false)
	best := plan.y0
	for {
		a, ok := s.next(ceiling)
		if !ok {
			return best
		}
		if top := axisBounds(a.y0, a.vy, a.ay, a.span).Hi; top > best {
			best = top
		}
		if s.state.VY < 0 && s.t > 0 {
			return best
		}
		s.advance(a, a.span, true)
	}
}

// wideCoyoteProfile opens the window to 0.2 s, which is where the trade is
// large enough for a fixture to tell the two models apart with room to spare.
func wideCoyoteProfile() MovementProfile {
	profile := testProfile()
	coyote := *profile.Coyote
	coyote.Window = 0.2
	profile.Coyote = &coyote
	return profile
}

// ledgeRoom is a floor on the left and a higher platform on the right, placed
// at a distance given in cells from the left ledge.
func ledgeRoom(platformFrom int) []string {
	const height, columns = 12, 22
	rows := make([]string, height)
	for i := range rows {
		blank := make([]byte, columns)
		for c := range blank {
			blank[c] = '.'
		}
		rows[i] = string(blank)
	}
	floor := make([]byte, columns)
	shelf := make([]byte, columns)
	for c := range floor {
		floor[c] = '.'
		shelf[c] = '.'
	}
	for c := 0; c <= 5; c++ {
		floor[c] = '#'
	}
	for c := platformFrom; c < columns; c++ {
		shelf[c] = '#'
	}
	rows[height-1] = string(floor)
	rows[8] = string(shelf) // world height 4
	return rows
}

func TestModellingCoyoteAsAnEnvelopeOffsetWouldCertifyAJumpThatDoesNotExist(t *testing.T) {
	profile := wideCoyoteProfile()

	// The three numbers the fixture turns on, all at a destination three
	// cells above the ledge.
	plain := reachAtHeight(t, profile, 0, 3)
	naive := plain + profile.CoyoteHorizontalGain(profile.Coyote.Window)
	best := 0.0
	for tau := 0.0; tau <= profile.Coyote.Window+1e-12; tau += 0.005 {
		if got := reachAtHeight(t, profile, tau, 3); got > best {
			best = got
		}
	}
	if !(best < naive) {
		t.Fatalf("the real coyote reach %v must fall short of the naive envelope %v", best, naive)
	}
	t.Logf("plain=%.4f real-coyote-max=%.4f naive-offset=%.4f", plain, best, naive)

	// A platform the naive envelope reaches and the phase machine does not.
	// The left ledge is at 5.6, so a platform whose footing starts at 11.4
	// demands 5.8 cells.
	const demanded = 11.4 - 5.6
	if !(best < demanded && demanded < naive) {
		t.Fatalf("the fixture must sit strictly between the two models: real %v, demanded %v, naive %v", best, demanded, naive)
	}
	far := buildGraph(t, ledgeRoom(11), profile, 0)
	ledge := nodeAt(t, &far.Graph, 5.6, 1)
	shelf := nodeAt(t, &far.Graph, 11.4, 4)
	result := checkEdge(t, ledgeRoom(11), profile, 0, ledge, shelf)
	if result.Judgement.Certified() {
		t.Fatalf("the naive envelope would certify this and the phase machine must not: %s", result.Judgement.Detail)
	}

	// The same fixture one cell closer IS reached, and only with the window:
	// the plain jump falls short of it too.
	const closer = 10.4 - 5.6
	if !(plain < closer && closer < best) {
		t.Fatalf("the control must need the window and nothing more: plain %v, demanded %v, real %v", plain, closer, best)
	}
	near := buildGraph(t, ledgeRoom(10), profile, 0)
	nearLedge := nodeAt(t, &near.Graph, 5.6, 1)
	nearShelf := nodeAt(t, &near.Graph, 10.4, 4)
	reached := checkEdge(t, ledgeRoom(10), profile, 0, nearLedge, nearShelf)
	if !reached.Judgement.Certified() {
		t.Fatalf("the coyote window must carry this one: %s/%s (%s)",
			reached.Judgement.Verdict, reached.Judgement.Reason, reached.Judgement.Detail)
	}
	coyote := false
	for _, phase := range reached.Edge.Witness.Phases {
		if phase.Mode == MotionModeCoyote {
			coyote = true
		}
	}
	if !coyote {
		t.Fatalf("the witness must contain the coyote phase it depends on: %v", reached.Edge.Witness.Phases)
	}
}

// reachAtHeight returns the farthest a jump taken tau into the coyote window
// travels before descending through a height rise above the ledge.
func reachAtHeight(t *testing.T, profile MovementProfile, tau, rise float64) float64 {
	t.Helper()
	plan := newLaunch()
	plan.y0 = 0
	plan.vx = profile.MaxRunSpeed
	plan.mode = MotionModeAirborne
	plan.vy = profile.JumpVelocity
	if tau > 0 {
		plan.mode = MotionModeCoyote
		plan.vy = 0
		plan.coyoteJumpAt = tau
	}
	ceiling := flightCeiling(profile, testBudget())
	flight, ok := descentTime(profile, 0, plan, rise, ceiling)
	if !ok {
		return 0
	}
	return horizontalReach(profile, 0, plan, flight)
}

func TestTheHorizontalAccelerationIsPiecewiseAndContinuous(t *testing.T) {
	// Section 4.6: dx(t) = v0 t + a t^2/2 up to ts = (V-v0)/a and then adds
	// V(t-ts). The join at ts is C1, and a discontinuity there would be a
	// phantom jump in velocity.
	profile := singleGravityProfile()
	profile.AirAccel = 40
	plan := newLaunch()
	plan.mode = MotionModeAirborne
	plan.vy = profile.JumpVelocity
	plan.vx = 2
	plan.airHold = 1

	v0, accel, limit := 2.0, profile.AirAccel, profile.MaxRunSpeed
	ts := (limit - v0) / accel
	closed := func(t float64) float64 {
		if t <= ts {
			return v0*t + accel*t*t/2
		}
		return v0*ts + accel*ts*ts/2 + limit*(t-ts)
	}
	for _, at := range []float64{0, ts / 2, ts - 1e-6, ts, ts + 1e-6, ts * 2, ts * 3} {
		got := horizontalReach(profile, 0, plan, at)
		if math.Abs(got-closed(at)) > 1e-9 {
			t.Fatalf("dx(%v): want %v, got %v", at, closed(at), got)
		}
	}
	before, after := horizontalReach(profile, 0, plan, ts-1e-7), horizontalReach(profile, 0, plan, ts+1e-7)
	if slope := (after - before) / 2e-7; math.Abs(slope-limit) > 1e-3 {
		t.Fatalf("the derivative at ts must be the run speed, got %v", slope)
	}
}

func TestStoppingDistanceAndRunUpAreTheSameArithmetic(t *testing.T) {
	// v^2/(2a) in both directions: the distance to build a launch speed and
	// the distance to shed an arrival speed. They differ only in which
	// acceleration they use, and the profile keeps the two apart on purpose.
	profile := testProfile()
	if got, want := runUpDistance(profile, 8), 64.0/(2*profile.GroundAccel); math.Abs(got-want) > 1e-12 {
		t.Fatalf("run-up: want %v, got %v", want, got)
	}
	if got, want := profile.StoppingDistance(8), 64.0/(2*profile.Braking); math.Abs(got-want) > 1e-12 {
		t.Fatalf("braking: want %v, got %v", want, got)
	}
	if runUpDistance(profile, 8) == profile.StoppingDistance(8) {
		t.Fatalf("GroundAccel and Braking are not interchangeable and this profile must not pretend they are")
	}
}

func TestWalkDurationUsesTheTriangularProfileWhenTheRunSpeedIsNeverReached(t *testing.T) {
	profile := testProfile()
	// A hop short enough that the character is still accelerating when it
	// must start braking.
	short := 0.1
	peak := math.Sqrt(2 * short / (1/profile.GroundAccel + 1/profile.Braking))
	if peak >= profile.MaxRunSpeed {
		t.Fatalf("this fixture needs a distance the run speed is never reached over")
	}
	want := peak/profile.GroundAccel + peak/profile.Braking
	if got := walkDuration(profile, short); math.Abs(got-want) > 1e-12 {
		t.Fatalf("short walk: want %v, got %v", want, got)
	}
	// A long one cruises.
	long := 20.0
	v := profile.MaxRunSpeed
	ramp := v*v/(2*profile.GroundAccel) + v*v/(2*profile.Braking)
	wantLong := v/profile.GroundAccel + v/profile.Braking + (long-ramp)/v
	if got := walkDuration(profile, long); math.Abs(got-wantLong) > 1e-12 {
		t.Fatalf("long walk: want %v, got %v", wantLong, got)
	}
	if walkDuration(profile, 0) != 0 {
		t.Fatalf("standing still takes no time")
	}
}

func TestTheFallIsClampedAtTheTerminalSpeed(t *testing.T) {
	profile := testProfile()
	plan := newLaunch()
	plan.y0 = 100
	plan.mode = MotionModeAirborne
	s := newStepper(profile, 0, plan, true)
	for i := 0; i < 8; i++ {
		a, ok := s.next(10)
		if !ok {
			break
		}
		s.advance(a, a.span, true)
	}
	if s.state.VY < -profile.MaxFallSpeed-1e-9 {
		t.Fatalf("the fall must clamp at %v, reached %v", profile.MaxFallSpeed, s.state.VY)
	}
	clamped := false
	for _, phase := range s.phases {
		if phase.AccelY == 0 && phase.Start.VY < 0 {
			clamped = true
		}
	}
	if !clamped {
		t.Fatalf("the clamp must be its own phase, not a quietly wrong acceleration: %v", s.phases)
	}
}
