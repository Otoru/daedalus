package daedalus

import (
	"fmt"
	"math"
	"sort"
	"unicode/utf8"
)

const (
	// Fixed v1 Config defaults. CellSize is 1. MinDistance defaults to 6.
	defaultCellSize    = 1.0
	defaultMinDistance = 6.0
	// MinDistance floor, in Cells. It is an invariant in its own right and must
	// not be confused with defaultCellSize, which happens to have the same value
	// but describes a different quantity.
	minimumMinDistance     = 1.0
	defaultMaxAttempts     = 30
	maximumMaxAttempts     = 1024
	maximumGridDimension   = 256
	maximumMinRoomGap      = 256
	defaultGeometryMinSize = 3
	defaultGeometryMaxSize = 9
	// defaultCircleMinSize is 5 because a disc on a square Grid needs an odd
	// diameter of at least 5, with a centre Cell. A shared 3..9 span would
	// include diameters that are not circles.
	defaultCircleMinSize        = 5
	defaultMaxFootprintCells    = 81
	defaultMinRoomGap           = 1
	defaultMinTerrainPatchCells = 8
	defaultMaxTerrainPatchCells = 24
	// maximumCorridorWidth is the largest Corridor width a request may name.
	// A larger value is nonsense, not a product limit, so it is ErrInvalidConfig.
	maximumCorridorWidth = 64

	// Default dynamic geometry profile weights, in canonical order.
	defaultRectangleWeight = 4
	defaultLWeight         = 2
	defaultTWeight         = 2
	defaultCrossWeight     = 1
	defaultCircleWeight    = 2
)

var canonicalRoomShapes = [...]RoomShape{
	RoomShapeRectangle,
	RoomShapeL,
	RoomShapeT,
	RoomShapeCross,
	RoomShapeCircle,
}

// roomGeometryCombination is an already validated mask and bounding-box
// combination. Canonical order is Shape, Width, then Height.
type roomGeometryCombination struct {
	shape  RoomShape
	width  uint32
	height uint32
}

// effectiveConfig centralizes defaults and validated data so generation phases
// do not need to reinterpret the public Config.
type effectiveConfig struct {
	width                uint32
	height               uint32
	cellSize             float64
	seed                 Seed
	minDistance          float64
	maxAttempts          uint32
	maxRooms             uint32
	corridorOrder        CorridorOrder
	extraEdgeCount       uint32
	maxRoomEdges         uint32
	roomRoleRequests     []RoomRoleRequest
	densityRegions       []DensityRegion
	roomGeometry         RoomGeometry
	geometryCombinations []roomGeometryCombination
	corridorWidths       []CorridorWidthWeight
	plantCatalog         *PlantCatalog
	terrain              *TerrainConfig
}

// normalizeConfig validates all input before any generation phase and returns
// an effective copy with defaults applied and geometry enumerated.
func normalizeConfig(config Config) (effectiveConfig, error) {
	width, height, err := normalizeGrid(config.Width, config.Height)
	if err != nil {
		return effectiveConfig{}, err
	}
	terrain, err := normalizeTerrainConfig(config.Terrain)
	if err != nil {
		return effectiveConfig{}, err
	}
	cellSize, err := normalizeCellSize(config.CellSize)
	if err != nil {
		return effectiveConfig{}, err
	}
	minDistance, err := normalizeMinDistance(config.MinDistance)
	if err != nil {
		return effectiveConfig{}, err
	}
	maxAttempts, err := normalizeMaxAttempts(config.MaxAttempts)
	if err != nil {
		return effectiveConfig{}, err
	}
	maxRooms, err := normalizeMaxRooms(config.MaxRooms)
	if err != nil {
		return effectiveConfig{}, err
	}
	if !validCorridorOrder(config.CorridorOrder) {
		return effectiveConfig{}, fmt.Errorf("%w: CorridorOrder desconhecida", ErrInvalidConfig)
	}
	if err := validateExtraEdgeCount(config.ExtraEdgeCount, maxRooms); err != nil {
		return effectiveConfig{}, err
	}
	if err := validateMaxRoomEdges(config.MaxRoomEdges); err != nil {
		return effectiveConfig{}, err
	}

	roleRequests, err := validateRoomRoleRequests(config.RoomRoleRequests, maxRooms)
	if err != nil {
		return effectiveConfig{}, err
	}
	densityRegions, err := validateDensityRegions(config.DensityRegions, config.Width, config.Height)
	if err != nil {
		return effectiveConfig{}, err
	}
	geometry, combinations, err := normalizeRoomGeometry(config.RoomGeometry, config.Width, config.Height)
	if err != nil {
		return effectiveConfig{}, err
	}
	corridorWidths, err := normalizeCorridorGeometry(config.CorridorGeometry)
	if err != nil {
		return effectiveConfig{}, err
	}
	catalog, err := validatePlantCatalog(config.PlantCatalog)
	if err != nil {
		return effectiveConfig{}, err
	}

	return effectiveConfig{
		width:                width,
		height:               height,
		cellSize:             cellSize,
		seed:                 config.Seed,
		minDistance:          minDistance,
		maxAttempts:          maxAttempts,
		maxRooms:             maxRooms,
		corridorOrder:        config.CorridorOrder,
		extraEdgeCount:       config.ExtraEdgeCount,
		maxRoomEdges:         config.MaxRoomEdges,
		roomRoleRequests:     roleRequests,
		densityRegions:       densityRegions,
		roomGeometry:         geometry,
		geometryCombinations: combinations,
		corridorWidths:       corridorWidths,
		plantCatalog:         catalog,
		terrain:              terrain,
	}, nil
}

