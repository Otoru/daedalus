package daedalus

import "testing"

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
