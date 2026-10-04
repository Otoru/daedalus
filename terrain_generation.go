package daedalus

import (
	"context"
	"fmt"

	"github.com/Otoru/daedalus/core"
)

// connectivitySpine is a compact row-major bitset. It is deliberately private:
// it proves the generator's own reachability promise, not a caller's navigation
// policy.
type connectivitySpine struct {
	width  uint32
	height uint32
	bits   []uint64
}

func newConnectivitySpine(width, height uint32) *connectivitySpine {
	cellCount := uint64(width) * uint64(height)
	return &connectivitySpine{width: width, height: height, bits: make([]uint64, (cellCount+63)/64)}
}

func (spine *connectivitySpine) index(cell Cell) (uint64, bool) {
	if cell.X < 0 || cell.Y < 0 || uint32(cell.X) >= spine.width || uint32(cell.Y) >= spine.height {
		return 0, false
	}
	index := uint64(cell.Y)*uint64(spine.width) + uint64(cell.X)
	if index>>6 >= uint64(len(spine.bits)) {
		return 0, false
	}
	return index, true
}

func (spine *connectivitySpine) mark(cell Cell) {
	index, ok := spine.index(cell)
	if !ok {
		return
	}
	spine.bits[index>>6] |= uint64(1) << (index & 63)
}

func (spine *connectivitySpine) protected(cell Cell) bool {
	index, ok := spine.index(cell)
	return ok && spine.bits[index>>6]&(uint64(1)<<(index&63)) != 0
}

// buildConnectivitySpine marks every Room anchor, each Room-to-Door footprint
// path, every Corridor centerline Cell, and every Door Cell. Room BFS expands
// only the room footprint and visits neighbours in North, East, South, West.
func buildConnectivitySpine(ctx context.Context, layout Layout) (*connectivitySpine, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	spine := newConnectivitySpine(layout.Grid.Width, layout.Grid.Height)
	work := uint64(0)
	check := func() error {
		work++
		if work&255 == 0 {
			return ctx.Err()
		}
		return nil
	}
	for _, room := range layout.Rooms {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := markRoomConnectivity(spine, layout, room, check); err != nil {
			return nil, err
		}
	}
	if err := markCorridorCenterlines(spine, layout.Corridors, check); err != nil {
		return nil, err
	}
	if err := markDoorCells(spine, layout.Doors, check); err != nil {
		return nil, err
	}
	return spine, nil
}

func markRoomConnectivity(
	spine *connectivitySpine,
	layout Layout,
	room Room,
	check func() error,
) error {
	cells, parents, visited, err := roomFootprintParents(spine, room, check)
	if err != nil {
		return err
	}
	for _, doorID := range room.DoorIDs {
		if err := markRoomDoorPath(spine, layout, room, cells, parents, visited, doorID); err != nil {
			return err
		}
	}
	return nil
}

func roomFootprintParents(
	spine *connectivitySpine,
	room Room,
	check func() error,
) (map[uint64]int, []int, []bool, error) {
	cells := make(map[uint64]int, len(room.Cells))
	for local, cell := range room.Cells {
		index, ok := spine.index(cell)
		if !ok {
			return nil, nil, nil, fmt.Errorf("%w: Room %d has out-of-grid Cell", errGeneratorInvariant, room.ID)
		}
		cells[index] = local
	}
	rootIndex, ok := spine.index(room.At)
	if !ok {
		return nil, nil, nil, fmt.Errorf("%w: Room %d anchor is outside Grid", errGeneratorInvariant, room.ID)
	}
	rootLocal, exists := cells[rootIndex]
	if !exists {
		return nil, nil, nil, fmt.Errorf("%w: Room %d anchor is outside footprint", errGeneratorInvariant, room.ID)
	}
	parents := make([]int, len(room.Cells))
	for index := range parents {
		parents[index] = -1
	}
	visited := make([]bool, len(room.Cells))
	queue := make([]int, 0, len(room.Cells))
	queue = append(queue, rootLocal)
	visited[rootLocal] = true
	spine.mark(room.At)
	for head := 0; head < len(queue); head++ {
		if err := check(); err != nil {
			return nil, nil, nil, err
		}
		expandRoomFootprintBFS(spine, room, cells, parents, visited, &queue, head)
	}
	return cells, parents, visited, nil
}

