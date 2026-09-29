package pathfinding

import (
	"context"
	"fmt"

	"github.com/Otoru/daedalus"
)

const (
	// Distance is an int32. A scaled or shifted value outside this band would
	// wrap, so the call fails instead. Unreachable sits inside the band and
	// is rejected on its own: a finite distance must not become the sentinel.
	maxScaledDistance int64 = 2147483647
	minScaledDistance int64 = -2147483648

	// Default flee ratio. Query substitutes these when the corresponding
	// argument is zero. Inversion by a factor past one, not by minus one, is
	// what gives the rescan a slope to propagate.
	defaultFleeNumerator   int32 = -12
	defaultFleeDenominator int32 = 10
)

// Scale multiplies every finite distance by numerator and divides by
// denominator. The product and the quotient are separate int64 statements,
// and the quotient truncates toward zero. Unreachable is preserved. A
// negative numerator inverts the field. The whole call fails with
// ErrInvalidNavigation, and the receiver is left untouched, if the
// denominator is zero or any quotient leaves the int32 band. A finite
// quotient of -1 is kept, and marked so it is not read as Unreachable: the
// flee factor truncates a distance of one to that sentinel's bits.
func (field Field) Scale(numerator, denominator int32) (Field, error) {
	scaled := Field{
		Width:        field.Width,
		Height:       field.Height,
		Distances:    append([]Distance(nil), field.Distances...),
		generation:   field.generation,
		sourceStamps: append([]uint32(nil), field.sourceStamps...),
	}
	if err := scaled.scaleInPlace(numerator, denominator); err != nil {
		return Field{}, err
	}
	return scaled, nil
}

