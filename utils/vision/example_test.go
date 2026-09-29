package vision_test

import (
	"context"
	"fmt"

	"github.com/Otoru/daedalus"
	"github.com/Otoru/daedalus/utils/vision"
)

func ExampleCompute() {
	grid := vision.OpacityGrid{Width: 3, Height: 1, Transparent: []byte{0x07}}
	field, _ := vision.Compute(context.Background(), grid, daedalus.Cell{X: 1}, 1)
	fmt.Println(field.VisibleCells(nil))

	// Output:
	// [{0 0} {1 0} {2 0}]
}

// A generated Layout becomes a packed transparency grid. Room and corridor
// cells are open; empty cells stay solid. The printed byte is LSB-first:
// the rightmost bit is cell 0.
func ExampleNewOpacityGrid() {
	room := daedalus.RoomID(1)
	corridor := daedalus.CorridorID(1)
	layout := daedalus.Layout{Grid: daedalus.Grid{
		Width:  3,
		Height: 1,
		Cells: []daedalus.CellState{
			{Kind: daedalus.CellKindEmpty},
			{Kind: daedalus.CellKindRoom, RoomID: &room},
			{Kind: daedalus.CellKindCorridor, CorridorIDs: []daedalus.CorridorID{corridor}},
		},
	}}
	grid := vision.NewOpacityGrid(layout)
	fmt.Printf("%08b\n", grid.Transparent[0])
	fmt.Println(grid.TransparentAt(daedalus.Cell{X: 0}))
	fmt.Println(grid.TransparentAt(daedalus.Cell{X: 1}))
	fmt.Println(grid.TransparentAt(daedalus.Cell{X: 2}))

	// Output:
	// 00000110
	// false
	// true
	// true
}

// Smoke on a room floor is not the default conversion. NewOpacityGridFunc
// is how a caller keeps the corridor open and the smoked room solid.
func ExampleNewOpacityGridFunc() {
	room := daedalus.RoomID(1)
	layout := daedalus.Layout{Grid: daedalus.Grid{
		Width:  2,
		Height: 1,
		Cells: []daedalus.CellState{
			{Kind: daedalus.CellKindRoom, RoomID: &room},
			{Kind: daedalus.CellKindCorridor, CorridorIDs: []daedalus.CorridorID{1}},
		},
	}}
	grid := vision.NewOpacityGridFunc(layout, func(state daedalus.CellState) bool {
		return state.Kind == daedalus.CellKindCorridor
	})
	fmt.Println(grid.TransparentAt(daedalus.Cell{X: 0}))
	fmt.Println(grid.TransparentAt(daedalus.Cell{X: 1}))
	fmt.Printf("%08b\n", grid.Transparent[0])

	// Output:
	// false
	// true
	// 00000010
}
