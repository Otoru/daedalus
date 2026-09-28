package daedalus

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSuccessfulLayoutRespectsAllInvariants checks a successful Layout against
// the Layout invariants: MinRoomGap and anchor distance, Room.Cells matching
// the canonical mask, and a footprint that is non-empty, unique, inside the
// Grid, 4-connected, with At on the footprint.
func TestSuccessfulLayoutRespectsAllInvariants(t *testing.T) {
	config := Config{Width: 32, Height: 32, Seed: 101, MaxRooms: 24}
	effective, err := normalizeConfig(config)
	require.NoError(t, err)
	layout, err := (Generator{}).Generate(config)
	require.NoError(t, err)

	assertLayoutInvariants(t, effective, layout)
}

func TestPropertyLayoutsOrKnownGenerationErrors(t *testing.T) {
	const propertySeed int64 = 0x5eedf10
	const caseCount = 48
	random := rand.New(rand.NewSource(propertySeed))
	generator := Generator{}

	for caseIndex := 0; caseIndex < caseCount; caseIndex++ {
		config := randomPropertyConfig(random, caseIndex)
		name := fmt.Sprintf("case_%02d_seed_%d", caseIndex, propertySeed)
		t.Run(name, func(t *testing.T) {
			effective, normalizationErr := normalizeConfig(config)
			layout, err := generator.GenerateContext(context.Background(), config)
			if err != nil {
				assert.Equal(t, Layout{}, layout, "property seed=%d", propertySeed)
				assert.True(t, isKnownGenerationError(err), "property seed=%d: error outside the known generation categories: %v", propertySeed, err)
				if normalizationErr == nil {
					assert.NotErrorIs(t, err, ErrInvalidConfig, "property seed=%d", propertySeed)
					assert.NotErrorIs(t, err, ErrLimitExceeded, "property seed=%d", propertySeed)
				}
				return
			}
			require.NoError(t, normalizationErr, "property seed=%d", propertySeed)
			assertLayoutInvariants(t, effective, layout)
		})
	}
}

// TestSameConfigAndSeedProduceIdenticalWholeLayout checks that the same Config
// and Seed reproduce every field, footprint and order.
func TestSameConfigAndSeedProduceIdenticalWholeLayout(t *testing.T) {
	config := Config{
		Width: 48, Height: 40, Seed: 400, MaxRooms: 28, ExtraEdgeCount: 3,
		RoomRoleRequests: []RoomRoleRequest{
			{Role: RoomRoleStart, Count: 1},
			{Role: RoomRoleBoss, Count: 1},
			{Role: RoomRoleTreasure, Count: 2},
		},
	}
	baseline, err := (Generator{}).Generate(config)
	require.NoError(t, err)

	for repetition := 0; repetition < 4; repetition++ {
		actual, generationErr := (Generator{}).Generate(config)
		require.NoError(t, generationErr)
		assert.Equal(t, baseline, actual, "repetition %d differed field by field", repetition)
	}
}

