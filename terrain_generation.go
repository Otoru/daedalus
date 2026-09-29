package daedalus

import (
	"context"
	"fmt"
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
		cells := make(map[uint64]int, len(room.Cells))
		for local, cell := range room.Cells {
			index, ok := spine.index(cell)
			if !ok {
				return nil, fmt.Errorf("%w: Room %d has out-of-grid Cell", errGeneratorInvariant, room.ID)
			}
			cells[index] = local
		}
		rootIndex, ok := spine.index(room.At)
		if !ok {
			return nil, fmt.Errorf("%w: Room %d anchor is outside Grid", errGeneratorInvariant, room.ID)
		}
		rootLocal, exists := cells[rootIndex]
		if !exists {
			return nil, fmt.Errorf("%w: Room %d anchor is outside footprint", errGeneratorInvariant, room.ID)
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
				return nil, err
			}
			current := room.Cells[queue[head]]
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
				parents[nextLocal] = queue[head]
				queue = append(queue, nextLocal)
			}
		}
		for _, doorID := range room.DoorIDs {
			if int(doorID) >= len(layout.Doors) {
				return nil, fmt.Errorf("%w: Room %d has invalid DoorID", errGeneratorInvariant, room.ID)
			}
			door := layout.Doors[doorID]
			currentIndex, inside := spine.index(door.At)
			if !inside {
				return nil, fmt.Errorf("%w: Door %d is outside Grid", errGeneratorInvariant, door.ID)
			}
			currentLocal, inFootprint := cells[currentIndex]
			if !inFootprint || !visited[currentLocal] {
				return nil, fmt.Errorf("%w: Door %d is outside Room footprint", errGeneratorInvariant, door.ID)
			}
			for currentLocal >= 0 {
				spine.mark(room.Cells[currentLocal])
				currentLocal = parents[currentLocal]
			}
		}
	}
	for _, corridor := range layout.Corridors {
		for _, cell := range corridor.Centerline {
			spine.mark(cell)
			if err := check(); err != nil {
				return nil, err
			}
		}
	}
	for _, door := range layout.Doors {
		spine.mark(door.At)
		if err := check(); err != nil {
			return nil, err
		}
	}
	return spine, nil
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

func selectTerrainCandidate(candidates []terrainCandidate, stream *splitMix64) byte {
	var total uint64
	for _, candidate := range candidates {
		total += uint64(candidate.weight)
	}
	if total == 0 {
		return 0
	}
	draw := stream.uniformInt(weightedSelectionFirstTicket, total)
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
// Every eligible neighbour is enqueued in canonical N/E/S/W order until the
// target is reached. Patches are processed in seed order, so earlier patches
// claim cells before later patches can use them.
func placeTerrain(ctx context.Context, effective effectiveConfig, layout *Layout) error {
	if effective.terrain == nil {
		return nil
	}
	spine, err := buildConnectivitySpine(ctx, *layout)
	if err != nil {
		return err
	}
	cellCount := len(layout.Grid.Cells)
	streams := newRNGStreams(effective.seed)
	seeds := make([]terrainSeed, 0, cellCount/8)
	var roomOrdinal, corridorOrdinal uint64
	for index, state := range layout.Grid.Cells {
		if index&255 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		var distribution *TerrainDistribution
		switch state.Kind {
		case CellKindRoom:
			distribution = effective.terrain.Rooms
		case CellKindCorridor:
			distribution = effective.terrain.Corridors
		default:
			continue
		}
		if distribution == nil {
			continue
		}
		if spine.protected(state.At) {
			if state.Kind == CellKindRoom {
				roomOrdinal = 0
			} else {
				corridorOrdinal = 0
			}
			continue
		}
		ordinal := &roomOrdinal
		if state.Kind == CellKindCorridor {
			ordinal = &corridorOrdinal
		}
		seedSlot := *ordinal%uint64(distribution.MaxPatchCells) == 0
		(*ordinal)++
		if !seedSlot {
			continue
		}
		label := selectTerrainCandidate(
			terrainCandidates(effective.terrain, distribution, false),
			&streams.terrain,
		)
		if label == 0 {
			continue
		}
		target := int(streams.terrain.uniformInt(uint64(distribution.MinPatchCells), uint64(distribution.MaxPatchCells)))
		seeds = append(seeds, terrainSeed{index: index, label: label, target: target})
	}
	indices := make([]byte, cellCount)
	width := int(layout.Grid.Width)
	for _, seed := range seeds {
		if err := ctx.Err(); err != nil {
			return err
		}
		if indices[seed.index] != 0 {
			continue
		}
		seedKind := layout.Grid.Cells[seed.index].Kind
		indices[seed.index] = seed.label
		if spine.protected(layout.Grid.Cells[seed.index].At) {
			continue
		}
		queue := []int{seed.index}
		for head := 0; head < len(queue) && len(queue) < seed.target; head++ {
			if head&255 == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			current := queue[head]
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
				indices[next] = seed.label
				queue = append(queue, next)
			}
		}
	}
	palette := append([]TerrainDefinition(nil), effective.terrain.Definitions...)
	layout.Grid.Terrain = &TerrainLayer{Palette: palette, Indices: indices}
	return nil
}
