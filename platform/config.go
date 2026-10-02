package platform

import "math"

// V1 product limits for platform generation. They apply equally to the SDK and
// to any service in front of it: a request above one of them fails with
// ErrLimitExceeded before any allocation, generation or RNG consumption, and
// is never silently truncated.
//
// MaxRoomSide is 1024 while the dungeon generator's per-axis ceiling is 256.
// That divergence is deliberate: a dungeon grid is roughly square, and a
// platform room is wide and short. The product ceiling is the same 65,536
// cells, so a room may be 1024x64 but not 1024x1024, in the same way that the
// dungeon rejects 512x128 despite its 65,536 cells.
const (
	// MaxRoomSide is the largest width or height of one room's grid, in cells.
	MaxRoomSide = 1024
	// MaxRoomCells is the largest Width x Height product of one room's grid.
	MaxRoomCells = 65536
	// MaxRooms is the largest number of rooms in one plane. The reference
	// metroidvania's randomiser graph holds 368 scenes, so 512 is headroom
	// over a full-size map rather than an aspiration.
	MaxRooms = 512
	// MaxTransitionsPerSide is the largest number of openings on one side of
	// one room. More than one opening per side is normal; eight is a ceiling.
	MaxTransitionsPerSide = 8
	// MaxMotionNodes is the largest number of nodes in one jump graph.
	MaxMotionNodes = 65536
	// MaxMotionEdges is the largest number of edges in one jump graph. A
	// refined graph is dense in the pathological case; this is the ceiling at
	// which the builder stops and reports VerdictUnknown rather than growing.
	MaxMotionEdges = 524288
	// MaxBeatsPerRoom is the largest number of beats one room may be built
	// from.
	MaxBeatsPerRoom = 64
	// MaxTerrainKinds is the number of palette entries addressable by one
	// non-zero index byte.
	MaxTerrainKinds = 255
)

// ProfileVersionM1 is the version string of the M1 movement profile: the full
// first moveset, with dash, double jump, wall jump, coyote time and a variable
// jump. A JumpGraph names the version it was built for, and a graph built for
// one version says nothing about another.
const ProfileVersionM1 = "m1"

// Model names identify the movement model that produced a Judgement. A
// consumer that treats a certificate as binding checks the name.
const (
	// ModelM1 is the analytic phase-machine model for the M1 profile.
	ModelM1 = "daedalus/platform/m1"
	// ModelFake is FakeOracle's crude envelope model. It ignores collision
	// entirely and is therefore NOT sound. Nothing it certifies may reach a
	// player-facing guarantee.
	ModelFake = "daedalus/platform/fake"
)

// UnlimitedStamina is the sentinel for a wall cling that never ends and for a
// fall that never damages. It is positive infinity rather than zero because
// zero is a meaningful value for both: zero stamina is a wall that cannot be
// held at all.
var UnlimitedStamina = math.Inf(1)

// DoubleJumpMode says what a mid-air jump does to vertical velocity. The zero
// value is DoubleJumpModeUnspecified and is rejected by validation: a boolean
// would not be enough, and a default would quietly pick one of two genuinely
// different manoeuvres.
//
// The difference is not cosmetic. A reset makes the second jump's height
// independent of when it is taken; an impulse makes a late second jump, taken
// while already falling fast, gain almost nothing. Routes that exist under one
// do not exist under the other.
type DoubleJumpMode int

const (
	// DoubleJumpModeUnspecified is the invalid zero value.
	DoubleJumpModeUnspecified DoubleJumpMode = iota
	// DoubleJumpModeReset sets vy to Velocity, discarding whatever it was.
	DoubleJumpModeReset
	// DoubleJumpModeImpulse adds Velocity to vy at the moment of the jump, so
	// the result depends on vy(tau-).
	DoubleJumpModeImpulse
)

// String returns the mode's lowercase name.
func (m DoubleJumpMode) String() string {
	switch m {
	case DoubleJumpModeReset:
		return "reset"
	case DoubleJumpModeImpulse:
		return "impulse"
	}
	return "unspecified"
}

// VariableJumpMode says what releasing the jump button does. The zero value is
// rejected by validation for the same reason DoubleJumpMode's is.
type VariableJumpMode int

const (
	// VariableJumpModeUnspecified is the invalid zero value.
	VariableJumpModeUnspecified VariableJumpMode = iota
	// VariableJumpModeNone means the jump is not variable: the button's
	// release has no effect and every jump reaches the full apex.
	VariableJumpModeNone
	// VariableJumpModeCutVelocity multiplies a rising vy by CutFactor at the
	// moment of release. CutFactor zero stops the rise outright.
	VariableJumpModeCutVelocity
	// VariableJumpModeSwapGravity leaves vy alone and raises the gravity for
	// the rest of the rise to ReleaseGravity.
	VariableJumpModeSwapGravity
)

// String returns the mode's lowercase name.
func (m VariableJumpMode) String() string {
	switch m {
	case VariableJumpModeNone:
		return "none"
	case VariableJumpModeCutVelocity:
		return "cut-velocity"
	case VariableJumpModeSwapGravity:
		return "swap-gravity"
	}
	return "unspecified"
}

// DashExitMode says which velocity survives a dash burst. "The dash ends" is
// not a specification: what the character is doing in the frame after it is
// what decides whether the dash chains into a jump.
type DashExitMode int

const (
	// DashExitModeUnspecified is the invalid zero value.
	DashExitModeUnspecified DashExitMode = iota
	// DashExitModeKeep leaves the dash velocity in place and lets gravity and
	// the air acceleration take it from there.
	DashExitModeKeep
	// DashExitModeZero drops the velocity to zero on both axes.
	DashExitModeZero
	// DashExitModeClampToRun keeps the dash's direction and clamps the
	// magnitude to MaxRunSpeed.
	DashExitModeClampToRun
)

// String returns the mode's lowercase name.
func (m DashExitMode) String() string {
	switch m {
	case DashExitModeKeep:
		return "keep"
	case DashExitModeZero:
		return "zero"
	case DashExitModeClampToRun:
		return "clamp-to-run"
	}
	return "unspecified"
}

