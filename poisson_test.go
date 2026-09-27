package daedalus

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoissonGridUmPorUmProduzRetanguloUnitario(t *testing.T) {
	req := mustPlacementRequest(t, Config{Width: 1, Height: 1, Seed: 7})

	placements, err := (poissonDiskRoomsPlacer{}).Place(req)

	require.NoError(t, err)
	require.Len(t, placements, 1)
	assert.Equal(t, RoomPlacement{
		Shape:  RoomShapeRectangle,
		Origin: Cell{X: 0, Y: 0},
		Width:  1,
		Height: 1,
		Cells:  []Cell{{X: 0, Y: 0}},
	}, placements[0])
}

func TestPoissonCanceladoNaoDevolveResultadoParcial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := mustPlacementRequest(t, Config{Width: 32, Height: 32, Seed: 11})
	req.Context = ctx

	placements, err := (poissonDiskRoomsPlacer{}).Place(req)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, placements)
}

func TestPoissonRespeitaTetoEInvariantesDosPlacements(t *testing.T) {
	const maximumRooms = uint32(20)
	geometry := unitRectangleGeometry(1)
	req := mustPlacementRequest(t, Config{
		Width: 32, Height: 32, Seed: 12345,
		MinDistance: 3, MaxAttempts: 30, MaxRooms: maximumRooms,
		RoomGeometry: &geometry,
	})

	placements, err := (poissonDiskRoomsPlacer{}).Place(req)

	require.NoError(t, err)
	require.Len(t, placements, int(maximumRooms))
	assertPlacementInvariants(t, req, placements)
}

func TestPoissonDistanciaAcimaDaDiagonalTerminaComPrimeiraRoom(t *testing.T) {
	geometry := unitRectangleGeometry(0)
	req := mustPlacementRequest(t, Config{
		Width: 8, Height: 8, Seed: 99,
		MinDistance: 100, MaxAttempts: 8, MaxRooms: 64,
		RoomGeometry: &geometry,
	})

	placements, err := (poissonDiskRoomsPlacer{}).Place(req)

	require.NoError(t, err)
	require.Len(t, placements, 1)
	assert.Equal(t, Cell{X: 3, Y: 3}, placements[0].Origin,
		"em empate no Grid par, a ordem Y/X escolhe a primeira Cell")
}

func TestPoissonRegioesUsamMaiorDistanciaLocal(t *testing.T) {
	geometry := unitRectangleGeometry(0)
	req := mustPlacementRequest(t, Config{
		Width: 40, Height: 24, Seed: 2026,
		MinDistance: 2, MaxAttempts: 30, MaxRooms: 80,
		DensityRegions: []DensityRegion{{
			Min: Cell{X: 20, Y: 0}, Max: Cell{X: 40, Y: 24}, MinDistance: 6,
		}},
		RoomGeometry: &geometry,
	})

	placements, err := (poissonDiskRoomsPlacer{}).Place(req)

	require.NoError(t, err)
	require.Greater(t, len(placements), 1)
	assertPlacementInvariants(t, req, placements)
}

func TestPoissonRegioesVaziasPreservamSequencia(t *testing.T) {
	geometry := unitRectangleGeometry(0)
	config := Config{
		Width: 24, Height: 24, Seed: 314159,
		MinDistance: 3, MaxAttempts: 12, MaxRooms: 30,
		RoomGeometry: &geometry,
	}
	withoutRegions := mustPlacementRequest(t, config)
	config.DensityRegions = []DensityRegion{}
	withEmptyRegions := mustPlacementRequest(t, config)

	first, firstErr := (poissonDiskRoomsPlacer{}).Place(withoutRegions)
	second, secondErr := (poissonDiskRoomsPlacer{}).Place(withEmptyRegions)

	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	assert.Equal(t, first, second)
}

