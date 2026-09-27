package daedalus

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAC01LayoutBemSucedidoRespeitaTodasAsInvariantes(t *testing.T) {
	config := Config{Width: 32, Height: 32, Seed: 101, MaxRooms: 24}
	effective, err := normalizeConfig(config)
	require.NoError(t, err)
	layout, err := (Generator{}).Generate(config)
	require.NoError(t, err)

	assertLayoutInvariants(t, effective, layout)
}

func TestPropriedadeLayoutsOuErrosDaEspecificacao(t *testing.T) {
	const propertySeed int64 = 0x5eedf10
	const caseCount = 48
	random := rand.New(rand.NewSource(propertySeed))
	generator := Generator{}

	for caseIndex := 0; caseIndex < caseCount; caseIndex++ {
		config := randomPropertyConfig(random, caseIndex)
		name := fmt.Sprintf("caso_%02d_seed_%d", caseIndex, propertySeed)
		t.Run(name, func(t *testing.T) {
			effective, normalizationErr := normalizeConfig(config)
			layout, err := generator.GenerateContext(context.Background(), config)
			if err != nil {
				assert.Equal(t, Layout{}, layout, "seed da propriedade=%d", propertySeed)
				assert.True(t, isSpecifiedGenerationError(err), "seed da propriedade=%d: erro fora da spec: %v", propertySeed, err)
				if normalizationErr == nil {
					assert.NotErrorIs(t, err, ErrInvalidConfig, "seed da propriedade=%d", propertySeed)
					assert.NotErrorIs(t, err, ErrLimitExceeded, "seed da propriedade=%d", propertySeed)
				}
				return
			}
			require.NoError(t, normalizationErr, "seed da propriedade=%d", propertySeed)
			assertLayoutInvariants(t, effective, layout)
		})
	}
}

func TestAC04MesmaConfigESeedProduzemLayoutInteiroIgual(t *testing.T) {
	config := Config{
		Width: 48, Height: 40, Seed: 404, MaxRooms: 28, ExtraEdgeCount: 3,
		RoomRoleRequests: []RoomRoleRequest{
			{Role: RoomRoleStart, Count: 1},
			{Role: RoomRoleBoss, Count: 1},
			{Role: RoomRoleTreasure, Count: 2},
		},
	}
	baseline, err := (Generator{}).Generate(config)
	require.NoError(t, err)

	for repetition := 0; repetition < 4; repetition++ {
		actual, generationErr := (Generator{}).Generate(config)
		require.NoError(t, generationErr)
		assert.Equal(t, baseline, actual, "repetição %d divergiu campo a campo", repetition)
	}
}

func TestAC05SolicitacoesConcorrentesSaoIsoladas(t *testing.T) {
	generator := Generator{}
	config := Config{Width: 40, Height: 40, Seed: 505, MaxRooms: 24, ExtraEdgeCount: 2}
	baseline, err := generator.Generate(config)
	require.NoError(t, err)

	const requestCount = 12
	results := make([]Layout, requestCount)
	errs := make([]error, requestCount)
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(requestCount)
	for requestIndex := range results {
		go func(index int) {
			defer wait.Done()
			<-start
			results[index], errs[index] = generator.Generate(config)
		}(requestIndex)
	}
	close(start)
	wait.Wait()

	for requestIndex := range results {
		require.NoError(t, errs[requestIndex])
		assert.Equal(t, baseline, results[requestIndex], "solicitação concorrente %d", requestIndex)
	}

	mutated := results[0]
	mutated.Rooms[0].Cells[0] = Cell{X: -1, Y: -1}
	mutated.Grid.Cells[0].Kind = CellKindCorridor
	if len(mutated.Corridors) > 0 && len(mutated.Corridors[0].Cells) > 0 {
		mutated.Corridors[0].Cells[0] = Cell{X: -2, Y: -2}
	}
	if len(mutated.Doors) > 0 {
		mutated.Doors[0].CorridorIDs[0] = CorridorID(999)
	}
	fresh, err := generator.Generate(config)
	require.NoError(t, err)
	assert.Equal(t, baseline, fresh, "mutar um resultado não pode contaminar outra solicitação")

	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	var canceledLayout Layout
	var canceledErr error
	var independentLayout Layout
	var independentErr error
	wait.Add(2)
	go func() {
		defer wait.Done()
		canceledLayout, canceledErr = generator.GenerateContext(canceledContext, config)
	}()
	go func() {
		defer wait.Done()
		independentLayout, independentErr = generator.Generate(config)
	}()
	wait.Wait()
	assert.ErrorIs(t, canceledErr, context.Canceled)
	assert.Equal(t, Layout{}, canceledLayout)
	require.NoError(t, independentErr)
	assert.Equal(t, baseline, independentLayout)
}

