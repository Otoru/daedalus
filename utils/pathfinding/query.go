package pathfinding

import (
	"context"
	"fmt"

	"github.com/Otoru/daedalus"
)

// Query is one goal set and the positions that read it. Flee, when set,
// runs Flee on that field before the positions are read. A zero
// FleeNumerator means -12 and a zero FleeDenominator means 10, independently;
// that is the factor that gives the rescan a corridor to propagate.
type Query struct {
	Sources         []Source
	Positions       []daedalus.Cell
	Flee            bool
	FleeNumerator   int32
	FleeDenominator int32
}

// Answer reads every query against one cost grid and returns one result
// slice per query, in query order. The grid is the expensive part of a turn
// and it travels once however many goal sets the turn has. One scratch field
// is reused across the queries.
//
// MaxQueries, MaxSources per query, and MaxStepsPerCall as the sum of every
// query's positions are ErrLimitExceeded. Those ceilings are checked before
// any result slice or scratch buffer is allocated.
func Answer(ctx context.Context, grid CostGrid, queries []Query) ([][]StepResult, error) {
	if len(queries) > MaxQueries {
		return nil, fmt.Errorf("%w: query count %d exceeds %d", daedalus.ErrLimitExceeded, len(queries), MaxQueries)
	}
	var steps int64
	for index, query := range queries {
		if len(query.Sources) > MaxSources {
			return nil, fmt.Errorf("%w: query %d source count %d exceeds %d", daedalus.ErrLimitExceeded, index, len(query.Sources), MaxSources)
		}
		steps += int64(len(query.Positions))
		if steps > int64(MaxStepsPerCall) {
			return nil, fmt.Errorf("%w: step count %d exceeds %d", daedalus.ErrLimitExceeded, steps, MaxStepsPerCall)
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(queries) == 0 {
		return [][]StepResult{}, nil
	}

	results := make([][]StepResult, len(queries))
	var scratch Field
	for index, query := range queries {
		if err := ComputeInto(ctx, &scratch, grid, query.Sources); err != nil {
			return nil, err
		}
		if query.Flee {
			numerator, denominator := fleeRatio(query)
			if err := Flee(ctx, &scratch, grid, numerator, denominator); err != nil {
				return nil, err
			}
		}
		results[index] = scratch.Steps(query.Positions, nil)
	}
	return results, nil
}

func fleeRatio(query Query) (int32, int32) {
	numerator := query.FleeNumerator
	denominator := query.FleeDenominator
	if numerator == 0 {
		numerator = defaultFleeNumerator
	}
	if denominator == 0 {
		denominator = defaultFleeDenominator
	}
	return numerator, denominator
}
