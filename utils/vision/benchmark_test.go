package vision_test

import (
	"context"
	"testing"

	"github.com/Otoru/daedalus"
	"github.com/Otoru/daedalus/utils/vision"
)

type visibilityLoad struct {
	name    string
	width   uint32
	height  uint32
	queries int
	radius  uint32
	open    bool
}

// BenchmarkComputeVisibility records the three release loads from the plan.
// The benchmark reports allocations as well as time; latency budgets are
// hardware-specific and are intentionally not asserted in the test suite.
func BenchmarkComputeVisibility(b *testing.B) {
	for _, load := range visibilityLoads() {
		b.Run(load.name, func(b *testing.B) {
			grid, queries := load.gridAndQueries()
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				fields, err := vision.Answer(context.Background(), grid, queries)
				if err != nil {
					b.Fatal(err)
				}
				if len(fields) != len(queries) {
					b.Fatalf("Answer returned %d fields for %d queries", len(fields), len(queries))
				}
			}
		})
	}
}

// BenchmarkComputeVisibilityIntoWarm measures the steady-state API after one
// warm-up pass at the same grid and radius. It should report zero allocations.
func BenchmarkComputeVisibilityIntoWarm(b *testing.B) {
	for _, load := range visibilityLoads() {
		b.Run(load.name, func(b *testing.B) {
			grid, queries := load.gridAndQueries()
			dst := make([]vision.Field, len(queries))
			if _, err := vision.AnswerInto(context.Background(), dst, grid, queries); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				fields, err := vision.AnswerInto(context.Background(), dst, grid, queries)
				if err != nil {
					b.Fatal(err)
				}
				if len(fields) != len(queries) {
					b.Fatalf("AnswerInto returned %d fields for %d queries", len(fields), len(queries))
				}
			}
		})
	}
}

func visibilityLoads() []visibilityLoad {
	return []visibilityLoad{
		{name: "Small_64x64", width: 64, height: 64, queries: 16, radius: 12},
		{name: "Typical_128x128", width: 128, height: 128, queries: 64, radius: 24},
		{name: "Maximum_v1_256x256", width: 256, height: 256, queries: vision.MaxQueries, radius: 361, open: true},
	}
}

func (load visibilityLoad) gridAndQueries() (vision.OpacityGrid, []vision.Query) {
	grid := openGrid(int(load.width), int(load.height))
	if !load.open {
		for y := uint32(0); y < load.height; y++ {
			for x := uint32(0); x < load.width; x++ {
				if (x+y)%11 == 0 {
					grid.SetTransparent(daedalus.Cell{X: int32(x), Y: int32(y)}, false)
				}
			}
		}
	}
	origins := make([]daedalus.Cell, 0, load.queries)
	for y := uint32(0); y < load.height && len(origins) < load.queries; y++ {
		for x := uint32(0); x < load.width && len(origins) < load.queries; x++ {
			at := daedalus.Cell{X: int32(x), Y: int32(y)}
			if grid.TransparentAt(at) {
				origins = append(origins, at)
			}
		}
	}
	queries := make([]vision.Query, len(origins))
	for index, origin := range origins {
		queries[index] = vision.Query{Origin: origin, Radius: load.radius}
	}
	return grid, queries
}