// normalizeTerrainConfig validates every field before producing the detached,
// bytewise-canonical representation consumed by later generation phases.
func normalizeTerrainConfig(source *TerrainConfig) (*TerrainConfig, error) {
	if source == nil {
		return nil, nil
	}
	if len(source.Definitions) == 0 {
		return nil, fmt.Errorf("%w: TerrainConfig.Definitions must not be empty", ErrInvalidConfig)
	}
	if len(source.Definitions) > MaxTerrainKinds {
		return nil, fmt.Errorf("%w: TerrainConfig.Definitions has %d entries, above %d", ErrLimitExceeded, len(source.Definitions), MaxTerrainKinds)
	}

	definitionIDs := make(map[TerrainID]TerrainDefinition, len(source.Definitions))
	var definitionBytes uint64
	for _, definition := range source.Definitions {
		if definition.ID == "" || !utf8.ValidString(string(definition.ID)) {
			return nil, fmt.Errorf("%w: TerrainID must be non-empty UTF-8", ErrInvalidConfig)
		}
		if _, exists := definitionIDs[definition.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate TerrainID", ErrInvalidConfig)
		}
		if definitionBytes > math.MaxUint64-uint64(len(string(definition.ID))) {
			return nil, fmt.Errorf("%w: Terrain definition ID bytes overflow", ErrLimitExceeded)
		}
		definitionBytes += uint64(len(string(definition.ID)))
		if definitionBytes > uint64(MaxTerrainPaletteBytes) {
			return nil, fmt.Errorf("%w: Terrain definition IDs use %d bytes, above %d", ErrLimitExceeded, definitionBytes, MaxTerrainPaletteBytes)
		}
		definitionIDs[definition.ID] = definition
	}
	if source.Rooms == nil && source.Corridors == nil {
		return nil, fmt.Errorf("%w: TerrainConfig must declare Rooms or Corridors", ErrInvalidConfig)
	}
	if err := validateTerrainDistribution(source.Rooms, definitionIDs); err != nil {
		return nil, err
	}
	if err := validateTerrainDistribution(source.Corridors, definitionIDs); err != nil {
		return nil, err
	}

	canonical := &TerrainConfig{
		Definitions: append([]TerrainDefinition(nil), source.Definitions...),
	}
	sort.Slice(canonical.Definitions, func(first, second int) bool {
		return canonical.Definitions[first].ID < canonical.Definitions[second].ID
	})
	canonical.Rooms = cloneTerrainDistribution(source.Rooms)
	canonical.Corridors = cloneTerrainDistribution(source.Corridors)
	return canonical, nil
}

func validateTerrainDistribution(source *TerrainDistribution, definitions map[TerrainID]TerrainDefinition) error {
	if source == nil {
		return nil
	}
	if (source.MinPatchCells == 0) != (source.MaxPatchCells == 0) {
		return fmt.Errorf("%w: TerrainDistribution patch range must set both bounds", ErrInvalidConfig)
	}
	if source.MinPatchCells > source.MaxPatchCells {
		return fmt.Errorf("%w: TerrainDistribution patch minimum exceeds maximum", ErrInvalidConfig)
	}
	if source.MaxPatchCells > MaxCells {
		return fmt.Errorf("%w: TerrainDistribution patch maximum exceeds %d", ErrLimitExceeded, MaxCells)
	}
	seen := make(map[TerrainID]struct{}, len(source.Terrains))
	total := uint64(source.NoneWeight)
	for _, terrain := range source.Terrains {
		if terrain.TerrainID == "" || !utf8.ValidString(string(terrain.TerrainID)) {
			return fmt.Errorf("%w: TerrainID in weight must be non-empty UTF-8", ErrInvalidConfig)
		}
		_, exists := definitions[terrain.TerrainID]
		if !exists {
			return fmt.Errorf("%w: TerrainID %q is not declared", ErrInvalidConfig, terrain.TerrainID)
		}
		if _, exists := seen[terrain.TerrainID]; exists {
			return fmt.Errorf("%w: duplicate TerrainID in TerrainDistribution", ErrInvalidConfig)
		}
		if terrain.Weight == 0 {
			return fmt.Errorf("%w: Terrain weight must be positive", ErrInvalidConfig)
		}
		if total > math.MaxUint64-uint64(terrain.Weight) {
			return fmt.Errorf("%w: TerrainDistribution weight sum overflows uint64", ErrInvalidConfig)
		}
		total += uint64(terrain.Weight)
		seen[terrain.TerrainID] = struct{}{}
	}
	if total == 0 {
		return fmt.Errorf("%w: TerrainDistribution total weight must be positive", ErrInvalidConfig)
	}
	if source.NoneWeight == 0 {
		passable := false
		for terrainID := range seen {
			if definitions[terrainID].EntryCost != 0 {
				passable = true
				break
			}
		}
		if !passable {
			return fmt.Errorf("%w: TerrainDistribution has no passable candidate for the connectivity spine", ErrInvalidConfig)
		}
	}
	return nil
}

