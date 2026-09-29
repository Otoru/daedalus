package vision

import (
	"context"
	"fmt"

	"github.com/Otoru/daedalus"
)

const (
	// MaxRadius is large enough to cover every legal skinny grid.
	MaxRadius                        = daedalus.MaxCells - 1
	fieldCancellationInterval uint64 = 256
)

// Field is a row-major, least-significant-bit-first visibility bitset.
type Field struct {
	Width   uint32
	Height  uint32
	Visible []byte

	rows []scanRow
}

type scanRow struct {
	depth      int64
	start, end slope
}

type slope struct {
	numerator   int64
	denominator int64
}

// Compute creates one visibility field from a transparent origin.
func Compute(ctx context.Context, grid OpacityGrid, origin daedalus.Cell, radius uint32) (Field, error) {
	var field Field
	if err := ComputeInto(ctx, &field, grid, origin, radius); err != nil {
		return Field{}, err
	}
	return field, nil
}

// ComputeInto fills dst and reuses its visibility and scan buffers after they
// have grown. Invalid requests leave dst unchanged.
func ComputeInto(ctx context.Context, dst *Field, grid OpacityGrid, origin daedalus.Cell, radius uint32) error {
	if dst == nil {
		return fmt.Errorf("%w: destination Field is nil", daedalus.ErrInvalidVisibility)
	}
	if err := grid.Validate(); err != nil {
		return err
	}
	if radius > MaxRadius {
		return fmt.Errorf("%w: radius %d exceeds %d", daedalus.ErrLimitExceeded, radius, MaxRadius)
	}
	if !grid.TransparentAt(origin) {
		return fmt.Errorf("%w: origin (%d,%d) is outside or opaque", daedalus.ErrInvalidVisibility, origin.X, origin.Y)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	cellCount := uint64(grid.Width) * uint64(grid.Height)
	byteCount := int((cellCount + 7) / 8)
	dst.Width, dst.Height = grid.Width, grid.Height
	if cap(dst.Visible) < byteCount {
		dst.Visible = make([]byte, byteCount)
	} else {
		dst.Visible = dst.Visible[:byteCount]
		clear(dst.Visible)
	}
	dst.rows = dst.rows[:0]
	setVisibleBit(dst.Visible, grid, origin)

	var visited uint64
	for direction := daedalus.DirectionNorth; direction <= daedalus.DirectionWest; direction++ {
		dst.rows = append(dst.rows, scanRow{depth: 1, start: slope{-1, 1}, end: slope{1, 1}})
		for len(dst.rows) != 0 {
			last := len(dst.rows) - 1
			row := dst.rows[last]
			dst.rows = dst.rows[:last]
			if row.depth > int64(radius) {
				continue
			}
			if err := scanRowInto(ctx, dst, grid, origin, radius, direction, row, &visited); err != nil {
				return err
			}
		}
	}
	return nil
}

// VisibleAt reports whether at is set. Outside cells and short public slices
// are invisible and never panic.
func (field Field) VisibleAt(at daedalus.Cell) bool {
	if at.X < 0 || at.Y < 0 || uint32(at.X) >= field.Width || uint32(at.Y) >= field.Height {
		return false
	}
	row := int64(at.Y) * int64(field.Width)
	index := row + int64(at.X)
	return visibleBit(field.Visible, index)
}

// VisibleCells appends set cells in canonical row-major order to dst.
func (field Field) VisibleCells(dst []daedalus.Cell) []daedalus.Cell {
	dst = dst[:0]
	for y := uint32(0); y < field.Height; y++ {
		for x := uint32(0); x < field.Width; x++ {
			at := daedalus.Cell{X: int32(x), Y: int32(y)}
			if field.VisibleAt(at) {
				dst = append(dst, at)
			}
		}
	}
	return dst
}

func scanRowInto(ctx context.Context, field *Field, grid OpacityGrid, origin daedalus.Cell, radius uint32, direction daedalus.Direction, row scanRow, visited *uint64) error {
	minCol := roundTiesUp(row.start, row.depth)
	maxCol := roundTiesDown(row.end, row.depth)
	previousWall := false
	hasPrevious := false
	for col := minCol; col <= maxCol; col++ {
		*visited = *visited + 1
		if *visited%fieldCancellationInterval == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		at := quadrantCell(origin, direction, row.depth, col)
		inBounds := at.X >= 0 && at.Y >= 0 && uint32(at.X) < grid.Width && uint32(at.Y) < grid.Height
		if !withinRadius(origin, at, radius) {
			continue
		}
		tileSlope := slope{2*col - 1, 2 * row.depth}
		symmetric := slope{col, row.depth}.between(row.start, row.end)
		wall := !inBounds || !grid.TransparentAt(at)
		if (wall || symmetric) && inBounds {
			setVisibleBit(field.Visible, grid, at)
		}
		if wall {
			if hasPrevious && !previousWall {
				field.rows = append(field.rows, scanRow{depth: row.depth + 1, start: row.start, end: tileSlope})
			}
			previousWall = true
			hasPrevious = true
			continue
		}
		if hasPrevious && previousWall {
			row.start = tileSlope
		}
		previousWall = false
		hasPrevious = true
	}
	if hasPrevious && !previousWall {
		field.rows = append(field.rows, scanRow{depth: row.depth + 1, start: row.start, end: row.end})
	}
	return nil
}

func quadrantCell(origin daedalus.Cell, direction daedalus.Direction, depth, col int64) daedalus.Cell {
	switch direction {
	case daedalus.DirectionNorth:
		return daedalus.Cell{X: origin.X + int32(col), Y: origin.Y - int32(depth)}
	case daedalus.DirectionEast:
		return daedalus.Cell{X: origin.X + int32(depth), Y: origin.Y + int32(col)}
	case daedalus.DirectionSouth:
		return daedalus.Cell{X: origin.X + int32(col), Y: origin.Y + int32(depth)}
	default:
		return daedalus.Cell{X: origin.X - int32(depth), Y: origin.Y + int32(col)}
	}
}

func roundTiesUp(value slope, multiplier int64) int64 {
	return floorDiv(2*value.numerator*multiplier+value.denominator, 2*value.denominator)
}

func roundTiesDown(value slope, multiplier int64) int64 {
	return ceilDiv(2*value.numerator*multiplier-value.denominator, 2*value.denominator)
}

func (value slope) less(other slope) bool {
	return value.numerator*other.denominator < other.numerator*value.denominator
}

func (value slope) between(start, end slope) bool {
	return !value.less(start) && !end.less(value)
}

func floorDiv(numerator, denominator int64) int64 {
	if numerator >= 0 {
		return numerator / denominator
	}
	return -((-numerator + denominator - 1) / denominator)
}

func ceilDiv(numerator, denominator int64) int64 { return -floorDiv(-numerator, denominator) }

func withinRadius(origin, at daedalus.Cell, radius uint32) bool {
	dx := int64(at.X) - int64(origin.X)
	dy := int64(at.Y) - int64(origin.Y)
	dx2 := dx * dx
	dy2 := dy * dy
	distance2 := dx2 + dy2
	radius64 := int64(radius)
	radius2 := radius64 * radius64
	return distance2 <= radius2
}

func setVisibleBit(bits []byte, grid OpacityGrid, at daedalus.Cell) {
	index, ok := grid.Index(at)
	if ok && index >= 0 && index/8 < int64(len(bits)) {
		bits[index>>3] |= byte(1) << uint(index&7)
	}
}

func visibleBit(bits []byte, index int64) bool {
	if index < 0 || index>>3 >= int64(len(bits)) {
		return false
	}
	return bits[index>>3]&(byte(1)<<uint(index&7)) != 0
}
