package daedalus

import "errors"

var (
	errPlacementInvalidMask  = errors.New("placement does not match the canonical mask")
	errPlacementOutOfBounds  = errors.New("placement is outside Grid bounds")
	errPlacementAreaExceeded = errors.New("placement exceeds the effective maximum area")
	errPlacementDistance     = errors.New("placement anchor does not meet the minimum distance")
	errPlacementOverlap      = errors.New("placement overlaps an accepted footprint")
	errPlacementGap          = errors.New("placement does not meet the minimum gap between Rooms")
)

const (
	// Limits of the public Cell-coordinate representation.
	minCellCoordinate int64 = -1 << 31
	maxCellCoordinate int64 = 1<<31 - 1
	// The grid encodes owner+1 to reserve zero for an empty Cell.
	placementOwnerEncodingOffset uint32 = 1
	// unownedPlacementOwner never identifies an accepted Room; during validation,
	// the candidate has not yet been marked in the occupancy grid.
	unownedPlacementOwner = ^uint32(0)
)

// acceptedPlacement is an already accepted Room whose anchor and absolute
// footprint are materialized once by the request running the Placer.
type acceptedPlacement struct {
	anchor    Cell
	footprint []Cell
}

// placementOccupancy keeps, per request, the index of the Room occupying each
// Cell. The index is encoded with an offset to distinguish Room zero from an
// empty Cell.
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

// gapWindow is one footprint Cell's Chebyshev neighbourhood, clipped to the
// occupancy grid. Inclusive bounds match the pairwise oracle in
// footprintsRespectGap.
type gapWindow struct {
	minX int64
	maxX int64
	minY int64
	maxY int64
}

func (occupancy *placementOccupancy) footprintRespectsGap(
	footprint []Cell,
	minRoomGap uint32,
	candidateOwner uint32,
) bool {
	radius := int64(minRoomGap)
	for _, cell := range footprint {
		window := occupancy.clampedGapWindow(cell, radius)
		if occupancy.gapWindowHasForeignOwner(window, candidateOwner) {
			return false
		}
	}
	return true
}

func (occupancy *placementOccupancy) clampedGapWindow(cell Cell, radius int64) gapWindow {
	window := gapWindow{
		minX: int64(cell.X) - radius,
		maxX: int64(cell.X) + radius,
		minY: int64(cell.Y) - radius,
		maxY: int64(cell.Y) + radius,
	}
	if window.minX < 0 {
		window.minX = 0
	}
	if window.maxX >= occupancy.width {
		window.maxX = occupancy.width - 1
	}
	if window.minY < 0 {
		window.minY = 0
	}
	if window.maxY >= occupancy.height {
		window.maxY = occupancy.height - 1
	}
	return window
}

func (occupancy *placementOccupancy) gapWindowHasForeignOwner(window gapWindow, candidateOwner uint32) bool {
	for y := window.minY; y <= window.maxY; y++ {
		rowStart := y * occupancy.width
		for x := window.minX; x <= window.maxX; x++ {
			index := rowStart + x
			encodedOwner := occupancy.owners[int(index)]
			if encodedOwner == 0 {
				continue
			}
			owner := encodedOwner - placementOwnerEncodingOffset
			if owner != candidateOwner {
				return true
			}
		}
	}
	return false
}

// buildPlacementFromAt derives the bounding box and local mask from the first
// occupied Cell in canonical order. The generation path supplies a non-negative
// anchor and dimensions bounded by the v1 Grid; therefore the int64 subtraction
// always fits in Cell, including when Origin is negative.
func buildPlacementFromAt(at Cell, shape RoomShape, width, height uint32) RoomPlacement {
	return buildPlacementFromAtInto(at, shape, width, height, nil)
}

