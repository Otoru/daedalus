// Package service adapts the pure SDK to the gRPC v1 service.
package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/Otoru/daedalus"
	daedalusv1 "github.com/Otoru/daedalus/internal/gen/go/daedalus/v1"
	"github.com/Otoru/daedalus/utils/pathfinding"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maximumGridDimension uint32 = 256

// GenerateFunc is the shareable generation boundary used by the
// gRPC and HTTP adapters.
type GenerateFunc func(context.Context, daedalus.Config) (daedalus.Layout, error)

// Server implements DaedalusService without retaining state between admitted work items.
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

// ComputeSteps answers one cardinal step per position. Limits are rejected
// before a slot is taken, so an oversized request fails even while admission
// is stopped. The service stores nothing: the cost grid travels on the call,
// and no field is retained after the response is built.
func (server *Server) ComputeSteps(
	ctx context.Context,
	request *daedalusv1.ComputeStepsRequest,
) (*daedalusv1.ComputeStepsResponse, error) {
	if request == nil || request.CostGrid == nil {
		return nil, status.Error(codes.InvalidArgument, "request must contain cost grid")
	}
	if err := checkNavigationLimits(request); err != nil {
		return nil, StatusError(err)
	}
	grid, queries, err := navigationFromProto(request)
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

	navigationContext, cancel := context.WithCancel(ctx)
	stopShutdownCancellation := context.AfterFunc(server.context, cancel)
	defer func() {
		stopShutdownCancellation()
		cancel()
	}()

	results, err := pathfinding.Answer(navigationContext, grid, queries)
	if err != nil {
		return nil, StatusError(err)
	}
	return stepsToProto(results), nil
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

// StatusError converts SDK and Context categories into gRPC status.
// Invalid Config and an invalid navigation request are InvalidArgument, a
// resource limit is ResourceExhausted, a deadline is DeadlineExceeded, and
// caller cancellation is Canceled. A missing plant, an unroutable edge, and
// dishonest plugin output are FailedPrecondition. Anything else is
// Internal. The status text has no stack trace and no sensitive data.
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
	case errors.Is(err, daedalus.ErrInvalidNavigation):
		return status.Error(codes.InvalidArgument, "invalid navigation request")
	case errors.Is(err, daedalus.ErrInvalidVisibility):
		return status.Error(codes.InvalidArgument, "invalid visibility request")
	case errors.Is(err, daedalus.ErrLimitExceeded):
		return status.Error(codes.ResourceExhausted, "resource limit exceeded")
	case errors.Is(err, daedalus.ErrNoCompatiblePlant):
		// HTTP maps a missing compatible plant to 422. gRPC has no 422, so
		// FailedPrecondition is the closest status: Config was accepted and
		// generation still cannot produce a Layout.
		return status.Error(codes.FailedPrecondition, "no compatible plant")
	case errors.Is(err, daedalus.ErrUnroutableEdge):
		// HTTP maps an unroutable edge to 422, the same category as a
		// missing plant. gRPC reports FailedPrecondition for the same reason.
		return status.Error(codes.FailedPrecondition, "edge has no orthogonal route")
	case errors.Is(err, daedalus.ErrUnconnectablePlacement):
		// The Rooms cannot host a separated spanning tree. That is a property
		// of the placement, known before routing, and the same 422 category.
		return status.Error(codes.FailedPrecondition, "placement cannot be connected under the separation rule")
	case errors.Is(err, daedalus.ErrInvalidPlugin):
		// Unreachable over gRPC, which only ever runs the built-in algorithms.
		// Dishonest output from an injected Placer or Connector — a mask that
		// does not match the declared shape, a placement outside the Grid, a
		// collision, or a disconnected graph — is the same caller-facing
		// category as a missing plant, so it is FailedPrecondition rather
		// than Internal.
		return status.Error(codes.FailedPrecondition, "invalid plugin output")
	default:
		return status.Error(codes.Internal, "internal failure while generating layout")
	}
}

// checkHardLimits rejects a grid, room count, or footprint above the v1
// maxima before generation allocates. The failure is ErrLimitExceeded,
// mapped to ResourceExhausted on gRPC and to HTTP 413, and nothing is
// truncated to fit.
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
