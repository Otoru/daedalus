package vision_test

import (
	"testing"

	"github.com/Otoru/daedalus"
	"github.com/Otoru/daedalus/utils/vision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestZeroOpacityGridIsOpaque(t *testing.T) {
	var grid vision.OpacityGrid

	for _, at := range []daedalus.Cell{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 0, Y: 1}, {X: -1, Y: -1}} {
		assert.False(t, grid.TransparentAt(at), "zero grid at %+v", at)
		_, inside := grid.Index(at)
		assert.False(t, inside, "zero grid contains %+v", at)
	}

	filled := vision.OpacityGrid{Width: 2, Height: 2, Transparent: make([]byte, 1)}
	assert.False(t, filled.TransparentAt(daedalus.Cell{X: 1, Y: 1}))
	require.NoError(t, filled.Validate())
}

func TestTransparentAtOutsideTheGridIsOpaque(t *testing.T) {
	grid := vision.OpacityGrid{Width: 2, Height: 3, Transparent: []byte{0xFF}}

	outside := []daedalus.Cell{
		{X: -1, Y: 0},
		{X: 0, Y: -1},
		{X: 2, Y: 0},
		{X: 0, Y: 3},
		{X: 5, Y: 5},
	}
	for _, at := range outside {
		assert.NotPanics(t, func() {
			assert.False(t, grid.TransparentAt(at), "outside %+v", at)
		})
		_, inside := grid.Index(at)
		assert.False(t, inside, "%+v", at)
	}

	short := vision.OpacityGrid{Width: 3, Height: 3, Transparent: []byte{0xFF}}
	assert.NotPanics(t, func() {
		assert.True(t, short.TransparentAt(daedalus.Cell{X: 0, Y: 0}))
		assert.False(t, short.TransparentAt(daedalus.Cell{X: 2, Y: 2}))
	})
	assert.False(t, short.SetTransparent(daedalus.Cell{X: 2, Y: 2}, true))
	assert.Equal(t, []byte{0xFF}, short.Transparent)
}

func TestBitsAreLeastSignificantFirst(t *testing.T) {
	for i := 0; i < 16; i++ {
		grid := vision.OpacityGrid{Width: 16, Height: 1, Transparent: make([]byte, 2)}
		at := daedalus.Cell{X: int32(i)}
		require.True(t, grid.SetTransparent(at, true))

		byteIndex := i / 8
		bit := byte(1 << (i % 8))
		assert.Equal(t, bit, grid.Transparent[byteIndex], "cell %d", i)
		assert.Equal(t, byte(0), grid.Transparent[1-byteIndex], "cell %d spills", i)
		assert.True(t, grid.TransparentAt(at), "cell %d", i)
		require.NoError(t, grid.Validate())
	}
}

func TestIndexIsRowMajor(t *testing.T) {
	grid := vision.OpacityGrid{Width: 3, Height: 2, Transparent: make([]byte, 1)}
	index, ok := grid.Index(daedalus.Cell{X: 2, Y: 1})
	require.True(t, ok)
	assert.Equal(t, int64(5), index)

	index, ok = grid.Index(daedalus.Cell{X: 0, Y: 0})
	require.True(t, ok)
	assert.Equal(t, int64(0), index)

	require.True(t, grid.SetTransparent(daedalus.Cell{X: 0, Y: 1}, true))
	assert.Equal(t, byte(1<<3), grid.Transparent[0])
	assert.True(t, grid.TransparentAt(daedalus.Cell{X: 0, Y: 1}))
	assert.False(t, grid.TransparentAt(daedalus.Cell{X: 1, Y: 0}))
}

func TestSetTransparentEditsOneBit(t *testing.T) {
	at := daedalus.Cell{X: 1, Y: 0}
	grid := vision.OpacityGrid{Width: 3, Height: 1, Transparent: []byte{0x07}}

	require.True(t, grid.SetTransparent(at, false))
	assert.False(t, grid.TransparentAt(at))
	assert.True(t, grid.TransparentAt(daedalus.Cell{X: 0}))
	assert.True(t, grid.TransparentAt(daedalus.Cell{X: 2}))
	assert.Equal(t, []byte{0x05}, grid.Transparent)

	require.True(t, grid.SetTransparent(at, true))
	assert.Equal(t, []byte{0x07}, grid.Transparent)
	assert.False(t, grid.SetTransparent(daedalus.Cell{X: 3}, true))
	assert.Equal(t, []byte{0x07}, grid.Transparent)

	short := vision.OpacityGrid{Width: 2, Height: 1}
	assert.False(t, short.SetTransparent(daedalus.Cell{X: 0}, true))
}

