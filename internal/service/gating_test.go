package service

import (
	"context"
	"testing"

	"github.com/Otoru/daedalus"
	daedalusv1 "github.com/Otoru/daedalus/internal/gen/go/daedalus/v1"
	"github.com/Otoru/daedalus/utils/gating"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func serviceChainLayout() daedalus.Layout {
	start, middle, target := daedalus.RoomRoleStart, daedalus.RoomRoleTreasure, daedalus.RoomRoleBoss
	return daedalus.Layout{
		Grid:      daedalus.Grid{Width: 8, Height: 1, Cells: make([]daedalus.CellState, 8)},
		Rooms:     []daedalus.Room{{ID: 0, Role: &start, DoorIDs: []daedalus.DoorID{0}}, {ID: 1, Role: &middle, DoorIDs: []daedalus.DoorID{1, 2}}, {ID: 2, Role: &target, DoorIDs: []daedalus.DoorID{3}}},
		Corridors: []daedalus.Corridor{{ID: 0, FromRoomID: 0, ToRoomID: 1, FromDoorID: 0, ToDoorID: 1, Centerline: []daedalus.Cell{{X: 2, Y: 0}}}, {ID: 1, FromRoomID: 1, ToRoomID: 2, FromDoorID: 2, ToDoorID: 3, Centerline: []daedalus.Cell{{X: 5, Y: 0}}}},
		Doors:     []daedalus.Door{{ID: 0, RoomID: 0, CorridorIDs: []daedalus.CorridorID{0}}, {ID: 1, RoomID: 1, CorridorIDs: []daedalus.CorridorID{0}}, {ID: 2, RoomID: 1, CorridorIDs: []daedalus.CorridorID{1}}, {ID: 3, RoomID: 2, CorridorIDs: []daedalus.CorridorID{1}}},
	}
}

func TestBuildGatingPlanWireMatchesSDKBytes(t *testing.T) {
	layout := serviceChainLayout()
	target := uint32(2)
	request := gating.Request{Seed: 17, StartRoomID: 0, TargetRoomID: func() *daedalus.RoomID { id := daedalus.RoomID(target); return &id }(), MainGateCount: 1}
	want, err := gating.Build(context.Background(), layout, request)
	require.NoError(t, err)

	server := New(nil, NewAdmission(1))
	response, err := server.BuildGatingPlan(context.Background(), &daedalusv1.BuildGatingPlanRequest{
		Layout: LayoutToProto(layout), Request: &daedalusv1.GatingRequest{Seed: uint64(request.Seed), StartRoomId: uint32(request.StartRoomID), TargetRoomId: &target, MainGateCount: 1},
	})
	require.NoError(t, err)
	wantWire := &daedalusv1.GatingPlan{Seed: uint64(want.Seed), Gates: []*daedalusv1.Gate{{Id: 0, Kind: daedalusv1.GateKind_GATE_KIND_MAIN, DoorId: uint32(want.Gates[0].DoorID), KeyRoomId: uint32(want.Gates[0].KeyRoomID)}}}
	wantBytes, err := proto.Marshal(wantWire)
	require.NoError(t, err)
	gotBytes, err := proto.Marshal(response.Plan)
	require.NoError(t, err)
	require.Equal(t, wantBytes, gotBytes)
}

func TestBuildGatingPlanChecksLimitsBeforeAdmission(t *testing.T) {
	server := New(nil, NewAdmission(1))
	server.BeginShutdown()
	_, err := server.BuildGatingPlan(context.Background(), &daedalusv1.BuildGatingPlanRequest{
		Layout:  &daedalusv1.Layout{Grid: &daedalusv1.Grid{Width: 1, Height: 1}, Rooms: make([]*daedalusv1.Room, daedalus.MaxRooms+1)},
		Request: &daedalusv1.GatingRequest{},
	})
	require.Error(t, err)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
}

func TestRoomRoleMaxEdgesRoundTrips(t *testing.T) {
	config := daedalus.Config{RoomRoleRequests: []daedalus.RoomRoleRequest{{Role: daedalus.RoomRoleBoss, Count: 1, MaxRoomEdges: 1}}}
	wire := ConfigToProto(config)
	require.Equal(t, uint32(1), wire.RoomRoleRequests[0].MaxRoomEdges)
	got, err := ConfigFromProto(wire)
	require.NoError(t, err)
	require.Equal(t, uint32(1), got.RoomRoleRequests[0].MaxRoomEdges)
}
