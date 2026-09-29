package daedalus

// Seed is the source of all deterministic random streams for a generation
// request. It is an unsigned 64-bit integer and all values are accepted: zero
// is valid and does not mean "random". The same Seed combined with the same
// effective Config produces the same Layout throughout major version v1 on
// every supported platform.
type Seed uint64

// RoomID identifies a Room within a Layout. IDs are stable indices in creation
// order, starting at 0, and their canonical order is ascending numeric order.
type RoomID uint32

// CorridorID identifies a Corridor within a Layout. IDs are stable indices in
// creation order, starting at 0, and their canonical order is ascending numeric
// order.
type CorridorID uint32

// DoorID identifies a Door within a Layout. IDs are stable indices in creation
// order, starting at 0, and their canonical order is ascending numeric order.
type DoorID uint32

// PlantID is an opaque asset identifier resolved by the calling game; Daedalus
// never interprets it. When present, it is a non-empty UTF-8 string compared
// byte by byte and case-sensitively. The zero value (an empty string) means no
// associated Plant.
type PlantID string

// Cell is an integer (X, Y) Grid coordinate, never a pixel position. A Cell is
// valid only within a Grid: 0 ≤ X < Width and 0 ≤ Y < Height. The canonical
// Cell order is Y then X.
type Cell struct {
	// X is the zero-based horizontal Cell coordinate.
	X int32
	// Y is the zero-based vertical Cell coordinate.
	Y int32
}

// Direction is one of the Grid's four cardinal directions. Diagonals are
// invalid. The canonical order is North, East, South, West, with values
// increasing from zero; the zero value is therefore DirectionNorth.
type Direction int

const (
	// DirectionNorth points toward decreasing Y: vector (0, -1).
	DirectionNorth Direction = iota
	// DirectionEast points toward increasing X: vector (1, 0).
	DirectionEast
	// DirectionSouth points toward increasing Y: vector (0, 1).
	DirectionSouth
	// DirectionWest points toward decreasing X: vector (-1, 0).
	DirectionWest
)

// Delta returns the Direction's unit vector in Cell coordinates: North=(0,-1),
// East=(1,0), South=(0,1), West=(-1,0). A Direction outside the declared values
// returns the zero Cell; callers must use only the type's four constants.
func (d Direction) Delta() Cell {
	switch d {
	case DirectionNorth:
		return Cell{X: 0, Y: -1}
	case DirectionEast:
		return Cell{X: 1, Y: 0}
	case DirectionSouth:
		return Cell{X: 0, Y: 1}
	case DirectionWest:
		return Cell{X: -1, Y: 0}
	}
	return Cell{}
}

// Opposite returns the opposite Direction: North↔South and East↔West. Applied
// twice, it returns the original Direction. A Direction outside the declared
// values returns DirectionNorth; callers must use only the type's four constants.
func (d Direction) Opposite() Direction {
	switch d {
	case DirectionNorth:
		return DirectionSouth
	case DirectionSouth:
		return DirectionNorth
	case DirectionEast:
		return DirectionWest
	case DirectionWest:
		return DirectionEast
	}
	return DirectionNorth
}

// CellKind is the physical state of a Grid Cell. The zero value is CellKindEmpty.
type CellKind int

const (
	// CellKindEmpty marks a free Cell with no Room or Corridor.
	CellKindEmpty CellKind = iota
	// CellKindRoom marks a Cell occupied by exactly one Room.
	CellKindRoom
	// CellKindCorridor marks an internal Cell of one or more Corridors.
	CellKindCorridor
)

// CellState is the physical state of a Cell in the generated Grid.
type CellState struct {
	// At is this Cell's coordinate in the Grid.
	At Cell
	// Kind is the Cell's physical state: Empty, Room, or Corridor.
	Kind CellKind
	// RoomID references the Room occupying the Cell. It is non-nil if and only
	// if Kind == CellKindRoom; otherwise it is nil and must not be read.
	RoomID *RoomID
	// CorridorIDs lists, in ascending order, all Corridors containing this Cell.
	// It is non-empty if and only if Kind == CellKindCorridor; a Corridor Cell
	// may be shared by more than one Corridor.
	CorridorIDs []CorridorID
}

// CorridorOrder selects the preferred bend for a Corridor's orthogonal L-route.
// The zero value is CorridorOrderXThenY, which is also the Config default.
type CorridorOrder int

const (
	// CorridorOrderXThenY traces the X axis first and then the Y axis: for a
	// connection from A to B, the bend is (B.X, A.Y).
	CorridorOrderXThenY CorridorOrder = iota
	// CorridorOrderYThenX traces the Y axis first and then the X axis: for a
	// connection from A to B, the bend is (A.X, B.Y).
	CorridorOrderYThenX
)

// RoomRole is a Room's optional thematic role, assigned deterministically over
// the backbone tree from Config.RoomRoleRequests.
type RoomRole int

const (
	// RoomRoleStart marks the starting Room. It always receives RoomID 0, the
	// first Room accepted by placement.
	RoomRoleStart RoomRole = iota
	// RoomRoleBoss marks the boss Room: the unassigned Room farthest from Start
	// by weighted path distance in the tree. It requires a RoomRoleStart request
	// in the same Config.
	RoomRoleBoss
	// RoomRoleTreasure marks treasure Rooms. Each one is the unassigned Room
	// whose minimum weighted path distance to the anchors is greatest. The
	// anchors are the Start and Boss Rooms already assigned, plus every
	// Treasure already placed. Equal distances take the smaller RoomID. With
	// no Start and no Boss, the first Treasure is farthest from RoomID 0.
	RoomRoleTreasure
)
