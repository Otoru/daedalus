package daedalus

import (
	"context"
	"errors"
	"math"
	"sort"
)

const (
	// topologyFirstRoomIndex é a posição canônica da RoomID 0, raiz das
	// distâncias temáticas e primeira Room aceita pelo posicionamento.
	topologyFirstRoomIndex = 0
	// topologyNextRoomOffset avança ao próximo par no grafo completo sem
	// produzir aresta própria.
	topologyNextRoomOffset = 1
	// topologyUndirectedEdgeDivisor remove a duplicidade das duas orientações
	// ao dimensionar o grafo simples completo.
	topologyUndirectedEdgeDivisor = 2
	// topologyNoExtraEdges desliga integralmente a fase de atalhos.
	topologyNoExtraEdges uint32 = 0
	// topologyNoRoomIndex representa a ausência de candidata temática.
	topologyNoRoomIndex = -1
)

var (
	// errBossRoleWithoutStart sinaliza quebra de uma invariável interna: a
	// validação da Config deve rejeitar Boss sem Start antes desta fase.
	errBossRoleWithoutStart = errors.New("atribuição de Boss alcançada sem solicitação Start")
	// errTopologyUnknownRoom sinaliza uma Connection que escapou da validação
	// do Connector com um RoomID inexistente.
	errTopologyUnknownRoom = errors.New("topologia contém RoomID desconhecido")
	// errTopologyDisconnected sinaliza uma árvore de backbone que escapou da
	// validação do Connector sem alcançar todas as Rooms.
	errTopologyDisconnected = errors.New("topologia de backbone desconectada")
)

type weightedRoomNeighbor struct {
	roomIndex int
	weight    float64
}

// applyTopologyOptions fixa a ordem normativa da seção 8.1: os papéis são
// projetados sobre a árvore de backbone antes que qualquer atalho seja
// acrescentado. Nenhuma das fases recebe ou consome stream aleatório.
func applyTopologyOptions(
	ctx context.Context,
	rooms []PlacedRoom,
	backbone []Connection,
	requests []RoomRoleRequest,
	extraEdgeCount uint32,
) ([]*RoomRole, []Connection, error) {
	roles, err := assignRoomRoles(ctx, rooms, backbone, requests)
	if err != nil {
		return nil, nil, err
	}
	connections, err := addExtraConnections(ctx, rooms, backbone, extraEdgeCount)
	if err != nil {
		return nil, nil, err
	}
	return roles, connections, nil
}