func TestPoissonMesmaConfiguracaoReproduzGeometriaCampoACampo(t *testing.T) {
	req := mustPlacementRequest(t, Config{
		Width: 48, Height: 48, Seed: 424242,
		MinDistance: 6, MaxAttempts: 30, MaxRooms: 32,
	})

	first, firstErr := (poissonDiskRoomsPlacer{}).Place(req)
	second, secondErr := (poissonDiskRoomsPlacer{}).Place(req)

	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	require.NotEmpty(t, first)
	assert.Equal(t, first, second)
	assert.Greater(t, len(first[0].Cells), 1, "o perfil padrão produz footprint dinâmico")
	assertPlacementInvariants(t, req, first)
}

func TestPoissonMaxAttemptsAlteraRemocaoDoPontoAtivo(t *testing.T) {
	geometry := unitRectangleGeometry(0)
	foundDifference := false
	for seed := Seed(0); seed < 256 && !foundDifference; seed++ {
		baseConfig := Config{
			Width: 12, Height: 12, Seed: seed,
			MinDistance: 3, MaxAttempts: 1, MaxRooms: 20,
			RoomGeometry: &geometry,
		}
		oneAttempt, oneErr := (poissonDiskRoomsPlacer{}).Place(mustPlacementRequest(t, baseConfig))
		require.NoError(t, oneErr)
		baseConfig.MaxAttempts = 2
		twoAttempts, twoErr := (poissonDiskRoomsPlacer{}).Place(mustPlacementRequest(t, baseConfig))
		require.NoError(t, twoErr)
		foundDifference = !assert.ObjectsAreEqual(oneAttempt, twoAttempts)
	}
	assert.True(t, foundDifference,
		"o limite por ponto ativo precisa encerrar a iteração após a quantidade configurada")
}

