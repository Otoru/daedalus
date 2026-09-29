package pathfinding

import (
	"context"
	"fmt"

	"github.com/Otoru/daedalus"
)

// StepStatus distinguishes the five outcomes of reading one cell of a field.
// Moved, arrived, unreachable, and outside are different facts a game has to
// tell apart; a bare Direction would force one value to mean three of them.
// Blocked is the fifth: the cell is reachable, and no neighbour is strictly
// nearer, so the mover holds. That is the safe pocket of a flee field.
type StepStatus uint8

const (
	// StepStatusMoved means Direction is the step to take.
	StepStatusMoved StepStatus = iota
	// StepStatusArrived means the cell is a source and no neighbour is
	// strictly nearer. The mover is on a goal.
	StepStatusArrived
	// StepStatusUnreachable means the cell has no finite distance and neither
	// does any neighbour. There is nothing to descend to.
	StepStatusUnreachable
	// StepStatusOutside means the cell is not on the field.
	StepStatusOutside
	// StepStatusBlocked means the cell is reachable and no neighbour is
	// strictly nearer, and the cell is not a source. On a flee field this is
	// the safe pocket: hold position.
	StepStatusBlocked
)

// StepResult is the read of one cell.
//
// Direction is meaningful only when Status is StepStatusMoved. The tie-break
// is a rule, not an accident of the scan: neighbours are considered North,
// East, South, West, and the first direction that reaches the strict minimum
// wins. A caller who wants variety randomises on its own side. This package
// never draws.
//
// Distance is the distance of the neighbour stepped onto when Status is
// StepStatusMoved, the cell's own distance when Status is StepStatusArrived
// or StepStatusBlocked, and Unreachable when Status is StepStatusUnreachable
// or StepStatusOutside.
type StepResult struct {
	Direction daedalus.Direction
	Distance  Distance
	Status    StepStatus
}

// Step reads the next cardinal move from the field at from.
//
// The chosen neighbour must have a strictly smaller distance than from. On a
// computed field whose entry costs are at least MinCost that is automatic for
// every non-source. On a transformed field it is what produces
// StepStatusBlocked instead of a step that makes no progress.
//
// A position standing on an impassable cell is not unreachable. Its distance
// is Unreachable because nothing can enter it, and it still descends to its
// cheapest reachable neighbour under the same North, East, South, West
// tie-break. It is StepStatusUnreachable only when no neighbour is reachable
// either. Clone, then Set each creature to CostImpassable, queries from a
// cell the caller has just closed; without this rule every creature is told
// it cannot move.
func (field Field) Step(from daedalus.Cell) StepResult {
	return (&field).step(from)
}

func (field *Field) step(from daedalus.Cell) StepResult {
	if !field.inBounds(from) {
		return StepResult{Distance: Unreachable, Status: StepStatusOutside}
	}
	index := field.offset(from)
	if index < 0 || index >= int64(len(field.Distances)) {
		return StepResult{Distance: Unreachable, Status: StepStatusUnreachable}
	}
	self := field.Distances[index]
	selfFinite := field.distanceFinite(index)

	bestDistance := Distance(0)
	bestDirection := daedalus.DirectionNorth
	found := false
	for direction := daedalus.DirectionNorth; direction <= daedalus.DirectionWest; direction++ {
		neighborDistance, ok := field.descendingNeighbor(from, self, selfFinite, direction)
		if !ok {
			continue
		}
		if !found || neighborDistance < bestDistance {
			found = true
			bestDistance = neighborDistance
			bestDirection = direction
		}
	}
	if found {
		return StepResult{Direction: bestDirection, Distance: bestDistance, Status: StepStatusMoved}
	}
	if !selfFinite {
		return StepResult{Distance: Unreachable, Status: StepStatusUnreachable}
	}
	if field.sourceAt(index) {
		return StepResult{Distance: self, Status: StepStatusArrived}
	}
	return StepResult{Distance: self, Status: StepStatusBlocked}
}

// Steps reads Step for every position, in order. dst is reused when its
// capacity already holds the results; otherwise a new slice is allocated.
func (field Field) Steps(positions []daedalus.Cell, dst []StepResult) []StepResult {
	if cap(dst) < len(positions) {
		dst = make([]StepResult, len(positions))
	} else {
		dst = dst[:len(positions)]
	}
	view := &field
	for index, at := range positions {
		dst[index] = view.step(at)
	}
	return dst
}

// Path descends from from until it arrives at a source. The returned slice
// reuses dst and includes from. The bool is true when the last cell is a
// source. A blocked, unreachable, or outside read stops the walk, as does a
// walk longer than the field, so a transformed field cannot loop.
func (field Field) Path(from daedalus.Cell, dst []daedalus.Cell) ([]daedalus.Cell, bool) {
	return (&field).path(from, dst)
}

