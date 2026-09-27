package daedalus

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMascaraRetangulo(t *testing.T) {
	got := RoomShapeOffsets(RoomShapeRectangle, 3, 2)
	want := []Cell{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 2, Y: 0}, {X: 0, Y: 1}, {X: 1, Y: 1}, {X: 2, Y: 1}}
	verificarCellsIguais(t, got, want)
}

func TestMascaraL(t *testing.T) {
	got := RoomShapeOffsets(RoomShapeL, 3, 3)
	want := []Cell{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 2, Y: 0}, {X: 0, Y: 1}, {X: 0, Y: 2}}
	verificarCellsIguais(t, got, want)
}

func TestMascaraT(t *testing.T) {
	got := RoomShapeOffsets(RoomShapeT, 5, 3)
	want := []Cell{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 2, Y: 0}, {X: 3, Y: 0}, {X: 4, Y: 0}, {X: 2, Y: 1}, {X: 2, Y: 2}}
	verificarCellsIguais(t, got, want)
}

func TestMascaraCross(t *testing.T) {
	got := RoomShapeOffsets(RoomShapeCross, 5, 5)
	want := []Cell{{X: 2, Y: 0}, {X: 2, Y: 1}, {X: 0, Y: 2}, {X: 1, Y: 2}, {X: 2, Y: 2}, {X: 3, Y: 2}, {X: 4, Y: 2}, {X: 2, Y: 3}, {X: 2, Y: 4}}
	verificarCellsIguais(t, got, want)
}

func TestMascaraCircleD5ED7(t *testing.T) {
	tests := []struct {
		diameter uint32
		want     []Cell
	}{
		{5, []Cell{{X: 2, Y: 0}, {X: 1, Y: 1}, {X: 2, Y: 1}, {X: 3, Y: 1}, {X: 0, Y: 2}, {X: 1, Y: 2}, {X: 2, Y: 2}, {X: 3, Y: 2}, {X: 4, Y: 2}, {X: 1, Y: 3}, {X: 2, Y: 3}, {X: 3, Y: 3}, {X: 2, Y: 4}}},
		{7, []Cell{{X: 3, Y: 0}, {X: 1, Y: 1}, {X: 2, Y: 1}, {X: 3, Y: 1}, {X: 4, Y: 1}, {X: 5, Y: 1}, {X: 1, Y: 2}, {X: 2, Y: 2}, {X: 3, Y: 2}, {X: 4, Y: 2}, {X: 5, Y: 2}, {X: 0, Y: 3}, {X: 1, Y: 3}, {X: 2, Y: 3}, {X: 3, Y: 3}, {X: 4, Y: 3}, {X: 5, Y: 3}, {X: 6, Y: 3}, {X: 1, Y: 4}, {X: 2, Y: 4}, {X: 3, Y: 4}, {X: 4, Y: 4}, {X: 5, Y: 4}, {X: 1, Y: 5}, {X: 2, Y: 5}, {X: 3, Y: 5}, {X: 4, Y: 5}, {X: 5, Y: 5}, {X: 3, Y: 6}}},
	}
	for _, tc := range tests {
		verificarCellsIguais(t, RoomShapeOffsets(RoomShapeCircle, tc.diameter, tc.diameter), tc.want)
	}
}

func TestRoomShapeTemOrdemCanonica(t *testing.T) {
	assert.Equal(t, RoomShape(0), RoomShapeRectangle)
	assert.Equal(t, RoomShape(1), RoomShapeL)
	assert.Equal(t, RoomShape(2), RoomShapeT)
	assert.Equal(t, RoomShape(3), RoomShapeCross)
	assert.Equal(t, RoomShape(4), RoomShapeCircle)
}

func TestMascaraValidaEConexa(t *testing.T) {
	shapes := []RoomShape{RoomShapeRectangle, RoomShapeL, RoomShapeT, RoomShapeCross, RoomShapeCircle}
	for _, shape := range shapes {
		for width := uint32(1); width <= 9; width++ {
			for height := uint32(1); height <= 9; height++ {
				if !ValidRoomShapeDimensions(shape, width, height) {
					continue
				}
				verificarMascaraSemDuplicatasEConexa(t, shape, width, height)
			}
		}
	}
}

func TestMascaraOrdenadaEPrimeiroOffset(t *testing.T) {
	tests := []struct {
		shape         RoomShape
		width, height uint32
		want          Cell
	}{
		{RoomShapeRectangle, 3, 2, Cell{}},
		{RoomShapeL, 3, 3, Cell{}},
		{RoomShapeT, 5, 3, Cell{}},
		{RoomShapeCross, 5, 5, Cell{X: 2}},
		{RoomShapeCircle, 5, 5, Cell{X: 2}},
	}
	for _, tc := range tests {
		got := RoomShapeOffsets(tc.shape, tc.width, tc.height)
		verificarOrdemCanonica(t, got)
		assert.Equal(t, tc.want, RoomShapeFirstOffset(tc.shape, tc.width, tc.height),
			"primeiro offset de %v", tc.shape)
	}
}

