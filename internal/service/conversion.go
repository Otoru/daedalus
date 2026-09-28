package service

import (
	"fmt"

	"github.com/Otoru/daedalus"
	daedalusv1 "github.com/Otoru/daedalus/internal/gen/go/daedalus/v1"
)

const (
	invalidCorridorOrder daedalus.CorridorOrder = -1
	invalidRoomRole      daedalus.RoomRole      = -1
	invalidDirection     daedalus.Direction     = -1
	invalidRoomShape     daedalus.RoomShape     = -1
)

// ConfigFromProto converts a wire message into a value independent of the SDK.
func ConfigFromProto(source *daedalusv1.Config) (daedalus.Config, error) {
	if source == nil {
		return daedalus.Config{}, fmt.Errorf("%w: missing config", daedalus.ErrInvalidConfig)
	}
	target := daedalus.Config{
		Width: source.Width, Height: source.Height, CellSize: source.CellSize,
		Seed: daedalus.Seed(source.Seed), MinDistance: source.MinDistance,
		MaxAttempts: source.MaxAttempts, MaxRooms: source.MaxRooms,
		CorridorOrder:  mapCorridorOrder(source.CorridorOrder),
		ExtraEdgeCount: source.ExtraEdgeCount,
	}

	target.RoomRoleRequests = make([]daedalus.RoomRoleRequest, len(source.RoomRoleRequests))
	for index, request := range source.RoomRoleRequests {
		if request == nil {
			return daedalus.Config{}, fmt.Errorf(
				"%w: room_role_requests[%d] missing", daedalus.ErrInvalidConfig, index,
			)
		}
		target.RoomRoleRequests[index] = daedalus.RoomRoleRequest{
			Role: mapRoomRole(request.Role), Count: request.Count,
			RequiredTags: append([]string(nil), request.RequiredTags...),
		}
	}

	target.DensityRegions = make([]daedalus.DensityRegion, len(source.DensityRegions))
	for index, region := range source.DensityRegions {
		if region == nil || region.Min == nil || region.Max == nil {
			return daedalus.Config{}, fmt.Errorf(
				"%w: density_regions[%d] incomplete", daedalus.ErrInvalidConfig, index,
			)
		}
		target.DensityRegions[index] = daedalus.DensityRegion{
			Min: cellFromProto(region.Min), Max: cellFromProto(region.Max),
			MinDistance: region.MinDistance,
		}
	}

	if source.RoomGeometry != nil {
		geometry, err := roomGeometryFromProto(source.RoomGeometry, source.Width, source.Height)
		if err != nil {
			return daedalus.Config{}, err
		}
		target.RoomGeometry = geometry
	}

	if source.CorridorGeometry != nil {
		geometry := source.CorridorGeometry
		target.CorridorGeometry = &daedalus.CorridorGeometry{
			Widths: make([]daedalus.CorridorWidthWeight, len(geometry.Widths)),
		}
		for index, weight := range geometry.Widths {
			if weight == nil {
				return daedalus.Config{}, fmt.Errorf(
					"%w: corridor_geometry.widths[%d] missing", daedalus.ErrInvalidConfig, index,
				)
			}
			target.CorridorGeometry.Widths[index] = daedalus.CorridorWidthWeight{
				Width: weight.Width, Weight: weight.Weight,
			}
		}
	}

	if source.PlantCatalog != nil {
		catalog, err := plantCatalogFromProto(source.PlantCatalog)
		if err != nil {
			return daedalus.Config{}, err
		}
		target.PlantCatalog = catalog
	}
	return target, nil
}

