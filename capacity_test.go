package daedalus

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoomOpeningCapacityLeavesTwoCellsBetweenOpenings(t *testing.T) {
	// A 1×1 has two opposite exits and no wall long enough for a second
	// opening on the same side. A 3×3 side holds one width-1 opening if two
	// Cells of slack are kept, and the four centered sides stay apart; the
	// same square with width 3 can use only a pair of opposite sides.
	assert.Equal(t, 2, roomOpeningCapacity(rectangleCells(0, 0, 1, 1), 0, 0, 1))
	assert.Equal(t, 4, roomOpeningCapacity(rectangleCells(0, 0, 3, 3), 0, 0, 1))
	assert.Equal(t, 2, roomOpeningCapacity(rectangleCells(0, 0, 3, 3), 0, 0, 3))
	wide := roomOpeningCapacity(rectangleCells(0, 0, 8, 3), 0, 0, 1)
	assert.Greater(t, wide, 4, "a longer wall hosts more separated openings than a 3×3")
}

func TestWidthThreeOnThreeByThreeLeavesOppositeSidesOnly(t *testing.T) {
	cells := rectangleCells(0, 0, 3, 3)
	assert.Equal(t, 2, roomOpeningCapacity(cells, 0, 0, 3))
}

func TestCornerRoomOnTheGridBorderHasOneOpening(t *testing.T) {
	cells := rectangleCells(0, 0, 1, 1)
	assert.Equal(t, 1, roomOpeningCapacity(cells, 8, 8, 1))
	assert.Equal(t, 2, roomOpeningCapacity(cells, 0, 0, 1), "an unnamed Grid does not clip exits")
}

func TestZeroWidthBudgetsOneCellAndEmptyFootprintHostsNothing(t *testing.T) {
	cells := rectangleCells(2, 2, 1, 1)
	assert.Equal(t, roomOpeningCapacity(cells, 0, 0, 1), roomOpeningCapacity(cells, 0, 0, 0))
	assert.Equal(t, 0, roomOpeningCapacity(nil, 0, 0, 1))
}

func TestOpeningCapacityIgnoresFootprintOrder(t *testing.T) {
	cells := rectangleCells(1, 4, 4, 3)
	reversed := append([]Cell(nil), cells...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	assert.Equal(t, roomOpeningCapacity(cells, 20, 20, 1), roomOpeningCapacity(reversed, 20, 20, 1))
	assert.Equal(t, roomOpeningCapacity(cells, 20, 20, 2), roomOpeningCapacity(reversed, 20, 20, 2))
}

func TestBoundaryTraceListsEveryExitOnce(t *testing.T) {
	shapes := []RoomShape{
		RoomShapeRectangle, RoomShapeL, RoomShapeT, RoomShapeCross, RoomShapeCircle,
	}
	for _, shape := range shapes {
		for size := uint32(1); size <= 9; size++ {
			if !ValidRoomShapeDimensions(shape, size, size) {
				continue
			}
			cells := RoomShapeOffsets(shape, size, size)
			slots := traceBoundarySlots(cells)
			require.NotEmpty(t, slots, "%s %d", shape, size)
			seen := make(map[boundarySlot]struct{}, len(slots))
			for _, slot := range slots {
				_, duplicate := seen[slot]
				require.False(t, duplicate, "duplicate slot %v on %s %d", slot, shape, size)
				seen[slot] = struct{}{}
			}
			occupied := footprintSet(cells)
			exits := 0
			for _, cell := range cells {
				for direction := DirectionNorth; direction <= DirectionWest; direction++ {
					delta := direction.Delta()
					neighbor := Cell{X: cell.X + delta.X, Y: cell.Y + delta.Y}
					if _, inside := occupied[neighbor]; inside {
						continue
					}
					exits++
					_, traced := seen[boundarySlot{cell: cell, out: direction}]
					assert.True(t, traced, "missing %s exit %v %v", shape, cell, direction)
				}
			}
			assert.Equal(t, exits, len(slots), "%s %d", shape, size)
		}
	}
}

func TestCircularPackingMatchesExactSearchOnSmallFootprints(t *testing.T) {
	shapes := []RoomShape{
		RoomShapeRectangle, RoomShapeL, RoomShapeT, RoomShapeCross, RoomShapeCircle,
	}
	for _, shape := range shapes {
		for size := uint32(1); size <= 7; size++ {
			if !ValidRoomShapeDimensions(shape, size, size) {
				continue
			}
			for _, width := range []uint32{1, 2, 3} {
				cells := RoomShapeOffsets(shape, size, size)
				slots := traceBoundarySlots(cells)
				openings := doorwayOpenings(slots, width, 0, 0, footprintSet(cells))
				if len(openings) == 0 || len(openings) > 16 {
					continue
				}
				conflict := func(left, right int) bool {
					return doorwaysConflict(openings[left], openings[right])
				}
				assert.Equal(t, exactDoorwayMIS(len(openings), conflict), maximumCompatibleOpenings(openings),
					"%s %d width %d openings %d", shape, size, width, len(openings))
			}
		}
	}
}

func TestPlacedRoomFootprintUsesCellsThenTheBoundingBox(t *testing.T) {
	named := PlacedRoom{Cells: []Cell{{X: 4, Y: 5}}}
	assert.Equal(t, []Cell{{X: 4, Y: 5}}, placedRoomFootprint(named))

	box := PlacedRoom{Origin: Cell{X: 2, Y: 3}, Width: 2, Height: 1}
	assert.Equal(t, []Cell{{X: 2, Y: 3}, {X: 3, Y: 3}}, placedRoomFootprint(box))
	assert.Nil(t, placedRoomFootprint(PlacedRoom{}))
}

func rectangleCells(originX, originY int32, width, height uint32) []Cell {
	cells := make([]Cell, 0, width*height)
	for y := uint32(0); y < height; y++ {
		for x := uint32(0); x < width; x++ {
			cells = append(cells, Cell{X: originX + int32(x), Y: originY + int32(y)})
		}
	}
	return cells
}
