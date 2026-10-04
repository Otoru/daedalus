package vision_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"math/rand"
	"path/filepath"
	"testing"

	"github.com/Otoru/daedalus"
	"github.com/Otoru/daedalus/utils/vision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeRadiusZeroContainsOnlyTransparentOrigin(t *testing.T) {
	grid := openGrid(3, 2)
	field, err := vision.Compute(context.Background(), grid, daedalus.Cell{X: 1, Y: 0}, 0)
	require.NoError(t, err)
	assert.True(t, field.VisibleAt(daedalus.Cell{X: 1, Y: 0}))
	assert.Equal(t, []daedalus.Cell{{X: 1, Y: 0}}, field.VisibleCells(nil))
}

func TestComputeWallsAreVisibleButBlockSight(t *testing.T) {
	grid := openGrid(5, 1)
	grid.SetTransparent(daedalus.Cell{X: 2, Y: 0}, false)
	field, err := vision.Compute(context.Background(), grid, daedalus.Cell{X: 0, Y: 0}, 4)
	require.NoError(t, err)
	assert.True(t, field.VisibleAt(daedalus.Cell{X: 2, Y: 0}))
	assert.False(t, field.VisibleAt(daedalus.Cell{X: 3, Y: 0}))
	assert.False(t, field.VisibleAt(daedalus.Cell{X: 4, Y: 0}))
}

func TestComputeRejectsInvalidOrOpaqueOrigin(t *testing.T) {
	grid := openGrid(2, 2)
	grid.SetTransparent(daedalus.Cell{X: 1, Y: 1}, false)
	for _, origin := range []daedalus.Cell{{X: -1}, {X: 2}, {X: 1, Y: 1}} {
		_, err := vision.Compute(context.Background(), grid, origin, 1)
		require.ErrorIs(t, err, daedalus.ErrInvalidVisibility)
	}
	_, err := vision.Compute(context.Background(), vision.OpacityGrid{}, daedalus.Cell{}, 1)
	require.ErrorIs(t, err, daedalus.ErrInvalidVisibility)
}

func TestComputeIntoClearsDirtyOutputAndReusesBuffers(t *testing.T) {
	grid := openGrid(7, 5)
	var reused vision.Field
	require.NoError(t, vision.ComputeInto(context.Background(), &reused, grid, daedalus.Cell{X: 3, Y: 2}, 3))
	for i := range reused.Visible {
		reused.Visible[i] = 0xff
	}
	visibleBacking := &reused.Visible[0]
	rows := reused.VisibleCells(make([]daedalus.Cell, 0, 2))
	require.NoError(t, vision.ComputeInto(context.Background(), &reused, grid, daedalus.Cell{X: 0, Y: 0}, 0))
	assert.Equal(t, []daedalus.Cell{{X: 0, Y: 0}}, reused.VisibleCells(nil))
	assert.Equal(t, visibleBacking, &reused.Visible[0])
	assert.NotEmpty(t, rows)
}

func TestVisibleCellsIsRowMajorAndReusesDestination(t *testing.T) {
	field, err := vision.Compute(context.Background(), openGrid(3, 2), daedalus.Cell{X: 1, Y: 0}, 1)
	require.NoError(t, err)
	dst := make([]daedalus.Cell, 0, 8)
	got := field.VisibleCells(dst)
	assert.Equal(t, cap(dst), cap(got))
	assert.Equal(t, []daedalus.Cell{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 2, Y: 0}, {X: 1, Y: 1}}, got)
	assert.False(t, field.VisibleAt(daedalus.Cell{X: -1}))
	assert.False(t, (vision.Field{Width: 1, Height: 1}).VisibleAt(daedalus.Cell{}))
}

func TestVisibilityIsReciprocalForExhaustiveSmallGridsAndRandomGrids(t *testing.T) {
	for _, dimensions := range [][2]int{{2, 2}, {3, 2}, {3, 3}} {
		width, height := dimensions[0], dimensions[1]
		for mask := 0; mask < 1<<(width*height); mask++ {
			assertReciprocalForEveryTransparentPair(t, maskGrid(width, height, mask), width, height, mask)
		}
	}
	assertReciprocalOnRandomGrids(t, rand.New(rand.NewSource(7)))
}

func assertReciprocalVisibility(t *testing.T, grid vision.OpacityGrid, a, b daedalus.Cell, radius uint32, msgAndArgs ...any) {
	t.Helper()
	left, err := vision.Compute(context.Background(), grid, a, radius)
	require.NoError(t, err)
	right, err := vision.Compute(context.Background(), grid, b, radius)
	require.NoError(t, err)
	assert.Equal(t, left.VisibleAt(b), right.VisibleAt(a), msgAndArgs...)
}

func assertReciprocalForEveryTransparentPair(t *testing.T, grid vision.OpacityGrid, width, height, mask int) {
	t.Helper()
	cells := width * height
	for first := 0; first < cells; first++ {
		if !grid.TransparentAt(indexCell(width, first)) {
			continue
		}
		for second := first + 1; second < cells; second++ {
			if !grid.TransparentAt(indexCell(width, second)) {
				continue
			}
			assertReciprocalVisibility(t, grid, indexCell(width, first), indexCell(width, second), 4,
				"grid %dx%d mask %#x pair %d,%d", width, height, mask, first, second)
		}
	}
}

