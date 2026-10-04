package gating

import (
	"container/heap"
	"context"
	"fmt"

	daedalus "github.com/Otoru/daedalus"
)

const keyPlacementSalt uint64 = 0x9e3779b97f4a7c15

func placeKeys(ctx context.Context, g *graph, selected []selectedGate, seed daedalus.Seed, startRoom int) ([]daedalus.RoomID, error) {
	keys := make([]daedalus.RoomID, len(selected))
	used := make([]bool, len(g.layout.Rooms))
	for index, gate := range selected {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		stage := gate.stage
		if gate.kind == GateKindMain {
			stage = index
		}
		locked := lockedForKeyStage(g, selected, stage)
		reachable := floodRooms(g, locked, startRoom)
		distances := roomDistances(g, locked, startRoom)
		best, reachableCount, unusedCount := bestKeyRoom(reachable, used, distances, seed, index)
		if best < 0 {
			return nil, fmt.Errorf("%w: no distinct reachable key room for gate %d (reachable=%d unused=%d)", daedalus.ErrInsufficientGates, index, reachableCount, unusedCount)
		}
		used[best] = true
		keys[index] = daedalus.RoomID(best)
	}
	return keys, nil
}

func lockedForKeyStage(g *graph, selected []selectedGate, stage int) []bool {
	locked := make([]bool, len(g.layout.Doors))
	for index, gate := range selected {
		if gate.kind != GateKindMain || index >= stage {
			locked[gate.door] = true
		}
	}
	return locked
}

func bestKeyRoom(reachable, used []bool, distances []uint64, seed daedalus.Seed, gateIndex int) (best, reachableCount, unusedCount int) {
	best = -1
	for room, isReachable := range reachable {
		if !isReachable {
			continue
		}
		reachableCount++
		if used[room] {
			continue
		}
		unusedCount++
		if best < 0 || distances[room] > distances[best] || (distances[room] == distances[best] && rank(seed, gateIndex, room) < rank(seed, gateIndex, best)) || (distances[room] == distances[best] && rank(seed, gateIndex, room) == rank(seed, gateIndex, best) && room < best) {
			best = room
		}
	}
	return
}

func rank(seed daedalus.Seed, gateID, room int) uint64 {
	value := uint64(seed)
	value ^= keyPlacementSalt
	shifted := uint64(gateID) << 32
	value ^= shifted
	value ^= uint64(room)
	value += 0x9e3779b97f4a7c15
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb
	return value ^ (value >> 31)
}

func floodRooms(g *graph, locked []bool, start int) []bool {
	reached := make([]bool, len(g.layout.Rooms))
	queue := []int{start}
	reached[start] = true
	for len(queue) > 0 {
		room := queue[0]
		queue = queue[1:]
		for _, ref := range g.adj[room] {
			corridor := g.layout.Corridors[ref.edge]
			if locked[corridor.FromDoorID] || locked[corridor.ToDoorID] || reached[ref.to] {
				continue
			}
			reached[ref.to] = true
			queue = append(queue, ref.to)
		}
	}
	return reached
}

func roomDistances(g *graph, locked []bool, start int) []uint64 {
	const unreachable = ^uint64(0)
	distance := make([]uint64, len(g.layout.Rooms))
	for i := range distance {
		distance[i] = unreachable
	}
	distance[start] = 0
	queue := &roomHeap{{room: start, distance: 0}}
	heap.Init(queue)
	for queue.Len() > 0 {
		item := heap.Pop(queue).(roomItem)
		if item.distance != distance[item.room] {
			continue
		}
		for _, ref := range g.adj[item.room] {
			corridor := g.layout.Corridors[ref.edge]
			if locked[corridor.FromDoorID] || locked[corridor.ToDoorID] {
				continue
			}
			next := item.distance + uint64(1) + uint64(len(corridor.Centerline))
			if next < distance[ref.to] {
				distance[ref.to] = next
				heap.Push(queue, roomItem{room: ref.to, distance: next})
			}
		}
	}
	return distance
}

