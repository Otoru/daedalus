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
	"google.golang.org/protobuf/proto"
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
		{name: "placement cannot be separated", err: daedalus.ErrUnconnectablePlacement, code: codes.FailedPrecondition},
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
			MaxFootprintCells: 12, MinRoomGap: 1,
			Shapes: []*daedalusv1.RoomShapeWeight{{
				Shape: daedalusv1.RoomShape_ROOM_SHAPE_L, Weight: 2,
				Width:  &daedalusv1.DimensionRange{Min: 2, Max: 3},
				Height: &daedalusv1.DimensionRange{Min: 2, Max: 4},
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
	assert.Equal(t, daedalus.DimensionRange{Min: 2, Max: 3}, got.RoomGeometry.Shapes[0].Width)
	assert.Equal(t, daedalus.DimensionRange{Min: 2, Max: 4}, got.RoomGeometry.Shapes[0].Height)
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
		{CorridorGeometry: &daedalusv1.CorridorGeometry{Widths: []*daedalusv1.CorridorWidthWeight{nil}}},
	}
	for _, protoConfig := range cases {
		_, err := ConfigFromProto(protoConfig)

		require.Error(t, err)
		assert.ErrorIs(t, err, daedalus.ErrInvalidConfig)
	}
}

func TestAbsentCorridorGeometryStaysNil(t *testing.T) {
	t.Parallel()

	got, err := ConfigFromProto(&daedalusv1.Config{Width: 16, Height: 16, Seed: 1, MaxRooms: 4})

	require.NoError(t, err)
	assert.Nil(t, got.CorridorGeometry)
}

func TestOmittedMaxRoomEdgesStaysUnlimited(t *testing.T) {
	t.Parallel()

	got, err := ConfigFromProto(&daedalusv1.Config{Width: 16, Height: 16, Seed: 1})

	require.NoError(t, err)
	assert.Zero(t, got.MaxRoomEdges)
}

func TestMaxRoomEdgesCopiesTheWireValue(t *testing.T) {
	t.Parallel()

	got, err := ConfigFromProto(&daedalusv1.Config{
		Width: 16, Height: 16, Seed: 1, MaxRoomEdges: 4,
	})

	require.NoError(t, err)
	assert.Equal(t, uint32(4), got.MaxRoomEdges)
	assert.Equal(t, uint32(4), ConfigToProto(got).GetMaxRoomEdges())
}