func plantCatalogFromProto(source *daedalusv1.PlantCatalog) (*daedalus.PlantCatalog, error) {
	target := &daedalus.PlantCatalog{
		Rooms:     make([]daedalus.RoomPlant, len(source.Rooms)),
		Corridors: make([]daedalus.CorridorPlant, len(source.Corridors)),
	}
	for index, plant := range source.Rooms {
		if plant == nil {
			return nil, fmt.Errorf("%w: plant_catalog.rooms[%d] missing", daedalus.ErrInvalidConfig, index)
		}
		target.Rooms[index] = daedalus.RoomPlant{
			ID: daedalus.PlantID(plant.Id), Tags: append([]string(nil), plant.Tags...),
			Weight: plant.Weight, DoorDirections: make([]daedalus.Direction, len(plant.DoorDirections)),
		}
		for directionIndex, direction := range plant.DoorDirections {
			target.Rooms[index].DoorDirections[directionIndex] = mapDirection(direction)
		}
	}
	for index, plant := range source.Corridors {
		if plant == nil {
			return nil, fmt.Errorf(
				"%w: plant_catalog.corridors[%d] missing", daedalus.ErrInvalidConfig, index,
			)
		}
		target.Corridors[index] = daedalus.CorridorPlant{
			ID: daedalus.PlantID(plant.Id), Tags: append([]string(nil), plant.Tags...),
			Weight: plant.Weight,
		}
	}
	return target, nil
}

// ConfigToProto converts an SDK Config into a wire message. A nil
// CorridorGeometry stays absent. A present value is copied even when Widths
// is empty, because that empty geometry is invalid and is not the one-Cell
// default.
func ConfigToProto(source daedalus.Config) *daedalusv1.Config {
	target := &daedalusv1.Config{
		Width: source.Width, Height: source.Height, CellSize: source.CellSize,
		Seed: uint64(source.Seed), MinDistance: source.MinDistance,
		MaxAttempts: source.MaxAttempts, MaxRooms: source.MaxRooms,
		CorridorOrder:    mapCorridorOrderToProto(source.CorridorOrder),
		ExtraEdgeCount:   source.ExtraEdgeCount,
		RoomRoleRequests: make([]*daedalusv1.RoomRoleRequest, len(source.RoomRoleRequests)),
		DensityRegions:   make([]*daedalusv1.DensityRegion, len(source.DensityRegions)),
		RoomGeometry:     roomGeometryToProto(source.RoomGeometry),
		CorridorGeometry: corridorGeometryToProto(source.CorridorGeometry),
		PlantCatalog:     plantCatalogToProto(source.PlantCatalog),
	}
	for index, request := range source.RoomRoleRequests {
		target.RoomRoleRequests[index] = &daedalusv1.RoomRoleRequest{
			Role: mapRoomRoleToProto(request.Role), Count: request.Count,
			RequiredTags: append([]string(nil), request.RequiredTags...),
		}
	}
	for index, region := range source.DensityRegions {
		target.DensityRegions[index] = &daedalusv1.DensityRegion{
			Min: cellToProto(region.Min), Max: cellToProto(region.Max),
			MinDistance: region.MinDistance,
		}
	}
	return target
}

func roomGeometryFromProto(source *daedalusv1.RoomGeometry, gridWidth, gridHeight uint32) (*daedalus.RoomGeometry, error) {
	target := &daedalus.RoomGeometry{
		MaxFootprintCells: source.MaxFootprintCells,
		MinRoomGap:        source.MinRoomGap,
		Shapes:            make([]daedalus.RoomShapeWeight, len(source.Shapes)),
	}
	for index, weight := range source.Shapes {
		if weight == nil {
			return nil, fmt.Errorf(
				"%w: room_geometry.shapes[%d] missing", daedalus.ErrInvalidConfig, index,
			)
		}
		shape := mapRoomShape(weight.Shape)
		width, height, known := daedalus.DefaultDimensionRanges(shape, gridWidth, gridHeight)
		if weight.Width != nil {
			width = daedalus.DimensionRange{Min: weight.Width.Min, Max: weight.Width.Max}
		} else if !known {
			width = daedalus.DimensionRange{}
		}
		if weight.Height != nil {
			height = daedalus.DimensionRange{Min: weight.Height.Min, Max: weight.Height.Max}
		} else if !known {
			height = daedalus.DimensionRange{}
		}
		target.Shapes[index] = daedalus.RoomShapeWeight{
			Shape: shape, Weight: weight.Weight, Width: width, Height: height,
		}
	}
	return target, nil
}

