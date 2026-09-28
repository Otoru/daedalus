package pathfinding_test

import (
	"context"
	"testing"

	"github.com/Otoru/daedalus"
	"github.com/Otoru/daedalus/utils/pathfinding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStepTieBreakFollowsNorthEastSouthWest(t *testing.T) {
	unreachable := pathfinding.Unreachable
	tests := []struct {
		name      string
		distances []pathfinding.Distance
		want      daedalus.Direction
	}{
		{
			name: "north east south and west",
			distances: []pathfinding.Distance{
				unreachable, 1, unreachable,
				1, 5, 1,
				unreachable, 1, unreachable,
			},
			want: daedalus.DirectionNorth,
		},
		{
			name: "east south and west",
			distances: []pathfinding.Distance{
				unreachable, unreachable, unreachable,
				1, 5, 1,
				unreachable, 1, unreachable,
			},
			want: daedalus.DirectionEast,
		},
		{
			name: "south and west",
			distances: []pathfinding.Distance{
				unreachable, unreachable, unreachable,
				1, 5, unreachable,
				unreachable, 1, unreachable,
			},
			want: daedalus.DirectionSouth,
		},
		{
			name: "west only",
			distances: []pathfinding.Distance{
				unreachable, unreachable, unreachable,
				1, 5, unreachable,
				unreachable, unreachable, unreachable,
			},
			want: daedalus.DirectionWest,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			field := pathfinding.Field{Width: 3, Height: 3, Distances: test.distances}
			got := field.Step(cell(1, 1))
			assert.Equal(t, pathfinding.StepStatusMoved, got.Status)
			assert.Equal(t, test.want, got.Direction)
			assert.Equal(t, pathfinding.Distance(1), got.Distance)
		})
	}
}

func TestReversedScanDisagreesOnFourWayTie(t *testing.T) {
	unreachable := pathfinding.Unreachable
	field := pathfinding.Field{Width: 3, Height: 3, Distances: []pathfinding.Distance{
		unreachable, 1, unreachable,
		1, 5, 1,
		unreachable, 1, unreachable,
	}}
	from := cell(1, 1)
	forward := field.Step(from)
	reverse := reverseScan(field, from)

	assert.Equal(t, daedalus.DirectionNorth, forward.Direction)
	assert.Equal(t, daedalus.DirectionWest, reverse)
	assert.NotEqual(t, reverse, forward.Direction)
}

func TestStepOnImpassableCellStillDescends(t *testing.T) {
	open := fieldGrid(3, 3, makeUniformCosts(9, pathfinding.MinCost)...)
	terrain := open.Clone()
	creature := cell(1, 1)
	require.True(t, terrain.Set(creature, pathfinding.CostImpassable))
	assert.Equal(t, pathfinding.MinCost, open.At(creature), "the static terrain stays open")

	field, err := pathfinding.Compute(context.Background(), terrain, []pathfinding.Source{{At: cell(0, 0)}})
	require.NoError(t, err)
	assert.Equal(t, pathfinding.Unreachable, field.DistanceAt(creature))

	got := field.Step(creature)
	assert.Equal(t, pathfinding.StepStatusMoved, got.Status)
	assert.NotEqual(t, pathfinding.StepStatusUnreachable, got.Status)
	assert.Equal(t, daedalus.DirectionNorth, got.Direction)
	assert.Equal(t, pathfinding.Distance(1), got.Distance)

	walled := fieldGrid(3, 1, pathfinding.MinCost, pathfinding.CostImpassable, pathfinding.CostImpassable)
	walledField, err := pathfinding.Compute(context.Background(), walled, []pathfinding.Source{{At: cell(0, 0)}})
	require.NoError(t, err)
	alone := walledField.Step(cell(2, 0))
	assert.Equal(t, pathfinding.StepStatusUnreachable, alone.Status)
	beside := walledField.Step(cell(1, 0))
	assert.Equal(t, pathfinding.StepStatusMoved, beside.Status)
	assert.Equal(t, daedalus.DirectionWest, beside.Direction)
}

func TestBlockedIsTheFleeSafePocketAndNeverAComputedField(t *testing.T) {
	grid, threat, _, _, escape := deadEndCorridor()
	computed, err := pathfinding.Compute(context.Background(), grid, []pathfinding.Source{{At: threat}})
	require.NoError(t, err)
	for y := uint32(0); y < grid.Height; y++ {
		for x := uint32(0); x < grid.Width; x++ {
			at := cell(int32(x), int32(y))
			got := computed.Step(at)
			assert.NotEqual(t, pathfinding.StepStatusBlocked, got.Status, "computed field at %v", at)
		}
	}
	assert.Equal(t, pathfinding.StepStatusArrived, computed.Step(threat).Status)
	assert.Equal(t, pathfinding.StepStatusMoved, computed.Step(escape).Status)

	require.NoError(t, pathfinding.Flee(context.Background(), &computed, grid, -12, 10))
	pocket := computed.Step(escape)
	assert.Equal(t, pathfinding.StepStatusBlocked, pocket.Status)
	assert.NotEqual(t, pathfinding.Unreachable, pocket.Distance)
	assert.Equal(t, pathfinding.StepStatusMoved, computed.Step(threat).Status)
}

