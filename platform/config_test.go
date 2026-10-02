package platform

import (
	"errors"
	"math"
	"testing"
)

const tolerance = 1e-12

func closeTo(got, want float64) bool { return math.Abs(got-want) <= tolerance }

func TestDefaultProfileValidates(t *testing.T) {
	profile := DefaultProfile()
	if err := profile.Validate(); err != nil {
		t.Fatalf("the shipped default profile does not validate: %v", err)
	}
	if profile.Version != ProfileVersionM1 {
		t.Fatalf("Version = %q, want %q", profile.Version, ProfileVersionM1)
	}
	for _, part := range []struct {
		name    string
		present bool
	}{
		{"DoubleJump", profile.DoubleJump != nil},
		{"Dash", profile.Dash != nil},
		{"WallJump", profile.WallJump != nil},
		{"Coyote", profile.Coyote != nil},
		{"VariableJump", profile.VariableJump != nil},
		{"Climb", profile.Climb != nil},
	} {
		if !part.present {
			t.Errorf("the M1 default is missing %s; M1 is the full moveset", part.name)
		}
	}
}

// TestDefaultProfileAnchorNumbers locks the four numbers the design notes
// verified numerically: an apex of 4 cells in 0.4 s gives gravity 50 and a
// jump velocity of 20, and at 8 cells per second a same-height jump lasts
// 0.8 s and covers 6.4 cells. The last two are checked on a single-gravity
// variant, because the shipped default deliberately falls faster than it
// rises and that changes the airtime.
func TestDefaultProfileAnchorNumbers(t *testing.T) {
	profile := DefaultProfile()
	if !closeTo(profile.GravityUp, 50) {
		t.Fatalf("GravityUp = %v, want 50", profile.GravityUp)
	}
	if !closeTo(profile.JumpVelocity, 20) {
		t.Fatalf("JumpVelocity = %v, want 20", profile.JumpVelocity)
	}
	if !closeTo(profile.MaxRunSpeed, 8) {
		t.Fatalf("MaxRunSpeed = %v, want 8", profile.MaxRunSpeed)
	}
	if !closeTo(profile.ApexHeight(), 4) {
		t.Fatalf("ApexHeight() = %v, want 4", profile.ApexHeight())
	}
	if !closeTo(profile.TimeToApex(), 0.4) {
		t.Fatalf("TimeToApex() = %v, want 0.4", profile.TimeToApex())
	}

	symmetric := profile
	symmetric.GravityDown = symmetric.GravityUp
	if !closeTo(symmetric.SameHeightAirtime(), 0.8) {
		t.Fatalf("SameHeightAirtime() = %v, want 0.8", symmetric.SameHeightAirtime())
	}
	if !closeTo(symmetric.SameHeightRange(), 6.4) {
		t.Fatalf("SameHeightRange() = %v, want 6.4", symmetric.SameHeightRange())
	}

	// The shipped default falls faster, so its airtime is shorter than the
	// symmetric one. A model that reused 2J/g here would overstate the reach.
	if profile.SameHeightAirtime() >= symmetric.SameHeightAirtime() {
		t.Fatal("a heavier fall gravity must shorten the airtime")
	}
}

// TestDefaultProfileInheritsRatiosNotNumbers checks the two ratios the
// reference game publishes, which are the only part of it this profile claims
// to inherit. The absolute speeds are this project's own.
func TestDefaultProfileInheritsRatiosNotNumbers(t *testing.T) {
	profile := DefaultProfile()
	dash := profile.Dash
	if ratio := dash.Speed / profile.MaxRunSpeed; math.Abs(ratio-2.41) > 1e-9 {
		t.Fatalf("dash over run = %v, want the published 2.41", ratio)
	}
	if ratio := dash.ShadowSpeed / dash.Speed; math.Abs(ratio-1.4) > 1e-9 {
		t.Fatalf("shadow over dash = %v, want the published 1.4", ratio)
	}
	if !closeTo(dash.Cooldown, 0.6) {
		t.Fatalf("dash cooldown = %v, want the published 0.6 s", dash.Cooldown)
	}
}

