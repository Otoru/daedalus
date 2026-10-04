package pathfinding_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/Otoru/daedalus"
	"github.com/Otoru/daedalus/utils/pathfinding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeUsesEntryCostsAndSourceBiases(t *testing.T) {
	grid := fieldGrid(5, 3,
		1, 2, 3, 4, 5,
		1, 0, 9, 0, 1,
		1, 1, 1, 1, 1,
	)
	sources := []pathfinding.Source{
		{At: cell(0, 0), Bias: 7},
		{At: cell(4, 2), Bias: 2},
	}

	field, err := pathfinding.Compute(context.Background(), grid, sources)
	require.NoError(t, err)
	assert.Equal(t, uint32(5), field.Width)
	assert.Equal(t, uint32(3), field.Height)
	assert.Equal(t, pathfinding.Distance(7), field.DistanceAt(cell(0, 0)))
	assert.Equal(t, pathfinding.Distance(2), field.DistanceAt(cell(4, 2)))
	assert.Equal(t, pathfinding.Distance(3), field.DistanceAt(cell(3, 2)))
	assert.Equal(t, pathfinding.Distance(13), field.DistanceAt(cell(2, 1)))
	assert.Equal(t, pathfinding.Unreachable, field.DistanceAt(cell(1, 1)))
	assert.Equal(t, pathfinding.Unreachable, field.DistanceAt(cell(-1, 0)))
	assert.Equal(t, pathfinding.Unreachable, field.DistanceAt(cell(5, 0)))
}

func TestComputeRejectsMalformedQueriesBeforeChangingDestination(t *testing.T) {
	validGrid := fieldGrid(2, 2, 1, 1, 1, 1)
	dirty := pathfinding.Field{Width: 9, Height: 8, Distances: []pathfinding.Distance{77}}

	tests := []struct {
		name    string
		grid    pathfinding.CostGrid
		sources []pathfinding.Source
		want    error
	}{
		{name: "invalid grid", grid: pathfinding.CostGrid{Width: 2, Height: 2}, sources: []pathfinding.Source{{At: cell(0, 0)}}, want: daedalus.ErrInvalidNavigation},
		{name: "no sources", grid: validGrid, want: daedalus.ErrInvalidNavigation},
		{name: "too many sources", grid: validGrid, sources: make([]pathfinding.Source, pathfinding.MaxSources+1), want: daedalus.ErrLimitExceeded},
		{name: "outside", grid: validGrid, sources: []pathfinding.Source{{At: cell(2, 0)}}, want: daedalus.ErrInvalidNavigation},
		{name: "impassable", grid: fieldGrid(2, 1, 1, 0), sources: []pathfinding.Source{{At: cell(1, 0)}}, want: daedalus.ErrInvalidNavigation},
		{name: "duplicate", grid: validGrid, sources: []pathfinding.Source{{At: cell(0, 0)}, {At: cell(0, 0), Bias: 1}}, want: daedalus.ErrInvalidNavigation},
		{name: "negative bias", grid: validGrid, sources: []pathfinding.Source{{At: cell(0, 0), Bias: -1}}, want: daedalus.ErrInvalidNavigation},
		{name: "large bias", grid: validGrid, sources: []pathfinding.Source{{At: cell(0, 0), Bias: pathfinding.MaxDistance + 1}}, want: daedalus.ErrInvalidNavigation},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dst := dirty
			err := pathfinding.ComputeInto(context.Background(), &dst, test.grid, test.sources)
			require.ErrorIs(t, err, test.want)
			assert.Equal(t, dirty.Width, dst.Width)
			assert.Equal(t, dirty.Height, dst.Height)
			assert.Equal(t, dirty.Distances, dst.Distances)
		})
	}

	err := pathfinding.ComputeInto(context.Background(), nil, validGrid, []pathfinding.Source{{At: cell(0, 0)}})
	require.ErrorIs(t, err, daedalus.ErrInvalidNavigation)
}

func TestComputePropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := pathfinding.Compute(ctx, fieldGrid(2, 2, 1, 1, 1, 1), []pathfinding.Source{{At: cell(0, 0)}})
	require.ErrorIs(t, err, context.Canceled)
}

func TestSourceOrderCannotChangeDistances(t *testing.T) {
	grid := fieldGrid(7, 5,
		1, 1, 1, 2, 1, 1, 1,
		1, 0, 3, 4, 5, 0, 1,
		1, 1, 1, 7, 1, 1, 1,
		2, 0, 1, 2, 1, 0, 2,
		1, 1, 1, 1, 1, 1, 1,
	)
	sources := []pathfinding.Source{
		{At: cell(0, 0), Bias: 13},
		{At: cell(6, 0), Bias: 3},
		{At: cell(0, 4), Bias: 8},
		{At: cell(6, 4), Bias: 21},
	}

	var baseline []pathfinding.Distance
	permutations := 0
	permuteSources(sources, func(permutation []pathfinding.Source) {
		field, err := pathfinding.Compute(context.Background(), grid, permutation)
		require.NoError(t, err)
		if baseline == nil {
			baseline = slices.Clone(field.Distances)
		} else {
			require.Equal(t, baseline, field.Distances)
		}
		permutations++
	})
	assert.Equal(t, 24, permutations)
}

func TestComputeMatchesBellmanFordOracle(t *testing.T) {
	const width, height = uint32(32), uint32(32)
	costs := make([]pathfinding.Cost, int(width*height))
	for y := uint32(0); y < height; y++ {
		for x := uint32(0); x < width; x++ {
			row := int64(y) * int64(width)
			index := row + int64(x)
			costs[index] = pathfinding.Cost(1 + (x*17+y*31)%uint32(pathfinding.MaxCost))
			if (x*7+y*11)%23 == 0 {
				costs[index] = pathfinding.CostImpassable
			}
		}
	}
	grid := pathfinding.CostGrid{Width: width, Height: height, Costs: costs}
	sources := []pathfinding.Source{{At: cell(1, 0), Bias: 5}, {At: cell(31, 31), Bias: 17}, {At: cell(16, 16), Bias: 2}}

	field, err := pathfinding.Compute(context.Background(), grid, sources)
	require.NoError(t, err)
	assert.Equal(t, bellmanFord(grid, sources), field.Distances)
}

func TestEntryCostSymmetryIdentity(t *testing.T) {
	uniform := fieldGrid(4, 1, 3, 3, 3, 3)
	fromLeft, err := pathfinding.Compute(context.Background(), uniform, []pathfinding.Source{{At: cell(0, 0)}})
	require.NoError(t, err)
	fromRight, err := pathfinding.Compute(context.Background(), uniform, []pathfinding.Source{{At: cell(3, 0)}})
	require.NoError(t, err)
	assert.Equal(t, fromLeft.DistanceAt(cell(3, 0)), fromRight.DistanceAt(cell(0, 0)))

	varied := fieldGrid(4, 1, 2, 5, 7, 11)
	fromLeft, err = pathfinding.Compute(context.Background(), varied, []pathfinding.Source{{At: cell(0, 0)}})
	require.NoError(t, err)
	fromRight, err = pathfinding.Compute(context.Background(), varied, []pathfinding.Source{{At: cell(3, 0)}})
	require.NoError(t, err)
	leftToRight := fromLeft.DistanceAt(cell(3, 0)) - pathfinding.Distance(varied.At(cell(3, 0)))
	rightToLeft := fromRight.DistanceAt(cell(0, 0)) - pathfinding.Distance(varied.At(cell(0, 0)))
	assert.Equal(t, leftToRight, rightToLeft)
}