// RefillCondition is a bitset of the events that restore a consumable
// resource. The zero value restores nothing, which is a legal and very
// restrictive profile.
type RefillCondition uint8

const (
	// RefillOnGround restores on contact with a supporting surface.
	RefillOnGround RefillCondition = 1 << iota
	// RefillOnWallCling restores on attaching to a wall.
	RefillOnWallCling
	// RefillOnWallJump restores on leaving a wall.
	RefillOnWallJump
	// RefillOnClimb restores on attaching to a ladder or rope.
	RefillOnClimb
	// RefillOnCooldown restores after the declared cooldown elapses, with no
	// contact at all.
	RefillOnCooldown
)

// Has reports whether the set contains the condition.
func (c RefillCondition) Has(other RefillCondition) bool { return c&other == other }

// DashDirection is a bitset of the directions a dash may be aimed in.
type DashDirection uint8

const (
	// DashDirectionHorizontal allows a purely left or right dash.
	DashDirectionHorizontal DashDirection = 1 << iota
	// DashDirectionUp allows a purely upward dash.
	DashDirectionUp
	// DashDirectionDown allows a purely downward dash.
	DashDirectionDown
	// DashDirectionDiagonal allows the four diagonals.
	DashDirectionDiagonal
)

// Has reports whether the set contains the direction.
func (d DashDirection) Has(other DashDirection) bool { return d&other == other }

// DashCancel is a bitset of the events that end a dash before its duration.
type DashCancel uint8

const (
	// DashCancelByJump ends the dash when the jump button is pressed.
	DashCancelByJump DashCancel = 1 << iota
	// DashCancelByWallContact ends the dash on touching a wall.
	DashCancelByWallContact
	// DashCancelByDamage ends the dash on taking damage.
	DashCancelByDamage
	// DashCancelByDash ends the dash when a new dash is started.
	DashCancelByDash
)

// Has reports whether the set contains the cancellation.
func (c DashCancel) Has(other DashCancel) bool { return c&other == other }

// CoyoteSource is a bitset of the departures that open a coyote window.
type CoyoteSource uint8

const (
	// CoyoteFromLedge opens the window after walking off a floor.
	CoyoteFromLedge CoyoteSource = 1 << iota
	// CoyoteFromWallRelease opens it after letting go of a wall.
	CoyoteFromWallRelease
	// CoyoteFromDropThrough opens it after dropping through a semi-solid.
	CoyoteFromDropThrough
)

// Has reports whether the set contains the source.
func (c CoyoteSource) Has(other CoyoteSource) bool { return c&other == other }

// DoubleJumpProfile parameterises mid-air jumps.
type DoubleJumpProfile struct {
	// Mode says whether the jump resets or adds to vertical velocity.
	// Unspecified is rejected.
	Mode DoubleJumpMode
	// Velocity is the reset target or the added impulse, in cells per second,
	// and is positive.
	Velocity float64
	// Charges is the number of mid-air jumps available between refills, and is
	// at least one.
	Charges uint8
	// RefillOn names the events that restore the charges.
	RefillOn RefillCondition
	// MinDelay is the shortest time, in seconds, between the takeoff and the
	// mid-air jump. It is a floor on the planning variable tau, not its value.
	MinDelay float64
}

// DashProfile parameterises the dash burst. "Extra reach" without a charge
// state accepts false routes, which is why every one of these fields exists.
type DashProfile struct {
	// Speed is the burst's speed, in cells per second.
	Speed float64
	// ShadowSpeed is the burst's speed once AbilityShadowDash is held. Zero
	// means the upgrade does not change the speed.
	ShadowSpeed float64
	// ShadowPassesHazard reports whether the upgraded burst crosses hazards
	// without damage.
	ShadowPassesHazard bool
	// Duration is the burst's length in seconds.
	Duration float64
	// SuspendsGravity reports whether gravity is off during the burst. When it
	// is, the burst is a straight segment and not a parabola.
	SuspendsGravity bool
	// Exit says which velocity survives the burst.
	Exit DashExitMode
	// Charges is the number of bursts available between refills, at least one.
	Charges uint8
	// Cooldown is the time, in seconds, before another burst is accepted.
	Cooldown float64
	// RefillOn names the events that restore the charges.
	RefillOn RefillCondition
	// Directions names the aims the burst accepts.
	Directions DashDirection
	// Cancel names the events that end the burst early.
	Cancel DashCancel
}

// WallJumpProfile parameterises clinging to a wall and leaving it. A boolean
// "the character can wall jump" is not enough to decide a single route.
type WallJumpProfile struct {
	// ImpulseX is the horizontal speed away from the wall, in cells per
	// second, and is positive; the sign comes from the wall's normal.
	ImpulseX float64
	// ImpulseY is the vertical speed, in cells per second, and is positive.
	ImpulseY float64
	// SameWallReuse reports whether the same wall may be used twice in a row.
	// When it is false, a shaft needs two facing walls.
	SameWallReuse bool
	// MaxConsecutive caps the wall jumps between two ground contacts. Zero
	// means no cap.
	MaxConsecutive uint8
	// Stamina is how long, in seconds, a cling can be held.
	// UnlimitedStamina means it never ends.
	Stamina float64
	// SlideSpeed is the descent speed while clinging, in cells per second, and
	// is not negative. Zero is a cling that does not slide.
	SlideSpeed float64
	// InputLock is how long, in seconds, horizontal input is ignored after the
	// jump. It is why a wall jump cannot be steered back into its own wall.
	InputLock float64
	// MinWallHeight is the vertical extent, in cells, a face must have before
	// it can be grabbed.
	MinWallHeight float64
}

