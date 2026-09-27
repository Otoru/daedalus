package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Otoru/daedalus"
	daedalusv1 "github.com/Otoru/daedalus/internal/gen/go/daedalus/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestStatusErrorMapsSDKCategories(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		code codes.Code
	}{
		{name: "invalid configuration", err: daedalus.ErrInvalidConfig, code: codes.InvalidArgument},
		{name: "limit exceeded", err: daedalus.ErrLimitExceeded, code: codes.ResourceExhausted},
		{name: "incompatible plant", err: daedalus.ErrNoCompatiblePlant, code: codes.FailedPrecondition},
		{name: "edge without route", err: daedalus.ErrUnroutableEdge, code: codes.FailedPrecondition},
		{name: "deadline", err: context.DeadlineExceeded, code: codes.DeadlineExceeded},
		{name: "cancellation", err: context.Canceled, code: codes.Canceled},
		{name: "internal failure", err: errors.New("internal secret"), code: codes.Internal},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := StatusError(testCase.err)

			assert.Equal(t, testCase.code, status.Code(got))
			if testCase.code == codes.Internal {
				assert.NotContains(t, got.Error(), "internal secret")
			}
		})
	}
}

// TestGenerateRejectsLimitsBeforeInvokingTheGenerator checks that a Grid or
// Room limit returns ResourceExhausted before the generator is called.
func TestGenerateRejectsLimitsBeforeInvokingTheGenerator(t *testing.T) {
	t.Parallel()

	var called atomic.Bool
	server := New(
		func(context.Context, daedalus.Config) (daedalus.Layout, error) {
			called.Store(true)
			return daedalus.Layout{}, nil
		},
		NewAdmission(1),
	)

	cases := []struct {
		name   string
		config *daedalusv1.Config
	}{
		{name: "width", config: &daedalusv1.Config{Width: 257, Height: 1}},
		{name: "cells", config: &daedalusv1.Config{Width: 256, Height: 257}},
		{name: "rooms", config: &daedalusv1.Config{Width: 1, Height: 1, MaxRooms: 257}},
		{
			name: "footprint",
			config: &daedalusv1.Config{
				Width: 1, Height: 1,
				RoomGeometry: &daedalusv1.RoomGeometry{MaxFootprintCells: 4097},
			},
		},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			_, err := server.Generate(
				context.Background(),
				&daedalusv1.GenerateRequest{Config: testCase.config},
			)

			require.Error(t, err)
			assert.Equal(t, codes.ResourceExhausted, status.Code(err))
		})
	}
	assert.False(t, called.Load())
}

