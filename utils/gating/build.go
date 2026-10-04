package gating

import (
	"context"
	"fmt"
	"sort"

	daedalus "github.com/Otoru/daedalus"
)

type selectedGate struct {
	kind  GateKind
	edge  treeEdge
	door  daedalus.DoorID
	stage int
}

func build(ctx context.Context, layout daedalus.Layout, request Request) (Plan, error) {
	var zero Plan
	if ctx == nil {
		ctx = context.Background()
	}
	count := uint64(request.MainGateCount) + uint64(request.OptionalGateCount)
	if count > uint64(MaxGates) {
		return zero, fmt.Errorf("%w: gate count exceeds %d", daedalus.ErrLimitExceeded, MaxGates)
	}
	g, err := buildGraph(ctx, layout)
	if err != nil {
		return zero, err
	}
	if request.StartRoomID >= daedalus.RoomID(len(layout.Rooms)) {
		return zero, fmt.Errorf("%w: unknown start room %d", daedalus.ErrInvalidGating, request.StartRoomID)
	}
	startBlock := g.blockOf[request.StartRoomID]
	target, err := chooseTarget(g, request)
	if err != nil {
		return zero, err
	}
	targetBlock := g.blockOf[target]
	main, err := selectMain(g, startBlock, targetBlock, request.MainGateCount)
	if err != nil {
		return zero, err
	}
	mainPath, err := targetPath(g, startBlock, targetBlock)
	if err != nil {
		return zero, err
	}
	optional, err := selectOptional(g, startBlock, main, mainPath, request.OptionalGateCount)
	if err != nil {
		return zero, err
	}
	selected := append(main, optional...)
	keys, err := placeKeys(ctx, g, selected, request.Seed, int(request.StartRoomID))
	if err != nil {
		return zero, err
	}
	plan := Plan{Seed: request.Seed, Gates: make([]Gate, len(selected))}
	for i, gate := range selected {
		plan.Gates[i] = Gate{ID: GateID(i), Kind: gate.kind, DoorID: gate.door, KeyRoomID: keys[i]}
	}
	if err := Validate(ctx, layout, plan, request.StartRoomID); err != nil {
		if err == context.Canceled || err == context.DeadlineExceeded {
			return zero, err
		}
		return zero, fmt.Errorf("%w: Build produced an invalid plan: %v", daedalus.ErrInvalidGating, err)
	}
	return plan, nil
}

func chooseTarget(g *graph, request Request) (daedalus.RoomID, error) {
	if request.TargetRoomID != nil {
		if *request.TargetRoomID >= daedalus.RoomID(len(g.layout.Rooms)) {
			return 0, fmt.Errorf("%w: unknown target room %d", daedalus.ErrInvalidGating, *request.TargetRoomID)
		}
		return *request.TargetRoomID, nil
	}
	foundBoss := -1
	for i, room := range g.layout.Rooms {
		if room.Role != nil && *room.Role == daedalus.RoomRoleBoss {
			if foundBoss >= 0 {
				return 0, fmt.Errorf("%w: multiple Boss rooms", daedalus.ErrInvalidGating)
			}
			foundBoss = i
		}
	}
	if foundBoss >= 0 {
		return daedalus.RoomID(foundBoss), nil
	}
	return chooseFarthestLeaf(g, request.StartRoomID)
}

func chooseFarthestLeaf(g *graph, startRoomID daedalus.RoomID) (daedalus.RoomID, error) {
	startBlock := g.blockOf[startRoomID]
	_, _, distance := rootedTree(g, startBlock)
	bestRoom := int(startRoomID)
	bestDistance := uint64(0)
	for block, rooms := range g.blocks {
		if block == startBlock {
			continue
		}
		if len(g.blockAdj[block]) > 1 {
			continue
		}
		candidate := rooms[0]
		for _, room := range rooms[1:] {
			if room < candidate {
				candidate = room
			}
		}
		if distance[block] > bestDistance || (distance[block] == bestDistance && candidate < bestRoom) {
			bestRoom, bestDistance = candidate, distance[block]
		}
	}
	return daedalus.RoomID(bestRoom), nil
}

func rootedTree(g *graph, start int) ([]int, []int, []uint64) {
	parent := make([]int, len(g.blocks))
	edge := make([]int, len(g.blocks))
	distance := make([]uint64, len(g.blocks))
	for i := range parent {
		parent[i], edge[i] = -1, -1
	}
	queue := []int{start}
	parent[start] = start
	for len(queue) > 0 {
		block := queue[0]
		queue = queue[1:]
		for _, next := range g.blockAdj[block] {
			if parent[next.to] >= 0 {
				continue
			}
			parent[next.to] = block
			edge[next.to] = next.corridor
			distance[next.to] = distance[block] + next.cost
			queue = append(queue, next.to)
		}
	}
	return parent, edge, distance
}