// CoyoteProfile parameterises the window during which the ground jump is still
// accepted after leaving a surface.
//
// The window is NOT extra reach. A jump taken at tau into it starts
// GravityUp*tau*tau/2 lower and GainX = MaxRunSpeed*tau further along. The
// loss is quadratic and the gain is linear, so the trade is a curve and not an
// offset. Adding MaxRunSpeed*Window to a reach envelope would certify jumps a
// player cannot make. tau is a variable of the search; this struct only bounds
// it.
type CoyoteProfile struct {
	// Window is the window's length in seconds.
	Window float64
	// AppliesTo names the departures that open the window.
	AppliesTo CoyoteSource
}

// VariableJumpProfile parameterises a jump whose height depends on how long
// the button is held.
type VariableJumpProfile struct {
	// Mode says what the release does. Unspecified is rejected.
	Mode VariableJumpMode
	// CutFactor multiplies a rising vy on release, under
	// VariableJumpModeCutVelocity. It is in [0, 1].
	CutFactor float64
	// ReleaseGravity is the gravity applied for the rest of the rise under
	// VariableJumpModeSwapGravity, in cells per second squared, and is at
	// least GravityUp.
	ReleaseGravity float64
	// MinHoldTime is how long, in seconds, the jump is committed for before a
	// release has any effect.
	MinHoldTime float64
}

// ClimbProfile parameterises ladders and static ropes. A swinging rope is a
// dynamic body and is not covered.
type ClimbProfile struct {
	// Speed is the vertical speed on the ladder, in cells per second.
	Speed float64
	// CaptureHalfWidth is how far, in cells, the foot centre may be from the
	// ladder's axis and still attach.
	CaptureHalfWidth float64
	// CanJumpOff reports whether the ground jump is available while climbing.
	CanJumpOff bool
	// ExitImpulseX is the horizontal speed given by jumping off sideways, in
	// cells per second.
	ExitImpulseX float64
}

// MovementProfile is the complete declaration of what the character can do.
// It is the thing a Verdict is relative to: a certificate names the profile's
// version, and a graph built for one profile says nothing about another.
//
// Units are declared once and are not negotiable inside this package. Space is
// CELLS and time is SECONDS, in the world frame described in the package
// documentation: x grows right, y grows up, and the reference point is the
// centre of the feet. CellSize converts a cell to the caller's own unit and is
// the only place that conversion exists. In the dungeon generator CellSize is
// copied to the output and no algorithm reads it; here it carries physical
// meaning, because a profile expressed in cells per second is a different
// character on a grid whose cells are twice as large.
type MovementProfile struct {
	// Version identifies the profile's shape, such as ProfileVersionM1.
	Version string

	// CellSize is one cell's size in the caller's units: finite and positive.
	// Every other length in this struct is in cells, not in those units.
	CellSize float64
	// ControlRate is the fixed step, in hertz, that witness commands are
	// expressed against. Zero means the witness is continuous and a consumer
	// must discretise it itself.
	ControlRate float64

	// GravityUp is the downward acceleration while rising, in cells per second
	// squared, and is positive.
	GravityUp float64
	// GravityDown is the downward acceleration while falling, in cells per
	// second squared, and is positive. Keeping it separate from GravityUp is
	// what makes a jump feel snappy; a model with one gravity cannot express
	// it.
	GravityDown float64
	// JumpVelocity is the upward speed the ground jump sets, in cells per
	// second, and is positive. The jump REPLACES vertical velocity.
	JumpVelocity float64
	// MaxRunSpeed is the largest horizontal speed, in cells per second.
	MaxRunSpeed float64
	// GroundAccel is the horizontal acceleration on a surface, in cells per
	// second squared.
	GroundAccel float64
	// AirAccel is the horizontal acceleration in flight, in cells per second
	// squared. It is not interchangeable with GroundAccel.
	AirAccel float64
	// Braking is the horizontal deceleration when the input opposes the
	// motion, in cells per second squared. The stopping distance is
	// v*v/(2*Braking), and landing near an edge at speed is not the same as
	// being able to stop there.
	Braking float64

	// BodyHalfWidth is r: half the body's width, in cells, and is positive.
	BodyHalfWidth float64
	// BodyHeight is h: the body's height above the feet, in cells, and is
	// positive.
	BodyHeight float64
	// Margin is epsilon: the conservative clearance applied to footing and to
	// obstacle expansion, in cells. It is a declared safety policy, not a
	// numerical fudge factor.
	Margin float64

	// MaxFallSpeed is the terminal downward speed, in cells per second.
	// UnlimitedStamina means there is none, and the fall stays parabolic.
	MaxFallSpeed float64
	// MaxSafeFallHeight is the largest drop, in cells, that does not damage
	// the character. UnlimitedStamina means falling never damages. It is a
	// damage rule and is NOT the search's fall horizon, which lives in
	// SearchBudget.FallHorizon: one is about the game, the other about how far
	// down the builder bothers to look.
	MaxSafeFallHeight float64

	// DoubleJump parameterises mid-air jumps, or is nil when the profile has
	// none.
	DoubleJump *DoubleJumpProfile
	// Dash parameterises the dash, or is nil when the profile has none.
	Dash *DashProfile
	// WallJump parameterises wall cling and wall jump, or is nil.
	WallJump *WallJumpProfile
	// Coyote parameterises the late-jump window, or is nil for none.
	Coyote *CoyoteProfile
	// VariableJump parameterises the held jump, or is nil for none.
	VariableJump *VariableJumpProfile
	// Climb parameterises ladders and ropes, or is nil for none.
	Climb *ClimbProfile
}

