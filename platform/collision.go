package platform

import "math"

// This file holds the geometry side of the M1 oracle: the immutable index
// derived from one room's grid, and the analytic continuous-collision
// primitives every manoeuvre is checked against.
//
// # The body and the asymmetric expansion
//
// The character's reference point is the CENTRE OF ITS FEET. Its body is the
// axis-aligned box [x-r, x+r] x [y, y+h] for r = BodyHalfWidth and
// h = BodyHeight. A solid cell occupying [ox0, ox1] x [oy0, oy1] therefore
// overlaps the body exactly when the feet lie in
//
//	x in (ox0-r, ox1+r)  and  y in (oy0-h, oy1)
//
// The vertical band is NOT symmetric, and expanding it by h/2 is the mistake
// this convention invites: the feet are the origin, so the body extends h
// UPWARD and nothing downward. The clearance margin eps widens the two sides
// and the underside of the obstacle but never its top edge, because landing
// on a top edge is a legal support contact and must not be turned into a
// collision. See the departure exemption in sweep for the other half of that
// rule.
//
// # Nothing is sampled
//
// A trajectory phase is a constant-acceleration arc, so x(t) and y(t) are
// quadratics. The times at which each one lies inside its band are solved in
// closed form; the collision times are the intersection of the two time sets.
// The parabola is never evaluated on a grid of sample times, because a sample
// grid misses the thin crossings that matter most: the jump that clips a
// corner between two samples is exactly the false edge this package exists to
// prevent.
//
// # Outside the room is solid
//
// Grid.At fails closed and leaves the policy to the caller. This oracle's
// policy is that everything outside the grid blocks: a room is a sealed box.
// It is the conservative choice — a manoeuvre that would leave the room is
// refused rather than certified against geometry nobody authored — and it is
// what makes a transition edge a separate, explicit thing rather than an
// accident of an unbounded jump.

const (
	// contactEpsilon separates a legal contact from a penetration in the
	// comparisons that decide a landing. It is a numeric tolerance on the
	// solver's roots, not a physical clearance: the physical clearance is the
	// profile's Margin.
	contactEpsilon = 1e-9
	// timeEpsilon is the smallest manoeuvre time the solver treats as real.
	// An event at or below it is the departure contact the arc starts from.
	timeEpsilon = 1e-9
	// maxHeadroomProbe caps how far above a supporting cell headroom is
	// measured. Headroom beyond it is irrelevant to standing and measuring it
	// exactly would make a tall room's surfaces differ for no reason.
	maxHeadroomProbe = 64
)

// rect is an axis-aligned box in the world frame.
type rect struct {
	X Span
	Y Span
}

// xRun is a maximal horizontal run of cells in one grid row, in cell indices
// and inclusive at both ends.
type xRun struct {
	x0 int32
	x1 int32
}

// span returns the run's world extent along x.
func (r xRun) span() Span { return Span{Lo: float64(r.x0), Hi: float64(r.x1) + 1} }

// landingSite is one place an arc may legally come to rest: a supporting top
// edge, restricted to the foot positions that carry the whole body.
type landingSite struct {
	surface  SurfaceID
	interval uint32
	at       float64
	footing  Span
	semi     bool
}

// geometry is the immutable index the oracle derives from one room's grid. It
// is built once per query and shared by every manoeuvre tested against that
// room, which is what keeps graph construction out of O(cells) per candidate.
type geometry struct {
	grid    Grid
	room    RoomID
	profile MovementProfile

	// blocking holds, per grid row, the maximal runs of blocking cells in
	// ascending x order. Semi-solid cells are deliberately absent: they are
	// landing sites and never blocks, which is what lets a rising body pass
	// through one.
	blocking [][]xRun
	// hazards holds, per grid row, the maximal runs of hazard cells.
	hazards [][]xRun
	// surfaces is the derived surface index in ascending SurfaceID order.
	surfaces []Surface
	// landings holds, per grid row, the landing sites whose height is that
	// row's top edge.
	landings [][]landingSite
	// floorRun maps a floor or semi-solid SurfaceID to the blocking run it
	// sits on, for the departure exemption. A semi-solid surface has none.
	floorRun []departure
}

// departure records the blocking run a surface's top edge belongs to, so an
// arc leaving that surface can overhang its own ledge without being told it
// walked into it.
type departure struct {
	row   int32
	run   xRun
	valid bool
}