func TestAC07GridUmPorUmTemUmaRoomSemCorredorOuDoor(t *testing.T) {
	config := Config{Width: 1, Height: 1, Seed: 7, MinDistance: 6}
	effective, err := normalizeConfig(config)
	require.NoError(t, err)
	layout, err := (Generator{}).Generate(config)
	require.NoError(t, err)

	require.Len(t, layout.Rooms, 1)
	assert.Equal(t, Room{
		ID: 0, At: Cell{X: 0, Y: 0}, Shape: RoomShapeRectangle,
		Origin: Cell{X: 0, Y: 0}, Width: 1, Height: 1,
		Cells: []Cell{{X: 0, Y: 0}},
	}, layout.Rooms[0])
	assert.Empty(t, layout.Corridors)
	assert.Empty(t, layout.Doors)
	assertLayoutInvariants(t, effective, layout)
}

func TestAC08GridPequenoEDistanciaAltaTerminamSemTruncar(t *testing.T) {
	first := fixedGeometryConfigForTest(2, 2, 10)
	first.Seed = 80
	first.MinDistance = 6
	second := fixedGeometryConfigForTest(7, 5, 20)
	second.Seed = 81
	second.MinDistance = 100
	cases := []struct {
		config Config
		wantAt Cell
	}{
		{config: first, wantAt: Cell{X: 0, Y: 0}},
		{config: second, wantAt: Cell{X: 3, Y: 2}},
	}
	for caseIndex, testCase := range cases {
		effective, err := normalizeConfig(testCase.config)
		require.NoError(t, err)
		layout, err := (Generator{}).Generate(testCase.config)
		require.NoError(t, err, "caso %d", caseIndex)
		require.Len(t, layout.Rooms, 1, "caso %d", caseIndex)
		assert.Equal(t, testCase.wantAt, layout.Rooms[0].At, "primeira Room mais próxima do centro no caso %d", caseIndex)
		assertLayoutInvariants(t, effective, layout)
	}
}

func TestAC09aRegioesDeDensidadeUsamMaiorDistanciaLocal(t *testing.T) {
	config := Config{
		Width: 48, Height: 48, Seed: 9091, MinDistance: 3, MaxRooms: 36,
		DensityRegions: []DensityRegion{
			{Min: Cell{X: 0, Y: 0}, Max: Cell{X: 24, Y: 48}, MinDistance: 8},
			{Min: Cell{X: 24, Y: 0}, Max: Cell{X: 48, Y: 24}, MinDistance: 5},
		},
	}
	effective, err := normalizeConfig(config)
	require.NoError(t, err)
	layout, err := (Generator{}).Generate(config)
	require.NoError(t, err)

	for first := 0; first < len(layout.Rooms); first++ {
		for second := first + 1; second < len(layout.Rooms); second++ {
			firstDistance := localMinDistance(layout.Rooms[first].At, effective.minDistance, effective.densityRegions)
			secondDistance := localMinDistance(layout.Rooms[second].At, effective.minDistance, effective.densityRegions)
			requiredDistance := max(firstDistance, secondDistance)
			distanceSquared := squaredCellDistance(layout.Rooms[first].At, layout.Rooms[second].At)
			assert.GreaterOrEqual(t, float64(distanceSquared), requiredDistance*requiredDistance,
				"par de âncoras %d/%d", first, second)
		}
	}
	assertLayoutInvariants(t, effective, layout)
}