// TestBallisticMatchesTheVerifiedExamples checks the closed form against the
// two worked results the design notes verified against numerical integration:
// rising two cells and falling three, from the anchor profile.
func TestBallisticMatchesTheVerifiedExamples(t *testing.T) {
	profile := DefaultProfile()
	fake := &FakeOracle{}
	cases := []struct {
		name        string
		rise        float64
		wantAirtime float64
		wantReach   float64
	}{
		{name: "rise two cells", rise: 2, wantAirtime: 0.6828427124746191, wantReach: 5.462741699796953},
		{name: "fall three cells", rise: -3, wantAirtime: 0.9291502622129182, wantReach: 7.4332020977033455},
		{name: "same height", rise: 0, wantAirtime: 0.8, wantReach: 6.4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reach, airtime, ok := fake.ballistic(profile, profile.JumpVelocity, tc.rise)
			if !ok {
				t.Fatal("the ballistic form must be supported")
			}
			if math.Abs(airtime-tc.wantAirtime) > 1e-9 {
				t.Fatalf("airtime = %.16g, want %.16g", airtime, tc.wantAirtime)
			}
			if math.Abs(reach-tc.wantReach) > 1e-9 {
				t.Fatalf("reach = %.16g, want %.16g", reach, tc.wantReach)
			}
		})
	}

	// A target above the apex has no solution at all.
	if reach, _, _ := fake.ballistic(profile, profile.JumpVelocity, profile.ApexHeight()+0.001); reach >= 0 {
		t.Fatal("a target above the apex must have no ballistic solution")
	}
}

// TestCoyoteTradesHeightForDistance is the quantified form of the rule that
// coyote time is not extra reach. The loss is quadratic in tau and the gain is
// linear, so the two can never be folded into one envelope offset.
func TestCoyoteTradesHeightForDistance(t *testing.T) {
	profile := DefaultProfile()
	cases := []struct {
		tau      float64
		wantLoss float64
		wantGain float64
	}{
		{tau: 0.00, wantLoss: 0.00, wantGain: 0.00},
		{tau: 0.02, wantLoss: 0.01, wantGain: 0.16},
		{tau: 0.04, wantLoss: 0.04, wantGain: 0.32},
		{tau: 0.08, wantLoss: 0.16, wantGain: 0.64},
		{tau: 0.16, wantLoss: 0.64, wantGain: 1.28},
	}
	for _, tc := range cases {
		if got := profile.CoyoteHeightLoss(tc.tau); math.Abs(got-tc.wantLoss) > 1e-9 {
			t.Errorf("CoyoteHeightLoss(%v) = %v, want %v", tc.tau, got, tc.wantLoss)
		}
		if got := profile.CoyoteHorizontalGain(tc.tau); math.Abs(got-tc.wantGain) > 1e-9 {
			t.Errorf("CoyoteHorizontalGain(%v) = %v, want %v", tc.tau, got, tc.wantGain)
		}
	}

	// Doubling tau doubles the gain and quadruples the loss. That asymmetry is
	// the whole claim, so it is asserted rather than left to the table.
	if !closeTo(profile.CoyoteHorizontalGain(0.08), 2*profile.CoyoteHorizontalGain(0.04)) {
		t.Fatal("the horizontal gain must be linear in tau")
	}
	if !closeTo(profile.CoyoteHeightLoss(0.08), 4*profile.CoyoteHeightLoss(0.04)) {
		t.Fatal("the height loss must be quadratic in tau")
	}
}

func TestStoppingDistance(t *testing.T) {
	profile := DefaultProfile()
	if got := profile.StoppingDistance(profile.MaxRunSpeed); !closeTo(got, 64.0/240.0) {
		t.Fatalf("StoppingDistance(8) = %v, want %v", got, 64.0/240.0)
	}
	if got := profile.StoppingDistance(0); got != 0 {
		t.Fatalf("StoppingDistance(0) = %v, want 0", got)
	}
	none := profile
	none.Braking = 0
	if got := none.StoppingDistance(1); !math.IsInf(got, 1) {
		t.Fatalf("with no braking the stopping distance is %v, want +Inf", got)
	}
}