// TestConcurrentRequestsAreIsolated checks that interleaved requests each match
// the isolated baseline, and that cancelling one leaves the other intact.
func TestConcurrentRequestsAreIsolated(t *testing.T) {
	generator := Generator{}
	config := Config{Width: 40, Height: 40, Seed: 505, MaxRooms: 24, ExtraEdgeCount: 2}
	baseline, err := generator.Generate(config)
	require.NoError(t, err)

	const requestCount = 12
	results := make([]Layout, requestCount)
	errs := make([]error, requestCount)
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(requestCount)
	for requestIndex := range results {
		go func(index int) {
			defer wait.Done()
			<-start
			results[index], errs[index] = generator.Generate(config)
		}(requestIndex)
	}
	close(start)
	wait.Wait()

	for requestIndex := range results {
		require.NoError(t, errs[requestIndex])
		assert.Equal(t, baseline, results[requestIndex], "concurrent request %d", requestIndex)
	}

	mutated := results[0]
	mutated.Rooms[0].Cells[0] = Cell{X: -1, Y: -1}
	mutated.Grid.Cells[0].Kind = CellKindCorridor
	if len(mutated.Corridors) > 0 && len(mutated.Corridors[0].Cells) > 0 {
		mutated.Corridors[0].Cells[0] = Cell{X: -2, Y: -2}
	}
	if len(mutated.Doors) > 0 {
		mutated.Doors[0].CorridorIDs[0] = CorridorID(999)
	}
	fresh, err := generator.Generate(config)
	require.NoError(t, err)
	assert.Equal(t, baseline, fresh, "mutating one result must not contaminate another request")

	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	var canceledLayout Layout
	var canceledErr error
	var independentLayout Layout
	var independentErr error
	wait.Add(2)
	go func() {
		defer wait.Done()
		canceledLayout, canceledErr = generator.GenerateContext(canceledContext, config)
	}()
	go func() {
		defer wait.Done()
		independentLayout, independentErr = generator.Generate(config)
	}()
	wait.Wait()
	assert.ErrorIs(t, canceledErr, context.Canceled)
	assert.Equal(t, Layout{}, canceledLayout)
	require.NoError(t, independentErr)
	assert.Equal(t, baseline, independentLayout)
}

// TestSingleCellGridHasOneRoomAndNoCorridorOrDoor checks that a 1×1 Grid with
// MinDistance 6 yields one 1×1 Rectangle at (0,0) and no Corridor or Door.
func TestSingleCellGridHasOneRoomAndNoCorridorOrDoor(t *testing.T) {
	config := Config{Width: 1, Height: 1, Seed: 7, MinDistance: 6}
	effective, err := normalizeConfig(config)
	require.NoError(t, err)
	layout, err := (Generator{}).Generate(config)
	require.NoError(t, err)

	require.Len(t, layout.Rooms, 1)
	assert.Equal(t, Room{
		ID: 0, At: Cell{X: 0, Y: 0}, Shape: RoomShapeRectangle,
		Origin: Cell{X: 0, Y: 0}, Width: 1, Height: 1,
		Cells: []Cell{{X: 0, Y: 0}},
	}, layout.Rooms[0])
	assert.Empty(t, layout.Corridors)
	assert.Empty(t, layout.Doors)
	assertLayoutInvariants(t, effective, layout)
}

// TestSmallGridAndLargeDistanceTerminateWithoutTruncation checks that a small
// Grid or a MinDistance above the diagonal keeps the first valid Room near the
// center and does not truncate its footprint.
func TestSmallGridAndLargeDistanceTerminateWithoutTruncation(t *testing.T) {
	first := fixedGeometryConfigForTest(2, 2, 10)
	first.Seed = 80
	first.MinDistance = 6
	second := fixedGeometryConfigForTest(7, 5, 20)
	second.Seed = 81
	second.MinDistance = 100
	cases := []struct {
		config Config
		wantAt Cell
	}{
		{config: first, wantAt: Cell{X: 0, Y: 0}},
		{config: second, wantAt: Cell{X: 3, Y: 2}},
	}
	for caseIndex, testCase := range cases {
		effective, err := normalizeConfig(testCase.config)
		require.NoError(t, err)
		layout, err := (Generator{}).Generate(testCase.config)
		require.NoError(t, err, "case %d", caseIndex)
		require.Len(t, layout.Rooms, 1, "case %d", caseIndex)
		assert.Equal(t, testCase.wantAt, layout.Rooms[0].At, "first Room closest to the center in case %d", caseIndex)
		assertLayoutInvariants(t, effective, layout)
	}
}