// newGeometry validates the grid and derives the index.
func newGeometry(grid Grid, room RoomID, profile MovementProfile) (*geometry, error) {
	if err := validateGrid(grid); err != nil {
		return nil, err
	}
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	g := &geometry{
		grid:     grid,
		room:     room,
		profile:  profile,
		blocking: make([][]xRun, grid.Height),
		hazards:  make([][]xRun, grid.Height),
		landings: make([][]landingSite, grid.Height),
	}
	for row := int32(0); row < int32(grid.Height); row++ {
		g.blocking[row] = g.runs(row, func(k CellKind) bool { return k.Blocks() })
		g.hazards[row] = g.runs(row, func(k CellKind) bool { return k == CellKindHazard })
	}
	g.deriveSurfaces()
	return g, nil
}

// runs returns the maximal runs of one row whose cells satisfy keep.
func (g *geometry) runs(row int32, keep func(CellKind) bool) []xRun {
	var out []xRun
	width := int32(g.grid.Width)
	for x := int32(0); x < width; {
		kind, _ := g.grid.At(Cell{X: x, Y: row})
		if !keep(kind) {
			x++
			continue
		}
		start := x
		for x < width {
			next, _ := g.grid.At(Cell{X: x, Y: row})
			if !keep(next) {
				break
			}
			x++
		}
		out = append(out, xRun{x0: start, x1: x - 1})
	}
	return out
}

// blocksAt reports whether a cell blocks, treating everything outside the
// room as solid.
func (g *geometry) blocksAt(x, y int32) bool {
	kind, inside := g.grid.At(Cell{X: x, Y: y})
	if !inside {
		return true
	}
	return kind.Blocks()
}

// lethalAt reports whether a cell kills on contact, through its kind or
// through a lethal terrain on it.
func (g *geometry) lethalAt(x, y int32) bool {
	kind, inside := g.grid.At(Cell{X: x, Y: y})
	if !inside {
		return false
	}
	if kind == CellKindHazard {
		return true
	}
	definition, ok := g.grid.TerrainAt(Cell{X: x, Y: y})
	return ok && definition.Lethal
}

// runAt returns the blocking run of a row that contains a column.
func (g *geometry) runAt(row, column int32) (xRun, bool) {
	if row < 0 || row >= int32(len(g.blocking)) {
		return xRun{}, false
	}
	for _, run := range g.blocking[row] {
		if column >= run.x0 && column <= run.x1 {
			return run, true
		}
	}
	return xRun{}, false
}

// headroomAt returns the free vertical space, in cells, above the top edge of
// a supporting cell, capped at maxHeadroomProbe.
func (g *geometry) headroomAt(column, row int32) float64 {
	free := 0.0
	for probe := row - 1; free < maxHeadroomProbe; probe-- {
		if g.blocksAt(column, probe) {
			break
		}
		free++
	}
	return free
}

// standHazardAt reports whether standing on a supporting cell puts the body in
// contact with something lethal: a lethal terrain underfoot, or a hazard cell
// anywhere in the body's column.
func (g *geometry) standHazardAt(column, row int32) bool {
	if definition, ok := g.grid.TerrainAt(Cell{X: column, Y: row}); ok && definition.Lethal {
		return true
	}
	needed := int32(math.Ceil(g.profile.BodyHeight))
	for i := int32(0); i < needed; i++ {
		if g.lethalAt(column, row-1-i) {
			return true
		}
	}
	return false
}

// deriveSurfaces builds the surface index: floors and semi-solid top edges
// first, in row-major order, then the two wall orientations, then ceilings,
// then climbables. The order is fixed so SurfaceIDs are reproducible.
func (g *geometry) deriveSurfaces() {
	g.deriveFloors()
	g.deriveWalls()
	g.deriveCeilings()
	g.deriveClimbables()
}

// deriveFloors emits one Surface per maximal run of supporting cells with a
// non-blocking cell above, split into intervals wherever headroom or hazard
// changes along the run.
func (g *geometry) deriveFloors() {
	width := int32(g.grid.Width)
	for row := int32(0); row < int32(g.grid.Height); row++ {
		for column := int32(0); column < width; {
			kind, _ := g.grid.At(Cell{X: column, Y: row})
			if !kind.Supports() || g.blocksAt(column, row-1) {
				column++
				continue
			}
			start := column
			for column < width {
				next, _ := g.grid.At(Cell{X: column, Y: row})
				if next != kind || g.blocksAt(column, row-1) {
					break
				}
				column++
			}
			g.emitFloor(kind, row, start, column-1)
		}
	}
}

