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

	scan := quadrantScan{ctx: ctx, field: dst, grid: grid, origin: origin, radius: radius}
	for direction := daedalus.DirectionNorth; direction <= daedalus.DirectionWest; direction++ {
		scan.direction = direction
		dst.rows = append(dst.rows, scanRow{depth: 1, start: slope{-1, 1}, end: slope{1, 1}})
		for len(dst.rows) != 0 {
			last := len(dst.rows) - 1
			row := dst.rows[last]
			dst.rows = dst.rows[:last]
			if row.depth > int64(radius) {
				continue
			}
			if err := scan.scanRow(row); err != nil {
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

// quadrantScan carries the fixed inputs of one quadrant sweep plus the shared
// cancellation counter, so scanning a row takes a single receiver.
type quadrantScan struct {
	ctx       context.Context
	field     *Field
	grid      OpacityGrid
	origin    daedalus.Cell
	radius    uint32
	direction daedalus.Direction
	visited   uint64
}

// rowEdges tracks the wall transitions seen so far in one row, which is what
// splits the row into the child rows scanned at the next depth.
type rowEdges struct {
	previousWall bool
	hasPrevious  bool
}

// tick counts one visited cell and reports cancellation on the sampled cells.
func (scan *quadrantScan) tick() error {
	scan.visited++
	if scan.visited%fieldCancellationInterval != 0 {
		return nil
	}
	return scan.ctx.Err()
}

// scanRow sweeps one row of the quadrant, revealing its cells and queueing the
// child rows left by the transparent runs it finds.
func (scan *quadrantScan) scanRow(row scanRow) error {
	minCol := roundTiesUp(row.start, row.depth)
	maxCol := roundTiesDown(row.end, row.depth)
	var edges rowEdges
	for col := minCol; col <= maxCol; col++ {
		if err := scan.tick(); err != nil {
			return err
		}
		at := quadrantCell(scan.origin, scan.direction, row.depth, col)
		if !withinRadius(scan.origin, at, scan.radius) {
			continue
		}
		wall := scan.revealCell(at, row, col)
		scan.stepColumn(&row, &edges, wall, slope{2*col - 1, 2 * row.depth})
	}
	if edges.hasPrevious && !edges.previousWall {
		scan.field.rows = append(scan.field.rows, scanRow{depth: row.depth + 1, start: row.start, end: row.end})
	}
	return nil
}

// revealCell sets at when it is a wall or symmetrically visible, and reports
// whether it blocks sight. A cell outside the grid counts as a wall.
func (scan *quadrantScan) revealCell(at daedalus.Cell, row scanRow, col int64) bool {
	inBounds := at.X >= 0 && at.Y >= 0 && uint32(at.X) < scan.grid.Width && uint32(at.Y) < scan.grid.Height
	wall := !inBounds || !scan.grid.TransparentAt(at)
	symmetric := slope{col, row.depth}.between(row.start, row.end)
	if inBounds && (wall || symmetric) {
		setVisibleBit(scan.field.Visible, scan.grid, at)
	}
	return wall
}

// stepColumn records the transition at tileSlope: a transparent run that ends
// at a wall queues a child row, and a wall run that ends narrows row.start.
func (scan *quadrantScan) stepColumn(row *scanRow, edges *rowEdges, wall bool, tileSlope slope) {
	switch {
	case wall && edges.hasPrevious && !edges.previousWall:
		scan.field.rows = append(scan.field.rows, scanRow{depth: row.depth + 1, start: row.start, end: tileSlope})
	case !wall && edges.hasPrevious && edges.previousWall:
		row.start = tileSlope
	}
	edges.previousWall = wall
	edges.hasPrevious = true
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
