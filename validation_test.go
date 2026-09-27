package daedalus

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizacaoAplicaDefaults(t *testing.T) {
	effective, err := normalizeConfig(Config{Width: 16, Height: 12, Seed: 0})
	require.NoError(t, err)

	assert.Equal(t, 1.0, effective.cellSize)
	assert.Equal(t, 6.0, effective.minDistance)
	assert.Equal(t, uint32(30), effective.maxAttempts)
	assert.Equal(t, uint32(MaxRooms), effective.maxRooms)
	assert.Equal(t, CorridorOrderXThenY, effective.corridorOrder)
	assert.Equal(t, Seed(0), effective.seed)
	assert.Empty(t, effective.roomRoleRequests)
	assert.Empty(t, effective.densityRegions)
	assert.Nil(t, effective.plantCatalog)
}

func TestNormalizacaoDoGridUmPorUmMantemRetangulo(t *testing.T) {
	effective, err := normalizeConfig(Config{Width: 1, Height: 1})
	require.NoError(t, err)

	assert.Equal(t, RoomGeometry{
		MinWidth:          1,
		MaxWidth:          1,
		MinHeight:         1,
		MaxHeight:         1,
		MaxFootprintCells: 81,
		MinRoomGap:        1,
		Shapes:            []RoomShapeWeight{{Shape: RoomShapeRectangle, Weight: 4}},
	}, effective.roomGeometry)
	assert.Equal(t, []roomGeometryCombination{{shape: RoomShapeRectangle, width: 1, height: 1}}, effective.geometryCombinations)
}

func TestNormalizacaoPadraoUsaPerfilDinamico(t *testing.T) {
	effective, err := normalizeConfig(Config{Width: 16, Height: 16})
	require.NoError(t, err)

	assert.Equal(t, uint32(3), effective.roomGeometry.MinWidth)
	assert.Equal(t, uint32(9), effective.roomGeometry.MaxWidth)
	assert.Equal(t, uint32(3), effective.roomGeometry.MinHeight)
	assert.Equal(t, uint32(9), effective.roomGeometry.MaxHeight)
	assert.Equal(t, []RoomShapeWeight{
		{Shape: RoomShapeRectangle, Weight: 4},
		{Shape: RoomShapeL, Weight: 2},
		{Shape: RoomShapeT, Weight: 2},
		{Shape: RoomShapeCross, Weight: 1},
		{Shape: RoomShapeCircle, Weight: 2},
	}, effective.roomGeometry.Shapes)
	assert.Contains(t, effective.geometryCombinations, roomGeometryCombination{shape: RoomShapeCircle, width: 5, height: 5})
}

func TestNormalizacaoPadraoFiltraFormasSemDimensaoValida(t *testing.T) {
	effective, err := normalizeConfig(Config{Width: 2, Height: 2})
	require.NoError(t, err)

	assert.Equal(t, []RoomShapeWeight{
		{Shape: RoomShapeRectangle, Weight: 4},
		{Shape: RoomShapeL, Weight: 2},
	}, effective.roomGeometry.Shapes)
	for _, combination := range effective.geometryCombinations {
		assert.NotEqual(t, RoomShapeCross, combination.shape)
		assert.NotEqual(t, RoomShapeCircle, combination.shape)
	}
}

func TestNormalizacaoEnumeraCombinacoesEmOrdemCanonica(t *testing.T) {
	config := Config{
		Width:  7,
		Height: 7,
		RoomGeometry: &RoomGeometry{
			MinWidth:          2,
			MaxWidth:          5,
			MinHeight:         2,
			MaxHeight:         5,
			MaxFootprintCells: 25,
			Shapes: []RoomShapeWeight{
				{Shape: RoomShapeCircle, Weight: 2},
				{Shape: RoomShapeRectangle, Weight: 4},
				{Shape: RoomShapeT, Weight: 1},
			},
		},
	}

	first, err := normalizeConfig(config)
	require.NoError(t, err)
	second, err := normalizeConfig(config)
	require.NoError(t, err)
	assert.True(t, reflect.DeepEqual(first, second))

	for index := 1; index < len(first.geometryCombinations); index++ {
		previous := first.geometryCombinations[index-1]
		current := first.geometryCombinations[index]
		ordered := previous.shape < current.shape ||
			previous.shape == current.shape && previous.width < current.width ||
			previous.shape == current.shape && previous.width == current.width && previous.height < current.height
		assert.True(t, ordered, "combinações fora da ordem: %+v antes de %+v", previous, current)
	}
}