func cloneTerrainDistribution(source *TerrainDistribution) *TerrainDistribution {
	if source == nil {
		return nil
	}
	clone := &TerrainDistribution{
		NoneWeight:    source.NoneWeight,
		Terrains:      append([]TerrainWeight(nil), source.Terrains...),
		MinPatchCells: source.MinPatchCells,
		MaxPatchCells: source.MaxPatchCells,
	}
	if clone.MinPatchCells == 0 {
		clone.MinPatchCells = defaultMinTerrainPatchCells
		clone.MaxPatchCells = defaultMaxTerrainPatchCells
	}
	sort.Slice(clone.Terrains, func(first, second int) bool {
		return clone.Terrains[first].TerrainID < clone.Terrains[second].TerrainID
	})
	return clone
}

func normalizeGrid(width, height uint32) (uint32, uint32, error) {
	if width == 0 || height == 0 {
		return 0, 0, fmt.Errorf("%w: width and height are required", ErrInvalidConfig)
	}
	if width > maximumGridDimension || height > maximumGridDimension {
		return 0, 0, fmt.Errorf("%w: Grid dimension exceeds the v1 maximum", ErrLimitExceeded)
	}
	if uint64(width)*uint64(height) > uint64(MaxCells) {
		return 0, 0, fmt.Errorf("%w: Cell count exceeds the v1 maximum", ErrLimitExceeded)
	}
	return width, height, nil
}

func normalizeCellSize(value float64) (float64, error) {
	if value == 0 {
		value = defaultCellSize
	}
	if !isFinite(value) || value <= 0 {
		return 0, fmt.Errorf("%w: CellSize must be finite and positive", ErrInvalidConfig)
	}
	return value, nil
}

func normalizeMinDistance(value float64) (float64, error) {
	if value == 0 {
		value = defaultMinDistance
	}
	if !isFinite(value) || value < minimumMinDistance {
		return 0, fmt.Errorf("%w: MinDistance must be finite and at least one Cell", ErrInvalidConfig)
	}
	return value, nil
}

func normalizeMaxAttempts(value uint32) (uint32, error) {
	if value == 0 {
		value = defaultMaxAttempts
	}
	if value > maximumMaxAttempts {
		return 0, fmt.Errorf("%w: MaxAttempts exceeds the v1 maximum", ErrInvalidConfig)
	}
	return value, nil
}

func normalizeMaxRooms(value uint32) (uint32, error) {
	if value == 0 {
		value = MaxRooms
	}
	if value > MaxRooms {
		return 0, fmt.Errorf("%w: MaxRooms exceeds the v1 maximum", ErrLimitExceeded)
	}
	return value, nil
}

func validateExtraEdgeCount(count, maxRooms uint32) error {
	maximumEdges := uint64(maxRooms) * uint64(maxRooms-1) / 2
	if uint64(count) > maximumEdges {
		return fmt.Errorf("%w: ExtraEdgeCount exceeds the possible edges", ErrInvalidConfig)
	}
	return nil
}

// validateMaxRoomEdges rejects a ceiling of 1. A spanning tree over n Rooms
// needs n-1 edges, and one Corridor per Room spans exactly two Rooms, so any
// floor of three or more Rooms would fail later as ErrUnconnectablePlacement.
// Zero stays unlimited so an omitted field keeps today's Layout.
func validateMaxRoomEdges(count uint32) error {
	if count == 0 || count >= 2 {
		return nil
	}
	return fmt.Errorf(
		"%w: MaxRoomEdges of 1 lets each Room hold one Corridor, which spans exactly two Rooms, "+
			"so a floor of three or more Rooms cannot be connected; zero means unlimited",
		ErrInvalidConfig,
	)
}

// normalizeCorridorGeometry accepts a nil geometry as "every Corridor is one
// Cell wide" and returns a nil slice so routing never draws a width. A non-nil
// geometry must list widths in 1..64, duplicate-free, each with a positive
// weight, and must declare width 1. The returned slice is sorted by Width so
// the draw does not depend on the request order.
func normalizeCorridorGeometry(source *CorridorGeometry) ([]CorridorWidthWeight, error) {
	if source == nil {
		return nil, nil
	}
	if len(source.Widths) == 0 {
		return nil, fmt.Errorf("%w: CorridorGeometry.Widths must not be empty", ErrInvalidConfig)
	}
	weights := make([]CorridorWidthWeight, len(source.Widths))
	seen := make(map[uint32]struct{}, len(source.Widths))
	for index, item := range source.Widths {
		if item.Width < 1 || item.Width > maximumCorridorWidth {
			return nil, fmt.Errorf("%w: Corridor width must be from 1 to 64", ErrInvalidConfig)
		}
		if item.Weight == 0 {
			return nil, fmt.Errorf("%w: Corridor width weight must be positive", ErrInvalidConfig)
		}
		if _, exists := seen[item.Width]; exists {
			return nil, fmt.Errorf("%w: duplicate Corridor width", ErrInvalidConfig)
		}
		seen[item.Width] = struct{}{}
		weights[index] = item
	}
	// Degradation only walks declared widths, so a catalog without 1 leaves a
	// Room whose perimeter admits nothing wider with no routable edge at all.
	if _, exists := seen[1]; !exists {
		return nil, fmt.Errorf(
			"%w: CorridorGeometry.Widths must declare width 1, the fallback every Corridor can degrade to",
			ErrInvalidConfig,
		)
	}
	sort.Slice(weights, func(first, second int) bool {
		return weights[first].Width < weights[second].Width
	})
	return weights, nil
}