// emitFloor appends the surface for one run of supporting cells.
func (g *geometry) emitFloor(kind CellKind, row, from, to int32) {
	surfaceKind := SurfaceKindFloor
	if kind == CellKindSemiSolid {
		surfaceKind = SurfaceKindSemiSolid
	}
	extent := Span{Lo: float64(from), Hi: float64(to) + 1}
	at := WorldY(row, g.grid.Height)
	id := SurfaceID(len(g.surfaces))
	intervals := g.splitRun(row, from, to)

	g.surfaces = append(g.surfaces, Surface{
		ID:        id,
		Room:      g.room,
		Kind:      surfaceKind,
		Extent:    extent,
		At:        at,
		Intervals: intervals,
	})

	exemption := departure{}
	if kind == CellKindSolid {
		if run, ok := g.runAt(row, from); ok {
			exemption = departure{row: row, run: run, valid: true}
		}
	}
	for len(g.floorRun) < len(g.surfaces) {
		g.floorRun = append(g.floorRun, departure{})
	}
	g.floorRun[id] = exemption

	for index, interval := range intervals {
		if interval.Footing.IsEmpty() || interval.Hazard {
			continue
		}
		if interval.Headroom < g.profile.BodyHeight {
			continue
		}
		g.landings[row] = append(g.landings[row], landingSite{
			surface:  id,
			interval: uint32(index),
			at:       at,
			footing:  interval.Footing,
			semi:     surfaceKind == SurfaceKindSemiSolid,
		})
	}
}

// splitRun decomposes a run of supporting cells into the surface intervals of
// uniform headroom and hazard.
//
// The split is computed over REAL foot positions, not over columns, because
// the conditions are properties of the whole body: a foot at x occupies
// [x-r-eps, x+r+eps], so a single low column forbids an interval of foot
// positions around it rather than just that column. The breakpoints are
// therefore the influence boundaries of every column, and the value on each
// resulting piece is the minimum headroom and the disjunction of the hazards
// over the columns that reach it. Taking the minimum is what makes the
// interval's claim hold for EVERY point in it, which is the quantifier
// MotionNode's contract demands.
func (g *geometry) splitRun(row, from, to int32) []SurfaceInterval {
	inset := g.profile.BodyHalfWidth + g.profile.Margin
	footing := Span{Lo: float64(from) + inset, Hi: float64(to) + 1 - inset}
	if footing.IsEmpty() {
		headroom := math.Inf(1)
		hazard := false
		for column := from; column <= to; column++ {
			headroom = math.Min(headroom, g.headroomAt(column, row))
			hazard = hazard || g.standHazardAt(column, row)
		}
		return []SurfaceInterval{{Footing: Span{Lo: 1, Hi: 0}, Headroom: headroom, Hazard: hazard}}
	}

	cuts := []float64{footing.Lo, footing.Hi}
	for column := from; column <= to; column++ {
		cuts = append(cuts, float64(column)-inset, float64(column)+1+inset)
	}
	cuts = sortedUnique(cuts, footing)

	var out []SurfaceInterval
	for i := 0; i+1 < len(cuts); i++ {
		lo, hi := cuts[i], cuts[i+1]
		if hi-lo <= contactEpsilon {
			continue
		}
		middle := float64((lo + hi) / 2)
		leftEdge := float64(middle - inset)
		rightEdge := float64(middle + inset)
		headroom := math.Inf(1)
		hazard := false
		touched := false
		for column := from; column <= to; column++ {
			if float64(column) >= rightEdge || float64(float64(column)+1) <= leftEdge {
				continue
			}
			touched = true
			headroom = math.Min(headroom, g.headroomAt(column, row))
			hazard = hazard || g.standHazardAt(column, row)
		}
		if !touched {
			continue
		}
		piece := SurfaceInterval{Footing: Span{Lo: lo, Hi: hi}, Headroom: headroom, Hazard: hazard}
		if n := len(out); n > 0 && out[n-1].Headroom == piece.Headroom && out[n-1].Hazard == piece.Hazard {
			out[n-1].Footing.Hi = hi
			continue
		}
		out = append(out, piece)
	}
	return out
}

