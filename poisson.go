package daedalus

import (
	"context"
	"errors"
	"math"
)

var errPlacementRequestNotNormalized = errors.New("placement request is not normalized")

const (
	// roomShapeCount is the number of contiguous shapes in the v1 contract.
	roomShapeCount = int(RoomShapeCircle) + 1
	// annulusSquareWidthFactor and annulusSquareHalfFactor describe the
	// [-2r,2r] square enclosing the annulus.
	annulusSquareWidthFactor = 4.0
	annulusSquareHalfFactor  = 2.0
	// uniformAccelerationRange covers the Bridson grid's 5×5 neighbourhood when
	// no DensityRegions exist.
	uniformAccelerationRange = 2
	// densityAccelerationMargin is the section 7 margin for the Bridson grid's
	// variable range.
	densityAccelerationMargin = 1
	// bridsonDimensions is the sqrt(2) divisor for a Cell side in two dimensions,
	// as specified by section 7.
	bridsonDimensions = 2.0
	// cancellationCandidateInterval limits the interval between Context checks
	// during candidate proposals.
	cancellationCandidateInterval uint64 = 256
)

// poissonDiskRoomsPlacer implements the built-in, frozen
// poisson_disk_rooms_v1 algorithm. The type has no state: every buffer and
// stream belongs to one Place call.
type poissonDiskRoomsPlacer struct{}

var _ Placer = poissonDiskRoomsPlacer{}

// Place proposes Rooms according to section 7 of the specification.
func (poissonDiskRoomsPlacer) Place(req PlacementRequest) ([]RoomPlacement, error) {
	run, err := preparePoissonRun(req)
	if err != nil {
		return nil, err
	}
	for len(run.active) > 0 && uint32(len(run.accepted)) < run.req.MaxRooms {
		if err := run.ctx.Err(); err != nil {
			return nil, err
		}
		// One placement-stream draw per outer iteration. Attempts below must
		// not draw another active index.
		activeIndex := int(run.streams.placement.uniformInt(0, uint64(len(run.active)-1)))
		if err := run.attemptFromActive(activeIndex); err != nil {
			return nil, err
		}
	}
	if err := run.ctx.Err(); err != nil {
		return nil, err
	}
	return run.placements, nil
}

// poissonRun is the mutable state of one poisson_disk_rooms_v1 Place call.
// Draw order is frozen: one active-index draw per outer iteration, then
// offsetX and offsetY on each attempt, and a geometry draw only after that
// anchor lands inside the Grid.
type poissonRun struct {
	req                 PlacementRequest
	ctx                 context.Context
	streams             rngStreams
	geometrySampler     roomGeometrySampler
	accepted            []acceptedPlacement
	placements          []RoomPlacement
	active              []Cell
	nearbyBuffer        []acceptedPlacement
	localOffsetsScratch []Cell
	footprintScratch    []Cell
	occupancy           *placementOccupancy
	acceleration        *anchorAccelerationGrid
	acceptance          placementAcceptance
	hasDensityRegions   bool
	candidateAttempts   uint64
}

func preparePoissonRun(req PlacementRequest) (*poissonRun, error) {
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
	occupancy := newPlacementOccupancy(req.Width, req.Height)
	occupancy.mark(0, firstFootprint)
	acceleration := newAnchorAccelerationGrid(req)
	acceleration.insert(firstFootprint[0], 0)
	return &poissonRun{
		req:                 req,
		ctx:                 ctx,
		streams:             streams,
		geometrySampler:     geometrySampler,
		accepted:            accepted,
		placements:          placements,
		active:              active,
		nearbyBuffer:        make([]acceptedPlacement, 0, req.MaxRooms),
		localOffsetsScratch: make([]Cell, 0, req.RoomGeometry.MaxFootprintCells),
		footprintScratch:    make([]Cell, 0, req.RoomGeometry.MaxFootprintCells),
		occupancy:           occupancy,
		acceleration:        acceleration,
		acceptance: placementAcceptance{
			gridWidth:         req.Width,
			gridHeight:        req.Height,
			maxFootprintCells: req.RoomGeometry.MaxFootprintCells,
			minRoomGap:        req.RoomGeometry.MinRoomGap,
			minDistance:       req.MinDistance,
			densityRegions:    req.DensityRegions,
			occupancy:         occupancy,
		},
		hasDensityRegions: len(req.DensityRegions) > 0,
	}, nil
}

func (run *poissonRun) attemptFromActive(activeIndex int) error {
	base := run.active[activeIndex]
	for attempt := uint32(0); attempt < run.req.MaxAttempts; attempt++ {
		if run.candidateAttempts%cancellationCandidateInterval == 0 {
			if err := run.ctx.Err(); err != nil {
				return err
			}
		}
		run.candidateAttempts++
		anchor, inside := run.proposeAnchor(base)
		if !inside {
			continue
		}
		accepted, err := run.acceptAnchor(anchor)
		if err != nil {
			return err
		}
		if accepted {
			return nil
		}
	}
	run.active = append(run.active[:activeIndex], run.active[activeIndex+1:]...)
	return nil
}