func expandRoomFootprintBFS(
	spine *connectivitySpine,
	room Room,
	cells map[uint64]int,
	parents []int,
	visited []bool,
	queue *[]int,
	head int,
) {
	current := room.Cells[(*queue)[head]]
	for _, direction := range terrainNeighborOrder {
		delta := direction.Delta()
		next := Cell{X: current.X + delta.X, Y: current.Y + delta.Y}
		nextIndex, inside := spine.index(next)
		if !inside {
			continue
		}
		nextLocal, inFootprint := cells[nextIndex]
		if !inFootprint || visited[nextLocal] {
			continue
		}
		visited[nextLocal] = true
		parents[nextLocal] = (*queue)[head]
		*queue = append(*queue, nextLocal)
	}
}

func markRoomDoorPath(
	spine *connectivitySpine,
	layout Layout,
	room Room,
	cells map[uint64]int,
	parents []int,
	visited []bool,
	doorID DoorID,
) error {
	if int(doorID) >= len(layout.Doors) {
		return fmt.Errorf("%w: Room %d has invalid DoorID", errGeneratorInvariant, room.ID)
	}
	door := layout.Doors[doorID]
	currentIndex, inside := spine.index(door.At)
	if !inside {
		return fmt.Errorf("%w: Door %d is outside Grid", errGeneratorInvariant, door.ID)
	}
	currentLocal, inFootprint := cells[currentIndex]
	if !inFootprint || !visited[currentLocal] {
		return fmt.Errorf("%w: Door %d is outside Room footprint", errGeneratorInvariant, door.ID)
	}
	for currentLocal >= 0 {
		spine.mark(room.Cells[currentLocal])
		currentLocal = parents[currentLocal]
	}
	return nil
}

func markCorridorCenterlines(spine *connectivitySpine, corridors []Corridor, check func() error) error {
	for _, corridor := range corridors {
		for _, cell := range corridor.Centerline {
			spine.mark(cell)
			if err := check(); err != nil {
				return err
			}
		}
	}
	return nil
}