func normalizeRoomGeometry(source *RoomGeometry, gridWidth, gridHeight uint32) (RoomGeometry, []roomGeometryCombination, error) {
	if source == nil {
		geometry, combinations := applyDefaultRoomGeometry(gridWidth, gridHeight)
		return geometry, combinations, nil
	}
	return normalizeSuppliedRoomGeometry(source, gridWidth, gridHeight)
}

func applyDefaultRoomGeometry(gridWidth, gridHeight uint32) (RoomGeometry, []roomGeometryCombination) {
	return materializeShapeRanges(defaultRoomGeometry(gridWidth, gridHeight), gridWidth, gridHeight)
}

func normalizeSuppliedRoomGeometry(source *RoomGeometry, gridWidth, gridHeight uint32) (RoomGeometry, []roomGeometryCombination, error) {
	if err := validateRoomGeometryLimits(source); err != nil {
		return RoomGeometry{}, nil, err
	}
	weights, err := canonicalRoomShapeWeights(source.Shapes)
	if err != nil {
		return RoomGeometry{}, nil, err
	}
	for _, weight := range weights {
		if shapeRangeAdmits(weight.Shape, weight.Width, weight.Height) {
			continue
		}
		return RoomGeometry{}, nil, fmt.Errorf(
			"%w: %s width %d..%d height %d..%d admits no legal size",
			ErrInvalidConfig, roomShapeName(weight.Shape),
			weight.Width.Min, weight.Width.Max, weight.Height.Min, weight.Height.Max,
		)
	}
	geometry, combinations := materializeShapeRanges(RoomGeometry{
		MaxFootprintCells: source.MaxFootprintCells,
		MinRoomGap:        source.MinRoomGap,
		Shapes:            weights,
	}, gridWidth, gridHeight)
	if len(combinations) == 0 {
		return RoomGeometry{}, nil, fmt.Errorf("%w: geometry has no valid combination within the area", ErrInvalidConfig)
	}
	return geometry, combinations, nil
}

func validateRoomGeometryLimits(source *RoomGeometry) error {
	if source.MaxFootprintCells == 0 {
		return fmt.Errorf("%w: MaxFootprintCells is required", ErrInvalidConfig)
	}
	if source.MaxFootprintCells > MaxFootprintCells {
		return fmt.Errorf("%w: MaxFootprintCells exceeds the v1 maximum", ErrLimitExceeded)
	}
	if source.MinRoomGap > maximumMinRoomGap {
		return fmt.Errorf("%w: MinRoomGap exceeds the v1 maximum", ErrInvalidConfig)
	}
	if len(source.Shapes) == 0 {
		return fmt.Errorf("%w: Shapes must not be empty", ErrInvalidConfig)
	}
	return nil
}

// canonicalRoomShapeWeights returns weights in canonical Shape order. Each
// canonical shape scans the whole list, so an unknown shape or a zero weight
// fails on the first pass and a duplicate fails when that shape is reached.
func canonicalRoomShapeWeights(shapes []RoomShapeWeight) ([]RoomShapeWeight, error) {
	weights := make([]RoomShapeWeight, 0, len(shapes))
	var weightSum uint64
	for _, canonicalShape := range canonicalRoomShapes {
		weight, found, nextSum, err := takeCanonicalShape(shapes, canonicalShape, weightSum)
		if err != nil {
			return nil, err
		}
		weightSum = nextSum
		if found {
			weights = append(weights, weight)
		}
	}
	if len(weights) != len(shapes) {
		return nil, fmt.Errorf("%w: RoomShape duplicada ou desconhecida", ErrInvalidConfig)
	}
	return weights, nil
}

func takeCanonicalShape(shapes []RoomShapeWeight, canonical RoomShape, weightSum uint64) (RoomShapeWeight, bool, uint64, error) {
	var matched RoomShapeWeight
	found := false
	for _, shapeWeight := range shapes {
		if err := validateShapeWeight(shapeWeight); err != nil {
			return RoomShapeWeight{}, false, weightSum, err
		}
		if shapeWeight.Shape != canonical {
			continue
		}
		if found {
			return RoomShapeWeight{}, false, weightSum, fmt.Errorf("%w: RoomShape duplicada", ErrInvalidConfig)
		}
		found = true
		nextSum := weightSum + uint64(shapeWeight.Weight)
		if nextSum > uint64(math.MaxUint32) {
			return RoomShapeWeight{}, false, weightSum, fmt.Errorf("%w: soma dos pesos de RoomShape excede uint32", ErrInvalidConfig)
		}
		weightSum = nextSum
		matched = shapeWeight
	}
	return matched, found, weightSum, nil
}