func roomGeometryToProto(source *daedalus.RoomGeometry) *daedalusv1.RoomGeometry {
	if source == nil {
		return nil
	}
	target := &daedalusv1.RoomGeometry{
		MaxFootprintCells: source.MaxFootprintCells, MinRoomGap: source.MinRoomGap,
		Shapes: make([]*daedalusv1.RoomShapeWeight, len(source.Shapes)),
	}
	for index, weight := range source.Shapes {
		target.Shapes[index] = &daedalusv1.RoomShapeWeight{
			Shape: mapRoomShapeToProto(weight.Shape), Weight: weight.Weight,
			Width:  &daedalusv1.DimensionRange{Min: weight.Width.Min, Max: weight.Width.Max},
			Height: &daedalusv1.DimensionRange{Min: weight.Height.Min, Max: weight.Height.Max},
		}
	}
	return target
}

func corridorGeometryToProto(source *daedalus.CorridorGeometry) *daedalusv1.CorridorGeometry {
	if source == nil {
		return nil
	}
	target := &daedalusv1.CorridorGeometry{
		Widths: make([]*daedalusv1.CorridorWidthWeight, len(source.Widths)),
	}
	for index, weight := range source.Widths {
		target.Widths[index] = &daedalusv1.CorridorWidthWeight{
			Width: weight.Width, Weight: weight.Weight,
		}
	}
	return target
}

func plantCatalogToProto(source *daedalus.PlantCatalog) *daedalusv1.PlantCatalog {
	if source == nil {
		return nil
	}
	target := &daedalusv1.PlantCatalog{
		Rooms:     make([]*daedalusv1.RoomPlant, len(source.Rooms)),
		Corridors: make([]*daedalusv1.CorridorPlant, len(source.Corridors)),
	}
	for index, plant := range source.Rooms {
		target.Rooms[index] = &daedalusv1.RoomPlant{
			Id: string(plant.ID), Tags: append([]string(nil), plant.Tags...),
			Weight: plant.Weight, DoorDirections: make([]daedalusv1.Direction, len(plant.DoorDirections)),
		}
		for directionIndex, direction := range plant.DoorDirections {
			target.Rooms[index].DoorDirections[directionIndex] = mapDirectionToProto(direction)
		}
	}
	for index, plant := range source.Corridors {
		target.Corridors[index] = &daedalusv1.CorridorPlant{
			Id: string(plant.ID), Tags: append([]string(nil), plant.Tags...),
			Weight: plant.Weight,
		}
	}
	return target
}

// LayoutToProto converts the complete result into a message owned by the
// request, preserving optional fields.
func LayoutToProto(source daedalus.Layout) *daedalusv1.Layout {
	target := &daedalusv1.Layout{
		Seed: uint64(source.Seed),
		Grid: &daedalusv1.Grid{
			Width: source.Grid.Width, Height: source.Grid.Height,
			CellSize: source.Grid.CellSize,
			Cells:    make([]*daedalusv1.CellState, len(source.Grid.Cells)),
		},
		Rooms:     make([]*daedalusv1.Room, len(source.Rooms)),
		Corridors: make([]*daedalusv1.Corridor, len(source.Corridors)),
		Doors:     make([]*daedalusv1.Door, len(source.Doors)),
	}
	for index, cellState := range source.Grid.Cells {
		target.Grid.Cells[index] = cellStateToProto(cellState)
	}
	for index, room := range source.Rooms {
		target.Rooms[index] = roomToProto(room)
	}
	for index, corridor := range source.Corridors {
		target.Corridors[index] = corridorToProto(corridor)
	}
	for index, door := range source.Doors {
		target.Doors[index] = doorToProto(door)
	}
	return target
}

func cellStateToProto(source daedalus.CellState) *daedalusv1.CellState {
	target := &daedalusv1.CellState{
		At: cellToProto(source.At), Kind: mapCellKindToProto(source.Kind),
		CorridorIds: make([]uint32, len(source.CorridorIDs)),
	}
	if source.RoomID != nil {
		roomID := uint32(*source.RoomID)
		target.RoomId = &roomID
	}
	for index, corridorID := range source.CorridorIDs {
		target.CorridorIds[index] = uint32(corridorID)
	}
	return target
}