func TestGenerateRejectsAMaxRoomEdgesCeilingOfOne(t *testing.T) {
	t.Parallel()

	server := New(daedalus.Generator{}.GenerateContext, NewAdmission(1))

	_, err := server.Generate(context.Background(), &daedalusv1.GenerateRequest{
		Config: &daedalusv1.Config{Width: 8, Height: 8, Seed: 1, MaxRoomEdges: 1},
	})

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestPresentCorridorGeometryCopiesWidths(t *testing.T) {
	t.Parallel()

	got, err := ConfigFromProto(&daedalusv1.Config{
		Width: 20, Height: 14, Seed: 0, MaxRooms: 4, MinDistance: 5,
		CorridorGeometry: &daedalusv1.CorridorGeometry{
			Widths: []*daedalusv1.CorridorWidthWeight{
				{Width: 3, Weight: 2},
				{Width: 1, Weight: 5},
			},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, got.CorridorGeometry)
	assert.Equal(t, []daedalus.CorridorWidthWeight{
		{Width: 3, Weight: 2},
		{Width: 1, Weight: 5},
	}, got.CorridorGeometry.Widths)
}

func TestCorridorGeometryRoundTripsThroughProto(t *testing.T) {
	t.Parallel()

	source := &daedalusv1.Config{
		Width: 20, Height: 14, CellSize: 1, Seed: 7,
		MinDistance: 5, MaxAttempts: 30, MaxRooms: 4,
		CorridorOrder: daedalusv1.CorridorOrder_CORRIDOR_ORDER_Y_THEN_X,
		CorridorGeometry: &daedalusv1.CorridorGeometry{
			Widths: []*daedalusv1.CorridorWidthWeight{
				{Width: 2, Weight: 1},
				{Width: 1, Weight: 3},
			},
		},
		RoomGeometry: &daedalusv1.RoomGeometry{
			MaxFootprintCells: 9, MinRoomGap: 2,
			Shapes: []*daedalusv1.RoomShapeWeight{{
				Shape: daedalusv1.RoomShape_ROOM_SHAPE_RECTANGLE, Weight: 1,
				Width:  &daedalusv1.DimensionRange{Min: 2, Max: 3},
				Height: &daedalusv1.DimensionRange{Min: 2, Max: 3},
			}},
		},
	}

	got, err := ConfigFromProto(source)
	require.NoError(t, err)
	assert.True(t, proto.Equal(source, ConfigToProto(got)))
}

func TestOmittedShapeDimensionsUseTheDynamicProfile(t *testing.T) {
	t.Parallel()

	got, err := ConfigFromProto(&daedalusv1.Config{
		Width: 16, Height: 16, Seed: 1,
		RoomGeometry: &daedalusv1.RoomGeometry{
			MaxFootprintCells: 81, MinRoomGap: 1,
			Shapes: []*daedalusv1.RoomShapeWeight{
				{Shape: daedalusv1.RoomShape_ROOM_SHAPE_RECTANGLE, Weight: 1},
				{
					Shape:  daedalusv1.RoomShape_ROOM_SHAPE_CIRCLE,
					Weight: 1,
					Height: &daedalusv1.DimensionRange{Min: 7, Max: 9},
				},
			},
		},
	})

	require.NoError(t, err)
	require.Len(t, got.RoomGeometry.Shapes, 2)
	assert.Equal(t, daedalus.DimensionRange{Min: 3, Max: 9}, got.RoomGeometry.Shapes[0].Width)
	assert.Equal(t, daedalus.DimensionRange{Min: 3, Max: 9}, got.RoomGeometry.Shapes[0].Height)
	assert.Equal(t, daedalus.DimensionRange{Min: 5, Max: 9}, got.RoomGeometry.Shapes[1].Width)
	assert.Equal(t, daedalus.DimensionRange{Min: 7, Max: 9}, got.RoomGeometry.Shapes[1].Height)
	assert.NotEqual(t, daedalus.DimensionRange{}, got.RoomGeometry.Shapes[0].Width)

	again, err := ConfigFromProto(ConfigToProto(got))
	require.NoError(t, err)
	assert.Equal(t, got.RoomGeometry, again.RoomGeometry)
}

func TestOmittedCircleOnASmallGridIsTheUnclampedProfile(t *testing.T) {
	t.Parallel()

	got, err := ConfigFromProto(&daedalusv1.Config{
		Width: 4, Height: 4,
		RoomGeometry: &daedalusv1.RoomGeometry{
			MaxFootprintCells: 16,
			Shapes: []*daedalusv1.RoomShapeWeight{{
				Shape: daedalusv1.RoomShape_ROOM_SHAPE_CIRCLE, Weight: 1,
			}},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, daedalus.DimensionRange{Min: 5, Max: 9}, got.RoomGeometry.Shapes[0].Width)
	assert.Equal(t, daedalus.DimensionRange{Min: 5, Max: 9}, got.RoomGeometry.Shapes[0].Height)
}

func TestExplicitZeroDimensionRangeIsNotTheOmittedProfile(t *testing.T) {
	t.Parallel()

	got, err := ConfigFromProto(&daedalusv1.Config{
		Width: 16, Height: 16,
		RoomGeometry: &daedalusv1.RoomGeometry{
			MaxFootprintCells: 81,
			Shapes: []*daedalusv1.RoomShapeWeight{{
				Shape:  daedalusv1.RoomShape_ROOM_SHAPE_CIRCLE,
				Weight: 1,
				Width:  &daedalusv1.DimensionRange{Min: 0, Max: 0},
				Height: &daedalusv1.DimensionRange{Min: 6, Max: 6},
			}},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, daedalus.DimensionRange{}, got.RoomGeometry.Shapes[0].Width)
	assert.Equal(t, daedalus.DimensionRange{Min: 6, Max: 6}, got.RoomGeometry.Shapes[0].Height)
}

func TestEmptyCorridorGeometryStaysPresent(t *testing.T) {
	t.Parallel()

	got, err := ConfigFromProto(&daedalusv1.Config{
		Width: 8, Height: 8, Seed: 1,
		CorridorGeometry: &daedalusv1.CorridorGeometry{},
	})

	require.NoError(t, err)
	require.NotNil(t, got.CorridorGeometry)
	assert.Empty(t, got.CorridorGeometry.Widths)
}

func TestGenerateKeepsAbsentCorridorGeometryValidAndRejectsAnEmptyOne(t *testing.T) {
	t.Parallel()

	server := New(daedalus.Generator{}.GenerateContext, NewAdmission(1))

	_, absentErr := server.Generate(context.Background(), &daedalusv1.GenerateRequest{
		Config: &daedalusv1.Config{Width: 16, Height: 16, Seed: 0, MaxRooms: 4},
	})
	require.NoError(t, absentErr)

	_, emptyErr := server.Generate(context.Background(), &daedalusv1.GenerateRequest{
		Config: &daedalusv1.Config{
			Width: 8, Height: 8, Seed: 1,
			CorridorGeometry: &daedalusv1.CorridorGeometry{},
		},
	})
	require.Error(t, emptyErr)
	assert.Equal(t, codes.InvalidArgument, status.Code(emptyErr))
}

func TestLayoutToProtoCarriesCenterlineAndDoorSpan(t *testing.T) {
	t.Parallel()

	got := LayoutToProto(daedalus.Layout{
		Corridors: []daedalus.Corridor{{
			Cells:      []daedalus.Cell{{X: 1, Y: 2}, {X: 2, Y: 2}, {X: 1, Y: 3}},
			Centerline: []daedalus.Cell{{X: 1, Y: 2}, {X: 2, Y: 2}},
		}},
		Doors: []daedalus.Door{{
			Span: 2, Direction: daedalus.DirectionEast, At: daedalus.Cell{X: 0, Y: 2},
		}},
	})

	require.Len(t, got.Corridors, 1)
	require.Len(t, got.Corridors[0].Centerline, 2)
	assert.Equal(t, int32(1), got.Corridors[0].Centerline[0].X)
	assert.Equal(t, int32(2), got.Corridors[0].Centerline[0].Y)
	assert.Equal(t, int32(2), got.Corridors[0].Centerline[1].X)
	require.Len(t, got.Corridors[0].Cells, 3)
	require.Len(t, got.Doors, 1)
	assert.Equal(t, uint32(2), got.Doors[0].Span)
}

func TestTerrainConfigRoundTripsThroughProtoWithoutChangingBytes(t *testing.T) {
	t.Parallel()

	source := &daedalusv1.Config{
		Width: 16, Height: 16, Seed: 9,
		CorridorOrder:    daedalusv1.CorridorOrder_CORRIDOR_ORDER_X_THEN_Y,
		RoomRoleRequests: []*daedalusv1.RoomRoleRequest{},
		DensityRegions:   []*daedalusv1.DensityRegion{},
		Terrain: &daedalusv1.TerrainConfig{
			Definitions: []*daedalusv1.TerrainDefinition{
				{Id: "grass", EntryCost: 1, Transparent: true},
				{Id: "water", EntryCost: 7, Transparent: true},
			},
			Rooms: &daedalusv1.TerrainDistribution{
				NoneWeight: 2, MinPatchCells: 8, MaxPatchCells: 12,
				Terrains: []*daedalusv1.TerrainWeight{{TerrainId: "water", Weight: 3}},
			},
		},
	}
	got, err := ConfigFromProto(source)
	require.NoError(t, err)
	assert.True(t, proto.Equal(source, ConfigToProto(got)))
}

func TestLayoutToProtoCarriesTerrainByCopy(t *testing.T) {
	t.Parallel()

	indices := []byte{0, 1, 0, 1}
	layer := &daedalus.TerrainLayer{
		Palette: []daedalus.TerrainDefinition{{ID: "water", EntryCost: 4, Transparent: true}},
		Indices: indices,
	}
	got := LayoutToProto(daedalus.Layout{Grid: daedalus.Grid{Width: 2, Height: 2, Terrain: layer}})
	require.NotNil(t, got.Grid.Terrain)
	assert.Equal(t, indices, got.Grid.Terrain.Indices)
	indices[1] = 9
	assert.Equal(t, byte(1), got.Grid.Terrain.Indices[1])
}

func TestConfigFromProtoRejectsTerrainEntryCostAboveByte(t *testing.T) {
	t.Parallel()

	_, err := ConfigFromProto(&daedalusv1.Config{Terrain: &daedalusv1.TerrainConfig{
		Definitions: []*daedalusv1.TerrainDefinition{{Id: "water", EntryCost: 256}},
		Rooms:       &daedalusv1.TerrainDistribution{NoneWeight: 1},
	}})
	require.Error(t, err)
	assert.ErrorIs(t, err, daedalus.ErrInvalidConfig)
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