func TestPathTerminatesOnASource(t *testing.T) {
	grid := fieldGrid(5, 1, makeUniformCosts(5, pathfinding.MinCost)...)
	goal := cell(4, 0)
	field, err := pathfinding.Compute(context.Background(), grid, []pathfinding.Source{{At: goal}})
	require.NoError(t, err)

	path, arrived := field.Path(cell(0, 0), nil)
	require.True(t, arrived)
	require.LessOrEqual(t, len(path), len(grid.Costs))
	assert.Equal(t, cell(0, 0), path[0])
	assert.Equal(t, goal, path[len(path)-1])
	assert.Equal(t, pathfinding.StepStatusArrived, field.Step(path[len(path)-1]).Status)

	already, arrived := field.Path(goal, nil)
	require.True(t, arrived)
	assert.Equal(t, []daedalus.Cell{goal}, already)

	split := fieldGrid(3, 1, pathfinding.MinCost, pathfinding.CostImpassable, pathfinding.MinCost)
	strandedField, err := pathfinding.Compute(context.Background(), split, []pathfinding.Source{{At: cell(0, 0)}})
	require.NoError(t, err)
	stranded, arrived := strandedField.Path(cell(2, 0), nil)
	require.False(t, arrived)
	require.NotEmpty(t, stranded)
	assert.NotEqual(t, cell(0, 0), stranded[len(stranded)-1])
}

func TestRouteFollowsTheFieldAndReportsTheEntryCostShift(t *testing.T) {
	grid := fieldGrid(4, 1, 2, 5, 7, 11)
	from := cell(0, 0)
	to := cell(3, 0)
	path, err := pathfinding.Route(context.Background(), grid, from, to, nil)
	require.NoError(t, err)
	assert.Equal(t, []daedalus.Cell{cell(0, 0), cell(1, 0), cell(2, 0), cell(3, 0)}, path)

	sourcedAtTo, err := pathfinding.Compute(context.Background(), grid, []pathfinding.Source{{At: to}})
	require.NoError(t, err)
	sourcedAtFrom, err := pathfinding.Compute(context.Background(), grid, []pathfinding.Source{{At: from}})
	require.NoError(t, err)
	shift := pathfinding.Distance(grid.At(to)) - pathfinding.Distance(grid.At(from))
	assert.Equal(t, shift, sourcedAtFrom.DistanceAt(to)-sourcedAtTo.DistanceAt(from))

	_, err = pathfinding.Route(context.Background(), grid, cell(0, 1), to, nil)
	require.ErrorIs(t, err, daedalus.ErrInvalidNavigation)

	blocked := fieldGrid(1, 3, pathfinding.MinCost, pathfinding.CostImpassable, pathfinding.MinCost)
	_, err = pathfinding.Route(context.Background(), blocked, cell(2, 0), cell(0, 0), nil)
	require.ErrorIs(t, err, daedalus.ErrInvalidNavigation)
}

func TestStepOutsideTheField(t *testing.T) {
	field, err := pathfinding.Compute(context.Background(), fieldGrid(2, 2, 1, 1, 1, 1), []pathfinding.Source{{At: cell(0, 0)}})
	require.NoError(t, err)
	for _, at := range []daedalus.Cell{cell(-1, 0), cell(0, -1), cell(2, 0), cell(0, 2)} {
		got := field.Step(at)
		assert.Equal(t, pathfinding.StepStatusOutside, got.Status, "%v", at)
		assert.Equal(t, pathfinding.Unreachable, got.Distance, "%v", at)
	}
}

func TestStepsReusesItsBuffer(t *testing.T) {
	field, err := pathfinding.Compute(context.Background(), fieldGrid(3, 1, 1, 1, 1), []pathfinding.Source{{At: cell(2, 0)}})
	require.NoError(t, err)
	positions := []daedalus.Cell{cell(0, 0), cell(1, 0)}
	dst := make([]pathfinding.StepResult, 0, len(positions))
	allocations := testing.AllocsPerRun(50, func() {
		dst = field.Steps(positions, dst)
	})
	assert.Zero(t, allocations)
	require.Len(t, dst, 2)
	assert.Equal(t, pathfinding.StepStatusMoved, dst[0].Status)
}

func reverseScan(field pathfinding.Field, from daedalus.Cell) daedalus.Direction {
	self := field.DistanceAt(from)
	best := daedalus.DirectionWest
	bestDistance := pathfinding.Distance(0)
	found := false
	order := []daedalus.Direction{
		daedalus.DirectionWest,
		daedalus.DirectionSouth,
		daedalus.DirectionEast,
		daedalus.DirectionNorth,
	}
	for _, direction := range order {
		delta := direction.Delta()
		neighbor := cell(from.X+delta.X, from.Y+delta.Y)
		distance := field.DistanceAt(neighbor)
		if distance == pathfinding.Unreachable {
			continue
		}
		if self != pathfinding.Unreachable && distance >= self {
			continue
		}
		if !found || distance < bestDistance {
			found = true
			bestDistance = distance
			best = direction
		}
	}
	return best
}
