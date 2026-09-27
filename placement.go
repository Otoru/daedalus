package daedalus

import "errors"

var (
	errPlacementInvalidMask  = errors.New("placement não coincide com a máscara canônica")
	errPlacementOutOfBounds  = errors.New("placement está fora dos limites do Grid")
	errPlacementAreaExceeded = errors.New("placement excede a área máxima efetiva")
	errPlacementDistance     = errors.New("âncora do placement não respeita a distância mínima")
	errPlacementOverlap      = errors.New("placement sobrepõe um footprint aceito")
	errPlacementGap          = errors.New("placement não respeita o espaçamento mínimo entre Rooms")
)

const (
	// Limites da representação pública de coordenadas em Cell.
	minCellCoordinate int64 = -1 << 31
	maxCellCoordinate int64 = 1<<31 - 1
	// A grade codifica owner+1 para reservar zero como Cell vazia.
	placementOwnerEncodingOffset uint32 = 1
	// unownedPlacementOwner nunca identifica uma Room aceita; durante a
	// validação, o candidato ainda não foi marcado na grade de ocupação.
	unownedPlacementOwner = ^uint32(0)
)

// acceptedPlacement é uma Room já aceita, com âncora e footprint absoluto
// materializados uma única vez pela solicitação que executa o Placer.
type acceptedPlacement struct {
	anchor    Cell
	footprint []Cell
}

// placementOccupancy mantém, por solicitação, o índice da Room que ocupa
// cada Cell. O índice é codificado com deslocamento para distinguir a Room
// zero de uma Cell vazia.
type placementOccupancy struct {
	width  int64
	height int64
	owners []uint32
}

func newPlacementOccupancy(width, height uint32) *placementOccupancy {
	cellCount := int64(width) * int64(height)
	return &placementOccupancy{
		width:  int64(width),
		height: int64(height),
		owners: make([]uint32, int(cellCount)),
	}
}

func (occupancy *placementOccupancy) mark(owner uint32, footprint []Cell) {
	encodedOwner := owner + placementOwnerEncodingOffset
	for _, cell := range footprint {
		index, ok := occupancy.index(cell)
		if ok {
			occupancy.owners[index] = encodedOwner
		}
	}
}

func (occupancy *placementOccupancy) index(cell Cell) (int, bool) {
	x := int64(cell.X)
	y := int64(cell.Y)
	if x < 0 || x >= occupancy.width || y < 0 || y >= occupancy.height {
		return 0, false
	}
	rowStart := y * occupancy.width
	index := rowStart + x
	return int(index), true
}

func (occupancy *placementOccupancy) ownerAt(cell Cell) (uint32, bool) {
	index, ok := occupancy.index(cell)
	if !ok {
		return 0, false
	}
	encodedOwner := occupancy.owners[index]
	if encodedOwner == 0 {
		return 0, false
	}
	return encodedOwner - placementOwnerEncodingOffset, true
}

func (occupancy *placementOccupancy) footprintOverlaps(footprint []Cell) bool {
	for _, cell := range footprint {
		if _, occupied := occupancy.ownerAt(cell); occupied {
			return true
		}
	}
	return false
}

func (occupancy *placementOccupancy) footprintRespectsGap(
	footprint []Cell,
	minRoomGap uint32,
	candidateOwner uint32,
) bool {
	radius := int64(minRoomGap)
	for _, cell := range footprint {
		minimumX := int64(cell.X) - radius
		if minimumX < 0 {
			minimumX = 0
		}
		maximumX := int64(cell.X) + radius
		if maximumX >= occupancy.width {
			maximumX = occupancy.width - 1
		}
		minimumY := int64(cell.Y) - radius
		if minimumY < 0 {
			minimumY = 0
		}
		maximumY := int64(cell.Y) + radius
		if maximumY >= occupancy.height {
			maximumY = occupancy.height - 1
		}

		for y := minimumY; y <= maximumY; y++ {
			rowStart := y * occupancy.width
			for x := minimumX; x <= maximumX; x++ {
				index := rowStart + x
				encodedOwner := occupancy.owners[int(index)]
				if encodedOwner == 0 {
					continue
				}
				owner := encodedOwner - placementOwnerEncodingOffset
				if owner != candidateOwner {
					return false
				}
			}
		}
	}
	return true
}

// buildPlacementFromAt deriva a bounding box e a máscara local a partir da
// primeira Cell ocupada em ordem canônica. O caminho de geração fornece uma
// âncora não negativa e dimensões limitadas pelo Grid v1; por isso a subtração
// feita em int64 sempre cabe em Cell, inclusive quando a Origin é negativa.
func buildPlacementFromAt(at Cell, shape RoomShape, width, height uint32) RoomPlacement {
	return buildPlacementFromAtInto(at, shape, width, height, nil)
}

