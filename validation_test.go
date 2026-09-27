package daedalus

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizationAppliesDefaults(t *testing.T) {
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

func TestSingleCellGridNormalizationKeepsRectangle(t *testing.T) {
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

// TestDefaultNormalizationUsesDynamicProfile covers AC-19: a Config with no
// RoomGeometry normalizes to the default dynamic profile, including Circle.
func TestDefaultNormalizationUsesDynamicProfile(t *testing.T) {
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

func TestDefaultNormalizationFiltersShapesWithoutValidDimensions(t *testing.T) {
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

func TestNormalizationEnumeratesCombinationsInCanonicalOrder(t *testing.T) {
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
		assert.True(t, ordered, "combinations out of order: %+v before %+v", previous, current)
	}
}

func TestValidationDistinguishesInvalidConfigFromExceededLimit(t *testing.T) {
	invalidCases := []struct {
		name   string
		change func(*Config)
	}{
		{"missing width", func(config *Config) { config.Width = 0 }},
		{"missing height", func(config *Config) { config.Height = 0 }},
		{"negative CellSize", func(config *Config) { config.CellSize = -1 }},
		{"infinite CellSize", func(config *Config) { config.CellSize = math.Inf(1) }},
		{"MinDistance below the minimum", func(config *Config) { config.MinDistance = 0.5 }},
		{"MinDistance NaN", func(config *Config) { config.MinDistance = math.NaN() }},
		{"MaxAttempts exceeded", func(config *Config) { config.MaxAttempts = 1025 }},
		{"invalid CorridorOrder", func(config *Config) { config.CorridorOrder = CorridorOrder(9) }},
		{"ExtraEdgeCount exceeded", func(config *Config) { config.MaxRooms = 2; config.ExtraEdgeCount = 2 }},
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
		{"width exceeded", func(config *Config) { config.Width = 257 }},
		{"height exceeded", func(config *Config) { config.Height = 257 }},
		{"MaxRooms exceeded", func(config *Config) { config.MaxRooms = MaxRooms + 1 }},
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

func TestValidationRejectsGeometryWithoutValidAreaCombination(t *testing.T) {
	config := Config{Width: 8, Height: 8, RoomGeometry: &RoomGeometry{
		MinWidth: 2, MaxWidth: 2, MinHeight: 2, MaxHeight: 2,
		MaxFootprintCells: 1,
		Shapes:            []RoomShapeWeight{{Shape: RoomShapeRectangle, Weight: 1}},
	}}

	_, err := normalizeConfig(config)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestValidationRejectsOverflowingWeightSum(t *testing.T) {
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

func TestValidationRejectsOverlappingDensityRegions(t *testing.T) {
	config := Config{Width: 10, Height: 10, DensityRegions: []DensityRegion{
		{Min: Cell{X: 1, Y: 1}, Max: Cell{X: 5, Y: 5}, MinDistance: 2},
		{Min: Cell{X: 4, Y: 4}, Max: Cell{X: 8, Y: 8}, MinDistance: 3},
	}}

	_, err := normalizeConfig(config)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestValidationRejectsBossWithoutStart(t *testing.T) {
	config := Config{Width: 8, Height: 8, RoomRoleRequests: []RoomRoleRequest{
		{Role: RoomRoleBoss, Count: 1},
	}}

	_, err := normalizeConfig(config)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

// TestValidationRejectsInvalidGeometryFields covers AC-26: invalid
// RoomGeometry fails in normalizeConfig, the step Generate runs before any
// placement draw or partial Layout.
func TestValidationRejectsInvalidGeometryFields(t *testing.T) {
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
		{"missing minimum width", func(geometry *RoomGeometry) { geometry.MinWidth = 0 }, ErrInvalidConfig},
		{"maximum width smaller", func(geometry *RoomGeometry) { geometry.MaxWidth = 0 }, ErrInvalidConfig},
		{"height above the Grid", func(geometry *RoomGeometry) { geometry.MaxHeight = 9 }, ErrInvalidConfig},
		{"missing area", func(geometry *RoomGeometry) { geometry.MaxFootprintCells = 0 }, ErrInvalidConfig},
		{"area above the ceiling", func(geometry *RoomGeometry) { geometry.MaxFootprintCells = MaxFootprintCells + 1 }, ErrLimitExceeded},
		{"gap above the ceiling", func(geometry *RoomGeometry) { geometry.MinRoomGap = 257 }, ErrInvalidConfig},
		{"empty shapes", func(geometry *RoomGeometry) { geometry.Shapes = nil }, ErrInvalidConfig},
		{"duplicate shape", func(geometry *RoomGeometry) { geometry.Shapes = append(geometry.Shapes, geometry.Shapes[0]) }, ErrInvalidConfig},
		{"invalid shape", func(geometry *RoomGeometry) { geometry.Shapes[0].Shape = RoomShape(99) }, ErrInvalidConfig},
		{"missing weight", func(geometry *RoomGeometry) { geometry.Shapes[0].Weight = 0 }, ErrInvalidConfig},
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

func TestValidationRejectsInvalidRoles(t *testing.T) {
	cases := []struct {
		name     string
		requests []RoomRoleRequest
	}{
		{"invalid role", []RoomRoleRequest{{Role: RoomRole(99), Count: 1}}},
		{"duplicate role", []RoomRoleRequest{{Role: RoomRoleStart, Count: 1}, {Role: RoomRoleStart, Count: 1}}},
		{"invalid Start Count", []RoomRoleRequest{{Role: RoomRoleStart, Count: 2}}},
		{"Treasure Count exceeded", []RoomRoleRequest{{Role: RoomRoleTreasure, Count: 9}}},
		{"empty tag", []RoomRoleRequest{{Role: RoomRoleStart, Count: 1, RequiredTags: []string{""}}}},
		{"duplicate tag", []RoomRoleRequest{{Role: RoomRoleStart, Count: 1, RequiredTags: []string{"key", "key"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeConfig(Config{Width: 8, Height: 8, MaxRooms: 8, RoomRoleRequests: tc.requests})
			assert.ErrorIs(t, err, ErrInvalidConfig)
		})
	}
}

func TestValidationRejectsInvalidDensityRegions(t *testing.T) {
	cases := []struct {
		name   string
		region DensityRegion
	}{
		{"empty rectangle", DensityRegion{Min: Cell{X: 2, Y: 2}, Max: Cell{X: 2, Y: 3}, MinDistance: 1}},
		{"negative coordinate", DensityRegion{Min: Cell{X: -1, Y: 0}, Max: Cell{X: 2, Y: 2}, MinDistance: 1}},
		{"outside the Grid", DensityRegion{Min: Cell{}, Max: Cell{X: 9, Y: 2}, MinDistance: 1}},
		{"distance below the minimum", DensityRegion{Min: Cell{}, Max: Cell{X: 2, Y: 2}, MinDistance: 0.5}},
		{"non-finite distance", DensityRegion{Min: Cell{}, Max: Cell{X: 2, Y: 2}, MinDistance: math.NaN()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeConfig(Config{Width: 8, Height: 8, DensityRegions: []DensityRegion{tc.region}})
			assert.ErrorIs(t, err, ErrInvalidConfig)
		})
	}
}

func TestValidationRejectsInvalidCatalog(t *testing.T) {
	valid := PlantCatalog{
		Rooms:     []RoomPlant{{ID: "room", Weight: 1, DoorDirections: []Direction{DirectionNorth}}},
		Corridors: []CorridorPlant{{ID: "corridor", Weight: 1}},
	}
	cases := []struct {
		name   string
		change func(*PlantCatalog)
	}{
		{"empty Rooms", func(catalog *PlantCatalog) { catalog.Rooms = nil }},
		{"empty Corridors", func(catalog *PlantCatalog) { catalog.Corridors = nil }},
		{"empty Room ID", func(catalog *PlantCatalog) { catalog.Rooms[0].ID = "" }},
		{"duplicate Room ID", func(catalog *PlantCatalog) { catalog.Rooms = append(catalog.Rooms, catalog.Rooms[0]) }},
		{"duplicate Corridor ID", func(catalog *PlantCatalog) { catalog.Corridors = append(catalog.Corridors, catalog.Corridors[0]) }},
		{"missing Room Weight", func(catalog *PlantCatalog) { catalog.Rooms[0].Weight = 0 }},
		{"missing Corridor Weight", func(catalog *PlantCatalog) { catalog.Corridors[0].Weight = 0 }},
		{"empty Directions", func(catalog *PlantCatalog) { catalog.Rooms[0].DoorDirections = nil }},
		{"duplicate Direction", func(catalog *PlantCatalog) {
			catalog.Rooms[0].DoorDirections = []Direction{DirectionNorth, DirectionNorth}
		}},
		{"invalid Direction", func(catalog *PlantCatalog) { catalog.Rooms[0].DoorDirections = []Direction{Direction(9)} }},
		{"empty Room tag", func(catalog *PlantCatalog) { catalog.Rooms[0].Tags = []string{""} }},
		{"duplicate Corridor tag", func(catalog *PlantCatalog) { catalog.Corridors[0].Tags = []string{"stone", "stone"} }},
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
