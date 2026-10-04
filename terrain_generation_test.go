package daedalus

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sort"
	"testing"
)

func TestConnectivitySpineProtectsRoomBFSAndRoutes(t *testing.T) {
	roomCells := []Cell{{X: 1, Y: 1}, {X: 2, Y: 1}, {X: 1, Y: 2}, {X: 2, Y: 2}}
	layout := Layout{
		Grid:      Grid{Width: 7, Height: 4, Cells: make([]CellState, 28)},
		Rooms:     []Room{{ID: 0, At: roomCells[0], Cells: roomCells, DoorIDs: []DoorID{0}}},
		Doors:     []Door{{ID: 0, RoomID: 0, At: roomCells[3]}},
		Corridors: []Corridor{{ID: 0, Centerline: []Cell{{X: 3, Y: 2}, {X: 4, Y: 2}}}},
	}
	spine, err := buildConnectivitySpine(context.Background(), layout)
	if err != nil {
		t.Fatal(err)
	}
	if !spine.protected(roomCells[0]) || !spine.protected(roomCells[3]) {
		t.Fatal("Room anchor and Door Cell must be protected")
	}
	if spine.protected(roomCells[2]) {
		t.Fatal("a side Room Cell not on the anchor-to-Door path need not be protected")
	}
	for _, cell := range layout.Corridors[0].Centerline {
		if !spine.protected(cell) {
			t.Fatalf("centerline Cell %v is not protected", cell)
		}
	}
	if spine.protected(Cell{X: 6, Y: 3}) {
		t.Fatal("unrelated Empty Cell must not be protected")
	}
}

func TestConnectivitySpineCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := buildConnectivitySpine(ctx, Layout{Grid: Grid{Width: 1, Height: 1}, Rooms: []Room{{At: Cell{}}}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestTerrainPatchesAndStreamIsolation(t *testing.T) {
	base := Config{Width: 32, Height: 32, Seed: 91, MinDistance: 3, MaxRooms: 3}
	without, err := (Generator{}).Generate(base)
	if err != nil {
		t.Fatal(err)
	}
	withConfig := base
	withConfig.Terrain = &TerrainConfig{
		Definitions: []TerrainDefinition{{ID: "stone", EntryCost: 0}, {ID: "water", EntryCost: 2}},
		Rooms:       &TerrainDistribution{NoneWeight: 1, Terrains: []TerrainWeight{{TerrainID: "stone", Weight: 2}, {TerrainID: "water", Weight: 3}}},
		Corridors:   &TerrainDistribution{NoneWeight: 2, Terrains: []TerrainWeight{{TerrainID: "stone", Weight: 1}, {TerrainID: "water", Weight: 1}}},
	}
	with, err := (Generator{}).Generate(withConfig)
	if err != nil {
		t.Fatal(err)
	}
	if with.Grid.Terrain == nil {
		t.Fatal("non-nil terrain config must attach a layer")
	}
	withBase := with
	withBase.Grid.Terrain = nil
	if !reflect.DeepEqual(without, withBase) {
		t.Fatal("terrain configuration changed geometry, topology, plants, or widths")
	}
}

func TestTerrainStreamIsDedicatedAndDoesNotConsumePlacement(t *testing.T) {
	streams := newRNGStreams(Seed(0x1234))
	placementState := streams.placement
	terrainState := streams.terrain
	streams.terrain.Next()
	if streams.placement != placementState {
		t.Fatal("terrain placement consumed the placement stream")
	}
	if streams.terrain == terrainState {
		t.Fatal("terrain stream did not advance")
	}
	if terrainState == placementState {
		t.Fatal("terrain stream reused the placement salt")
	}
}

func TestTerrainPlacementCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	effective, err := normalizeConfig(Config{
		Width: 4, Height: 4,
		Terrain: &TerrainConfig{Definitions: []TerrainDefinition{{ID: "water", EntryCost: 1}}, Rooms: &TerrainDistribution{NoneWeight: 1, Terrains: []TerrainWeight{{TerrainID: "water", Weight: 1}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	layout := Layout{Grid: Grid{Width: 4, Height: 4, Cells: make([]CellState, 16)}}
	for index := range layout.Grid.Cells {
		layout.Grid.Cells[index].At = Cell{X: int32(index % 4), Y: int32(index / 4)}
		layout.Grid.Cells[index].Kind = CellKindRoom
	}
	layout.Rooms = []Room{{At: Cell{}, Cells: []Cell{{}}}}
	if err := placeTerrain(ctx, effective, &layout); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestTerrainPatchGolden(t *testing.T) {
	layout := terrainGoldenLayout()
	effective, err := normalizeConfig(Config{
		Width: 6, Height: 4, Seed: 0xBEEF,
		Terrain: &TerrainConfig{
			Definitions: []TerrainDefinition{{ID: "grass", EntryCost: 1}, {ID: "rock", EntryCost: 0}, {ID: "water", EntryCost: 2}},
			Rooms:       &TerrainDistribution{Terrains: []TerrainWeight{{TerrainID: "water", Weight: 1}}},
			Corridors:   &TerrainDistribution{NoneWeight: 1, Terrains: []TerrainWeight{{TerrainID: "water", Weight: 2}, {TerrainID: "grass", Weight: 1}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := placeTerrain(context.Background(), effective, &layout); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/golden/terrain_patch.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Palette []TerrainDefinition `json:"palette"`
		Indices []int               `json:"indices"`
	}
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	if terrainNeighborOrder != [...]Direction{DirectionNorth, DirectionEast, DirectionSouth, DirectionWest} {
		t.Fatalf("terrain neighbor order = %v, want N/E/S/O", terrainNeighborOrder)
	}
	indices := make([]int, len(layout.Grid.Terrain.Indices))
	for index, value := range layout.Grid.Terrain.Indices {
		indices[index] = int(value)
	}
	if !reflect.DeepEqual(layout.Grid.Terrain.Palette, golden.Palette) || !reflect.DeepEqual(indices, golden.Indices) {
		t.Fatalf("terrain golden changed: palette=%v indices=%v", layout.Grid.Terrain.Palette, layout.Grid.Terrain.Indices)
	}
}

func TestTerrainSpineNeverReceivesImpassableTerrain(t *testing.T) {
	for seed := Seed(0); seed < 32; seed++ {
		assertSpineRejectsImpassableTerrain(t, seed)
	}
}

func assertSpineRejectsImpassableTerrain(t *testing.T, seed Seed) {
	t.Helper()
	layout := terrainGoldenLayout()
	effective, err := normalizeConfig(Config{
		Width: 6, Height: 4, Seed: seed,
		Terrain: &TerrainConfig{
			Definitions: []TerrainDefinition{{ID: "rock", EntryCost: 0}, {ID: "water", EntryCost: 1}},
			Rooms:       &TerrainDistribution{Terrains: []TerrainWeight{{TerrainID: "rock", Weight: 99}, {TerrainID: "water", Weight: 1}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertNoImpassableProtectedCandidate(t, effective, seed)
	if err := placeTerrain(context.Background(), effective, &layout); err != nil {
		t.Fatal(err)
	}
	assertProtectedCellsAvoidImpassable(t, layout, seed)
}

func assertNoImpassableProtectedCandidate(t *testing.T, effective effectiveConfig, seed Seed) {
	t.Helper()
	for _, candidate := range terrainCandidates(effective.terrain, effective.terrain.Rooms, true) {
		if candidate.index == 1 {
			t.Fatalf("protected candidate set contains impassable terrain at seed %d", seed)
		}
	}
}

func assertProtectedCellsAvoidImpassable(t *testing.T, layout Layout, seed Seed) {
	t.Helper()
	spine, err := buildConnectivitySpine(context.Background(), layout)
	if err != nil {
		t.Fatal(err)
	}
	for index, state := range layout.Grid.Cells {
		if spine.protected(state.At) && layout.Grid.Terrain.Indices[index] == 1 {
			t.Fatalf("protected Cell %v received impassable terrain at seed %d", state.At, seed)
		}
	}
}

func TestTerrainPatchHistogramHasRecognizableRegions(t *testing.T) {
	config := Config{
		Width: 96, Height: 96, Seed: 4242, MinDistance: 6,
		Terrain: &TerrainConfig{
			Definitions: []TerrainDefinition{{ID: "grass", EntryCost: 1}, {ID: "water", EntryCost: 3}},
			Rooms:       &TerrainDistribution{NoneWeight: 5, Terrains: []TerrainWeight{{TerrainID: "water", Weight: 3}, {TerrainID: "grass", Weight: 2}}},
			Corridors:   &TerrainDistribution{NoneWeight: 5, Terrains: []TerrainWeight{{TerrainID: "water", Weight: 3}, {TerrainID: "grass", Weight: 2}}},
		},
	}
	layout, err := (Generator{}).Generate(config)
	if err != nil {
		t.Fatal(err)
	}
	histogram := terrainRegionHistogram(layout.Grid)
	for _, id := range []TerrainID{"water", "grass"} {
		assertTerrainRegionIsNotSpeckled(t, layout, histogram, id)
	}
}

func assertTerrainRegionIsNotSpeckled(t *testing.T, layout Layout, histogram map[byte]map[int]int, id TerrainID) {
	t.Helper()
	index := terrainPaletteIndex(layout.Grid.Terrain.Palette, id)
	sizes, cells := terrainRegionSizes(histogram[index])
	if len(sizes) == 0 {
		t.Fatalf("no regions for %s", id)
	}
	sort.Ints(sizes)
	median := sizes[len(sizes)/2]
	singles := histogram[index][1]
	if median <= 1 || singles*2 >= len(sizes) {
		t.Fatalf("%s remains speckled: regions=%d singles=%d median=%d cells=%d histogram=%v", id, len(sizes), singles, median, cells, histogram[index])
	}
	t.Logf("%s: regions=%d cells=%d median=%d histogram=%v", id, len(sizes), cells, median, histogram[index])
}

func terrainPaletteIndex(palette []TerrainDefinition, id TerrainID) byte {
	for paletteIndex, definition := range palette {
		if definition.ID == id {
			return byte(paletteIndex + 1)
		}
	}
	return 0
}

func terrainRegionSizes(counts map[int]int) ([]int, int) {
	sizes := make([]int, 0)
	cells := 0
	for size, count := range counts {
		for repeat := 0; repeat < count; repeat++ {
			sizes = append(sizes, size)
			cells += size
		}
	}
	return sizes, cells
}

func TestTerrainAreaGrowthGolden(t *testing.T) {
	layout := terrainAreaLayout()
	effective, err := normalizeConfig(Config{
		Width: 7, Height: 7, Seed: 11,
		Terrain: &TerrainConfig{
			Definitions: []TerrainDefinition{{ID: "water", EntryCost: 1}},
			Rooms:       &TerrainDistribution{Terrains: []TerrainWeight{{TerrainID: "water", Weight: 1}}, MinPatchCells: 8, MaxPatchCells: 8},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := placeTerrain(context.Background(), effective, &layout); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/golden/terrain_patch_area.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden []int
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	indices := make([]int, len(layout.Grid.Terrain.Indices))
	for index, value := range layout.Grid.Terrain.Indices {
		indices[index] = int(value)
	}
	if !reflect.DeepEqual(indices, golden) {
		t.Fatalf("area-growth golden changed: %v", indices)
	}
}

func terrainRegionHistogram(grid Grid) map[byte]map[int]int {
	histogram := make(map[byte]map[int]int)
	visited := make([]bool, len(grid.Cells))
	width := int(grid.Width)
	for index, state := range grid.Cells {
		if visited[index] || grid.Terrain.Indices[index] == 0 {
			continue
		}
		size := floodTerrainRegion(grid, visited, index, grid.Terrain.Indices[index], state.Kind, width)
		label := grid.Terrain.Indices[index]
		if histogram[label] == nil {
			histogram[label] = make(map[int]int)
		}
		histogram[label][size]++
	}
	return histogram
}

func floodTerrainRegion(grid Grid, visited []bool, start int, label byte, kind CellKind, width int) int {
	queue := []int{start}
	visited[start] = true
	for head := 0; head < len(queue); head++ {
		enqueueTerrainRegionNeighbors(grid, visited, &queue, queue[head], label, kind, width)
	}
	return len(queue)
}

func enqueueTerrainRegionNeighbors(
	grid Grid,
	visited []bool,
	queue *[]int,
	current int,
	label byte,
	kind CellKind,
	width int,
) {
	x, y := current%width, current/width
	for _, direction := range terrainNeighborOrder {
		delta := direction.Delta()
		nextX, nextY := x+int(delta.X), y+int(delta.Y)
		if nextX < 0 || nextX >= width || nextY < 0 || nextY >= int(grid.Height) {
			continue
		}
		next := nextY*width + nextX
		if visited[next] || grid.Terrain.Indices[next] != label || grid.Cells[next].Kind != kind {
			continue
		}
		visited[next] = true
		*queue = append(*queue, next)
	}
}

func terrainGoldenLayout() Layout {
	cells := make([]CellState, 24)
	for index := range cells {
		cells[index].At = Cell{X: int32(index % 6), Y: int32(index / 6)}
	}
	roomCells := []Cell{{X: 1, Y: 1}, {X: 2, Y: 1}, {X: 3, Y: 1}, {X: 1, Y: 2}, {X: 2, Y: 2}, {X: 3, Y: 2}, {X: 1, Y: 3}, {X: 2, Y: 3}, {X: 3, Y: 3}}
	for _, cell := range roomCells {
		cells[int(cell.Y)*6+int(cell.X)].Kind = CellKindRoom
	}
	corridorCells := []Cell{{X: 4, Y: 1}, {X: 4, Y: 2}}
	for _, cell := range corridorCells {
		cells[int(cell.Y)*6+int(cell.X)].Kind = CellKindCorridor
	}
	return Layout{
		Grid:      Grid{Width: 6, Height: 4, Cells: cells},
		Rooms:     []Room{{ID: 0, At: roomCells[0], Cells: roomCells}},
		Corridors: []Corridor{{ID: 0, Centerline: corridorCells}},
	}
}

func terrainAreaLayout() Layout {
	cells := make([]CellState, 49)
	roomCells := make([]Cell, 0, 25)
	for y := int32(1); y <= 5; y++ {
		for x := int32(1); x <= 5; x++ {
			cell := Cell{X: x, Y: y}
			roomCells = append(roomCells, cell)
			cells[int(y)*7+int(x)] = CellState{At: cell, Kind: CellKindRoom}
		}
	}
	for index := range cells {
		if cells[index].At == (Cell{}) {
			cells[index].At = Cell{X: int32(index % 7), Y: int32(index / 7)}
		}
	}
	return Layout{Grid: Grid{Width: 7, Height: 7, Cells: cells}, Rooms: []Room{{ID: 0, At: roomCells[0], Cells: roomCells}}}
}
