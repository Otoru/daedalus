// Package service adapts the pure SDK to the gRPC v1 service.
package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/Otoru/daedalus"
	daedalusv1 "github.com/Otoru/daedalus/internal/gen/go/daedalus/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maximumGridDimension uint32 = 256

// GenerateFunc is the shareable generation boundary used by the
// gRPC and HTTP adapters.
type GenerateFunc func(context.Context, daedalus.Config) (daedalus.Layout, error)

// Server implements DaedalusService without retaining state between generations.
type Server struct {
	daedalusv1.UnimplementedDaedalusServiceServer

	generate  GenerateFunc
	admission *Admission
	context   context.Context
	cancel    context.CancelFunc
	serving   atomic.Bool
}

// New creates the service with the given generator and shared limiter.
// A nil generate selects only the SDK's built-in algorithms.
func New(generate GenerateFunc, admission *Admission) *Server {
	if generate == nil {
		generator := daedalus.Generator{}
		generate = generator.GenerateContext
	}
	if admission == nil {
		panic("admission cannot be nil")
	}
	serviceContext, cancel := context.WithCancel(context.Background())
	server := &Server{
		generate: generate, admission: admission,
		context: serviceContext, cancel: cancel,
	}
	server.serving.Store(true)
	return server
}

// Generate validates, admits, and runs a complete generation.
func (server *Server) Generate(
	ctx context.Context,
	request *daedalusv1.GenerateRequest,
) (*daedalusv1.GenerateResponse, error) {
	if request == nil || request.Config == nil {
		return nil, status.Error(codes.InvalidArgument, "request must contain config")
	}
	if err := checkHardLimits(request.Config); err != nil {
		return nil, StatusError(err)
	}
	config, err := ConfigFromProto(request.Config)
	if err != nil {
		return nil, StatusError(err)
	}
	if err := server.admission.acquire(ctx); err != nil {
		if errors.Is(err, errAdmissionStopped) {
			return nil, status.Error(codes.Unavailable, "service is shutting down")
		}
		return nil, StatusError(err)
	}
	defer server.admission.release()

	generationContext, cancel := context.WithCancel(ctx)
	stopShutdownCancellation := context.AfterFunc(server.context, cancel)
	defer func() {
		stopShutdownCancellation()
		cancel()
	}()

	layout, err := server.generate(generationContext, config)
	if err != nil {
		return nil, StatusError(err)
	}
	return &daedalusv1.GenerateResponse{Layout: LayoutToProto(layout)}, nil
}

// BeginShutdown marks the service as not serving, stops admissions, and
// cancels in-flight work. It is idempotent.
func (server *Server) BeginShutdown() {
	if server.serving.Swap(false) {
		server.admission.Stop()
		server.cancel()
	}
}

// Serving reports whether the service still accepts new requests.
func (server *Server) Serving() bool {
	return server.serving.Load()
}

// Wait waits for generations already admitted to finish.
func (server *Server) Wait(ctx context.Context) error {
	return server.admission.Wait(ctx)
}

// StatusError converts SDK and Context categories into gRPC status
// without revealing internal details.
func StatusError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "generation deadline exceeded")
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "generation canceled")
	case errors.Is(err, daedalus.ErrInvalidConfig):
		return status.Error(codes.InvalidArgument, "invalid configuration")
	case errors.Is(err, daedalus.ErrLimitExceeded):
		return status.Error(codes.ResourceExhausted, "resource limit exceeded")
	case errors.Is(err, daedalus.ErrNoCompatiblePlant):
		// The specification fixes HTTP 422, but not the gRPC status. The
		// conservative reading uses FailedPrecondition, its closest analogue.
		return status.Error(codes.FailedPrecondition, "no compatible plant")
	case errors.Is(err, daedalus.ErrUnroutableEdge):
		// Same conservative reading as ErrNoCompatiblePlant.
		return status.Error(codes.FailedPrecondition, "edge has no orthogonal route")
	default:
		return status.Error(codes.Internal, "internal failure while generating layout")
	}
}

func checkHardLimits(config *daedalusv1.Config) error {
	if config.Width > maximumGridDimension || config.Height > maximumGridDimension {
		return fmt.Errorf("%w: grid dimension exceeds the v1 maximum", daedalus.ErrLimitExceeded)
	}
	cellCount := uint64(config.Width) * uint64(config.Height)
	if cellCount > uint64(daedalus.MaxCells) {
		return fmt.Errorf("%w: cell count exceeds the v1 maximum", daedalus.ErrLimitExceeded)
	}
	if config.MaxRooms > daedalus.MaxRooms {
		return fmt.Errorf("%w: max_rooms exceeds the v1 maximum", daedalus.ErrLimitExceeded)
	}
	if config.RoomGeometry != nil &&
		config.RoomGeometry.MaxFootprintCells > daedalus.MaxFootprintCells {
		return fmt.Errorf("%w: max_footprint_cells exceeds the v1 maximum", daedalus.ErrLimitExceeded)
	}
	return nil
}
