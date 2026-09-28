package pathfinding_test

import (
	"context"
	"slices"
	"testing"

	"github.com/Otoru/daedalus"
	"github.com/Otoru/daedalus/utils/pathfinding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScaleTruncatesTowardZeroAndPreservesUnreachable(t *testing.T) {
	original := pathfinding.Field{
		Width: 4, Height: 1,
		Distances: []pathfinding.Distance{5, -5, pathfinding.Unreachable, 1},
	}
	scaled, err := original.Scale(1, 2)
	require.NoError(t, err)
	assert.Equal(t, []pathfinding.Distance{2, -2, pathfinding.Unreachable, 0}, scaled.Distances)
	assert.Equal(t, []pathfinding.Distance{5, -5, pathfinding.Unreachable, 1}, original.Distances)

	inverted, err := original.Scale(-12, 10)
	require.NoError(t, err)
	assert.Equal(t, []pathfinding.Distance{-6, 6, pathfinding.Unreachable, -1}, inverted.Distances)
	assert.Equal(t, pathfinding.StepStatusBlocked, inverted.Step(cell(3, 0)).Status)

	negativeDenominator, err := pathfinding.Field{Distances: []pathfinding.Distance{5}}.Scale(1, -2)
	require.NoError(t, err)
	assert.Equal(t, pathfinding.Distance(-2), negativeDenominator.Distances[0])
}

func TestScaleRejectsAQuotientOutsideTheBand(t *testing.T) {
	original := pathfinding.Field{Width: 2, Height: 1, Distances: []pathfinding.Distance{3, 2000000000}}
	_, err := original.Scale(2, 1)
	require.ErrorIs(t, err, daedalus.ErrInvalidNavigation)
	assert.Equal(t, pathfinding.Distance(3), original.Distances[0])
	assert.Equal(t, pathfinding.Distance(2000000000), original.Distances[1])

	_, err = pathfinding.Field{Distances: []pathfinding.Distance{4}}.Scale(1, 0)
	require.ErrorIs(t, err, daedalus.ErrInvalidNavigation)
}

func TestScaleKeepsArrivalOnAPositiveFactor(t *testing.T) {
	grid := fieldGrid(3, 1, 1, 1, 1)
	field, err := pathfinding.Compute(context.Background(), grid, []pathfinding.Source{{At: cell(0, 0)}})
	require.NoError(t, err)
	scaled, err := field.Scale(2, 1)
	require.NoError(t, err)
	assert.Equal(t, pathfinding.StepStatusArrived, scaled.Step(cell(0, 0)).Status)
	assert.Equal(t, pathfinding.StepStatusMoved, scaled.Step(cell(2, 0)).Status)
	assert.Equal(t, daedalus.DirectionWest, scaled.Step(cell(2, 0)).Direction)
}

func TestRescanLowersAViolatorAndLeavesAnOptimalField(t *testing.T) {
	grid := fieldGrid(4, 1, 1, 4, 2, 3)
	optimal, err := pathfinding.Compute(context.Background(), grid, []pathfinding.Source{{At: cell(0, 0)}})
	require.NoError(t, err)
	baseline := slices.Clone(optimal.Distances)

	require.NoError(t, pathfinding.Rescan(context.Background(), &optimal, grid))
	assert.Equal(t, baseline, optimal.Distances)

	raised := optimal
	raised.Distances = slices.Clone(optimal.Distances)
	raised.Distances[2] += 9
	require.NoError(t, pathfinding.Rescan(context.Background(), &raised, grid))
	assert.Equal(t, baseline, raised.Distances)

	err = pathfinding.Rescan(context.Background(), nil, grid)
	require.ErrorIs(t, err, daedalus.ErrInvalidNavigation)
	err = pathfinding.Rescan(context.Background(), &optimal, fieldGrid(2, 2, 1, 1, 1, 1))
	require.ErrorIs(t, err, daedalus.ErrInvalidNavigation)
}

func TestFleeWithoutRescanWalksIntoTheDeadEnd(t *testing.T) {
	grid, threat, entrance, deadEnd, escape := deadEndCorridor()
	sources := []pathfinding.Source{{At: threat}}

	computed, err := pathfinding.Compute(context.Background(), grid, sources)
	require.NoError(t, err)
	inverted, err := computed.Scale(-12, 10)
	require.NoError(t, err)
	into := inverted.Step(entrance)
	if into.Direction != daedalus.DirectionSouth || into.Status != pathfinding.StepStatusMoved {
		t.Fatalf("inversion alone at entrance: status %d direction %d distance %d", into.Status, into.Direction, into.Distance)
	}
	intoDeadEnd, arrived := inverted.Path(entrance, nil)
	require.False(t, arrived)
	require.NotEmpty(t, intoDeadEnd)
	assert.Equal(t, deadEnd, intoDeadEnd[len(intoDeadEnd)-1])

	fled, err := pathfinding.Compute(context.Background(), grid, sources)
	require.NoError(t, err)
	require.NoError(t, pathfinding.Flee(context.Background(), &fled, grid, -12, 10))
	out := fled.Step(entrance)
	if out.Direction != daedalus.DirectionNorth || out.Status != pathfinding.StepStatusMoved {
		t.Fatalf("flee at entrance: status %d direction %d distance %d; dead end %d escape %d entrance %d",
			out.Status, out.Direction, out.Distance, fled.DistanceAt(deadEnd), fled.DistanceAt(escape), fled.DistanceAt(entrance))
	}
	around, arrived := fled.Path(entrance, nil)
	require.False(t, arrived)
	assert.NotContains(t, around, deadEnd)
	require.NotEmpty(t, around)
	assert.Equal(t, escape, around[len(around)-1])
	assert.Equal(t, pathfinding.StepStatusBlocked, fled.Step(escape).Status)

	composed, err := computed.Scale(-12, 10)
	require.NoError(t, err)
	require.NoError(t, pathfinding.Rescan(context.Background(), &composed, grid))
	assert.Equal(t, fled.Distances, composed.Distances)
}

func TestFleeLeavesTheReceiverUntouchedWhenScaleFails(t *testing.T) {
	field := pathfinding.Field{Width: 1, Height: 1, Distances: []pathfinding.Distance{2000000000}}
	before := slices.Clone(field.Distances)
	err := pathfinding.Flee(context.Background(), &field, fieldGrid(1, 1, 1), 3, 1)
	require.ErrorIs(t, err, daedalus.ErrInvalidNavigation)
	assert.Equal(t, before, field.Distances)
}

func deadEndCorridor() (pathfinding.CostGrid, daedalus.Cell, daedalus.Cell, daedalus.Cell, daedalus.Cell) {
	const width, height uint32 = 31, 3
	const branchX = 26
	costs := make([]pathfinding.Cost, int(width*height))
	for x := uint32(0); x < width; x++ {
		costs[x] = pathfinding.MinCost
	}
	costs[int(width)+branchX] = pathfinding.MinCost
	costs[int(2*width)+branchX] = pathfinding.MinCost
	grid := pathfinding.CostGrid{Width: width, Height: height, Costs: costs}
	return grid, cell(30, 0), cell(branchX, 1), cell(branchX, 2), cell(0, 0)
}