// assignRoomRoles atribui papéis na ordem declarada, reservando RoomID 0
// para Start quando a solicitação existe. A reserva antecipada é a leitura
// conservadora da seção 8.1: Start recebe RoomID 0 mesmo quando sua entrada
// aparece depois de Boss ou Treasure; as demais entradas preservam a ordem
// do chamador e nunca podem ocupar essa Room.
func assignRoomRoles(
	ctx context.Context,
	rooms []PlacedRoom,
	backbone []Connection,
	requests []RoomRoleRequest,
) ([]*RoomRole, error) {
	ctx = topologyContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	roles := make([]*RoomRole, len(rooms))
	if len(requests) == 0 || len(rooms) == 0 {
		return roles, nil
	}

	hasStart := false
	hasBoss := false
	needsDistances := false
	for _, request := range requests {
		switch request.Role {
		case RoomRoleStart:
			hasStart = true
		case RoomRoleBoss:
			hasBoss = true
			needsDistances = true
		case RoomRoleTreasure:
			needsDistances = true
		}
	}
	if hasBoss && !hasStart {
		return nil, errBossRoleWithoutStart
	}

	startIndex := topologyFirstRoomIndex
	if rooms[startIndex].ID != RoomID(topologyFirstRoomIndex) {
		return nil, errTopologyUnknownRoom
	}
	if hasStart {
		roles[startIndex] = roomRolePointer(RoomRoleStart)
	}

	// A seção 8.1 mede Treasure "mais distante de Start", mas exige Start
	// apenas para Boss; a validação da Config aceita Treasure sozinho. Nesse
	// caso a origem continua sendo a RoomID 0, a primeira Room aceita, que é
	// exatamente a Room que Start ocuparia. A leitura é conservadora e vira
	// contrato congelado: sem ela, "distante de Start" não teria referência
	// e a atribuição seria indefinida.
	var distances []float64
	if needsDistances {
		var err error
		distances, err = weightedTreeDistances(ctx, rooms, backbone, startIndex)
		if err != nil {
			return nil, err
		}
	}

	for _, request := range requests {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		switch request.Role {
		case RoomRoleStart:
			continue
		case RoomRoleBoss:
			assignFarthestRole(rooms, roles, distances, RoomRoleBoss)
		case RoomRoleTreasure:
			for assigned := uint32(0); assigned < request.Count; assigned++ {
				if !assignFarthestRole(rooms, roles, distances, RoomRoleTreasure) {
					break
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return roles, nil
}

func weightedTreeDistances(
	ctx context.Context,
	rooms []PlacedRoom,
	backbone []Connection,
	startIndex int,
) ([]float64, error) {
	centers := make([]roomCenter, len(rooms))
	for roomIndex, room := range rooms {
		centers[roomIndex] = boundingBoxCenter(room)
	}

	adjacency := make([][]weightedRoomNeighbor, len(rooms))
	for _, connection := range backbone {
		fromIndex := roomIndexByID(rooms, connection.FromRoomID)
		toIndex := roomIndexByID(rooms, connection.ToRoomID)
		if fromIndex == topologyNoRoomIndex || toIndex == topologyNoRoomIndex {
			return nil, errTopologyUnknownRoom
		}
		squaredWeight := squaredCenterDistance(centers[fromIndex], centers[toIndex])
		weight := math.Sqrt(float64(squaredWeight))
		adjacency[fromIndex] = append(adjacency[fromIndex], weightedRoomNeighbor{roomIndex: toIndex, weight: weight})
		adjacency[toIndex] = append(adjacency[toIndex], weightedRoomNeighbor{roomIndex: fromIndex, weight: weight})
	}

	distances := make([]float64, len(rooms))
	visited := make([]bool, len(rooms))
	queue := make([]int, 0, len(rooms))
	visited[startIndex] = true
	queue = append(queue, startIndex)
	for queueIndex := topologyFirstRoomIndex; queueIndex < len(queue); queueIndex++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		currentIndex := queue[queueIndex]
		for _, neighbor := range adjacency[currentIndex] {
			if visited[neighbor.roomIndex] {
				continue
			}
			pathDistance := float64(distances[currentIndex])
			pathDistance += float64(neighbor.weight)
			distances[neighbor.roomIndex] = pathDistance
			visited[neighbor.roomIndex] = true
			queue = append(queue, neighbor.roomIndex)
		}
	}
	for _, reached := range visited {
		if !reached {
			return nil, errTopologyDisconnected
		}
	}
	return distances, nil
}

func assignFarthestRole(
	rooms []PlacedRoom,
	roles []*RoomRole,
	distances []float64,
	role RoomRole,
) bool {
	selectedIndex := topologyNoRoomIndex
	for roomIndex, room := range rooms {
		if roles[roomIndex] != nil {
			continue
		}
		if selectedIndex == topologyNoRoomIndex ||
			distances[roomIndex] > distances[selectedIndex] ||
			(distances[roomIndex] == distances[selectedIndex] && room.ID < rooms[selectedIndex].ID) {
			selectedIndex = roomIndex
		}
	}
	if selectedIndex == topologyNoRoomIndex {
		return false
	}
	roles[selectedIndex] = roomRolePointer(role)
	return true
}

func roomRolePointer(role RoomRole) *RoomRole {
	assigned := role
	return &assigned
}

// addExtraConnections acrescenta as menores arestas do grafo completo que
// não pertencem ao backbone. Com contagem zero, retorna imediatamente sem
// formar, ordenar ou percorrer o conjunto de descartadas, conforme a seção
// 8.1.
func addExtraConnections(
	ctx context.Context,
	rooms []PlacedRoom,
	backbone []Connection,
	extraEdgeCount uint32,
) ([]Connection, error) {
	ctx = topologyContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if extraEdgeCount == topologyNoExtraEdges {
		return backbone, nil
	}

	roomCount := len(rooms)
	backbonePairs := make([]bool, roomCount*roomCount)
	for _, connection := range backbone {
		fromIndex := roomIndexByID(rooms, connection.FromRoomID)
		toIndex := roomIndexByID(rooms, connection.ToRoomID)
		if fromIndex == topologyNoRoomIndex || toIndex == topologyNoRoomIndex {
			return nil, errTopologyUnknownRoom
		}
		backbonePairs[fromIndex*roomCount+toIndex] = true
		backbonePairs[toIndex*roomCount+fromIndex] = true
	}

	centers := make([]roomCenter, roomCount)
	for roomIndex, room := range rooms {
		centers[roomIndex] = boundingBoxCenter(room)
	}
	maximumCandidateCount := roomCount * (roomCount - topologyNextRoomOffset) / topologyUndirectedEdgeDivisor
	candidates := make([]primEdgeCandidate, 0, maximumCandidateCount)
	for firstIndex := topologyFirstRoomIndex; firstIndex < roomCount; firstIndex++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for secondIndex := firstIndex + topologyNextRoomOffset; secondIndex < roomCount; secondIndex++ {
			if backbonePairs[firstIndex*roomCount+secondIndex] {
				continue
			}
			fromIndex := firstIndex
			toIndex := secondIndex
			if rooms[toIndex].ID < rooms[fromIndex].ID {
				fromIndex, toIndex = toIndex, fromIndex
			}
			candidates = append(candidates, primEdgeCandidate{
				connection: Connection{
					FromRoomID: rooms[fromIndex].ID,
					ToRoomID:   rooms[toIndex].ID,
				},
				toIndex:       toIndex,
				squaredWeight: squaredCenterDistance(centers[fromIndex], centers[toIndex]),
				// A orientação canônica da aresta descartada é do menor para
				// o maior RoomID; portanto esta é a Cell de destino usada pelo
				// terceiro desempate congelado do Prim.
				destination: rooms[toIndex].At,
			})
		}
	}
	sort.Slice(candidates, func(first, second int) bool {
		return primEdgeLess(candidates[first], candidates[second])
	})

	selectedCount := len(candidates)
	if uint64(selectedCount) > uint64(extraEdgeCount) {
		selectedCount = int(extraEdgeCount)
	}
	connections := make([]Connection, 0, len(backbone)+selectedCount)
	connections = append(connections, backbone...)
	for candidateIndex := topologyFirstRoomIndex; candidateIndex < selectedCount; candidateIndex++ {
		connections = append(connections, candidates[candidateIndex].connection)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return connections, nil
}

func roomIndexByID(rooms []PlacedRoom, id RoomID) int {
	for roomIndex, room := range rooms {
		if room.ID == id {
			return roomIndex
		}
	}
	return topologyNoRoomIndex
}

func topologyContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
