package daedalus

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAnchorDerivationReconstructsCanonicalMasks covers AC-20: for every
// Shape, the materialized Cells are exactly the canonical mask of the
// declared bounds.
func TestAnchorDerivationReconstructsCanonicalMasks(t *testing.T) {
	tests := []struct {
		name   string
		shape  RoomShape
		width  uint32
		height uint32
	}{
		{"rectangle", RoomShapeRectangle, 4, 3},
		{"L", RoomShapeL, 4, 3},
		{"T", RoomShapeT, 5, 4},
		{"cross", RoomShapeCross, 5, 5},
		{"circle", RoomShapeCircle, 5, 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			at := Cell{X: 10, Y: 12}
			placement := buildPlacementFromAt(at, tc.shape, tc.width, tc.height)
			footprint, ok := absoluteFootprint(placement)
			require.True(t, ok)

			firstOffset := RoomShapeFirstOffset(tc.shape, tc.width, tc.height)
			assert.Equal(t, Cell{X: at.X - firstOffset.X, Y: at.Y - firstOffset.Y}, placement.Origin)
			assert.Equal(t, RoomShapeOffsets(tc.shape, tc.width, tc.height), placement.Cells)
			require.NotEmpty(t, footprint)
			assert.Equal(t, at, footprint[0])
			if tc.shape == RoomShapeCross || tc.shape == RoomShapeCircle {
				assert.NotContains(t, footprint, placement.Origin)
			}
		})
	}
}

func TestAbsoluteFootprintReusesRequestBuffer(t *testing.T) {
	placement := buildPlacementFromAt(Cell{X: 10, Y: 12}, RoomShapeRectangle, 4, 3)
	buffer := make([]Cell, 0, len(placement.Cells))
	var footprint []Cell
	var ok bool

	allocations := testing.AllocsPerRun(100, func() {
		footprint, ok = absoluteFootprintInto(placement, buffer[:0])
	})

	require.True(t, ok)
	assert.Zero(t, allocations, "hot materialization must reuse the request buffer")
	assert.Equal(t, Cell{X: 10, Y: 12}, footprint[0])
}

func TestBoundaryAnchorMayProduceNegativeOrigin(t *testing.T) {
	placement := buildPlacementFromAt(Cell{X: 0, Y: 0}, RoomShapeCross, 5, 5)

	assert.Equal(t, Cell{X: -2, Y: 0}, placement.Origin)
	assert.False(t, placementWithinBounds(placement, 8, 8))
	assert.ErrorIs(t, validatePlacementForAcceptance(placement, acceptanceRules(8, 8, 25, 0, 1, nil, nil)), errPlacementOutOfBounds)
}

func TestBoundsAcceptsBoundingBoxAtExactGridLimit(t *testing.T) {
	placement := buildPlacementFromAt(Cell{X: 2, Y: 2}, RoomShapeRectangle, 3, 3)

	assert.True(t, placementWithinBounds(placement, 5, 5))
	assert.NoError(t, validatePlacementForAcceptance(placement, acceptanceRules(5, 5, 9, 0, 1, nil, nil)))
}

func TestGapZeroAcceptsTouchingFootprintsAndRejectsOverlap(t *testing.T) {
	accepted := buildPlacementFromAt(Cell{X: 0, Y: 0}, RoomShapeRectangle, 2, 2)
	touching := buildPlacementFromAt(Cell{X: 2, Y: 0}, RoomShapeRectangle, 2, 2)
	overlapping := buildPlacementFromAt(Cell{X: 1, Y: 1}, RoomShapeRectangle, 2, 2)
	acceptedPlacements, occupancy := mustAcceptedState(t, 8, 8, accepted)

	assert.NoError(t, validatePlacementForAcceptance(touching, acceptanceRules(8, 8, 4, 0, 1, acceptedPlacements, occupancy)))
	assert.ErrorIs(t, validatePlacementForAcceptance(overlapping, acceptanceRules(8, 8, 4, 0, 1, acceptedPlacements, occupancy)), errPlacementOverlap)
}

