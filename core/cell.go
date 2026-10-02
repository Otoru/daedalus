package core

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
