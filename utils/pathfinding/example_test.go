package pathfinding_test

import (
	"context"
	"fmt"

	"github.com/Otoru/daedalus"
	"github.com/Otoru/daedalus/utils/pathfinding"
)

// Chase sources the quarry and steps downhill. The same read is flee,
// explore, and a weighted goal; only the field changes.
func Example_chase() {
	grid := pathfinding.CostGrid{Width: 4, Height: 1, Costs: []pathfinding.Cost{1, 1, 1, 1}}
	field, err := pathfinding.Compute(context.Background(), grid, []pathfinding.Source{
		{At: daedalus.Cell{X: 3}},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(stepLabel(field.Step(daedalus.Cell{X: 0})))

	// Output:
	// moved east 2
}

// Flee sources the threat, inverts by -12/10, and rescans before the same
// downhill step. The rescan, not a second algorithm, is what points the
// step away from the threat.
func Example_flee() {
	grid := pathfinding.CostGrid{Width: 4, Height: 1, Costs: []pathfinding.Cost{1, 1, 1, 1}}
	field, err := pathfinding.Compute(context.Background(), grid, []pathfinding.Source{
		{At: daedalus.Cell{X: 3}},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	if err := pathfinding.Flee(context.Background(), &field, grid, -12, 10); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(stepLabel(field.Step(daedalus.Cell{X: 1})))

	// Output:
	// moved west -3
}

// Explore sources the frontier that has not been visited yet. Stepping
// downhill walks toward the nearest of those cells.
func Example_explore() {
	grid := pathfinding.CostGrid{Width: 5, Height: 1, Costs: []pathfinding.Cost{1, 1, 1, 1, 1}}
	field, err := pathfinding.Compute(context.Background(), grid, []pathfinding.Source{
		{At: daedalus.Cell{X: 0}},
		{At: daedalus.Cell{X: 4}},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(stepLabel(field.Step(daedalus.Cell{X: 2})))

	// Output:
	// moved east 1
}

// Weighted goals are sources that do not share a bias. The near goal at the
// east carries a penalty, so the step leaves it for the far goal at the west.
// With equal biases the same cell steps east.
func Example_weightedGoals() {
	grid := pathfinding.CostGrid{Width: 5, Height: 1, Costs: []pathfinding.Cost{1, 1, 1, 1, 1}}
	weighted, err := pathfinding.Compute(context.Background(), grid, []pathfinding.Source{
		{At: daedalus.Cell{X: 0}},
		{At: daedalus.Cell{X: 4}, Bias: 10},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	equal, err := pathfinding.Compute(context.Background(), grid, []pathfinding.Source{
		{At: daedalus.Cell{X: 0}},
		{At: daedalus.Cell{X: 4}},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	from := daedalus.Cell{X: 3}
	fmt.Println(stepLabel(weighted.Step(from)))
	fmt.Println(stepLabel(equal.Step(from)))

	// Output:
	// moved west 2
	// moved east 0
}

func stepLabel(result pathfinding.StepResult) string {
	status := [...]string{"moved", "arrived", "unreachable", "outside", "blocked"}
	direction := [...]string{"north", "east", "south", "west"}
	if result.Status == pathfinding.StepStatusMoved {
		return fmt.Sprintf("%s %s %d", status[result.Status], direction[result.Direction], result.Distance)
	}
	return fmt.Sprintf("%s %d", status[result.Status], result.Distance)
}
