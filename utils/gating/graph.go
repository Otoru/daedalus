package gating

import (
	"context"
	"fmt"
	"sort"

	daedalus "github.com/Otoru/daedalus"
)

type edgeRef struct {
	to   int
	edge int
}

type graph struct {
	layout    daedalus.Layout
	adj       [][]edgeRef
	bridges   []bool
	blockOf   []int
	blocks    [][]int
	blockAdj  [][]treeEdge
	edgeBlock []treeEdge
}

type treeEdge struct {
	to, corridor, fromRoom, toRoom int
	cost                           uint64
}

func buildGraph(ctx context.Context, layout daedalus.Layout) (*graph, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateGraphLayout(ctx, layout); err != nil {
		return nil, err
	}
	g := &graph{layout: layout, adj: make([][]edgeRef, len(layout.Rooms)), bridges: make([]bool, len(layout.Corridors))}
	for _, corridor := range layout.Corridors {
		from := int(corridor.FromRoomID)
		to := int(corridor.ToRoomID)
		g.adj[from] = append(g.adj[from], edgeRef{to: to, edge: int(corridor.ID)})
		g.adj[to] = append(g.adj[to], edgeRef{to: from, edge: int(corridor.ID)})
	}
	for room := range g.adj {
		sort.Slice(g.adj[room], func(i, j int) bool {
			if g.adj[room][i].to != g.adj[room][j].to {
				return g.adj[room][i].to < g.adj[room][j].to
			}
			return g.adj[room][i].edge < g.adj[room][j].edge
		})
	}
	if err := g.discoverBridges(ctx); err != nil {
		return nil, err
	}
	if err := g.buildBlocks(ctx); err != nil {
		return nil, err
	}
	if err := g.checkConnected(); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *graph) checkConnected() error {
	reached := make([]bool, len(g.layout.Rooms))
	queue := []int{0}
	reached[0] = true
	for len(queue) > 0 {
		room := queue[0]
		queue = queue[1:]
		for _, next := range g.adj[room] {
			if !reached[next.to] {
				reached[next.to] = true
				queue = append(queue, next.to)
			}
		}
	}
	for room, ok := range reached {
		if !ok {
			return fmt.Errorf("%w: RoomID %d is disconnected", daedalus.ErrInvalidGating, room)
		}
	}
	return nil
}

func validateGraphLayout(ctx context.Context, layout daedalus.Layout) error {
	if len(layout.Rooms) == 0 || len(layout.Rooms) > daedalus.MaxRooms {
		return fmt.Errorf("%w: room count %d", daedalus.ErrInvalidGating, len(layout.Rooms))
	}
	if uint64(len(layout.Corridors)) > maxSimpleCorridors || uint64(len(layout.Doors)) > maxDoors {
		return fmt.Errorf("%w: topology exceeds gating ceiling", daedalus.ErrLimitExceeded)
	}
	for i, room := range layout.Rooms {
		if err := ctx.Err(); err != nil {
			return err
		}
		if room.ID != daedalus.RoomID(i) {
			return fmt.Errorf("%w: rooms[%d].id = %d", daedalus.ErrInvalidGating, i, room.ID)
		}
	}
	if err := validateGraphEdges(layout); err != nil {
		return err
	}
	return validateGraphOwnership(layout)
}

func validateGraphEdges(layout daedalus.Layout) error {
	for i, corridor := range layout.Corridors {
		if corridor.ID != daedalus.CorridorID(i) || corridor.FromRoomID >= daedalus.RoomID(len(layout.Rooms)) || corridor.ToRoomID >= daedalus.RoomID(len(layout.Rooms)) || corridor.FromRoomID == corridor.ToRoomID {
			return fmt.Errorf("%w: corridors[%d] has invalid endpoints or id", daedalus.ErrInvalidGating, i)
		}
		if corridor.FromDoorID >= daedalus.DoorID(len(layout.Doors)) || corridor.ToDoorID >= daedalus.DoorID(len(layout.Doors)) {
			return fmt.Errorf("%w: corridors[%d] has unknown door", daedalus.ErrInvalidGating, i)
		}
	}
	return validateGraphDoors(layout)
}

func validateGraphDoors(layout daedalus.Layout) error {
	for i, door := range layout.Doors {
		if door.ID != daedalus.DoorID(i) || door.RoomID >= daedalus.RoomID(len(layout.Rooms)) || len(door.CorridorIDs) == 0 {
			return fmt.Errorf("%w: doors[%d] is malformed", daedalus.ErrInvalidGating, i)
		}
		for j, corridorID := range door.CorridorIDs {
			if corridorID >= daedalus.CorridorID(len(layout.Corridors)) || (j > 0 && corridorID <= door.CorridorIDs[j-1]) {
				return fmt.Errorf("%w: doors[%d].corridor_ids not canonical", daedalus.ErrInvalidGating, i)
			}
		}
	}
	return nil
}