func TestGapOneRejectsDiagonalContact(t *testing.T) {
	accepted := buildPlacementFromAt(Cell{X: 0, Y: 0}, RoomShapeRectangle, 1, 1)
	diagonal := buildPlacementFromAt(Cell{X: 1, Y: 1}, RoomShapeRectangle, 1, 1)
	acceptedPlacements, occupancy := mustAcceptedState(t, 4, 4, accepted)

	assert.False(t, footprintsRespectGap(mustFootprint(t, diagonal), mustFootprint(t, accepted), 1))
	assert.ErrorIs(t, validatePlacementForAcceptance(diagonal, acceptanceRules(4, 4, 1, 1, 1, acceptedPlacements, occupancy)), errPlacementGap)
}

func TestAnchorDistanceUsesGreaterLocalDistance(t *testing.T) {
	regions := []DensityRegion{
		{Min: Cell{X: 0, Y: 0}, Max: Cell{X: 3, Y: 3}, MinDistance: 2},
		{Min: Cell{X: 4, Y: 0}, Max: Cell{X: 8, Y: 3}, MinDistance: 5},
	}
	a := Cell{X: 1, Y: 1}
	b := Cell{X: 5, Y: 1}

	assert.Equal(t, 2.0, localMinDistance(a, 3, regions))
	assert.Equal(t, 5.0, localMinDistance(b, 3, regions))
	assert.False(t, anchorsRespectDistance(a, b, 3, regions))
	assert.True(t, anchorsRespectDistance(a, Cell{X: 6, Y: 1}, 3, regions))
	// Max is exclusive: this Cell lies outside the first region.
	assert.Equal(t, 3.0, localMinDistance(Cell{X: 3, Y: 2}, 3, regions))
}

func TestValidMaskRejectsTamperedOffsets(t *testing.T) {
	canonical := buildPlacementFromAt(Cell{X: 3, Y: 3}, RoomShapeRectangle, 2, 2)
	tests := []struct {
		name  string
		cells []Cell
	}{
		{"duplicates", []Cell{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}}},
		{"disconnected", []Cell{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 0, Y: 1}, {X: 3, Y: 3}}},
		{"different order", []Cell{{X: 1, Y: 0}, {X: 0, Y: 0}, {X: 0, Y: 1}, {X: 1, Y: 1}}},
		{"different shape", RoomShapeOffsets(RoomShapeL, 2, 2)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			placement := canonical
			placement.Cells = tc.cells
			assert.False(t, placementHasCanonicalMask(placement))
		})
	}
}

func TestPlacementValidationStopsAtFirstViolatedRule(t *testing.T) {
	placement := RoomPlacement{
		Shape:  RoomShapeRectangle,
		Origin: Cell{X: -1, Y: 0},
		Width:  2,
		Height: 2,
		Cells:  []Cell{{X: 0, Y: 0}},
	}

	err := validatePlacementForAcceptance(placement, acceptanceRules(1, 1, 1, 0, 1, nil, nil))
	assert.ErrorIs(t, err, errPlacementInvalidMask)
}

// TestPlacementAreaRespectsEffectiveLimit covers the area clause of AC-21:
// a footprint larger than the effective limit is rejected.
func TestPlacementAreaRespectsEffectiveLimit(t *testing.T) {
	placement := buildPlacementFromAt(Cell{X: 0, Y: 0}, RoomShapeRectangle, 2, 2)

	assert.False(t, placementWithinArea(placement, 3))
	assert.ErrorIs(t, validatePlacementForAcceptance(placement, acceptanceRules(4, 4, 3, 0, 1, nil, nil)), errPlacementAreaExceeded)
}

func TestOverlapDetectsSharedCell(t *testing.T) {
	a := []Cell{{X: 1, Y: 1}, {X: 2, Y: 1}}
	b := []Cell{{X: 2, Y: 1}, {X: 3, Y: 1}}
	c := []Cell{{X: 3, Y: 1}}

	assert.True(t, footprintsOverlap(a, b))
	assert.False(t, footprintsOverlap(a, c))
}