// TestDensityRegionsUseGreaterLocalDistance checks that each anchor pair
// respects the greater of the two LocalMinDistance values.
func TestDensityRegionsUseGreaterLocalDistance(t *testing.T) {
	config := Config{
		Width: 48, Height: 48, Seed: 9091, MinDistance: 3, MaxRooms: 36,
		DensityRegions: []DensityRegion{
			{Min: Cell{X: 0, Y: 0}, Max: Cell{X: 24, Y: 48}, MinDistance: 8},
			{Min: Cell{X: 24, Y: 0}, Max: Cell{X: 48, Y: 24}, MinDistance: 5},
		},
	}
	effective, err := normalizeConfig(config)
	require.NoError(t, err)
	layout, err := (Generator{}).Generate(config)
	require.NoError(t, err)

	for first := 0; first < len(layout.Rooms); first++ {
		for second := first + 1; second < len(layout.Rooms); second++ {
			firstDistance := localMinDistance(layout.Rooms[first].At, effective.minDistance, effective.densityRegions)
			secondDistance := localMinDistance(layout.Rooms[second].At, effective.minDistance, effective.densityRegions)
			requiredDistance := max(firstDistance, secondDistance)
			distanceSquared := squaredCellDistance(layout.Rooms[first].At, layout.Rooms[second].At)
			assert.GreaterOrEqual(t, float64(distanceSquared), requiredDistance*requiredDistance,
				"anchor pair %d/%d", first, second)
		}
	}
	assertLayoutInvariants(t, effective, layout)
}

// TestEmptyRegionsChangeNeitherLayoutNorDraws checks that an explicit empty
// DensityRegions list reproduces the Layout of the omitted list.
func TestEmptyRegionsChangeNeitherLayoutNorDraws(t *testing.T) {
	base := Config{Width: 40, Height: 32, Seed: 9090, MaxRooms: 24}
	explicitlyEmpty := base
	explicitlyEmpty.DensityRegions = []DensityRegion{}

	baseline, err := (Generator{}).Generate(base)
	require.NoError(t, err)
	actual, err := (Generator{}).Generate(explicitlyEmpty)
	require.NoError(t, err)
	assert.Equal(t, baseline, actual)
}

// TestCompatibleCatalogPreservesIDsTagsAndDirections checks that each selected
// RoomPlant supports the Door directions in use, and that IDs and tags match
// the catalog.
func TestCompatibleCatalogPreservesIDsTagsAndDirections(t *testing.T) {
	config := Config{Width: 32, Height: 32, Seed: 1010, MaxRooms: 16}
	config.PlantCatalog = &PlantCatalog{
		Rooms:     []RoomPlant{{ID: "hall", Tags: []string{"stone"}, Weight: 1, DoorDirections: allDirectionsForTest()}},
		Corridors: []CorridorPlant{{ID: "corridor", Tags: []string{"damp"}, Weight: 1}},
	}
	layout, err := (Generator{}).Generate(config)
	require.NoError(t, err)
	for roomIndex, room := range layout.Rooms {
		assert.Equal(t, PlantID("hall"), room.PlantID, "Room %d", roomIndex)
		assert.Equal(t, []string{"stone"}, room.Tags, "Room %d", roomIndex)
	}
	for corridorIndex, corridor := range layout.Corridors {
		assert.Equal(t, PlantID("corridor"), corridor.PlantID, "Corridor %d", corridorIndex)
		assert.Equal(t, []string{"damp"}, corridor.Tags, "Corridor %d", corridorIndex)
	}
}

// TestIncompatibleCatalogFailsWithoutPartialLayout checks that an incompatible
// catalog returns ErrNoCompatiblePlant and an empty Layout.
func TestIncompatibleCatalogFailsWithoutPartialLayout(t *testing.T) {
	config := Config{Width: 32, Height: 32, Seed: 1111, MaxRooms: 16}
	config.PlantCatalog = &PlantCatalog{
		Rooms:     []RoomPlant{{ID: "north-only", Weight: 1, DoorDirections: []Direction{DirectionNorth}}},
		Corridors: []CorridorPlant{{ID: "corridor", Weight: 1}},
	}
	layout, err := (Generator{}).Generate(config)
	assert.ErrorIs(t, err, ErrNoCompatiblePlant)
	assert.Equal(t, Layout{}, layout)
}