func validateGraphOwnership(layout daedalus.Layout) error {
	for roomIndex, room := range layout.Rooms {
		seen := make([]bool, len(layout.Doors))
		for _, doorID := range room.DoorIDs {
			if doorID >= daedalus.DoorID(len(layout.Doors)) || layout.Doors[doorID].RoomID != room.ID {
				return fmt.Errorf("%w: rooms[%d] references foreign door", daedalus.ErrInvalidGating, roomIndex)
			}
			if seen[doorID] {
				return fmt.Errorf("%w: rooms[%d] repeats DoorID %d", daedalus.ErrInvalidGating, roomIndex, doorID)
			}
			seen[doorID] = true
		}
	}
	for _, corridor := range layout.Corridors {
		fromDoor := layout.Doors[corridor.FromDoorID]
		toDoor := layout.Doors[corridor.ToDoorID]
		if fromDoor.RoomID != corridor.FromRoomID || toDoor.RoomID != corridor.ToRoomID || !containsCorridor(fromDoor.CorridorIDs, corridor.ID) || !containsCorridor(toDoor.CorridorIDs, corridor.ID) || !containsDoor(layout.Rooms[corridor.FromRoomID].DoorIDs, corridor.FromDoorID) || !containsDoor(layout.Rooms[corridor.ToRoomID].DoorIDs, corridor.ToDoorID) {
			return fmt.Errorf("%w: corridor %d has inconsistent door ownership", daedalus.ErrInvalidGating, corridor.ID)
		}
	}
	return nil
}

func containsCorridor(values []daedalus.CorridorID, wanted daedalus.CorridorID) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsDoor(values []daedalus.DoorID, wanted daedalus.DoorID) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

type bridgeFrame struct{ room, next int }

type bridgeSearch struct {
	graph                 *graph
	disc, low, parentEdge []int
	time                  int
}

func (g *graph) discoverBridges(ctx context.Context) error {
	n := len(g.adj)
	search := bridgeSearch{graph: g, disc: make([]int, n), low: make([]int, n), parentEdge: make([]int, n)}
	for i := range search.parentEdge {
		search.parentEdge[i] = -1
	}
	for root := 0; root < n; root++ {
		if search.disc[root] != 0 {
			continue
		}
		if err := search.visitRoot(ctx, root); err != nil {
			return err
		}
	}
	return nil
}

func (search *bridgeSearch) visitRoot(ctx context.Context, root int) error {
	search.time++
	search.disc[root], search.low[root] = search.time, search.time
	stack := []bridgeFrame{{room: root}}
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		last := &stack[len(stack)-1]
		if last.next == len(search.graph.adj[last.room]) {
			search.finishFrame(stack)
			stack = stack[:len(stack)-1]
			continue
		}
		ref := search.graph.adj[last.room][last.next]
		last.next++
		if ref.edge == search.parentEdge[last.room] {
			continue
		}
		if search.disc[ref.to] == 0 {
			search.parentEdge[ref.to] = ref.edge
			search.time++
			search.disc[ref.to], search.low[ref.to] = search.time, search.time
			stack = append(stack, bridgeFrame{room: ref.to})
			continue
		}
		if search.disc[ref.to] < search.low[last.room] {
			search.low[last.room] = search.disc[ref.to]
		}
	}
	return nil
}

func (search *bridgeSearch) finishFrame(stack []bridgeFrame) {
	if len(stack) < 2 {
		return
	}
	child := stack[len(stack)-1].room
	parent := stack[len(stack)-2].room
	if search.low[child] > search.disc[parent] {
		search.graph.bridges[search.parentEdge[child]] = true
	}
	if search.low[child] < search.low[parent] {
		search.low[parent] = search.low[child]
	}
}

func (g *graph) buildBlocks(ctx context.Context) error {
	n := len(g.adj)
	dsu := make([]int, n)
	for i := range dsu {
		dsu[i] = i
	}
	find := func(x int) int {
		for dsu[x] != x {
			dsu[x] = dsu[dsu[x]]
			x = dsu[x]
		}
		return x
	}
	union := func(a, b int) {
		a, b = find(a), find(b)
		if a != b {
			dsu[b] = a
		}
	}
	for corridor, edge := range g.layout.Corridors {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !g.bridges[corridor] {
			union(int(edge.FromRoomID), int(edge.ToRoomID))
		}
	}
	g.blockOf = make([]int, n)
	rootToBlock := make([]int, n)
	for i := range rootToBlock {
		rootToBlock[i] = -1
	}
	for room := 0; room < n; room++ {
		root := find(room)
		if rootToBlock[root] < 0 {
			rootToBlock[root] = len(g.blocks)
			g.blocks = append(g.blocks, nil)
		}
		block := rootToBlock[root]
		g.blockOf[room] = block
		g.blocks[block] = append(g.blocks[block], room)
	}
	g.attachBlockBridges()
	return nil
}

func (g *graph) attachBlockBridges() {
	g.blockAdj = make([][]treeEdge, len(g.blocks))
	for corridor, edge := range g.layout.Corridors {
		if !g.bridges[corridor] {
			continue
		}
		from := g.blockOf[edge.FromRoomID]
		to := g.blockOf[edge.ToRoomID]
		cost := uint64(1)
		centerline := uint64(len(edge.Centerline))
		cost += centerline
		left := treeEdge{to: to, corridor: corridor, fromRoom: int(edge.FromRoomID), toRoom: int(edge.ToRoomID), cost: cost}
		right := treeEdge{to: from, corridor: corridor, fromRoom: int(edge.ToRoomID), toRoom: int(edge.FromRoomID), cost: cost}
		g.blockAdj[from] = append(g.blockAdj[from], left)
		g.blockAdj[to] = append(g.blockAdj[to], right)
		g.edgeBlock = append(g.edgeBlock, left)
	}
	for block := range g.blockAdj {
		sort.Slice(g.blockAdj[block], func(i, j int) bool { return g.blockAdj[block][i].corridor < g.blockAdj[block][j].corridor })
	}
}
