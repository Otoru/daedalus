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
					b.Fatal("Generate não devolveu Rooms")
				}
			}
		})
	}
}

func generationBenchmarks() []generationBenchmark {
	return []generationBenchmark{
		{
			name: "Pequena_seed_3",
			config: Config{
				Width: 64, Height: 64, Seed: 3,
				MinDistance: 6, MaxAttempts: 30, MaxRooms: 128,
			},
		},
		{
			name: "Tipica_seed_11",
			config: Config{
				Width: 128, Height: 128, Seed: 11,
				MinDistance: 6, MaxAttempts: 30, MaxRooms: 256,
			},
		},
		{
			name:   "Maxima_v1_seed_17",
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
			MinWidth: 3, MaxWidth: 9, MinHeight: 3, MaxHeight: 9,
			MaxFootprintCells: 81,
			MinRoomGap:        1,
			Shapes: []RoomShapeWeight{
				{Shape: RoomShapeRectangle, Weight: 4},
				{Shape: RoomShapeL, Weight: 2},
				{Shape: RoomShapeT, Weight: 2},
				{Shape: RoomShapeCross, Weight: 1},
				{Shape: RoomShapeCircle, Weight: 2},
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

func TestCargaMaximaDoBenchmarkHabilitaRecursosV1(t *testing.T) {
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