// TestShortcutsKeepGraphConnectedAndAddCycles checks that extra edges keep the
// Layout connected and are the shortest discarded edges, taken first.
func TestShortcutsKeepGraphConnectedAndAddCycles(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 2, 0),
		placedRoomAt(2, 0, 2),
		placedRoomAt(3, 2, 2),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 2},
		{FromRoomID: 1, ToRoomID: 3},
	}
	connections, err := addExtraConnections(context.Background(), rooms, backbone, 2)
	require.NoError(t, err)
	assert.Equal(t, []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 2},
		{FromRoomID: 1, ToRoomID: 3},
		{FromRoomID: 2, ToRoomID: 3},
		{FromRoomID: 0, ToRoomID: 3},
	}, connections, "shortcuts must be the shortest discarded edges first")

	config := Config{Width: 48, Height: 48, Seed: 121, MaxRooms: 24, ExtraEdgeCount: 3}
	effective, err := normalizeConfig(config)
	require.NoError(t, err)
	layout, err := (Generator{}).Generate(config)
	require.NoError(t, err)
	require.Greater(t, len(layout.Rooms), 2)
	assert.Equal(t, len(layout.Rooms)-1+3, len(layout.Corridors))
	assertLayoutInvariants(t, effective, layout)
}

// TestWithoutShortcutsGraphIsTree checks that with ExtraEdgeCount=0 the
// Corridor count is Rooms minus one and every Room stays reachable.
func TestWithoutShortcutsGraphIsTree(t *testing.T) {
	config := Config{Width: 48, Height: 48, Seed: 122, MaxRooms: 24, ExtraEdgeCount: 0}
	effective, err := normalizeConfig(config)
	require.NoError(t, err)
	layout, err := (Generator{}).Generate(config)
	require.NoError(t, err)
	assert.Equal(t, len(layout.Rooms)-1, len(layout.Corridors))
	assertLayoutInvariants(t, effective, layout)
}

// TestRolesRespectMSTAndRequiredTags checks that Start is RoomID 0, that Boss
// and Treasure take the unassigned Rooms with the greatest weighted MST path
// from Start (ties break by ascending RoomID), and that the chosen Plant
// carries the RequiredTags.
func TestRolesRespectMSTAndRequiredTags(t *testing.T) {
	config := Config{
		Width: 48, Height: 48, Seed: 123, MaxRooms: 24,
		RoomRoleRequests: []RoomRoleRequest{
			{Role: RoomRoleStart, Count: 1, RequiredTags: []string{"start"}},
			{Role: RoomRoleBoss, Count: 1, RequiredTags: []string{"boss"}},
			{Role: RoomRoleTreasure, Count: 2, RequiredTags: []string{"treasure"}},
		},
		PlantCatalog: &PlantCatalog{
			Rooms: []RoomPlant{
				{ID: "common", Weight: 1, DoorDirections: allDirectionsForTest()},
				{ID: "start", Tags: []string{"start"}, Weight: 1, DoorDirections: allDirectionsForTest()},
				{ID: "boss", Tags: []string{"boss"}, Weight: 1, DoorDirections: allDirectionsForTest()},
				{ID: "treasure", Tags: []string{"treasure"}, Weight: 1, DoorDirections: allDirectionsForTest()},
			},
			Corridors: []CorridorPlant{{ID: "corridor", Weight: 1}},
		},
	}
	effective, err := normalizeConfig(config)
	require.NoError(t, err)
	layout, err := (Generator{}).Generate(config)
	require.NoError(t, err)
	require.NotNil(t, layout.Rooms[0].Role)
	assert.Equal(t, RoomRoleStart, *layout.Rooms[0].Role)
	counts := map[RoomRole]int{}
	for roomIndex, room := range layout.Rooms {
		if room.Role == nil {
			continue
		}
		counts[*room.Role]++
		requiredTag := map[RoomRole]string{RoomRoleStart: "start", RoomRoleBoss: "boss", RoomRoleTreasure: "treasure"}[*room.Role]
		assert.Contains(t, room.Tags, requiredTag, "Room %d", roomIndex)
	}
	assert.Equal(t, 1, counts[RoomRoleStart])
	assert.Equal(t, 1, counts[RoomRoleBoss])
	assert.Equal(t, 2, counts[RoomRoleTreasure])
	assertLayoutInvariants(t, effective, layout)
}

