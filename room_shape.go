package daedalus

// RoomShape identifica a máscara canônica de uma Room. A ordem canônica é
// Rectangle, L, T, Cross e Circle; Rectangle é o valor zero.
type RoomShape int

const (
	// RoomShapeRectangle ocupa toda a bounding box.
	RoomShapeRectangle RoomShape = iota
	// RoomShapeL ocupa a linha superior e a coluna esquerda.
	RoomShapeL
	// RoomShapeT ocupa a linha superior e a coluna central.
	RoomShapeT
	// RoomShapeCross ocupa a linha central e a coluna central.
	RoomShapeCross
	// RoomShapeCircle ocupa o disco inteiro dentro de uma bounding box quadrada.
	RoomShapeCircle
)

const (
	// Limites geométricos invariantes das máscaras do contrato v1.
	roomShapeMinDimension uint32 = 2
	roomShapeMinWide      uint32 = 3
	roomShapeMinCircle    uint32 = 5
	roomShapeParity       uint32 = 2
	roomShapeMaxCellCoord uint32 = 1<<31 - 1
)

// ValidRoomShapeDimensions informa se width e height descrevem uma máscara
// válida para shape, sem construir uma Room ou seus offsets.
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

// RoomShapeOffsets devolve os offsets locais canônicos da máscara, relativos
// à Origin e ordenados por Y e depois X. Dimensões inválidas devolvem nil.
func RoomShapeOffsets(shape RoomShape, width, height uint32) []Cell {
	return roomShapeOffsetsInto(shape, width, height, nil)
}

// roomShapeOffsetsInto materializa a máscara no buffer privado informado.
// O wrapper público preserva slices próprios ao chamar sem buffer.
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

// RoomShapeFirstOffset devolve o offset da primeira Cell ocupada em ordem
// canônica Y/X. Dimensões inválidas devolvem a Cell zero; valide-as com
// ValidRoomShapeDimensions antes de usar o resultado.
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
