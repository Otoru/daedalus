package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Otoru/daedalus"
	daedalusv1 "github.com/Otoru/daedalus/internal/gen/go/daedalus/v1"
	"github.com/Otoru/daedalus/utils/pathfinding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestStatusErrorMapsEachSentinel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		sentinel error
		code     codes.Code
	}{
		{name: "invalid configuration", sentinel: daedalus.ErrInvalidConfig, code: codes.InvalidArgument},
		{name: "invalid navigation", sentinel: daedalus.ErrInvalidNavigation, code: codes.InvalidArgument},
		{name: "limit exceeded", sentinel: daedalus.ErrLimitExceeded, code: codes.ResourceExhausted},
		{name: "incompatible plant", sentinel: daedalus.ErrNoCompatiblePlant, code: codes.FailedPrecondition},
		{name: "edge without route", sentinel: daedalus.ErrUnroutableEdge, code: codes.FailedPrecondition},
		{name: "placement cannot be separated", sentinel: daedalus.ErrUnconnectablePlacement, code: codes.FailedPrecondition},
		{name: "invalid plugin", sentinel: daedalus.ErrInvalidPlugin, code: codes.FailedPrecondition},
		{name: "deadline", sentinel: context.DeadlineExceeded, code: codes.DeadlineExceeded},
		{name: "cancellation", sentinel: context.Canceled, code: codes.Canceled},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			wrapped := fmt.Errorf("caller context: %w", testCase.sentinel)
			require.ErrorIs(t, wrapped, testCase.sentinel)
			assert.Equal(t, testCase.code, status.Code(StatusError(wrapped)))
		})
	}
}

func TestDirectionAndStepStatusConvertBothWays(t *testing.T) {
	t.Parallel()

	directions := []struct {
		goValue daedalus.Direction
		proto   daedalusv1.Direction
	}{
		{daedalus.DirectionNorth, daedalusv1.Direction_DIRECTION_NORTH},
		{daedalus.DirectionEast, daedalusv1.Direction_DIRECTION_EAST},
		{daedalus.DirectionSouth, daedalusv1.Direction_DIRECTION_SOUTH},
		{daedalus.DirectionWest, daedalusv1.Direction_DIRECTION_WEST},
	}
	for _, pair := range directions {
		assert.Equal(t, pair.proto, mapDirectionToProto(pair.goValue))
		assert.Equal(t, pair.goValue, mapDirection(pair.proto))
		// A cast keeps the integer and drops the proto's unspecified slot,
		// so North becomes unspecified and West becomes South.
		assert.NotEqual(t, pair.proto, daedalusv1.Direction(pair.goValue))
	}
	assert.Equal(t, invalidDirection, mapDirection(daedalusv1.Direction_DIRECTION_UNSPECIFIED))
	assert.NotEqual(t, daedalus.DirectionNorth, mapDirection(daedalusv1.Direction_DIRECTION_UNSPECIFIED))
	assert.Equal(t, daedalusv1.Direction_DIRECTION_UNSPECIFIED, mapDirectionToProto(daedalus.Direction(99)))

	statuses := []struct {
		goValue pathfinding.StepStatus
		proto   daedalusv1.StepStatus
	}{
		{pathfinding.StepStatusMoved, daedalusv1.StepStatus_STEP_STATUS_MOVED},
		{pathfinding.StepStatusArrived, daedalusv1.StepStatus_STEP_STATUS_ARRIVED},
		{pathfinding.StepStatusUnreachable, daedalusv1.StepStatus_STEP_STATUS_UNREACHABLE},
		{pathfinding.StepStatusOutside, daedalusv1.StepStatus_STEP_STATUS_OUTSIDE},
		{pathfinding.StepStatusBlocked, daedalusv1.StepStatus_STEP_STATUS_BLOCKED},
	}
	for _, pair := range statuses {
		assert.Equal(t, pair.proto, mapStepStatusToProto(pair.goValue))
		got, err := mapStepStatus(pair.proto)
		require.NoError(t, err)
		assert.Equal(t, pair.goValue, got)
		assert.NotEqual(t, pair.proto, daedalusv1.StepStatus(pair.goValue))
	}
	_, err := mapStepStatus(daedalusv1.StepStatus_STEP_STATUS_UNSPECIFIED)
	require.ErrorIs(t, err, daedalus.ErrInvalidNavigation)
	_, err = mapStepStatus(daedalusv1.StepStatus(99))
	require.ErrorIs(t, err, daedalus.ErrInvalidNavigation)
	assert.Equal(t, daedalusv1.StepStatus_STEP_STATUS_UNSPECIFIED, mapStepStatusToProto(pathfinding.StepStatus(99)))
}