func validateShapeWeight(shapeWeight RoomShapeWeight) error {
	if !validRoomShape(shapeWeight.Shape) {
		return fmt.Errorf("%w: RoomShape desconhecida", ErrInvalidConfig)
	}
	if shapeWeight.Weight == 0 {
		return fmt.Errorf("%w: RoomShape weight must be positive", ErrInvalidConfig)
	}
	if err := validateDimensionRange(shapeWeight.Width, "width"); err != nil {
		return err
	}
	if err := validateDimensionRange(shapeWeight.Height, "height"); err != nil {
		return err
	}
	return nil
}

// validateDimensionRange rejects an empty span. Max 0 and Min greater than Max
// are the same kind of failure: the caller named a range that contains nothing.
// A span that contains integers but no legal mask is a later, separate check.
func validateDimensionRange(span DimensionRange, axis string) error {
	if span.Max == 0 {
		return fmt.Errorf("%w: %s range maximum is 0", ErrInvalidConfig, axis)
	}
	if span.Min > span.Max {
		return fmt.Errorf("%w: %s range minimum exceeds maximum", ErrInvalidConfig, axis)
	}
	return nil
}

func defaultRoomGeometry(gridWidth, gridHeight uint32) RoomGeometry {
	shapes := make([]RoomShapeWeight, 0, len(dynamicShapeProfiles))
	for _, profile := range dynamicShapeProfiles {
		width, height, ok := DefaultDimensionRanges(profile.shape, gridWidth, gridHeight)
		if !ok {
			continue
		}
		shapes = append(shapes, RoomShapeWeight{
			Shape: profile.shape, Weight: profile.weight, Width: width, Height: height,
		})
	}
	return RoomGeometry{
		MaxFootprintCells: defaultMaxFootprintCells,
		MinRoomGap:        defaultMinRoomGap,
		Shapes:            shapes,
	}
}

// enumerateGeometryCombinations materializes, in canonical Shape, Width, and
// Height order, every dimension pair that produces a valid mask within the
// area. Each shape walks its own width and height span, and a size past the
// Grid is not a candidate. The geometry draw indexes this stable slice instead
// of iterating a map, which would make the generated Layout depend on Go's map
// ordering.
// In the worst permitted case (geometry 1..256 in both dimensions, five
// shapes), there are about 2.1×10^5 combinations, around 2.5 MB; this is the
// cost of detecting unsatisfiable geometry before any draw.
func enumerateGeometryCombinations(geometry RoomGeometry, gridWidth, gridHeight uint32) []roomGeometryCombination {
	combinations := make([]roomGeometryCombination, 0)
	for _, shape := range canonicalRoomShapes {
		weight, found := shapeWeightFor(geometry.Shapes, shape)
		if !found {
			continue
		}
		for width := weight.Width.Min; width <= weight.Width.Max && width <= gridWidth; width++ {
			for height := weight.Height.Min; height <= weight.Height.Max && height <= gridHeight; height++ {
				if !combinationIsAdmissible(shape, width, height, geometry.MaxFootprintCells) {
					continue
				}
				combinations = append(combinations, roomGeometryCombination{shape: shape, width: width, height: height})
			}
		}
	}
	return combinations
}

// combinationIsAdmissible reports whether the dimensions describe a valid
// canonical mask for the shape and whether the resulting footprint fits within
// the effective area limit. Both conditions decide the same thing — may this
// combination ever be drawn — so they belong together.
func combinationIsAdmissible(shape RoomShape, width, height, maxFootprintCells uint32) bool {
	if !ValidRoomShapeDimensions(shape, width, height) {
		return false
	}
	return roomShapeCellCount(shape, width, height) <= uint64(maxFootprintCells)
}

func roomShapeCellCount(shape RoomShape, width, height uint32) uint64 {
	switch shape {
	case RoomShapeRectangle:
		return uint64(width) * uint64(height)
	case RoomShapeL, RoomShapeT, RoomShapeCross:
		return uint64(width) + uint64(height) - 1
	case RoomShapeCircle:
		return uint64(len(RoomShapeOffsets(shape, width, height)))
	default:
		return 0
	}
}

func filterShapesWithCombinations(weights []RoomShapeWeight, combinations []roomGeometryCombination) []RoomShapeWeight {
	filtered := make([]RoomShapeWeight, 0, len(weights))
	for _, shape := range canonicalRoomShapes {
		if !containsCombination(combinations, shape) {
			continue
		}
		for _, weight := range weights {
			if weight.Shape == shape {
				filtered = append(filtered, weight)
				break
			}
		}
	}
	return filtered
}

type dynamicShapeProfile struct {
	shape     RoomShape
	weight    uint32
	minWidth  uint32
	maxWidth  uint32
	minHeight uint32
	maxHeight uint32
}

