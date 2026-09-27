package daedalus

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDerivacaoDaAncoraReconstroiMascarasCanonicas(t *testing.T) {
	tests := []struct {
		name   string
		shape  RoomShape
		width  uint32
		height uint32
	}{
		{"retangulo", RoomShapeRectangle, 4, 3},
		{"L", RoomShapeL, 4, 3},
		{"T", RoomShapeT, 5, 4},
		{"cruz", RoomShapeCross, 5, 5},
		{"circulo", RoomShapeCircle, 5, 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			at := Cell{X: 10, Y: 12}
			placement := buildPlacementFromAt(at, tc.shape, tc.width, tc.height)
			footprint, ok := absoluteFootprint(placement)
			require.True(t, ok)

			firstOffset := RoomShapeFirstOffset(tc.shape, tc.width, tc.height)
			assert.Equal(t, Cell{X: at.X - firstOffset.X, Y: at.Y - firstOffset.Y}, placement.Origin)
			assert.Equal(t, RoomShapeOffsets(tc.shape, tc.width, tc.height), placement.Cells)
			require.NotEmpty(t, footprint)
			assert.Equal(t, at, footprint[0])
			if tc.shape == RoomShapeCross || tc.shape == RoomShapeCircle {
				assert.NotContains(t, footprint, placement.Origin)
			}
		})
	}
}

func TestFootprintAbsolutoReutilizaBufferDaSolicitacao(t *testing.T) {
	placement := buildPlacementFromAt(Cell{X: 10, Y: 12}, RoomShapeRectangle, 4, 3)
	buffer := make([]Cell, 0, len(placement.Cells))
	var footprint []Cell
	var ok bool

	allocations := testing.AllocsPerRun(100, func() {
		footprint, ok = absoluteFootprintInto(placement, buffer[:0])
	})

	require.True(t, ok)
	assert.Zero(t, allocations, "a materialização quente precisa reutilizar o buffer da solicitação")
	assert.Equal(t, Cell{X: 10, Y: 12}, footprint[0])
}

func TestAncoraNaBordaPodeGerarOrigemNegativa(t *testing.T) {
	placement := buildPlacementFromAt(Cell{X: 0, Y: 0}, RoomShapeCross, 5, 5)

	assert.Equal(t, Cell{X: -2, Y: 0}, placement.Origin)
	assert.False(t, placementWithinBounds(placement, 8, 8))
	assert.ErrorIs(t, validatePlacementForAcceptance(placement, 8, 8, 25, 0, 1, nil, nil, nil), errPlacementOutOfBounds)
}

func TestBoundsAceitaBoundingBoxNoLimiteExatoDoGrid(t *testing.T) {
	placement := buildPlacementFromAt(Cell{X: 2, Y: 2}, RoomShapeRectangle, 3, 3)

	assert.True(t, placementWithinBounds(placement, 5, 5))
	assert.NoError(t, validatePlacementForAcceptance(placement, 5, 5, 9, 0, 1, nil, nil, nil))
}

func TestGapZeroAceitaFootprintsEncostadosERejeitaSobreposicao(t *testing.T) {
	accepted := buildPlacementFromAt(Cell{X: 0, Y: 0}, RoomShapeRectangle, 2, 2)
	touching := buildPlacementFromAt(Cell{X: 2, Y: 0}, RoomShapeRectangle, 2, 2)
	overlapping := buildPlacementFromAt(Cell{X: 1, Y: 1}, RoomShapeRectangle, 2, 2)
	acceptedPlacements, occupancy := mustAcceptedState(t, 8, 8, accepted)

	assert.NoError(t, validatePlacementForAcceptance(touching, 8, 8, 4, 0, 1, nil, acceptedPlacements, occupancy))
	assert.ErrorIs(t, validatePlacementForAcceptance(overlapping, 8, 8, 4, 0, 1, nil, acceptedPlacements, occupancy), errPlacementOverlap)
}