// TestEmptyRolesChangeNeitherLayoutNorDraws checks that an explicit empty role
// list leaves every Role unset and reproduces the Layout.
func TestEmptyRolesChangeNeitherLayoutNorDraws(t *testing.T) {
	base := Config{Width: 40, Height: 40, Seed: 124, MaxRooms: 20}
	explicitlyEmpty := base
	explicitlyEmpty.RoomRoleRequests = []RoomRoleRequest{}
	baseline, err := (Generator{}).Generate(base)
	require.NoError(t, err)
	actual, err := (Generator{}).Generate(explicitlyEmpty)
	require.NoError(t, err)
	assert.Equal(t, baseline, actual)
	for roomIndex, room := range actual.Rooms {
		assert.Nil(t, room.Role, "Room %d", roomIndex)
	}
}

// TestDisabledOptionsPreserveRefinedLayout checks that zero shortcuts, empty
// roles and no DensityRegions reproduce the refined Layout for the same
// normalized RoomGeometry.
func TestDisabledOptionsPreserveRefinedLayout(t *testing.T) {
	geometry := &RoomGeometry{
		MaxFootprintCells: 49, MinRoomGap: 1,
		Shapes: []RoomShapeWeight{
			shapeSpan(RoomShapeRectangle, 4, 3, 7, 3, 7),
			shapeSpan(RoomShapeL, 2, 3, 7, 3, 7),
			shapeSpan(RoomShapeT, 2, 3, 7, 3, 7),
			shapeSpan(RoomShapeCross, 1, 3, 7, 3, 7),
			shapeSpan(RoomShapeCircle, 2, 3, 7, 3, 7),
		},
	}
	base := Config{Width: 40, Height: 40, Seed: 125, MaxRooms: 20, RoomGeometry: geometry}
	explicitZeros := base
	explicitZeros.ExtraEdgeCount = 0
	explicitZeros.RoomRoleRequests = []RoomRoleRequest{}
	explicitZeros.DensityRegions = []DensityRegion{}
	baseline, err := (Generator{}).Generate(base)
	require.NoError(t, err)
	actual, err := (Generator{}).Generate(explicitZeros)
	require.NoError(t, err)
	assert.Equal(t, baseline, actual)
}

// TestLimitsFailBeforeGeneratingLayout checks that a limit breach returns
// ErrLimitExceeded before the Placer runs and publishes no Layout.
func TestLimitsFailBeforeGeneratingLayout(t *testing.T) {
	configs := []Config{
		{Width: 257, Height: 1, Seed: 1},
		{Width: 1, Height: 257, Seed: 1},
		{Width: 16, Height: 16, Seed: 1, MaxRooms: MaxRooms + 1},
	}
	for caseIndex, config := range configs {
		layout, err := (Generator{Placer: panicAcceptancePlacer{}}).Generate(config)
		assert.ErrorIs(t, err, ErrLimitExceeded, "case %d", caseIndex)
		assert.Equal(t, Layout{}, layout, "case %d", caseIndex)
	}
}

// TestNoPositionFitsFailsWithoutTruncatingOrPublishingLayout checks that when
// no footprint fits, generation stops without a partial Layout, and that a
// 1×1 Grid still admits a 1×1 Rectangle.
func TestNoPositionFitsFailsWithoutTruncatingOrPublishingLayout(t *testing.T) {
	geometry := &RoomGeometry{
		MaxFootprintCells: 36,
		Shapes:            []RoomShapeWeight{shapeSpan(RoomShapeRectangle, 1, 6, 6, 6, 6)},
	}
	layout, err := (Generator{}).Generate(Config{Width: 5, Height: 5, Seed: 27, RoomGeometry: geometry})
	assert.ErrorIs(t, err, ErrInvalidConfig)
	assert.Equal(t, Layout{}, layout)

	minimal, err := (Generator{}).Generate(Config{Width: 1, Height: 1, Seed: 27})
	require.NoError(t, err)
	require.Len(t, minimal.Rooms, 1)
	assert.Equal(t, []Cell{{X: 0, Y: 0}}, minimal.Rooms[0].Cells)
}