// DefaultProfile returns this project's declared M1 profile.
//
// It is NOT a calibration of any shipped game. The reference metroidvania's
// physical constants are not published — jump height, time to apex, whether
// rise and fall use different gravities, dash duration, wall-jump impulse,
// whether the double jump resets or adds, and the pixels-per-unit that would
// let any of its figures become a cell. Three things below are inherited
// honestly, because they survive the missing scale:
//
//   - the dash is 2.41x the run speed, from the published 20.0 and 8.3 engine
//     units per second;
//   - the shadow upgrade is 1.4x the dash, from the published 28.0;
//   - the dash cooldown is 0.6 s, which is a time and needs no scale.
//
// Everything else is a choice made here and marked as such. The geometry is
// anchored on an apex of 4 cells reached in 0.4 s, which gives GravityUp 50
// and JumpVelocity 20 exactly, and a run of 8 cells per second, which gives a
// same-height jump of 0.8 s covering 6.4 cells. Those four numbers are the
// worked example the design notes verify numerically, so a reader can check
// the profile against arithmetic rather than against a claim.
func DefaultProfile() MovementProfile {
	const (
		apex        = 4.0  // cells; project choice
		timeToApex  = 0.4  // seconds; project choice
		run         = 8.0  // cells per second; project choice
		dashRatio   = 2.41 // published: 20.0 / 8.3 engine units per second
		shadowRatio = 1.4  // published: 28.0 / 20.0 engine units per second
	)
	gravityUp := 2 * apex / (timeToApex * timeToApex)
	jump := 2 * apex / timeToApex
	dashSpeed := run * dashRatio

	return MovementProfile{
		Version:     ProfileVersionM1,
		CellSize:    1.0,
		ControlRate: 60,

		GravityUp:    gravityUp,
		GravityDown:  gravityUp * 1.25, // project choice: the fall is snappier than the rise
		JumpVelocity: jump,
		MaxRunSpeed:  run,
		GroundAccel:  80,  // project choice: full run speed in 0.1 s
		AirAccel:     40,  // project choice: half the ground figure
		Braking:      120, // project choice: stops from full speed in 0.267 cells

		BodyHalfWidth: 0.35, // project choice
		BodyHeight:    1.6,  // project choice
		Margin:        0.05, // project choice

		MaxFallSpeed:      20.0,             // project choice; the community-sourced ratio 2.518 would give 20.14
		MaxSafeFallHeight: UnlimitedStamina, // project choice: the reference game has no fall damage

		DoubleJump: &DoubleJumpProfile{
			// Reset rather than impulse is a project choice: the reference
			// game does not publish which one it uses.
			Mode:     DoubleJumpModeReset,
			Velocity: jump,
			Charges:  1,
			RefillOn: RefillOnGround | RefillOnWallCling | RefillOnWallJump | RefillOnClimb,
			MinDelay: 0.05,
		},
		Dash: &DashProfile{
			Speed:              dashSpeed,
			ShadowSpeed:        dashSpeed * shadowRatio,
			ShadowPassesHazard: true,
			Duration:           0.2, // project choice; the reference game's DASH_TIME is not published
			SuspendsGravity:    true,
			Exit:               DashExitModeClampToRun,
			Charges:            1,
			Cooldown:           0.6, // published
			RefillOn:           RefillOnGround | RefillOnWallCling | RefillOnWallJump | RefillOnCooldown,
			Directions:         DashDirectionHorizontal,
			Cancel:             DashCancelByJump | DashCancelByWallContact | DashCancelByDamage,
		},
		WallJump: &WallJumpProfile{
			ImpulseX:       run,         // project choice
			ImpulseY:       jump * 0.85, // project choice
			SameWallReuse:  true,        // the reference game allows it indefinitely
			MaxConsecutive: 0,
			Stamina:        UnlimitedStamina,
			SlideSpeed:     4.0,  // project choice
			InputLock:      0.10, // project choice
			MinWallHeight:  2.0,  // project choice
		},
		Coyote: &CoyoteProfile{
			// Project choice. A community report puts the reference game's
			// window at two physics frames of a 50 Hz step, which would be
			// 0.04 s, but neither half of that was confirmed in code.
			Window:    0.08,
			AppliesTo: CoyoteFromLedge | CoyoteFromWallRelease,
		},
		VariableJump: &VariableJumpProfile{
			// Project choice: the reference game has a jump-release queue but
			// does not publish what the release does.
			Mode:        VariableJumpModeCutVelocity,
			CutFactor:   0,
			MinHoldTime: 0.08,
		},
		Climb: &ClimbProfile{
			Speed:            4.0,
			CaptureHalfWidth: 0.4,
			CanJumpOff:       true,
			ExitImpulseX:     run * 0.5,
		},
	}
}

// ApexHeight returns H: the height, in cells, a full jump gains over its
// takeoff, which is JumpVelocity squared over twice GravityUp.
func (p MovementProfile) ApexHeight() float64 {
	return p.JumpVelocity * p.JumpVelocity / (2 * p.GravityUp)
}

// TimeToApex returns Ta: the time, in seconds, from takeoff to the apex.
func (p MovementProfile) TimeToApex() float64 { return p.JumpVelocity / p.GravityUp }

// SameHeightAirtime returns the time, in seconds, a full jump spends in the
// air before returning to its takeoff height. It uses GravityUp for the rise
// and GravityDown for the fall, so it is Ta + sqrt(2H/GravityDown) and equals
// 2*Ta only when the two gravities agree.
func (p MovementProfile) SameHeightAirtime() float64 {
	return p.TimeToApex() + math.Sqrt(2*p.ApexHeight()/p.GravityDown)
}

// SameHeightRange returns the horizontal distance, in cells, a full jump at
// MaxRunSpeed covers before returning to its takeoff height. It is the
// envelope's widest same-level reach and must NOT be reused for a destination
// at another height: a lower platform lengthens the flight and a higher one
// shortens it.
func (p MovementProfile) SameHeightRange() float64 {
	return p.MaxRunSpeed * p.SameHeightAirtime()
}

// StoppingDistance returns the distance, in cells, needed to brake from speed
// to rest. Landing on a narrow platform at speed is not the same as being able
// to stay on it.
func (p MovementProfile) StoppingDistance(speed float64) float64 {
	if p.Braking <= 0 {
		return math.Inf(1)
	}
	return speed * speed / (2 * p.Braking)
}

// CoyoteHeightLoss returns the apex height, in cells, given up by jumping tau
// seconds after leaving the surface: GravityUp*tau*tau/2. It is quadratic.
func (p MovementProfile) CoyoteHeightLoss(tau float64) float64 {
	return p.GravityUp * tau * tau / 2
}

