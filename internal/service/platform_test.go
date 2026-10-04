package service

import (
	"context"
	"testing"

	"github.com/Otoru/daedalus"
	daedalusv1 "github.com/Otoru/daedalus/internal/gen/go/daedalus/v1"
	"github.com/Otoru/daedalus/platform"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

// This deliberately uses a numeric enum value a newer client could send to
// an older server. It must never become empty space by default.
func TestLayoutFromProtoRejectsUnknownCellKind(t *testing.T) {
	t.Parallel()

	_, err := layoutFromProto(&daedalusv1.Layout{Grid: &daedalusv1.Grid{
		Width: 1, Height: 1,
		Cells: []*daedalusv1.CellState{{At: &daedalusv1.Cell{}, Kind: daedalusv1.CellKind(99)}},
	}})

	require.ErrorIs(t, err, daedalus.ErrInvalidGating)
}

func TestGeneratePlatformRejectsLimitsBeforeAdmission(t *testing.T) {
	t.Parallel()
	server := New(nil, NewAdmission(1))
	server.BeginShutdown()
	_, err := server.GeneratePlatform(context.Background(), &daedalusv1.GeneratePlatformRequest{Config: &daedalusv1.PlatformConfig{Width: 1025, Height: 1}})
	require.Error(t, err)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
}

func TestGeneratePlatformCarriesUnknownJudgementAndReason(t *testing.T) {
	t.Parallel()
	server := NewWithPlatform(nil, func(_ context.Context, config platform.Config) (PlatformLayout, error) {
		return PlatformLayout{Config: config, Plane: platform.Plane{Width: 1, Height: 1}, Judgement: platform.Judgement{
			Verdict: platform.VerdictUnknown, Reason: platform.ReasonBudgetExhausted,
			Model: platform.ModelFake, ProfileVersion: platform.ProfileVersionM1,
			Budget: platform.BudgetReport{Exhausted: true},
		}}, nil
	}, NewAdmission(1))
	response, err := server.GeneratePlatform(context.Background(), validPlatformRequest())
	require.NoError(t, err)
	require.Equal(t, daedalusv1.PlatformVerdict_PLATFORM_VERDICT_UNKNOWN, response.Layout.Judgement.Verdict)
	require.Equal(t, daedalusv1.PlatformVerdictReason_PLATFORM_VERDICT_REASON_BUDGET_EXHAUSTED, response.Layout.Judgement.Reason)
	require.True(t, response.Layout.Judgement.Budget.Exhausted)
	payload, err := protojson.Marshal(response.Layout.Judgement)
	require.NoError(t, err)
	require.Contains(t, string(payload), "PLATFORM_VERDICT_UNKNOWN")
}

func TestGeneratePlatformUsesAnExplicitMovementProfile(t *testing.T) {
	t.Parallel()

	var request daedalusv1.GeneratePlatformRequest
	require.NoError(t, (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal([]byte(`{
  "config": {
    "width": 1, "height": 1,
    "beat_definitions": [{"kind": 1}],
    "spine": {"beats": [{"kind": 1, "weight": 1}]},
    "profile": {
      "version": "m1-test", "cell_size": 1, "control_rate": 60,
      "gravity_up": 50, "gravity_down": 62.5, "jump_velocity": 10,
      "max_run_speed": 8, "ground_accel": 80, "air_accel": 40, "braking": 120,
      "body_half_width": 0.35, "body_height": 1.6, "margin": 0.05,
      "max_fall_speed": 20, "max_safe_fall_height": "Infinity"
    }
  }
}`), &request))

	var got platform.Config
	server := NewWithPlatform(nil, func(_ context.Context, config platform.Config) (PlatformLayout, error) {
		got = config
		return PlatformLayout{Config: config, Judgement: platform.Judgement{
			Verdict: platform.VerdictCertified, Reason: platform.ReasonWitnessFound,
			Model: platform.ModelM1, ProfileVersion: config.Profile.Version,
		}}, nil
	}, NewAdmission(1))

	_, err := server.GeneratePlatform(context.Background(), &request)
	require.NoError(t, err)
	require.Equal(t, 10.0, got.Profile.JumpVelocity)
	require.Equal(t, "m1-test", got.Profile.Version)
}

func TestPlatformProfileWirePreservesDefaultAndChangesTheGraph(t *testing.T) {
	base, err := platformConfigFromProto(profileProofConfig())
	require.NoError(t, err)

	explicitRequest := profileProofConfig()
	explicitRequest.Profile = movementProfileToProto(platform.DefaultProfile())
	explicit, err := platformConfigFromProto(explicitRequest)
	require.NoError(t, err)

	baseLayout, err := platform.Generate(context.Background(), platform.NewM1Oracle(), base)
	require.NoError(t, err)
	explicitLayout, err := platform.Generate(context.Background(), platform.NewM1Oracle(), explicit)
	require.NoError(t, err)
	require.Equal(t, baseLayout.Plane, explicitLayout.Plane)
	require.Len(t, explicitLayout.Plane.Rooms, len(baseLayout.Plane.Rooms))
	require.Len(t, explicitLayout.JumpGraph.Nodes, len(baseLayout.JumpGraph.Nodes))
	require.Len(t, explicitLayout.JumpGraph.Edges, len(baseLayout.JumpGraph.Edges))

	lowJumpRequest := profileProofConfig()
	lowJump := platform.DefaultProfile()
	lowJump.Version = "m1-low-jump"
	lowJump.JumpVelocity = 10
	lowJumpRequest.Profile = movementProfileToProto(lowJump)
	lowJumpConfig, err := platformConfigFromProto(lowJumpRequest)
	require.NoError(t, err)
	lowJumpLayout, err := platform.Generate(context.Background(), platform.NewM1Oracle(), lowJumpConfig)
	require.NoError(t, err)

	t.Logf("default absent: rooms=%d nodes=%d edges=%d; default explicit: rooms=%d nodes=%d edges=%d; low-jump: rooms=%d nodes=%d edges=%d", len(baseLayout.Plane.Rooms), len(baseLayout.JumpGraph.Nodes), len(baseLayout.JumpGraph.Edges), len(explicitLayout.Plane.Rooms), len(explicitLayout.JumpGraph.Nodes), len(explicitLayout.JumpGraph.Edges), len(lowJumpLayout.Plane.Rooms), len(lowJumpLayout.JumpGraph.Nodes), len(lowJumpLayout.JumpGraph.Edges))
	require.Less(t, len(lowJumpLayout.JumpGraph.Edges), len(baseLayout.JumpGraph.Edges))
}

func TestGeneratePlatformRejectsAnInvalidExplicitProfile(t *testing.T) {
	request := validPlatformRequest()
	request.Config.Profile = movementProfileToProto(platform.DefaultProfile())
	request.Config.Profile.GravityUp = 0
	server := NewWithPlatform(nil, func(context.Context, platform.Config) (PlatformLayout, error) {
		t.Fatal("invalid profile must not reach generation")
		return PlatformLayout{}, nil
	}, NewAdmission(1))

	_, err := server.GeneratePlatform(context.Background(), request)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "GravityUp")
	t.Logf("code=%s message=%q", status.Code(err), status.Convert(err).Message())
}

func TestPlatformProfileRejectsZeroModes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*daedalusv1.MovementProfile)
	}{
		{"double jump", func(profile *daedalusv1.MovementProfile) { profile.DoubleJump.Mode = 0 }},
		{"dash exit", func(profile *daedalusv1.MovementProfile) { profile.Dash.Exit = 0 }},
		{"variable jump", func(profile *daedalusv1.MovementProfile) { profile.VariableJump.Mode = 0 }},
		{"coyote source", func(profile *daedalusv1.MovementProfile) { profile.Coyote.AppliesTo = 0 }},
		{"dash direction", func(profile *daedalusv1.MovementProfile) { profile.Dash.Directions = 0 }},
		{"dash cancel", func(profile *daedalusv1.MovementProfile) { profile.Dash.Cancel = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := profileProofConfig()
			config.Profile = movementProfileToProto(platform.DefaultProfile())
			test.mutate(config.Profile)
			_, err := platformConfigFromProto(config)
			require.ErrorIs(t, err, platform.ErrInvalidProfile)
		})
	}
}

