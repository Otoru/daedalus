package service

import (
	"fmt"

	"github.com/Otoru/daedalus"
	daedalusv1 "github.com/Otoru/daedalus/internal/gen/go/daedalus/v1"
	"github.com/Otoru/daedalus/utils/pathfinding"
)

// maxInt32AsUint32 is the largest uint32 that fits in an int32. A wider wire
// value has to be rejected: narrowing it would wrap into a negative number.
const maxInt32AsUint32 = uint32(1<<31 - 1)

// checkNavigationLimits rejects a grid or a query batch above the v1 ceilings
// before a slot is taken. An oversized request therefore fails even when
// admission has already stopped. Nothing is truncated to fit.
func checkNavigationLimits(request *daedalusv1.ComputeStepsRequest) error {
	grid := request.CostGrid
	if grid.Width > maximumGridDimension || grid.Height > maximumGridDimension {
		return fmt.Errorf("%w: grid dimension exceeds the v1 maximum", daedalus.ErrLimitExceeded)
	}
	cellCount := uint64(grid.Width) * uint64(grid.Height)
	if cellCount > uint64(daedalus.MaxCells) {
		return fmt.Errorf("%w: cell count exceeds the v1 maximum", daedalus.ErrLimitExceeded)
	}
	if len(request.Queries) > pathfinding.MaxQueries {
		return fmt.Errorf("%w: query count %d exceeds %d", daedalus.ErrLimitExceeded, len(request.Queries), pathfinding.MaxQueries)
	}
	var steps int64
	for index, query := range request.Queries {
		if query == nil {
			continue
		}
		if len(query.Sources) > pathfinding.MaxSources {
			return fmt.Errorf("%w: query %d source count %d exceeds %d", daedalus.ErrLimitExceeded, index, len(query.Sources), pathfinding.MaxSources)
		}
		steps += int64(len(query.Positions))
		if steps > int64(pathfinding.MaxStepsPerCall) {
			return fmt.Errorf("%w: step count %d exceeds %d", daedalus.ErrLimitExceeded, steps, pathfinding.MaxStepsPerCall)
		}
	}
	return nil
}

func navigationFromProto(request *daedalusv1.ComputeStepsRequest) (pathfinding.CostGrid, []pathfinding.Query, error) {
	grid, err := costGridFromProto(request.CostGrid)
	if err != nil {
		return pathfinding.CostGrid{}, nil, err
	}
	queries, err := queriesFromProto(request.Queries)
	if err != nil {
		return pathfinding.CostGrid{}, nil, err
	}
	return grid, queries, nil
}

// costGridFromProto copies costs only when the payload length is exactly
// width × height. A short slice would otherwise read as a partly walled map.
// Each entry is copied as a Cost, which is a uint8, so the 0..255 domain the
// bytes wire type already enforces is not widened.
func costGridFromProto(source *daedalusv1.CostGrid) (pathfinding.CostGrid, error) {
	product := uint64(source.Width) * uint64(source.Height)
	if uint64(len(source.Costs)) != product {
		return pathfinding.CostGrid{}, fmt.Errorf(
			"%w: CostGrid costs length %d disagrees with width %d height %d",
			daedalus.ErrInvalidNavigation, len(source.Costs), source.Width, source.Height,
		)
	}
	costs := make([]pathfinding.Cost, len(source.Costs))
	for index, entry := range source.Costs {
		costs[index] = pathfinding.Cost(entry)
	}
	return pathfinding.CostGrid{Width: source.Width, Height: source.Height, Costs: costs}, nil
}

func queriesFromProto(sources []*daedalusv1.Query) ([]pathfinding.Query, error) {
	queries := make([]pathfinding.Query, len(sources))
	for index, source := range sources {
		converted, err := queryFromProto(index, source)
		if err != nil {
			return nil, err
		}
		queries[index] = converted
	}
	return queries, nil
}

func queryFromProto(index int, source *daedalusv1.Query) (pathfinding.Query, error) {
	if source == nil {
		return pathfinding.Query{}, fmt.Errorf("%w: query %d is nil", daedalus.ErrInvalidNavigation, index)
	}
	sources := make([]pathfinding.Source, len(source.Sources))
	for sourceIndex, wire := range source.Sources {
		converted, err := sourceFromProto(index, sourceIndex, wire)
		if err != nil {
			return pathfinding.Query{}, err
		}
		sources[sourceIndex] = converted
	}
	positions := make([]daedalus.Cell, len(source.Positions))
	for positionIndex, cell := range source.Positions {
		if cell == nil {
			return pathfinding.Query{}, fmt.Errorf(
				"%w: query %d position %d is nil", daedalus.ErrInvalidNavigation, index, positionIndex,
			)
		}
		positions[positionIndex] = daedalus.Cell{X: cell.X, Y: cell.Y}
	}
	denominator, err := fleeDenominatorFromProto(index, source)
	if err != nil {
		return pathfinding.Query{}, err
	}
	return pathfinding.Query{
		Sources:         sources,
		Positions:       positions,
		Flee:            source.Flee,
		FleeNumerator:   source.GetFleeNumerator(),
		FleeDenominator: denominator,
	}, nil
}