func TestConfigFromProtoConvertsACompleteRequest(t *testing.T) {
	t.Parallel()

	got, err := ConfigFromProto(&daedalusv1.Config{
		Width: 12, Height: 13, CellSize: 2, Seed: 42,
		MinDistance: 3, MaxAttempts: 4, MaxRooms: 5,
		CorridorOrder:  daedalusv1.CorridorOrder_CORRIDOR_ORDER_Y_THEN_X,
		ExtraEdgeCount: 6,
		RoomRoleRequests: []*daedalusv1.RoomRoleRequest{{
			Role: daedalusv1.RoomRole_ROOM_ROLE_START, Count: 1,
			RequiredTags: []string{"start"},
		}},
		DensityRegions: []*daedalusv1.DensityRegion{{
			Min: &daedalusv1.Cell{X: 1, Y: 2},
			Max: &daedalusv1.Cell{X: 3, Y: 4}, MinDistance: 7,
		}},
		RoomGeometry: &daedalusv1.RoomGeometry{
			MinWidth: 2, MaxWidth: 3, MinHeight: 2, MaxHeight: 4,
			MaxFootprintCells: 12, MinRoomGap: 1,
			Shapes: []*daedalusv1.RoomShapeWeight{{
				Shape: daedalusv1.RoomShape_ROOM_SHAPE_L, Weight: 2,
			}},
		},
		PlantCatalog: &daedalusv1.PlantCatalog{
			Rooms: []*daedalusv1.RoomPlant{{
				Id: "room", Tags: []string{"start"}, Weight: 3,
				DoorDirections: []daedalusv1.Direction{
					daedalusv1.Direction_DIRECTION_NORTH,
				},
			}},
			Corridors: []*daedalusv1.CorridorPlant{{
				Id: "corridor", Tags: []string{"stone"}, Weight: 4,
			}},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, uint32(12), got.Width)
	assert.Equal(t, daedalus.CorridorOrderYThenX, got.CorridorOrder)
	require.Len(t, got.RoomRoleRequests, 1)
	assert.Equal(t, daedalus.RoomRoleStart, got.RoomRoleRequests[0].Role)
	require.Len(t, got.DensityRegions, 1)
	assert.Equal(t, daedalus.Cell{X: 3, Y: 4}, got.DensityRegions[0].Max)
	require.NotNil(t, got.RoomGeometry)
	assert.Equal(t, daedalus.RoomShapeL, got.RoomGeometry.Shapes[0].Shape)
	require.NotNil(t, got.PlantCatalog)
	assert.Equal(t, daedalus.DirectionNorth, got.PlantCatalog.Rooms[0].DoorDirections[0])
	assert.Equal(t, daedalus.PlantID("corridor"), got.PlantCatalog.Corridors[0].ID)
}

func TestConfigFromProtoRejectsMissingNestedMessages(t *testing.T) {
	t.Parallel()

	cases := []*daedalusv1.Config{
		{DensityRegions: []*daedalusv1.DensityRegion{nil}},
		{DensityRegions: []*daedalusv1.DensityRegion{{Min: nil, Max: &daedalusv1.Cell{}}}},
		{RoomRoleRequests: []*daedalusv1.RoomRoleRequest{nil}},
		{PlantCatalog: &daedalusv1.PlantCatalog{Rooms: []*daedalusv1.RoomPlant{nil}}},
		{RoomGeometry: &daedalusv1.RoomGeometry{Shapes: []*daedalusv1.RoomShapeWeight{nil}}},
	}
	for _, protoConfig := range cases {
		_, err := ConfigFromProto(protoConfig)

		require.Error(t, err)
		assert.ErrorIs(t, err, daedalus.ErrInvalidConfig)
	}
}

func TestLayoutToProtoPreservesPresenceAndFootprints(t *testing.T) {
	t.Parallel()

	roomID := daedalus.RoomID(0)
	role := daedalus.RoomRoleStart
	got := LayoutToProto(daedalus.Layout{
		Seed: 9,
		Grid: daedalus.Grid{
			Width: 1, Height: 1, CellSize: 2,
			Cells: []daedalus.CellState{{
				At: daedalus.Cell{}, Kind: daedalus.CellKindRoom, RoomID: &roomID,
			}},
		},
		Rooms: []daedalus.Room{{
			ID: 0, At: daedalus.Cell{}, Shape: daedalus.RoomShapeRectangle,
			Origin: daedalus.Cell{}, Width: 1, Height: 1,
			Cells: []daedalus.Cell{{}}, Role: &role, PlantID: "hall",
			Tags: []string{"start"},
		}},
	})

	require.NotNil(t, got)
	assert.Equal(t, uint64(9), got.Seed)
	require.Len(t, got.Grid.Cells, 1)
	require.NotNil(t, got.Grid.Cells[0].RoomId)
	assert.Equal(t, uint32(0), *got.Grid.Cells[0].RoomId)
	require.Len(t, got.Rooms, 1)
	require.NotNil(t, got.Rooms[0].Role)
	assert.Equal(t, daedalusv1.RoomRole_ROOM_ROLE_START, *got.Rooms[0].Role)
	assert.Equal(t, daedalusv1.RoomShape_ROOM_SHAPE_RECTANGLE, got.Rooms[0].Shape)
	require.Len(t, got.Rooms[0].Cells, 1)
}

func TestAdmissionSerializesExcessGenerations(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{}, 2)
	release := make(chan struct{}, 2)
	var concurrent atomic.Int32
	var maximum atomic.Int32
	server := New(func(ctx context.Context, _ daedalus.Config) (daedalus.Layout, error) {
		current := concurrent.Add(1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			concurrent.Add(-1)
			return daedalus.Layout{}, ctx.Err()
		}
		concurrent.Add(-1)
		return daedalus.Layout{}, nil
	}, NewAdmission(1))

	request := validRequest()
	results := make(chan error, 2)
	go func() {
		_, err := server.Generate(context.Background(), request)
		results <- err
	}()
	<-entered
	go func() {
		_, err := server.Generate(context.Background(), request)
		results <- err
	}()
	select {
	case <-entered:
		t.Fatal("second generation entered before the first was released")
	case <-time.After(30 * time.Millisecond):
	}
	release <- struct{}{}
	<-entered
	release <- struct{}{}

	require.NoError(t, <-results)
	require.NoError(t, <-results)
	assert.Equal(t, int32(1), maximum.Load())
}

// TestAdmissionHonorsDeadlineBeforeEntry checks that an expired deadline is
// reported as DeadlineExceeded and does not disturb the request already admitted.
func TestAdmissionHonorsDeadlineBeforeEntry(t *testing.T) {
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

	firstDone := make(chan error)
	go func() {
		_, err := server.Generate(context.Background(), validRequest())
		firstDone <- err
	}()
	<-entered

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := server.Generate(ctx, validRequest())

	require.Error(t, err)
	assert.Equal(t, codes.DeadlineExceeded, status.Code(err))
	close(release)
	require.NoError(t, <-firstDone)
}

func TestGeneratePropagatesClientCancellation(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	server := New(func(ctx context.Context, _ daedalus.Config) (daedalus.Layout, error) {
		close(entered)
		<-ctx.Done()
		return daedalus.Layout{}, ctx.Err()
	}, NewAdmission(1))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		_, err := server.Generate(ctx, validRequest())
		done <- err
	}()
	<-entered

	cancel()

	err := <-done
	require.Error(t, err)
	assert.Equal(t, codes.Canceled, status.Code(err))
}

// TestBeginShutdownCancelsWorkAndStopsAdmission checks that shutdown cancels
// the generation in progress and stops admitting new work.
func TestBeginShutdownCancelsWorkAndStopsAdmission(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	server := New(func(ctx context.Context, _ daedalus.Config) (daedalus.Layout, error) {
		close(entered)
		<-ctx.Done()
		return daedalus.Layout{}, ctx.Err()
	}, NewAdmission(1))
	done := make(chan error)
	go func() {
		_, err := server.Generate(context.Background(), validRequest())
		done <- err
	}()
	<-entered

	server.BeginShutdown()

	err := <-done
	require.Error(t, err)
	assert.Equal(t, codes.Canceled, status.Code(err))
	_, err = server.Generate(context.Background(), validRequest())
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func validRequest() *daedalusv1.GenerateRequest {
	return &daedalusv1.GenerateRequest{
		Config: &daedalusv1.Config{Width: 1, Height: 1, Seed: 1},
	}
}
