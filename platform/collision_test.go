package platform

import (
	"math"
	"strings"
	"testing"
)

// The fixtures in this file are ASCII rooms. Row zero is the TOP row, which
// is the grid frame's convention, so a map reads on the screen the way it
// reads in the file and nobody has to flip it mentally. The world height of a
// row's top edge is WorldY(row, height), and every expected number in these
// tests is written in the world frame.
//
//	'.' air          '#' solid
//	'=' semi-solid   '^' hazard        'H' climbable
func roomFromRows(t *testing.T, rows []string) Grid {
	t.Helper()
	if len(rows) == 0 {
		t.Fatalf("a room needs at least one row")
	}
	width := len(rows[0])
	cells := make([]CellKind, 0, width*len(rows))
	for y, row := range rows {
		if len(row) != width {
			t.Fatalf("row %d is %d wide, row 0 is %d wide", y, len(row), width)
		}
		for _, symbol := range row {
			switch symbol {
			case '.':
				cells = append(cells, CellKindEmpty)
			case '#':
				cells = append(cells, CellKindSolid)
			case '=':
				cells = append(cells, CellKindSemiSolid)
			case '^':
				cells = append(cells, CellKindHazard)
			case 'H':
				cells = append(cells, CellKindClimbable)
			default:
				t.Fatalf("row %d holds the unknown symbol %q", y, string(symbol))
			}
		}
	}
	return Grid{Width: uint32(width), Height: uint32(len(rows)), Cells: cells}
}

// testProfile is DefaultProfile with the knobs these tests need to be exact.
func testProfile() MovementProfile { return DefaultProfile() }

// testBudget is DefaultSearchBudget with a coarser launch grid, so a fixture
// room produces a graph small enough to reason about by hand.
func testBudget() SearchBudget {
	budget := DefaultSearchBudget()
	budget.LaunchResolution = 1.0
	budget.TimeResolution = 0.04
	return budget
}

func mustGeometry(t *testing.T, rows []string, profile MovementProfile) *geometry {
	t.Helper()
	geom, err := newGeometry(roomFromRows(t, rows), 0, profile)
	if err != nil {
		t.Fatalf("newGeometry: %v", err)
	}
	return geom
}

// findSurface returns the first surface of a kind whose At matches.
func findSurface(t *testing.T, geom *geometry, kind SurfaceKind, at float64, within Span) Surface {
	t.Helper()
	for _, surface := range geom.surfaces {
		if surface.Kind != kind || math.Abs(surface.At-at) > 1e-9 {
			continue
		}
		if !within.IsEmpty() && !surface.Extent.Overlaps(within) {
			continue
		}
		return surface
	}
	t.Fatalf("no %s surface at %v within %v; have %s", kind, at, within, describeSurfaces(geom))
	return Surface{}
}

func describeSurfaces(geom *geometry) string {
	var out []string
	for _, surface := range geom.surfaces {
		out = append(out, surface.Kind.String()+"@"+surface.Extent.String()+"/"+Point(surface.At).String())
	}
	return strings.Join(out, " ")
}

func TestInsideTimesSolvesTheBandExactly(t *testing.T) {
	// A parabola rising from 0 with v=10 and a=-50 peaks at 1.0 at t=0.2 and
	// returns to 0 at t=0.4. The band (0.5, 2) is entered and left once.
	times := insideTimes(0, 10, -50, 0.5, 2, 0.4)
	if times.n != 1 {
		t.Fatalf("want one interval, got %v", times.at[:times.n])
	}
	// 0 + 10t - 25t^2 = 0.5  =>  t = (10 +- sqrt(100-50))/50
	lo := (10 - math.Sqrt(50)) / 50
	hi := (10 + math.Sqrt(50)) / 50
	if math.Abs(times.at[0].Lo-lo) > 1e-9 || math.Abs(times.at[0].Hi-hi) > 1e-9 {
		t.Fatalf("want [%v, %v], got %v", lo, hi, times.at[0])
	}
	// A band the parabola only touches tangentially is never entered.
	if touched := insideTimes(0, 10, -50, 1.0, 2, 0.4); touched.n != 0 {
		t.Fatalf("a tangent touch is not an entry, got %v", touched.at[:touched.n])
	}
}

