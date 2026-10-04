package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/Otoru/daedalus"
	daedalusv1 "github.com/Otoru/daedalus/internal/gen/go/daedalus/v1"
	"github.com/Otoru/daedalus/utils/gating"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	maxGatingCorridors = uint64(daedalus.MaxRooms) * uint64(daedalus.MaxRooms-1) / 2
	maxGatingDoors     = 2 * maxGatingCorridors
)

func checkGatingLimits(request *daedalusv1.BuildGatingPlanRequest) error {
	if request == nil || request.Layout == nil || request.Request == nil {
		return fmt.Errorf("%w: layout and request are required", daedalus.ErrInvalidGating)
	}
	layout := request.Layout
	if layout.Grid == nil {
		return fmt.Errorf("%w: layout grid is required", daedalus.ErrInvalidGating)
	}
	if layout.Grid.Width > maximumGridDimension || layout.Grid.Height > maximumGridDimension || uint64(layout.Grid.Width)*uint64(layout.Grid.Height) > uint64(daedalus.MaxCells) {
		return fmt.Errorf("%w: layout grid exceeds the v1 maximum", daedalus.ErrLimitExceeded)
	}
	if len(layout.Rooms) > daedalus.MaxRooms {
		return fmt.Errorf("%w: room count exceeds the v1 maximum", daedalus.ErrLimitExceeded)
	}
	if uint64(len(layout.Corridors)) > maxGatingCorridors || uint64(len(layout.Doors)) > maxGatingDoors {
		return fmt.Errorf("%w: gating topology exceeds the v1 maximum", daedalus.ErrLimitExceeded)
	}
	if len(layout.Grid.Cells) > daedalus.MaxCells {
		return fmt.Errorf("%w: grid cell count exceeds the v1 maximum", daedalus.ErrLimitExceeded)
	}
	if err := checkGatingPayloadLimits(layout); err != nil {
		return err
	}
	count := uint64(request.Request.MainGateCount) + uint64(request.Request.OptionalGateCount)
	if count > uint64(gating.MaxGates) {
		return fmt.Errorf("%w: gate count exceeds %d", daedalus.ErrLimitExceeded, gating.MaxGates)
	}
	return nil
}

func checkGatingPayloadLimits(layout *daedalusv1.Layout) error {
	for _, room := range layout.Rooms {
		if room != nil && len(room.Cells) > daedalus.MaxCells {
			return fmt.Errorf("%w: room footprint exceeds the v1 maximum", daedalus.ErrLimitExceeded)
		}
	}
	for _, corridor := range layout.Corridors {
		if corridor != nil && (len(corridor.Cells) > daedalus.MaxCells || len(corridor.Centerline) > daedalus.MaxCells) {
			return fmt.Errorf("%w: corridor payload exceeds the v1 maximum", daedalus.ErrLimitExceeded)
		}
	}
	return nil
}

func gatingRequestFromProto(source *daedalusv1.GatingRequest) gating.Request {
	target := gating.Request{Seed: daedalus.Seed(source.Seed), StartRoomID: daedalus.RoomID(source.StartRoomId), MainGateCount: source.MainGateCount, OptionalGateCount: source.OptionalGateCount}
	if source.TargetRoomId != nil {
		roomID := daedalus.RoomID(*source.TargetRoomId)
		target.TargetRoomID = &roomID
	}
	return target
}

func gatingPlanToProto(source gating.Plan) *daedalusv1.GatingPlan {
	target := &daedalusv1.GatingPlan{Seed: uint64(source.Seed), Gates: make([]*daedalusv1.Gate, len(source.Gates))}
	for i, gate := range source.Gates {
		kind := daedalusv1.GateKind_GATE_KIND_MAIN
		if gate.Kind == gating.GateKindOptional {
			kind = daedalusv1.GateKind_GATE_KIND_OPTIONAL
		}
		target.Gates[i] = &daedalusv1.Gate{Id: uint32(gate.ID), Kind: kind, DoorId: uint32(gate.DoorID), KeyRoomId: uint32(gate.KeyRoomID)}
	}
	return target
}

func (server *Server) BuildGatingPlan(ctx context.Context, request *daedalusv1.BuildGatingPlanRequest) (*daedalusv1.BuildGatingPlanResponse, error) {
	if err := checkGatingLimits(request); err != nil {
		return nil, StatusError(err)
	}
	layout, err := layoutFromProto(request.Layout)
	if err != nil {
		return nil, StatusError(err)
	}
	gatingRequest := gatingRequestFromProto(request.Request)
	if err := server.admission.acquire(ctx); err != nil {
		if errors.Is(err, errAdmissionStopped) {
			return nil, status.Error(codes.Unavailable, "service is shutting down")
		}
		return nil, StatusError(err)
	}
	defer server.admission.release()
	workContext, cancel := context.WithCancel(ctx)
	stopShutdownCancellation := context.AfterFunc(server.context, cancel)
	defer func() { stopShutdownCancellation(); cancel() }()
	plan, err := gating.Build(workContext, layout, gatingRequest)
	if err != nil {
		return nil, StatusError(err)
	}
	return &daedalusv1.BuildGatingPlanResponse{Plan: gatingPlanToProto(plan)}, nil
}