func TestValidacaoDistingueConfigInvalidaDeLimiteExcedido(t *testing.T) {
	invalidCases := []struct {
		name   string
		change func(*Config)
	}{
		{"largura ausente", func(config *Config) { config.Width = 0 }},
		{"altura ausente", func(config *Config) { config.Height = 0 }},
		{"CellSize negativo", func(config *Config) { config.CellSize = -1 }},
		{"CellSize infinito", func(config *Config) { config.CellSize = math.Inf(1) }},
		{"MinDistance abaixo do minimo", func(config *Config) { config.MinDistance = 0.5 }},
		{"MinDistance NaN", func(config *Config) { config.MinDistance = math.NaN() }},
		{"MaxAttempts excedido", func(config *Config) { config.MaxAttempts = 1025 }},
		{"CorridorOrder invalido", func(config *Config) { config.CorridorOrder = CorridorOrder(9) }},
		{"ExtraEdgeCount excedido", func(config *Config) { config.MaxRooms = 2; config.ExtraEdgeCount = 2 }},
	}
	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			config := Config{Width: 8, Height: 8}
			tc.change(&config)
			_, err := normalizeConfig(config)
			assert.ErrorIs(t, err, ErrInvalidConfig)
			assert.False(t, errors.Is(err, ErrLimitExceeded))
		})
	}

	limitCases := []struct {
		name   string
		change func(*Config)
	}{
		{"largura excedida", func(config *Config) { config.Width = 257 }},
		{"altura excedida", func(config *Config) { config.Height = 257 }},
		{"MaxRooms excedido", func(config *Config) { config.MaxRooms = MaxRooms + 1 }},
	}
	for _, tc := range limitCases {
		t.Run(tc.name, func(t *testing.T) {
			config := Config{Width: 8, Height: 8}
			tc.change(&config)
			_, err := normalizeConfig(config)
			assert.ErrorIs(t, err, ErrLimitExceeded)
			assert.False(t, errors.Is(err, ErrInvalidConfig))
		})
	}
}

