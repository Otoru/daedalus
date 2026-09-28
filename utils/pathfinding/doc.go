// Package pathfinding prices movement on a Layout.
//
// utils is the umbrella for utilities that consume a Layout. Pathfinding is
// the first of them; later siblings sit beside this package, not inside it.
// This package imports the root and nothing else outside the standard
// library. It uses Cell, CellKind, CellState, Layout, Room, Direction, and
// MaxCells. The root imports nothing at all, including this package. That
// dependency runs one way.
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
// Step reads a field. The tie-break is normative: neighbours are scanned
// North, East, South, West, and the first direction that reaches the strict
// minimum wins. A caller who wants variety randomises on its own side; this
// package never draws. The chosen neighbour must be strictly nearer than
// the cell itself. On a computed field, entry costs of at least MinCost
// make that automatic. On a transformed field, a cell with no such
// neighbour is Blocked: reachable, but already as good as this field gets,
// so the mover holds. A cell standing on an impassable distance is not
// unreachable. It steps to its cheapest reachable neighbour, and it is
// Unreachable only when no neighbour is reachable either. The workflow that
// requires this is Clone, then Set each creature's cell to CostImpassable,
// then query from the cell just closed.
//
// Route sources the field at the destination and descends from the start.
// With entry costs, the distance at the start and the cost of walking
// start→destination differ by exactly cost(destination)−cost(start). The
// path is optimal either way; only the reported number shifts.
//
// Scale multiplies every finite distance and divides, in int64, in separate
// statements, truncating toward zero. Unreachable stays Unreachable. A
// negative numerator inverts the field. A result outside the int32 band
// fails the whole call with ErrInvalidNavigation rather than wrapping. A
// finite quotient of -1 keeps the cell finite: that is what the flee factor
// does to a distance of one, and treating it as Unreachable would open a
// hole beside the threat. Rescan lowers distances until
// every cell satisfies d(c) ≤ d(n) + cost(c). It never raises a value and
// never gives an unreachable cell a finite one, so it terminates. Flee is
// Scale then Rescan. Inversion alone is not a flee map: it walks into
// whichever dead end is furthest from the threat. The rescan is what makes
// that far cell's low value propagate back down the corridor, so descent
// leads away and around. The factor callers want is −12/10. Query leaves
// a zero numerator or denominator as that factor.
//
// Answer is the batch entry point a turn actually calls. One cost grid
// travels once, however many goal sets the turn has, and one scratch field
// is reused across those queries. MaxQueries, MaxSources per query, and
// MaxStepsPerCall summed across every query are ErrLimitExceeded, checked
// before any result is allocated.
//
// Chase, flee, explore, and weighted goals are the same field read four
// ways, not four algorithms. Chase sources the quarry and steps downhill.
// Flee sources the threat, applies Flee, and steps downhill on the result.
// Explore sources the frontier not yet visited and steps toward the nearest
// cell of it. Weighted goals are several sources with different biases; a
// higher bias yields to a nearer low-bias goal, and the read is still Step.
package pathfinding