// buildPlacementFromAtInto deriva o placement no scratch privado informado.
// O resultado precisa ser copiado antes da próxima reutilização do buffer.
func buildPlacementFromAtInto(
	at Cell,
	shape RoomShape,
	width, height uint32,
	scratch []Cell,
) RoomPlacement {
	// A especificação só define esta derivação para dimensões válidas. Na
	// leitura conservadora, dimensões inválidas preservam os dados recebidos
	// e seguem o contrato dos helpers canônicos: offset zero e máscara nil;
	// a primeira etapa da validação rejeita o resultado.
	firstOffset := RoomShapeFirstOffset(shape, width, height)
	originX := int64(at.X) - int64(firstOffset.X)
	originY := int64(at.Y) - int64(firstOffset.Y)

	return RoomPlacement{
		Shape:  shape,
		Origin: Cell{X: int32(originX), Y: int32(originY)},
		Width:  width,
		Height: height,
		Cells:  roomShapeOffsetsInto(shape, width, height, scratch),
	}
}

// absoluteFootprint materializa os offsets locais sem alterar sua ordem
// canônica. false indica que ao menos uma soma não cabe na representação de
// Cell; placements dentro do Grid v1 nunca alcançam esse caso.
func absoluteFootprint(placement RoomPlacement) ([]Cell, bool) {
	return absoluteFootprintInto(placement, nil)
}

// absoluteFootprintInto materializa os offsets no buffer privado da
// solicitação. O chamador deve copiar o resultado antes de reutilizar o
// buffer quando o footprint precisar sobreviver à tentativa atual.
func absoluteFootprintInto(placement RoomPlacement, buffer []Cell) ([]Cell, bool) {
	cellCount := len(placement.Cells)
	if cap(buffer) < cellCount {
		buffer = make([]Cell, cellCount)
	} else {
		buffer = buffer[:cellCount]
	}
	for index, offset := range placement.Cells {
		x := int64(placement.Origin.X) + int64(offset.X)
		y := int64(placement.Origin.Y) + int64(offset.Y)
		if x < minCellCoordinate || x > maxCellCoordinate || y < minCellCoordinate || y > maxCellCoordinate {
			return nil, false
		}
		buffer[index] = Cell{X: int32(x), Y: int32(y)}
	}
	return buffer, true
}

func placementHasCanonicalMask(placement RoomPlacement) bool {
	if !ValidRoomShapeDimensions(placement.Shape, placement.Width, placement.Height) {
		return false
	}
	centerX := int64(placement.Width-1) / int64(roomShapeParity)
	centerY := int64(placement.Height-1) / int64(roomShapeParity)
	radiusSquared := centerX * centerX
	cellIndex := 0
	for y := int64(0); y < int64(placement.Height); y++ {
		for x := int64(0); x < int64(placement.Width); x++ {
			if !roomShapeContains(placement.Shape, x, y, centerX, centerY, radiusSquared) {
				continue
			}
			if cellIndex >= len(placement.Cells) || placement.Cells[cellIndex] != (Cell{X: int32(x), Y: int32(y)}) {
				return false
			}
			cellIndex++
		}
	}
	return cellIndex == len(placement.Cells)
}

func placementWithinBounds(placement RoomPlacement, gridWidth, gridHeight uint32) bool {
	originX := int64(placement.Origin.X)
	originY := int64(placement.Origin.Y)
	if originX < 0 || originY < 0 {
		return false
	}
	maxX := originX + int64(placement.Width)
	maxY := originY + int64(placement.Height)
	return maxX <= int64(gridWidth) && maxY <= int64(gridHeight)
}

func placementWithinArea(placement RoomPlacement, maxFootprintCells uint32) bool {
	return uint64(len(placement.Cells)) <= uint64(maxFootprintCells)
}

// footprintsOverlap e footprintsRespectGap são a tradução literal da regra da
// seção 7, comparando par a par. O caminho de produção usa a grade de
// ocupação, que é muito mais barata; estas duas permanecem como oráculo de
// referência nos testes de equivalência. Não as remova por parecerem código
// morto: é contra elas que a otimização é verificada.
func footprintsOverlap(first, second []Cell) bool {
	occupied := make(map[Cell]struct{}, len(first))
	for _, cell := range first {
		occupied[cell] = struct{}{}
	}
	for _, cell := range second {
		if _, exists := occupied[cell]; exists {
			return true
		}
	}
	return false
}

func footprintsRespectGap(first, second []Cell, minRoomGap uint32) bool {
	requiredGap := int64(minRoomGap)
	for _, firstCell := range first {
		for _, secondCell := range second {
			deltaX := absInt64(int64(firstCell.X) - int64(secondCell.X))
			deltaY := absInt64(int64(firstCell.Y) - int64(secondCell.Y))
			chebyshevDistance := deltaX
			if deltaY > chebyshevDistance {
				chebyshevDistance = deltaY
			}
			if chebyshevDistance <= requiredGap {
				return false
			}
		}
	}
	return true
}

func localMinDistance(cell Cell, defaultDistance float64, regions []DensityRegion) float64 {
	for _, region := range regions {
		// A convenção congelada no F02 é semiaberta: Min inclusiva e Max
		// exclusiva, conforme a documentação pública de DensityRegion.
		if cell.X >= region.Min.X && cell.X < region.Max.X && cell.Y >= region.Min.Y && cell.Y < region.Max.Y {
			return region.MinDistance
		}
	}
	return defaultDistance
}