func roomToProto(source daedalus.Room) *daedalusv1.Room {
	target := &daedalusv1.Room{
		Id: uint32(source.ID), At: cellToProto(source.At),
		PlantId: string(source.PlantID), Tags: append([]string(nil), source.Tags...),
		DoorIds: make([]uint32, len(source.DoorIDs)), Shape: mapRoomShapeToProto(source.Shape),
		Origin: cellToProto(source.Origin), Width: source.Width, Height: source.Height,
		Cells: make([]*daedalusv1.Cell, len(source.Cells)),
	}
	if source.Role != nil {
		role := mapRoomRoleToProto(*source.Role)
		target.Role = &role
	}
	for index, doorID := range source.DoorIDs {
		target.DoorIds[index] = uint32(doorID)
	}
	for index, cell := range source.Cells {
		target.Cells[index] = cellToProto(cell)
	}
	return target
}

func corridorToProto(source daedalus.Corridor) *daedalusv1.Corridor {
	target := &daedalusv1.Corridor{
		Id: uint32(source.ID), FromRoomId: uint32(source.FromRoomID),
		ToRoomId: uint32(source.ToRoomID), FromDoorId: uint32(source.FromDoorID),
		ToDoorId: uint32(source.ToDoorID), Cells: make([]*daedalusv1.Cell, len(source.Cells)),
		Centerline: make([]*daedalusv1.Cell, len(source.Centerline)),
		PlantId:    string(source.PlantID), Tags: append([]string(nil), source.Tags...),
	}
	for index, cell := range source.Cells {
		target.Cells[index] = cellToProto(cell)
	}
	for index, cell := range source.Centerline {
		target.Centerline[index] = cellToProto(cell)
	}
	return target
}

func doorToProto(source daedalus.Door) *daedalusv1.Door {
	target := &daedalusv1.Door{
		Id: uint32(source.ID), RoomId: uint32(source.RoomID), At: cellToProto(source.At),
		Direction:   mapDirectionToProto(source.Direction),
		Span:        source.Span,
		CorridorIds: make([]uint32, len(source.CorridorIDs)),
	}
	for index, corridorID := range source.CorridorIDs {
		target.CorridorIds[index] = uint32(corridorID)
	}
	return target
}

func cellFromProto(source *daedalusv1.Cell) daedalus.Cell {
	return daedalus.Cell{X: source.X, Y: source.Y}
}

func cellToProto(source daedalus.Cell) *daedalusv1.Cell {
	return &daedalusv1.Cell{X: source.X, Y: source.Y}
}

func mapCorridorOrder(source daedalusv1.CorridorOrder) daedalus.CorridorOrder {
	switch source {
	case daedalusv1.CorridorOrder_CORRIDOR_ORDER_UNSPECIFIED,
		daedalusv1.CorridorOrder_CORRIDOR_ORDER_X_THEN_Y:
		return daedalus.CorridorOrderXThenY
	case daedalusv1.CorridorOrder_CORRIDOR_ORDER_Y_THEN_X:
		return daedalus.CorridorOrderYThenX
	default:
		return invalidCorridorOrder
	}
}

func mapCorridorOrderToProto(source daedalus.CorridorOrder) daedalusv1.CorridorOrder {
	switch source {
	case daedalus.CorridorOrderYThenX:
		return daedalusv1.CorridorOrder_CORRIDOR_ORDER_Y_THEN_X
	case daedalus.CorridorOrderXThenY:
		return daedalusv1.CorridorOrder_CORRIDOR_ORDER_X_THEN_Y
	default:
		return daedalusv1.CorridorOrder_CORRIDOR_ORDER_UNSPECIFIED
	}
}

func mapRoomRole(source daedalusv1.RoomRole) daedalus.RoomRole {
	switch source {
	case daedalusv1.RoomRole_ROOM_ROLE_START:
		return daedalus.RoomRoleStart
	case daedalusv1.RoomRole_ROOM_ROLE_BOSS:
		return daedalus.RoomRoleBoss
	case daedalusv1.RoomRole_ROOM_ROLE_TREASURE:
		return daedalus.RoomRoleTreasure
	default:
		return invalidRoomRole
	}
}

