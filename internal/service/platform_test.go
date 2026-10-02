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
}

func validPlatformRequest() *daedalusv1.GeneratePlatformRequest {
	return &daedalusv1.GeneratePlatformRequest{Config: &daedalusv1.PlatformConfig{
		Width: 1, Height: 1,
		BeatDefinitions: []*daedalusv1.PlatformBeatDefinition{{Kind: uint32(platform.BeatKindRest)}},
		Spine:           &daedalusv1.PlatformBeatDistribution{Beats: []*daedalusv1.PlatformBeatWeight{{Kind: uint32(platform.BeatKindRest), Weight: 1}}},
	}}
}