func TestGapUmRejeitaContatoDiagonal(t *testing.T) {
	accepted := buildPlacementFromAt(Cell{X: 0, Y: 0}, RoomShapeRectangle, 1, 1)
	diagonal := buildPlacementFromAt(Cell{X: 1, Y: 1}, RoomShapeRectangle, 1, 1)
	acceptedPlacements, occupancy := mustAcceptedState(t, 4, 4, accepted)

	assert.False(t, footprintsRespectGap(mustFootprint(t, diagonal), mustFootprint(t, accepted), 1))
	assert.ErrorIs(t, validatePlacementForAcceptance(diagonal, 4, 4, 1, 1, 1, nil, acceptedPlacements, occupancy), errPlacementGap)
}

func TestDistanciaEntreAncorasUsaMaiorDistanciaLocal(t *testing.T) {
	regions := []DensityRegion{
		{Min: Cell{X: 0, Y: 0}, Max: Cell{X: 3, Y: 3}, MinDistance: 2},
		{Min: Cell{X: 4, Y: 0}, Max: Cell{X: 8, Y: 3}, MinDistance: 5},
	}
	a := Cell{X: 1, Y: 1}
	b := Cell{X: 5, Y: 1}

	assert.Equal(t, 2.0, localMinDistance(a, 3, regions))
	assert.Equal(t, 5.0, localMinDistance(b, 3, regions))
	assert.False(t, anchorsRespectDistance(a, b, 3, regions))
	assert.True(t, anchorsRespectDistance(a, Cell{X: 6, Y: 1}, 3, regions))
	// Max é exclusivo: esta Cell fica fora da primeira região.
	assert.Equal(t, 3.0, localMinDistance(Cell{X: 3, Y: 2}, 3, regions))
}

func TestMascaraValidaRejeitaOffsetsAdulterados(t *testing.T) {
	canonical := buildPlacementFromAt(Cell{X: 3, Y: 3}, RoomShapeRectangle, 2, 2)
	tests := []struct {
		name  string
		cells []Cell
	}{
		{"duplicados", []Cell{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}}},
		{"desconexos", []Cell{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 0, Y: 1}, {X: 3, Y: 3}}},
		{"ordem diferente", []Cell{{X: 1, Y: 0}, {X: 0, Y: 0}, {X: 0, Y: 1}, {X: 1, Y: 1}}},
		{"forma diferente", RoomShapeOffsets(RoomShapeL, 2, 2)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			placement := canonical
			placement.Cells = tc.cells
			assert.False(t, placementHasCanonicalMask(placement))
		})
	}
}

func TestValidacaoDePlacementParaNaPrimeiraRegraViolada(t *testing.T) {
	placement := RoomPlacement{
		Shape:  RoomShapeRectangle,
		Origin: Cell{X: -1, Y: 0},
		Width:  2,
		Height: 2,
		Cells:  []Cell{{X: 0, Y: 0}},
	}

	err := validatePlacementForAcceptance(placement, 1, 1, 1, 0, 1, nil, nil, nil)
	assert.ErrorIs(t, err, errPlacementInvalidMask)
}

func TestAreaDoPlacementRespeitaLimiteEfetivo(t *testing.T) {
	placement := buildPlacementFromAt(Cell{X: 0, Y: 0}, RoomShapeRectangle, 2, 2)

	assert.False(t, placementWithinArea(placement, 3))
	assert.ErrorIs(t, validatePlacementForAcceptance(placement, 4, 4, 3, 0, 1, nil, nil, nil), errPlacementAreaExceeded)
}

func TestSobreposicaoDetectaCellCompartilhada(t *testing.T) {
	a := []Cell{{X: 1, Y: 1}, {X: 2, Y: 1}}
	b := []Cell{{X: 2, Y: 1}, {X: 3, Y: 1}}
	c := []Cell{{X: 3, Y: 1}}

	assert.True(t, footprintsOverlap(a, b))
	assert.False(t, footprintsOverlap(a, c))
}