func validatePlan(ctx context.Context, layout daedalus.Layout, plan Plan, startRoomID daedalus.RoomID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	g, err := buildGraph(ctx, layout)
	if err != nil {
		return err
	}
	if startRoomID >= daedalus.RoomID(len(layout.Rooms)) {
		return fmt.Errorf("%w: unknown start room %d", daedalus.ErrInvalidGating, startRoomID)
	}
	if uint64(len(plan.Gates)) > uint64(MaxGates) {
		return fmt.Errorf("%w: plan has too many gates", daedalus.ErrInvalidGating)
	}
	if err := validatePlanGates(ctx, g, plan, startRoomID); err != nil {
		return err
	}
	return oracleFixedPoint(ctx, g, plan, startRoomID)
}

func validatePlanGates(ctx context.Context, g *graph, plan Plan, startRoomID daedalus.RoomID) error {
	layout := g.layout
	seenDoor := make([]bool, len(layout.Doors))
	seenKey := make([]bool, len(layout.Rooms))
	mainDone := false
	for i, gate := range plan.Gates {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := validateGate(g, gate, i, &mainDone, seenDoor, seenKey); err != nil {
			return err
		}
	}
	for index := range plan.Gates {
		if plan.Gates[index].KeyRoomID == startRoomID && index > 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

func validateGate(g *graph, gate Gate, index int, mainDone *bool, seenDoor, seenKey []bool) error {
	if gate.ID != GateID(index) || (gate.Kind != GateKindMain && gate.Kind != GateKindOptional) || gate.DoorID >= daedalus.DoorID(len(seenDoor)) || gate.KeyRoomID >= daedalus.RoomID(len(seenKey)) {
		return fmt.Errorf("%w: gates[%d] has an invalid id, kind, door, or key", daedalus.ErrInvalidGating, index)
	}
	if gate.Kind == GateKindOptional {
		*mainDone = true
	} else if *mainDone {
		return fmt.Errorf("%w: main gates must precede optional gates", daedalus.ErrInvalidGating)
	}
	if seenDoor[gate.DoorID] || seenKey[gate.KeyRoomID] {
		return fmt.Errorf("%w: gate %d duplicates a Door or key Room", daedalus.ErrInvalidGating, index)
	}
	seenDoor[gate.DoorID], seenKey[gate.KeyRoomID] = true, true
	if len(g.layout.Doors[gate.DoorID].CorridorIDs) != 1 {
		return fmt.Errorf("%w: gate %d uses a shared Door", daedalus.ErrInvalidGating, index)
	}
	corridorID := int(g.layout.Doors[gate.DoorID].CorridorIDs[0])
	if !g.bridges[corridorID] {
		return fmt.Errorf("%w: gate %d Door %d is not a final-graph bridge", daedalus.ErrInvalidGating, index, gate.DoorID)
	}
	return nil
}

func oracleFixedPoint(ctx context.Context, g *graph, plan Plan, startRoomID daedalus.RoomID) error {
	locked := make([]bool, len(g.layout.Doors))
	for _, gate := range plan.Gates {
		locked[gate.DoorID] = true
	}
	reached := make([]bool, len(g.layout.Rooms))
	collected := make([]bool, len(plan.Gates))
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		beforeReached, beforeCollected := countTrue(reached), countTrue(collected)
		collectReachableKeys(g, plan, startRoomID, locked, reached, collected)
		if countTrue(reached) == beforeReached && countTrue(collected) == beforeCollected {
			break
		}
	}
	return checkOracleCompletion(plan, reached, collected)
}

func collectReachableKeys(g *graph, plan Plan, startRoomID daedalus.RoomID, locked, reached, collected []bool) {
	current := floodRooms(g, locked, int(startRoomID))
	for room, ok := range current {
		reached[room] = reached[room] || ok
	}
	for i, gate := range plan.Gates {
		if !collected[i] && reached[gate.KeyRoomID] {
			collected[i] = true
			locked[gate.DoorID] = false
		}
	}
}

func checkOracleCompletion(plan Plan, reached, collected []bool) error {
	for room, ok := range reached {
		if !ok {
			return fmt.Errorf("%w: unreachable RoomID %d", daedalus.ErrInvalidGating, room)
		}
	}
	for i, ok := range collected {
		if !ok {
			gate := plan.Gates[i]
			return fmt.Errorf("%w: uncollected GateID %d (DoorID %d, KeyRoomID %d)", daedalus.ErrInvalidGating, gate.ID, gate.DoorID, gate.KeyRoomID)
		}
	}
	return nil
}

func countTrue(values []bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}