func TestFieldInvariantsOnHandDrawnAndGeneratedGrids(t *testing.T) {
	handGrid := fieldGrid(6, 5,
		1, 1, 4, 1, 1, 1,
		1, 0, 2, 0, 5, 1,
		1, 1, 3, 1, 1, 1,
		2, 0, 1, 0, 7, 2,
		1, 1, 1, 1, 1, 1,
	)
	handSources := []pathfinding.Source{{At: cell(0, 0), Bias: 4}, {At: cell(5, 4), Bias: 9}}
	handField, err := pathfinding.Compute(context.Background(), handGrid, handSources)
	require.NoError(t, err)
	checkFieldInvariants(t, handGrid, handSources, handField)

	layout, err := (daedalus.Generator{}).Generate(daedalus.Config{
		Width: 128, Height: 128, Seed: 11, MinDistance: 6, MaxAttempts: 30, MaxRooms: 128,
	})
	require.NoError(t, err)
	generatedGrid := pathfinding.NewCostGrid(layout)
	require.NotEmpty(t, layout.Rooms)
	generatedSources := []pathfinding.Source{{At: layout.Rooms[0].At}}
	generatedField, err := pathfinding.Compute(context.Background(), generatedGrid, generatedSources)
	require.NoError(t, err)
	checkFieldInvariants(t, generatedGrid, generatedSources, generatedField)
}

func TestFieldInvariantCheckerReportsEveryMutation(t *testing.T) {
	grid := fieldGrid(5, 1, 1, 1, 0, 1, 1)
	sources := []pathfinding.Source{{At: cell(0, 0), Bias: 3}}
	baseline, err := pathfinding.Compute(context.Background(), grid, sources)
	require.NoError(t, err)

	tests := []struct {
		name   string
		mutate func(*pathfinding.Field)
	}{
		{name: "distance plus one", mutate: func(field *pathfinding.Field) { field.Distances[1]++ }},
		{name: "distance minus one", mutate: func(field *pathfinding.Field) { field.Distances[1]-- }},
		{name: "unreachable made finite", mutate: func(field *pathfinding.Field) { field.Distances[2] = 99 }},
		{name: "reachable made unreachable", mutate: func(field *pathfinding.Field) { field.Distances[1] = pathfinding.Unreachable }},
		{name: "source bias altered", mutate: func(field *pathfinding.Field) { field.Distances[0]++ }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutated := baseline
			mutated.Distances = slices.Clone(baseline.Distances)
			test.mutate(&mutated)
			reporter := &recordingReporter{}
			checkFieldInvariants(reporter, grid, sources, mutated)
			require.NotEmpty(t, reporter.failures)
			t.Logf("%s -> %s", test.name, reporter.failures[0])
		})
	}
}

func TestComputeIntoResetsDirtyBuffersAndAllocatesOnlyForGrowth(t *testing.T) {
	grid := fieldGrid(16, 16, makeUniformCosts(16*16, 3)...)
	sources := []pathfinding.Source{{At: cell(0, 0), Bias: 2}, {At: cell(15, 15), Bias: 7}}
	fresh, err := pathfinding.Compute(context.Background(), grid, sources)
	require.NoError(t, err)

	var reused pathfinding.Field
	require.NoError(t, pathfinding.ComputeInto(context.Background(), &reused, grid, sources))
	for index := range reused.Distances {
		reused.Distances[index] = pathfinding.Distance(index + 12345)
	}
	require.NoError(t, pathfinding.ComputeInto(context.Background(), &reused, grid, sources))
	assert.Equal(t, fresh.Distances, reused.Distances)

	allocations := testing.AllocsPerRun(100, func() {
		if err := pathfinding.ComputeInto(context.Background(), &reused, grid, sources); err != nil {
			panic(err)
		}
	})
	assert.Zero(t, allocations)
	t.Logf("ComputeInto allocations after warm-up: %.0f", allocations)
}

// A fresh field may allocate its distance buffer, stamp buffers, and the
// queue's backing storage. It must not allocate once per bucket growth:
// a 64×64 open grid used to do that hundreds of times inside one search.
func TestFreshComputeIntoAllocatesBuffersNotBucketGrowth(t *testing.T) {
	const side = 64
	grid := fieldGrid(side, side, makeUniformCosts(side*side, pathfinding.MinCost)...)
	sources := []pathfinding.Source{{At: cell(0, 0)}}

	allocations := testing.AllocsPerRun(5, func() {
		var field pathfinding.Field
		if err := pathfinding.ComputeInto(context.Background(), &field, grid, sources); err != nil {
			panic(err)
		}
		if field.DistanceAt(cell(side-1, side-1)) == pathfinding.Unreachable {
			panic("fresh field did not reach the far cell")
		}
	})
	t.Logf("fresh ComputeInto allocations: %.0f", allocations)
	assert.Less(t, allocations, float64(16))
}