func TestVizinhancaNaGradeEquivaleAComparacaoParAPar(t *testing.T) {
	const (
		gridSize       = uint32(32)
		casesPerGap    = 500
		maximumGap     = uint32(3)
		maximumCells   = 24
		randomSeed     = int64(0xDAEDA1)
		acceptedOwner  = uint32(0)
		candidateOwner = uint32(1)
	)
	random := rand.New(rand.NewSource(randomSeed))

	for minRoomGap := uint32(0); minRoomGap <= maximumGap; minRoomGap++ {
		for testCase := 0; testCase < casesPerGap; testCase++ {
			acceptedFootprint := randomFootprint(random, gridSize, maximumCells)
			candidateFootprint := randomFootprint(random, gridSize, maximumCells)
			occupancy := newPlacementOccupancy(gridSize, gridSize)
			occupancy.mark(acceptedOwner, acceptedFootprint)

			expected := footprintsRespectGap(candidateFootprint, acceptedFootprint, minRoomGap)
			actual := occupancy.footprintRespectsGap(candidateFootprint, minRoomGap, candidateOwner)
			assert.Equal(t, expected, actual, "gap=%d caso=%d", minRoomGap, testCase)
		}
	}
}

func TestVizinhancaNaGradeIgnoraCellsDaMesmaRoom(t *testing.T) {
	const owner = uint32(7)
	footprint := []Cell{{X: 4, Y: 4}, {X: 5, Y: 4}}
	occupancy := newPlacementOccupancy(8, 8)
	occupancy.mark(owner, footprint)

	assert.True(t, occupancy.footprintRespectsGap(footprint, 3, owner))
}

func randomFootprint(random *rand.Rand, gridSize uint32, maximumCells int) []Cell {
	cellCount := random.Intn(maximumCells) + 1
	seen := make(map[Cell]struct{}, cellCount)
	footprint := make([]Cell, 0, cellCount)
	for len(footprint) < cellCount {
		cell := Cell{X: int32(random.Intn(int(gridSize))), Y: int32(random.Intn(int(gridSize)))}
		if _, exists := seen[cell]; exists {
			continue
		}
		seen[cell] = struct{}{}
		footprint = append(footprint, cell)
	}
	return footprint
}

func mustFootprint(t *testing.T, placement RoomPlacement) []Cell {
	t.Helper()
	footprint, ok := absoluteFootprint(placement)
	require.True(t, ok)
	return footprint
}

func mustAcceptedState(t testing.TB, gridWidth, gridHeight uint32, placements ...RoomPlacement) ([]acceptedPlacement, *placementOccupancy) {
	t.Helper()
	accepted := make([]acceptedPlacement, 0, len(placements))
	occupancy := newPlacementOccupancy(gridWidth, gridHeight)
	for index, placement := range placements {
		footprint, ok := absoluteFootprint(placement)
		require.True(t, ok)
		require.NotEmpty(t, footprint)
		accepted = append(accepted, acceptedPlacement{anchor: footprint[0], footprint: footprint})
		occupancy.mark(uint32(index), footprint)
	}
	return accepted, occupancy
}

func BenchmarkAceitacao256(b *testing.B) {
	const (
		gridSize     = uint32(256)
		roomSize     = uint32(9)
		acceptedSide = 16
	)
	placements := make([]RoomPlacement, 0, acceptedSide*acceptedSide)
	for y := 0; y < acceptedSide; y++ {
		for x := 0; x < acceptedSide; x++ {
			placements = append(placements, buildPlacementFromAt(
				Cell{X: int32(x) * int32(roomSize), Y: int32(y) * int32(roomSize)},
				RoomShapeRectangle,
				roomSize,
				roomSize,
			))
		}
	}
	accepted, occupancy := mustAcceptedState(b, gridSize, gridSize, placements...)
	candidate := buildPlacementFromAt(Cell{X: 240, Y: 240}, RoomShapeRectangle, roomSize, roomSize)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := validatePlacementForAcceptance(
			candidate,
			gridSize,
			gridSize,
			roomSize*roomSize,
			1,
			0,
			nil,
			accepted,
			occupancy,
		); err != nil {
			b.Fatal(err)
		}
	}
}
