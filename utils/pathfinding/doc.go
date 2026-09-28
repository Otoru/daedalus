// Package pathfinding prices movement on a Layout.
//
// utils is the umbrella for utilities that consume a Layout. Pathfinding is
// the first of them; later siblings sit beside this package, not inside it.
// This package imports the root and nothing else outside the standard
// library. It uses Cell, CellKind, CellState, Layout, Room, and MaxCells.
// Direction joins that list when a later fragment computes the field. The
// root imports nothing at all, including this package. That dependency runs
// one way.
//
// Arithmetic is integer-only. Cost is a uint8 and every index is an int64.
// There is no floating-point value and no call into math. That removes the
// fused-multiply-add hazard this repository otherwise has to split by hand,
// because that contraction applies only to floating-point operands. The
// index is still two statements, y*Width and then +x, matching the house
// style for a product that feeds a coordinate.
//
// The vocabulary introduced here is Cost, CostGrid, and CostRule. A Cost is
// one Cell's entry cost: what a route pays to step onto that Cell, not what
// it pays to leave it. Entry cost is what makes several goals and the flee
// transform compose, and it makes a source's own cost irrelevant to its own
// field. The opposite convention — paying to leave a Cell — is the intuitive
// one, and it is the wrong one.
//
// CostImpassable is zero, so the zero CostGrid is entirely impassable. A
// caller who forgets to fill the grid gets no movement rather than an open
// plain, and a short or truncated payload decodes to walls rather than to
// floor. Failing closed is the point. MinCost is the cheapest step that
// still moves. MaxCost is the highest entry cost a Cell can carry.
//
// DefaultCostRule is the only passability rule this module asserts.
// CellKindEmpty is CostImpassable, and every Room or Corridor Cell is
// MinCost. The root package deliberately ships no passability helper. This
// is that decision, made once, in the package that needs it. A Cell kind
// this rule does not name is impassable as well.
//
// Clone followed by Set is how a caller marks a creature as an obstacle for
// one turn without disturbing the static terrain. Clone detaches the Costs
// slice; Set on the clone writes only the clone, and Set on the original
// writes only the original.
//
// A CostGrid is Width by Height in row-major order, the same order as
// daedalus.Grid.Cells: the Cost of (x, y) is Costs[y*Width+x]. Validate
// reports a zero dimension, or a Costs length that disagrees with that
// product, as ErrInvalidNavigation. A product above daedalus.MaxCells is
// ErrLimitExceeded. Both are wrapped, and both answer errors.Is. There is
// no separate dimension constant; the cell ceiling is MaxCells.
//
// MaxSources, MaxQueries, and MaxStepsPerCall bound one navigation call.
// MaxSources equals daedalus.MaxFootprintCells so a whole Room fits as a
// goal. RoomSources copies that footprint. Count and dimension ceilings are
// ErrLimitExceeded; a source outside the grid, a source on an impassable
// Cell, a duplicate source, or a bias out of range is ErrInvalidNavigation.
//
// The distance field, the flee transform, and the recipes that call them
// are not in this package yet.
package pathfinding