func TestAC09bRegioesVaziasNaoAlteramLayoutNemConsomemDraws(t *testing.T) {
	base := Config{Width: 40, Height: 32, Seed: 9092, MaxRooms: 24}
	explicitlyEmpty := base
	explicitlyEmpty.DensityRegions = []DensityRegion{}

	baseline, err := (Generator{}).Generate(base)
	require.NoError(t, err)
	actual, err := (Generator{}).Generate(explicitlyEmpty)
	require.NoError(t, err)
	assert.Equal(t, baseline, actual)
}

func TestAC10CatalogoCompativelPreservaIDsTagsEDirecoes(t *testing.T) {
	config := Config{Width: 32, Height: 32, Seed: 1010, MaxRooms: 16}
	config.PlantCatalog = &PlantCatalog{
		Rooms:     []RoomPlant{{ID: "sala", Tags: []string{"pedra"}, Weight: 1, DoorDirections: allDirectionsForTest()}},
		Corridors: []CorridorPlant{{ID: "corredor", Tags: []string{"úmido"}, Weight: 1}},
	}
	layout, err := (Generator{}).Generate(config)
	require.NoError(t, err)
	for roomIndex, room := range layout.Rooms {
		assert.Equal(t, PlantID("sala"), room.PlantID, "Room %d", roomIndex)
		assert.Equal(t, []string{"pedra"}, room.Tags, "Room %d", roomIndex)
	}
	for corridorIndex, corridor := range layout.Corridors {
		assert.Equal(t, PlantID("corredor"), corridor.PlantID, "Corridor %d", corridorIndex)
		assert.Equal(t, []string{"úmido"}, corridor.Tags, "Corridor %d", corridorIndex)
	}
}

func TestAC11CatalogoIncompativelFalhaSemLayoutParcial(t *testing.T) {
	config := Config{Width: 32, Height: 32, Seed: 1111, MaxRooms: 16}
	config.PlantCatalog = &PlantCatalog{
		Rooms:     []RoomPlant{{ID: "somente-norte", Weight: 1, DoorDirections: []Direction{DirectionNorth}}},
		Corridors: []CorridorPlant{{ID: "corredor", Weight: 1}},
	}
	layout, err := (Generator{}).Generate(config)
	assert.ErrorIs(t, err, ErrNoCompatiblePlant)
	assert.Equal(t, Layout{}, layout)
}

func TestAC12aAtalhosMantemGrafoConectadoEAcrescentamCiclos(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 2, 0),
		placedRoomAt(2, 0, 2),
		placedRoomAt(3, 2, 2),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 2},
		{FromRoomID: 1, ToRoomID: 3},
	}
	connections, err := addExtraConnections(context.Background(), rooms, backbone, 2)
	require.NoError(t, err)
	assert.Equal(t, []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 2},
		{FromRoomID: 1, ToRoomID: 3},
		{FromRoomID: 2, ToRoomID: 3},
		{FromRoomID: 0, ToRoomID: 3},
	}, connections, "os atalhos devem ser as primeiras arestas descartadas na ordem normativa")

	config := Config{Width: 48, Height: 48, Seed: 121, MaxRooms: 24, ExtraEdgeCount: 3}
	effective, err := normalizeConfig(config)
	require.NoError(t, err)
	layout, err := (Generator{}).Generate(config)
	require.NoError(t, err)
	require.Greater(t, len(layout.Rooms), 2)
	assert.Equal(t, len(layout.Rooms)-1+3, len(layout.Corridors))
	assertLayoutInvariants(t, effective, layout)
}

func TestAC12bSemAtalhosGrafoEArvore(t *testing.T) {
	config := Config{Width: 48, Height: 48, Seed: 122, MaxRooms: 24, ExtraEdgeCount: 0}
	effective, err := normalizeConfig(config)
	require.NoError(t, err)
	layout, err := (Generator{}).Generate(config)
	require.NoError(t, err)
	assert.Equal(t, len(layout.Rooms)-1, len(layout.Corridors))
	assertLayoutInvariants(t, effective, layout)
}