// dynamicShapeProfiles is the dynamic profile before the Grid clamp. Rectangle,
// L, T, and Cross share 3..9 because every integer in that span is a legal
// mask for them. Circle starts at 5 because smaller diameters are not circles.
var dynamicShapeProfiles = []dynamicShapeProfile{
	{RoomShapeRectangle, defaultRectangleWeight, defaultGeometryMinSize, defaultGeometryMaxSize, defaultGeometryMinSize, defaultGeometryMaxSize},
	{RoomShapeL, defaultLWeight, defaultGeometryMinSize, defaultGeometryMaxSize, defaultGeometryMinSize, defaultGeometryMaxSize},
	{RoomShapeT, defaultTWeight, defaultGeometryMinSize, defaultGeometryMaxSize, defaultGeometryMinSize, defaultGeometryMaxSize},
	{RoomShapeCross, defaultCrossWeight, defaultGeometryMinSize, defaultGeometryMaxSize, defaultGeometryMinSize, defaultGeometryMaxSize},
	{RoomShapeCircle, defaultCircleWeight, defaultCircleMinSize, defaultGeometryMaxSize, defaultCircleMinSize, defaultGeometryMaxSize},
}

// DefaultDimensionRanges is the width and height an omitted wire range means
// for shape. ok is false when shape is not a canonical mask. When the dynamic
// profile has a legal size on the grid, both ranges are that profile clamped
// with min(profile minimum, side)..min(profile maximum, side). When it does
// not, both ranges are the unclamped profile: legal for the mask, and left for
// validation to drop because the Grid cannot hold them. Omission never means
// the zero DimensionRange.
func DefaultDimensionRanges(shape RoomShape, gridWidth, gridHeight uint32) (width, height DimensionRange, ok bool) {
	profile, found := dynamicProfile(shape)
	if !found {
		return DimensionRange{}, DimensionRange{}, false
	}
	width = clampedProfileRange(profile.minWidth, profile.maxWidth, gridWidth)
	height = clampedProfileRange(profile.minHeight, profile.maxHeight, gridHeight)
	if shapeRangeAdmits(shape, width, height) {
		return width, height, true
	}
	return DimensionRange{Min: profile.minWidth, Max: profile.maxWidth},
		DimensionRange{Min: profile.minHeight, Max: profile.maxHeight}, true
}

func dynamicProfile(shape RoomShape) (dynamicShapeProfile, bool) {
	for _, profile := range dynamicShapeProfiles {
		if profile.shape == shape {
			return profile, true
		}
	}
	return dynamicShapeProfile{}, false
}

func clampedProfileRange(profileMin, profileMax, gridSide uint32) DimensionRange {
	return DimensionRange{
		Min: minimum(profileMin, gridSide),
		Max: minimum(profileMax, gridSide),
	}
}

func materializeShapeRanges(geometry RoomGeometry, gridWidth, gridHeight uint32) (RoomGeometry, []roomGeometryCombination) {
	fitted := make([]RoomShapeWeight, 0, len(geometry.Shapes))
	for _, weight := range geometry.Shapes {
		clamped, ok := fitShapeToGrid(weight, gridWidth, gridHeight)
		if !ok {
			continue
		}
		fitted = append(fitted, clamped)
	}
	geometry.Shapes = fitted
	combinations := enumerateGeometryCombinations(geometry, gridWidth, gridHeight)
	geometry.Shapes = filterShapesWithCombinations(geometry.Shapes, combinations)
	return geometry, combinations
}

// fitShapeToGrid keeps a shape whose span still contains a size the Grid can
// hold, with each maximum pulled down to the Grid. A span that starts past
// the Grid is dropped. That drop is not the "admits no legal size" error:
// the mask was legal, and only this Grid refuses it.
func fitShapeToGrid(weight RoomShapeWeight, gridWidth, gridHeight uint32) (RoomShapeWeight, bool) {
	width, widthFits := fitAxisToGrid(weight.Width, gridWidth)
	height, heightFits := fitAxisToGrid(weight.Height, gridHeight)
	if !widthFits || !heightFits {
		return RoomShapeWeight{}, false
	}
	weight.Width = width
	weight.Height = height
	return weight, true
}

func fitAxisToGrid(span DimensionRange, gridSide uint32) (DimensionRange, bool) {
	if span.Min > gridSide {
		return DimensionRange{}, false
	}
	if span.Max > gridSide {
		span.Max = gridSide
	}
	if span.Min > span.Max {
		return DimensionRange{}, false
	}
	return span, true
}

// shapeRangeAdmits reports whether the span contains at least one size
// ValidRoomShapeDimensions accepts for shape. It does not look at the Grid or
// the footprint. A well-formed range that fails here is a caller error. A
// range that passes here and then cannot sit on the Grid is dropped.
func shapeRangeAdmits(shape RoomShape, width, height DimensionRange) bool {
	switch shape {
	case RoomShapeRectangle:
		return axisAdmits(width, 1) && axisAdmits(height, 1)
	case RoomShapeL:
		return axisAdmits(width, roomShapeMinDimension) && axisAdmits(height, roomShapeMinDimension)
	case RoomShapeT:
		return axisAdmits(width, roomShapeMinWide) && axisAdmits(height, roomShapeMinDimension)
	case RoomShapeCross:
		return axisAdmits(width, roomShapeMinWide) && axisAdmits(height, roomShapeMinWide)
	case RoomShapeCircle:
		return circleRangeAdmits(width, height)
	default:
		return false
	}
}