func anchorsRespectDistance(first, second Cell, defaultDistance float64, regions []DensityRegion) bool {
	deltaX := int64(first.X) - int64(second.X)
	deltaY := int64(first.Y) - int64(second.Y)
	deltaXSquared := deltaX * deltaX
	deltaYSquared := deltaY * deltaY
	squaredDistance := deltaXSquared + deltaYSquared

	requiredDistance := defaultDistance
	if len(regions) > 0 {
		requiredDistance = localMinDistance(first, defaultDistance, regions)
		secondDistance := localMinDistance(second, defaultDistance, regions)
		if secondDistance > requiredDistance {
			requiredDistance = secondDistance
		}
	}
	requiredSquared := float64(requiredDistance) * float64(requiredDistance)

	// Nos Grids v1, cada delta é no máximo 255 e squaredDistance no máximo
	// 130.050, muito abaixo de 2^53. A conversão para float64 é portanto
	// exata e permite comparar com o quadrado da distância configurável sem
	// arredondar a distância discreta.
	return float64(squaredDistance) >= requiredSquared
}

// validatePlacementForAcceptance concentra a ordem normativa da seção 7.
// Ela apenas lê accepted; o chamador efetua o append depois de nil, mantendo
// atômica toda tentativa rejeitada.
func validatePlacementForAcceptance(
	candidate RoomPlacement,
	gridWidth, gridHeight, maxFootprintCells, minRoomGap uint32,
	minDistance float64,
	densityRegions []DensityRegion,
	accepted []acceptedPlacement,
	occupancy *placementOccupancy,
) error {
	_, err := validatePlacementAndMaterialize(
		candidate,
		gridWidth,
		gridHeight,
		maxFootprintCells,
		minRoomGap,
		minDistance,
		densityRegions,
		accepted,
		occupancy,
	)
	return err
}

// validatePlacementAndMaterialize devolve o footprint absoluto que deve ser
// guardado em acceptedPlacement quando a validação tem sucesso. Assim o
// caminho de aceitação não recompõe uma Room já validada.
func validatePlacementAndMaterialize(
	candidate RoomPlacement,
	gridWidth, gridHeight, maxFootprintCells, minRoomGap uint32,
	minDistance float64,
	densityRegions []DensityRegion,
	accepted []acceptedPlacement,
	occupancy *placementOccupancy,
) ([]Cell, error) {
	footprint, err := validatePlacementAndMaterializeInto(
		candidate,
		gridWidth,
		gridHeight,
		maxFootprintCells,
		minRoomGap,
		minDistance,
		densityRegions,
		accepted,
		occupancy,
		nil,
	)
	if err != nil {
		return nil, err
	}
	return footprint, nil
}

// validatePlacementAndMaterializeInto mantém a ordem normativa de validação,
// mas materializa no scratch privado da solicitação. Em erro posterior à
// materialização, devolve o scratch para que o laço Poisson possa reutilizá-lo.
func validatePlacementAndMaterializeInto(
	candidate RoomPlacement,
	gridWidth, gridHeight, maxFootprintCells, minRoomGap uint32,
	minDistance float64,
	densityRegions []DensityRegion,
	accepted []acceptedPlacement,
	occupancy *placementOccupancy,
	scratch []Cell,
) ([]Cell, error) {
	if !placementHasCanonicalMask(candidate) {
		return scratch[:0], errPlacementInvalidMask
	}
	if !placementWithinBounds(candidate, gridWidth, gridHeight) {
		return scratch[:0], errPlacementOutOfBounds
	}
	if !placementWithinArea(candidate, maxFootprintCells) {
		return scratch[:0], errPlacementAreaExceeded
	}

	candidateFootprint, ok := absoluteFootprintInto(candidate, scratch[:0])
	if !ok {
		return scratch[:0], errPlacementOutOfBounds
	}
	// Sem a grade, sobreposição e gap não teriam como ser verificados e a
	// sequência normativa ficaria incompleta em silêncio. Chamar com Rooms
	// aceitas e sem grade é erro de programação, não entrada de usuário.
	if len(accepted) > 0 && occupancy == nil {
		panic("grade de ocupação ausente com placements já aceitos")
	}

	candidateAt := candidateFootprint[0]
	for _, placement := range accepted {
		if !anchorsRespectDistance(candidateAt, placement.anchor, minDistance, densityRegions) {
			return candidateFootprint, errPlacementDistance
		}
	}

	if occupancy != nil && occupancy.footprintOverlaps(candidateFootprint) {
		return candidateFootprint, errPlacementOverlap
	}
	// O candidato ainda não possui owner. O sentinela evita confundi-lo com
	// qualquer Room, inclusive quando accepted contém só vizinhos Bridson.
	if occupancy != nil && !occupancy.footprintRespectsGap(candidateFootprint, minRoomGap, unownedPlacementOwner) {
		return candidateFootprint, errPlacementGap
	}
	return candidateFootprint, nil
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
