// Package vision packs terrain transparency for a Layout.
//
// utils is the umbrella for utilities that consume a Layout. Vision sits
// beside pathfinding, not inside it. This package imports the root and
// nothing else outside the standard library. It uses Cell, CellKind,
// CellState, Layout, and MaxCells. The root imports nothing at all,
// including this package. That dependency runs one way.
//
// Arithmetic is integer-only. Every index is an int64. There is no
// floating-point value and no call into math. That removes the
// fused-multiply-add hazard this repository otherwise has to split by
// hand, because that contraction applies only to floating-point operands.
// The index is still two statements, y*Width and then +x, matching the
// house style for a product that feeds a coordinate.
//
// The vocabulary introduced here is OpacityGrid and OpacityRule. Cost and
// opacity are independent facts: a closed glass door can be impassable and
// still transparent, and smoke can be cheap to cross and opaque. Reusing a
// cost grid would make the two utilities disagree with the terrain.
//
// The stored bit is transparent, not opaque. Bit i of a Width by Height
// grid is Transparent[i>>3] & (1<<(i&7)), with i = y*Width+x in row-major
// order, least-significant bit first inside each byte. A zero byte is
// solid terrain. A caller who forgets to fill the grid, and a short or
// truncated payload, reads as opaque rather than as an open view. Outside
// cells are opaque too. Failing closed is the point: a forgotten grid must
// not become an open view.
//
// The slice length is exactly (Width*Height+7)/8. Unused high bits of the
// final byte are not cells and must be zero, so one terrain has one
// encoding. Validate rejects a zero dimension, a length that disagrees
// with that formula, or a nonzero padding bit as ErrInvalidVisibility. A
// product above daedalus.MaxCells is ErrLimitExceeded. Both are wrapped,
// and both answer errors.Is. There is no separate dimension constant; the
// cell ceiling is MaxCells. The SDK grid has no separate side ceiling.
//
// DefaultOpacityRule is the conversion of a generated Layout. CellKindRoom
// and CellKindCorridor are transparent. CellKindEmpty and any kind this
// rule does not name are opaque. The root package ships no visibility
// helper. This is that decision, made once, in the package that needs it.
// Entities do not become opaque by default; whether creatures occlude one
// another is game policy, applied with NewOpacityGridFunc or by cloning
// the grid and editing bits.
//
// Clone followed by SetTransparent is how a caller marks a dynamic blocker
// for one query without disturbing the static terrain. Clone detaches the
// Transparent slice; SetTransparent on the clone writes only the clone,
// and SetTransparent on the original writes only the original. A nil
// Transparent stays nil.
package vision