// Rescan lowers distances until every finite cell satisfies
// d(c) ≤ d(n) + cost(c) for each passable neighbour n. It only lowers
// values, and it never writes a finite distance into an unreachable cell, so
// it terminates. The grid must be the same shape as the field.
//
// After a negative Scale the distances are negative, and the bucket queue
// needs non-negative keys. Rescan rebases by the minimum finite distance,
// relaxes, and subtracts that offset back. The shift is integer and exact.
func Rescan(ctx context.Context, field *Field, grid CostGrid) error {
	if field == nil {
		return fmt.Errorf("%w: destination Field is nil", daedalus.ErrInvalidNavigation)
	}
	if err := grid.Validate(); err != nil {
		return err
	}
	if grid.Width != field.Width || grid.Height != field.Height || len(grid.Costs) != len(field.Distances) {
		return fmt.Errorf("%w: rescan grid %d x %d does not match field %d x %d", daedalus.ErrInvalidNavigation, grid.Width, grid.Height, field.Width, field.Height)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	offset, err := rebaseOffset(field)
	if err != nil {
		return err
	}
	if err := shiftFinite(field, offset); err != nil {
		return err
	}
	relaxErr := relaxNonNegative(ctx, field, grid)
	restoreErr := shiftFinite(field, -offset)
	if relaxErr != nil {
		return relaxErr
	}
	return restoreErr
}

// Flee is Scale by numerator/denominator followed by Rescan, the composition
// callers actually want. The classic factor is -12, 10.
//
// Inversion alone is not a flee map. It sends the mover into whichever dead
// end is furthest from the threat, because that dead end holds the lowest
// inverted distance. The rescan is what makes the far end of a corridor
// propagate its low value back down the corridor, so descent leads away and
// around. The receiver is unchanged when Scale or Rescan fails.
func Flee(ctx context.Context, field *Field, grid CostGrid, numerator, denominator int32) error {
	if field == nil {
		return fmt.Errorf("%w: destination Field is nil", daedalus.ErrInvalidNavigation)
	}
	original := append([]Distance(nil), field.Distances...)
	if err := field.scaleInPlace(numerator, denominator); err != nil {
		return err
	}
	if err := Rescan(ctx, field, grid); err != nil {
		copy(field.Distances, original)
		return err
	}
	return nil
}

func (field *Field) scaleInPlace(numerator, denominator int32) error {
	if denominator == 0 {
		return fmt.Errorf("%w: scale denominator is zero", daedalus.ErrInvalidNavigation)
	}
	for index := range field.Distances {
		if !field.distanceFinite(int64(index)) {
			continue
		}
		if _, err := scaleOne(field.Distances[index], numerator, denominator); err != nil {
			return err
		}
	}
	for index := range field.Distances {
		if !field.distanceFinite(int64(index)) {
			continue
		}
		scaled, err := scaleOne(field.Distances[index], numerator, denominator)
		if err != nil {
			return err
		}
		field.Distances[index] = scaled
		if scaled == Unreachable {
			field.markFiniteSentinel(index)
		}
	}
	return nil
}

func scaleOne(distance Distance, numerator, denominator int32) (Distance, error) {
	product := int64(distance) * int64(numerator)
	quotient := product / int64(denominator)
	if quotient > maxScaledDistance || quotient < minScaledDistance {
		return 0, fmt.Errorf("%w: scaled distance %d is outside the representable band", daedalus.ErrInvalidNavigation, quotient)
	}
	return Distance(quotient), nil
}

func rebaseOffset(field *Field) (int64, error) {
	minimum, finite := Distance(0), false
	for index := range field.Distances {
		if !field.distanceFinite(int64(index)) {
			continue
		}
		distance := field.Distances[index]
		if !finite || distance < minimum {
			minimum = distance
			finite = true
		}
	}
	if !finite || minimum >= 0 {
		return 0, nil
	}
	return -int64(minimum), nil
}

func shiftFinite(field *Field, delta int64) error {
	if delta == 0 {
		return nil
	}
	for index := range field.Distances {
		if !field.distanceFinite(int64(index)) {
			continue
		}
		if _, err := shiftOne(field.Distances[index], delta); err != nil {
			return err
		}
	}
	for index := range field.Distances {
		if !field.distanceFinite(int64(index)) {
			continue
		}
		shifted, err := shiftOne(field.Distances[index], delta)
		if err != nil {
			return err
		}
		field.Distances[index] = shifted
		if shifted == Unreachable {
			field.markFiniteSentinel(index)
		}
	}
	return nil
}

func shiftOne(distance Distance, delta int64) (Distance, error) {
	shifted := int64(distance) + delta
	if shifted > maxScaledDistance || shifted < minScaledDistance {
		return 0, fmt.Errorf("%w: distance %d is outside the representable band", daedalus.ErrInvalidNavigation, shifted)
	}
	return Distance(shifted), nil
}

func relaxNonNegative(ctx context.Context, field *Field, grid CostGrid) error {
	field.resetQueue(len(field.Distances))
	for index := range field.Distances {
		if !field.distanceFinite(int64(index)) {
			continue
		}
		field.queue.push(bucketEntry{index: int64(index), distance: field.Distances[index]})
	}

	width := int64(grid.Width)
	var settled uint64
	for {
		entry, ok := field.queue.pop()
		if !ok {
			return nil
		}
		if field.Distances[entry.index] != entry.distance {
			continue
		}
		if settled%fieldCancellationInterval == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		settled++
		if err := relaxEntry(field, grid, width, entry); err != nil {
			return err
		}
	}
}

func relaxEntry(field *Field, grid CostGrid, width int64, entry bucketEntry) error {
	y := entry.index / width
	x := entry.index - y*width
	for direction := daedalus.DirectionNorth; direction <= daedalus.DirectionWest; direction++ {
		delta := direction.Delta()
		neighborX := x + int64(delta.X)
		neighborY := y + int64(delta.Y)
		if neighborX < 0 || neighborY < 0 || neighborX >= width || neighborY >= int64(grid.Height) {
			continue
		}
		row := neighborY * width
		neighborIndex := row + neighborX
		cost := grid.Costs[neighborIndex]
		if cost == CostImpassable || !field.distanceFinite(neighborIndex) {
			continue
		}
		candidate, err := descendCandidate(entry.distance, cost)
		if err != nil {
			return err
		}
		if candidate >= field.Distances[neighborIndex] {
			continue
		}
		field.Distances[neighborIndex] = candidate
		field.queue.push(bucketEntry{index: neighborIndex, distance: candidate})
	}
	return nil
}

func descendCandidate(from Distance, cost Cost) (Distance, error) {
	sum := int64(from) + int64(cost)
	if sum > maxScaledDistance || sum < minScaledDistance || sum == int64(Unreachable) {
		return 0, fmt.Errorf("%w: relaxed distance %d is outside the representable band", daedalus.ErrInvalidNavigation, sum)
	}
	return Distance(sum), nil
}
