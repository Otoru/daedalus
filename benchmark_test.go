package daedalus

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type generationBenchmark struct {
	name   string
	config Config
}

// BenchmarkGenerate records the three runtime loads and nothing more. Small is
// 64×64 with MinDistance 6, MaxAttempts 30 and MaxRooms 128. Typical is 128×128
// with the same distance and attempts and MaxRooms 256. Maximum v1 is 256×256
// with MinDistance 1, MaxAttempts 1024, MaxRooms 256, dynamic RoomGeometry, a
// gap, cycles, roles and density regions. Nothing here asserts p95 or p99, and
// this machine is not the reference hardware of the release SLA.
func BenchmarkGenerate(b *testing.B) {
	for _, benchmark := range generationBenchmarks() {
		b.Run(benchmark.name, func(b *testing.B) {
			generator := Generator{}
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				layout, err := generator.Generate(benchmark.config)
				if err != nil {
					b.Fatal(err)
				}
				if len(layout.Rooms) == 0 {
					b.Fatal("Generate returned no Rooms")
				}
			}
		})
	}
}

func generationBenchmarks() []generationBenchmark {
	return []generationBenchmark{
		{
			name: "Small_seed_3",
			config: Config{
				Width: 64, Height: 64, Seed: 3,
				MinDistance: 6, MaxAttempts: 30, MaxRooms: 128,
			},
		},
		{
			name: "Typical_seed_11",
			config: Config{
				Width: 128, Height: 128, Seed: 11,
				MinDistance: 6, MaxAttempts: 30, MaxRooms: 256,
			},
		},
		{
			name:   "Maximum_v1_seed_17",
			config: maximumV1BenchmarkConfig(),
		},
	}
}

func maximumV1BenchmarkConfig() Config {
	allDirections := []Direction{DirectionNorth, DirectionEast, DirectionSouth, DirectionWest}
	return Config{
		Width: 256, Height: 256, Seed: 17,
		MinDistance: 1, MaxAttempts: 1024, MaxRooms: 256,
		ExtraEdgeCount: 16,
		RoomGeometry: &RoomGeometry{
			MaxFootprintCells: 81,
			MinRoomGap:        1,
			Shapes: []RoomShapeWeight{
				shapeSpan(RoomShapeRectangle, 4, 3, 9, 3, 9),
				shapeSpan(RoomShapeL, 2, 3, 9, 3, 9),
				shapeSpan(RoomShapeT, 2, 3, 9, 3, 9),
				shapeSpan(RoomShapeCross, 1, 3, 9, 3, 9),
				shapeSpan(RoomShapeCircle, 2, 5, 9, 5, 9),
			},
		},
		RoomRoleRequests: []RoomRoleRequest{
			{Role: RoomRoleStart, Count: 1},
			{Role: RoomRoleBoss, Count: 1},
			{Role: RoomRoleTreasure, Count: 8},
		},
		DensityRegions: []DensityRegion{
			{Min: Cell{X: 0, Y: 0}, Max: Cell{X: 128, Y: 256}, MinDistance: 2},
			{Min: Cell{X: 128, Y: 0}, Max: Cell{X: 256, Y: 256}, MinDistance: 3},
		},
		PlantCatalog: &PlantCatalog{
			Rooms: []RoomPlant{
				{ID: "benchmark_room_a", Tags: []string{"benchmark"}, Weight: 1, DoorDirections: allDirections},
				{ID: "benchmark_room_b", Tags: []string{"benchmark"}, Weight: 1, DoorDirections: allDirections},
			},
			Corridors: []CorridorPlant{
				{ID: "benchmark_corridor_a", Tags: []string{"benchmark"}, Weight: 1},
				{ID: "benchmark_corridor_b", Tags: []string{"benchmark"}, Weight: 1},
			},
		},
	}
}

func TestMaximumBenchmarkLoadEnablesV1Features(t *testing.T) {
	config := maximumV1BenchmarkConfig()

	assert.Equal(t, uint32(256), config.Width)
	assert.Equal(t, uint32(256), config.Height)
	assert.Equal(t, uint32(MaxRooms), config.MaxRooms)
	assert.Equal(t, uint32(1024), config.MaxAttempts)
	require.NotNil(t, config.RoomGeometry)
	assert.Equal(t, uint32(81), config.RoomGeometry.MaxFootprintCells)
	assert.NotZero(t, config.RoomGeometry.MinRoomGap)
	assert.NotZero(t, config.ExtraEdgeCount)
	assert.NotEmpty(t, config.RoomRoleRequests)
	assert.NotEmpty(t, config.DensityRegions)
	assert.NotNil(t, config.PlantCatalog)
}