func TestFootingInsetsTheBody(t *testing.T) {
	profile := DefaultProfile()
	footing := profile.Footing(Span{Lo: 0, Hi: 4})
	inset := profile.BodyHalfWidth + profile.Margin
	if !closeTo(footing.Lo, inset) || !closeTo(footing.Hi, 4-inset) {
		t.Fatalf("Footing([0,4]) = %v, want [%v, %v]", footing, inset, 4-inset)
	}
	narrow := profile.Footing(Span{Lo: 0, Hi: 2 * inset * 0.5})
	if !narrow.IsEmpty() {
		t.Fatalf("an edge narrower than the body has footing %v, want empty", narrow)
	}
}

func TestFullResourcesIsNotTheZeroValue(t *testing.T) {
	profile := DefaultProfile()
	full := profile.FullResources()
	if full == (Resources{}) {
		t.Fatal("the zero Resources is the exhausted state, not the fresh one")
	}
	if full.DashCharges != profile.Dash.Charges {
		t.Fatalf("DashCharges = %d, want %d", full.DashCharges, profile.Dash.Charges)
	}
	if full.AirJumps != profile.DoubleJump.Charges {
		t.Fatalf("AirJumps = %d, want %d", full.AirJumps, profile.DoubleJump.Charges)
	}
	if !math.IsInf(full.ClingRemaining, 1) {
		t.Fatalf("ClingRemaining = %v, want unlimited", full.ClingRemaining)
	}
	if full.LastWall != WallSideNone {
		t.Fatalf("LastWall = %v, want none", full.LastWall)
	}

	bare := MovementProfile{}
	if got := bare.FullResources(); got != (Resources{}) {
		t.Fatalf("a profile with no upgrades has resources %v, want the zero value", got)
	}
}

func TestProfileAbilities(t *testing.T) {
	profile := DefaultProfile()
	want := NewAbilitySet(AbilityDash, AbilityShadowDash, AbilityDoubleJump, AbilityWallJump, AbilityClimb)
	if got := profile.Abilities(); got != want {
		t.Fatalf("Abilities() = %v, want %v", got, want)
	}
	stripped := profile
	stripped.Dash = nil
	stripped.Climb = nil
	if got := stripped.Abilities(); got != NewAbilitySet(AbilityDoubleJump, AbilityWallJump) {
		t.Fatalf("Abilities() = %v, want double-jump and wall-jump", got)
	}
}

// TestProfileValidateRejectsUnspecifiedModes is the reason three mechanics
// carry a mode instead of a boolean. A reset double jump and an impulse one
// are different characters, and defaulting one of them would silently decide
// which routes exist.
func TestProfileValidateRejectsUnspecifiedModes(t *testing.T) {
	cases := []struct {
		name    string
		corrupt func(*MovementProfile)
	}{
		{name: "double jump mode", corrupt: func(p *MovementProfile) { p.DoubleJump.Mode = DoubleJumpModeUnspecified }},
		{name: "variable jump mode", corrupt: func(p *MovementProfile) { p.VariableJump.Mode = VariableJumpModeUnspecified }},
		{name: "dash exit mode", corrupt: func(p *MovementProfile) { p.Dash.Exit = DashExitModeUnspecified }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			profile := DefaultProfile()
			profile.DoubleJump = cloneDoubleJump(profile.DoubleJump)
			profile.VariableJump = cloneVariableJump(profile.VariableJump)
			profile.Dash = cloneDash(profile.Dash)
			tc.corrupt(&profile)
			err := profile.Validate()
			if err == nil {
				t.Fatal("an unspecified mode was accepted and silently defaulted")
			}
			if !errors.Is(err, ErrInvalidProfile) {
				t.Fatalf("error %v does not wrap ErrInvalidProfile", err)
			}
		})
	}
}

func cloneDoubleJump(in *DoubleJumpProfile) *DoubleJumpProfile {
	out := *in
	return &out
}

func cloneVariableJump(in *VariableJumpProfile) *VariableJumpProfile {
	out := *in
	return &out
}

func cloneDash(in *DashProfile) *DashProfile {
	out := *in
	return &out
}

func cloneWallJump(in *WallJumpProfile) *WallJumpProfile {
	out := *in
	return &out
}