func TestCloneDetachesFromTheOriginal(t *testing.T) {
	at := daedalus.Cell{X: 0, Y: 0}
	original := vision.OpacityGrid{Width: 1, Height: 1, Transparent: []byte{0x01}}
	clone := original.Clone()

	require.True(t, clone.SetTransparent(at, false))
	assert.True(t, original.TransparentAt(at), "writing the clone must leave the original untouched")
	assert.False(t, clone.TransparentAt(at))

	require.True(t, original.SetTransparent(at, true))
	clone.Transparent[0] = 0x00
	assert.Equal(t, byte(0x01), original.Transparent[0], "writing the clone slice must leave the original untouched")
	assert.False(t, clone.TransparentAt(at))

	nilGrid := vision.OpacityGrid{}
	assert.Nil(t, nilGrid.Clone().Transparent)

	empty := vision.OpacityGrid{Transparent: []byte{}}
	clonedEmpty := empty.Clone()
	assert.NotNil(t, clonedEmpty.Transparent)
	assert.Empty(t, clonedEmpty.Transparent)
}

func TestMalformedGridReturnsInvalidVisibility(t *testing.T) {
	cases := []struct {
		name string
		grid vision.OpacityGrid
		want string
	}{
		{name: "zero width", grid: vision.OpacityGrid{Width: 0, Height: 4}, want: "0"},
		{name: "zero height", grid: vision.OpacityGrid{Width: 4, Height: 0, Transparent: []byte{0xFF}}, want: "0"},
		{name: "both dimensions zero", grid: vision.OpacityGrid{}, want: "0"},
		{name: "bytes shorter than the product", grid: vision.OpacityGrid{Width: 9, Height: 1, Transparent: []byte{0xFF}}, want: "1"},
		{name: "bytes longer than the product", grid: vision.OpacityGrid{Width: 8, Height: 1, Transparent: []byte{0xFF, 0x00}}, want: "2"},
		{name: "nil bytes", grid: vision.OpacityGrid{Width: 2, Height: 2}, want: "0"},
		{name: "padding bit set", grid: vision.OpacityGrid{Width: 1, Height: 1, Transparent: []byte{0x02}}, want: "padding"},
		{name: "high padding on a partial byte", grid: vision.OpacityGrid{Width: 7, Height: 1, Transparent: []byte{0xFF}}, want: "padding"},
		{name: "padding in the final byte only", grid: vision.OpacityGrid{Width: 9, Height: 1, Transparent: []byte{0xFF, 0x02}}, want: "padding"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.grid.Validate()
			require.ErrorIs(t, err, daedalus.ErrInvalidVisibility)
			assert.NotErrorIs(t, err, daedalus.ErrLimitExceeded)
			assert.NotErrorIs(t, err, daedalus.ErrInvalidNavigation)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestCanonicalPaddingIsAccepted(t *testing.T) {
	cases := []vision.OpacityGrid{
		{Width: 1, Height: 1, Transparent: []byte{0x01}},
		{Width: 1, Height: 1, Transparent: []byte{0x00}},
		{Width: 7, Height: 1, Transparent: []byte{0x7F}},
		{Width: 8, Height: 1, Transparent: []byte{0xFF}},
		{Width: 9, Height: 1, Transparent: []byte{0xFF, 0x01}},
		{Width: 9, Height: 1, Transparent: []byte{0x00, 0x00}},
	}
	for _, grid := range cases {
		require.NoError(t, grid.Validate(), "%+v", grid)
	}
}

func TestGridOverTheCellCeilingReturnsLimitExceeded(t *testing.T) {
	grid := vision.OpacityGrid{Width: 256, Height: 257, Transparent: []byte{0x01}}
	err := grid.Validate()
	require.ErrorIs(t, err, daedalus.ErrLimitExceeded)
	assert.NotErrorIs(t, err, daedalus.ErrInvalidVisibility)
	assert.Contains(t, err.Error(), "65792")

	legalSide := vision.OpacityGrid{Width: 1000, Height: 1, Transparent: make([]byte, 125)}
	require.NoError(t, legalSide.Validate())

	skinny := vision.OpacityGrid{Width: daedalus.MaxCells, Height: 1, Transparent: make([]byte, daedalus.MaxCells/8)}
	require.NoError(t, skinny.Validate())

	exact := vision.OpacityGrid{Width: 256, Height: 256, Transparent: make([]byte, daedalus.MaxCells/8)}
	require.NoError(t, exact.Validate())
}

func TestDefaultOpacityRuleReadsTerrain(t *testing.T) {
	roomID := daedalus.RoomID(1)
	corridorID := daedalus.CorridorID(2)
	assert.False(t, vision.DefaultOpacityRule(daedalus.CellState{Kind: daedalus.CellKindEmpty}))
	assert.True(t, vision.DefaultOpacityRule(daedalus.CellState{Kind: daedalus.CellKindRoom, RoomID: &roomID}))
	assert.True(t, vision.DefaultOpacityRule(daedalus.CellState{Kind: daedalus.CellKindRoom}))
	assert.True(t, vision.DefaultOpacityRule(daedalus.CellState{Kind: daedalus.CellKindCorridor, CorridorIDs: []daedalus.CorridorID{corridorID}}))
	assert.False(t, vision.DefaultOpacityRule(daedalus.CellState{Kind: daedalus.CellKind(99)}))
	assert.False(t, vision.DefaultOpacityRule(daedalus.CellState{}))
}

func TestNewOpacityGridUsesTheDefaultRule(t *testing.T) {
	layout := rectangleLayout()
	grid := vision.NewOpacityGrid(layout)

	require.NoError(t, grid.Validate())
	assert.Equal(t, uint32(2), grid.Width)
	assert.Equal(t, uint32(2), grid.Height)
	assert.Equal(t, []byte{0x06}, grid.Transparent)
	assert.False(t, grid.TransparentAt(daedalus.Cell{X: 0, Y: 0}))
	assert.True(t, grid.TransparentAt(daedalus.Cell{X: 1, Y: 0}))
	assert.True(t, grid.TransparentAt(daedalus.Cell{X: 0, Y: 1}))
	assert.False(t, grid.TransparentAt(daedalus.Cell{X: 1, Y: 1}))
}

func TestNewOpacityGridFuncNilRuleUsesTheDefault(t *testing.T) {
	layout := rectangleLayout()
	assert.Equal(t, vision.NewOpacityGrid(layout), vision.NewOpacityGridFunc(layout, nil))
}

func TestNewOpacityGridFuncAppliesTheRuleToEveryCell(t *testing.T) {
	layout := rectangleLayout()
	layout.Grid.Cells = layout.Grid.Cells[:2]
	var calls int
	grid := vision.NewOpacityGridFunc(layout, func(state daedalus.CellState) bool {
		calls++
		return state.Kind == daedalus.CellKindRoom
	})

	assert.Equal(t, 4, calls)
	require.NoError(t, grid.Validate())
	assert.Equal(t, []byte{0x02}, grid.Transparent)
	assert.False(t, grid.TransparentAt(daedalus.Cell{X: 0, Y: 0}))
	assert.True(t, grid.TransparentAt(daedalus.Cell{X: 1, Y: 0}))
	assert.False(t, grid.TransparentAt(daedalus.Cell{X: 0, Y: 1}))
	assert.False(t, grid.TransparentAt(daedalus.Cell{X: 1, Y: 1}))
}

func TestNewOpacityGridPacksANonByteAlignedLayout(t *testing.T) {
	roomID := daedalus.RoomID(1)
	corridorID := daedalus.CorridorID(1)
	room := daedalus.CellState{Kind: daedalus.CellKindRoom, RoomID: &roomID}
	corridor := daedalus.CellState{Kind: daedalus.CellKindCorridor, CorridorIDs: []daedalus.CorridorID{corridorID}}
	empty := daedalus.CellState{Kind: daedalus.CellKindEmpty}
	layout := daedalus.Layout{Grid: daedalus.Grid{
		Width:  3,
		Height: 3,
		Cells:  []daedalus.CellState{empty, room, corridor, empty, room, empty, corridor, empty, room},
	}}

	grid := vision.NewOpacityGrid(layout)

	require.NoError(t, grid.Validate())
	assert.Equal(t, []byte{0x56, 0x01}, grid.Transparent)
	assert.True(t, grid.TransparentAt(daedalus.Cell{X: 2, Y: 2}))
	assert.False(t, grid.TransparentAt(daedalus.Cell{X: 1, Y: 2}))
}

func TestNewOpacityGridRefusesADimensionCeilingWithoutFillingBits(t *testing.T) {
	grid := vision.NewOpacityGrid(daedalus.Layout{Grid: daedalus.Grid{Width: daedalus.MaxCells, Height: 2}})
	assert.Nil(t, grid.Transparent)
	require.ErrorIs(t, grid.Validate(), daedalus.ErrLimitExceeded)
}

func TestNewOpacityGridLeavesAZeroDimensionUnfilled(t *testing.T) {
	grid := vision.NewOpacityGrid(daedalus.Layout{})
	assert.Nil(t, grid.Transparent)
	require.ErrorIs(t, grid.Validate(), daedalus.ErrInvalidVisibility)
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
	}
}
