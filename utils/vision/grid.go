package vision

import (
	"fmt"

	"github.com/Otoru/daedalus"
)

// OpacityGrid is a Width × Height array of transparency bits in row-major
// order, the same order as daedalus.Grid.Cells. The stored bit is
// transparent, not opaque: bit i is Transparent[i>>3] & (1<<(i&7)), with
// i = y*Width+x, least-significant bit first inside each byte. A zero byte
// is solid terrain.
type OpacityGrid struct {
	Width       uint32
	Height      uint32
	Transparent []byte
}

// OpacityRule reports whether one Cell is transparent. DefaultOpacityRule is
// the rule NewOpacityGrid uses, and the rule NewOpacityGridFunc uses when
// the caller passes nil.
type OpacityRule func(daedalus.CellState) bool

// DefaultOpacityRule is the conversion of a generated Layout. CellKindRoom
// and CellKindCorridor are transparent. CellKindEmpty and any kind this
// rule does not name are opaque. Entities do not occlude by default.
func DefaultOpacityRule(state daedalus.CellState) bool {
	switch state.Kind {
	case daedalus.CellKindRoom, daedalus.CellKindCorridor:
		return true
	default:
		return false
	}
}

// NewOpacityGrid packs layout with DefaultOpacityRule. A zero dimension or a
// product above daedalus.MaxCells is returned unfilled, so Validate rejects
// it and no oversized slice is allocated.
func NewOpacityGrid(layout daedalus.Layout) OpacityGrid {
	return NewOpacityGridFunc(layout, nil)
}

// NewOpacityGridFunc packs every Cell of layout with rule. A nil rule uses
// DefaultOpacityRule. Cells the layout does not store are read as the zero
// CellState, which DefaultOpacityRule treats as opaque. The product and the
// sum that locate a Cell stay in separate statements inside Index.
func NewOpacityGridFunc(layout daedalus.Layout, rule OpacityRule) OpacityGrid {
	if rule == nil {
		rule = DefaultOpacityRule
	}
	grid := OpacityGrid{Width: layout.Grid.Width, Height: layout.Grid.Height}
	if grid.Width == 0 || grid.Height == 0 {
		return grid
	}
	product := uint64(grid.Width) * uint64(grid.Height)
	if product > uint64(daedalus.MaxCells) {
		return grid
	}
	bits := make([]byte, (product+7)/8)
	cells := layout.Grid.Cells
	for y := uint32(0); y < grid.Height; y++ {
		for x := uint32(0); x < grid.Width; x++ {
			at := daedalus.Cell{X: int32(x), Y: int32(y)}
			index, ok := grid.Index(at)
			if !ok {
				continue
			}
			var state daedalus.CellState
			if int(index) < len(cells) {
				state = cells[int(index)]
			}
			if rule(state) {
				setTransparentBit(bits, index)
			}
		}
	}
	grid.Transparent = bits
	return grid
}

// Index reports the row-major position of at and whether at lies inside the
// grid. Coordinates outside the grid, including negatives, are not inside.
// The product and the sum are separate statements: the index is y*Width+x,
// and nothing folds those two operations into one expression.
func (grid OpacityGrid) Index(at daedalus.Cell) (int64, bool) {
	if at.X < 0 || at.Y < 0 {
		return 0, false
	}
	if uint32(at.X) >= grid.Width || uint32(at.Y) >= grid.Height {
		return 0, false
	}
	row := int64(at.Y) * int64(grid.Width)
	index := row + int64(at.X)
	return index, true
}

// TransparentAt reports whether at is transparent. A Cell outside the grid,
// or a Cell whose byte the Transparent slice does not hold, is opaque. It
// does not panic.
func (grid OpacityGrid) TransparentAt(at daedalus.Cell) bool {
	index, ok := grid.Index(at)
	if !ok {
		return false
	}
	return transparentBit(grid.Transparent, index)
}