// sortedUnique returns the cuts inside the span, in ascending order, with the
// span's own bounds first and last and with duplicates removed.
func sortedUnique(values []float64, within Span) []float64 {
	kept := make([]float64, 0, len(values))
	for _, value := range values {
		if value < within.Lo-contactEpsilon || value > within.Hi+contactEpsilon {
			continue
		}
		kept = append(kept, math.Min(math.Max(value, within.Lo), within.Hi))
	}
	for i := 1; i < len(kept); i++ {
		for j := i; j > 0 && kept[j] < kept[j-1]; j-- {
			kept[j], kept[j-1] = kept[j-1], kept[j]
		}
	}
	out := kept[:0]
	for i, value := range kept {
		if i > 0 && value-out[len(out)-1] <= contactEpsilon {
			continue
		}
		out = append(out, value)
	}
	return out
}

// deriveWalls emits the vertical faces a character can cling to: maximal
// vertical runs of blocking cells whose neighbour on that side is free.
func (g *geometry) deriveWalls() {
	g.deriveWallSide(SurfaceKindWallLeft)
	g.deriveWallSide(SurfaceKindWallRight)
}

func (g *geometry) deriveWallSide(kind SurfaceKind) {
	step := int32(-1)
	if kind == SurfaceKindWallRight {
		step = 1
	}
	for column := int32(0); column < int32(g.grid.Width); column++ {
		for row := int32(0); row < int32(g.grid.Height); {
			if !g.exposedFace(column, row, step) {
				row++
				continue
			}
			start := row
			for row < int32(g.grid.Height) && g.exposedFace(column, row, step) {
				row++
			}
			face := float64(column)
			if kind == SurfaceKindWallRight {
				face = float64(column) + 1
			}
			extent := Span{Lo: WorldY(row, g.grid.Height), Hi: WorldY(start, g.grid.Height)}
			g.surfaces = append(g.surfaces, Surface{
				ID:        SurfaceID(len(g.surfaces)),
				Room:      g.room,
				Kind:      kind,
				Extent:    extent,
				At:        face,
				Intervals: g.wallIntervals(extent),
			})
		}
	}
}

// exposedFace reports whether a blocking cell has a free neighbour one step
// away along x.
func (g *geometry) exposedFace(column, row, step int32) bool {
	return g.blocksAt(column, row) && !g.blocksAt(column+step, row)
}

// wallIntervals returns the foot heights at which the whole body is beside the
// face. A face shorter than the body carries no usable interval.
func (g *geometry) wallIntervals(extent Span) []SurfaceInterval {
	footing := Span{Lo: extent.Lo, Hi: extent.Hi - g.profile.BodyHeight}
	return []SurfaceInterval{{Footing: footing, Headroom: extent.Length(), Hazard: false}}
}

// deriveCeilings emits the solid bottom edges that stop a rising body. They
// are not stood on; they exist so a debug overlay and a diagnostic can name
// the thing a jump hit.
func (g *geometry) deriveCeilings() {
	width := int32(g.grid.Width)
	for row := int32(0); row < int32(g.grid.Height); row++ {
		for column := int32(0); column < width; {
			if !g.blocksAt(column, row) || g.blocksAt(column, row+1) {
				column++
				continue
			}
			start := column
			for column < width && g.blocksAt(column, row) && !g.blocksAt(column, row+1) {
				column++
			}
			extent := Span{Lo: float64(start), Hi: float64(column)}
			g.surfaces = append(g.surfaces, Surface{
				ID:     SurfaceID(len(g.surfaces)),
				Room:   g.room,
				Kind:   SurfaceKindCeiling,
				Extent: extent,
				At:     WorldY(row+1, g.grid.Height),
			})
		}
	}
}

// deriveClimbables emits one surface per maximal vertical run of climbable
// cells in a column.
func (g *geometry) deriveClimbables() {
	for column := int32(0); column < int32(g.grid.Width); column++ {
		for row := int32(0); row < int32(g.grid.Height); {
			if kind, _ := g.grid.At(Cell{X: column, Y: row}); kind != CellKindClimbable {
				row++
				continue
			}
			start := row
			for row < int32(g.grid.Height) {
				kind, _ := g.grid.At(Cell{X: column, Y: row})
				if kind != CellKindClimbable {
					break
				}
				row++
			}
			extent := Span{Lo: WorldY(row, g.grid.Height), Hi: WorldY(start, g.grid.Height)}
			footing := Span{Lo: extent.Lo, Hi: extent.Hi - g.profile.BodyHeight}
			g.surfaces = append(g.surfaces, Surface{
				ID:     SurfaceID(len(g.surfaces)),
				Room:   g.room,
				Kind:   SurfaceKindClimbable,
				Extent: extent,
				At:     float64(column) + 0.5,
				Intervals: []SurfaceInterval{{
					Footing:  footing,
					Headroom: extent.Length(),
					Hazard:   false,
				}},
			})
		}
	}
}