// CoyoteHorizontalGain returns the distance, in cells, gained by jumping tau
// seconds after leaving the surface at full run speed: MaxRunSpeed*tau. It is
// linear, which is exactly why the trade cannot be folded into an envelope
// offset.
func (p MovementProfile) CoyoteHorizontalGain(tau float64) float64 {
	return p.MaxRunSpeed * tau
}

// Footing returns the span of legal foot positions on a surface whose top edge
// spans [a, b], under the conservative full-support policy:
// [a+r+margin, b-r-margin]. An empty result means the edge is too narrow to
// stand on at all under that policy; an engine that lets a foot hang over the
// edge has a different policy and a different number.
func (p MovementProfile) Footing(edge Span) Span {
	inset := p.BodyHalfWidth + p.Margin
	return Span{Lo: edge.Lo + inset, Hi: edge.Hi - inset}
}

// FullResources returns the fresh consumable state for the profile: every
// charge available, no cooldown pending, no wall used. The zero Resources is
// the exhausted state, so this is the only correct way to spell "rested".
func (p MovementProfile) FullResources() Resources {
	full := Resources{LastWall: WallSideNone, ClingRemaining: 0}
	if p.DoubleJump != nil {
		full.AirJumps = p.DoubleJump.Charges
	}
	if p.Dash != nil {
		full.DashCharges = p.Dash.Charges
	}
	if p.WallJump != nil {
		full.ClingRemaining = p.WallJump.Stamina
	}
	return full
}

// Abilities returns the set of abilities the profile actually parameterises. A
// Config whose progression grants an ability the profile does not describe is
// rejected, because a graph cannot carry an edge it has no mechanic for.
func (p MovementProfile) Abilities() AbilitySet {
	var set AbilitySet
	if p.Dash != nil {
		set = set.With(AbilityDash)
		if p.Dash.ShadowSpeed > 0 || p.Dash.ShadowPassesHazard {
			set = set.With(AbilityShadowDash)
		}
	}
	if p.DoubleJump != nil {
		set = set.With(AbilityDoubleJump)
	}
	if p.WallJump != nil {
		set = set.With(AbilityWallJump)
	}
	if p.Climb != nil {
		set = set.With(AbilityClimb)
	}
	return set
}

// Validate checks that the profile describes a character. Every failure wraps
// ErrInvalidProfile. An unspecified mode is a failure and never a default: a
// double jump that does not say whether it resets or adds is two different
// characters, and picking one silently is how a generator certifies routes the
// consumer's controller cannot perform.
func (p MovementProfile) Validate() error {
	if p.Version == "" {
		return profileError("version is empty")
	}
	for _, field := range []struct {
		name  string
		value float64
	}{
		{"CellSize", p.CellSize},
		{"GravityUp", p.GravityUp},
		{"GravityDown", p.GravityDown},
		{"JumpVelocity", p.JumpVelocity},
		{"MaxRunSpeed", p.MaxRunSpeed},
		{"GroundAccel", p.GroundAccel},
		{"AirAccel", p.AirAccel},
		{"Braking", p.Braking},
		{"BodyHalfWidth", p.BodyHalfWidth},
		{"BodyHeight", p.BodyHeight},
	} {
		if math.IsNaN(field.value) || math.IsInf(field.value, 0) || field.value <= 0 {
			return profileError("%s must be finite and positive, got %v", field.name, field.value)
		}
	}
	if math.IsNaN(p.Margin) || math.IsInf(p.Margin, 0) || p.Margin < 0 {
		return profileError("Margin must be finite and not negative, got %v", p.Margin)
	}
	if math.IsNaN(p.ControlRate) || p.ControlRate < 0 || math.IsInf(p.ControlRate, 0) {
		return profileError("ControlRate must be finite and not negative, got %v", p.ControlRate)
	}
	if math.IsNaN(p.MaxFallSpeed) || p.MaxFallSpeed <= 0 {
		return profileError("MaxFallSpeed must be positive, or UnlimitedStamina, got %v", p.MaxFallSpeed)
	}
	if math.IsNaN(p.MaxSafeFallHeight) || p.MaxSafeFallHeight <= 0 {
		return profileError("MaxSafeFallHeight must be positive, or UnlimitedStamina, got %v", p.MaxSafeFallHeight)
	}
	if err := p.validateDoubleJump(); err != nil {
		return err
	}
	if err := p.validateDash(); err != nil {
		return err
	}
	if err := p.validateWallJump(); err != nil {
		return err
	}
	if err := p.validateCoyote(); err != nil {
		return err
	}
	if err := p.validateVariableJump(); err != nil {
		return err
	}
	return p.validateClimb()
}

func (p MovementProfile) validateDoubleJump() error {
	jump := p.DoubleJump
	if jump == nil {
		return nil
	}
	if jump.Mode == DoubleJumpModeUnspecified {
		return profileError("DoubleJump.Mode must say whether the jump resets or adds to vertical velocity")
	}
	if jump.Mode != DoubleJumpModeReset && jump.Mode != DoubleJumpModeImpulse {
		return profileError("DoubleJump.Mode %d is not a declared mode", int(jump.Mode))
	}
	if !positive(jump.Velocity) {
		return profileError("DoubleJump.Velocity must be finite and positive, got %v", jump.Velocity)
	}
	if jump.Charges == 0 {
		return profileError("DoubleJump.Charges must be at least one; a profile with no mid-air jump leaves DoubleJump nil")
	}
	if !nonNegative(jump.MinDelay) {
		return profileError("DoubleJump.MinDelay must be finite and not negative, got %v", jump.MinDelay)
	}
	return nil
}