func TestProfileValidateRejectsMalformedNumbers(t *testing.T) {
	cases := []struct {
		name    string
		corrupt func(*MovementProfile)
	}{
		{name: "empty version", corrupt: func(p *MovementProfile) { p.Version = "" }},
		{name: "zero cell size", corrupt: func(p *MovementProfile) { p.CellSize = 0 }},
		{name: "negative gravity", corrupt: func(p *MovementProfile) { p.GravityUp = -1 }},
		{name: "infinite jump", corrupt: func(p *MovementProfile) { p.JumpVelocity = math.Inf(1) }},
		{name: "nan run speed", corrupt: func(p *MovementProfile) { p.MaxRunSpeed = math.NaN() }},
		{name: "zero body height", corrupt: func(p *MovementProfile) { p.BodyHeight = 0 }},
		{name: "negative margin", corrupt: func(p *MovementProfile) { p.Margin = -0.1 }},
		{name: "negative control rate", corrupt: func(p *MovementProfile) { p.ControlRate = -1 }},
		{name: "zero fall speed", corrupt: func(p *MovementProfile) { p.MaxFallSpeed = 0 }},
		{name: "zero safe fall height", corrupt: func(p *MovementProfile) { p.MaxSafeFallHeight = 0 }},
		{
			name: "double jump with no charge",
			corrupt: func(p *MovementProfile) {
				p.DoubleJump = cloneDoubleJump(p.DoubleJump)
				p.DoubleJump.Charges = 0
			},
		},
		{
			name: "dash that never refills",
			corrupt: func(p *MovementProfile) {
				p.Dash = cloneDash(p.Dash)
				p.Dash.RefillOn = 0
			},
		},
		{
			name: "dash with no direction",
			corrupt: func(p *MovementProfile) {
				p.Dash = cloneDash(p.Dash)
				p.Dash.Directions = 0
			},
		},
		{
			name: "wall jump that no shaft can satisfy",
			corrupt: func(p *MovementProfile) {
				p.WallJump = cloneWallJump(p.WallJump)
				p.WallJump.SameWallReuse = false
				p.WallJump.MaxConsecutive = 1
			},
		},
		{
			name:    "coyote window of zero",
			corrupt: func(p *MovementProfile) { p.Coyote = &CoyoteProfile{Window: 0, AppliesTo: CoyoteFromLedge} },
		},
		{
			name:    "coyote with no source",
			corrupt: func(p *MovementProfile) { p.Coyote = &CoyoteProfile{Window: 0.1} },
		},
		{
			name: "cut factor out of range",
			corrupt: func(p *MovementProfile) {
				p.VariableJump = cloneVariableJump(p.VariableJump)
				p.VariableJump.CutFactor = 1.5
			},
		},
		{
			name: "release gravity below the rise gravity",
			corrupt: func(p *MovementProfile) {
				p.VariableJump = &VariableJumpProfile{Mode: VariableJumpModeSwapGravity, ReleaseGravity: 1}
			},
		},
		{
			name:    "climb with no speed",
			corrupt: func(p *MovementProfile) { p.Climb = &ClimbProfile{CaptureHalfWidth: 0.4} },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			profile := DefaultProfile()
			tc.corrupt(&profile)
			err := profile.Validate()
			if err == nil {
				t.Fatal("a malformed profile was accepted")
			}
			if !errors.Is(err, ErrInvalidProfile) {
				t.Fatalf("error %v does not wrap ErrInvalidProfile", err)
			}
		})
	}
}

func TestProfileValidateAcceptsAMinimalMoveset(t *testing.T) {
	bare := DefaultProfile()
	bare.DoubleJump = nil
	bare.Dash = nil
	bare.WallJump = nil
	bare.Coyote = nil
	bare.VariableJump = nil
	bare.Climb = nil
	if err := bare.Validate(); err != nil {
		t.Fatalf("a profile with only the base moveset was rejected: %v", err)
	}
	if got := bare.Abilities(); got != 0 {
		t.Fatalf("Abilities() = %v, want the empty set", got)
	}
}

func TestSearchBudgetValidate(t *testing.T) {
	budget := DefaultSearchBudget()
	if err := budget.Validate(); err != nil {
		t.Fatalf("the default budget does not validate: %v", err)
	}
	cases := []struct {
		name    string
		corrupt func(*SearchBudget)
	}{
		{
			name: "no ceiling at all",
			corrupt: func(b *SearchBudget) {
				b.MaxCandidateEdges, b.MaxExpandedNodes, b.MaxCollisionTests = 0, 0, 0
			},
		},
		{name: "no fall horizon", corrupt: func(b *SearchBudget) { b.FallHorizon = 0 }},
		{name: "no launch resolution", corrupt: func(b *SearchBudget) { b.LaunchResolution = 0 }},
		{name: "no time resolution", corrupt: func(b *SearchBudget) { b.TimeResolution = math.NaN() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			corrupted := DefaultSearchBudget()
			tc.corrupt(&corrupted)
			if err := corrupted.Validate(); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("error %v does not wrap ErrInvalidConfig", err)
			}
		})
	}
}