// departureOf returns the departure exemption recorded for a surface, or the
// zero exemption for a surface that has none.
func (g *geometry) departureOf(id SurfaceID) departure {
	if int(id) >= len(g.floorRun) {
		return departure{}
	}
	return g.floorRun[id]
}

// bodyFree reports whether the body standing with its feet at (x, y) overlaps
// no blocking cell and no hazard.
func (g *geometry) bodyFree(x, y float64) bool {
	r := g.profile.BodyHalfWidth
	h := g.profile.BodyHeight
	box := rect{X: Span{Lo: x - r, Hi: x + r}, Y: Span{Lo: y, Hi: y + h}}
	highest, lowest := rowBand(box.Y, g.grid.Height)
	for row := highest; row <= lowest; row++ {
		if row < 0 || row >= int32(g.grid.Height) {
			return false
		}
		for _, run := range g.blocking[row] {
			if overlapsOpen(box.X, run.span()) {
				return false
			}
		}
		for _, run := range g.hazards[row] {
			if overlapsOpen(box.X, run.span()) {
				return false
			}
		}
	}
	return true
}

// overlapsOpen reports whether two spans share an interior point. Touching at
// a bound is not an overlap: contact is legal, penetration is not.
func overlapsOpen(a, b Span) bool {
	return a.Lo < b.Hi-contactEpsilon && b.Lo < a.Hi-contactEpsilon
}

// --- continuous collision -------------------------------------------------

// arc is one constant-acceleration segment of a trajectory, in the world
// frame, with the feet as the reference point.
type arc struct {
	x0, y0 float64
	vx, vy float64
	ax, ay float64
	span   float64 // the segment's duration
}

func (a arc) xAt(t float64) float64 {
	linear := float64(a.vx * t)
	quad := float64(float64(0.5*a.ax) * float64(t*t))
	return a.x0 + linear + quad
}

func (a arc) yAt(t float64) float64 {
	linear := float64(a.vy * t)
	quad := float64(float64(0.5*a.ay) * float64(t*t))
	return a.y0 + linear + quad
}

func (a arc) vxAt(t float64) float64 { return a.vx + float64(a.ax*t) }
func (a arc) vyAt(t float64) float64 { return a.vy + float64(a.ay*t) }

// bounds returns the closed range an axis covers over [0, span], including the
// vertex of the parabola when it falls inside.
func axisBounds(p0, v, acc, span float64) Span {
	linear := float64(v * span)
	quad := float64(float64(0.5*acc) * float64(span*span))
	endpoint := float64(p0 + linear + quad)
	lo := math.Min(p0, endpoint)
	hi := math.Max(p0, endpoint)
	if acc != 0 {
		if vertex := -v / acc; vertex > 0 && vertex < span {
			linear := float64(v * vertex)
			quad := float64(float64(0.5*acc) * float64(vertex*vertex))
			at := float64(p0 + linear + quad)
			lo = math.Min(lo, at)
			hi = math.Max(hi, at)
		}
	}
	return Span{Lo: lo, Hi: hi}
}

// timeSet is a small, fixed-capacity set of disjoint ascending time spans.
//
// It is a value rather than a slice because the collision test runs hundreds
// of thousands of times per room and a slice here is a heap allocation per
// obstacle per phase. A quadratic crosses each of the two band bounds at most
// twice, which cuts the phase into at most five pieces and leaves at most
// three of them inside, so four entries is the ceiling with slack.
type timeSet struct {
	n  int
	at [4]Span
}

func (t *timeSet) push(span Span) {
	if t.n > 0 && span.Lo-t.at[t.n-1].Hi <= contactEpsilon {
		t.at[t.n-1].Hi = span.Hi
		return
	}
	if t.n == len(t.at) {
		t.at[t.n-1].Hi = span.Hi
		return
	}
	t.at[t.n] = span
	t.n++
}

// first returns the earliest time in the set.
func (t timeSet) first() (float64, bool) {
	if t.n == 0 {
		return 0, false
	}
	return t.at[0].Lo, true
}

// rootSet holds the real roots of a quadratic inside a phase.
type rootSet struct {
	n  int
	at [2]float64
}

