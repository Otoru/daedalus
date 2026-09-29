package daedalus

// Layout is the complete, immutable, successfully generated result of a dungeon
// request. It describes geometry, not process input: it does not repeat Config
// tuning, only the Seed and resulting Grid.
//
// Generator-guaranteed invariants: at least one Room exists; every Room.At is
// distinct; with n Rooms there are at least n-1 Corridors if n > 1 and zero if
// n == 1; the Room/Corridor graph is connected (a tree when
// Config.ExtraEdgeCount == 0); all slices, including nested ones, are allocated
// per request, so mutating one Layout never mutates another request's result.
type Layout struct {
	// Seed is the effective Seed that produced this Layout.
	Seed Seed
	// Grid is the Layout's physical Cell space.
	Grid Grid
	// Rooms lists Rooms in canonical RoomID order.
	Rooms []Room
	// Corridors lists Corridors in canonical CorridorID order.
	Corridors []Corridor
	// Doors lists Doors in canonical DoorID order.
	Doors []Door
}

// Grid is a Layout's rectangular Width × Height Cell space, where
// 0 ≤ X < Width and 0 ≤ Y < Height.
type Grid struct {
	// Width is the Grid width in Cells, in the range 1..256.
	Width uint32
	// Height is the Grid height in Cells, in the range 1..256.
	Height uint32
	// CellSize is one Cell's size in opaque caller-defined units, copied from
	// Config only for output; no algorithm converts it to pixels.
	CellSize float64
	// Cells contains exactly Width × Height states in canonical order (Y then X):
	// the state of (x, y) is at Cells[y*Width+x].
	Cells []CellState
	// Terrain is an optional palette-indexed overlay. It is nil when no terrain
	// was requested, preserving the historical Layout representation.
	Terrain *TerrainLayer `json:",omitempty"`
}

// Room is a topological Layout vertex and contains the Room mask's absolute
// footprint in canonical Y/X order.
type Room struct {
	// ID is the stable Room identifier, in creation order.
	ID RoomID
	// At is the first occupied footprint Cell in canonical Y/X order.
	At Cell
	// Shape is the Room's canonical mask.
	Shape RoomShape
	// Origin is the Room bounding box's top-left corner.
	Origin Cell
	// Width is the bounding-box width in Cells.
	Width uint32
	// Height is the bounding-box height in Cells.
	Height uint32
	// Cells is the Room's absolute footprint, ordered by Y then X.
	Cells []Cell
	// Role is the Room's thematic role, or nil when Config.RoomRoleRequests is
	// empty or no role was assigned to this Room. No Room receives more than one
	// Role in v1.
	Role *RoomRole
	// PlantID is the asset metadata selected from the catalog, or the empty
	// string when Config.PlantCatalog is absent. It never changes topology.
	PlantID PlantID
	// Tags is the canonical copy of the selected Plant's tags; empty when there
	// is no catalog or the Plant declares no tags.
	Tags []string
	// DoorIDs lists the Room's openings in Cell (Y, X) then Direction order; it
	// may be empty when the Room has no Corridors.
	DoorIDs []DoorID
}

// Corridor is a topological edge between two Rooms and its ordered internal
// orthogonal Cells. Corridors are logical edges over the physical Grid.Cells
// space; Corridor Cells may be shared between Corridors.
type Corridor struct {
	// ID is the stable Corridor identifier, in creation order.
	ID CorridorID
	// FromRoomID is the edge's source Room; always distinct from ToRoomID.
	FromRoomID RoomID
	// ToRoomID is the edge's destination Room; always distinct from FromRoomID.
	ToRoomID RoomID
	// FromDoorID is the Door used in the source Room.
	FromDoorID DoorID
	// ToDoorID is the Door used in the destination Room.
	ToDoorID DoorID
	// Centerline is the ordered route from the From side to the To side,
	// excluding the endpoint Rooms' Cells. It is 4-connected and empty when the
	// Rooms are adjacent. It is always present, whatever the routed width; at
	// width 1 it holds the same Cells as the band.
	Centerline []Cell
	// Cells is the full occupied band in row-major order (Y then X). At width 1
	// the band is the route itself, so Cells equals Centerline. Above that,
	// every bend includes its W×W block, so the region stays 4-connected.
	Cells []Cell
	// PlantID is the asset metadata selected from the catalog, or the empty
	// string when Config.PlantCatalog is absent. It never changes topology.
	PlantID PlantID
	// Tags is the canonical copy of the selected Plant's tags; empty when there
	// is no catalog or the Plant declares no tags.
	Tags []string
}

// Door is a Room's logical opening identified by a Cell and cardinal Direction.
// A Door is unique by (RoomID, At, Direction), even when multiple edges use it.
type Door struct {
	// ID is the stable Door identifier, in creation order.
	ID DoorID
	// RoomID is the Room to which this opening belongs.
	RoomID RoomID
	// At is a boundary Cell belonging to the owning Room's footprint. With
	// Span 1 it is the opening Cell. With a wider Span it is the Cell of the
	// span with the smallest (Y, X).
	At Cell
	// Direction is the direction of the Corridor's first internal step away from
	// the Room; with empty Cells, it is the direction between endpoints.
	Direction Direction
	// Span is the number of boundary Cells in this opening, and is never zero:
	// a one-Cell doorway spans 1. It equals the width the Corridor was routed
	// at, which is 1 whenever Config.CorridorGeometry is nil.
	Span uint32
	// CorridorIDs lists, in ascending order, one or more edges using this opening.
	CorridorIDs []CorridorID
}
