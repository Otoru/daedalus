package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/Otoru/daedalus"
	daedalusv1 "github.com/Otoru/daedalus/internal/gen/go/daedalus/v1"
	"github.com/Otoru/daedalus/utils/vision"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// checkVisibilityLimits rejects service-side ceilings before admission. An
// oversized request therefore cannot occupy a slot while valid work waits.
func checkVisibilityLimits(request *daedalusv1.ComputeVisibilityRequest) error {
	grid := request.OpacityGrid
	if grid.Width > maximumGridDimension || grid.Height > maximumGridDimension {
		return fmt.Errorf("%w: grid dimension exceeds the v1 maximum", daedalus.ErrLimitExceeded)
	}
	cellCount := uint64(grid.Width) * uint64(grid.Height)
	if cellCount > uint64(daedalus.MaxCells) {
		return fmt.Errorf("%w: cell count exceeds the v1 maximum", daedalus.ErrLimitExceeded)
	}
	if len(request.Queries) > vision.MaxQueries {
		return fmt.Errorf("%w: query count %d exceeds %d", daedalus.ErrLimitExceeded, len(request.Queries), vision.MaxQueries)
	}
	for index, query := range request.Queries {
		if query != nil && query.Radius > vision.MaxRadius {
			return fmt.Errorf("%w: query %d radius %d exceeds %d", daedalus.ErrLimitExceeded, index, query.Radius, vision.MaxRadius)
		}
	}
	return nil
}

func visibilityFromProto(request *daedalusv1.ComputeVisibilityRequest) (vision.OpacityGrid, []vision.Query, error) {
	if request.OpacityGrid == nil {
		return vision.OpacityGrid{}, nil, fmt.Errorf("%w: request must contain opacity grid", daedalus.ErrInvalidVisibility)
	}
	grid := vision.OpacityGrid{
		Width: request.OpacityGrid.Width, Height: request.OpacityGrid.Height,
		Transparent: append([]byte(nil), request.OpacityGrid.Transparent...),
	}
	queries := make([]vision.Query, len(request.Queries))
	for index, query := range request.Queries {
		if query == nil || query.Origin == nil {
			return vision.OpacityGrid{}, nil, fmt.Errorf("%w: query %d is missing origin", daedalus.ErrInvalidVisibility, index)
		}
		queries[index] = vision.Query{
			Origin: daedalus.Cell{X: query.Origin.X, Y: query.Origin.Y},
			Radius: query.Radius,
		}
	}
	return grid, queries, nil
}

func visibilityToProto(fields []vision.Field) *daedalusv1.ComputeVisibilityResponse {
	response := &daedalusv1.ComputeVisibilityResponse{Fields: make([]*daedalusv1.VisibilityField, len(fields))}
	for index, field := range fields {
		response.Fields[index] = &daedalusv1.VisibilityField{Visible: append([]byte(nil), field.Visible...)}
	}
	return response
}

// ComputeVisibility answers one visibility field per query. The grid and
// fields belong only to this call; no observer, field, or grid is retained.
func (server *Server) ComputeVisibility(
	ctx context.Context,
	request *daedalusv1.ComputeVisibilityRequest,
) (*daedalusv1.ComputeVisibilityResponse, error) {
	if request == nil || request.OpacityGrid == nil {
		return nil, status.Error(codes.InvalidArgument, "request must contain opacity grid")
	}
	if err := checkVisibilityLimits(request); err != nil {
		return nil, StatusError(err)
	}
	grid, queries, err := visibilityFromProto(request)
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

	visibilityContext, cancel := context.WithCancel(ctx)
	stopShutdownCancellation := context.AfterFunc(server.context, cancel)
	defer func() {
		stopShutdownCancellation()
		cancel()
	}()

	fields, err := vision.Answer(visibilityContext, grid, queries)
	if err != nil {
		return nil, StatusError(err)
	}
	return visibilityToProto(fields), nil
}