func TestAC12cPapeisRespeitamMSTERequiredTags(t *testing.T) {
	config := Config{
		Width: 48, Height: 48, Seed: 123, MaxRooms: 24,
		RoomRoleRequests: []RoomRoleRequest{
			{Role: RoomRoleStart, Count: 1, RequiredTags: []string{"inicio"}},
			{Role: RoomRoleBoss, Count: 1, RequiredTags: []string{"chefe"}},
			{Role: RoomRoleTreasure, Count: 2, RequiredTags: []string{"tesouro"}},
		},
		PlantCatalog: &PlantCatalog{
			Rooms: []RoomPlant{
				{ID: "comum", Weight: 1, DoorDirections: allDirectionsForTest()},
				{ID: "inicio", Tags: []string{"inicio"}, Weight: 1, DoorDirections: allDirectionsForTest()},
				{ID: "chefe", Tags: []string{"chefe"}, Weight: 1, DoorDirections: allDirectionsForTest()},
				{ID: "tesouro", Tags: []string{"tesouro"}, Weight: 1, DoorDirections: allDirectionsForTest()},
			},
			Corridors: []CorridorPlant{{ID: "corredor", Weight: 1}},
		},
	}
	effective, err := normalizeConfig(config)
	require.NoError(t, err)
	layout, err := (Generator{}).Generate(config)
	require.NoError(t, err)
	require.NotNil(t, layout.Rooms[0].Role)
	assert.Equal(t, RoomRoleStart, *layout.Rooms[0].Role)
	counts := map[RoomRole]int{}
	for roomIndex, room := range layout.Rooms {
		if room.Role == nil {
			continue
		}
		counts[*room.Role]++
		requiredTag := map[RoomRole]string{RoomRoleStart: "inicio", RoomRoleBoss: "chefe", RoomRoleTreasure: "tesouro"}[*room.Role]
		assert.Contains(t, room.Tags, requiredTag, "Room %d", roomIndex)
	}
	assert.Equal(t, 1, counts[RoomRoleStart])
	assert.Equal(t, 1, counts[RoomRoleBoss])
	assert.Equal(t, 2, counts[RoomRoleTreasure])
	assertLayoutInvariants(t, effective, layout)
}

func TestAC12dPapeisVaziosNaoAlteramLayoutNemConsomemDraws(t *testing.T) {
	base := Config{Width: 40, Height: 40, Seed: 124, MaxRooms: 20}
	explicitlyEmpty := base
	explicitlyEmpty.RoomRoleRequests = []RoomRoleRequest{}
	baseline, err := (Generator{}).Generate(base)
	require.NoError(t, err)
	actual, err := (Generator{}).Generate(explicitlyEmpty)
	require.NoError(t, err)
	assert.Equal(t, baseline, actual)
	for roomIndex, room := range actual.Rooms {
		assert.Nil(t, room.Role, "Room %d", roomIndex)
	}
}

func TestAC12eOpcoesDesligadasPreservamLayoutRefinado(t *testing.T) {
	geometry := &RoomGeometry{
		MinWidth: 3, MaxWidth: 7, MinHeight: 3, MaxHeight: 7,
		MaxFootprintCells: 49, MinRoomGap: 1,
		Shapes: []RoomShapeWeight{
			{Shape: RoomShapeRectangle, Weight: 4},
			{Shape: RoomShapeL, Weight: 2},
			{Shape: RoomShapeT, Weight: 2},
			{Shape: RoomShapeCross, Weight: 1},
			{Shape: RoomShapeCircle, Weight: 2},
		},
	}
	base := Config{Width: 40, Height: 40, Seed: 125, MaxRooms: 20, RoomGeometry: geometry}
	explicitZeros := base
	explicitZeros.ExtraEdgeCount = 0
	explicitZeros.RoomRoleRequests = []RoomRoleRequest{}
	explicitZeros.DensityRegions = []DensityRegion{}
	baseline, err := (Generator{}).Generate(base)
	require.NoError(t, err)
	actual, err := (Generator{}).Generate(explicitZeros)
	require.NoError(t, err)
	assert.Equal(t, baseline, actual)
}

func TestAC14LimitesFalhamAntesDeGerarLayout(t *testing.T) {
	configs := []Config{
		{Width: 257, Height: 1, Seed: 1},
		{Width: 1, Height: 257, Seed: 1},
		{Width: 16, Height: 16, Seed: 1, MaxRooms: MaxRooms + 1},
	}
	for caseIndex, config := range configs {
		layout, err := (Generator{Placer: panicAcceptancePlacer{}}).Generate(config)
		assert.ErrorIs(t, err, ErrLimitExceeded, "caso %d", caseIndex)
		assert.Equal(t, Layout{}, layout, "caso %d", caseIndex)
	}
}

