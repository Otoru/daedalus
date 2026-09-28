package pathfinding

import (
	"context"
	"fmt"

	"github.com/Otoru/daedalus"
)

const (
	// Unreachable distinguishes the absence of a route from every finite
	// distance. A large finite sentinel would compare as merely far away and
	// could make a caller walk toward a goal that cannot be reached.
	Unreachable Distance = -1
	// MaxDistance is the largest accepted Source bias and the greatest cost of
	// entering MaxCost Cells across a maximum-sized Grid.
	MaxDistance Distance = daedalus.MaxCells * Distance(MaxCost)

	fieldFirstGeneration      uint32 = 1
	fieldCancellationInterval uint64 = 256
	fieldDirectionCount              = 4
)

// Distance is an accumulated integer entry cost in a distance Field.
type Distance int32

// Source is one fixed origin of a distance Field. Bias is the origin's
// starting distance; entering the Source Cell itself never adds its Cost.
type Source struct {
	At   daedalus.Cell
	Bias Distance
}

// Field stores the cheapest accumulated entry cost from a set of Sources.
// Distances is row-major and uses Unreachable for impassable Cells and
// passable components that contain no Source. The private buffers let
// ComputeInto reuse all search storage after it has grown once.
type Field struct {
	Width     uint32
	Height    uint32
	Distances []Distance

	queue        bucketQueue
	generation   uint32
	settled      []uint32
	sourceStamps []uint32
}

// Compute creates the distance Field for grid and sources. Movement pays the
// Cost of the Cell being entered, and every Source remains fixed at its Bias.
func Compute(ctx context.Context, grid CostGrid, sources []Source) (Field, error) {
	var field Field
	if err := ComputeInto(ctx, &field, grid, sources); err != nil {
		return Field{}, err
	}
	return field, nil
}

// ComputeInto fills dst with the distance Field for grid and sources. Grid and
// source validation finishes before any reusable buffer grows. A subsequent
// call of the same or smaller size reuses Distances, stamps, and bucket slices.
func ComputeInto(ctx context.Context, dst *Field, grid CostGrid, sources []Source) error {
	if dst == nil {
		return fmt.Errorf("%w: destination Field is nil", daedalus.ErrInvalidNavigation)
	}
	if err := validateFieldRequest(grid, sources); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	cellCount := len(grid.Costs)
	dst.prepare(grid.Width, grid.Height, cellCount)
	for _, source := range sources {
		index, _ := grid.Index(source.At)
		dst.Distances[index] = source.Bias
		dst.sourceStamps[index] = dst.generation
		dst.queue.push(bucketEntry{index: index, distance: source.Bias})
	}

	var settleCount uint64
	for {
		entry, ok := dst.queue.pop()
		if !ok {
			break
		}
		if dst.Distances[entry.index] != entry.distance ||
			dst.settled[entry.index] == dst.generation {
			continue
		}
		if settleCount%fieldCancellationInterval == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		settleCount++
		dst.settled[entry.index] = dst.generation
		dst.expand(grid, entry)
	}
	return nil
}

// DistanceAt returns the distance at one Cell. A Cell outside the Field, or a
// slot absent from a short public Distances slice, is Unreachable.
func (field Field) DistanceAt(at daedalus.Cell) Distance {
	if at.X < 0 || at.Y < 0 || uint32(at.X) >= field.Width || uint32(at.Y) >= field.Height {
		return Unreachable
	}
	row := int64(at.Y) * int64(field.Width)
	index := row + int64(at.X)
	if index < 0 || index >= int64(len(field.Distances)) {
		return Unreachable
	}
	return field.Distances[index]
}

func validateFieldRequest(grid CostGrid, sources []Source) error {
	if err := grid.Validate(); err != nil {
		return err
	}
	if len(sources) > MaxSources {
		return fmt.Errorf("%w: source count %d exceeds %d", daedalus.ErrLimitExceeded, len(sources), MaxSources)
	}
	if len(sources) == 0 {
		return fmt.Errorf("%w: source set is empty", daedalus.ErrInvalidNavigation)
	}
	for index, source := range sources {
		cellIndex, inside := grid.Index(source.At)
		if !inside {
			return fmt.Errorf("%w: source %d at (%d,%d) is outside the grid", daedalus.ErrInvalidNavigation, index, source.At.X, source.At.Y)
		}
		if grid.Costs[cellIndex] == CostImpassable {
			return fmt.Errorf("%w: source %d at (%d,%d) is impassable", daedalus.ErrInvalidNavigation, index, source.At.X, source.At.Y)
		}
		if source.Bias < 0 || source.Bias > MaxDistance {
			return fmt.Errorf("%w: source %d bias %d is outside 0..%d", daedalus.ErrInvalidNavigation, index, source.Bias, MaxDistance)
		}
		for previous := 0; previous < index; previous++ {
			if sources[previous].At == source.At {
				return fmt.Errorf("%w: source %d duplicates Cell (%d,%d)", daedalus.ErrInvalidNavigation, index, source.At.X, source.At.Y)
			}
		}
	}
	return nil
}

func (field *Field) prepare(width, height uint32, cellCount int) {
	field.Width = width
	field.Height = height
	if cap(field.Distances) < cellCount {
		field.Distances = make([]Distance, cellCount)
	} else {
		field.Distances = field.Distances[:cellCount]
	}
	for index := range field.Distances {
		field.Distances[index] = Unreachable
	}
	if cap(field.settled) < cellCount {
		field.settled = make([]uint32, cellCount)
	} else {
		field.settled = field.settled[:cellCount]
	}
	if cap(field.sourceStamps) < cellCount {
		field.sourceStamps = make([]uint32, cellCount)
	} else {
		field.sourceStamps = field.sourceStamps[:cellCount]
	}
	field.nextGeneration()
	field.queue.reset()
}

func (field *Field) nextGeneration() {
	field.generation++
	if field.generation != 0 {
		return
	}
	clear(field.settled)
	clear(field.sourceStamps)
	field.generation = fieldFirstGeneration
}

func (field *Field) expand(grid CostGrid, entry bucketEntry) {
	width := int64(grid.Width)
	y := entry.index / width
	x := entry.index - y*width
	for direction := daedalus.DirectionNorth; direction < daedalus.Direction(fieldDirectionCount); direction++ {
		delta := direction.Delta()
		neighborX := x + int64(delta.X)
		neighborY := y + int64(delta.Y)
		if neighborX < 0 || neighborY < 0 || neighborX >= width || neighborY >= int64(grid.Height) {
			continue
		}
		row := neighborY * width
		neighborIndex := row + neighborX
		cost := grid.Costs[neighborIndex]
		if cost == CostImpassable || field.sourceStamps[neighborIndex] == field.generation ||
			field.settled[neighborIndex] == field.generation {
			continue
		}
		candidate := entry.distance + Distance(cost)
		previous := field.Distances[neighborIndex]
		if previous != Unreachable && previous <= candidate {
			continue
		}
		field.Distances[neighborIndex] = candidate
		field.queue.push(bucketEntry{index: neighborIndex, distance: candidate})
	}
}