func selectMain(g *graph, startBlock, targetBlock int, count uint32) ([]selectedGate, error) {
	if count == 0 {
		return []selectedGate{}, nil
	}
	if targetBlock == startBlock {
		return nil, fmt.Errorf("%w: target is in the start block", daedalus.ErrInsufficientGates)
	}
	path, err := targetPath(g, startBlock, targetBlock)
	if err != nil {
		return nil, err
	}
	if len(path) < int(count) {
		return nil, fmt.Errorf("%w: only %d main bridges", daedalus.ErrInsufficientGates, len(path))
	}
	chosen := make([]selectedGate, 0, count)
	used := make([]bool, len(path))
	total := uint64(0)
	for _, edge := range path {
		total += edge.cost
	}
	for ordinal := uint32(1); ordinal <= count; ordinal++ {
		pick := pickMainIndex(path, used, total, ordinal, count)
		used[pick] = true
		edge := path[pick]
		door, ok := startSideDoor(g, edge)
		if !ok {
			return nil, fmt.Errorf("%w: corridor %d has a shared or ambiguous start-side Door", daedalus.ErrInsufficientGates, edge.corridor)
		}
		chosen = append(chosen, selectedGate{kind: GateKindMain, edge: edge, door: door})
	}
	sort.SliceStable(chosen, func(i, j int) bool {
		return pathIndex(path, chosen[i].edge.corridor) < pathIndex(path, chosen[j].edge.corridor)
	})
	return chosen, nil
}

func pickMainIndex(path []treeEdge, used []bool, total uint64, ordinal, count uint32) int {
	product := uint64(ordinal) * total
	threshold := product / uint64(count+1)
	cumulative := uint64(0)
	pick := -1
	for i, edge := range path {
		cumulative += edge.cost
		if !used[i] && cumulative >= threshold {
			pick = i
			break
		}
	}
	if pick < 0 {
		for i := range path {
			if !used[i] {
				pick = i
				break
			}
		}
	}
	return pick
}

func targetPath(g *graph, startBlock, targetBlock int) ([]treeEdge, error) {
	parent, _, _ := rootedTree(g, startBlock)
	if targetBlock == startBlock {
		return []treeEdge{}, nil
	}
	var reverse []treeEdge
	for block := targetBlock; block != startBlock; block = parent[block] {
		if parent[block] < 0 {
			return nil, fmt.Errorf("%w: target is disconnected", daedalus.ErrInvalidGating)
		}
		for _, candidate := range g.blockAdj[parent[block]] {
			if candidate.to == block {
				reverse = append(reverse, candidate)
				break
			}
		}
	}
	path := make([]treeEdge, len(reverse))
	for i := range reverse {
		path[i] = reverse[len(reverse)-1-i]
	}
	return path, nil
}

func pathIndex(path []treeEdge, corridor int) int {
	for i, edge := range path {
		if edge.corridor == corridor {
			return i
		}
	}
	return len(path)
}

func startSideDoor(g *graph, edge treeEdge) (daedalus.DoorID, bool) {
	corridor := g.layout.Corridors[edge.corridor]
	room := daedalus.RoomID(edge.fromRoom)
	var doorID daedalus.DoorID
	if corridor.FromRoomID == room {
		doorID = corridor.FromDoorID
	} else if corridor.ToRoomID == room {
		doorID = corridor.ToDoorID
	} else {
		return 0, false
	}
	return doorID, len(g.layout.Doors[doorID].CorridorIDs) == 1
}

type optionalCandidate struct {
	gate     selectedGate
	stage    int
	far      uint64
	treasure int
}

func selectOptional(g *graph, startBlock int, main []selectedGate, mainPath []treeEdge, count uint32) ([]selectedGate, error) {
	if count == 0 {
		return []selectedGate{}, nil
	}
	parent, _, distance := rootedTree(g, startBlock)
	mainCorridors := make([]int, 0, len(mainPath))
	for _, edge := range mainPath {
		mainCorridors = append(mainCorridors, edge.corridor)
	}
	mainStage := optionalStages(g, startBlock, main)
	candidates := optionalCandidates(g, parent, distance, mainStage, mainCorridors)
	sort.Slice(candidates, func(i, j int) bool { return optionalCandidateLess(candidates[i], candidates[j]) })
	if len(candidates) < int(count) {
		return nil, fmt.Errorf("%w: only %d treasure branches", daedalus.ErrInsufficientGates, len(candidates))
	}
	result := make([]selectedGate, count)
	for i := range result {
		result[i] = candidates[i].gate
	}
	return result, nil
}