// roots returns the roots in (0, span) of c + v t + a t^2/2 = 0.
func roots(c, v, a, span float64) rootSet {
	var out rootSet
	add := func(t float64) {
		if t > 0 && t < span && !math.IsNaN(t) {
			out.at[out.n] = t
			out.n++
		}
	}
	if math.IsNaN(c) || math.IsInf(c, 0) {
		return out
	}
	if a == 0 {
		if v != 0 {
			add(-c / v)
		}
		return out
	}
	squaredVelocity := float64(v * v)
	coefficient := float64(2 * a)
	constant := float64(coefficient * c)
	discriminant := float64(squaredVelocity - constant)
	if discriminant < 0 {
		return out
	}
	root := math.Sqrt(discriminant)
	add((-v + root) / a)
	add((-v - root) / a)
	return out
}

// insideTimes returns the sub-intervals of [0, span] on which the quadratic
// p(t) = p0 + v t + a t^2/2 lies strictly between lo and hi.
//
// It solves rather than samples. The roots of p = lo and of p = hi cut [0,
// span] into at most five pieces on which the predicate is constant, so one
// midpoint evaluation per piece decides it exactly. A NaN or infinite bound
// simply never produces a root, which leaves that side unbounded.
func insideTimes(p0, v, a, lo, hi, span float64) timeSet {
	var cuts [6]float64
	count := 0
	cuts[count], count = 0, count+1
	cuts[count], count = span, count+1
	for _, set := range [2]rootSet{roots(p0-lo, v, a, span), roots(p0-hi, v, a, span)} {
		for i := 0; i < set.n; i++ {
			cuts[count], count = set.at[i], count+1
		}
	}
	for i := 1; i < count; i++ {
		for j := i; j > 0 && cuts[j] < cuts[j-1]; j-- {
			cuts[j], cuts[j-1] = cuts[j-1], cuts[j]
		}
	}
	var out timeSet
	for i := 0; i+1 < count; i++ {
		from, to := cuts[i], cuts[i+1]
		if to-from <= contactEpsilon {
			continue
		}
		middle := (from + to) / 2
		linear := float64(v * middle)
		quad := float64(float64(0.5*a) * float64(middle*middle))
		value := float64(p0 + linear + quad)
		if !(value > lo && value < hi) {
			continue
		}
		out.push(Span{Lo: from, Hi: to})
	}
	return out
}

// intersectTimes returns the intersection of two time sets.
func intersectTimes(a, b timeSet) timeSet {
	var out timeSet
	i, j := 0, 0
	for i < a.n && j < b.n {
		if common, ok := a.at[i].Intersect(b.at[j]); ok && common.Length() > contactEpsilon {
			out.push(common)
		}
		if a.at[i].Hi < b.at[j].Hi {
			i++
		} else {
			j++
		}
	}
	return out
}

// obstacleBox returns the forbidden FOOT positions for one blocking run, which
// is the cell box expanded by the body and by the clearance margin.
//
// The horizontal bounds and the underside grow by r+eps and h+eps; the top
// edge does not grow at all. That asymmetry is the whole point: the top edge
// is where a landing contact happens, and expanding it would make every
// landing a collision. The underside is where a jump grazes a ceiling, and not
// expanding it would certify a jump that passes through the ceiling by less
// than the declared clearance.
func (g *geometry) obstacleBox(row int32, run xRun) rect {
	r := g.profile.BodyHalfWidth + g.profile.Margin
	cell := run.span()
	top := WorldY(row, g.grid.Height)
	bottom := WorldY(row+1, g.grid.Height)
	return rect{
		X: Span{Lo: cell.Lo - r, Hi: cell.Hi + r},
		Y: Span{Lo: bottom - g.profile.BodyHeight - g.profile.Margin, Hi: top},
	}
}

// contactKind names what an arc ran into.
type contactKind int

const (
	contactNone contactKind = iota
	contactBlocked
	contactHazard
	contactLanding
)

// contact is the first thing an arc meets inside one segment.
type contact struct {
	kind     contactKind
	at       float64
	site     landingSite
	wallSide WallSide
	wallFace float64
}