func axisAdmits(span DimensionRange, minimumLegal uint32) bool {
	if span.Max == 0 || span.Min > span.Max {
		return false
	}
	low := span.Min
	if low < minimumLegal {
		low = minimumLegal
	}
	high := span.Max
	if high > roomShapeMaxCellCoord {
		high = roomShapeMaxCellCoord
	}
	return low <= high
}

func circleRangeAdmits(width, height DimensionRange) bool {
	if width.Max == 0 || height.Max == 0 || width.Min > width.Max || height.Min > height.Max {
		return false
	}
	low := width.Min
	if height.Min > low {
		low = height.Min
	}
	high := width.Max
	if height.Max < high {
		high = height.Max
	}
	if low < roomShapeMinCircle {
		low = roomShapeMinCircle
	}
	if high > roomShapeMaxCellCoord {
		high = roomShapeMaxCellCoord
	}
	if low > high {
		return false
	}
	if low%roomShapeParity == 0 {
		if low == math.MaxUint32 {
			return false
		}
		low++
	}
	return low <= high
}

func shapeWeightFor(weights []RoomShapeWeight, shape RoomShape) (RoomShapeWeight, bool) {
	for _, weight := range weights {
		if weight.Shape == shape {
			return weight, true
		}
	}
	return RoomShapeWeight{}, false
}

func roomShapeName(shape RoomShape) string {
	switch shape {
	case RoomShapeRectangle:
		return "rectangle"
	case RoomShapeL:
		return "L"
	case RoomShapeT:
		return "T"
	case RoomShapeCross:
		return "cross"
	case RoomShapeCircle:
		return "circle"
	default:
		return "shape"
	}
}

func validateRoomRoleRequests(requests []RoomRoleRequest, maxRooms uint32) ([]RoomRoleRequest, error) {
	validated := make([]RoomRoleRequest, 0, len(requests))
	seen := make(map[RoomRole]struct{}, len(requests))
	hasStart := false
	var assignedRooms uint64
	for _, request := range requests {
		if !validRoomRole(request.Role) {
			return nil, fmt.Errorf("%w: RoomRole desconhecido", ErrInvalidConfig)
		}
		if _, exists := seen[request.Role]; exists {
			return nil, fmt.Errorf("%w: RoomRole solicitado mais de uma vez", ErrInvalidConfig)
		}
		seen[request.Role] = struct{}{}
		if err := validateRoleCount(request.Role, request.Count, maxRooms); err != nil {
			return nil, err
		}
		if err := validateTags(request.RequiredTags); err != nil {
			return nil, fmt.Errorf("%w: required role tags are invalid", err)
		}
		if request.Role == RoomRoleStart {
			hasStart = true
		}
		assignedRooms += uint64(request.Count)
		validated = append(validated, RoomRoleRequest{
			Role: request.Role, Count: request.Count,
			RequiredTags: append([]string(nil), request.RequiredTags...),
		})
	}
	if err := validateRoleTotals(seen, hasStart, assignedRooms, maxRooms); err != nil {
		return nil, err
	}
	return validated, nil
}

// validateRoleCount requires Count 1 for Start and for Boss. Start is a single
// Room, RoomID 0, and Boss is the single farthest unassigned Room, so any other
// Count is unsatisfiable. A Room has at most one Role. Rejecting the Count here
// avoids discovering that after a stream has been consumed.
func validateRoleCount(role RoomRole, count, maxRooms uint32) error {
	if role == RoomRoleStart || role == RoomRoleBoss {
		if count != 1 {
			return fmt.Errorf("%w: Start e Boss exigem Count igual a um", ErrInvalidConfig)
		}
		return nil
	}
	if count > maxRooms {
		return fmt.Errorf("%w: Count de Treasure excede MaxRooms", ErrInvalidConfig)
	}
	return nil
}

// validateRoleTotals rejects a Boss request without Start, because distance
// without an origin is undefined, and a role total above MaxRooms. More roles
// than the Room limit can never be satisfied. Unsatisfiable requests are
// rejected before allocation or any draw.
func validateRoleTotals(seen map[RoomRole]struct{}, hasStart bool, assignedRooms uint64, maxRooms uint32) error {
	if _, hasBoss := seen[RoomRoleBoss]; hasBoss && !hasStart {
		return fmt.Errorf("%w: Boss requires a Start request", ErrInvalidConfig)
	}
	if assignedRooms > uint64(maxRooms) {
		return fmt.Errorf("%w: total role count exceeds MaxRooms", ErrInvalidConfig)
	}
	return nil
}

