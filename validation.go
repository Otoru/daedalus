package daedalus

import (
	"fmt"
	"math"
	"unicode/utf8"
)

const (
	// Defaults e limites fixos da Config v1, definidos na seção 5.3.
	defaultCellSize    = 1.0
	defaultMinDistance = 6.0
	// Piso de MinDistance da seção 5.3, em Cells. É invariante próprio e
	// não deve ser confundido com defaultCellSize, que por acaso tem o
	// mesmo valor mas descreve outra grandeza.
	minimumMinDistance       = 1.0
	defaultMaxAttempts       = 30
	maximumMaxAttempts       = 1024
	maximumGridDimension     = 256
	maximumMinRoomGap        = 256
	defaultGeometryMinSize   = 3
	defaultGeometryMaxSize   = 9
	defaultMaxFootprintCells = 81
	defaultMinRoomGap        = 1

	// Pesos do perfil geométrico dinâmico padrão, em ordem canônica.
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

// roomGeometryCombination é uma combinação de máscara e bounding box já
// validada. A ordem canônica é Shape, Width e Height.
type roomGeometryCombination struct {
	shape  RoomShape
	width  uint32
	height uint32
}

// effectiveConfig concentra defaults e dados validados para que as fases de
// geração não precisem reinterpretar a Config pública.
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
	roomRoleRequests     []RoomRoleRequest
	densityRegions       []DensityRegion
	roomGeometry         RoomGeometry
	geometryCombinations []roomGeometryCombination
	plantCatalog         *PlantCatalog
}

// normalizeConfig valida toda a entrada antes de qualquer fase de geração e
// devolve uma cópia efetiva, com defaults aplicados e geometria enumerada.
func normalizeConfig(config Config) (effectiveConfig, error) {
	var effective effectiveConfig

	if config.Width == 0 || config.Height == 0 {
		return effective, fmt.Errorf("%w: largura e altura são obrigatórias", ErrInvalidConfig)
	}
	if config.Width > maximumGridDimension || config.Height > maximumGridDimension {
		return effective, fmt.Errorf("%w: dimensão do Grid excede o máximo v1", ErrLimitExceeded)
	}
	cellCount := uint64(config.Width) * uint64(config.Height)
	if cellCount > uint64(MaxCells) {
		return effective, fmt.Errorf("%w: quantidade de Cells excede o máximo v1", ErrLimitExceeded)
	}

	effective.width = config.Width
	effective.height = config.Height
	effective.seed = config.Seed

	effective.cellSize = config.CellSize
	if effective.cellSize == 0 {
		effective.cellSize = defaultCellSize
	}
	if !isFinite(effective.cellSize) || effective.cellSize <= 0 {
		return effectiveConfig{}, fmt.Errorf("%w: CellSize deve ser finito e positivo", ErrInvalidConfig)
	}

	effective.minDistance = config.MinDistance
	if effective.minDistance == 0 {
		effective.minDistance = defaultMinDistance
	}
	if !isFinite(effective.minDistance) || effective.minDistance < minimumMinDistance {
		return effectiveConfig{}, fmt.Errorf("%w: MinDistance deve ser finito e ao menos uma Cell", ErrInvalidConfig)
	}

	effective.maxAttempts = config.MaxAttempts
	if effective.maxAttempts == 0 {
		effective.maxAttempts = defaultMaxAttempts
	}
	if effective.maxAttempts > maximumMaxAttempts {
		return effectiveConfig{}, fmt.Errorf("%w: MaxAttempts excede o máximo v1", ErrInvalidConfig)
	}

	effective.maxRooms = config.MaxRooms
	if effective.maxRooms == 0 {
		effective.maxRooms = MaxRooms
	}
	if effective.maxRooms > MaxRooms {
		return effectiveConfig{}, fmt.Errorf("%w: MaxRooms excede o máximo v1", ErrLimitExceeded)
	}

	if !validCorridorOrder(config.CorridorOrder) {
		return effectiveConfig{}, fmt.Errorf("%w: CorridorOrder desconhecida", ErrInvalidConfig)
	}
	effective.corridorOrder = config.CorridorOrder
	effective.extraEdgeCount = config.ExtraEdgeCount
	maximumEdges := uint64(effective.maxRooms) * uint64(effective.maxRooms-1) / 2
	if uint64(effective.extraEdgeCount) > maximumEdges {
		return effectiveConfig{}, fmt.Errorf("%w: ExtraEdgeCount excede as arestas possíveis", ErrInvalidConfig)
	}

	roleRequests, err := validateRoomRoleRequests(config.RoomRoleRequests, effective.maxRooms)
	if err != nil {
		return effectiveConfig{}, err
	}
	effective.roomRoleRequests = roleRequests

	densityRegions, err := validateDensityRegions(config.DensityRegions, config.Width, config.Height)
	if err != nil {
		return effectiveConfig{}, err
	}
	effective.densityRegions = densityRegions

	geometry, combinations, err := normalizeRoomGeometry(config.RoomGeometry, config.Width, config.Height)
	if err != nil {
		return effectiveConfig{}, err
	}
	effective.roomGeometry = geometry
	effective.geometryCombinations = combinations

	catalog, err := validatePlantCatalog(config.PlantCatalog)
	if err != nil {
		return effectiveConfig{}, err
	}
	effective.plantCatalog = catalog

	return effective, nil
}