func (p MovementProfile) validateDash() error {
	dash := p.Dash
	if dash == nil {
		return nil
	}
	if dash.Exit == DashExitModeUnspecified {
		return profileError("Dash.Exit must say which velocity survives the burst")
	}
	if dash.Exit != DashExitModeKeep && dash.Exit != DashExitModeZero && dash.Exit != DashExitModeClampToRun {
		return profileError("Dash.Exit %d is not a declared mode", int(dash.Exit))
	}
	if !positive(dash.Speed) {
		return profileError("Dash.Speed must be finite and positive, got %v", dash.Speed)
	}
	if dash.ShadowSpeed != 0 && !positive(dash.ShadowSpeed) {
		return profileError("Dash.ShadowSpeed must be zero or finite and positive, got %v", dash.ShadowSpeed)
	}
	if !positive(dash.Duration) {
		return profileError("Dash.Duration must be finite and positive, got %v", dash.Duration)
	}
	if dash.Charges == 0 {
		return profileError("Dash.Charges must be at least one; a profile with no dash leaves Dash nil")
	}
	if !nonNegative(dash.Cooldown) {
		return profileError("Dash.Cooldown must be finite and not negative, got %v", dash.Cooldown)
	}
	if dash.Directions == 0 {
		return profileError("Dash.Directions must allow at least one aim")
	}
	if dash.RefillOn == 0 {
		return profileError("Dash.RefillOn must name at least one refill event; a charge that never returns is spent once per map")
	}
	return nil
}

func (p MovementProfile) validateWallJump() error {
	wall := p.WallJump
	if wall == nil {
		return nil
	}
	if !positive(wall.ImpulseX) {
		return profileError("WallJump.ImpulseX must be finite and positive, got %v", wall.ImpulseX)
	}
	if !positive(wall.ImpulseY) {
		return profileError("WallJump.ImpulseY must be finite and positive, got %v", wall.ImpulseY)
	}
	if math.IsNaN(wall.Stamina) || wall.Stamina <= 0 {
		return profileError("WallJump.Stamina must be positive, or UnlimitedStamina, got %v", wall.Stamina)
	}
	if !nonNegative(wall.SlideSpeed) {
		return profileError("WallJump.SlideSpeed must be finite and not negative, got %v", wall.SlideSpeed)
	}
	if !nonNegative(wall.InputLock) {
		return profileError("WallJump.InputLock must be finite and not negative, got %v", wall.InputLock)
	}
	if !positive(wall.MinWallHeight) {
		return profileError("WallJump.MinWallHeight must be finite and positive, got %v", wall.MinWallHeight)
	}
	if !wall.SameWallReuse && wall.MaxConsecutive == 1 {
		return profileError("WallJump forbids reusing the same wall and caps consecutive jumps at one, which no shaft can satisfy")
	}
	return nil
}

func (p MovementProfile) validateCoyote() error {
	coyote := p.Coyote
	if coyote == nil {
		return nil
	}
	if !positive(coyote.Window) {
		return profileError("Coyote.Window must be finite and positive; a profile with no window leaves Coyote nil")
	}
	if coyote.AppliesTo == 0 {
		return profileError("Coyote.AppliesTo must name at least one departure")
	}
	return nil
}

func (p MovementProfile) validateVariableJump() error {
	variable := p.VariableJump
	if variable == nil {
		return nil
	}
	switch variable.Mode {
	case VariableJumpModeUnspecified:
		return profileError("VariableJump.Mode must say whether releasing the button cuts velocity or swaps gravity")
	case VariableJumpModeNone:
	case VariableJumpModeCutVelocity:
		if math.IsNaN(variable.CutFactor) || variable.CutFactor < 0 || variable.CutFactor > 1 {
			return profileError("VariableJump.CutFactor must be in [0, 1], got %v", variable.CutFactor)
		}
	case VariableJumpModeSwapGravity:
		if !positive(variable.ReleaseGravity) || variable.ReleaseGravity < p.GravityUp {
			return profileError("VariableJump.ReleaseGravity must be finite and at least GravityUp %v, got %v", p.GravityUp, variable.ReleaseGravity)
		}
	default:
		return profileError("VariableJump.Mode %d is not a declared mode", int(variable.Mode))
	}
	if !nonNegative(variable.MinHoldTime) {
		return profileError("VariableJump.MinHoldTime must be finite and not negative, got %v", variable.MinHoldTime)
	}
	return nil
}

func (p MovementProfile) validateClimb() error {
	climb := p.Climb
	if climb == nil {
		return nil
	}
	if !positive(climb.Speed) {
		return profileError("Climb.Speed must be finite and positive, got %v", climb.Speed)
	}
	if !positive(climb.CaptureHalfWidth) {
		return profileError("Climb.CaptureHalfWidth must be finite and positive, got %v", climb.CaptureHalfWidth)
	}
	if !nonNegative(climb.ExitImpulseX) {
		return profileError("Climb.ExitImpulseX must be finite and not negative, got %v", climb.ExitImpulseX)
	}
	return nil
}

func positive(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0
}

func nonNegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

// SearchBudget bounds what the oracle may spend answering one question. A
// budget is part of the contract and not a performance knob: a search that
// stops because it ran out returns VerdictUnknown, so the budget decides which
// questions get an answer.
//
// The ceilings are counts rather than milliseconds on purpose. A wall-clock
// ceiling makes the output depend on the machine, and the output of this
// package has to be reproducible.
type SearchBudget struct {
	// MaxCandidateEdges caps the candidate manoeuvres enumerated. Zero means
	// no ceiling of this kind.
	MaxCandidateEdges uint64
	// MaxExpandedNodes caps the graph nodes expanded. Zero means no ceiling.
	MaxExpandedNodes uint64
	// MaxCollisionTests caps the continuous collision tests. Zero means no
	// ceiling.
	MaxCollisionTests uint64
	// FallHorizon is how far below a takeoff, in cells, a destination is still
	// considered. Without it the candidate set of any tall room is unbounded
	// downward. It is a search parameter and is NOT the profile's fall-damage
	// rule.
	FallHorizon float64
	// LaunchResolution is the spacing, in cells, between sampled launch
	// positions along an interval. A coarser value is faster and misses more,
	// and what it misses becomes VerdictUnknown rather than VerdictRejected.
	LaunchResolution float64
	// TimeResolution is the spacing, in seconds, between sampled manoeuvre
	// events such as the mid-air jump's tau and the coyote window's tau. Same
	// trade.
	TimeResolution float64
}