// sweep finds the earliest event an arc meets on (0, a.span], testing every
// blocking run, hazard run and landing site the arc's bounding box reaches.
//
// # The departure exemption
//
// An arc that leaves a ledge sideways starts with its body still over the
// platform it is leaving, because the full-support footing puts the feet
// r+eps inside the edge. Under the overhang-tolerant rule this package
// declares for LEAVING a surface — the character keeps its footing while any
// part of the body is over it — that first instant is not a collision. The
// exemption is narrow: only the departure run's TOP ROW is shrunk, and only to
// its own footing span, so the arc still cannot pass through the platform's
// interior and still collides with everything below the top row. A tall block
// therefore still stops a character who slides down its side.
func (g *geometry) sweep(a arc, exempt departure, ignoreSemi SurfaceID, ignoreSemiActive bool, phaseMode MotionMode, abilities AbilitySet, spent *spend) contact {
	if a.span <= timeEpsilon {
		return contact{kind: contactNone}
	}

	// A landing and the collision box of the platform it lands on fire at the
	// SAME instant: the feet cross the top edge, which is both the legal
	// contact and the boundary of the forbidden box. The two are therefore
	// tracked apart and the landing wins the tie. It can only win it over its
	// own platform: the full-support footing stops r+eps short of the edge,
	// so a landing that is legal is never simultaneous with entering a
	// different run's expanded box.
	blocked := contact{kind: contactNone, at: math.Inf(1)}
	landing := contact{kind: contactNone, at: math.Inf(1)}
	note := func(c contact) {
		if c.kind == contactNone || c.at >= blocked.at {
			return
		}
		blocked = c
	}

	r := g.profile.BodyHalfWidth + g.profile.Margin
	xRange := axisBounds(a.x0, a.vx, a.ax, a.span)
	yRange := axisBounds(a.y0, a.vy, a.ay, a.span)
	bodyX := Span{Lo: xRange.Lo - r, Hi: xRange.Hi + r}
	bodyY := Span{Lo: yRange.Lo, Hi: yRange.Hi + g.profile.BodyHeight}

	// One row of slack on each side: a tangent touch sits exactly on a row
	// boundary, and the temporal test below is exact, so over-scanning costs
	// a comparison and under-scanning loses a contact.
	topRow, bottomRow := rowBand(bodyY, g.grid.Height)
	topRow--
	bottomRow++

	// Leaving the room is a collision with the surround.
	note(g.surroundContact(a, bodyX, bodyY, spent))

	hazardImmune := phaseMode == MotionModeDashing &&
		g.profile.Dash != nil && g.profile.Dash.ShadowPassesHazard &&
		abilities.Has(AbilityShadowDash)

	for row := maxInt32(topRow, 0); row <= minInt32(bottomRow, int32(g.grid.Height)-1); row++ {
		for _, run := range g.blocking[row] {
			if !overlapsOpen(bodyX, run.span()) {
				continue
			}
			box, ok := g.departureBox(row, run, exempt)
			if !ok {
				continue
			}
			if !spent.collision() {
				return contact{kind: contactNone}
			}
			if at, hit := g.boxTimes(a, box).first(); hit {
				side, face := g.wallContact(a, at, box)
				note(contact{kind: contactBlocked, at: at, wallSide: side, wallFace: face})
			}
		}
		if !hazardImmune {
			for _, run := range g.hazards[row] {
				if !overlapsOpen(bodyX, run.span()) {
					continue
				}
				if !spent.collision() {
					return contact{kind: contactNone}
				}
				// A hazard's top edge IS expanded: there is no legal contact
				// with a spike, so standing on one is death, not a landing.
				box := g.obstacleBox(row, run)
				box.Y.Hi += g.profile.Margin
				if at, ok := g.boxTimes(a, box).first(); ok {
					note(contact{kind: contactHazard, at: at})
				}
			}
		}
		for _, site := range g.landings[row] {
			if site.semi && ignoreSemiActive && site.surface == ignoreSemi {
				continue
			}
			if !overlapsOpen(xRange, site.footing) {
				continue
			}
			at, ok := g.landingTime(a, site)
			if !ok || at >= landing.at {
				continue
			}
			landing = contact{kind: contactLanding, at: at, site: site}
		}
	}

	switch {
	case landing.kind != contactNone && landing.at <= blocked.at+contactEpsilon:
		return landing
	case blocked.kind != contactNone && !math.IsInf(blocked.at, 1):
		return blocked
	default:
		return contact{kind: contactNone}
	}
}

// rowBand returns the grid rows whose interior a world-frame vertical span
// reaches. A span that merely touches a row boundary does not reach into that
// row, which is what keeps a body resting on a floor out of the floor.
func rowBand(y Span, height uint32) (int32, int32) {
	lo := int32(math.Floor(float64(height)-y.Hi-1)) + 1
	hi := int32(math.Ceil(float64(height)-y.Lo)) - 1
	return lo, hi
}