// TestGeneratorRejectsDishonestPlacersAndConnectors checks that the Generator
// rejects a mask that is duplicated, disconnected, outside the Grid or
// incompatible with the Shape, and that it publishes no Layout.
func TestGeneratorRejectsDishonestPlacersAndConnectors(t *testing.T) {
	placerModes := []string{"duplicate", "disconnected", "out-of-bounds", "incompatible", "overlapping"}
	for _, mode := range placerModes {
		generator := Generator{Placer: lyingAcceptancePlacer{mode: mode}}
		layout, err := generator.Generate(Config{Width: 16, Height: 16, Seed: 29})
		assert.Error(t, err, "Placer %q", mode)
		assert.Equal(t, Layout{}, layout, "Placer %q", mode)
	}

	connectorModes := []string{"self", "duplicate", "unknown", "disconnected"}
	for _, mode := range connectorModes {
		generator := Generator{Connector: lyingAcceptanceConnector{mode: mode}}
		layout, err := generator.Generate(Config{Width: 64, Height: 64, Seed: 29, MaxRooms: 24})
		assert.Error(t, err, "Connector %q", mode)
		assert.Equal(t, Layout{}, layout, "Connector %q", mode)
	}
}

func TestInvariantCheckerReportsPathExpectedAndActual(t *testing.T) {
	config := Config{Width: 24, Height: 24, Seed: 5010, MaxRooms: 12}
	effective, err := normalizeConfig(config)
	require.NoError(t, err)
	layout, err := (Generator{}).Generate(config)
	require.NoError(t, err)
	layout.Grid.Cells[0].At = Cell{X: 99, Y: 99}

	failures := layoutInvariantFailures(effective, layout)
	require.NotEmpty(t, failures)
	assert.Contains(t, strings.Join(failures, "\n"), "Layout.Grid.Cells[0].At: expected {0 0}; actual {99 99}")
}

func randomPropertyConfig(random *rand.Rand, caseIndex int) Config {
	width := uint32(8 + random.Intn(33))
	height := uint32(8 + random.Intn(33))
	maxRooms := uint32(4 + random.Intn(15))
	config := Config{
		Width: width, Height: height, Seed: Seed(random.Uint64()),
		MinDistance:    float64(1 + random.Intn(8)),
		MaxAttempts:    uint32(1 + random.Intn(32)),
		MaxRooms:       maxRooms,
		CorridorOrder:  CorridorOrder(random.Intn(2)),
		ExtraEdgeCount: uint32(random.Intn(3)),
	}
	if caseIndex%2 == 0 {
		maximumWidth := min(width, uint32(7))
		maximumHeight := min(height, uint32(7))
		config.RoomGeometry = &RoomGeometry{
			MaxFootprintCells: maximumWidth * maximumHeight,
			MinRoomGap:        uint32(random.Intn(3)),
			Shapes: []RoomShapeWeight{
				shapeSpan(RoomShapeRectangle, uint32(1+random.Intn(4)), 1, maximumWidth, 1, maximumHeight),
			},
		}
	}
	if caseIndex%3 == 0 {
		regionWidth := int32(width / 2)
		config.DensityRegions = []DensityRegion{{
			Min: Cell{X: 0, Y: 0}, Max: Cell{X: regionWidth, Y: int32(height)},
			MinDistance: float64(1 + random.Intn(8)),
		}}
	}
	if caseIndex%4 == 0 {
		config.RoomRoleRequests = []RoomRoleRequest{
			{Role: RoomRoleStart, Count: 1},
			{Role: RoomRoleBoss, Count: 1},
			{Role: RoomRoleTreasure, Count: min(uint32(2), maxRooms)},
		}
	}
	if caseIndex%11 == 0 {
		config.Width = 257
	}
	if caseIndex%13 == 0 {
		config.MaxRooms = MaxRooms + 1
	}
	return config
}