// DefaultSearchBudget returns the budget a request gets when it declares none.
// The figures are this project's choice and are sized so that one room of the
// default profile is answerable, not so that a whole plane is.
func DefaultSearchBudget() SearchBudget {
	return SearchBudget{
		MaxCandidateEdges: 1 << 20,
		MaxExpandedNodes:  1 << 18,
		MaxCollisionTests: 1 << 22,
		FallHorizon:       64,
		LaunchResolution:  0.25,
		TimeResolution:    0.02,
	}
}

// Validate checks the budget's numbers. A budget whose every ceiling is zero
// is rejected: an unbounded search has no VerdictUnknown to give and will not
// terminate on a pathological room.
func (b SearchBudget) Validate() error {
	if b.MaxCandidateEdges == 0 && b.MaxExpandedNodes == 0 && b.MaxCollisionTests == 0 {
		return configError("SearchBudget declares no ceiling at all")
	}
	if !positive(b.FallHorizon) {
		return configError("SearchBudget.FallHorizon must be finite and positive, got %v", b.FallHorizon)
	}
	if !positive(b.LaunchResolution) {
		return configError("SearchBudget.LaunchResolution must be finite and positive, got %v", b.LaunchResolution)
	}
	if !positive(b.TimeResolution) {
		return configError("SearchBudget.TimeResolution must be finite and positive, got %v", b.TimeResolution)
	}
	return nil
}

// BeatDefinition declares the geometry budget and the demand of one beat kind.
// It is the caller's parameterisation of a kind Daedalus already knows: the
// vocabulary is closed and the parameters are not.
//
// The shape deliberately mirrors the dungeon generator's terrain vocabulary —
// a list of definitions plus per-context weighted distributions — so that a
// caller who has configured terrain already knows how to configure beats.
type BeatDefinition struct {
	// Kind is the beat this entry parameterises. It must be a declared kind
	// and must not repeat within one BeatConfig.
	Kind BeatKind
	// MinCells and MaxCells bound the beat's horizontal extent, in cells.
	// Both zero requests the generator's canonical span for the kind;
	// declaring only one bound is invalid.
	MinCells uint32
	MaxCells uint32
	// Difficulty is the caller's demand rating for the kind, 0..255. Daedalus
	// uses it only to order and to pace; it is not a claim about humans.
	Difficulty uint8
	// Requires is the AbilitySet a beat of this kind is built to demand. For
	// BeatKindGate it is the gate's key; for every other kind it is the floor
	// below which the beat is not emitted.
	Requires AbilitySet
}

// BeatWeight associates a positive selection weight with a declared beat kind.
type BeatWeight struct {
	// Kind is the beat kind, which must have a BeatDefinition.
	Kind BeatKind
	// Weight is the positive relative selection weight.
	Weight uint32
}

// BeatDistribution is the weighted vocabulary available in one context, plus
// the run length of a stretch built from it.
//
// Unlike the terrain distribution it mirrors, there is no "none" weight.
// Terrain has a real absence — a cell with no terrain — while rhythm does not:
// the absence of demand is BeatKindRest, which is a declared kind with its own
// weight. Giving rest a weight rather than an absence is what lets a caller
// ask for more breathing room instead of less of everything.
type BeatDistribution struct {
	// Beats lists positive weights by kind, with no duplicate kinds, and is
	// non-empty.
	Beats []BeatWeight
	// MinRunBeats and MaxRunBeats bound the number of beats in one stretch.
	// Both zero requests the generator's canonical span; declaring only one
	// bound is invalid.
	MinRunBeats uint32
	MaxRunBeats uint32
}

// BeatConfig is the rhythm vocabulary and the per-context distributions.
type BeatConfig struct {
	// Definitions parameterises each kind the map may use, with no duplicate
	// kinds.
	Definitions []BeatDefinition
	// Spine is the distribution for the main progression path. It is
	// required.
	Spine *BeatDistribution
	// Branches is the distribution for optional side paths, or nil to reuse
	// Spine.
	Branches *BeatDistribution
}

// Definition returns the definition for a kind and whether it was declared.
func (c BeatConfig) Definition(kind BeatKind) (BeatDefinition, bool) {
	for _, definition := range c.Definitions {
		if definition.Kind == kind {
			return definition, true
		}
	}
	return BeatDefinition{}, false
}

// Validate checks the beat vocabulary. Every failure wraps ErrInvalidBeats.
func (c BeatConfig) Validate() error {
	if len(c.Definitions) == 0 {
		return beatsError("no beat definitions")
	}
	seen := make(map[BeatKind]bool, len(c.Definitions))
	for index, definition := range c.Definitions {
		if !definition.Kind.IsKnown() {
			return beatsError("definition %d names %s, which is not a declared beat kind", index, definition.Kind)
		}
		if seen[definition.Kind] {
			return beatsError("beat kind %s is defined twice", definition.Kind)
		}
		seen[definition.Kind] = true
		if (definition.MinCells == 0) != (definition.MaxCells == 0) {
			return beatsError("beat kind %s declares only one of MinCells and MaxCells", definition.Kind)
		}
		if definition.MinCells > definition.MaxCells {
			return beatsError("beat kind %s has MinCells %d above MaxCells %d", definition.Kind, definition.MinCells, definition.MaxCells)
		}
	}
	if c.Spine == nil {
		return beatsError("the spine distribution is required")
	}
	if err := c.Spine.validate("spine", seen); err != nil {
		return err
	}
	if c.Branches == nil {
		return nil
	}
	return c.Branches.validate("branches", seen)
}

