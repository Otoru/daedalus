package pathfinding_test

import (
	"context"
	"testing"

	"github.com/Otoru/daedalus"
	"github.com/Otoru/daedalus/utils/pathfinding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnswerEnforcesTheAggregateStepCap(t *testing.T) {
	ctx := context.Background()
	invalid := pathfinding.CostGrid{}

	_, err := pathfinding.Answer(ctx, invalid, make([]pathfinding.Query, pathfinding.MaxQueries+1))
	require.ErrorIs(t, err, daedalus.ErrLimitExceeded)
	assert.NotErrorIs(t, err, daedalus.ErrInvalidNavigation)

	_, err = pathfinding.Answer(ctx, invalid, []pathfinding.Query{{
		Sources: make([]pathfinding.Source, pathfinding.MaxSources+1),
	}})
	require.ErrorIs(t, err, daedalus.ErrLimitExceeded)
	assert.NotErrorIs(t, err, daedalus.ErrInvalidNavigation)

	_, err = pathfinding.Answer(ctx, invalid, []pathfinding.Query{
		{Positions: make([]daedalus.Cell, pathfinding.MaxStepsPerCall)},
		{Positions: []daedalus.Cell{cell(0, 0)}},
	})
	require.ErrorIs(t, err, daedalus.ErrLimitExceeded)
	assert.NotErrorIs(t, err, daedalus.ErrInvalidNavigation)

	grid := fieldGrid(1, 1, pathfinding.MinCost)
	results, err := pathfinding.Answer(ctx, grid, []pathfinding.Query{{
		Sources:   []pathfinding.Source{{At: cell(0, 0)}},
		Positions: make([]daedalus.Cell, pathfinding.MaxStepsPerCall),
	}})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Len(t, results[0], pathfinding.MaxStepsPerCall)
	assert.Equal(t, pathfinding.StepStatusArrived, results[0][0].Status)
}

func TestAnswerReusesOneScratchField(t *testing.T) {
	const side = 24
	grid := fieldGrid(side, side, makeUniformCosts(side*side, pathfinding.MinCost)...)
	query := pathfinding.Query{
		Sources:   []pathfinding.Source{{At: cell(0, 0)}},
		Positions: []daedalus.Cell{cell(side-1, side-1)},
	}
	one := []pathfinding.Query{query}
	many := []pathfinding.Query{query, query, query, query, query, query, query, query}
	ctx := context.Background()
	_, err := pathfinding.Answer(ctx, grid, many)
	require.NoError(t, err)

	single := testing.AllocsPerRun(30, func() {
		if _, callErr := pathfinding.Answer(ctx, grid, one); callErr != nil {
			panic(callErr)
		}
	})
	multi := testing.AllocsPerRun(30, func() {
		if _, callErr := pathfinding.Answer(ctx, grid, many); callErr != nil {
			panic(callErr)
		}
	})
	t.Logf("Answer allocations: one query %.0f, eight queries %.0f", single, multi)
	// The scratch field is allocated once per Answer. Each extra query adds
	// its result slice, so eight queries sit a small additive gap above one
	// query. Eight fresh fields would land near eight times the one-query cost.
	assert.Greater(t, multi, single)
	assert.Less(t, multi, single+16)
	assert.Less(t, multi, single*4)
}

func TestAnswerPreservesQueryOrderAndFleeDefaults(t *testing.T) {
	grid := fieldGrid(5, 1, 1, 1, 1, 1, 1)
	ctx := context.Background()
	results, err := pathfinding.Answer(ctx, grid, []pathfinding.Query{
		{Sources: []pathfinding.Source{{At: cell(0, 0)}}, Positions: []daedalus.Cell{cell(2, 0)}},
		{Sources: []pathfinding.Source{{At: cell(4, 0)}}, Positions: []daedalus.Cell{cell(2, 0)}},
	})
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, daedalus.DirectionWest, results[0][0].Direction)
	assert.Equal(t, daedalus.DirectionEast, results[1][0].Direction)

	implicit, err := pathfinding.Answer(ctx, grid, []pathfinding.Query{{
		Sources:   []pathfinding.Source{{At: cell(4, 0)}},
		Positions: []daedalus.Cell{cell(1, 0)},
		Flee:      true,
	}})
	require.NoError(t, err)
	explicit, err := pathfinding.Answer(ctx, grid, []pathfinding.Query{{
		Sources:         []pathfinding.Source{{At: cell(4, 0)}},
		Positions:       []daedalus.Cell{cell(1, 0)},
		Flee:            true,
		FleeNumerator:   -12,
		FleeDenominator: 10,
	}})
	require.NoError(t, err)
	assert.Equal(t, explicit, implicit)
	assert.Equal(t, pathfinding.StepStatusMoved, implicit[0][0].Status)
	assert.Equal(t, daedalus.DirectionWest, implicit[0][0].Direction)

	empty, err := pathfinding.Answer(ctx, grid, nil)
	require.NoError(t, err)
	assert.Empty(t, empty)

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = pathfinding.Answer(canceled, grid, []pathfinding.Query{{
		Sources: []pathfinding.Source{{At: cell(0, 0)}},
	}})
	require.ErrorIs(t, err, context.Canceled)
}