func TestDistanceCeilingsAreSafe(t *testing.T) {
	assert.Equal(t, pathfinding.Distance(16_711_680), pathfinding.MaxDistance)
	assert.Less(t, int64(pathfinding.MaxDistance), int64(1<<31-1))
	assert.Equal(t, int64(daedalus.MaxCells)*int64(pathfinding.MaxCost), int64(pathfinding.MaxDistance))
}

type fieldReporter interface {
	Helper()
	Errorf(string, ...any)
}

type recordingReporter struct {
	failures []string
}

func (reporter *recordingReporter) Helper() {}

func (reporter *recordingReporter) Errorf(format string, args ...any) {
	reporter.failures = append(reporter.failures, fmt.Sprintf(format, args...))
}

func checkFieldInvariants(reporter fieldReporter, grid pathfinding.CostGrid, sources []pathfinding.Source, field pathfinding.Field) {
	reporter.Helper()
	sourceByIndex := make(map[int64]pathfinding.Distance, len(sources))
	for _, source := range sources {
		index, ok := grid.Index(source.At)
		if !ok {
			reporter.Errorf("source %+v is outside", source.At)
			continue
		}
		sourceByIndex[index] = source.Bias
		if got := field.DistanceAt(source.At); got != source.Bias {
			reporter.Errorf("source %+v: got %d, want bias %d", source.At, got, source.Bias)
		}
	}

	reachable := reachableCells(grid, sources)
	for y := uint32(0); y < grid.Height; y++ {
		for x := uint32(0); x < grid.Width; x++ {
			checkFieldCell(reporter, grid, field, reachable, sourceByIndex, cell(int32(x), int32(y)))
		}
	}
}

func checkFieldCell(reporter fieldReporter, grid pathfinding.CostGrid, field pathfinding.Field, reachable map[int64]bool, sources map[int64]pathfinding.Distance, at daedalus.Cell) {
	index, _ := grid.Index(at)
	distance := field.DistanceAt(at)
	if grid.At(at) == pathfinding.CostImpassable || !reachable[index] {
		if distance != pathfinding.Unreachable {
			reporter.Errorf("unreachable %+v: got %d", at, distance)
		}
		return
	}
	if distance == pathfinding.Unreachable {
		reporter.Errorf("reachable %+v is unreachable", at)
		return
	}
	if _, source := sources[index]; source {
		return
	}
	checkFieldNeighbors(reporter, grid, field, at, distance)
}

func checkFieldNeighbors(reporter fieldReporter, grid pathfinding.CostGrid, field pathfinding.Field, at daedalus.Cell, distance pathfinding.Distance) {
	foundEquality := false
	foundDescent := false
	for _, neighbor := range cardinalNeighbors(grid, at) {
		if grid.At(neighbor) == pathfinding.CostImpassable {
			continue
		}
		neighborDistance := field.DistanceAt(neighbor)
		if neighborDistance == pathfinding.Unreachable {
			reporter.Errorf("reachable neighbours disagree at %+v and %+v", at, neighbor)
			continue
		}
		bound := neighborDistance + pathfinding.Distance(grid.At(at))
		if distance > bound {
			reporter.Errorf("Bellman upper bound at %+v: %d > %d", at, distance, bound)
		}
		if distance == bound {
			foundEquality = true
		}
		if neighborDistance < distance {
			foundDescent = true
		}
	}
	if !foundEquality {
		reporter.Errorf("Bellman equality missing at %+v", at)
	}
	if !foundDescent {
		reporter.Errorf("monotone descent missing at %+v", at)
	}
}