func TestComputeStepsRejectsAMissingCostGrid(t *testing.T) {
	t.Parallel()

	server := New(nil, NewAdmission(1))
	_, err := server.ComputeSteps(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = server.ComputeSteps(context.Background(), &daedalusv1.ComputeStepsRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestComputeStepsRejectsACostLengthThatDisagreesWithTheGrid(t *testing.T) {
	t.Parallel()

	server := New(nil, NewAdmission(1))
	_, err := server.ComputeSteps(context.Background(), &daedalusv1.ComputeStepsRequest{
		CostGrid: &daedalusv1.CostGrid{Width: 2, Height: 2, Costs: []byte{1, 1, 1}},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestComputeStepsRejectsAGridWiderThanTheMaximum(t *testing.T) {
	t.Parallel()

	server := New(nil, NewAdmission(1))
	_, err := server.ComputeSteps(context.Background(), &daedalusv1.ComputeStepsRequest{
		CostGrid: &daedalusv1.CostGrid{Width: 257, Height: 1, Costs: []byte{1}},
	})
	require.Error(t, err)
	assert.Equal(t, codes.ResourceExhausted, status.Code(err))
}

func TestComputeStepsRejectsAnOversizedGridBeforeAdmission(t *testing.T) {
	t.Parallel()

	server := New(nil, NewAdmission(1))
	server.BeginShutdown()

	_, err := server.ComputeSteps(context.Background(), &daedalusv1.ComputeStepsRequest{
		CostGrid: &daedalusv1.CostGrid{Width: 257, Height: 1, Costs: []byte{1}},
	})
	require.Error(t, err)
	assert.Equal(t, codes.ResourceExhausted, status.Code(err))
}

func TestComputeStepsRejectsASourceThatLeavesTheFieldUndefined(t *testing.T) {
	t.Parallel()

	server := New(nil, NewAdmission(1))
	request := corridorRequest()
	request.Queries[0].Sources[0].At = &daedalusv1.Cell{X: 20, Y: 0}

	_, err := server.ComputeSteps(context.Background(), request)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestComputeStepsRejectsABiasOutsideTheDistanceRange(t *testing.T) {
	t.Parallel()

	server := New(nil, NewAdmission(1))
	request := corridorRequest()
	request.Queries[0].Sources[0].Bias = uint32(pathfinding.MaxDistance) + 1

	_, err := server.ComputeSteps(context.Background(), request)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	request.Queries[0].Sources[0].Bias = ^uint32(0)
	_, err = server.ComputeSteps(context.Background(), request)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestComputeStepsAnswersOutsideAndUnreachableWithoutFailingTheCall(t *testing.T) {
	t.Parallel()

	server := New(nil, NewAdmission(1))
	response, err := server.ComputeSteps(context.Background(), corridorRequest())
	require.NoError(t, err)
	require.Len(t, response.GetResults(), 1)
	steps := response.Results[0].GetSteps()
	require.Len(t, steps, 4)

	assert.Equal(t, daedalusv1.StepStatus_STEP_STATUS_ARRIVED, steps[0].GetStatus())
	assert.Equal(t, daedalusv1.Direction_DIRECTION_UNSPECIFIED, steps[0].GetDirection())
	assert.Equal(t, int32(0), steps[0].GetDistance())

	assert.Equal(t, daedalusv1.StepStatus_STEP_STATUS_MOVED, steps[1].GetStatus())
	assert.Equal(t, daedalusv1.Direction_DIRECTION_WEST, steps[1].GetDirection())
	assert.Equal(t, int32(0), steps[1].GetDistance())

	assert.Equal(t, daedalusv1.StepStatus_STEP_STATUS_UNREACHABLE, steps[2].GetStatus())
	assert.Equal(t, daedalusv1.Direction_DIRECTION_UNSPECIFIED, steps[2].GetDirection())
	assert.Equal(t, int32(-1), steps[2].GetDistance())

	assert.Equal(t, daedalusv1.StepStatus_STEP_STATUS_OUTSIDE, steps[3].GetStatus())
	assert.Equal(t, daedalusv1.Direction_DIRECTION_UNSPECIFIED, steps[3].GetDirection())
	assert.Equal(t, int32(-1), steps[3].GetDistance())
}

func TestComputeStepsRetainsNothingBetweenCalls(t *testing.T) {
	t.Parallel()

	server := New(nil, NewAdmission(1))
	towardOrigin := &daedalusv1.ComputeStepsRequest{
		CostGrid: &daedalusv1.CostGrid{Width: 3, Height: 1, Costs: []byte{1, 1, 1}},
		Queries: []*daedalusv1.Query{{
			Sources:   []*daedalusv1.Source{{At: &daedalusv1.Cell{X: 0, Y: 0}}},
			Positions: []*daedalusv1.Cell{{X: 2, Y: 0}},
		}},
	}
	towardFarEnd := &daedalusv1.ComputeStepsRequest{
		CostGrid: &daedalusv1.CostGrid{Width: 3, Height: 1, Costs: []byte{1, 1, 1}},
		Queries: []*daedalusv1.Query{{
			Sources:   []*daedalusv1.Source{{At: &daedalusv1.Cell{X: 2, Y: 0}}},
			Positions: []*daedalusv1.Cell{{X: 0, Y: 0}},
		}},
	}

	first, err := server.ComputeSteps(context.Background(), towardOrigin)
	require.NoError(t, err)
	require.Len(t, first.GetResults(), 1)
	require.Len(t, first.Results[0].GetSteps(), 1)
	assert.Equal(t, daedalusv1.Direction_DIRECTION_WEST, first.Results[0].Steps[0].GetDirection())

	second, err := server.ComputeSteps(context.Background(), towardFarEnd)
	require.NoError(t, err)
	require.Len(t, second.GetResults(), 1)
	require.Len(t, second.Results[0].GetSteps(), 1)
	assert.Equal(t, daedalusv1.Direction_DIRECTION_EAST, second.Results[0].Steps[0].GetDirection())
}

func TestComputeStepsHonorsDeadlineWhileAdmissionIsHeld(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	server := New(func(ctx context.Context, _ daedalus.Config) (daedalus.Layout, error) {
		close(entered)
		select {
		case <-release:
			return daedalus.Layout{}, nil
		case <-ctx.Done():
			return daedalus.Layout{}, ctx.Err()
		}
	}, NewAdmission(1))

	firstDone := make(chan error, 1)
	go func() {
		_, err := server.Generate(context.Background(), validRequest())
		firstDone <- err
	}()
	<-entered

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := server.ComputeSteps(ctx, corridorRequest())
	require.Error(t, err)
	assert.Equal(t, codes.DeadlineExceeded, status.Code(err))

	close(release)
	require.NoError(t, <-firstDone)
}

func TestComputeStepsShutdownWhileWaitingReturnsUnavailableOrCanceled(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	server := New(func(ctx context.Context, _ daedalus.Config) (daedalus.Layout, error) {
		close(entered)
		select {
		case <-release:
			return daedalus.Layout{}, nil
		case <-ctx.Done():
			return daedalus.Layout{}, ctx.Err()
		}
	}, NewAdmission(1))

	go func() {
		_, _ = server.Generate(context.Background(), validRequest())
	}()
	<-entered

	done := make(chan error, 1)
	go func() {
		_, err := server.ComputeSteps(context.Background(), corridorRequest())
		done <- err
	}()

	server.BeginShutdown()

	err := <-done
	require.Error(t, err)
	code := status.Code(err)
	assert.Contains(t, []codes.Code{codes.Unavailable, codes.Canceled}, code)
	close(release)
}

// corridorRequest is a 4×1 turn. Cells 0 and 1 are open, cell 2 is a wall,
// and cell 3 is open but cut off from the source at (0, 0). The positions
// are the source, the open neighbour, the walled-off cell, and a cell outside
// the grid.
func corridorRequest() *daedalusv1.ComputeStepsRequest {
	return &daedalusv1.ComputeStepsRequest{
		CostGrid: &daedalusv1.CostGrid{
			Width: 4, Height: 1,
			Costs: []byte{1, 1, 0, 1},
		},
		Queries: []*daedalusv1.Query{{
			Sources: []*daedalusv1.Source{{
				At: &daedalusv1.Cell{X: 0, Y: 0},
			}},
			Positions: []*daedalusv1.Cell{
				{X: 0, Y: 0},
				{X: 1, Y: 0},
				{X: 3, Y: 0},
				{X: 9, Y: 0},
			},
		}},
	}
}