func TestObstacleBoxExpandsVerticallyByTheWholeBodyUpward(t *testing.T) {
	// The feet are the reference point, so a cell at rows [oy0, oy1] forbids
	// foot heights (oy0-h-eps, oy1]. Expanding by h/2 would be wrong in both
	// directions, and this test is the one that catches it.
	profile := testProfile()
	geom := mustGeometry(t, []string{
		"....",
		".##.",
		"####",
	}, profile)

	box := geom.obstacleBox(1, xRun{x0: 1, x1: 2})
	wantTop := WorldY(1, 3) // 2
	wantBottom := WorldY(2, 3) - profile.BodyHeight - profile.Margin
	if math.Abs(box.Y.Hi-wantTop) > 1e-12 {
		t.Fatalf("top edge must not be expanded: want %v, got %v", wantTop, box.Y.Hi)
	}
	if math.Abs(box.Y.Lo-wantBottom) > 1e-12 {
		t.Fatalf("underside must be expanded by the whole body height: want %v, got %v", wantBottom, box.Y.Lo)
	}
	if half := WorldY(2, 3) - profile.BodyHeight/2; math.Abs(box.Y.Lo-half) < 1e-9 {
		t.Fatalf("the vertical expansion is h/2, which this convention forbids")
	}
	inset := profile.BodyHalfWidth + profile.Margin
	if math.Abs(box.X.Lo-(1-inset)) > 1e-12 || math.Abs(box.X.Hi-(3+inset)) > 1e-12 {
		t.Fatalf("horizontal expansion is r+eps on each side, got %v", box.X)
	}
}

func TestSurfacesAreIntervalsAndALowCeilingFragmentsThem(t *testing.T) {
	// A ten-cell floor with a two-cell overhang above columns 4 and 5. The
	// run is one surface, and the body is wider than a cell, so the low
	// headroom reaches further than the two columns that cause it.
	profile := testProfile()
	geom := mustGeometry(t, []string{
		"..........",
		"....##....",
		"..........",
		"##########",
	}, profile)

	floor := findSurface(t, geom, SurfaceKindFloor, WorldY(3, 4), Span{Lo: 0, Hi: 10})
	if len(floor.Intervals) < 3 {
		t.Fatalf("a low ceiling must fragment the run; got %d interval(s): %v", len(floor.Intervals), floor.Intervals)
	}
	inset := profile.BodyHalfWidth + profile.Margin
	if got, want := floor.Intervals[0].Footing.Lo, 0+inset; math.Abs(got-want) > 1e-12 {
		t.Fatalf("footing starts at a+r+eps: want %v, got %v", want, got)
	}
	last := floor.Intervals[len(floor.Intervals)-1]
	if got, want := last.Footing.Hi, 10-inset; math.Abs(got-want) > 1e-12 {
		t.Fatalf("footing ends at b-r-eps: want %v, got %v", want, got)
	}
	var low *SurfaceInterval
	for i := range floor.Intervals {
		if floor.Intervals[i].Headroom < 3 {
			low = &floor.Intervals[i]
			break
		}
	}
	if low == nil {
		t.Fatalf("no interval reports the reduced headroom: %v", floor.Intervals)
	}
	if low.Headroom != 1 {
		t.Fatalf("the overhang leaves one cell of headroom, got %v", low.Headroom)
	}
	// The fragment is wider than the two columns, because the body is.
	if width := low.Footing.Length(); width <= 2 {
		t.Fatalf("the low fragment must be widened by the body half width, got %v", width)
	}
	for i := 0; i+1 < len(floor.Intervals); i++ {
		if math.Abs(floor.Intervals[i].Footing.Hi-floor.Intervals[i+1].Footing.Lo) > 1e-12 {
			t.Fatalf("intervals of one run must tile its footing: %v", floor.Intervals)
		}
	}
}

func TestANarrowLedgeHasAnEmptyFootingAndCannotBeStoodOn(t *testing.T) {
	// A one-cell ledge is narrower than a body whose half width is 0.6, so
	// under the full-support policy its footing is empty. The surface still
	// exists: the geometry is real and unusable, and saying so is more useful
	// than dropping it.
	profile := testProfile()
	profile.BodyHalfWidth = 0.6
	geom := mustGeometry(t, []string{
		"......",
		"......",
		"..#...",
		"......",
	}, profile)

	ledge := findSurface(t, geom, SurfaceKindFloor, WorldY(2, 4), Span{Lo: 2, Hi: 3})
	if len(ledge.Intervals) != 1 {
		t.Fatalf("want one interval, got %v", ledge.Intervals)
	}
	if !ledge.Intervals[0].Footing.IsEmpty() {
		t.Fatalf("a cell narrower than 2(r+eps) carries no footing, got %v", ledge.Intervals[0].Footing)
	}
	if len(geom.landings[2]) != 0 {
		t.Fatalf("an empty footing is not a landing site, got %v", geom.landings[2])
	}
}

func TestSemiSolidIsALandingSiteAndNeverABlockingRun(t *testing.T) {
	profile := testProfile()
	geom := mustGeometry(t, []string{
		"....",
		"....",
		"....",
		"====",
		"....",
		"####",
	}, profile)

	for row, runs := range geom.blocking {
		if row == 5 {
			continue
		}
		if len(runs) != 0 {
			t.Fatalf("row %d holds a blocking run %v; a semi-solid is never a block", row, runs)
		}
	}
	if len(geom.landings[3]) == 0 {
		t.Fatalf("the semi-solid top edge must be a landing site")
	}
	if !geom.landings[3][0].semi {
		t.Fatalf("the landing site must be marked one-way")
	}
}