// proposeAnchor draws offsetX and offsetY for this attempt and quantizes the
// candidate. Geometry is intentionally not drawn here: an anchor outside the
// Grid rejects the attempt without touching the geometry stream.
func (run *poissonRun) proposeAnchor(base Cell) (Cell, bool) {
	radius := run.req.MinDistance
	if run.hasDensityRegions {
		radius = localMinDistance(base, run.req.MinDistance, run.req.DensityRegions)
	}
	offsetX, offsetY := sampleUniformAnnulusByRejection(&run.streams.placement, radius)
	baseX := float64(base.X)
	baseY := float64(base.Y)
	continuousX := baseX + offsetX
	continuousY := baseY + offsetY
	anchorX := math.Floor(continuousX)
	anchorY := math.Floor(continuousY)
	if anchorX < 0 || anchorX >= float64(run.req.Width) || anchorY < 0 || anchorY >= float64(run.req.Height) {
		return Cell{}, false
	}
	return Cell{X: int32(anchorX), Y: int32(anchorY)}, true
}

func (run *poissonRun) acceptAnchor(anchor Cell) (bool, error) {
	geometry, sampled := run.geometrySampler.sample(&run.streams.roomGeometry)
	if !sampled {
		return false, errPlacementRequestNotNormalized
	}
	candidate := buildPlacementFromAtInto(
		anchor,
		geometry.shape,
		geometry.width,
		geometry.height,
		run.localOffsetsScratch[:0],
	)
	run.localOffsetsScratch = candidate.Cells
	nearby := run.acceleration.nearbyInto(anchor, run.accepted, run.nearbyBuffer[:0])
	var validationErr error
	run.acceptance.accepted = nearby
	run.footprintScratch, validationErr = validatePlacementAndMaterializeInto(
		candidate,
		run.acceptance,
		run.footprintScratch[:0],
	)
	if validationErr != nil {
		return false, nil
	}

	owner := uint32(len(run.accepted))
	acceptedFootprint := append([]Cell(nil), run.footprintScratch...)
	run.accepted = append(run.accepted, acceptedPlacement{anchor: anchor, footprint: acceptedFootprint})
	acceptedCandidate := candidate
	acceptedCandidate.Cells = append([]Cell(nil), candidate.Cells...)
	run.placements = append(run.placements, acceptedCandidate)
	run.occupancy.mark(owner, run.footprintScratch)
	run.acceleration.insert(anchor, int(owner))
	run.active = append(run.active, anchor)
	return true, nil
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

// sampleUniformAnnulusByRejection translates section 7's
// SampleUniformAnnulusByRejection. The specification does not state the
// transformation from uniform01 to the square; the frozen v1 interpretation is
// u*4r-2r on each axis. uniform01 belongs to [0,1), consistent with the
// annulus's exclusive outer bound; the inner bound is inclusive. Also under the
// section 7 interpretation, pairs outside the annulus are rejected internally
// without consuming a geometry attempt, as indicated by the pseudocode name
// and the normative list of proposal-rejection reasons.
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
	minimumDistance, maximumDistance := extremalRegionDistances(req.MinDistance, req.DensityRegions)
	side := minimumDistance / math.Sqrt(bridsonDimensions)
	columns := accelerationAxisCount(req.Width, side)
	rows := accelerationAxisCount(req.Height, side)
	neighborRange := bridsonNeighborRange(columns, rows, maximumDistance, side, len(req.DensityRegions) > 0)
	return &anchorAccelerationGrid{
		side: side, columns: columns, rows: rows, neighborRange: neighborRange,
		buckets: make([][]int, int(columns*rows)),
	}
}

func extremalRegionDistances(fallback float64, regions []DensityRegion) (float64, float64) {
	minimum := fallback
	maximum := fallback
	for _, region := range regions {
		if region.MinDistance < minimum {
			minimum = region.MinDistance
		}
		if region.MinDistance > maximum {
			maximum = region.MinDistance
		}
	}
	return minimum, maximum
}

func accelerationAxisCount(span uint32, side float64) int64 {
	count := int64(math.Ceil(float64(span) / side))
	if count < 1 {
		return 1
	}
	return count
}

func bridsonNeighborRange(columns, rows int64, maximumDistance, side float64, hasDensityRegions bool) int64 {
	if !hasDensityRegions {
		return int64(uniformAccelerationRange)
	}
	maximumUsefulRange := columns
	if rows > maximumUsefulRange {
		maximumUsefulRange = rows
	}
	ratio := maximumDistance / side
	if ratio >= float64(maximumUsefulRange) {
		return maximumUsefulRange
	}
	neighborRange := int64(math.Ceil(ratio)) + int64(densityAccelerationMargin)
	if neighborRange > maximumUsefulRange {
		return maximumUsefulRange
	}
	return neighborRange
}

func (grid *anchorAccelerationGrid) insert(anchor Cell, acceptedIndex int) {
	x, y := grid.cell(anchor)
	index := y*grid.columns + x
	grid.buckets[int(index)] = append(grid.buckets[int(index)], acceptedIndex)
}

// nearbyInto appends neighbours to the request buffer in deterministic bucket
// order and avoids allocation per Poisson attempt.
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

			// Doubling coordinates compares distance to the geometric center without
			// floating point: center=(dimension-1)/2.
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