// TestFallHorizonIsNotTheFallDamageRule keeps the two fall limits apart. One
// bounds the search, the other is a rule of the game, and conflating them
// either hides destinations or invents damage.
func TestFallHorizonIsNotTheFallDamageRule(t *testing.T) {
	profile := DefaultProfile()
	budget := DefaultSearchBudget()
	if !math.IsInf(profile.MaxSafeFallHeight, 1) {
		t.Fatalf("the default profile has fall damage at %v cells; the test assumes it has none", profile.MaxSafeFallHeight)
	}
	if math.IsInf(budget.FallHorizon, 1) {
		t.Fatal("the search still needs a finite fall horizon even when falling never hurts")
	}
}

func validBeats() BeatConfig {
	return BeatConfig{
		Definitions: []BeatDefinition{
			{Kind: BeatKindRest, MinCells: 4, MaxCells: 8},
			{Kind: BeatKindGap, MinCells: 3, MaxCells: 6, Difficulty: 40},
			{Kind: BeatKindGate, Requires: NewAbilitySet(AbilityDash)},
		},
		Spine: &BeatDistribution{
			Beats:       []BeatWeight{{Kind: BeatKindRest, Weight: 2}, {Kind: BeatKindGap, Weight: 5}},
			MinRunBeats: 3,
			MaxRunBeats: 9,
		},
	}
}

func TestBeatConfigValidate(t *testing.T) {
	if err := validBeats().Validate(); err != nil {
		t.Fatalf("a well-formed beat configuration was rejected: %v", err)
	}
	if definition, ok := validBeats().Definition(BeatKindGap); !ok || definition.MaxCells != 6 {
		t.Fatalf("Definition(gap) = %v, %v", definition, ok)
	}
	if _, ok := validBeats().Definition(BeatKindShaft); ok {
		t.Fatal("an undeclared kind must not resolve to a definition")
	}

	cases := []struct {
		name     string
		corrupt  func(*BeatConfig)
		sentinel error
	}{
		{name: "no definitions", corrupt: func(c *BeatConfig) { c.Definitions = nil }, sentinel: ErrInvalidBeats},
		{
			name:     "unspecified kind",
			corrupt:  func(c *BeatConfig) { c.Definitions[0].Kind = BeatKindUnspecified },
			sentinel: ErrInvalidBeats,
		},
		{
			name:     "duplicate definition",
			corrupt:  func(c *BeatConfig) { c.Definitions = append(c.Definitions, c.Definitions[0]) },
			sentinel: ErrInvalidBeats,
		},
		{
			name:     "half a cell span",
			corrupt:  func(c *BeatConfig) { c.Definitions[0].MaxCells = 0 },
			sentinel: ErrInvalidBeats,
		},
		{
			name:     "inverted cell span",
			corrupt:  func(c *BeatConfig) { c.Definitions[0].MinCells, c.Definitions[0].MaxCells = 9, 2 },
			sentinel: ErrInvalidBeats,
		},
		{name: "no spine", corrupt: func(c *BeatConfig) { c.Spine = nil }, sentinel: ErrInvalidBeats},
		{
			name:     "empty spine",
			corrupt:  func(c *BeatConfig) { c.Spine.Beats = nil },
			sentinel: ErrInvalidBeats,
		},
		{
			name:     "weight for an undefined kind",
			corrupt:  func(c *BeatConfig) { c.Spine.Beats[0].Kind = BeatKindShaft },
			sentinel: ErrInvalidBeats,
		},
		{
			name:     "zero weight",
			corrupt:  func(c *BeatConfig) { c.Spine.Beats[0].Weight = 0 },
			sentinel: ErrInvalidBeats,
		},
		{
			name: "duplicate weight",
			corrupt: func(c *BeatConfig) {
				c.Spine.Beats = append(c.Spine.Beats, BeatWeight{Kind: BeatKindRest, Weight: 1})
			},
			sentinel: ErrInvalidBeats,
		},
		{
			name:     "run length above the ceiling",
			corrupt:  func(c *BeatConfig) { c.Spine.MaxRunBeats = MaxBeatsPerRoom + 1 },
			sentinel: ErrLimitExceeded,
		},
		{
			name: "a branch distribution is checked too",
			corrupt: func(c *BeatConfig) {
				c.Branches = &BeatDistribution{Beats: []BeatWeight{{Kind: BeatKindSecret, Weight: 1}}}
			},
			sentinel: ErrInvalidBeats,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := validBeats()
			tc.corrupt(&config)
			err := config.Validate()
			if err == nil {
				t.Fatal("a malformed beat configuration was accepted")
			}
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("error %v does not wrap %v", err, tc.sentinel)
			}
		})
	}
}