// buildPlacementFromAtInto derives the placement in the supplied private
// scratch buffer. The result must be copied before the buffer is reused.
func buildPlacementFromAtInto(
	at Cell,
	shape RoomShape,
	width, height uint32,
	scratch []Cell,
) RoomPlacement {
	// The specification defines this derivation only for valid dimensions. Under
	// the conservative reading, invalid dimensions preserve the supplied data
	// and follow the canonical helpers' contract: zero offset and nil mask; the
	// first validation step rejects the result.
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

// absoluteFootprint materializes local offsets without changing their canonical
// order. False indicates that at least one sum does not fit the Cell
// representation; placements within the v1 Grid never reach that case.
func absoluteFootprint(placement RoomPlacement) ([]Cell, bool) {
	return absoluteFootprintInto(placement, nil)
}

// absoluteFootprintInto materializes offsets in the request's private buffer.
// The caller must copy the result before reusing the buffer when the footprint
// must outlive the current attempt.
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

// footprintsOverlap and footprintsRespectGap are literal translations of the
// section 7 rule, comparing every pair. The production path uses the much
// cheaper occupancy grid; these two remain as the reference oracle in
// equivalence tests. Do not remove them as apparent dead code: the optimization
// is verified against them.
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
		// The convention frozen in F02 is half-open: inclusive Min and exclusive
		// Max, as stated in DensityRegion's public documentation.
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

	// In v1 Grids, each delta is at most 255 and squaredDistance at most 130,050,
	// far below 2^53. Conversion to float64 is therefore exact and permits
	// comparison with the configurable distance squared without rounding the
	// discrete distance.
	return float64(squaredDistance) >= requiredSquared
}

// placementAcceptance carries the request-scoped inputs of the section 7
// acceptance checks. Build it once per request and pass it by value; only the
// accepted slice header changes between attempts, so the hot loop does not
// allocate a fresh value.
type placementAcceptance struct {
	gridWidth         uint32
	gridHeight        uint32
	maxFootprintCells uint32
	minRoomGap        uint32
	minDistance       float64
	densityRegions    []DensityRegion
	accepted          []acceptedPlacement
	occupancy         *placementOccupancy
}

// validatePlacementForAcceptance centralizes the normative section 7 order. It
// only reads rules.accepted; the caller appends after nil, keeping every
// rejected attempt atomic.
func validatePlacementForAcceptance(candidate RoomPlacement, rules placementAcceptance) error {
	_, err := validatePlacementAndMaterialize(candidate, rules)
	return err
}

// validatePlacementAndMaterialize returns the absolute footprint to store in
// acceptedPlacement when validation succeeds. This keeps the acceptance path
// from rebuilding an already validated Room.
func validatePlacementAndMaterialize(candidate RoomPlacement, rules placementAcceptance) ([]Cell, error) {
	footprint, err := validatePlacementAndMaterializeInto(candidate, rules, nil)
	if err != nil {
		return nil, err
	}
	return footprint, nil
}

// validatePlacementAndMaterializeInto preserves the normative validation order
// but materializes into the request's private scratch space. On an error after
// materialization, it returns the scratch so the Poisson loop can reuse it.
func validatePlacementAndMaterializeInto(
	candidate RoomPlacement,
	rules placementAcceptance,
	scratch []Cell,
) ([]Cell, error) {
	if !placementHasCanonicalMask(candidate) {
		return scratch[:0], errPlacementInvalidMask
	}
	if !placementWithinBounds(candidate, rules.gridWidth, rules.gridHeight) {
		return scratch[:0], errPlacementOutOfBounds
	}
	if !placementWithinArea(candidate, rules.maxFootprintCells) {
		return scratch[:0], errPlacementAreaExceeded
	}

	candidateFootprint, ok := absoluteFootprintInto(candidate, scratch[:0])
	if !ok {
		return scratch[:0], errPlacementOutOfBounds
	}
	// Without the grid, overlap and gap could not be checked and the normative
	// sequence would be silently incomplete. Calling with accepted Rooms and no
	// grid is a programming error, not user input.
	if len(rules.accepted) > 0 && rules.occupancy == nil {
		panic("occupancy grid is missing with already accepted placements")
	}

	candidateAt := candidateFootprint[0]
	for _, placement := range rules.accepted {
		if !anchorsRespectDistance(candidateAt, placement.anchor, rules.minDistance, rules.densityRegions) {
			return candidateFootprint, errPlacementDistance
		}
	}

	if rules.occupancy != nil && rules.occupancy.footprintOverlaps(candidateFootprint) {
		return candidateFootprint, errPlacementOverlap
	}
	// The candidate has no owner yet. The sentinel prevents confusing it with
	// any Room, including when accepted contains only Bridson neighbours.
	if rules.occupancy != nil && !rules.occupancy.footprintRespectsGap(candidateFootprint, rules.minRoomGap, unownedPlacementOwner) {
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