func TestAC27NenhumaPosicaoCabeFalhaSemTruncarOuPublicarLayout(t *testing.T) {
	geometry := &RoomGeometry{
		MinWidth: 6, MaxWidth: 6, MinHeight: 6, MaxHeight: 6,
		MaxFootprintCells: 36,
		Shapes:            []RoomShapeWeight{{Shape: RoomShapeRectangle, Weight: 1}},
	}
	layout, err := (Generator{}).Generate(Config{Width: 5, Height: 5, Seed: 27, RoomGeometry: geometry})
	assert.ErrorIs(t, err, ErrInvalidConfig)
	assert.Equal(t, Layout{}, layout)

	minimal, err := (Generator{}).Generate(Config{Width: 1, Height: 1, Seed: 27})
	require.NoError(t, err)
	require.Len(t, minimal.Rooms, 1)
	assert.Equal(t, []Cell{{X: 0, Y: 0}}, minimal.Rooms[0].Cells)
}

func TestAC29GeneratorRejeitaPlacersEConnectorsMentirosos(t *testing.T) {
	placerModes := []string{"duplicada", "desconexa", "fora", "incompativel", "sobreposta"}
	for _, mode := range placerModes {
		generator := Generator{Placer: lyingAcceptancePlacer{mode: mode}}
		layout, err := generator.Generate(Config{Width: 16, Height: 16, Seed: 29})
		assert.Error(t, err, "Placer %q", mode)
		assert.Equal(t, Layout{}, layout, "Placer %q", mode)
	}

	connectorModes := []string{"propria", "duplicada", "desconhecida", "desconectada"}
	for _, mode := range connectorModes {
		generator := Generator{Connector: lyingAcceptanceConnector{mode: mode}}
		layout, err := generator.Generate(Config{Width: 64, Height: 64, Seed: 29, MaxRooms: 24})
		assert.Error(t, err, "Connector %q", mode)
		assert.Equal(t, Layout{}, layout, "Connector %q", mode)
	}
}

func TestVerificadorDeInvariantesApontaCaminhoEsperadoEReal(t *testing.T) {
	config := Config{Width: 24, Height: 24, Seed: 5010, MaxRooms: 12}
	effective, err := normalizeConfig(config)
	require.NoError(t, err)
	layout, err := (Generator{}).Generate(config)
	require.NoError(t, err)
	layout.Grid.Cells[0].At = Cell{X: 99, Y: 99}

	failures := layoutInvariantFailures(effective, layout)
	require.NotEmpty(t, failures)
	assert.Contains(t, strings.Join(failures, "\n"), "Layout.Grid.Cells[0].At: esperado {0 0}; real {99 99}")
}

func randomPropertyConfig(random *rand.Rand, caseIndex int) Config {
	width := uint32(8 + random.Intn(33))
	height := uint32(8 + random.Intn(33))
	maxRooms := uint32(4 + random.Intn(15))
	config := Config{
		Width: width, Height: height, Seed: Seed(random.Uint64()),
		MinDistance:    float64(1 + random.Intn(8)),
		MaxAttempts:    uint32(1 + random.Intn(32)),
		MaxRooms:       maxRooms,
		CorridorOrder:  CorridorOrder(random.Intn(2)),
		ExtraEdgeCount: uint32(random.Intn(3)),
	}
	if caseIndex%2 == 0 {
		maximumWidth := min(width, uint32(7))
		maximumHeight := min(height, uint32(7))
		config.RoomGeometry = &RoomGeometry{
			MinWidth: 1, MaxWidth: maximumWidth, MinHeight: 1, MaxHeight: maximumHeight,
			MaxFootprintCells: maximumWidth * maximumHeight,
			MinRoomGap:        uint32(random.Intn(3)),
			Shapes: []RoomShapeWeight{
				{Shape: RoomShapeRectangle, Weight: uint32(1 + random.Intn(4))},
			},
		}
	}
	if caseIndex%3 == 0 {
		regionWidth := int32(width / 2)
		config.DensityRegions = []DensityRegion{{
			Min: Cell{X: 0, Y: 0}, Max: Cell{X: regionWidth, Y: int32(height)},
			MinDistance: float64(1 + random.Intn(8)),
		}}
	}
	if caseIndex%4 == 0 {
		config.RoomRoleRequests = []RoomRoleRequest{
			{Role: RoomRoleStart, Count: 1},
			{Role: RoomRoleBoss, Count: 1},
			{Role: RoomRoleTreasure, Count: min(uint32(2), maxRooms)},
		}
	}
	if caseIndex%11 == 0 {
		config.Width = 257
	}
	if caseIndex%13 == 0 {
		config.MaxRooms = MaxRooms + 1
	}
	return config
}

