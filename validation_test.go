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
	assert.Nil(t, effective.corridorWidths)
}

func TestSingleCellGridNormalizationKeepsRectangle(t *testing.T) {
	effective, err := normalizeConfig(Config{Width: 1, Height: 1})
	require.NoError(t, err)

	assert.Equal(t, RoomGeometry{
		MaxFootprintCells: 81,
		MinRoomGap:        1,
		Shapes:            []RoomShapeWeight{shapeSpan(RoomShapeRectangle, 4, 1, 1, 1, 1)},
	}, effective.roomGeometry)
	assert.Equal(t, []roomGeometryCombination{{shape: RoomShapeRectangle, width: 1, height: 1}}, effective.geometryCombinations)
}

// TestDefaultNormalizationUsesDynamicProfile checks that a Config with no
// RoomGeometry normalizes to the default dynamic profile, including Circle.
func TestDefaultNormalizationUsesDynamicProfile(t *testing.T) {
	effective, err := normalizeConfig(Config{Width: 16, Height: 16})
	require.NoError(t, err)

	assert.Equal(t, []RoomShapeWeight{
		shapeSpan(RoomShapeRectangle, 4, 3, 9, 3, 9),
		shapeSpan(RoomShapeL, 2, 3, 9, 3, 9),
		shapeSpan(RoomShapeT, 2, 3, 9, 3, 9),
		shapeSpan(RoomShapeCross, 1, 3, 9, 3, 9),
		shapeSpan(RoomShapeCircle, 2, 5, 9, 5, 9),
	}, effective.roomGeometry.Shapes)
	assert.Contains(t, effective.geometryCombinations, roomGeometryCombination{shape: RoomShapeCircle, width: 5, height: 5})
	assert.NotContains(t, effective.geometryCombinations, roomGeometryCombination{shape: RoomShapeCircle, width: 6, height: 6})
	assert.NotContains(t, effective.geometryCombinations, roomGeometryCombination{shape: RoomShapeCircle, width: 3, height: 3})
}

