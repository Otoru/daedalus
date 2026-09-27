package daedalus

import (
	"context"
	"errors"
	"math"
)

var errPlacementRequestNotNormalized = errors.New("solicitação de placement não está normalizada")

const (
	// roomShapeCount é a quantidade de formas contíguas do contrato v1.
	roomShapeCount = int(RoomShapeCircle) + 1
	// annulusSquareWidthFactor e annulusSquareHalfFactor descrevem o
	// quadrado [-2r,2r] que circunscreve o anel.
	annulusSquareWidthFactor = 4.0
	annulusSquareHalfFactor  = 2.0
	// uniformAccelerationRange cobre a vizinhança 5×5 da grade Bridson
	// quando não existem DensityRegions.
	uniformAccelerationRange = 2
	// densityAccelerationMargin é a margem prescrita pela seção 7 para
	// o alcance variável da grade Bridson.
	densityAccelerationMargin = 1
	// bridsonDimensions é o divisor sqrt(2) do lado de célula em duas
	// dimensões, conforme a seção 7.
	bridsonDimensions = 2.0
	// cancellationCandidateInterval limita o intervalo entre consultas de
	// Context durante propostas candidatas.
	cancellationCandidateInterval uint64 = 256
)

// poissonDiskRoomsPlacer implementa o algoritmo embutido e congelado
// poisson_disk_rooms_v1. O tipo não possui estado: todos os buffers e streams
// pertencem a uma chamada de Place.
type poissonDiskRoomsPlacer struct{}

var _ Placer = poissonDiskRoomsPlacer{}