func TestDimensoesDeMascaraInvalidas(t *testing.T) {
	invalid := []struct {
		shape         RoomShape
		width, height uint32
	}{
		{RoomShapeL, 1, 2}, {RoomShapeL, 2, 1},
		{RoomShapeT, 2, 2}, {RoomShapeT, 3, 1},
		{RoomShapeCross, 2, 3}, {RoomShapeCross, 3, 2},
		{RoomShapeCircle, 4, 5}, {RoomShapeCircle, 5, 4}, {RoomShapeCircle, 3, 3}, {RoomShapeCircle, 4, 4}, {RoomShapeCircle, 6, 6},
		{RoomShapeRectangle, 0, 1}, {RoomShapeRectangle, 1, 0}, {RoomShape(99), 3, 3},
	}
	for _, tc := range invalid {
		assert.False(t, ValidRoomShapeDimensions(tc.shape, tc.width, tc.height),
			"dimensões deveriam ser inválidas: shape=%v width=%d height=%d", tc.shape, tc.width, tc.height)
		assert.Nil(t, RoomShapeOffsets(tc.shape, tc.width, tc.height))
	}
}

func verificarCellsIguais(t *testing.T, got, want []Cell) {
	t.Helper()
	require.Len(t, got, len(want), "quantidade de Cells")
	assert.Equal(t, want, got)
}

func verificarMascaraSemDuplicatasEConexa(t *testing.T, shape RoomShape, width, height uint32) {
	t.Helper()
	got := RoomShapeOffsets(shape, width, height)
	seen := make(map[Cell]bool, len(got))
	for _, cell := range got {
		assert.False(t, seen[cell], "máscara %v %dx%d contém duplicata em %+v", shape, width, height, cell)
		seen[cell] = true
	}
	require.NotEmpty(t, got, "máscara válida %v %dx%d vazia", shape, width, height)

	visited := map[Cell]bool{got[0]: true}
	queue := []Cell{got[0]}
	directions := []Cell{{X: 0, Y: -1}, {X: 1, Y: 0}, {X: 0, Y: 1}, {X: -1, Y: 0}}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, direction := range directions {
			neighbor := Cell{X: current.X + direction.X, Y: current.Y + direction.Y}
			if seen[neighbor] && !visited[neighbor] {
				visited[neighbor] = true
				queue = append(queue, neighbor)
			}
		}
	}
	assert.Len(t, visited, len(got), "máscara %v %dx%d não é 4-conexa", shape, width, height)
}

func verificarOrdemCanonica(t *testing.T, cells []Cell) {
	t.Helper()
	for i := 1; i < len(cells); i++ {
		previous, current := cells[i-1], cells[i]
		assert.False(t, current.Y < previous.Y || current.Y == previous.Y && current.X <= previous.X,
			"ordem não canônica entre %+v e %+v", previous, current)
	}
}

// TestDirectionDeltaEspelhaVetoresCardinais fixa o mapeamento normativo da
// especificação: North=(0,-1), East=(1,0), South=(0,1), West=(-1,0).
func TestDirectionDeltaEspelhaVetoresCardinais(t *testing.T) {
	cases := []struct {
		direction Direction
		want      Cell
	}{
		{DirectionNorth, Cell{X: 0, Y: -1}},
		{DirectionEast, Cell{X: 1, Y: 0}},
		{DirectionSouth, Cell{X: 0, Y: 1}},
		{DirectionWest, Cell{X: -1, Y: 0}},
	}
	for _, tc := range cases {
		if got := tc.direction.Delta(); got != tc.want {
			t.Errorf("Delta de %d: obteve %+v, esperava %+v", tc.direction, got, tc.want)
		}
	}
}

// TestDirectionOrdemCanonica fixa a ordem canônica North, East, South, West
// como valores crescentes a partir de zero.
func TestDirectionOrdemCanonica(t *testing.T) {
	if DirectionNorth != 0 || DirectionEast != 1 || DirectionSouth != 2 || DirectionWest != 3 {
		t.Errorf("ordem canônica violada: North=%d East=%d South=%d West=%d",
			DirectionNorth, DirectionEast, DirectionSouth, DirectionWest)
	}
}

// TestDirectionOpposite fixa os pares opostos North/South e East/West usados
// na derivação de Doors de extremidade.
func TestDirectionOpposite(t *testing.T) {
	cases := []struct {
		direction Direction
		want      Direction
	}{
		{DirectionNorth, DirectionSouth},
		{DirectionSouth, DirectionNorth},
		{DirectionEast, DirectionWest},
		{DirectionWest, DirectionEast},
	}
	for _, tc := range cases {
		if got := tc.direction.Opposite(); got != tc.want {
			t.Errorf("Opposite de %d: obteve %d, esperava %d", tc.direction, got, tc.want)
		}
		if got := tc.direction.Opposite().Opposite(); got != tc.direction {
			t.Errorf("Opposite duplo de %d: obteve %d, esperava identidade", tc.direction, got)
		}
	}
}