func (field *Field) path(from daedalus.Cell, dst []daedalus.Cell) ([]daedalus.Cell, bool) {
	dst = dst[:0]
	if !field.inBounds(from) {
		return dst, false
	}
	limit := int64(field.Width) * int64(field.Height)
	current := from
	for step := int64(0); step <= limit; step++ {
		dst = append(dst, current)
		result := field.step(current)
		if result.Status == StepStatusArrived {
			return dst, true
		}
		if result.Status != StepStatusMoved {
			return dst, false
		}
		delta := result.Direction.Delta()
		current = daedalus.Cell{X: current.X + delta.X, Y: current.Y + delta.Y}
	}
	return dst, false
}

// Route computes the field sourced at to and descends from from.
//
// With entry costs, d_to(from) and the cost of walking from→to differ by
// exactly cost(to)−cost(from). The chosen path is optimal either way; only
// the reported number shifts.
func Route(ctx context.Context, grid CostGrid, from, to daedalus.Cell, dst []daedalus.Cell) ([]daedalus.Cell, error) {
	if err := grid.Validate(); err != nil {
		return nil, err
	}
	if _, inside := grid.Index(from); !inside {
		return nil, fmt.Errorf("%w: route start (%d,%d) is outside the grid", daedalus.ErrInvalidNavigation, from.X, from.Y)
	}
	field, err := Compute(ctx, grid, []Source{{At: to}})
	if err != nil {
		return nil, err
	}
	path, arrived := field.Path(from, dst)
	if !arrived {
		return nil, fmt.Errorf("%w: no route from (%d,%d) to (%d,%d)", daedalus.ErrInvalidNavigation, from.X, from.Y, to.X, to.Y)
	}
	return path, nil
}

// descendingNeighbor reports the neighbour's distance when direction leaves
// from and that neighbour is a legal strict descent. A finite cell may step
// only to a strictly smaller distance. An unreachable cell — the impassable
// cell a creature is standing on — may step to any finite neighbour.
func (field *Field) descendingNeighbor(from daedalus.Cell, self Distance, selfFinite bool, direction daedalus.Direction) (Distance, bool) {
	delta := direction.Delta()
	neighbor := daedalus.Cell{X: from.X + delta.X, Y: from.Y + delta.Y}
	if !field.inBounds(neighbor) {
		return 0, false
	}
	index := field.offset(neighbor)
	if !field.distanceFinite(index) {
		return 0, false
	}
	neighborDistance := field.Distances[index]
	if selfFinite && neighborDistance >= self {
		return 0, false
	}
	return neighborDistance, true
}

// finiteStamp marks a finite distance that is stored as Unreachable's bits.
// A negative Scale of a distance of one by the flee factor truncates to -1,
// which is the sentinel. The stamp keeps that cell finite. The low bits are
// the field generation, so a later ComputeInto, which advances the
// generation and does not clear stamps, cannot mistake a stale mark for a
// finite cell.
const finiteStamp uint32 = 0x80000000

func (field *Field) distanceFinite(index int64) bool {
	if index < 0 || index >= int64(len(field.Distances)) {
		return false
	}
	if field.Distances[index] != Unreachable {
		return true
	}
	if field.generation == 0 || index >= int64(len(field.sourceStamps)) {
		return false
	}
	stamp := field.sourceStamps[index]
	if stamp == field.generation {
		return true
	}
	return stamp == field.generation|finiteStamp
}

func (field *Field) markFiniteSentinel(index int) {
	if field.generation == 0 {
		field.generation = fieldFirstGeneration
	}
	if len(field.sourceStamps) < len(field.Distances) {
		stamps := make([]uint32, len(field.Distances))
		copy(stamps, field.sourceStamps)
		field.sourceStamps = stamps
	}
	if field.sourceStamps[index] == field.generation {
		return
	}
	field.sourceStamps[index] = field.generation | finiteStamp
}

func (field *Field) inBounds(at daedalus.Cell) bool {
	if at.X < 0 || at.Y < 0 {
		return false
	}
	return uint32(at.X) < field.Width && uint32(at.Y) < field.Height
}

func (field *Field) offset(at daedalus.Cell) int64 {
	row := int64(at.Y) * int64(field.Width)
	return row + int64(at.X)
}

func (field *Field) sourceAt(index int64) bool {
	if field.generation == 0 || index < 0 || index >= int64(len(field.sourceStamps)) {
		return false
	}
	return field.sourceStamps[index] == field.generation
}
