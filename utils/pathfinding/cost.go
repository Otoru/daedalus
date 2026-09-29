package pathfinding

import (
	"fmt"

	"github.com/Otoru/daedalus"
)

// Cost is one Cell's entry cost: what a route pays to step onto that Cell,
// not what it pays to leave it. Entry cost is what makes several goals and
// the flee transform compose, and it makes a source's own cost irrelevant
// to its own field.
type Cost uint8

const (
	// CostImpassable is a Cell a route cannot step onto. It is zero, so the
	// zero CostGrid is entirely impassable and a short payload decodes to
	// walls rather than to floor.
	CostImpassable Cost = 0
	// MinCost is the smallest cost that still admits a step.
	MinCost Cost = 1
	// MaxCost is the largest entry cost a Cell can carry.
	MaxCost Cost = 255
)

// CostGrid is a Width × Height array of entry Costs in row-major order, the
// same order as daedalus.Grid.Cells: the Cost of (x, y) is Costs[y*Width+x].
type CostGrid struct {
	Width  uint32
	Height uint32
	Costs  []Cost
}

// CostRule prices one Cell from its state. DefaultCostRule is the rule
// NewCostGrid uses, and the rule NewCostGridFunc uses when the caller
// passes nil.
type CostRule func(daedalus.CellState) Cost

// LayoutCostRule prices one Cell from its state and its optional terrain
// definition. A nil terrain means that the cell has no terrain overlay.
type LayoutCostRule func(daedalus.CellState, *daedalus.TerrainDefinition) Cost

// DefaultCostRule is the only passability rule this module asserts.
// CellKindEmpty is CostImpassable, and every Room or Corridor Cell is
// MinCost. The root package deliberately ships no passability helper; this
// is that decision, made once, in the package that needs it. Any other kind
// is impassable.
func DefaultCostRule(state daedalus.CellState) Cost {
	switch state.Kind {
	case daedalus.CellKindRoom, daedalus.CellKindCorridor:
		return MinCost
	default:
		return CostImpassable
	}
}

// DefaultLayoutCostRule preserves the base-kind rule and then applies the
// terrain entry cost independently of terrain transparency. Empty and unknown
// base kinds remain impassable; a Room or Corridor without terrain costs one.
func DefaultLayoutCostRule(state daedalus.CellState, terrain *daedalus.TerrainDefinition) Cost {
	base := DefaultCostRule(state)
	if base == CostImpassable {
		return CostImpassable
	}
	if terrain == nil {
		return MinCost
	}
	return Cost(terrain.EntryCost)
}

// NewCostGrid prices layout with DefaultCostRule. A zero dimension or a
// product above daedalus.MaxCells is returned unfilled, so Validate rejects
// it and no oversized slice is allocated.
func NewCostGrid(layout daedalus.Layout) CostGrid {
	return NewCostGridFunc(layout, nil)
}

// NewCostGridFunc prices every Cell of layout with rule. A nil rule uses
// DefaultCostRule. Cells the layout does not store are priced as the zero
// CellState, which DefaultCostRule treats as impassable. The product and
// the sum that locate a Cell stay in separate statements inside Index.
func NewCostGridFunc(layout daedalus.Layout, rule CostRule) CostGrid {
	if rule == nil {
		rule = DefaultCostRule
	}
	grid := CostGrid{Width: layout.Grid.Width, Height: layout.Grid.Height}
	if grid.Width == 0 || grid.Height == 0 {
		return grid
	}
	product := uint64(grid.Width) * uint64(grid.Height)
	if product > uint64(daedalus.MaxCells) {
		return grid
	}
	costs := make([]Cost, product)
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
			costs[int(index)] = rule(state)
		}
	}
	grid.Costs = costs
	return grid
}

// NewTerrainCostGrid prices layout with DefaultLayoutCostRule. Unlike the
// legacy constructors, it reads the optional terrain layer.
func NewTerrainCostGrid(layout daedalus.Layout) CostGrid {
	return NewTerrainCostGridFunc(layout, nil)
}

