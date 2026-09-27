package daedalus_test

import (
	"context"
	"testing"

	"github.com/Otoru/daedalus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestZeroGeneratorUsesNilStrategies checks that the zero Generator, whose
// Placer and Connector are nil, still returns a Layout.
func TestZeroGeneratorUsesNilStrategies(t *testing.T) {
	var generator daedalus.Generator
	require.Nil(t, generator.Placer)
	require.Nil(t, generator.Connector)

	layout, err := generator.Generate(daedalus.Config{Width: 16, Height: 16, Seed: 3})

	require.NoError(t, err)
	require.NotEmpty(t, layout.Rooms)
	assert.Equal(t, daedalus.Seed(3), layout.Seed)
	assert.Equal(t, uint32(16), layout.Grid.Width)
	assert.Len(t, layout.Grid.Cells, 16*16)
	assert.LessOrEqual(t, len(layout.Grid.Cells), daedalus.MaxCells)
}

// TestSameConfigAndSeedRepeatTheLayout checks that two calls with the same
// Config and Seed return equal Layouts.
func TestSameConfigAndSeedRepeatTheLayout(t *testing.T) {
	config := daedalus.Config{Width: 16, Height: 16, Seed: 3}
	generator := daedalus.Generator{}

	first, firstErr := generator.Generate(config)
	second, secondErr := generator.Generate(config)

	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	assert.Equal(t, first, second)
}

// TestGenerateAgreesWithGenerateContext checks that Generate and
// GenerateContext return the same Layout for the same input.
func TestGenerateAgreesWithGenerateContext(t *testing.T) {
	config := daedalus.Config{Width: 16, Height: 16, Seed: 3}
	generator := daedalus.Generator{}

	direct, directErr := generator.Generate(config)
	withContext, contextErr := generator.GenerateContext(context.Background(), config)

	require.NoError(t, directErr)
	require.NoError(t, contextErr)
	assert.Equal(t, direct, withContext)
}

// TestErrorSentinelsAreClassifiable checks that an integrator can classify
// each failure category with errors.Is, and that a failure returns the zero
// Layout.
func TestErrorSentinelsAreClassifiable(t *testing.T) {
	t.Run("invalid config", func(t *testing.T) {
		layout, err := daedalus.Generator{}.Generate(daedalus.Config{Height: 8, Seed: 1})

		assert.ErrorIs(t, err, daedalus.ErrInvalidConfig)
		assert.Equal(t, daedalus.Layout{}, layout)
	})

	t.Run("product limit", func(t *testing.T) {
		layout, err := daedalus.Generator{}.Generate(daedalus.Config{
			Width: 1, Height: 1, Seed: 1, MaxRooms: daedalus.MaxRooms + 1,
		})

		assert.ErrorIs(t, err, daedalus.ErrLimitExceeded)
		assert.NotErrorIs(t, err, daedalus.ErrInvalidConfig)
		assert.Equal(t, daedalus.Layout{}, layout)
	})

	t.Run("grid above the dimension limit", func(t *testing.T) {
		layout, err := daedalus.Generator{}.Generate(daedalus.Config{
			Width: 257, Height: 1, Seed: 1,
		})

		assert.ErrorIs(t, err, daedalus.ErrLimitExceeded)
		assert.Equal(t, daedalus.Layout{}, layout)
	})

	t.Run("footprint above MaxFootprintCells", func(t *testing.T) {
		layout, err := daedalus.Generator{}.Generate(daedalus.Config{
			Width: 8, Height: 8, Seed: 1,
			RoomGeometry: &daedalus.RoomGeometry{
				MinWidth: 1, MaxWidth: 1, MinHeight: 1, MaxHeight: 1,
				MaxFootprintCells: daedalus.MaxFootprintCells + 1,
				Shapes: []daedalus.RoomShapeWeight{{
					Shape: daedalus.RoomShapeRectangle, Weight: 1,
				}},
			},
		})

		assert.ErrorIs(t, err, daedalus.ErrLimitExceeded)
		assert.Equal(t, daedalus.Layout{}, layout)
	})

	t.Run("incompatible catalog", func(t *testing.T) {
		layout, err := daedalus.Generator{}.Generate(daedalus.Config{
			Width: 32, Height: 32, Seed: 1111, MaxRooms: 16,
			PlantCatalog: &daedalus.PlantCatalog{
				Rooms: []daedalus.RoomPlant{{
					ID: "north-only", Weight: 1, DoorDirections: []daedalus.Direction{daedalus.DirectionNorth},
				}},
				Corridors: []daedalus.CorridorPlant{{ID: "corridor", Weight: 1}},
			},
		})

		assert.ErrorIs(t, err, daedalus.ErrNoCompatiblePlant)
		assert.Equal(t, daedalus.Layout{}, layout)
	})

	t.Run("lying placer", func(t *testing.T) {
		layout, err := daedalus.Generator{Placer: lyingPlacer{}}.Generate(daedalus.Config{
			Width: 8, Height: 8, Seed: 1,
		})

		assert.ErrorIs(t, err, daedalus.ErrInvalidPlugin)
		assert.Equal(t, daedalus.Layout{}, layout)
	})

	t.Run("unroutable edge", func(t *testing.T) {
		layout, err := daedalus.Generator{
			Placer:    wallPlacer{},
			Connector: directConnector{},
		}.Generate(blockedCorridorConfig())

		assert.ErrorIs(t, err, daedalus.ErrUnroutableEdge)
		assert.Equal(t, daedalus.Layout{}, layout)
	})
}

// TestExternalPlacerAndConnectorAreAccepted checks that a Placer and a
// Connector written against the exported types are used as given.
func TestExternalPlacerAndConnectorAreAccepted(t *testing.T) {
	require.True(t, daedalus.ValidRoomShapeDimensions(daedalus.RoomShapeRectangle, 3, 3))
	require.Equal(t, daedalus.Cell{}, daedalus.RoomShapeFirstOffset(daedalus.RoomShapeRectangle, 3, 3))
	require.Equal(t, daedalus.Cell{X: 2}, daedalus.RoomShapeFirstOffset(daedalus.RoomShapeCross, 5, 5))

	generator := daedalus.Generator{Placer: twoRoomPlacer{}, Connector: treeConnector{}}
	layout, err := generator.Generate(daedalus.Config{
		Width: 8, Height: 4, Seed: 9, MinDistance: 1, MaxRooms: 2,
		RoomGeometry: &daedalus.RoomGeometry{
			MinWidth: 3, MaxWidth: 3, MinHeight: 3, MaxHeight: 3,
			MaxFootprintCells: 9, MinRoomGap: 1,
			Shapes: []daedalus.RoomShapeWeight{{
				Shape: daedalus.RoomShapeRectangle, Weight: 1,
			}},
		},
	})

	require.NoError(t, err)
	require.Len(t, layout.Rooms, 2)
	assert.Equal(t, daedalus.Cell{}, layout.Rooms[0].Origin)
	assert.Equal(t, daedalus.Cell{X: 5}, layout.Rooms[1].Origin)
	assert.Equal(t, daedalus.RoomShapeRectangle, layout.Rooms[0].Shape)
	assert.Len(t, layout.Corridors, 1)
	assert.Equal(t, daedalus.RoomID(0), layout.Corridors[0].FromRoomID)
	assert.Equal(t, daedalus.RoomID(1), layout.Corridors[0].ToRoomID)
}

// TestFunctionAdaptersMatchTheirInterfaces checks that PlacerFunc and
// ConnectorFunc forward the request untouched: wrapping the very strategies
// used by TestExternalPlacerAndConnectorAreAccepted in closures must produce
// the same Layout the interface values produce. An adapter that dropped or
// rewrote the request would move a Room or an edge and fail here.
func TestFunctionAdaptersMatchTheirInterfaces(t *testing.T) {
	var _ daedalus.Placer = daedalus.PlacerFunc(nil)
	var _ daedalus.Connector = daedalus.ConnectorFunc(nil)

	config := daedalus.Config{
		Width: 8, Height: 4, Seed: 9, MinDistance: 1, MaxRooms: 2,
		RoomGeometry: &daedalus.RoomGeometry{
			MinWidth: 3, MaxWidth: 3, MinHeight: 3, MaxHeight: 3,
			MaxFootprintCells: 9, MinRoomGap: 1,
			Shapes: []daedalus.RoomShapeWeight{{
				Shape: daedalus.RoomShapeRectangle, Weight: 1,
			}},
		},
	}

	fromInterfaces, err := daedalus.Generator{
		Placer:    twoRoomPlacer{},
		Connector: treeConnector{},
	}.Generate(config)
	require.NoError(t, err)

	fromClosures, err := daedalus.Generator{
		Placer: daedalus.PlacerFunc(func(request daedalus.PlacementRequest) ([]daedalus.RoomPlacement, error) {
			return twoRoomPlacer{}.Place(request)
		}),
		Connector: daedalus.ConnectorFunc(func(request daedalus.ConnectionRequest) ([]daedalus.Connection, error) {
			return treeConnector{}.Connect(request)
		}),
	}.Generate(config)
	require.NoError(t, err)

	assert.Equal(t, fromInterfaces, fromClosures)
}

// TestFunctionAdaptersPropagateErrors checks that an error returned by the
// adapted function reaches the caller instead of being swallowed.
func TestFunctionAdaptersPropagateErrors(t *testing.T) {
	layout, err := daedalus.Generator{
		Placer: daedalus.PlacerFunc(func(daedalus.PlacementRequest) ([]daedalus.RoomPlacement, error) {
			return lyingPlacer{}.Place(daedalus.PlacementRequest{})
		}),
	}.Generate(daedalus.Config{Width: 8, Height: 8, Seed: 1})

	assert.ErrorIs(t, err, daedalus.ErrInvalidPlugin)
	assert.Equal(t, daedalus.Layout{}, layout)
}

// TestMutatingALayoutDoesNotAffectAnotherCall checks that writing into a
// returned Layout leaves a Layout from another call unchanged.
func TestMutatingALayoutDoesNotAffectAnotherCall(t *testing.T) {
	config := daedalus.Config{
		Width: 16, Height: 16, Seed: 3, MaxRooms: 6,
		PlantCatalog: &daedalus.PlantCatalog{
			Rooms: []daedalus.RoomPlant{{
				ID: "hall", Tags: []string{"stone"}, Weight: 1,
				DoorDirections: []daedalus.Direction{
					daedalus.DirectionNorth, daedalus.DirectionEast,
					daedalus.DirectionSouth, daedalus.DirectionWest,
				},
			}},
			Corridors: []daedalus.CorridorPlant{{
				ID: "passage", Tags: []string{"damp"}, Weight: 1,
			}},
		},
	}
	generator := daedalus.Generator{}
	first, firstErr := generator.Generate(config)
	second, secondErr := generator.Generate(config)
	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	require.Equal(t, first, second)
	require.NotEmpty(t, first.Rooms)
	require.NotEmpty(t, first.Rooms[0].Cells)
	require.NotEmpty(t, first.Rooms[0].Tags)
	require.NotEmpty(t, first.Corridors)
	require.NotEmpty(t, first.Corridors[0].Cells)
	require.NotEmpty(t, first.Doors)
	require.NotEmpty(t, first.Doors[0].CorridorIDs)

	originalCell := second.Rooms[0].Cells[0]
	originalTag := second.Rooms[0].Tags[0]
	originalCorridorCell := second.Corridors[0].Cells[0]
	originalDoorCorridor := second.Doors[0].CorridorIDs[0]
	originalGridAt := second.Grid.Cells[0].At

	first.Rooms[0].Cells[0] = daedalus.Cell{X: 99, Y: 99}
	first.Rooms[0].Tags[0] = "changed"
	first.Corridors[0].Cells[0] = daedalus.Cell{X: 98, Y: 98}
	first.Doors[0].CorridorIDs[0] = daedalus.CorridorID(99)
	first.Grid.Cells[0].At = daedalus.Cell{X: 97, Y: 97}

	assert.Equal(t, originalCell, second.Rooms[0].Cells[0])
	assert.Equal(t, originalTag, second.Rooms[0].Tags[0])
	assert.Equal(t, originalCorridorCell, second.Corridors[0].Cells[0])
	assert.Equal(t, originalDoorCorridor, second.Doors[0].CorridorIDs[0])
	assert.Equal(t, originalGridAt, second.Grid.Cells[0].At)
}

// lyingPlacer returns a mask that does not match its Shape.
type lyingPlacer struct{}

func (lyingPlacer) Place(daedalus.PlacementRequest) ([]daedalus.RoomPlacement, error) {
	return []daedalus.RoomPlacement{{
		Shape:  daedalus.RoomShapeRectangle,
		Origin: daedalus.Cell{},
		Width:  2,
		Height: 2,
		Cells: []daedalus.Cell{
			{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1},
		},
	}}, nil
}

// twoRoomPlacer proposes two 3×3 rectangles far enough apart to connect.
type twoRoomPlacer struct{}

func (twoRoomPlacer) Place(request daedalus.PlacementRequest) ([]daedalus.RoomPlacement, error) {
	if err := request.Context.Err(); err != nil {
		return nil, err
	}
	mask := daedalus.RoomShapeOffsets(daedalus.RoomShapeRectangle, 3, 3)
	return []daedalus.RoomPlacement{
		{Shape: daedalus.RoomShapeRectangle, Origin: daedalus.Cell{}, Width: 3, Height: 3, Cells: mask},
		{Shape: daedalus.RoomShapeRectangle, Origin: daedalus.Cell{X: 5}, Width: 3, Height: 3, Cells: mask},
	}, nil
}

// treeConnector joins every Room to the previous one.
type treeConnector struct{}

func (treeConnector) Connect(request daedalus.ConnectionRequest) ([]daedalus.Connection, error) {
	if err := request.Context.Err(); err != nil {
		return nil, err
	}
	edges := make([]daedalus.Connection, 0, len(request.Rooms))
	for index := 1; index < len(request.Rooms); index++ {
		edges = append(edges, daedalus.Connection{
			FromRoomID: daedalus.RoomID(index - 1),
			ToRoomID:   daedalus.RoomID(index),
		})
	}
	return edges, nil
}

// wallPlacer places two Rooms on either side of a solid column, so the
// straight connection between them has no orthogonal route.
type wallPlacer struct{}

func (wallPlacer) Place(request daedalus.PlacementRequest) ([]daedalus.RoomPlacement, error) {
	if err := request.Context.Err(); err != nil {
		return nil, err
	}
	unit := daedalus.RoomShapeOffsets(daedalus.RoomShapeRectangle, 1, 1)
	column := daedalus.RoomShapeOffsets(daedalus.RoomShapeRectangle, 1, 5)
	return []daedalus.RoomPlacement{
		{Shape: daedalus.RoomShapeRectangle, Origin: daedalus.Cell{Y: 2}, Width: 1, Height: 1, Cells: unit},
		{Shape: daedalus.RoomShapeRectangle, Origin: daedalus.Cell{X: 4, Y: 2}, Width: 1, Height: 1, Cells: unit},
		{Shape: daedalus.RoomShapeRectangle, Origin: daedalus.Cell{X: 2}, Width: 1, Height: 5, Cells: column},
	}, nil
}

// directConnector asks for the blocked edge and one edge that keeps the
// other Room in the graph.
type directConnector struct{}

func (directConnector) Connect(daedalus.ConnectionRequest) ([]daedalus.Connection, error) {
	return []daedalus.Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 2},
	}, nil
}

func blockedCorridorConfig() daedalus.Config {
	return daedalus.Config{
		Width: 5, Height: 5, Seed: 1, MinDistance: 1, MaxRooms: 3,
		RoomGeometry: &daedalus.RoomGeometry{
			MinWidth: 1, MaxWidth: 1, MinHeight: 1, MaxHeight: 5,
			MaxFootprintCells: 5, MinRoomGap: 1,
			Shapes: []daedalus.RoomShapeWeight{{
				Shape: daedalus.RoomShapeRectangle, Weight: 1,
			}},
		},
	}
}