func normalizeRoomGeometry(source *RoomGeometry, gridWidth, gridHeight uint32) (RoomGeometry, []roomGeometryCombination, error) {
	if source == nil {
		geometry := defaultRoomGeometry(gridWidth, gridHeight)
		combinations := enumerateGeometryCombinations(geometry)
		geometry.Shapes = filterShapesWithCombinations(geometry.Shapes, combinations)
		return geometry, combinations, nil
	}

	geometry := RoomGeometry{
		MinWidth:          source.MinWidth,
		MaxWidth:          source.MaxWidth,
		MinHeight:         source.MinHeight,
		MaxHeight:         source.MaxHeight,
		MaxFootprintCells: source.MaxFootprintCells,
		MinRoomGap:        source.MinRoomGap,
	}
	if geometry.MinWidth == 0 || geometry.MinHeight == 0 {
		return RoomGeometry{}, nil, fmt.Errorf("%w: dimensões mínimas de Room são obrigatórias", ErrInvalidConfig)
	}
	if geometry.MaxWidth < geometry.MinWidth || geometry.MaxHeight < geometry.MinHeight {
		return RoomGeometry{}, nil, fmt.Errorf("%w: dimensões máximas de Room são menores que as mínimas", ErrInvalidConfig)
	}
	if geometry.MaxWidth > gridWidth || geometry.MaxHeight > gridHeight {
		return RoomGeometry{}, nil, fmt.Errorf("%w: dimensões de Room excedem o Grid", ErrInvalidConfig)
	}
	if geometry.MaxFootprintCells == 0 {
		return RoomGeometry{}, nil, fmt.Errorf("%w: MaxFootprintCells é obrigatório", ErrInvalidConfig)
	}
	if geometry.MaxFootprintCells > MaxFootprintCells {
		return RoomGeometry{}, nil, fmt.Errorf("%w: MaxFootprintCells excede o máximo v1", ErrLimitExceeded)
	}
	if geometry.MinRoomGap > maximumMinRoomGap {
		return RoomGeometry{}, nil, fmt.Errorf("%w: MinRoomGap excede o máximo v1", ErrInvalidConfig)
	}
	if len(source.Shapes) == 0 {
		return RoomGeometry{}, nil, fmt.Errorf("%w: Shapes não pode ser vazia", ErrInvalidConfig)
	}

	weights := make([]RoomShapeWeight, 0, len(source.Shapes))
	var weightSum uint64
	for _, canonicalShape := range canonicalRoomShapes {
		found := false
		for _, shapeWeight := range source.Shapes {
			if !validRoomShape(shapeWeight.Shape) {
				return RoomGeometry{}, nil, fmt.Errorf("%w: RoomShape desconhecida", ErrInvalidConfig)
			}
			if shapeWeight.Weight == 0 {
				return RoomGeometry{}, nil, fmt.Errorf("%w: peso de RoomShape deve ser positivo", ErrInvalidConfig)
			}
			if shapeWeight.Shape != canonicalShape {
				continue
			}
			if found {
				return RoomGeometry{}, nil, fmt.Errorf("%w: RoomShape duplicada", ErrInvalidConfig)
			}
			found = true
			weightSum += uint64(shapeWeight.Weight)
			if weightSum > uint64(math.MaxUint32) {
				return RoomGeometry{}, nil, fmt.Errorf("%w: soma dos pesos de RoomShape excede uint32", ErrInvalidConfig)
			}
			weights = append(weights, shapeWeight)
		}
	}
	if len(weights) != len(source.Shapes) {
		return RoomGeometry{}, nil, fmt.Errorf("%w: RoomShape duplicada ou desconhecida", ErrInvalidConfig)
	}
	geometry.Shapes = weights

	combinations := enumerateGeometryCombinations(geometry)
	if len(combinations) == 0 {
		return RoomGeometry{}, nil, fmt.Errorf("%w: geometria não possui combinação válida dentro da área", ErrInvalidConfig)
	}
	geometry.Shapes = filterShapesWithCombinations(geometry.Shapes, combinations)
	return geometry, combinations, nil
}