func markDoorCells(spine *connectivitySpine, doors []Door, check func() error) error {
	for _, door := range doors {
		spine.mark(door.At)
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}

type terrainCandidate struct {
	index  byte
	weight uint32
}

var terrainNeighborOrder = [...]Direction{DirectionNorth, DirectionEast, DirectionSouth, DirectionWest}

type terrainSeed struct {
	index  int
	label  byte
	target int
}

func terrainCandidates(config *TerrainConfig, distribution *TerrainDistribution, protected bool) []terrainCandidate {
	if distribution == nil {
		return nil
	}
	definitions := make(map[TerrainID]TerrainDefinition, len(config.Definitions))
	indices := make(map[TerrainID]byte, len(config.Definitions))
	for index, definition := range config.Definitions {
		definitions[definition.ID] = definition
		indices[definition.ID] = byte(index + 1)
	}
	candidates := make([]terrainCandidate, 0, len(distribution.Terrains)+1)
	if distribution.NoneWeight > 0 {
		candidates = append(candidates, terrainCandidate{weight: distribution.NoneWeight})
	}
	for _, terrain := range distribution.Terrains {
		definition := definitions[terrain.TerrainID]
		if protected && definition.EntryCost == 0 {
			continue
		}
		candidates = append(candidates, terrainCandidate{index: indices[terrain.TerrainID], weight: terrain.Weight})
	}
	return candidates
}

func selectTerrainCandidate(candidates []terrainCandidate, stream *core.SplitMix64) byte {
	var total uint64
	for _, candidate := range candidates {
		total += uint64(candidate.weight)
	}
	if total == 0 {
		return 0
	}
	draw := stream.UniformInt(weightedSelectionFirstTicket, total)
	for _, candidate := range candidates {
		weight := uint64(candidate.weight)
		if draw <= weight {
			return candidate.index
		}
		draw -= weight
	}
	return 0
}

// placeTerrain chooses sparse row-major seed slots, draws one terrain and one
// integer target size per non-empty seed, then grows each patch by FIFO BFS.
// Protected Cells reset their base kind's seed ordinal and are skipped before
// any draw. Each expansion enqueues all eligible neighbours in N/E/S/W order;
// the target is checked between expansions, so a batch may overshoot it by
// up to three Cells. Patches are processed in seed order, so earlier patches
// claim cells before later patches can use them.
func placeTerrain(ctx context.Context, effective effectiveConfig, layout *Layout) error {
	if effective.terrain == nil {
		return nil
	}
	spine, err := buildConnectivitySpine(ctx, *layout)
	if err != nil {
		return err
	}
	streams := newRNGStreams(effective.seed)
	seeds, err := collectTerrainSeeds(ctx, effective, layout, spine, &streams.terrain)
	if err != nil {
		return err
	}
	indices, err := growTerrainPatches(ctx, layout, spine, seeds)
	if err != nil {
		return err
	}
	palette := append([]TerrainDefinition(nil), effective.terrain.Definitions...)
	layout.Grid.Terrain = &TerrainLayer{Palette: palette, Indices: indices}
	return nil
}

func collectTerrainSeeds(
	ctx context.Context,
	effective effectiveConfig,
	layout *Layout,
	spine *connectivitySpine,
	stream *core.SplitMix64,
) ([]terrainSeed, error) {
	seeds := make([]terrainSeed, 0, len(layout.Grid.Cells)/8)
	var roomOrdinal, corridorOrdinal uint64
	for index, state := range layout.Grid.Cells {
		if index&255 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		seed, ok, err := maybeTerrainSeed(
			effective, spine, state, index,
			&roomOrdinal, &corridorOrdinal, stream,
		)
		if err != nil {
			return nil, err
		}
		if ok {
			seeds = append(seeds, seed)
		}
	}
	return seeds, nil
}

func maybeTerrainSeed(
	effective effectiveConfig,
	spine *connectivitySpine,
	state CellState,
	index int,
	roomOrdinal, corridorOrdinal *uint64,
	stream *core.SplitMix64,
) (terrainSeed, bool, error) {
	distribution := distributionForCellKind(effective.terrain, state.Kind)
	if distribution == nil {
		return terrainSeed{}, false, nil
	}
	ordinal := roomOrdinal
	if state.Kind == CellKindCorridor {
		ordinal = corridorOrdinal
	}
	if spine.protected(state.At) {
		*ordinal = 0
		return terrainSeed{}, false, nil
	}
	seedSlot := *ordinal%uint64(distribution.MaxPatchCells) == 0
	(*ordinal)++
	if !seedSlot {
		return terrainSeed{}, false, nil
	}
	label := selectTerrainCandidate(
		terrainCandidates(effective.terrain, distribution, false),
		stream,
	)
	if label == 0 {
		return terrainSeed{}, false, nil
	}
	target := int(stream.UniformInt(uint64(distribution.MinPatchCells), uint64(distribution.MaxPatchCells)))
	return terrainSeed{index: index, label: label, target: target}, true, nil
}

func distributionForCellKind(config *TerrainConfig, kind CellKind) *TerrainDistribution {
	switch kind {
	case CellKindRoom:
		return config.Rooms
	case CellKindCorridor:
		return config.Corridors
	default:
		return nil
	}
}

func growTerrainPatches(
	ctx context.Context,
	layout *Layout,
	spine *connectivitySpine,
	seeds []terrainSeed,
) ([]byte, error) {
	indices := make([]byte, len(layout.Grid.Cells))
	width := int(layout.Grid.Width)
	for _, seed := range seeds {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if indices[seed.index] != 0 {
			continue
		}
		seedKind := layout.Grid.Cells[seed.index].Kind
		indices[seed.index] = seed.label
		if spine.protected(layout.Grid.Cells[seed.index].At) {
			continue
		}
		if err := expandTerrainPatch(ctx, layout, spine, indices, seed, seedKind, width); err != nil {
			return nil, err
		}
	}
	return indices, nil
}

func expandTerrainPatch(
	ctx context.Context,
	layout *Layout,
	spine *connectivitySpine,
	indices []byte,
	seed terrainSeed,
	seedKind CellKind,
	width int,
) error {
	queue := []int{seed.index}
	for head := 0; head < len(queue) && len(queue) < seed.target; head++ {
		if head&255 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		enqueueTerrainNeighbors(layout, spine, indices, &queue, queue[head], seedKind, width)
	}
	return nil
}

func enqueueTerrainNeighbors(
	layout *Layout,
	spine *connectivitySpine,
	indices []byte,
	queue *[]int,
	current int,
	seedKind CellKind,
	width int,
) {
	label := indices[current]
	x := current % width
	y := current / width
	for _, direction := range terrainNeighborOrder {
		delta := direction.Delta()
		nextX, nextY := x+int(delta.X), y+int(delta.Y)
		if nextX < 0 || nextX >= width || nextY < 0 || nextY >= int(layout.Grid.Height) {
			continue
		}
		next := nextY*width + nextX
		if indices[next] != 0 || spine.protected(layout.Grid.Cells[next].At) || layout.Grid.Cells[next].Kind != seedKind {
			continue
		}
		indices[next] = label
		*queue = append(*queue, next)
	}
}