func (d *BeatDistribution) validate(context string, defined map[BeatKind]bool) error {
	if len(d.Beats) == 0 {
		return beatsError("the %s distribution is empty", context)
	}
	seen := make(map[BeatKind]bool, len(d.Beats))
	for index, weight := range d.Beats {
		if !weight.Kind.IsKnown() {
			return beatsError("%s weight %d names %s, which is not a declared beat kind", context, index, weight.Kind)
		}
		if !defined[weight.Kind] {
			return beatsError("%s weight %d names %s, which has no definition", context, index, weight.Kind)
		}
		if seen[weight.Kind] {
			return beatsError("%s distribution weights %s twice", context, weight.Kind)
		}
		seen[weight.Kind] = true
		if weight.Weight == 0 {
			return beatsError("%s weight for %s is zero; omit the kind instead", context, weight.Kind)
		}
	}
	if (d.MinRunBeats == 0) != (d.MaxRunBeats == 0) {
		return beatsError("the %s distribution declares only one of MinRunBeats and MaxRunBeats", context)
	}
	if d.MinRunBeats > d.MaxRunBeats {
		return beatsError("the %s distribution has MinRunBeats %d above MaxRunBeats %d", context, d.MinRunBeats, d.MaxRunBeats)
	}
	if d.MaxRunBeats > MaxBeatsPerRoom {
		return limitError("the %s distribution asks for %d beats per room, above the ceiling of %d", context, d.MaxRunBeats, MaxBeatsPerRoom)
	}
	return nil
}

// Config is the complete input to a platform-generation request. Like the
// dungeon Config it exists only as a Go value or as a protobuf message; there
// is no configuration file and no preset.
//
// Seed and the plane dimensions are required and have no safe default. The
// zero Config is not valid.
type Config struct {
	// Seed is the source of the request's random streams. Required; every
	// uint64 is accepted and zero does not mean random.
	Seed Seed
	// Width is the plane's width in cells, and is at least one.
	Width uint32
	// Height is the plane's height in cells, and is at least one.
	Height uint32
	// MaxRooms is the largest number of rooms the plane may hold: 1..MaxRooms.
	// Zero requests the product ceiling.
	MaxRooms uint32
	// Profile is the movement profile every verdict is relative to. Required.
	Profile MovementProfile
	// Progression declares which abilities exist and in which order they may
	// be obtained. The zero value is a map with no gating at all.
	Progression ProgressionPlan
	// Beats is the rhythm vocabulary and its distributions. Required.
	Beats BeatConfig
	// Budget bounds the oracle's searches. The zero value requests
	// DefaultSearchBudget.
	Budget SearchBudget
	// Terrain is the optional terrain vocabulary, or nil when the map carries
	// no overlay. Entries must have unique, non-empty IDs.
	Terrain []TerrainDefinition
	// Discipline selects the jump graph's node model. The zero value requests
	// NodeDisciplineRefined, which is the only one that is correct without a
	// proof about every node.
	Discipline NodeDiscipline
	// OmitWitness drops the Witness from every certified edge. The field is
	// negative on purpose: the zero Config asks for witnesses, because a
	// certificate nothing else can recheck is not evidence. A caller sets it
	// only when it has measured the memory and decided it would rather have a
	// graph it cannot audit.
	OmitWitness bool
}

// Normalize returns the effective Config: the one validation sees and the one
// a golden fixture is taken of. Defaults are applied here and nowhere else, so
// that "the zero value means X" is a single readable function rather than a
// property scattered over the generator.
func (c Config) Normalize() Config {
	effective := c
	if effective.MaxRooms == 0 {
		effective.MaxRooms = MaxRooms
	}
	if effective.Budget == (SearchBudget{}) {
		effective.Budget = DefaultSearchBudget()
	}
	if effective.Discipline == NodeDisciplineUnspecified {
		effective.Discipline = NodeDisciplineRefined
	}
	return effective
}

// Validate checks a Config after Normalize. Failures wrap ErrInvalidConfig,
// ErrInvalidProfile, ErrInvalidProgression, ErrInvalidBeats or
// ErrLimitExceeded, whichever names the part at fault.
func (c Config) Validate() error {
	effective := c.Normalize()
	if effective.Width == 0 || effective.Height == 0 {
		return configError("plane dimensions %dx%d include a zero", effective.Width, effective.Height)
	}
	if effective.MaxRooms > MaxRooms {
		return limitError("MaxRooms %d is above the ceiling of %d", effective.MaxRooms, MaxRooms)
	}
	if err := effective.Profile.Validate(); err != nil {
		return err
	}
	if err := effective.Progression.Validate(effective.Profile); err != nil {
		return err
	}
	if err := effective.Beats.Validate(); err != nil {
		return err
	}
	if err := effective.Budget.Validate(); err != nil {
		return err
	}
	return validateTerrainDefinitions(effective.Terrain)
}

func validateTerrainDefinitions(definitions []TerrainDefinition) error {
	if len(definitions) > MaxTerrainKinds {
		return limitError("%d terrain definitions, above the ceiling of %d", len(definitions), MaxTerrainKinds)
	}
	seen := make(map[TerrainID]bool, len(definitions))
	for index, definition := range definitions {
		if definition.ID == "" {
			return configError("terrain definition %d has an empty ID", index)
		}
		if seen[definition.ID] {
			return configError("terrain ID %q is defined twice", definition.ID)
		}
		seen[definition.ID] = true
	}
	return nil
}

// Validate checks the plan against the profile that must implement it. Every
// failure wraps ErrInvalidProgression. A plan may not grant an ability the
// profile does not parameterise: an edge needs a mechanic, and a grant without
// one is a gate that never opens.
func (p ProgressionPlan) Validate(profile MovementProfile) error {
	available := profile.Abilities()
	if !available.Contains(p.Base) {
		return progressionError("the base moveset %s includes an ability the profile does not parameterise", p.Base)
	}
	held := p.Base
	for index, step := range p.Steps {
		if step.Grants == 0 {
			return progressionError("step %d grants nothing", index)
		}
		if !available.Contains(step.Grants) {
			return progressionError("step %d grants %s, which the profile does not parameterise", index, step.Grants)
		}
		if held.Contains(step.Grants) {
			return progressionError("step %d re-grants %s, which is already held", index, step.Grants)
		}
		held = held.Union(step.Grants)
	}
	return nil
}