func TestValidacaoRejeitaGeometriaSemCombinacaoDeAreaValida(t *testing.T) {
	config := Config{Width: 8, Height: 8, RoomGeometry: &RoomGeometry{
		MinWidth: 2, MaxWidth: 2, MinHeight: 2, MaxHeight: 2,
		MaxFootprintCells: 1,
		Shapes:            []RoomShapeWeight{{Shape: RoomShapeRectangle, Weight: 1}},
	}}

	_, err := normalizeConfig(config)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestValidacaoRejeitaSomaDePesosComOverflow(t *testing.T) {
	config := Config{Width: 8, Height: 8, RoomGeometry: &RoomGeometry{
		MinWidth: 3, MaxWidth: 3, MinHeight: 3, MaxHeight: 3,
		MaxFootprintCells: 9,
		Shapes: []RoomShapeWeight{
			{Shape: RoomShapeRectangle, Weight: math.MaxUint32},
			{Shape: RoomShapeL, Weight: 1},
		},
	}}

	_, err := normalizeConfig(config)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestValidacaoRejeitaRegioesDeDensidadeSobrepostas(t *testing.T) {
	config := Config{Width: 10, Height: 10, DensityRegions: []DensityRegion{
		{Min: Cell{X: 1, Y: 1}, Max: Cell{X: 5, Y: 5}, MinDistance: 2},
		{Min: Cell{X: 4, Y: 4}, Max: Cell{X: 8, Y: 8}, MinDistance: 3},
	}}

	_, err := normalizeConfig(config)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestValidacaoRejeitaBossSemStart(t *testing.T) {
	config := Config{Width: 8, Height: 8, RoomRoleRequests: []RoomRoleRequest{
		{Role: RoomRoleBoss, Count: 1},
	}}

	_, err := normalizeConfig(config)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestValidacaoRejeitaCamposInvalidosDeGeometria(t *testing.T) {
	base := RoomGeometry{
		MinWidth: 1, MaxWidth: 4, MinHeight: 1, MaxHeight: 4,
		MaxFootprintCells: 16,
		Shapes:            []RoomShapeWeight{{Shape: RoomShapeRectangle, Weight: 1}},
	}
	cases := []struct {
		name   string
		change func(*RoomGeometry)
		want   error
	}{
		{"largura minima ausente", func(geometry *RoomGeometry) { geometry.MinWidth = 0 }, ErrInvalidConfig},
		{"largura maxima menor", func(geometry *RoomGeometry) { geometry.MaxWidth = 0 }, ErrInvalidConfig},
		{"altura acima do Grid", func(geometry *RoomGeometry) { geometry.MaxHeight = 9 }, ErrInvalidConfig},
		{"area ausente", func(geometry *RoomGeometry) { geometry.MaxFootprintCells = 0 }, ErrInvalidConfig},
		{"area acima do teto", func(geometry *RoomGeometry) { geometry.MaxFootprintCells = MaxFootprintCells + 1 }, ErrLimitExceeded},
		{"gap acima do teto", func(geometry *RoomGeometry) { geometry.MinRoomGap = 257 }, ErrInvalidConfig},
		{"formas vazias", func(geometry *RoomGeometry) { geometry.Shapes = nil }, ErrInvalidConfig},
		{"forma duplicada", func(geometry *RoomGeometry) { geometry.Shapes = append(geometry.Shapes, geometry.Shapes[0]) }, ErrInvalidConfig},
		{"forma invalida", func(geometry *RoomGeometry) { geometry.Shapes[0].Shape = RoomShape(99) }, ErrInvalidConfig},
		{"peso ausente", func(geometry *RoomGeometry) { geometry.Shapes[0].Weight = 0 }, ErrInvalidConfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			geometry := base
			geometry.Shapes = append([]RoomShapeWeight(nil), base.Shapes...)
			tc.change(&geometry)
			_, err := normalizeConfig(Config{Width: 8, Height: 8, RoomGeometry: &geometry})
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

func TestValidacaoRejeitaPapeisInvalidos(t *testing.T) {
	cases := []struct {
		name     string
		requests []RoomRoleRequest
	}{
		{"papel invalido", []RoomRoleRequest{{Role: RoomRole(99), Count: 1}}},
		{"papel duplicado", []RoomRoleRequest{{Role: RoomRoleStart, Count: 1}, {Role: RoomRoleStart, Count: 1}}},
		{"Count de Start invalido", []RoomRoleRequest{{Role: RoomRoleStart, Count: 2}}},
		{"Count de Treasure excedido", []RoomRoleRequest{{Role: RoomRoleTreasure, Count: 9}}},
		{"tag vazia", []RoomRoleRequest{{Role: RoomRoleStart, Count: 1, RequiredTags: []string{""}}}},
		{"tag duplicada", []RoomRoleRequest{{Role: RoomRoleStart, Count: 1, RequiredTags: []string{"chave", "chave"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeConfig(Config{Width: 8, Height: 8, MaxRooms: 8, RoomRoleRequests: tc.requests})
			assert.ErrorIs(t, err, ErrInvalidConfig)
		})
	}
}

func TestValidacaoRejeitaRegioesDeDensidadeInvalidas(t *testing.T) {
	cases := []struct {
		name   string
		region DensityRegion
	}{
		{"retangulo vazio", DensityRegion{Min: Cell{X: 2, Y: 2}, Max: Cell{X: 2, Y: 3}, MinDistance: 1}},
		{"coordenada negativa", DensityRegion{Min: Cell{X: -1, Y: 0}, Max: Cell{X: 2, Y: 2}, MinDistance: 1}},
		{"fora do Grid", DensityRegion{Min: Cell{}, Max: Cell{X: 9, Y: 2}, MinDistance: 1}},
		{"distancia abaixo do minimo", DensityRegion{Min: Cell{}, Max: Cell{X: 2, Y: 2}, MinDistance: 0.5}},
		{"distancia nao finita", DensityRegion{Min: Cell{}, Max: Cell{X: 2, Y: 2}, MinDistance: math.NaN()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeConfig(Config{Width: 8, Height: 8, DensityRegions: []DensityRegion{tc.region}})
			assert.ErrorIs(t, err, ErrInvalidConfig)
		})
	}
}

func TestValidacaoRejeitaCatalogoInvalido(t *testing.T) {
	valid := PlantCatalog{
		Rooms:     []RoomPlant{{ID: "room", Weight: 1, DoorDirections: []Direction{DirectionNorth}}},
		Corridors: []CorridorPlant{{ID: "corridor", Weight: 1}},
	}
	cases := []struct {
		name   string
		change func(*PlantCatalog)
	}{
		{"Rooms vazias", func(catalog *PlantCatalog) { catalog.Rooms = nil }},
		{"Corridors vazios", func(catalog *PlantCatalog) { catalog.Corridors = nil }},
		{"Room ID vazio", func(catalog *PlantCatalog) { catalog.Rooms[0].ID = "" }},
		{"Room ID duplicado", func(catalog *PlantCatalog) { catalog.Rooms = append(catalog.Rooms, catalog.Rooms[0]) }},
		{"Corridor ID duplicado", func(catalog *PlantCatalog) { catalog.Corridors = append(catalog.Corridors, catalog.Corridors[0]) }},
		{"Room Weight ausente", func(catalog *PlantCatalog) { catalog.Rooms[0].Weight = 0 }},
		{"Corridor Weight ausente", func(catalog *PlantCatalog) { catalog.Corridors[0].Weight = 0 }},
		{"Directions vazias", func(catalog *PlantCatalog) { catalog.Rooms[0].DoorDirections = nil }},
		{"Direction duplicada", func(catalog *PlantCatalog) {
			catalog.Rooms[0].DoorDirections = []Direction{DirectionNorth, DirectionNorth}
		}},
		{"Direction invalida", func(catalog *PlantCatalog) { catalog.Rooms[0].DoorDirections = []Direction{Direction(9)} }},
		{"tag de Room vazia", func(catalog *PlantCatalog) { catalog.Rooms[0].Tags = []string{""} }},
		{"tag de Corridor duplicada", func(catalog *PlantCatalog) { catalog.Corridors[0].Tags = []string{"pedra", "pedra"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			catalog := clonePlantCatalogForTest(valid)
			tc.change(&catalog)
			_, err := normalizeConfig(Config{Width: 8, Height: 8, PlantCatalog: &catalog})
			assert.ErrorIs(t, err, ErrInvalidConfig)
		})
	}
}

func clonePlantCatalogForTest(source PlantCatalog) PlantCatalog {
	clone := PlantCatalog{
		Rooms:     append([]RoomPlant(nil), source.Rooms...),
		Corridors: append([]CorridorPlant(nil), source.Corridors...),
	}
	for index := range clone.Rooms {
		clone.Rooms[index].Tags = append([]string(nil), clone.Rooms[index].Tags...)
		clone.Rooms[index].DoorDirections = append([]Direction(nil), clone.Rooms[index].DoorDirections...)
	}
	for index := range clone.Corridors {
		clone.Corridors[index].Tags = append([]string(nil), clone.Corridors[index].Tags...)
	}
	return clone
}