// sourceFromProto keeps bias inside Distance. The wire value is a uint32 and
// Distance is an int32; a value above MaxDistance is rejected instead of
// truncated, because truncation would wrap a large bias into a negative one.
func sourceFromProto(queryIndex, sourceIndex int, wire *daedalusv1.Source) (pathfinding.Source, error) {
	if wire == nil || wire.At == nil {
		return pathfinding.Source{}, fmt.Errorf(
			"%w: query %d source %d is missing a cell", daedalus.ErrInvalidNavigation, queryIndex, sourceIndex,
		)
	}
	if wire.Bias > uint32(pathfinding.MaxDistance) {
		return pathfinding.Source{}, fmt.Errorf(
			"%w: query %d source %d bias %d is outside 0..%d",
			daedalus.ErrInvalidNavigation, queryIndex, sourceIndex, wire.Bias, pathfinding.MaxDistance,
		)
	}
	return pathfinding.Source{
		At:   daedalus.Cell{X: wire.At.X, Y: wire.At.Y},
		Bias: pathfinding.Distance(wire.Bias),
	}, nil
}

// fleeDenominatorFromProto narrows the optional uint32 onto the int32 the
// field expects. Zero, including an absent field, stays zero so the algorithm
// can substitute its default. A value that does not fit in int32 is rejected
// rather than wrapped.
func fleeDenominatorFromProto(index int, source *daedalusv1.Query) (int32, error) {
	raw := source.GetFleeDenominator()
	if raw > maxInt32AsUint32 {
		return 0, fmt.Errorf(
			"%w: query %d flee denominator %d is outside the int32 range",
			daedalus.ErrInvalidNavigation, index, raw,
		)
	}
	return int32(raw), nil
}

func stepsToProto(results [][]pathfinding.StepResult) *daedalusv1.ComputeStepsResponse {
	response := &daedalusv1.ComputeStepsResponse{
		Results: make([]*daedalusv1.QueryResult, len(results)),
	}
	for index, steps := range results {
		converted := &daedalusv1.QueryResult{Steps: make([]*daedalusv1.Step, len(steps))}
		for stepIndex, step := range steps {
			converted.Steps[stepIndex] = stepToProto(step)
		}
		response.Results[index] = converted
	}
	return response
}

// stepToProto reports a direction only for a move. The Go zero Direction is
// North, so copying it onto an arrival, a hold, an unreachable cell, or a
// cell outside the grid would claim a step north that the read did not take.
func stepToProto(step pathfinding.StepResult) *daedalusv1.Step {
	direction := daedalusv1.Direction_DIRECTION_UNSPECIFIED
	if step.Status == pathfinding.StepStatusMoved {
		direction = mapDirectionToProto(step.Direction)
	}
	return &daedalusv1.Step{
		Direction: direction,
		Distance:  int32(step.Distance),
		Status:    mapStepStatusToProto(step.Status),
	}
}

// mapStepStatus decodes a wire outcome. Proto reserves 0 for unspecified,
// while the Go constants start at 0 with Moved, so the two enums are not the
// same integer. The switch is the mapping; subtracting one would track today's
// numbering and break as soon as a constant is inserted.
func mapStepStatus(source daedalusv1.StepStatus) (pathfinding.StepStatus, error) {
	switch source {
	case daedalusv1.StepStatus_STEP_STATUS_MOVED:
		return pathfinding.StepStatusMoved, nil
	case daedalusv1.StepStatus_STEP_STATUS_ARRIVED:
		return pathfinding.StepStatusArrived, nil
	case daedalusv1.StepStatus_STEP_STATUS_UNREACHABLE:
		return pathfinding.StepStatusUnreachable, nil
	case daedalusv1.StepStatus_STEP_STATUS_OUTSIDE:
		return pathfinding.StepStatusOutside, nil
	case daedalusv1.StepStatus_STEP_STATUS_BLOCKED:
		return pathfinding.StepStatusBlocked, nil
	default:
		return 0, fmt.Errorf("%w: step status %d is unspecified", daedalus.ErrInvalidNavigation, source)
	}
}

// mapStepStatusToProto encodes a step outcome onto the wire. See mapStepStatus
// for why the values differ by the unspecified slot and why this is a switch.
func mapStepStatusToProto(source pathfinding.StepStatus) daedalusv1.StepStatus {
	switch source {
	case pathfinding.StepStatusMoved:
		return daedalusv1.StepStatus_STEP_STATUS_MOVED
	case pathfinding.StepStatusArrived:
		return daedalusv1.StepStatus_STEP_STATUS_ARRIVED
	case pathfinding.StepStatusUnreachable:
		return daedalusv1.StepStatus_STEP_STATUS_UNREACHABLE
	case pathfinding.StepStatusOutside:
		return daedalusv1.StepStatus_STEP_STATUS_OUTSIDE
	case pathfinding.StepStatusBlocked:
		return daedalusv1.StepStatus_STEP_STATUS_BLOCKED
	default:
		return daedalusv1.StepStatus_STEP_STATUS_UNSPECIFIED
	}
}