func defaultRoomGeometry(gridWidth, gridHeight uint32) RoomGeometry {
	return RoomGeometry{
		MinWidth:          minimum(defaultGeometryMinSize, gridWidth),
		MaxWidth:          minimum(defaultGeometryMaxSize, gridWidth),
		MinHeight:         minimum(defaultGeometryMinSize, gridHeight),
		MaxHeight:         minimum(defaultGeometryMaxSize, gridHeight),
		MaxFootprintCells: defaultMaxFootprintCells,
		MinRoomGap:        defaultMinRoomGap,
		Shapes: []RoomShapeWeight{
			{Shape: RoomShapeRectangle, Weight: defaultRectangleWeight},
			{Shape: RoomShapeL, Weight: defaultLWeight},
			{Shape: RoomShapeT, Weight: defaultTWeight},
			{Shape: RoomShapeCross, Weight: defaultCrossWeight},
			{Shape: RoomShapeCircle, Weight: defaultCircleWeight},
		},
	}
}

// enumerateGeometryCombinations materializa, em ordem canônica de Shape,
// Width e Height, todos os pares de dimensões que produzem máscara válida
// dentro da área. A seção 7 exige essa lista ordenada para que SampleGeometry
// sorteie sem iteração de map. No pior caso permitido (geometria 1..256 nas
// duas dimensões, cinco formas) são cerca de 2,1×10^5 combinações, algo em
// torno de 2,5 MB; é o preço de detectar geometria insatisfazível antes de
// qualquer sorteio.
func enumerateGeometryCombinations(geometry RoomGeometry) []roomGeometryCombination {
	combinations := make([]roomGeometryCombination, 0)
	for _, shape := range canonicalRoomShapes {
		if !containsShape(geometry.Shapes, shape) {
			continue
		}
		for width := geometry.MinWidth; width <= geometry.MaxWidth; width++ {
			for height := geometry.MinHeight; height <= geometry.MaxHeight; height++ {
				if !ValidRoomShapeDimensions(shape, width, height) {
					continue
				}
				if roomShapeCellCount(shape, width, height) > uint64(geometry.MaxFootprintCells) {
					continue
				}
				combinations = append(combinations, roomGeometryCombination{shape: shape, width: width, height: height})
			}
		}
	}
	return combinations
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
		// A seção 8.1 descreve Start e Boss no singular ("ela recebe
		// RoomID 0", "a Room não atribuída mais distante"), e a seção 5.2
		// proíbe mais de um Role por Room em v1. A spec não escreve a
		// restrição de Count, mas qualquer outro valor seria
		// insatisfazível; rejeitar aqui evita descobrir isso depois do
		// consumo de RNG.
		if request.Role == RoomRoleStart || request.Role == RoomRoleBoss {
			if request.Count != 1 {
				return nil, fmt.Errorf("%w: Start e Boss exigem Count igual a um", ErrInvalidConfig)
			}
		} else if request.Count > maxRooms {
			return nil, fmt.Errorf("%w: Count de Treasure excede MaxRooms", ErrInvalidConfig)
		}
		if err := validateTags(request.RequiredTags); err != nil {
			return nil, fmt.Errorf("%w: tags requeridas do papel são inválidas", err)
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
	if _, hasBoss := seen[RoomRoleBoss]; hasBoss && !hasStart {
		return nil, fmt.Errorf("%w: Boss exige uma solicitação Start", ErrInvalidConfig)
	}
	// Regra conservadora, também não literal na spec: mais papéis do que o
	// teto de Rooms nunca pode ser satisfeito, e a seção 5.3 exige detectar
	// geometria insatisfazível antes de alocar ou sortear.
	if assignedRooms > uint64(maxRooms) {
		return nil, fmt.Errorf("%w: quantidade total de papéis excede MaxRooms", ErrInvalidConfig)
	}
	return validated, nil
}

func validateDensityRegions(regions []DensityRegion, width, height uint32) ([]DensityRegion, error) {
	validated := append([]DensityRegion(nil), regions...)
	for index, region := range validated {
		if region.Min.X < 0 || region.Min.Y < 0 || region.Max.X <= region.Min.X || region.Max.Y <= region.Min.Y {
			return nil, fmt.Errorf("%w: DensityRegion deve ser um retângulo não vazio", ErrInvalidConfig)
		}
		if int64(region.Max.X) > int64(width) || int64(region.Max.Y) > int64(height) {
			return nil, fmt.Errorf("%w: DensityRegion está fora do Grid", ErrInvalidConfig)
		}
		if !isFinite(region.MinDistance) || region.MinDistance < minimumMinDistance {
			return nil, fmt.Errorf("%w: MinDistance de DensityRegion deve ser finita e ao menos uma Cell", ErrInvalidConfig)
		}
		for previous := 0; previous < index; previous++ {
			if densityRegionsOverlap(validated[previous], region) {
				return nil, fmt.Errorf("%w: DensityRegions não podem se sobrepor", ErrInvalidConfig)
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
		return nil, fmt.Errorf("%w: catálogo deve conter Plants de Room e Corridor", ErrInvalidConfig)
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
			return nil, fmt.Errorf("%w: tags de RoomPlant são inválidas", err)
		}
		if len(plant.DoorDirections) == 0 {
			return nil, fmt.Errorf("%w: RoomPlant deve declarar DoorDirections", ErrInvalidConfig)
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
			return nil, fmt.Errorf("%w: tags de CorridorPlant são inválidas", err)
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
		return fmt.Errorf("%w: PlantID deve ser UTF-8 não vazio", ErrInvalidConfig)
	}
	if _, exists := seen[id]; exists {
		return fmt.Errorf("%w: PlantID duplicado na lista", ErrInvalidConfig)
	}
	if weight == 0 {
		return fmt.Errorf("%w: peso de Plant deve ser positivo", ErrInvalidConfig)
	}
	seen[id] = struct{}{}
	return nil
}

func validateTags(tags []string) error {
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		if tag == "" || !utf8.ValidString(tag) {
			return fmt.Errorf("%w: tag deve ser UTF-8 não vazia", ErrInvalidConfig)
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

func containsShape(weights []RoomShapeWeight, shape RoomShape) bool {
	for _, weight := range weights {
		if weight.Shape == shape {
			return true
		}
	}
	return false
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