// Place propõe Rooms segundo a seção 7 da especificação.
func (poissonDiskRoomsPlacer) Place(req PlacementRequest) ([]RoomPlacement, error) {
	ctx := req.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(req.geometryCombinations) == 0 {
		return nil, errPlacementRequestNotNormalized
	}

	streams := newRNGStreams(req.Seed)
	geometrySampler := newRoomGeometrySampler(req.RoomGeometry, req.geometryCombinations)
	geometry, ok := geometrySampler.sample(&streams.roomGeometry)
	if !ok {
		return nil, errPlacementRequestNotNormalized
	}
	first, ok := nearestPlacementToGridCenter(req.Width, req.Height, geometry)
	if !ok {
		return nil, errPlacementRequestNotNormalized
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	firstFootprint, ok := absoluteFootprint(first)
	if !ok || len(firstFootprint) == 0 {
		return nil, errPlacementRequestNotNormalized
	}
	accepted := make([]acceptedPlacement, 0, req.MaxRooms)
	accepted = append(accepted, acceptedPlacement{anchor: firstFootprint[0], footprint: firstFootprint})
	placements := make([]RoomPlacement, 0, req.MaxRooms)
	placements = append(placements, first)
	active := make([]Cell, 0, req.MaxRooms)
	active = append(active, firstFootprint[0])
	nearbyBuffer := make([]acceptedPlacement, 0, req.MaxRooms)
	localOffsetsScratch := make([]Cell, 0, req.RoomGeometry.MaxFootprintCells)
	footprintScratch := make([]Cell, 0, req.RoomGeometry.MaxFootprintCells)
	occupancy := newPlacementOccupancy(req.Width, req.Height)
	occupancy.mark(0, firstFootprint)
	acceleration := newAnchorAccelerationGrid(req)
	acceleration.insert(firstFootprint[0], 0)
	hasDensityRegions := len(req.DensityRegions) > 0
	var candidateAttempts uint64

	for len(active) > 0 && uint32(len(accepted)) < req.MaxRooms {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		activeIndex := int(streams.placement.uniformInt(0, uint64(len(active)-1)))
		base := active[activeIndex]
		acceptedOne := false
		for attempt := uint32(0); attempt < req.MaxAttempts; attempt++ {
			if candidateAttempts%cancellationCandidateInterval == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			candidateAttempts++

			radius := req.MinDistance
			if hasDensityRegions {
				radius = localMinDistance(base, req.MinDistance, req.DensityRegions)
			}
			offsetX, offsetY := sampleUniformAnnulusByRejection(&streams.placement, radius)
			baseX := float64(base.X)
			baseY := float64(base.Y)
			continuousX := baseX + offsetX
			continuousY := baseY + offsetY
			anchorX := math.Floor(continuousX)
			anchorY := math.Floor(continuousY)
			if anchorX < 0 || anchorX >= float64(req.Width) || anchorY < 0 || anchorY >= float64(req.Height) {
				continue
			}
			anchor := Cell{X: int32(anchorX), Y: int32(anchorY)}

			geometry, sampled := geometrySampler.sample(&streams.roomGeometry)
			if !sampled {
				return nil, errPlacementRequestNotNormalized
			}
			candidate := buildPlacementFromAtInto(
				anchor,
				geometry.shape,
				geometry.width,
				geometry.height,
				localOffsetsScratch[:0],
			)
			localOffsetsScratch = candidate.Cells
			nearby := acceleration.nearbyInto(anchor, accepted, nearbyBuffer[:0])
			var validationErr error
			footprintScratch, validationErr = validatePlacementAndMaterializeInto(
				candidate,
				req.Width,
				req.Height,
				req.RoomGeometry.MaxFootprintCells,
				req.RoomGeometry.MinRoomGap,
				req.MinDistance,
				req.DensityRegions,
				nearby,
				occupancy,
				footprintScratch[:0],
			)
			if validationErr != nil {
				continue
			}

			owner := uint32(len(accepted))
			acceptedFootprint := append([]Cell(nil), footprintScratch...)
			accepted = append(accepted, acceptedPlacement{anchor: anchor, footprint: acceptedFootprint})
			acceptedCandidate := candidate
			acceptedCandidate.Cells = append([]Cell(nil), candidate.Cells...)
			placements = append(placements, acceptedCandidate)
			occupancy.mark(owner, footprintScratch)
			acceleration.insert(anchor, int(owner))
			active = append(active, anchor)
			acceptedOne = true
			break
		}
		if !acceptedOne {
			active = append(active[:activeIndex], active[activeIndex+1:]...)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return placements, nil
}

type roomGeometrySampler struct {
	geometry     RoomGeometry
	combinations []roomGeometryCombination
	starts       [roomShapeCount]int
	counts       [roomShapeCount]int
}

func newRoomGeometrySampler(geometry RoomGeometry, combinations []roomGeometryCombination) roomGeometrySampler {
	sampler := roomGeometrySampler{geometry: geometry, combinations: combinations}
	for shape := range sampler.starts {
		sampler.starts[shape] = -1
	}
	for index, combination := range combinations {
		shape := int(combination.shape)
		if shape < 0 || shape >= roomShapeCount {
			continue
		}
		if sampler.starts[shape] < 0 {
			sampler.starts[shape] = index
		}
		sampler.counts[shape]++
	}
	return sampler
}

func (sampler roomGeometrySampler) sample(stream *splitMix64) (roomGeometryCombination, bool) {
	var weightSum uint64
	for _, shapeWeight := range sampler.geometry.Shapes {
		weightSum += uint64(shapeWeight.Weight)
	}
	if weightSum == 0 {
		return roomGeometryCombination{}, false
	}

	draw := stream.uniformInt(1, weightSum)
	var cumulative uint64
	selectedShape := RoomShapeRectangle
	foundShape := false
	for _, shapeWeight := range sampler.geometry.Shapes {
		cumulative += uint64(shapeWeight.Weight)
		if draw <= cumulative {
			selectedShape = shapeWeight.Shape
			foundShape = true
			break
		}
	}
	if !foundShape {
		return roomGeometryCombination{}, false
	}

	shape := int(selectedShape)
	first := sampler.starts[shape]
	count := sampler.counts[shape]
	if count == 0 {
		return roomGeometryCombination{}, false
	}
	selected := stream.uniformInt(0, uint64(count-1))
	return sampler.combinations[first+int(selected)], true
}

// sampleUniformAnnulusByRejection traduz SampleUniformAnnulusByRejection da
// seção 7. A spec não escreve a transformação de uniform01 para o
// quadrado; a interpretação congelada em v1 é u*4r-2r em cada eixo.
// uniform01 pertence a [0,1), coerente com o limite externo exclusivo do
// anel; o limite interno é inclusivo. Também por interpretação da seção 7,
// pares fora do anel são rejeitados internamente, sem consumir uma tentativa
// geométrica, como indica o nome do pseudocódigo e a lista normativa de
// motivos de rejeição de propostas.
func sampleUniformAnnulusByRejection(stream *splitMix64, radius float64) (float64, float64) {
	squareWidth := float64(radius) * annulusSquareWidthFactor
	squareHalf := float64(radius) * annulusSquareHalfFactor
	radiusSquared := float64(radius) * float64(radius)
	outerRadius := float64(radius) * annulusSquareHalfFactor
	outerSquared := float64(outerRadius) * float64(outerRadius)
	for {
		offsetX := stream.uniform01() * squareWidth
		offsetX = offsetX - squareHalf
		offsetY := stream.uniform01() * squareWidth
		offsetY = offsetY - squareHalf
		offsetXSquared := float64(offsetX) * float64(offsetX)
		offsetYSquared := float64(offsetY) * float64(offsetY)
		distanceSquared := offsetXSquared + offsetYSquared
		if distanceSquared >= radiusSquared && distanceSquared < outerSquared {
			return offsetX, offsetY
		}
	}
}

type anchorAccelerationGrid struct {
	side          float64
	columns       int64
	rows          int64
	neighborRange int64
	buckets       [][]int
}

func newAnchorAccelerationGrid(req PlacementRequest) *anchorAccelerationGrid {
	minimumDistance := req.MinDistance
	maximumDistance := req.MinDistance
	if len(req.DensityRegions) > 0 {
		for _, region := range req.DensityRegions {
			if region.MinDistance < minimumDistance {
				minimumDistance = region.MinDistance
			}
			if region.MinDistance > maximumDistance {
				maximumDistance = region.MinDistance
			}
		}
	}
	side := minimumDistance / math.Sqrt(bridsonDimensions)
	columns := int64(math.Ceil(float64(req.Width) / side))
	rows := int64(math.Ceil(float64(req.Height) / side))
	if columns < 1 {
		columns = 1
	}
	if rows < 1 {
		rows = 1
	}
	neighborRange := int64(uniformAccelerationRange)
	if len(req.DensityRegions) > 0 {
		maximumUsefulRange := columns
		if rows > maximumUsefulRange {
			maximumUsefulRange = rows
		}
		ratio := maximumDistance / side
		if ratio >= float64(maximumUsefulRange) {
			neighborRange = maximumUsefulRange
		} else {
			neighborRange = int64(math.Ceil(ratio)) + int64(densityAccelerationMargin)
			if neighborRange > maximumUsefulRange {
				neighborRange = maximumUsefulRange
			}
		}
	}
	return &anchorAccelerationGrid{
		side: side, columns: columns, rows: rows, neighborRange: neighborRange,
		buckets: make([][]int, int(columns*rows)),
	}
}

func (grid *anchorAccelerationGrid) insert(anchor Cell, acceptedIndex int) {
	x, y := grid.cell(anchor)
	index := y*grid.columns + x
	grid.buckets[int(index)] = append(grid.buckets[int(index)], acceptedIndex)
}

// nearbyInto acrescenta os vizinhos ao buffer da solicitação na ordem
// determinística dos buckets e evita alocação por tentativa Poisson.
func (grid *anchorAccelerationGrid) nearbyInto(
	anchor Cell,
	accepted []acceptedPlacement,
	buffer []acceptedPlacement,
) []acceptedPlacement {
	centerX, centerY := grid.cell(anchor)
	minimumX := centerX - grid.neighborRange
	if minimumX < 0 {
		minimumX = 0
	}
	maximumX := centerX + grid.neighborRange
	if maximumX >= grid.columns {
		maximumX = grid.columns - 1
	}
	minimumY := centerY - grid.neighborRange
	if minimumY < 0 {
		minimumY = 0
	}
	maximumY := centerY + grid.neighborRange
	if maximumY >= grid.rows {
		maximumY = grid.rows - 1
	}

	for y := minimumY; y <= maximumY; y++ {
		rowStart := y * grid.columns
		for x := minimumX; x <= maximumX; x++ {
			index := rowStart + x
			for _, acceptedIndex := range grid.buckets[int(index)] {
				buffer = append(buffer, accepted[acceptedIndex])
			}
		}
	}
	return buffer
}

func (grid *anchorAccelerationGrid) cell(anchor Cell) (int64, int64) {
	x := int64(math.Floor(float64(anchor.X) / grid.side))
	y := int64(math.Floor(float64(anchor.Y) / grid.side))
	if x >= grid.columns {
		x = grid.columns - 1
	}
	if y >= grid.rows {
		y = grid.rows - 1
	}
	return x, y
}

func nearestPlacementToGridCenter(
	gridWidth, gridHeight uint32,
	geometry roomGeometryCombination,
) (RoomPlacement, bool) {
	var nearest RoomPlacement
	var nearestSquared int64
	var offsetsScratch []Cell
	found := false
	for y := int64(0); y < int64(gridHeight); y++ {
		for x := int64(0); x < int64(gridWidth); x++ {
			anchor := Cell{X: int32(x), Y: int32(y)}
			candidate := buildPlacementFromAtInto(
				anchor,
				geometry.shape,
				geometry.width,
				geometry.height,
				offsetsScratch[:0],
			)
			offsetsScratch = candidate.Cells
			if !placementWithinBounds(candidate, gridWidth, gridHeight) {
				continue
			}

			// Dobrar as coordenadas compara a distância ao centro geométrico
			// sem ponto flutuante: centro=(dimensão-1)/2.
			doubledX := x * int64(annulusSquareHalfFactor)
			doubledY := y * int64(annulusSquareHalfFactor)
			deltaX := doubledX - int64(gridWidth-1)
			deltaY := doubledY - int64(gridHeight-1)
			deltaXSquared := deltaX * deltaX
			deltaYSquared := deltaY * deltaY
			squared := deltaXSquared + deltaYSquared
			if !found || squared < nearestSquared {
				nearest = candidate
				nearestSquared = squared
				found = true
			}
		}
	}
	if found {
		nearest.Cells = append([]Cell(nil), offsetsScratch...)
	}
	return nearest, found
}