func TestPoissonChamadasConcorrentesSaoIdenticas(t *testing.T) {
	geometry := unitRectangleGeometry(1)
	req := mustPlacementRequest(t, Config{
		Width: 32, Height: 32, Seed: 271828,
		MinDistance: 3, MaxAttempts: 20, MaxRooms: 40,
		RoomGeometry: &geometry,
	})
	baseline, err := (poissonDiskRoomsPlacer{}).Place(req)
	require.NoError(t, err)

	const workers = 2
	results := make([][]RoomPlacement, workers)
	errors := make([]error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for worker := range workers {
		go func() {
			defer group.Done()
			results[worker], errors[worker] = (poissonDiskRoomsPlacer{}).Place(req)
		}()
	}
	group.Wait()

	for worker := range workers {
		require.NoError(t, errors[worker])
		assert.Equal(t, baseline, results[worker])
	}
}

func TestPoissonCellsAceitasNaoCompartilhamScratchNemResultados(t *testing.T) {
	geometry := unitRectangleGeometry(0)
	req := mustPlacementRequest(t, Config{
		Width: 16, Height: 16, Seed: 17,
		MinDistance: 2, MaxAttempts: 30, MaxRooms: 4,
		RoomGeometry: &geometry,
	})
	first, firstErr := (poissonDiskRoomsPlacer{}).Place(req)
	second, secondErr := (poissonDiskRoomsPlacer{}).Place(req)
	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	require.GreaterOrEqual(t, len(first), 2)
	require.Equal(t, first, second)

	secondPlacementCell := first[1].Cells[0]
	otherResultCell := second[0].Cells[0]
	first[0].Cells[0] = Cell{X: -1, Y: -1}

	assert.Equal(t, secondPlacementCell, first[1].Cells[0], "placements aceitos não compartilham o scratch")
	assert.Equal(t, otherResultCell, second[0].Cells[0], "chamadas distintas não compartilham Cells")
}

func TestGradeAceleracaoReutilizaBufferDeVizinhos(t *testing.T) {
	grid := &anchorAccelerationGrid{
		side: 1, columns: 1, rows: 1,
		buckets: [][]int{{0}},
	}
	accepted := []acceptedPlacement{{anchor: Cell{X: 1, Y: 1}}}
	buffer := make([]acceptedPlacement, 0, 1)
	var nearby []acceptedPlacement

	allocations := testing.AllocsPerRun(100, func() {
		nearby = grid.nearbyInto(Cell{}, accepted, buffer[:0])
	})

	assert.Zero(t, allocations, "a consulta quente precisa reutilizar o buffer da solicitação")
	require.Len(t, nearby, 1)
	assert.Equal(t, accepted[0], nearby[0])
}

func TestAmostragemDoAnelRepeteRejeicaoInternamente(t *testing.T) {
	const radius = 4.0
	stream := newSplitMix64(123)
	reference := newSplitMix64(123)

	offsetX, offsetY := sampleUniformAnnulusByRejection(&stream, radius)
	wantX, wantY := sampleUniformAnnulusReference(&reference, radius)

	assert.Equal(t, wantX, offsetX, "a transformação congelada é uniform01*4r-2r")
	assert.Equal(t, wantY, offsetY, "a transformação congelada é uniform01*4r-2r")
	assert.Equal(t, reference.state, stream.state, "pares rejeitados são repetidos dentro do amostrador")
	xSquared := offsetX * offsetX
	ySquared := offsetY * offsetY
	distanceSquared := xSquared + ySquared
	radiusSquared := radius * radius
	outerRadius := radius * 2
	outerSquared := outerRadius * outerRadius
	assert.GreaterOrEqual(t, distanceSquared, radiusSquared, "o limite interno é inclusivo")
	assert.Less(t, distanceSquared, outerSquared, "o limite externo é exclusivo")
}

func sampleUniformAnnulusReference(stream *splitMix64, radius float64) (float64, float64) {
	for {
		offsetX := stream.uniform01() * (4 * radius)
		offsetX -= 2 * radius
		offsetY := stream.uniform01() * (4 * radius)
		offsetY -= 2 * radius
		distanceSquared := offsetX*offsetX + offsetY*offsetY
		if distanceSquared >= radius*radius && distanceSquared < 4*radius*radius {
			return offsetX, offsetY
		}
	}
}

func mustPlacementRequest(t testing.TB, config Config) PlacementRequest {
	t.Helper()
	effective, err := normalizeConfig(config)
	require.NoError(t, err)
	return PlacementRequest{
		Context:              context.Background(),
		Width:                effective.width,
		Height:               effective.height,
		MinDistance:          effective.minDistance,
		DensityRegions:       effective.densityRegions,
		RoomGeometry:         effective.roomGeometry,
		MaxAttempts:          effective.maxAttempts,
		MaxRooms:             effective.maxRooms,
		Seed:                 effective.seed,
		geometryCombinations: effective.geometryCombinations,
	}
}

func unitRectangleGeometry(minRoomGap uint32) RoomGeometry {
	return RoomGeometry{
		MinWidth: 1, MaxWidth: 1,
		MinHeight: 1, MaxHeight: 1,
		MaxFootprintCells: 1,
		MinRoomGap:        minRoomGap,
		Shapes:            []RoomShapeWeight{{Shape: RoomShapeRectangle, Weight: 1}},
	}
}

func assertPlacementInvariants(t testing.TB, req PlacementRequest, placements []RoomPlacement) {
	t.Helper()
	accepted := make([]acceptedPlacement, 0, len(placements))
	occupancy := newPlacementOccupancy(req.Width, req.Height)
	for index, placement := range placements {
		err := validatePlacementForAcceptance(
			placement,
			req.Width,
			req.Height,
			req.RoomGeometry.MaxFootprintCells,
			req.RoomGeometry.MinRoomGap,
			req.MinDistance,
			req.DensityRegions,
			accepted,
			occupancy,
		)
		assert.NoError(t, err, "placement %d", index)
		footprint, ok := absoluteFootprint(placement)
		require.True(t, ok)
		accepted = append(accepted, acceptedPlacement{anchor: footprint[0], footprint: footprint})
		occupancy.mark(uint32(index), footprint)
	}
}