func validConfig() Config {
	return Config{
		Seed:    7,
		Width:   64,
		Height:  32,
		Profile: DefaultProfile(),
		Beats:   validBeats(),
	}
}

func TestConfigNormalizeAppliesEveryDefault(t *testing.T) {
	effective := validConfig().Normalize()
	if effective.MaxRooms != MaxRooms {
		t.Fatalf("MaxRooms = %d, want the product ceiling %d", effective.MaxRooms, MaxRooms)
	}
	if effective.Budget != DefaultSearchBudget() {
		t.Fatalf("Budget = %+v, want the default budget", effective.Budget)
	}
	if effective.Discipline != NodeDisciplineRefined {
		t.Fatalf("Discipline = %s, want refined", effective.Discipline)
	}
	if effective.OmitWitness {
		t.Fatal("the zero Config must ask for witnesses: a certificate nothing can recheck is not evidence")
	}
	// Normalize is idempotent, which is what makes it safe to call before
	// validation and again before generation.
	again := effective.Normalize()
	if again.MaxRooms != effective.MaxRooms || again.Budget != effective.Budget ||
		again.Discipline != effective.Discipline || again.OmitWitness != effective.OmitWitness {
		t.Fatal("Normalize is not idempotent")
	}
}

func TestConfigNormalizeKeepsExplicitChoices(t *testing.T) {
	config := validConfig()
	config.MaxRooms = 12
	config.Discipline = NodeDisciplineRestOnly
	config.OmitWitness = true
	config.Budget = SearchBudget{MaxExpandedNodes: 5, FallHorizon: 1, LaunchResolution: 1, TimeResolution: 1}
	effective := config.Normalize()
	if effective.MaxRooms != 12 || effective.Discipline != NodeDisciplineRestOnly || !effective.OmitWitness {
		t.Fatalf("Normalize overwrote an explicit choice: %+v", effective)
	}
	if effective.Budget.MaxExpandedNodes != 5 {
		t.Fatalf("Normalize replaced an explicit budget: %+v", effective.Budget)
	}
}

func TestConfigValidate(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("a well-formed config was rejected: %v", err)
	}
	cases := []struct {
		name     string
		corrupt  func(*Config)
		sentinel error
	}{
		{name: "zero width", corrupt: func(c *Config) { c.Width = 0 }, sentinel: ErrInvalidConfig},
		{name: "zero height", corrupt: func(c *Config) { c.Height = 0 }, sentinel: ErrInvalidConfig},
		{name: "too many rooms", corrupt: func(c *Config) { c.MaxRooms = MaxRooms + 1 }, sentinel: ErrLimitExceeded},
		{name: "bad profile", corrupt: func(c *Config) { c.Profile.GravityUp = 0 }, sentinel: ErrInvalidProfile},
		{name: "bad beats", corrupt: func(c *Config) { c.Beats.Spine = nil }, sentinel: ErrInvalidBeats},
		{
			name:     "bad budget",
			corrupt:  func(c *Config) { c.Budget = SearchBudget{MaxExpandedNodes: 1} },
			sentinel: ErrInvalidConfig,
		},
		{
			name: "progression granting an unparameterised ability",
			corrupt: func(c *Config) {
				c.Profile.Dash = nil
				c.Progression = ProgressionPlan{Steps: []ProgressionStep{{Grants: NewAbilitySet(AbilityDash)}}}
			},
			sentinel: ErrInvalidProgression,
		},
		{
			name:     "duplicate terrain id",
			corrupt:  func(c *Config) { c.Terrain = []TerrainDefinition{{ID: "ice"}, {ID: "ice"}} },
			sentinel: ErrInvalidConfig,
		},
		{
			name:     "empty terrain id",
			corrupt:  func(c *Config) { c.Terrain = []TerrainDefinition{{}} },
			sentinel: ErrInvalidConfig,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := validConfig()
			tc.corrupt(&config)
			err := config.Validate()
			if err == nil {
				t.Fatal("a malformed config was accepted")
			}
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("error %v does not wrap %v", err, tc.sentinel)
			}
		})
	}
}