func assertReciprocalOnRandomGrids(t *testing.T, rng *rand.Rand) {
	t.Helper()
	for sample := 0; sample < 100; sample++ {
		grid := openGrid(8, 7)
		for i := range grid.Transparent {
			grid.Transparent[i] = byte(rng.Intn(256))
		}
		for first := 0; first < 10; first++ {
			a := daedalus.Cell{X: int32(rng.Intn(8)), Y: int32(rng.Intn(7))}
			b := daedalus.Cell{X: int32(rng.Intn(8)), Y: int32(rng.Intn(7))}
			if !grid.TransparentAt(a) || !grid.TransparentAt(b) {
				continue
			}
			assertReciprocalVisibility(t, grid, a, b, 7, "sample %d pair %+v %+v", sample, a, b)
		}
	}
}

func TestVisibilityIsReciprocalAcrossGeneratedLayout(t *testing.T) {
	layout, err := (daedalus.Generator{}).Generate(daedalus.Config{
		Width: 40, Height: 40, Seed: 3, MinDistance: 6, MaxAttempts: 30, MaxRooms: 64,
	})
	require.NoError(t, err)
	grid := vision.NewOpacityGrid(layout)
	origins := transparentCells(grid)
	for first, origin := range origins {
		field, err := vision.Compute(context.Background(), grid, origin, 12)
		require.NoError(t, err)
		for _, target := range origins[first+1:] {
			reverse, err := vision.Compute(context.Background(), grid, target, 12)
			require.NoError(t, err)
			assert.Equal(t, field.VisibleAt(target), reverse.VisibleAt(origin), "origins %+v and %+v", origin, target)
		}
	}
}

func TestFirstWallDoesNotCreateAShadowTransition(t *testing.T) {
	// Bits 4, 7, 8, and 10 are the only floors. In the relevant child row,
	// the first candidate is a wall; it must not create a floor-to-wall split.
	grid := maskGrid(5, 5, 0x590)
	left, err := vision.Compute(context.Background(), grid, daedalus.Cell{X: 4, Y: 0}, 8)
	require.NoError(t, err)
	right, err := vision.Compute(context.Background(), grid, daedalus.Cell{X: 0, Y: 2}, 8)
	require.NoError(t, err)
	assert.Equal(t, left.VisibleAt(daedalus.Cell{X: 0, Y: 2}), right.VisibleAt(daedalus.Cell{X: 4, Y: 0}))
}

func TestVisibilityStaysWithinInclusiveRadius(t *testing.T) {
	field, err := vision.Compute(context.Background(), openGrid(9, 9), daedalus.Cell{X: 4, Y: 4}, 3)
	require.NoError(t, err)
	for _, at := range field.VisibleCells(nil) {
		dx := int64(at.X - 4)
		dy := int64(at.Y - 4)
		assert.LessOrEqual(t, dx*dx+dy*dy, int64(9), "cell %+v", at)
	}
}

func TestSymmetricShadowcastingKeepsTheCanonicalOpenDisk(t *testing.T) {
	field, err := vision.Compute(context.Background(), openGrid(5, 5), daedalus.Cell{X: 2, Y: 2}, 2)
	require.NoError(t, err)
	assert.Len(t, field.VisibleCells(nil), 13)
	for _, at := range field.VisibleCells(nil) {
		dx := int64(at.X - 2)
		dy := int64(at.Y - 2)
		assert.LessOrEqual(t, dx*dx+dy*dy, int64(4))
	}
}

func TestMaximumGridDistanceArithmeticFitsInt64(t *testing.T) {
	delta := int64(daedalus.MaxCells - 1)
	dx2 := delta * delta
	dy2 := delta * delta
	distance2 := dx2 + dy2
	radius2 := delta * delta
	assert.Greater(t, distance2, radius2)
	assert.Less(t, distance2, int64(^uint64(0)>>1))
}

func TestVisionProductionFilesContainNoFloatingPointOrMath(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	for _, name := range files {
		if filepath.Ext(name) != ".go" || filepath.Base(name) == "field_test.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		require.NoError(t, err, name)
		ast.Inspect(file, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.BasicLit:
				assert.NotEqual(t, token.FLOAT, value.Kind, "%s contains float literal", name)
			case *ast.ImportSpec:
				if value.Path.Value == `"math"` {
					t.Errorf("%s imports math", name)
				}
			case *ast.Ident:
				assert.NotEqual(t, "float32", value.Name, "%s names float32", name)
				assert.NotEqual(t, "float64", value.Name, "%s names float64", name)
			}
			return true
		})
	}
}

func openGrid(width, height int) vision.OpacityGrid {
	grid := vision.OpacityGrid{Width: uint32(width), Height: uint32(height), Transparent: make([]byte, (width*height+7)/8)}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			grid.SetTransparent(daedalus.Cell{X: int32(x), Y: int32(y)}, true)
		}
	}
	return grid
}

func maskGrid(width, height, mask int) vision.OpacityGrid {
	grid := vision.OpacityGrid{Width: uint32(width), Height: uint32(height), Transparent: make([]byte, (width*height+7)/8)}
	for i := 0; i < width*height; i++ {
		if mask&(1<<i) != 0 {
			grid.SetTransparent(indexCell(width, i), true)
		}
	}
	return grid
}

func indexCell(width, index int) daedalus.Cell {
	return daedalus.Cell{X: int32(index % width), Y: int32(index / width)}
}

func transparentCells(grid vision.OpacityGrid) []daedalus.Cell {
	cells := make([]daedalus.Cell, 0)
	for y := uint32(0); y < grid.Height; y++ {
		for x := uint32(0); x < grid.Width; x++ {
			at := daedalus.Cell{X: int32(x), Y: int32(y)}
			if grid.TransparentAt(at) {
				cells = append(cells, at)
			}
		}
	}
	return cells
}
