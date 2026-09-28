package pathfinding_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/Otoru/daedalus"
	"github.com/Otoru/daedalus/utils/pathfinding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestZeroCostGridIsImpassable(t *testing.T) {
	var grid pathfinding.CostGrid

	assert.Equal(t, pathfinding.Cost(0), pathfinding.CostImpassable)
	assert.NotEqual(t, pathfinding.MinCost, pathfinding.CostImpassable)

	for _, at := range []daedalus.Cell{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 0, Y: 1}, {X: -1, Y: -1}} {
		got := grid.At(at)
		assert.Equal(t, pathfinding.CostImpassable, got, "zero grid at %+v", at)
		assert.NotEqual(t, pathfinding.MinCost, got, "zero grid must not be an open plain at %+v", at)
		_, inside := grid.Index(at)
		assert.False(t, inside, "zero grid contains %+v", at)
	}

	filled := pathfinding.CostGrid{Width: 2, Height: 2, Costs: make([]pathfinding.Cost, 4)}
	assert.Equal(t, pathfinding.CostImpassable, filled.At(daedalus.Cell{X: 1, Y: 1}))
}

func TestAtOutsideTheGridIsImpassable(t *testing.T) {
	grid := pathfinding.CostGrid{
		Width:  2,
		Height: 3,
		Costs:  []pathfinding.Cost{pathfinding.MinCost, pathfinding.MinCost, pathfinding.MinCost, pathfinding.MinCost, pathfinding.MinCost, pathfinding.MinCost},
	}

	outside := []daedalus.Cell{
		{X: -1, Y: 0},
		{X: 0, Y: -1},
		{X: 2, Y: 0},
		{X: 0, Y: 3},
		{X: 5, Y: 5},
	}
	for _, at := range outside {
		assert.NotPanics(t, func() {
			got := grid.At(at)
			assert.Equal(t, pathfinding.CostImpassable, got, "outside %+v", at)
		})
		_, inside := grid.Index(at)
		assert.False(t, inside, "%+v", at)
	}

	short := pathfinding.CostGrid{Width: 2, Height: 2, Costs: []pathfinding.Cost{pathfinding.MinCost}}
	assert.NotPanics(t, func() {
		assert.Equal(t, pathfinding.CostImpassable, short.At(daedalus.Cell{X: 1, Y: 1}))
	})
}

func TestCloneDetachesFromTheOriginal(t *testing.T) {
	at := daedalus.Cell{X: 0, Y: 0}
	original := pathfinding.CostGrid{Width: 1, Height: 1, Costs: []pathfinding.Cost{pathfinding.MinCost}}
	clone := original.Clone()

	require.True(t, clone.Set(at, pathfinding.MaxCost))
	assert.Equal(t, pathfinding.MinCost, original.At(at), "writing the clone must leave the original untouched")
	assert.Equal(t, pathfinding.MaxCost, clone.At(at))

	require.True(t, original.Set(at, pathfinding.CostImpassable))
	assert.Equal(t, pathfinding.MaxCost, clone.At(at), "writing the original must leave the clone untouched")
	assert.Equal(t, pathfinding.CostImpassable, original.At(at))
}