func TestProgressionPlanValidate(t *testing.T) {
	profile := DefaultProfile()
	good := ProgressionPlan{
		Steps: []ProgressionStep{
			{Name: "dash", Grants: NewAbilitySet(AbilityDash)},
			{Name: "claw", Grants: NewAbilitySet(AbilityWallJump)},
			{Name: "wings", Grants: NewAbilitySet(AbilityDoubleJump)},
		},
	}
	if err := good.Validate(profile); err != nil {
		t.Fatalf("a well-formed plan was rejected: %v", err)
	}

	cases := []struct {
		name string
		plan ProgressionPlan
	}{
		{name: "step grants nothing", plan: ProgressionPlan{Steps: []ProgressionStep{{Name: "void"}}}},
		{
			name: "step re-grants",
			plan: ProgressionPlan{
				Base:  NewAbilitySet(AbilityDash),
				Steps: []ProgressionStep{{Grants: NewAbilitySet(AbilityDash)}},
			},
		},
		{
			name: "two steps grant the same ability",
			plan: ProgressionPlan{Steps: []ProgressionStep{
				{Grants: NewAbilitySet(AbilityDash)},
				{Grants: NewAbilitySet(AbilityDash)},
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.plan.Validate(profile)
			if !errors.Is(err, ErrInvalidProgression) {
				t.Fatalf("error %v does not wrap ErrInvalidProgression", err)
			}
		})
	}

	// A base holding an ability the profile cannot perform is rejected too.
	bare := DefaultProfile()
	bare.WallJump = nil
	plan := ProgressionPlan{Base: NewAbilitySet(AbilityWallJump)}
	if err := plan.Validate(bare); !errors.Is(err, ErrInvalidProgression) {
		t.Fatalf("error %v does not wrap ErrInvalidProgression", err)
	}
}

func TestBitsetsAreDisjointFlags(t *testing.T) {
	refill := RefillOnGround | RefillOnWallJump
	if !refill.Has(RefillOnGround) || !refill.Has(RefillOnWallJump) {
		t.Fatalf("%b lost a flag", refill)
	}
	if refill.Has(RefillOnCooldown) {
		t.Fatalf("%b holds a flag it was never given", refill)
	}
	directions := DashDirectionHorizontal | DashDirectionDown
	if !directions.Has(DashDirectionDown) || directions.Has(DashDirectionUp) {
		t.Fatalf("%b is not the requested direction set", directions)
	}
	cancels := DashCancelByJump | DashCancelByDamage
	if !cancels.Has(DashCancelByJump) || cancels.Has(DashCancelByDash) {
		t.Fatalf("%b is not the requested cancel set", cancels)
	}
	sources := CoyoteFromLedge | CoyoteFromDropThrough
	if !sources.Has(CoyoteFromDropThrough) || sources.Has(CoyoteFromWallRelease) {
		t.Fatalf("%b is not the requested source set", sources)
	}
	// Each flag occupies its own bit; a collision would make two mechanics
	// indistinguishable.
	for _, pair := range [][2]uint8{
		{uint8(RefillOnGround), uint8(RefillOnWallCling)},
		{uint8(RefillOnWallJump), uint8(RefillOnClimb)},
		{uint8(DashCancelByJump), uint8(DashCancelByWallContact)},
		{uint8(DashCancelByDamage), uint8(DashCancelByDash)},
		{uint8(CoyoteFromLedge), uint8(CoyoteFromWallRelease)},
	} {
		if pair[0]&pair[1] != 0 {
			t.Fatalf("flags %b and %b share a bit", pair[0], pair[1])
		}
	}
}
