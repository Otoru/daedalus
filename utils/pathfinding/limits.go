package pathfinding

// Ceilings for one navigation call. Grid dimensions are not restated here:
// a CostGrid reuses daedalus.MaxCells, and a zero dimension is a malformed
// grid rather than a breached ceiling.
const (
	// MaxSources is the maximum number of goal Cells in one query. It equals
	// daedalus.MaxFootprintCells, so a whole Room fits as a goal.
	MaxSources = 4096

	// MaxQueries is the maximum number of goal sets in one call.
	MaxQueries = 16

	// MaxStepsPerCall is the maximum number of steps one call may take.
	MaxStepsPerCall = 16384
)