func reachableCells(grid pathfinding.CostGrid, sources []pathfinding.Source) map[int64]bool {
	reachable := make(map[int64]bool, len(grid.Costs))
	queue := make([]daedalus.Cell, 0, len(grid.Costs))
	for _, source := range sources {
		index, ok := grid.Index(source.At)
		if ok && !reachable[index] {
			reachable[index] = true
			queue = append(queue, source.At)
		}
	}
	for head := 0; head < len(queue); head++ {
		for _, neighbor := range cardinalNeighbors(grid, queue[head]) {
			index, _ := grid.Index(neighbor)
			if grid.At(neighbor) != pathfinding.CostImpassable && !reachable[index] {
				reachable[index] = true
				queue = append(queue, neighbor)
			}
		}
	}
	return reachable
}

func bellmanFord(grid pathfinding.CostGrid, sources []pathfinding.Source) []pathfinding.Distance {
	distances := make([]pathfinding.Distance, len(grid.Costs))
	for index := range distances {
		distances[index] = pathfinding.Unreachable
	}
	sourceIndexes := make(map[int64]bool, len(sources))
	for _, source := range sources {
		index, _ := grid.Index(source.At)
		distances[index] = source.Bias
		sourceIndexes[index] = true
	}
	changed := true
	for changed {
		changed = false
		for y := uint32(0); y < grid.Height; y++ {
			for x := uint32(0); x < grid.Width; x++ {
				if relaxBellmanCell(grid, distances, sourceIndexes, cell(int32(x), int32(y))) {
					changed = true
				}
			}
		}
	}
	return distances
}

func relaxBellmanCell(grid pathfinding.CostGrid, distances []pathfinding.Distance, sourceIndexes map[int64]bool, at daedalus.Cell) bool {
	index, _ := grid.Index(at)
	if sourceIndexes[index] || grid.At(at) == pathfinding.CostImpassable {
		return false
	}
	changed := false
	for _, neighbor := range cardinalNeighbors(grid, at) {
		neighborDistance := distancesAt(grid, distances, neighbor)
		if neighborDistance == pathfinding.Unreachable {
			continue
		}
		candidate := neighborDistance + pathfinding.Distance(grid.At(at))
		if distances[index] == pathfinding.Unreachable || candidate < distances[index] {
			distances[index] = candidate
			changed = true
		}
	}
	return changed
}

func distancesAt(grid pathfinding.CostGrid, distances []pathfinding.Distance, at daedalus.Cell) pathfinding.Distance {
	index, ok := grid.Index(at)
	if !ok {
		return pathfinding.Unreachable
	}
	return distances[index]
}

func cardinalNeighbors(grid pathfinding.CostGrid, at daedalus.Cell) []daedalus.Cell {
	neighbors := make([]daedalus.Cell, 0, 4)
	for direction := daedalus.DirectionNorth; direction <= daedalus.DirectionWest; direction++ {
		delta := direction.Delta()
		neighbor := cell(at.X+delta.X, at.Y+delta.Y)
		if _, ok := grid.Index(neighbor); ok {
			neighbors = append(neighbors, neighbor)
		}
	}
	return neighbors
}

func permuteSources(sources []pathfinding.Source, visit func([]pathfinding.Source)) {
	working := slices.Clone(sources)
	var walk func(int)
	walk = func(at int) {
		if at == len(working) {
			visit(working)
			return
		}
		for index := at; index < len(working); index++ {
			working[at], working[index] = working[index], working[at]
			walk(at + 1)
			working[at], working[index] = working[index], working[at]
		}
	}
	walk(0)
}

func fieldGrid(width, height uint32, costs ...pathfinding.Cost) pathfinding.CostGrid {
	return pathfinding.CostGrid{Width: width, Height: height, Costs: costs}
}

func makeUniformCosts(count int, cost pathfinding.Cost) []pathfinding.Cost {
	costs := make([]pathfinding.Cost, count)
	for index := range costs {
		costs[index] = cost
	}
	return costs
}

func cell(x, y int32) daedalus.Cell {
	return daedalus.Cell{X: x, Y: y}
}
