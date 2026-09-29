// Package gating builds a detached, solvable progression overlay over a Layout.
//
// A gate locks one Door. Closing a Door blocks every Corridor named by that
// Door. Only final-graph bridges are eligible, so shortcuts and loops cannot
// produce decorative locks. The package imports only the root SDK and the
// standard library; it does not place content or retain state.
//
// Optional gates are thematic side-content gates: an optional request is
// satisfiable only when the Layout contains enough distinct Treasure Rooms in
// bridge subtrees outside the complete target path. A bridge on that path is
// protected even when it was not selected as a main gate. Keys also require
// distinct reachable Rooms at their stage. MainGateCount and
// OptionalGateCount are exact requests; Build returns ErrInsufficientGates
// when this topology or key-room budget cannot host them, rather than
// returning a partial Plan.
package gating