// departureBox returns the forbidden box of one blocking run, applying the
// departure exemption to its top row when the run is the one the arc left. It
// reports false when the exemption leaves nothing to test.
func (g *geometry) departureBox(row int32, run xRun, exempt departure) (rect, bool) {
	box := g.obstacleBox(row, run)
	if !exempt.valid || exempt.row != row || exempt.run != run {
		return box, true
	}
	inset := g.profile.BodyHalfWidth + g.profile.Margin
	box.X = Span{Lo: run.span().Lo + inset, Hi: run.span().Hi - inset}
	if box.X.IsEmpty() {
		return rect{}, false
	}
	return box, true
}

// surroundContact reports the arc leaving the room, which this oracle treats
// as walking into solid rock.
func (g *geometry) surroundContact(a arc, bodyX, bodyY Span, spent *spend) contact {
	if !spent.collision() {
		return contact{kind: contactNone}
	}
	r := g.profile.BodyHalfWidth + g.profile.Margin
	width := float64(g.grid.Width)
	height := float64(g.grid.Height)
	if bodyX.Lo >= r && bodyX.Hi <= width-r && bodyY.Lo >= 0 && bodyY.Hi <= height {
		return contact{kind: contactNone}
	}
	at := math.Inf(1)
	for _, times := range [4]timeSet{
		insideTimes(a.x0, a.vx, a.ax, math.Inf(-1), r, a.span),
		insideTimes(a.x0, a.vx, a.ax, width-r, math.Inf(1), a.span),
		insideTimes(a.y0, a.vy, a.ay, math.Inf(-1), 0, a.span),
		insideTimes(a.y0+g.profile.BodyHeight, a.vy, a.ay, height, math.Inf(1), a.span),
	} {
		if first, ok := times.first(); ok && first < at {
			at = first
		}
	}
	if math.IsInf(at, 1) {
		return contact{kind: contactNone}
	}
	return contact{kind: contactBlocked, at: at}
}

// boxTimes returns the times on (0, span] at which the feet lie strictly
// inside a forbidden box: the intersection of the horizontal and the vertical
// time sets, exactly as section 4.4 prescribes.
func (g *geometry) boxTimes(a arc, box rect) timeSet {
	xs := insideTimes(a.x0, a.vx, a.ax, box.X.Lo, box.X.Hi, a.span)
	if xs.n == 0 {
		return timeSet{}
	}
	ys := insideTimes(a.y0, a.vy, a.ay, box.Y.Lo, box.Y.Hi, a.span)
	if ys.n == 0 {
		return timeSet{}
	}
	return intersectTimes(xs, ys)
}

// landingTime returns the first time the feet cross a supporting top edge
// downward with the whole body over the footing.
//
// The crossing must be strictly descending. A trajectory that reaches the
// landing height exactly at its apex touches it with zero vertical velocity,
// and that tangent contact is NOT a landing: accepting it would certify a jump
// whose margin for error is zero, which is the one thing section 4.2 says must
// not happen implicitly.
func (g *geometry) landingTime(a arc, site landingSite) (float64, bool) {
	cuts := roots(a.y0-site.at, a.vy, a.ay, a.span)
	best := math.Inf(1)
	for i := 0; i < cuts.n; i++ {
		t := cuts.at[i]
		if t <= timeEpsilon {
			continue
		}
		if a.vyAt(t) >= -contactEpsilon {
			continue
		}
		x := a.xAt(t)
		if x < site.footing.Lo || x > site.footing.Hi {
			continue
		}
		if t < best {
			best = t
		}
	}
	if math.IsInf(best, 1) {
		return 0, false
	}
	return best, true
}

// wallContact classifies a blocking contact as a cling candidate when the body
// met the obstacle's side rather than its underside or its top.
func (g *geometry) wallContact(a arc, at float64, box rect) (WallSide, float64) {
	y := a.yAt(at)
	if y >= box.Y.Hi-contactEpsilon || y <= box.Y.Lo+contactEpsilon {
		return WallSideNone, 0
	}
	x := a.xAt(at)
	if math.Abs(x-box.X.Lo) <= math.Abs(x-box.X.Hi) {
		return WallSideRight, box.X.Lo
	}
	return WallSideLeft, box.X.Hi
}

func minInt32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

func maxInt32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}