func mapRoomRoleToProto(source daedalus.RoomRole) daedalusv1.RoomRole {
	switch source {
	case daedalus.RoomRoleStart:
		return daedalusv1.RoomRole_ROOM_ROLE_START
	case daedalus.RoomRoleBoss:
		return daedalusv1.RoomRole_ROOM_ROLE_BOSS
	case daedalus.RoomRoleTreasure:
		return daedalusv1.RoomRole_ROOM_ROLE_TREASURE
	default:
		return daedalusv1.RoomRole_ROOM_ROLE_UNSPECIFIED
	}
}

func mapDirection(source daedalusv1.Direction) daedalus.Direction {
	switch source {
	case daedalusv1.Direction_DIRECTION_NORTH:
		return daedalus.DirectionNorth
	case daedalusv1.Direction_DIRECTION_EAST:
		return daedalus.DirectionEast
	case daedalusv1.Direction_DIRECTION_SOUTH:
		return daedalus.DirectionSouth
	case daedalusv1.Direction_DIRECTION_WEST:
		return daedalus.DirectionWest
	default:
		return invalidDirection
	}
}

func mapDirectionToProto(source daedalus.Direction) daedalusv1.Direction {
	switch source {
	case daedalus.DirectionNorth:
		return daedalusv1.Direction_DIRECTION_NORTH
	case daedalus.DirectionEast:
		return daedalusv1.Direction_DIRECTION_EAST
	case daedalus.DirectionSouth:
		return daedalusv1.Direction_DIRECTION_SOUTH
	case daedalus.DirectionWest:
		return daedalusv1.Direction_DIRECTION_WEST
	default:
		return daedalusv1.Direction_DIRECTION_UNSPECIFIED
	}
}

func mapRoomShape(source daedalusv1.RoomShape) daedalus.RoomShape {
	switch source {
	case daedalusv1.RoomShape_ROOM_SHAPE_RECTANGLE:
		return daedalus.RoomShapeRectangle
	case daedalusv1.RoomShape_ROOM_SHAPE_L:
		return daedalus.RoomShapeL
	case daedalusv1.RoomShape_ROOM_SHAPE_T:
		return daedalus.RoomShapeT
	case daedalusv1.RoomShape_ROOM_SHAPE_CROSS:
		return daedalus.RoomShapeCross
	case daedalusv1.RoomShape_ROOM_SHAPE_CIRCLE:
		return daedalus.RoomShapeCircle
	default:
		return invalidRoomShape
	}
}

func mapRoomShapeToProto(source daedalus.RoomShape) daedalusv1.RoomShape {
	switch source {
	case daedalus.RoomShapeRectangle:
		return daedalusv1.RoomShape_ROOM_SHAPE_RECTANGLE
	case daedalus.RoomShapeL:
		return daedalusv1.RoomShape_ROOM_SHAPE_L
	case daedalus.RoomShapeT:
		return daedalusv1.RoomShape_ROOM_SHAPE_T
	case daedalus.RoomShapeCross:
		return daedalusv1.RoomShape_ROOM_SHAPE_CROSS
	case daedalus.RoomShapeCircle:
		return daedalusv1.RoomShape_ROOM_SHAPE_CIRCLE
	default:
		return daedalusv1.RoomShape_ROOM_SHAPE_UNSPECIFIED
	}
}

func mapCellKindToProto(source daedalus.CellKind) daedalusv1.CellKind {
	switch source {
	case daedalus.CellKindEmpty:
		return daedalusv1.CellKind_CELL_KIND_EMPTY
	case daedalus.CellKindRoom:
		return daedalusv1.CellKind_CELL_KIND_ROOM
	case daedalus.CellKindCorridor:
		return daedalusv1.CellKind_CELL_KIND_CORRIDOR
	default:
		return daedalusv1.CellKind_CELL_KIND_UNSPECIFIED
	}
}