func isSpecifiedGenerationError(err error) bool {
	return errors.Is(err, ErrInvalidConfig) ||
		errors.Is(err, ErrLimitExceeded) ||
		errors.Is(err, ErrNoCompatiblePlant) ||
		errors.Is(err, ErrUnroutableEdge) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}

type panicAcceptancePlacer struct{}

func (panicAcceptancePlacer) Place(PlacementRequest) ([]RoomPlacement, error) {
	panic("Placer não deveria ser chamado antes da validação")
}

type lyingAcceptancePlacer struct {
	mode string
}

func (placer lyingAcceptancePlacer) Place(PlacementRequest) ([]RoomPlacement, error) {
	validMask := RoomShapeOffsets(RoomShapeRectangle, 3, 3)
	switch placer.mode {
	case "duplicada":
		return []RoomPlacement{{
			Shape: RoomShapeRectangle, Origin: Cell{X: 0, Y: 0}, Width: 2, Height: 2,
			Cells: []Cell{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}},
		}}, nil
	case "desconexa":
		return []RoomPlacement{{
			Shape: RoomShapeRectangle, Origin: Cell{X: 0, Y: 0}, Width: 2, Height: 2,
			Cells: []Cell{{X: 0, Y: 0}, {X: 1, Y: 1}},
		}}, nil
	case "fora":
		return []RoomPlacement{{
			Shape: RoomShapeRectangle, Origin: Cell{X: -1, Y: -1}, Width: 3, Height: 3, Cells: validMask,
		}}, nil
	case "incompativel":
		return []RoomPlacement{{
			Shape: RoomShapeCircle, Origin: Cell{X: 0, Y: 0}, Width: 3, Height: 3,
			Cells: []Cell{{X: 0, Y: 0}},
		}}, nil
	case "sobreposta":
		return []RoomPlacement{
			{Shape: RoomShapeRectangle, Origin: Cell{X: 0, Y: 0}, Width: 3, Height: 3, Cells: validMask},
			{Shape: RoomShapeRectangle, Origin: Cell{X: 1, Y: 1}, Width: 3, Height: 3, Cells: validMask},
		}, nil
	default:
		return nil, errors.New("modo de Placer desconhecido")
	}
}

type lyingAcceptanceConnector struct {
	mode string
}

func (connector lyingAcceptanceConnector) Connect(request ConnectionRequest) ([]Connection, error) {
	roomCount := len(request.Rooms)
	if roomCount < 4 {
		return nil, errors.New("caso de teste exige ao menos quatro Rooms")
	}
	edges := make([]Connection, 0, roomCount-1)
	for roomIndex := 1; roomIndex < roomCount; roomIndex++ {
		edges = append(edges, Connection{FromRoomID: RoomID(roomIndex - 1), ToRoomID: RoomID(roomIndex)})
	}
	switch connector.mode {
	case "propria":
		edges[0] = Connection{FromRoomID: 0, ToRoomID: 0}
	case "duplicada":
		edges[1] = edges[0]
	case "desconhecida":
		edges[0] = Connection{FromRoomID: 0, ToRoomID: RoomID(roomCount + 10)}
	case "desconectada":
		edges = []Connection{
			{FromRoomID: 1, ToRoomID: 2},
			{FromRoomID: 2, ToRoomID: 3},
			{FromRoomID: 3, ToRoomID: 1},
		}
		for roomIndex := 4; roomIndex < roomCount; roomIndex++ {
			edges = append(edges, Connection{FromRoomID: RoomID(roomIndex - 1), ToRoomID: RoomID(roomIndex)})
		}
	}
	return edges, nil
}
