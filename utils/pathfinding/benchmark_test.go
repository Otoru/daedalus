package pathfinding

import (
	"context"
	"testing"

	"github.com/Otoru/daedalus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type computeStepLoad struct {
	name      string
	width     uint32
	height    uint32
	queries   int
	positions int
}

// BenchmarkComputeSteps records the three navigation loads and nothing more.
// Small is 64×64 with one query of 64 positions. Typical is 128×128 with two
// queries of 256 positions. Maximum v1 is 256×256 with sixteen queries of
// 1024 positions, which sits on MaxQueries and on the MaxStepsPerCall sum.
// Answer is the call a turn makes. An open grid settles every cell, so the
// load is the search, not a sparse maze. Nothing here asserts a latency
// figure; the measured numbers are the budget, and this machine is not a
// reference board.
func BenchmarkComputeSteps(b *testing.B) {
	for _, load := range computeStepLoads() {
		b.Run(load.name, func(b *testing.B) {
			grid, queries := load.gridAndQueries()
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				results, err := Answer(context.Background(), grid, queries)
				if err != nil {
					b.Fatal(err)
				}
				if len(results) != len(queries) {
					b.Fatalf("Answer returned %d results for %d queries", len(results), len(queries))
				}
			}
		})
	}
}

func TestComputeStepLoadsStayInsideTheCallCeilings(t *testing.T) {
	loads := computeStepLoads()
	require.Len(t, loads, 3)

	assert.Equal(t, "Small_64x64", loads[0].name)
	assert.Equal(t, uint32(64), loads[0].width)
	assert.Equal(t, uint32(64), loads[0].height)
	assert.Equal(t, 1, loads[0].queries)
	assert.Equal(t, 64, loads[0].positions)

	assert.Equal(t, "Typical_128x128", loads[1].name)
	assert.Equal(t, uint32(128), loads[1].width)
	assert.Equal(t, uint32(128), loads[1].height)
	assert.Equal(t, 2, loads[1].queries)
	assert.Equal(t, 256, loads[1].positions)

	assert.Equal(t, "Maximum_v1_256x256", loads[2].name)
	assert.Equal(t, uint32(256), loads[2].width)
	assert.Equal(t, uint32(256), loads[2].height)
	assert.Equal(t, MaxQueries, loads[2].queries)
	assert.Equal(t, MaxStepsPerCall, loads[2].queries*loads[2].positions)

	for _, load := range loads {
		assert.LessOrEqual(t, load.queries, MaxQueries)
		assert.LessOrEqual(t, load.queries*load.positions, MaxStepsPerCall)
		assert.LessOrEqual(t, load.positions, int(load.width)*int(load.height))

		grid, queries := load.gridAndQueries()
		require.NoError(t, grid.Validate())
		assert.Equal(t, load.width, grid.Width)
		assert.Equal(t, load.height, grid.Height)
		require.Len(t, queries, load.queries)
		for _, query := range queries {
			assert.Len(t, query.Sources, 1)
			assert.LessOrEqual(t, len(query.Sources), MaxSources)
			assert.Len(t, query.Positions, load.positions)
			assert.False(t, query.Flee)
		}
	}
}

func computeStepLoads() []computeStepLoad {
	return []computeStepLoad{
		{name: "Small_64x64", width: 64, height: 64, queries: 1, positions: 64},
		{name: "Typical_128x128", width: 128, height: 128, queries: 2, positions: 256},
		{name: "Maximum_v1_256x256", width: 256, height: 256, queries: 16, positions: 1024},
	}
}

func (load computeStepLoad) gridAndQueries() (CostGrid, []Query) {
	cellCount := int(load.width) * int(load.height)
	costs := make([]Cost, cellCount)
	for index := range costs {
		costs[index] = MinCost
	}
	grid := CostGrid{Width: load.width, Height: load.height, Costs: costs}

	shared := make([]daedalus.Cell, load.positions)
	for index := range shared {
		shared[index] = daedalus.Cell{X: int32(index % int(load.width)), Y: int32(index / int(load.width))}
	}
	queries := make([]Query, load.queries)
	for index := range queries {
		positions := make([]daedalus.Cell, len(shared))
		copy(positions, shared)
		queries[index] = Query{
			Sources:   []Source{{At: daedalus.Cell{X: int32(index), Y: 0}}},
			Positions: positions,
		}
	}
	return grid, queries
}