// SetTransparent writes transparent onto at and reports whether the Cell has
// a byte. Outside the grid, or past the end of a short Transparent slice,
// SetTransparent changes nothing and returns false. Clone plus
// SetTransparent is how a caller marks a dynamic blocker for one query
// without disturbing the static terrain.
func (grid *OpacityGrid) SetTransparent(at daedalus.Cell, transparent bool) bool {
	index, ok := grid.Index(at)
	if !ok || !bitInRange(grid.Transparent, index) {
		return false
	}
	if transparent {
		setTransparentBit(grid.Transparent, index)
	} else {
		clearTransparentBit(grid.Transparent, index)
	}
	return true
}

// Clone returns an OpacityGrid whose Transparent slice does not alias the
// receiver. Mutating either side leaves the other untouched. A nil
// Transparent stays nil.
func (grid OpacityGrid) Clone() OpacityGrid {
	cloned := OpacityGrid{Width: grid.Width, Height: grid.Height}
	if grid.Transparent == nil {
		return cloned
	}
	cloned.Transparent = make([]byte, len(grid.Transparent))
	copy(cloned.Transparent, grid.Transparent)
	return cloned
}

// Validate reports a malformed grid as ErrInvalidVisibility and a grid over
// daedalus.MaxCells as ErrLimitExceeded. A zero dimension is malformed even
// when the byte length happens to equal the product, which is zero. Unused
// high bits of the final byte must be clear: they are not cells, and a set
// one would give one terrain two encodings. Both errors are wrapped and
// answer errors.Is. The message names the offending value.
func (grid OpacityGrid) Validate() error {
	if grid.Width == 0 || grid.Height == 0 {
		return fmt.Errorf("%w: OpacityGrid width %d height %d is zero", daedalus.ErrInvalidVisibility, grid.Width, grid.Height)
	}
	product := uint64(grid.Width) * uint64(grid.Height)
	if product > uint64(daedalus.MaxCells) {
		return fmt.Errorf("%w: OpacityGrid has %d cells, above %d", daedalus.ErrLimitExceeded, product, daedalus.MaxCells)
	}
	byteLen := (product + 7) / 8
	if uint64(len(grid.Transparent)) != byteLen {
		return fmt.Errorf("%w: OpacityGrid transparent length %d disagrees with width %d height %d", daedalus.ErrInvalidVisibility, len(grid.Transparent), grid.Width, grid.Height)
	}
	spare := spareHighBits(product, grid.Transparent[len(grid.Transparent)-1])
	if spare != 0 {
		return fmt.Errorf("%w: OpacityGrid padding bits %#02x are set", daedalus.ErrInvalidVisibility, spare)
	}
	return nil
}

// transparentBit reads cell index. A missing byte is opaque: a short payload
// must not become an open view.
func transparentBit(bits []byte, index int64) bool {
	if !bitInRange(bits, index) {
		return false
	}
	byteIndex, mask := bitPlace(index)
	return bits[byteIndex]&mask != 0
}

func setTransparentBit(bits []byte, index int64) {
	byteIndex, mask := bitPlace(index)
	bits[byteIndex] |= mask
}

func clearTransparentBit(bits []byte, index int64) {
	byteIndex, mask := bitPlace(index)
	bits[byteIndex] &^= mask
}

func bitInRange(bits []byte, index int64) bool {
	byteIndex := index >> 3
	return byteIndex >= 0 && int(byteIndex) < len(bits)
}

// bitPlace splits a row-major cell index into a byte and an LSB-first mask.
// Bit 0 of a byte is the first cell in that byte, so a zero byte stays solid.
func bitPlace(index int64) (int64, byte) {
	shift := uint(index & 7)
	return index >> 3, byte(1) << shift
}

// spareHighBits returns the bits of last that are not cells. When product is
// a multiple of eight the final byte is full, and the high-bit mask would
// falsely condemn every set bit, so that case has no padding.
func spareHighBits(product uint64, last byte) byte {
	used := product & 7
	if used == 0 {
		return 0
	}
	mask := ^byte((1 << used) - 1)
	return last & mask
}
