package service

import (
	"context"
	"errors"
	"fmt"

	daedalusv1 "github.com/Otoru/daedalus/internal/gen/go/daedalus/v1"
	"github.com/Otoru/daedalus/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PlatformLayout is the hand-off shape until the platform generator lands.
// It keeps the plane, directed graph and three-valued judgement together.
type PlatformLayout struct {
	Config    platform.Config
	Plane     platform.Plane
	JumpGraph platform.JumpGraph
	Judgement platform.Judgement
}
type GeneratePlatformFunc func(context.Context, platform.Config) (PlatformLayout, error)

func (server *Server) GeneratePlatform(ctx context.Context, request *daedalusv1.GeneratePlatformRequest) (*daedalusv1.GeneratePlatformResponse, error) {
	if request == nil || request.Config == nil {
		return nil, status.Error(codes.InvalidArgument, "request must contain platform config")
	}
	if err := checkPlatformLimits(request.Config); err != nil {
		return nil, platformStatusError(err)
	}
	config, err := platformConfigFromProto(request.Config)
	if err != nil {
		return nil, platformStatusError(err)
	}
	if err := config.Validate(); err != nil {
		return nil, platformStatusError(err)
	}
	if server.generatePlatform == nil {
		return nil, status.Error(codes.Unimplemented, "platform generator is not installed")
	}
	if err := server.admission.acquire(ctx); err != nil {
		if errors.Is(err, errAdmissionStopped) {
			return nil, status.Error(codes.Unavailable, "service is shutting down")
		}
		return nil, platformStatusError(err)
	}
	defer server.admission.release()
	workContext, cancel := context.WithCancel(ctx)
	stopShutdownCancellation := context.AfterFunc(server.context, cancel)
	defer func() { stopShutdownCancellation(); cancel() }()
	layout, err := server.generatePlatform(workContext, config)
	if err != nil {
		return nil, platformStatusError(err)
	}
	if err := layout.Judgement.Validate(); err != nil {
		return nil, platformStatusError(err)
	}
	return &daedalusv1.GeneratePlatformResponse{Layout: platformLayoutToProto(layout)}, nil
}

func checkPlatformLimits(config *daedalusv1.PlatformConfig) error {
	if config.Width > platform.MaxRoomSide || config.Height > platform.MaxRoomSide {
		return fmt.Errorf("%w: platform side exceeds maximum", platform.ErrLimitExceeded)
	}
	if uint64(config.Width)*uint64(config.Height) > platform.MaxRoomCells {
		return fmt.Errorf("%w: platform cell count exceeds maximum", platform.ErrLimitExceeded)
	}
	if config.MaxRooms > platform.MaxRooms {
		return fmt.Errorf("%w: platform room count exceeds maximum", platform.ErrLimitExceeded)
	}
	return nil
}

func platformStatusError(err error) error {
	if errors.Is(err, platform.ErrLimitExceeded) {
		return status.Error(codes.ResourceExhausted, "platform resource limit exceeded")
	}
	if errors.Is(err, platform.ErrInvalidProfile) {
		return status.Errorf(codes.InvalidArgument, "invalid platform request: %v", err)
	}
	if errors.Is(err, platform.ErrInvalidConfig) || errors.Is(err, platform.ErrInvalidBeats) || errors.Is(err, platform.ErrInvalidGeometry) {
		return status.Error(codes.InvalidArgument, "invalid platform request")
	}
	return StatusError(err)
}
