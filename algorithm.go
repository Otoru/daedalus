package daedalus

import "context"

// Placer is the algorithm that proposes Room placements for a generation
// request. It is one of only two Strategy interfaces in v1; functions and
// closures satisfy it idiomatically, without class hierarchies or factories.
//
// Placer returns only RoomPlacements: it assigns no Plants, Doors, or IDs and
// does not mutate Layout. The built-in Generator uses poisson_disk_rooms_v1. A
// game-injected implementation is the implementer's responsibility:
// determinism, cancellation through PlacementRequest.Context, and concurrency
// safety belong to the plugin; a Placer shared between goroutines must be safe
// for simultaneous calls.
type Placer interface {
	// Place proposes the request's Rooms. It must return valid placements within
	// the request's Grid bounds, or an error. It must observe req.Context and
	// abandon work when canceled.
	Place(req PlacementRequest) ([]RoomPlacement, error)
}

// PlacerFunc adapts an ordinary function to Placer, so a closure can be
// injected wherever the interface is expected, without declaring a type:
//
//	generator := daedalus.Generator{Placer: daedalus.PlacerFunc(placeInAGrid)}
//
// The obligations of Placer are the function's: bounds, determinism,
// cancellation through req.Context, and safety under concurrent calls.
type PlacerFunc func(req PlacementRequest) ([]RoomPlacement, error)

// Place calls the adapted function.
func (place PlacerFunc) Place(req PlacementRequest) ([]RoomPlacement, error) {
	return place(req)
}

// RoomPlacement describes a proposed Room, with local Cells relative to Origin
// and ordered by the canonical mask.
type RoomPlacement struct {
	// Shape is the footprint's canonical discrete mask.
	Shape RoomShape
	// Origin is the bounding box's top-left corner.
	Origin Cell
	// Width is the bounding-box width in Cells.
	Width uint32
	// Height is the bounding-box height in Cells.
	Height uint32
	// Cells contains the mask's canonical local offsets in Y/X order.
	Cells []Cell
}

// PlacementRequest carries all variable data for a Room-placement request.
type PlacementRequest struct {
	// Context carries caller cancellation and deadline. The Placer must check it
	// at phase boundaries and in long loops, and never return a partial result
	// after cancellation.
	Context context.Context
	// Width is the target Grid width in Cells.
	Width uint32
	// Height is the target Grid height in Cells.
	Height uint32
	// MinDistance is the minimum Euclidean distance, in Cells, between Room
	// centers outside every DensityRegion.
	MinDistance float64
	// DensityRegions lists the Config's already validated density regions; empty
	// means uniform MinDistance.
	DensityRegions []DensityRegion
	// RoomGeometry contains geometry already normalized for this request.
	RoomGeometry RoomGeometry
	// MaxAttempts is the maximum number of candidates per active point.
	MaxAttempts uint32
	// MaxRooms is the maximum number of accepted Rooms; placement stops when reached.
	MaxRooms uint32
	// Seed is the source of the request's placement random stream.
	Seed Seed
	// geometryCombinations preserves the normalized, canonically ordered list
	// from Config validation. The built-in Placer receives it from Generator
	// without recomputing combinations or depending on map iteration.
	//
	// The field is deliberately private: a game-written Placer does not need it
	// because RoomGeometry arrives normalized and the combinations can be derived
	// from it. Only built-in algorithms use this shortcut, and only Generator
	// populates it.
	geometryCombinations []roomGeometryCombination
}

// Connector is the algorithm that chooses Room-to-Room edges for a generation
// request. It is one of only two Strategy interfaces in v1; functions and
// closures satisfy it idiomatically.
//
// Connector returns only edges: it does not route Cells or mutate Layout. The
// result must be a simple graph that makes every Room reachable; Generator
// rejects duplicate, self, unknown, or disconnected Connections. The final
// result contains between n-1 and n-1+ConnectionRequest.ExtraEdgeCount edges;
// with ExtraEdgeCount == 0 Generator rejects every cycle and requires a tree.
// The built-in Connector is prim_rooms_v1, which returns the backbone tree;
// ExtraEdgeCount shortcuts are added by Generator during the cycle phase, not
// by Connector. A game-injected implementation is responsible for its own
// determinism, cancellation, and concurrency safety.
type Connector interface {
	// Connect chooses the request's Room-to-Room edges, or returns an error. It
	// must observe req.Context and abandon work when canceled.
	Connect(req ConnectionRequest) ([]Connection, error)
}

// ConnectorFunc adapts an ordinary function to Connector, so a closure can be
// injected wherever the interface is expected, without declaring a type:
//
//	generator := daedalus.Generator{Connector: daedalus.ConnectorFunc(connectInOrder)}
//
// The obligations of Connector are the function's: a valid edge set,
// determinism, cancellation through req.Context, and safety under concurrent
// calls.
type ConnectorFunc func(req ConnectionRequest) ([]Connection, error)

// Connect calls the adapted function.
func (connect ConnectorFunc) Connect(req ConnectionRequest) ([]Connection, error) {
	return connect(req)
}

// PlacedRoom describes an already positioned Room, including anchor, mask,
// bounding box, and absolute footprint. Sequence order is canonical RoomID order.
type PlacedRoom struct {
	// ID is the stable Room identifier, in creation order.
	ID RoomID
	// At is the first occupied footprint Cell in canonical Y/X order.
	At Cell
	// Shape is the footprint's canonical discrete mask.
	Shape RoomShape
	// Origin is the bounding box's top-left corner.
	Origin Cell
	// Width is the bounding-box width in Cells.
	Width uint32
	// Height is the bounding-box height in Cells.
	Height uint32
	// Cells contains the absolute footprint, ordered by Y then X.
	Cells []Cell
}

// ConnectionRequest carries all variable data for a Room-connection request.
type ConnectionRequest struct {
	// Context carries caller cancellation and deadline. The Connector must check
	// it at phase boundaries and in long loops, and never return a partial result
	// after cancellation.
	Context context.Context
	// Rooms lists positioned Rooms in canonical RoomID order.
	Rooms []PlacedRoom
	// Width is the target Grid width in Cells.
	Width uint32
	// Height is the target Grid height in Cells.
	Height uint32
	// ExtraEdgeCount is the maximum number of discarded short edges Generator may
	// reintroduce after the backbone; 0 requires the result to be a tree.
	ExtraEdgeCount uint32
	// Seed is the source of the request's connection random stream. The built-in
	// prim_rooms_v1 Connector receives it but consumes no draw.
	Seed Seed
	// MaxCorridorWidth is the widest Corridor width declared for this request.
	// Zero means one Cell, the same budget as a nil CorridorGeometry. The
	// built-in Connector will not put more openings on a Room than that width
	// can keep Chebyshev-separated, and a game-supplied Connector needs the
	// same number to apply the same limit.
	MaxCorridorWidth uint32
}

// Connection is a topological edge chosen by Connector between two distinct Rooms.
type Connection struct {
	// FromRoomID is the edge's source Room; always distinct from ToRoomID.
	FromRoomID RoomID
	// ToRoomID is the edge's destination Room; always distinct from FromRoomID.
	ToRoomID RoomID
}