func TestDefaultNormalizationFiltersShapesWithoutValidDimensions(t *testing.T) {
	effective, err := normalizeConfig(Config{Width: 2, Height: 2})
	require.NoError(t, err)

	assert.Equal(t, []RoomShapeWeight{
		shapeSpan(RoomShapeRectangle, 4, 2, 2, 2, 2),
		shapeSpan(RoomShapeL, 2, 2, 2, 2, 2),
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
			MaxFootprintCells: 25,
			Shapes: []RoomShapeWeight{
				shapeSpan(RoomShapeCircle, 2, 2, 5, 2, 5),
				shapeSpan(RoomShapeRectangle, 4, 2, 5, 2, 5),
				shapeSpan(RoomShapeT, 1, 2, 5, 2, 5),
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
		{"MaxRoomEdges below 2", func(config *Config) { config.MaxRoomEdges = 1 }},
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
		MaxFootprintCells: 1,
		Shapes:            []RoomShapeWeight{shapeSpan(RoomShapeRectangle, 1, 2, 2, 2, 2)},
	}}

	_, err := normalizeConfig(config)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestValidationRejectsOverflowingWeightSum(t *testing.T) {
	config := Config{Width: 8, Height: 8, RoomGeometry: &RoomGeometry{
		MaxFootprintCells: 9,
		Shapes: []RoomShapeWeight{
			shapeSpan(RoomShapeRectangle, math.MaxUint32, 3, 3, 3, 3),
			shapeSpan(RoomShapeL, 1, 3, 3, 3, 3),
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

// TestValidationRejectsInvalidGeometryFields checks that invalid RoomGeometry
// fails in normalizeConfig, the step Generate runs before any placement draw
// or partial Layout.
func TestValidationRejectsInvalidGeometryFields(t *testing.T) {
	base := RoomGeometry{
		MaxFootprintCells: 16,
		Shapes:            []RoomShapeWeight{shapeSpan(RoomShapeRectangle, 1, 1, 4, 1, 4)},
	}
	cases := []struct {
		name   string
		change func(*RoomGeometry)
		want   error
	}{
		{"width maximum is zero", func(geometry *RoomGeometry) { geometry.Shapes[0].Width.Max = 0 }, ErrInvalidConfig},
		{"height minimum exceeds maximum", func(geometry *RoomGeometry) {
			geometry.Shapes[0].Height = DimensionRange{Min: 5, Max: 2}
		}, ErrInvalidConfig},
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

// TestDimensionRangesKeepEmptySpansImpossibleMasksAndGridDropsApart checks the
// three ways a per-shape span can fail. An empty span and a span with no legal
// mask are ErrInvalidConfig, and their messages differ. A span the mask
// accepts but the Grid cannot hold drops that shape and leaves Rectangle.
func TestDimensionRangesKeepEmptySpansImpossibleMasksAndGridDropsApart(t *testing.T) {
	rectangle := shapeSpan(RoomShapeRectangle, 1, 1, 4, 1, 4)

	t.Run("maximum is zero", func(t *testing.T) {
		geometry := RoomGeometry{
			MaxFootprintCells: 16,
			Shapes:            []RoomShapeWeight{shapeSpan(RoomShapeRectangle, 1, 1, 0, 1, 4)},
		}
		_, err := normalizeConfig(Config{Width: 8, Height: 8, RoomGeometry: &geometry})
		assert.ErrorIs(t, err, ErrInvalidConfig)
		assert.ErrorContains(t, err, "width range maximum is 0")
		assert.NotContains(t, err.Error(), "admits no legal size")
	})

	t.Run("minimum exceeds maximum", func(t *testing.T) {
		geometry := RoomGeometry{
			MaxFootprintCells: 16,
			Shapes:            []RoomShapeWeight{shapeSpan(RoomShapeRectangle, 1, 6, 2, 1, 4)},
		}
		_, err := normalizeConfig(Config{Width: 8, Height: 8, RoomGeometry: &geometry})
		assert.ErrorIs(t, err, ErrInvalidConfig)
		assert.ErrorContains(t, err, "width range minimum exceeds maximum")
	})

	t.Run("circle 6 by 6 admits no legal size on a large grid", func(t *testing.T) {
		geometry := RoomGeometry{
			MaxFootprintCells: 81,
			Shapes: []RoomShapeWeight{
				rectangle,
				shapeSpan(RoomShapeCircle, 1, 6, 6, 6, 6),
			},
		}
		_, err := normalizeConfig(Config{Width: 64, Height: 64, RoomGeometry: &geometry})
		assert.ErrorIs(t, err, ErrInvalidConfig)
		assert.ErrorContains(t, err, "admits no legal size")
		assert.ErrorContains(t, err, "circle")
		assert.NotContains(t, err.Error(), "no valid combination")
	})

	t.Run("circle larger than the grid is dropped and rectangle remains", func(t *testing.T) {
		geometry := RoomGeometry{
			MaxFootprintCells: 81,
			Shapes: []RoomShapeWeight{
				shapeSpan(RoomShapeRectangle, 4, 1, 3, 1, 3),
				shapeSpan(RoomShapeCircle, 2, 7, 9, 7, 9),
			},
		}
		effective, err := normalizeConfig(Config{Width: 5, Height: 5, RoomGeometry: &geometry})
		require.NoError(t, err)
		require.Len(t, effective.roomGeometry.Shapes, 1)
		assert.Equal(t, RoomShapeRectangle, effective.roomGeometry.Shapes[0].Shape)
		for _, combination := range effective.geometryCombinations {
			assert.NotEqual(t, RoomShapeCircle, combination.shape)
		}
	})

	t.Run("rectangle past the grid is not an impossible mask", func(t *testing.T) {
		geometry := RoomGeometry{
			MaxFootprintCells: 81,
			Shapes:            []RoomShapeWeight{shapeSpan(RoomShapeRectangle, 1, 9, 9, 9, 9)},
		}
		_, err := normalizeConfig(Config{Width: 8, Height: 8, RoomGeometry: &geometry})
		assert.ErrorIs(t, err, ErrInvalidConfig)
		assert.ErrorContains(t, err, "no valid combination within the area")
		assert.NotContains(t, err.Error(), "admits no legal size")
		assert.NotContains(t, err.Error(), "maximum is 0")
	})

	t.Run("a range that overlaps the grid keeps the sizes that fit", func(t *testing.T) {
		geometry := RoomGeometry{
			MaxFootprintCells: 81,
			Shapes:            []RoomShapeWeight{shapeSpan(RoomShapeRectangle, 1, 1, 9, 1, 9)},
		}
		effective, err := normalizeConfig(Config{Width: 8, Height: 8, RoomGeometry: &geometry})
		require.NoError(t, err)
		assert.Equal(t, DimensionRange{Min: 1, Max: 8}, effective.roomGeometry.Shapes[0].Width)
		assert.Equal(t, DimensionRange{Min: 1, Max: 8}, effective.roomGeometry.Shapes[0].Height)
		for _, combination := range effective.geometryCombinations {
			assert.LessOrEqual(t, combination.width, uint32(8))
			assert.LessOrEqual(t, combination.height, uint32(8))
		}
	})
}

func TestShapeRangeAdmitsMatchesValidRoomShapeDimensions(t *testing.T) {
	shapes := []RoomShape{RoomShapeRectangle, RoomShapeL, RoomShapeT, RoomShapeCross, RoomShapeCircle}
	for _, shape := range shapes {
		for width := uint32(0); width <= 12; width++ {
			for height := uint32(0); height <= 12; height++ {
				got := shapeRangeAdmits(shape, DimensionRange{Min: width, Max: width}, DimensionRange{Min: height, Max: height})
				want := ValidRoomShapeDimensions(shape, width, height)
				assert.Equal(t, want, got, "shape %d size %dx%d", shape, width, height)
			}
		}
	}
}

func TestOmittedProfileRangesStayLegalOrUnclamped(t *testing.T) {
	width, height, ok := DefaultDimensionRanges(RoomShapeCircle, 16, 16)
	require.True(t, ok)
	assert.Equal(t, DimensionRange{Min: 5, Max: 9}, width)
	assert.Equal(t, DimensionRange{Min: 5, Max: 9}, height)

	width, height, ok = DefaultDimensionRanges(RoomShapeCircle, 4, 4)
	require.True(t, ok)
	assert.Equal(t, DimensionRange{Min: 5, Max: 9}, width)
	assert.Equal(t, DimensionRange{Min: 5, Max: 9}, height)
	assert.NotEqual(t, DimensionRange{}, width)

	width, height, ok = DefaultDimensionRanges(RoomShapeRectangle, 1, 1)
	require.True(t, ok)
	assert.Equal(t, DimensionRange{Min: 1, Max: 1}, width)
	assert.Equal(t, DimensionRange{Min: 1, Max: 1}, height)

	_, _, ok = DefaultDimensionRanges(RoomShape(99), 16, 16)
	assert.False(t, ok)
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

// TestValidationRequiresCorridorWidthOne checks the one catalog shape the
// router cannot work with. Degradation walks declared widths only, so a
// catalog without width 1 has nowhere to fall back to; the rejection names
// the catalog rather than leaving a later Room to fail as unconnectable.
func TestValidationRequiresCorridorWidthOne(t *testing.T) {
	cases := []struct {
		name     string
		geometry *CorridorGeometry
		want     error
	}{
		{"nil geometry stays legal", nil, nil},
		{"only width 1", &CorridorGeometry{Widths: []CorridorWidthWeight{{Width: 1, Weight: 1}}}, nil},
		{"width 1 alongside a wider one", &CorridorGeometry{Widths: []CorridorWidthWeight{
			{Width: 1, Weight: 2},
			{Width: 3, Weight: 1},
		}}, nil},
		{"a single wide width", &CorridorGeometry{Widths: []CorridorWidthWeight{{Width: 3, Weight: 1}}}, ErrInvalidConfig},
		{"wide widths without 1", &CorridorGeometry{Widths: []CorridorWidthWeight{
			{Width: 2, Weight: 1},
			{Width: 3, Weight: 1},
		}}, ErrInvalidConfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeConfig(Config{Width: 8, Height: 8, CorridorGeometry: tc.geometry})
			if tc.want == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.want)
			assert.Contains(t, err.Error(), "CorridorGeometry.Widths must declare width 1")
		})
	}
}

// TestEmptyCorridorWidthsKeepTheEmptyMessage pins the order of the two
// catalog-shaped checks: an empty list is empty, not a list missing width 1.
func TestEmptyCorridorWidthsKeepTheEmptyMessage(t *testing.T) {
	_, err := normalizeConfig(Config{Width: 8, Height: 8, CorridorGeometry: &CorridorGeometry{}})
	require.ErrorIs(t, err, ErrInvalidConfig)
	assert.Contains(t, err.Error(), "must not be empty")
	assert.NotContains(t, err.Error(), "must declare width 1")
}