// NewTerrainCostGridFunc prices every Cell with rule. A nil rule uses
// DefaultLayoutCostRule. Invalid terrain rejects the whole layer before any
// cell is priced, but returns a valid, fully impassable CostGrid.
func NewTerrainCostGridFunc(layout daedalus.Layout, rule LayoutCostRule) CostGrid {
	if rule == nil {
		rule = DefaultLayoutCostRule
	}
	grid := CostGrid{Width: layout.Grid.Width, Height: layout.Grid.Height}
	if grid.Width == 0 || grid.Height == 0 {
		return grid
	}
	product := uint64(grid.Width) * uint64(grid.Height)
	if product > uint64(daedalus.MaxCells) {
		return grid
	}
	costs := make([]Cost, product)
	if layout.Grid.ValidateTerrain() != nil {
		grid.Costs = costs
		return grid
	}
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
			terrain, _ := layout.Grid.TerrainAt(at)
			costs[int(index)] = rule(state, terrain)
		}
	}
	grid.Costs = costs
	return grid
}

// Index reports the row-major position of at and whether at lies inside the
// grid. Coordinates outside the grid, including negatives, are not inside.
// The product and the sum are separate statements: the index is
// y*Width+x, and nothing folds those two operations into one expression.
func (grid CostGrid) Index(at daedalus.Cell) (int64, bool) {
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

// At returns the entry cost of at. A Cell outside the grid, or a Cell whose
// slot the Costs slice does not hold, is CostImpassable. It does not panic.
func (grid CostGrid) At(at daedalus.Cell) Cost {
	index, ok := grid.Index(at)
	if !ok || int(index) >= len(grid.Costs) {
		return CostImpassable
	}
	return grid.Costs[int(index)]
}

// Set writes cost onto at and reports whether the Cell has a slot. Outside
// the grid, or past the end of a short Costs slice, Set changes nothing and
// returns false. Clone plus Set is how a caller marks a creature as an
// obstacle for one turn without disturbing the static terrain.
func (grid *CostGrid) Set(at daedalus.Cell, cost Cost) bool {
	index, ok := grid.Index(at)
	if !ok || int(index) >= len(grid.Costs) {
		return false
	}
	grid.Costs[int(index)] = cost
	return true
}

// Clone returns a CostGrid whose Costs slice does not alias the receiver.
// Mutating either side leaves the other untouched. A nil Costs stays nil.
func (grid CostGrid) Clone() CostGrid {
	cloned := CostGrid{Width: grid.Width, Height: grid.Height}
	if grid.Costs == nil {
		return cloned
	}
	cloned.Costs = make([]Cost, len(grid.Costs))
	copy(cloned.Costs, grid.Costs)
	return cloned
}

// Validate reports a malformed grid as ErrInvalidNavigation and a grid over
// daedalus.MaxCells as ErrLimitExceeded. A zero dimension is malformed even
// when the Costs length happens to equal the product, which is zero. Both
// errors are wrapped and answer errors.Is. The message names the offending
// value.
func (grid CostGrid) Validate() error {
	if grid.Width == 0 || grid.Height == 0 {
		return fmt.Errorf("%w: CostGrid width %d height %d is zero", daedalus.ErrInvalidNavigation, grid.Width, grid.Height)
	}
	product := uint64(grid.Width) * uint64(grid.Height)
	if product > uint64(daedalus.MaxCells) {
		return fmt.Errorf("%w: CostGrid has %d cells, above %d", daedalus.ErrLimitExceeded, product, daedalus.MaxCells)
	}
	if uint64(len(grid.Costs)) != product {
		return fmt.Errorf("%w: CostGrid costs length %d disagrees with width %d height %d", daedalus.ErrInvalidNavigation, len(grid.Costs), grid.Width, grid.Height)
	}
	return nil
}

// RoomSources returns a detached copy of the Room footprint, in Room.Cells
// order, so the whole Room can be a goal. bias is the initial distance
// offset shared by those Cells when the caller builds sources. A negative
// bias is out of range — the offset is an unsigned value — and the result
// is nil. Every non-negative bias returns the footprint. The copy is not
// truncated at MaxSources and duplicates are kept: a count ceiling is
// ErrLimitExceeded, and a duplicate source is ErrInvalidNavigation, both
// decided when the request is checked.
func RoomSources(room daedalus.Room, bias int32) []daedalus.Cell {
	if bias < 0 {
		return nil
	}
	cells := make([]daedalus.Cell, len(room.Cells))
	copy(cells, room.Cells)
	return cells
}
