package daedalus

import "context"

const (
	// primSingleRoomLimit é a maior quantidade de Rooms que não exige aresta.
	primSingleRoomLimit = 1
	// primStartingRoomIndex corresponde à RoomID 0 na sequência canônica.
	primStartingRoomIndex = 0
	// boundingBoxCenterDivisor converte a extensão entre a primeira e a
	// última coordenada da bounding box em deslocamento até o centro.
	boundingBoxCenterDivisor = 2.0
	// boundingBoxCellAdjustment converte quantidade de Cells em extensão
	// entre os centros da primeira e da última Cell.
	boundingBoxCellAdjustment = 1.0
	// primCancellationUpdateInterval limita o intervalo entre consultas ao
	// Context durante atualizações de melhores chaves, conforme a seção 11.
	primCancellationUpdateInterval uint64 = 256
)

// primRoomsConnector implementa o algoritmo embutido e congelado
// prim_rooms_v1. O tipo não possui estado: todos os buffers pertencem à
// chamada de Connect.
type primRoomsConnector struct{}

var _ Connector = primRoomsConnector{}

type roomCenter struct {
	x float64
	y float64
}

type primEdgeCandidate struct {
	connection    Connection
	toIndex       int
	squaredWeight float64
	destination   Cell
}

// Connect escolhe somente a árvore de backbone entre as Rooms. Atalhos de
// ExtraEdgeCount pertencem à fase posterior do Generator.
func (primRoomsConnector) Connect(req ConnectionRequest) ([]Connection, error) {
	roomCount := len(req.Rooms)
	edgeCapacity := roomCount - primSingleRoomLimit
	if edgeCapacity < 0 {
		edgeCapacity = 0
	}
	edges := make([]Connection, 0, edgeCapacity)
	if roomCount <= primSingleRoomLimit {
		return edges, nil
	}

	ctx := req.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	centers := make([]roomCenter, roomCount)
	for index, room := range req.Rooms {
		centers[index] = boundingBoxCenter(room)
	}
	visited := make([]bool, roomCount)
	bestKeys := make([]primEdgeCandidate, roomCount)
	hasKey := make([]bool, roomCount)
	visited[primStartingRoomIndex] = true
	var keyUpdates uint64

	updateKeys := func(fromIndex int) error {
		from := req.Rooms[fromIndex]
		for toIndex, to := range req.Rooms {
			if visited[toIndex] {
				continue
			}
			if keyUpdates%primCancellationUpdateInterval == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			keyUpdates++

			candidate := primEdgeCandidate{
				connection: Connection{
					FromRoomID: from.ID,
					ToRoomID:   to.ID,
				},
				toIndex:       toIndex,
				squaredWeight: squaredCenterDistance(centers[fromIndex], centers[toIndex]),
				// A "Cell de destino" da seção 8 é interpretada como a
				// âncora At da Room de destino, a Cell que o contrato de
				// PlacedRoom fornece para desempates canônicos.
				destination: to.At,
			}
			if !hasKey[toIndex] || primEdgeLess(candidate, bestKeys[toIndex]) {
				bestKeys[toIndex] = candidate
				hasKey[toIndex] = true
			}
		}
		return nil
	}

	if err := updateKeys(primStartingRoomIndex); err != nil {
		return nil, err
	}
	for len(edges) < edgeCapacity {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var selected primEdgeCandidate
		hasSelected := false
		for roomIndex := range req.Rooms {
			if visited[roomIndex] || !hasKey[roomIndex] {
				continue
			}
			candidate := bestKeys[roomIndex]
			if !hasSelected || primEdgeLess(candidate, selected) {
				selected = candidate
				hasSelected = true
			}
		}

		edges = append(edges, selected.connection)
		visited[selected.toIndex] = true
		hasKey[selected.toIndex] = false
		if err := updateKeys(selected.toIndex); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return edges, nil
}

// boundingBoxCenter devolve o centro da bounding box como centroide das
// Cells, isto é, Origin + (dimensão-1)/2. A alternativa seria o centro da
// área, Origin + dimensão/2, e as duas produzem árvores diferentes porque o
// deslocamento depende da dimensão de cada Room. A seção 8 resolve a dúvida
// ao afirmar que, para Rooms 1×1, o peso "coincide com distância entre
// âncoras": isso só vale com (dimensão-1)/2, que anula o deslocamento quando
// a dimensão é 1.
func boundingBoxCenter(room PlacedRoom) roomCenter {
	width := float64(room.Width)
	widthSpan := width - boundingBoxCellAdjustment
	halfWidthSpan := widthSpan / boundingBoxCenterDivisor
	originX := float64(room.Origin.X)
	centerX := originX + halfWidthSpan

	height := float64(room.Height)
	heightSpan := height - boundingBoxCellAdjustment
	halfHeightSpan := heightSpan / boundingBoxCenterDivisor
	originY := float64(room.Origin.Y)
	centerY := originY + halfHeightSpan

	return roomCenter{x: centerX, y: centerY}
}

func squaredCenterDistance(first, second roomCenter) float64 {
	deltaX := float64(first.x) - float64(second.x)
	deltaY := float64(first.y) - float64(second.y)
	deltaXSquared := float64(deltaX) * float64(deltaX)
	deltaYSquared := float64(deltaY) * float64(deltaY)
	return deltaXSquared + deltaYSquared
}

func primEdgeLess(first, second primEdgeCandidate) bool {
	// A seção 8 define peso euclidiano. Como sqrt é estritamente crescente
	// para valores não negativos, comparar o quadrado preserva exatamente a
	// mesma ordem sem introduzir outro arredondamento no caminho congelado.
	if first.squaredWeight != second.squaredWeight {
		return first.squaredWeight < second.squaredWeight
	}
	if first.connection.FromRoomID != second.connection.FromRoomID {
		return first.connection.FromRoomID < second.connection.FromRoomID
	}
	if first.connection.ToRoomID != second.connection.ToRoomID {
		return first.connection.ToRoomID < second.connection.ToRoomID
	}

	// Em um grafo simples, FromRoomID e ToRoomID já identificam a aresta e
	// tornam este terceiro nível inalcançável no Prim. A seção 8 o congela
	// porque o mesmo comparador será reutilizado pela seleção de arestas
	// descartadas na fase de ciclos; por isso preservamos a ordem Cell Y/X.
	if first.destination.Y != second.destination.Y {
		return first.destination.Y < second.destination.Y
	}
	return first.destination.X < second.destination.X
}
