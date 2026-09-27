package daedalus

// RoomShape identifies a Room's canonical mask. The canonical order is
// Rectangle, L, T, Cross, and Circle; Rectangle is the zero value.
type RoomShape int

const (
	// RoomShapeRectangle occupies the entire bounding box.
	RoomShapeRectangle RoomShape = iota
	// RoomShapeL occupies the top row and left column.
	RoomShapeL
	// RoomShapeT occupies the top row and center column.
	RoomShapeT
	// RoomShapeCross occupies the center row and center column.
	RoomShapeCross
	// RoomShapeCircle occupies the full disk inside a square bounding box.
	RoomShapeCircle
)

const (
	// Invariant geometric limits for the v1 contract masks.
	roomShapeMinDimension uint32 = 2
	roomShapeMinWide      uint32 = 3
	roomShapeMinCircle    uint32 = 5
	roomShapeParity       uint32 = 2
	roomShapeMaxCellCoord uint32 = 1<<31 - 1
)

// ValidRoomShapeDimensions reports whether width and height describe a valid
// mask for shape without constructing a Room or its offsets.
func ValidRoomShapeDimensions(shape RoomShape, width, height uint32) bool {
	if width == 0 || height == 0 || width > roomShapeMaxCellCoord || height > roomShapeMaxCellCoord {
		return false
	}

	switch shape {
	case RoomShapeRectangle:
		return true
	case RoomShapeL:
		return width >= roomShapeMinDimension && height >= roomShapeMinDimension
	case RoomShapeT:
		return width >= roomShapeMinWide && height >= roomShapeMinDimension
	case RoomShapeCross:
		return width >= roomShapeMinWide && height >= roomShapeMinWide
	case RoomShapeCircle:
		return width == height && width >= roomShapeMinCircle && width%roomShapeParity != 0
	default:
		return false
	}
}

// RoomShapeOffsets returns the mask's canonical local offsets relative to
// Origin, ordered by Y then X. Invalid dimensions return nil.
func RoomShapeOffsets(shape RoomShape, width, height uint32) []Cell {
	return roomShapeOffsetsInto(shape, width, height, nil)
}

// roomShapeOffsetsInto materializes the mask in the supplied private buffer.
// The public wrapper preserves independent slices by calling it without a buffer.
func roomShapeOffsetsInto(shape RoomShape, width, height uint32, buffer []Cell) []Cell {
	if !ValidRoomShapeDimensions(shape, width, height) {
		return nil
	}

	offsets := buffer[:0]
	centerX := int64(width-1) / int64(roomShapeParity)
	centerY := int64(height-1) / int64(roomShapeParity)
	radiusSquared := centerX * centerX
	for y := int64(0); y < int64(height); y++ {
		for x := int64(0); x < int64(width); x++ {
			if roomShapeContains(shape, x, y, centerX, centerY, radiusSquared) {
				offsets = append(offsets, Cell{X: int32(x), Y: int32(y)})
			}
		}
	}
	return offsets
}

// RoomShapeFirstOffset returns the offset of the first occupied Cell in
// canonical Y/X order. Invalid dimensions return the zero Cell; validate them
// with ValidRoomShapeDimensions before using the result.
func RoomShapeFirstOffset(shape RoomShape, width, height uint32) Cell {
	if !ValidRoomShapeDimensions(shape, width, height) {
		return Cell{}
	}

	switch shape {
	case RoomShapeCross, RoomShapeCircle:
		return Cell{X: int32((width - 1) / roomShapeParity)}
	default:
		return Cell{}
	}
}

func roomShapeContains(shape RoomShape, x, y, centerX, centerY, radiusSquared int64) bool {
	switch shape {
	case RoomShapeRectangle:
		return true
	case RoomShapeL:
		return y == 0 || x == 0
	case RoomShapeT:
		return y == 0 || x == centerX
	case RoomShapeCross:
		return y == centerY || x == centerX
	case RoomShapeCircle:
		deltaX := x - centerX
		deltaY := y - centerY
		deltaXSquared := deltaX * deltaX
		deltaYSquared := deltaY * deltaY
		distanceSquared := deltaXSquared + deltaYSquared
		return distanceSquared <= radiusSquared
	default:
		return false
	}
}