func TestMalformedGridReturnsInvalidNavigation(t *testing.T) {
	cases := []struct {
		name string
		grid pathfinding.CostGrid
		want string
	}{
		{name: "zero width", grid: pathfinding.CostGrid{Width: 0, Height: 4, Costs: make([]pathfinding.Cost, 4)}, want: "0"},
		{name: "zero height", grid: pathfinding.CostGrid{Width: 4, Height: 0}, want: "0"},
		{name: "both dimensions zero", grid: pathfinding.CostGrid{}, want: "0"},
		{name: "costs shorter than the product", grid: pathfinding.CostGrid{Width: 2, Height: 2, Costs: []pathfinding.Cost{pathfinding.MinCost, pathfinding.MinCost, pathfinding.MinCost}}, want: "3"},
		{name: "costs longer than the product", grid: pathfinding.CostGrid{Width: 2, Height: 2, Costs: make([]pathfinding.Cost, 5)}, want: "5"},
		{name: "nil costs", grid: pathfinding.CostGrid{Width: 2, Height: 2}, want: "0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.grid.Validate()
			require.ErrorIs(t, err, daedalus.ErrInvalidNavigation)
			assert.NotErrorIs(t, err, daedalus.ErrLimitExceeded)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestGridOverTheCellCeilingReturnsLimitExceeded(t *testing.T) {
	grid := pathfinding.CostGrid{Width: 256, Height: 257, Costs: []pathfinding.Cost{pathfinding.MinCost}}
	err := grid.Validate()
	require.ErrorIs(t, err, daedalus.ErrLimitExceeded)
	assert.NotErrorIs(t, err, daedalus.ErrInvalidNavigation)
	assert.Contains(t, err.Error(), "65792")

	legalSide := pathfinding.CostGrid{Width: 1000, Height: 1, Costs: make([]pathfinding.Cost, 1000)}
	require.NoError(t, legalSide.Validate())

	exact := pathfinding.CostGrid{Width: 256, Height: 256, Costs: make([]pathfinding.Cost, daedalus.MaxCells)}
	require.NoError(t, exact.Validate())
}

func TestDefaultCostRulePricesEntry(t *testing.T) {
	roomID := daedalus.RoomID(1)
	corridorID := daedalus.CorridorID(2)
	assert.Equal(t, pathfinding.CostImpassable, pathfinding.DefaultCostRule(daedalus.CellState{Kind: daedalus.CellKindEmpty}))
	assert.Equal(t, pathfinding.MinCost, pathfinding.DefaultCostRule(daedalus.CellState{Kind: daedalus.CellKindRoom, RoomID: &roomID}))
	assert.Equal(t, pathfinding.MinCost, pathfinding.DefaultCostRule(daedalus.CellState{Kind: daedalus.CellKindCorridor, CorridorIDs: []daedalus.CorridorID{corridorID}}))
	assert.Equal(t, pathfinding.CostImpassable, pathfinding.DefaultCostRule(daedalus.CellState{Kind: daedalus.CellKind(99)}))
	assert.Equal(t, pathfinding.Cost(1), pathfinding.MinCost)
	assert.Equal(t, pathfinding.Cost(255), pathfinding.MaxCost)
}

func TestNewCostGridUsesTheDefaultRule(t *testing.T) {
	layout := rectangleLayout()
	grid := pathfinding.NewCostGrid(layout)

	require.NoError(t, grid.Validate())
	assert.Equal(t, uint32(2), grid.Width)
	assert.Equal(t, uint32(2), grid.Height)
	assert.Equal(t, pathfinding.CostImpassable, grid.At(daedalus.Cell{X: 0, Y: 0}))
	assert.Equal(t, pathfinding.MinCost, grid.At(daedalus.Cell{X: 1, Y: 0}))
	assert.Equal(t, pathfinding.MinCost, grid.At(daedalus.Cell{X: 0, Y: 1}))
	assert.Equal(t, pathfinding.CostImpassable, grid.At(daedalus.Cell{X: 1, Y: 1}))
}

func TestNewCostGridFuncNilRuleUsesTheDefault(t *testing.T) {
	layout := rectangleLayout()
	assert.Equal(t, pathfinding.NewCostGrid(layout), pathfinding.NewCostGridFunc(layout, nil))
}

func TestNewCostGridFuncAppliesTheRuleToEveryCell(t *testing.T) {
	layout := rectangleLayout()
	layout.Grid.Cells = layout.Grid.Cells[:2]
	var calls int
	grid := pathfinding.NewCostGridFunc(layout, func(state daedalus.CellState) pathfinding.Cost {
		calls++
		if state.Kind == daedalus.CellKindRoom {
			return pathfinding.MaxCost
		}
		return pathfinding.MinCost
	})

	assert.Equal(t, 4, calls)
	require.NoError(t, grid.Validate())
	assert.Equal(t, pathfinding.MinCost, grid.At(daedalus.Cell{X: 0, Y: 0}))
	assert.Equal(t, pathfinding.MaxCost, grid.At(daedalus.Cell{X: 1, Y: 0}))
	assert.Equal(t, pathfinding.MinCost, grid.At(daedalus.Cell{X: 0, Y: 1}))
	assert.Equal(t, pathfinding.MinCost, grid.At(daedalus.Cell{X: 1, Y: 1}))
}

func TestNewCostGridRefusesADimensionCeilingWithoutFillingCosts(t *testing.T) {
	grid := pathfinding.NewCostGrid(daedalus.Layout{Grid: daedalus.Grid{Width: daedalus.MaxCells, Height: 2}})
	assert.Nil(t, grid.Costs)
	require.ErrorIs(t, grid.Validate(), daedalus.ErrLimitExceeded)
}

func TestSetWritesACellTheGridContains(t *testing.T) {
	at := daedalus.Cell{X: 1, Y: 0}
	grid := pathfinding.CostGrid{Width: 2, Height: 1, Costs: []pathfinding.Cost{pathfinding.MinCost, pathfinding.MinCost}}

	require.True(t, grid.Set(at, pathfinding.MaxCost))
	assert.Equal(t, pathfinding.MaxCost, grid.At(at))
	assert.False(t, grid.Set(daedalus.Cell{X: 2, Y: 0}, pathfinding.CostImpassable))
	assert.Equal(t, pathfinding.MinCost, grid.At(daedalus.Cell{X: 0, Y: 0}))

	short := pathfinding.CostGrid{Width: 2, Height: 1}
	assert.False(t, short.Set(daedalus.Cell{X: 0, Y: 0}, pathfinding.MinCost))
}

func TestIndexIsRowMajor(t *testing.T) {
	grid := pathfinding.CostGrid{Width: 3, Height: 2, Costs: make([]pathfinding.Cost, 6)}
	index, ok := grid.Index(daedalus.Cell{X: 2, Y: 1})
	require.True(t, ok)
	assert.Equal(t, int64(5), index)

	index, ok = grid.Index(daedalus.Cell{X: 0, Y: 0})
	require.True(t, ok)
	assert.Equal(t, int64(0), index)
}

func TestRoomSourcesCopiesTheFootprint(t *testing.T) {
	room := daedalus.Room{Cells: []daedalus.Cell{{X: 2, Y: 0}, {X: 0, Y: 1}, {X: 1, Y: 1}}}

	got := pathfinding.RoomSources(room, 0)
	assert.Equal(t, room.Cells, got)
	assert.Equal(t, got, pathfinding.RoomSources(room, 40))

	got[0].X = 9
	assert.Equal(t, int32(2), room.Cells[0].X, "the returned slice must not share the footprint")

	assert.Nil(t, pathfinding.RoomSources(room, -1))

	empty := pathfinding.RoomSources(daedalus.Room{}, 0)
	assert.NotNil(t, empty)
	assert.Empty(t, empty)
}

func TestLimitsMatchTheCallCeilings(t *testing.T) {
	assert.Equal(t, 4096, pathfinding.MaxSources)
	assert.Equal(t, daedalus.MaxFootprintCells, pathfinding.MaxSources)
	assert.Equal(t, 16, pathfinding.MaxQueries)
	assert.Equal(t, 16384, pathfinding.MaxStepsPerCall)
}

func TestPathfindingIsIntegerOnly(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	directory := filepath.Dir(thisFile)
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	fileSet := token.NewFileSet()
	var parsed int
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, filepath.Join(directory, name), nil, parser.SkipObjectResolution)
		require.NoError(t, err)
		parsed++
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.BasicLit:
				assert.NotEqual(t, token.FLOAT, typed.Kind, "float literal %s", typed.Value)
			case *ast.Ident:
				assert.NotEqual(t, "float32", typed.Name)
				assert.NotEqual(t, "float64", typed.Name)
			case *ast.SelectorExpr:
				ident, ok := typed.X.(*ast.Ident)
				if ok {
					assert.NotEqual(t, "math", ident.Name)
				}
			case *ast.ImportSpec:
				path, err := strconv.Unquote(typed.Path.Value)
				require.NoError(t, err)
				assert.NotEqual(t, "math", path)
			}
			return true
		})
	}
	require.Positive(t, parsed)
}

func rectangleLayout() daedalus.Layout {
	roomID := daedalus.RoomID(0)
	corridorID := daedalus.CorridorID(0)
	return daedalus.Layout{
		Grid: daedalus.Grid{
			Width:  2,
			Height: 2,
			Cells: []daedalus.CellState{
				{At: daedalus.Cell{X: 0, Y: 0}, Kind: daedalus.CellKindEmpty},
				{At: daedalus.Cell{X: 1, Y: 0}, Kind: daedalus.CellKindRoom, RoomID: &roomID},
				{At: daedalus.Cell{X: 0, Y: 1}, Kind: daedalus.CellKindCorridor, CorridorIDs: []daedalus.CorridorID{corridorID}},
				{At: daedalus.Cell{X: 1, Y: 1}, Kind: daedalus.CellKindEmpty},
			},
		},
		Rooms: []daedalus.Room{{
			ID:    roomID,
			At:    daedalus.Cell{X: 1, Y: 0},
			Cells: []daedalus.Cell{{X: 1, Y: 0}},
		}},
	}
}
