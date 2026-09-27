package daedalus

import "testing"

func BenchmarkGenerateCargasDaSeção11(b *testing.B) {
	benchmarks := []struct {
		name   string
		config Config
	}{
		{
			name: "Pequena",
			config: Config{
				Width: 64, Height: 64, Seed: 3,
				MinDistance: 6, MaxAttempts: 30, MaxRooms: 128,
			},
		},
		{
			name: "Típica",
			config: Config{
				Width: 128, Height: 128, Seed: 11,
				MinDistance: 6, MaxAttempts: 30, MaxRooms: 256,
			},
		},
		{
			name: "Máxima_v1",
			config: Config{
				Width: 256, Height: 256, Seed: 17,
				MinDistance: 1, MaxAttempts: 1024, MaxRooms: 256,
				ExtraEdgeCount: 16,
				RoomRoleRequests: []RoomRoleRequest{
					{Role: RoomRoleStart, Count: 1},
					{Role: RoomRoleBoss, Count: 1},
					{Role: RoomRoleTreasure, Count: 8},
				},
				DensityRegions: []DensityRegion{{
					Min: Cell{X: 0, Y: 0}, Max: Cell{X: 128, Y: 128}, MinDistance: 3,
				}},
			},
		},
	}

	for _, benchmark := range benchmarks {
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