func optionalStages(g *graph, startBlock int, main []selectedGate) []int {
	mainStage := make([]int, len(g.blocks))
	mainGateCorridors := make([]int, 0, len(main))
	for _, gate := range main {
		mainGateCorridors = append(mainGateCorridors, gate.edge.corridor)
	}
	parentBlock, _, _ := rootedTree(g, startBlock)
	order := []int{startBlock}
	for len(order) > 0 {
		block := order[0]
		order = order[1:]
		for _, next := range g.blockAdj[block] {
			if parentBlock[next.to] != block {
				continue
			}
			mainStage[next.to] = mainStage[block]
			if containsInt(mainGateCorridors, next.corridor) {
				mainStage[next.to]++
			}
			order = append(order, next.to)
		}
	}
	return mainStage
}

func optionalCandidates(g *graph, parent []int, distance []uint64, mainStage []int, mainCorridors []int) []optionalCandidate {
	var candidates []optionalCandidate
	for _, edge := range allBridgeEdges(g) {
		parentBlock := g.blockOf[edge.fromRoom]
		childBlock := edge.to
		if parent[childBlock] != parentBlock {
			if parent[parentBlock] != childBlock {
				continue
			}
			parentBlock, childBlock = childBlock, parentBlock
		}
		if containsInt(mainCorridors, edge.corridor) {
			continue
		}
		edge = orientTreeEdge(g, parentBlock, childBlock, edge.corridor)
		treasure, far, ok := subtreeTreasure(g, childBlock, parentBlock, 0)
		if !ok {
			continue
		}
		door, ok := startSideDoor(g, edge)
		if !ok {
			continue
		}
		candidates = append(candidates, optionalCandidate{gate: selectedGate{kind: GateKindOptional, edge: edge, door: door, stage: mainStage[parentBlock]}, stage: mainStage[parentBlock], far: far + distance[parentBlock], treasure: treasure})
	}
	return candidates
}

func optionalCandidateLess(first, second optionalCandidate) bool {
	if first.stage != second.stage {
		return first.stage < second.stage
	}
	if first.far != second.far {
		return first.far > second.far
	}
	if first.treasure != second.treasure {
		return first.treasure < second.treasure
	}
	if first.gate.edge.corridor != second.gate.edge.corridor {
		return first.gate.edge.corridor < second.gate.edge.corridor
	}
	return first.gate.door < second.gate.door
}

func containsInt(values []int, wanted int) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func orientTreeEdge(g *graph, parent, child, corridor int) treeEdge {
	for _, edge := range g.blockAdj[parent] {
		if edge.to == child && edge.corridor == corridor {
			return edge
		}
	}
	return treeEdge{to: child, corridor: corridor}
}

func allBridgeEdges(g *graph) []treeEdge {
	result := make([]treeEdge, 0)
	for from, edges := range g.blockAdj {
		for _, edge := range edges {
			if from < edge.to {
				result = append(result, treeEdge{to: edge.to, corridor: edge.corridor, fromRoom: edge.fromRoom, toRoom: edge.toRoom, cost: edge.cost})
			}
		}
	}
	return result
}

func subtreeTreasure(g *graph, root, parent int, base uint64) (int, uint64, bool) {
	bestRoom, bestDistance := -1, uint64(0)
	stack := []struct {
		block, parent int
		distance      uint64
	}{{root, parent, base + edgeCostBetween(g, root, parent)}}
	for len(stack) > 0 {
		item := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, room := range g.blocks[item.block] {
			if g.layout.Rooms[room].Role != nil && *g.layout.Rooms[room].Role == daedalus.RoomRoleTreasure && (bestRoom < 0 || item.distance > bestDistance || (item.distance == bestDistance && room < bestRoom)) {
				bestRoom, bestDistance = room, item.distance
			}
		}
		for _, edge := range g.blockAdj[item.block] {
			if edge.to != item.parent {
				stack = append(stack, struct {
					block, parent int
					distance      uint64
				}{edge.to, item.block, item.distance + edge.cost})
			}
		}
	}
	return bestRoom, bestDistance, bestRoom >= 0
}

func edgeCostBetween(g *graph, a, b int) uint64 {
	for _, edge := range g.blockAdj[a] {
		if edge.to == b {
			return edge.cost
		}
	}
	return 0
}

type roomItem struct {
	room     int
	distance uint64
}
type roomHeap []roomItem

func (h roomHeap) Len() int { return len(h) }
func (h roomHeap) Less(i, j int) bool {
	if h[i].distance != h[j].distance {
		return h[i].distance < h[j].distance
	}
	return h[i].room < h[j].room
}
func (h roomHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *roomHeap) Push(x interface{}) { *h = append(*h, x.(roomItem)) }
func (h *roomHeap) Pop() interface{} {
	old := *h
	item := old[len(old)-1]
	*h = old[:len(old)-1]
	return item
}