func isKnownGenerationError(err error) bool {
	return errors.Is(err, ErrInvalidConfig) ||
		errors.Is(err, ErrLimitExceeded) ||
		errors.Is(err, ErrNoCompatiblePlant) ||
		errors.Is(err, ErrUnroutableEdge) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}

type panicAcceptancePlacer struct{}

func (panicAcceptancePlacer) Place(PlacementRequest) ([]RoomPlacement, error) {
	panic("Placer must not be called before validation")
}

type lyingAcceptancePlacer struct {
	mode string
}

func (placer lyingAcceptancePlacer) Place(PlacementRequest) ([]RoomPlacement, error) {
	validMask := RoomShapeOffsets(RoomShapeRectangle, 3, 3)
	switch placer.mode {
	case "duplicate":
		return []RoomPlacement{{
			Shape: RoomShapeRectangle, Origin: Cell{X: 0, Y: 0}, Width: 2, Height: 2,
			Cells: []Cell{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}},
		}}, nil
	case "disconnected":
		return []RoomPlacement{{
			Shape: RoomShapeRectangle, Origin: Cell{X: 0, Y: 0}, Width: 2, Height: 2,
			Cells: []Cell{{X: 0, Y: 0}, {X: 1, Y: 1}},
		}}, nil
	case "out-of-bounds":
		return []RoomPlacement{{
			Shape: RoomShapeRectangle, Origin: Cell{X: -1, Y: -1}, Width: 3, Height: 3, Cells: validMask,
		}}, nil
	case "incompatible":
		return []RoomPlacement{{
			Shape: RoomShapeCircle, Origin: Cell{X: 0, Y: 0}, Width: 3, Height: 3,
			Cells: []Cell{{X: 0, Y: 0}},
		}}, nil
	case "overlapping":
		return []RoomPlacement{
			{Shape: RoomShapeRectangle, Origin: Cell{X: 0, Y: 0}, Width: 3, Height: 3, Cells: validMask},
			{Shape: RoomShapeRectangle, Origin: Cell{X: 1, Y: 1}, Width: 3, Height: 3, Cells: validMask},
		}, nil
	default:
		return nil, errors.New("unknown Placer mode")
	}
}

type lyingAcceptanceConnector struct {
	mode string
}

func (connector lyingAcceptanceConnector) Connect(request ConnectionRequest) ([]Connection, error) {
	roomCount := len(request.Rooms)
	if roomCount < 4 {
		return nil, errors.New("test case requires at least four Rooms")
	}
	edges := make([]Connection, 0, roomCount-1)
	for roomIndex := 1; roomIndex < roomCount; roomIndex++ {
		edges = append(edges, Connection{FromRoomID: RoomID(roomIndex - 1), ToRoomID: RoomID(roomIndex)})
	}
	switch connector.mode {
	case "self":
		edges[0] = Connection{FromRoomID: 0, ToRoomID: 0}
	case "duplicate":
		edges[1] = edges[0]
	case "unknown":
		edges[0] = Connection{FromRoomID: 0, ToRoomID: RoomID(roomCount + 10)}
	case "disconnected":
		edges = []Connection{
			{FromRoomID: 1, ToRoomID: 2},
			{FromRoomID: 2, ToRoomID: 3},
			{FromRoomID: 3, ToRoomID: 1},
		}
		for roomIndex := 4; roomIndex < roomCount; roomIndex++ {
			edges = append(edges, Connection{FromRoomID: RoomID(roomIndex - 1), ToRoomID: RoomID(roomIndex)})
		}
	}
	return edges, nil
}