func TestGridNeighbourhoodMatchesPairwiseComparison(t *testing.T) {
	const (
		gridSize       = uint32(32)
		casesPerGap    = 500
		maximumGap     = uint32(3)
		maximumCells   = 24
		randomSeed     = int64(0xDAEDA1)
		acceptedOwner  = uint32(0)
		candidateOwner = uint32(1)
	)
	random := rand.New(rand.NewSource(randomSeed))

	for minRoomGap := uint32(0); minRoomGap <= maximumGap; minRoomGap++ {
		for testCase := 0; testCase < casesPerGap; testCase++ {
			acceptedFootprint := randomFootprint(random, gridSize, maximumCells)
			candidateFootprint := randomFootprint(random, gridSize, maximumCells)
			occupancy := newPlacementOccupancy(gridSize, gridSize)
			occupancy.mark(acceptedOwner, acceptedFootprint)

			expected := footprintsRespectGap(candidateFootprint, acceptedFootprint, minRoomGap)
			actual := occupancy.footprintRespectsGap(candidateFootprint, minRoomGap, candidateOwner)
			assert.Equal(t, expected, actual, "gap=%d case=%d", minRoomGap, testCase)
		}
	}
}

func TestGridNeighbourhoodIgnoresCellsOfSameRoom(t *testing.T) {
	const owner = uint32(7)
	footprint := []Cell{{X: 4, Y: 4}, {X: 5, Y: 4}}
	occupancy := newPlacementOccupancy(8, 8)
	occupancy.mark(owner, footprint)

	assert.True(t, occupancy.footprintRespectsGap(footprint, 3, owner))
}

func randomFootprint(random *rand.Rand, gridSize uint32, maximumCells int) []Cell {
	cellCount := random.Intn(maximumCells) + 1
	seen := make(map[Cell]struct{}, cellCount)
	footprint := make([]Cell, 0, cellCount)
	for len(footprint) < cellCount {
		cell := Cell{X: int32(random.Intn(int(gridSize))), Y: int32(random.Intn(int(gridSize)))}
		if _, exists := seen[cell]; exists {
			continue
		}
		seen[cell] = struct{}{}
		footprint = append(footprint, cell)
	}
	return footprint
}

func mustFootprint(t *testing.T, placement RoomPlacement) []Cell {
	t.Helper()
	footprint, ok := absoluteFootprint(placement)
	require.True(t, ok)
	return footprint
}

func mustAcceptedState(t testing.TB, gridWidth, gridHeight uint32, placements ...RoomPlacement) ([]acceptedPlacement, *placementOccupancy) {
	t.Helper()
	accepted := make([]acceptedPlacement, 0, len(placements))
	occupancy := newPlacementOccupancy(gridWidth, gridHeight)
	for index, placement := range placements {
		footprint, ok := absoluteFootprint(placement)
		require.True(t, ok)
		require.NotEmpty(t, footprint)
		accepted = append(accepted, acceptedPlacement{anchor: footprint[0], footprint: footprint})
		occupancy.mark(uint32(index), footprint)
	}
	return accepted, occupancy
}

func BenchmarkAcceptance256(b *testing.B) {
	const (
		gridSize     = uint32(256)
		roomSize     = uint32(9)
		acceptedSide = 16
	)
	placements := make([]RoomPlacement, 0, acceptedSide*acceptedSide)
	for y := 0; y < acceptedSide; y++ {
		for x := 0; x < acceptedSide; x++ {
			placements = append(placements, buildPlacementFromAt(
				Cell{X: int32(x) * int32(roomSize), Y: int32(y) * int32(roomSize)},
				RoomShapeRectangle,
				roomSize,
				roomSize,
			))
		}
	}
	accepted, occupancy := mustAcceptedState(b, gridSize, gridSize, placements...)
	candidate := buildPlacementFromAt(Cell{X: 240, Y: 240}, RoomShapeRectangle, roomSize, roomSize)

	rules := acceptanceRules(gridSize, gridSize, roomSize*roomSize, 1, 0, accepted, occupancy)

	b.ReportAllocs()
	for b.Loop() {
		if err := validatePlacementForAcceptance(candidate, rules); err != nil {
			b.Fatal(err)
		}
	}
}

func acceptanceRules(
	gridWidth, gridHeight, maxFootprintCells, minRoomGap uint32,
	minDistance float64,
	accepted []acceptedPlacement,
	occupancy *placementOccupancy,
) placementAcceptance {
	return placementAcceptance{
		gridWidth:         gridWidth,
		gridHeight:        gridHeight,
		maxFootprintCells: maxFootprintCells,
		minRoomGap:        minRoomGap,
		minDistance:       minDistance,
		accepted:          accepted,
		occupancy:         occupancy,
	}
}