func validateDensityRegions(regions []DensityRegion, width, height uint32) ([]DensityRegion, error) {
	validated := append([]DensityRegion(nil), regions...)
	for index, region := range validated {
		if region.Min.X < 0 || region.Min.Y < 0 || region.Max.X <= region.Min.X || region.Max.Y <= region.Min.Y {
			return nil, fmt.Errorf("%w: DensityRegion must be a non-empty rectangle", ErrInvalidConfig)
		}
		if int64(region.Max.X) > int64(width) || int64(region.Max.Y) > int64(height) {
			return nil, fmt.Errorf("%w: DensityRegion is outside the Grid", ErrInvalidConfig)
		}
		if !isFinite(region.MinDistance) || region.MinDistance < minimumMinDistance {
			return nil, fmt.Errorf("%w: DensityRegion MinDistance must be finite and at least one Cell", ErrInvalidConfig)
		}
		for previous := 0; previous < index; previous++ {
			if densityRegionsOverlap(validated[previous], region) {
				return nil, fmt.Errorf("%w: DensityRegions must not overlap", ErrInvalidConfig)
			}
		}
	}
	return validated, nil
}

func validatePlantCatalog(source *PlantCatalog) (*PlantCatalog, error) {
	if source == nil {
		return nil, nil
	}
	if len(source.Rooms) == 0 || len(source.Corridors) == 0 {
		return nil, fmt.Errorf("%w: catalog must contain Room and Corridor Plants", ErrInvalidConfig)
	}

	catalog := &PlantCatalog{
		Rooms:     make([]RoomPlant, 0, len(source.Rooms)),
		Corridors: make([]CorridorPlant, 0, len(source.Corridors)),
	}
	roomIDs := make(map[PlantID]struct{}, len(source.Rooms))
	for _, plant := range source.Rooms {
		if err := validatePlantIdentity(plant.ID, plant.Weight, roomIDs); err != nil {
			return nil, err
		}
		if err := validateTags(plant.Tags); err != nil {
			return nil, fmt.Errorf("%w: RoomPlant tags are invalid", err)
		}
		if len(plant.DoorDirections) == 0 {
			return nil, fmt.Errorf("%w: RoomPlant must declare DoorDirections", ErrInvalidConfig)
		}
		seenDirections := make(map[Direction]struct{}, len(plant.DoorDirections))
		for _, direction := range plant.DoorDirections {
			if !validDirection(direction) {
				return nil, fmt.Errorf("%w: Direction de RoomPlant desconhecida", ErrInvalidConfig)
			}
			if _, exists := seenDirections[direction]; exists {
				return nil, fmt.Errorf("%w: Direction duplicada em RoomPlant", ErrInvalidConfig)
			}
			seenDirections[direction] = struct{}{}
		}
		catalog.Rooms = append(catalog.Rooms, RoomPlant{
			ID: plant.ID, Weight: plant.Weight,
			Tags:           append([]string(nil), plant.Tags...),
			DoorDirections: append([]Direction(nil), plant.DoorDirections...),
		})
	}

	corridorIDs := make(map[PlantID]struct{}, len(source.Corridors))
	for _, plant := range source.Corridors {
		if err := validatePlantIdentity(plant.ID, plant.Weight, corridorIDs); err != nil {
			return nil, err
		}
		if err := validateTags(plant.Tags); err != nil {
			return nil, fmt.Errorf("%w: CorridorPlant tags are invalid", err)
		}
		catalog.Corridors = append(catalog.Corridors, CorridorPlant{
			ID: plant.ID, Weight: plant.Weight,
			Tags: append([]string(nil), plant.Tags...),
		})
	}
	return catalog, nil
}

func validatePlantIdentity(id PlantID, weight uint32, seen map[PlantID]struct{}) error {
	if id == "" || !utf8.ValidString(string(id)) {
		return fmt.Errorf("%w: PlantID must be non-empty UTF-8", ErrInvalidConfig)
	}
	if _, exists := seen[id]; exists {
		return fmt.Errorf("%w: PlantID duplicado na lista", ErrInvalidConfig)
	}
	if weight == 0 {
		return fmt.Errorf("%w: Plant weight must be positive", ErrInvalidConfig)
	}
	seen[id] = struct{}{}
	return nil
}

func validateTags(tags []string) error {
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		if tag == "" || !utf8.ValidString(tag) {
			return fmt.Errorf("%w: tag must be non-empty UTF-8", ErrInvalidConfig)
		}
		if _, exists := seen[tag]; exists {
			return fmt.Errorf("%w: tag duplicada", ErrInvalidConfig)
		}
		seen[tag] = struct{}{}
	}
	return nil
}

func densityRegionsOverlap(first, second DensityRegion) bool {
	return first.Min.X < second.Max.X && second.Min.X < first.Max.X &&
		first.Min.Y < second.Max.Y && second.Min.Y < first.Max.Y
}

func containsCombination(combinations []roomGeometryCombination, shape RoomShape) bool {
	for _, combination := range combinations {
		if combination.shape == shape {
			return true
		}
	}
	return false
}

func validRoomShape(shape RoomShape) bool {
	return shape >= RoomShapeRectangle && shape <= RoomShapeCircle
}

func validCorridorOrder(order CorridorOrder) bool {
	return order == CorridorOrderXThenY || order == CorridorOrderYThenX
}

func validRoomRole(role RoomRole) bool {
	return role >= RoomRoleStart && role <= RoomRoleTreasure
}

func validDirection(direction Direction) bool {
	return direction >= DirectionNorth && direction <= DirectionWest
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func minimum(first, second uint32) uint32 {
	if first < second {
		return first
	}
	return second
}