func profileProofConfig() *daedalusv1.PlatformConfig {
	return &daedalusv1.PlatformConfig{
		Seed: 4242, Width: 160, Height: 100,
		BeatDefinitions: []*daedalusv1.PlatformBeatDefinition{{Kind: 1}, {Kind: 2}, {Kind: 3}, {Kind: 4}, {Kind: 5}},
		Spine:           &daedalusv1.PlatformBeatDistribution{Beats: []*daedalusv1.PlatformBeatWeight{{Kind: 1, Weight: 1}, {Kind: 2, Weight: 1}, {Kind: 3, Weight: 1}, {Kind: 4, Weight: 1}, {Kind: 5, Weight: 1}}},
	}
}

func movementProfileToProto(profile platform.MovementProfile) *daedalusv1.MovementProfile {
	result := &daedalusv1.MovementProfile{Version: profile.Version, CellSize: profile.CellSize, ControlRate: profile.ControlRate, GravityUp: profile.GravityUp, GravityDown: profile.GravityDown, JumpVelocity: profile.JumpVelocity, MaxRunSpeed: profile.MaxRunSpeed, GroundAccel: profile.GroundAccel, AirAccel: profile.AirAccel, Braking: profile.Braking, BodyHalfWidth: profile.BodyHalfWidth, BodyHeight: profile.BodyHeight, Margin: profile.Margin, MaxFallSpeed: profile.MaxFallSpeed, MaxSafeFallHeight: profile.MaxSafeFallHeight}
	if value := profile.DoubleJump; value != nil {
		result.DoubleJump = &daedalusv1.MovementProfile_DoubleJumpProfile{Mode: daedalusv1.MovementProfile_DoubleJumpMode(value.Mode), Velocity: value.Velocity, Charges: uint32(value.Charges), RefillOn: uint32(value.RefillOn), MinDelay: value.MinDelay}
	}
	if value := profile.Dash; value != nil {
		result.Dash = &daedalusv1.MovementProfile_DashProfile{Speed: value.Speed, ShadowSpeed: value.ShadowSpeed, ShadowPassesHazard: value.ShadowPassesHazard, Duration: value.Duration, SuspendsGravity: value.SuspendsGravity, Exit: daedalusv1.MovementProfile_DashExitMode(value.Exit), Charges: uint32(value.Charges), Cooldown: value.Cooldown, RefillOn: uint32(value.RefillOn), Directions: uint32(value.Directions), Cancel: uint32(value.Cancel)}
	}
	if value := profile.WallJump; value != nil {
		result.WallJump = &daedalusv1.MovementProfile_WallJumpProfile{ImpulseX: value.ImpulseX, ImpulseY: value.ImpulseY, SameWallReuse: value.SameWallReuse, MaxConsecutive: uint32(value.MaxConsecutive), Stamina: value.Stamina, SlideSpeed: value.SlideSpeed, InputLock: value.InputLock, MinWallHeight: value.MinWallHeight}
	}
	if value := profile.Coyote; value != nil {
		result.Coyote = &daedalusv1.MovementProfile_CoyoteProfile{Window: value.Window, AppliesTo: uint32(value.AppliesTo)}
	}
	if value := profile.VariableJump; value != nil {
		result.VariableJump = &daedalusv1.MovementProfile_VariableJumpProfile{Mode: daedalusv1.MovementProfile_VariableJumpMode(value.Mode), CutFactor: value.CutFactor, ReleaseGravity: value.ReleaseGravity, MinHoldTime: value.MinHoldTime}
	}
	if value := profile.Climb; value != nil {
		result.Climb = &daedalusv1.MovementProfile_ClimbProfile{Speed: value.Speed, CaptureHalfWidth: value.CaptureHalfWidth, CanJumpOff: value.CanJumpOff, ExitImpulseX: value.ExitImpulseX}
	}
	return result
}

func validPlatformRequest() *daedalusv1.GeneratePlatformRequest {
	return &daedalusv1.GeneratePlatformRequest{Config: &daedalusv1.PlatformConfig{
		Width: 1, Height: 1,
		BeatDefinitions: []*daedalusv1.PlatformBeatDefinition{{Kind: uint32(platform.BeatKindRest)}},
		Spine:           &daedalusv1.PlatformBeatDistribution{Beats: []*daedalusv1.PlatformBeatWeight{{Kind: uint32(platform.BeatKindRest), Weight: 1}}},
	}}
}
