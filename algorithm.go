package daedalus

import "context"

// Placer é o algoritmo que propõe placements de Room para uma
// solicitação de geração. É uma das duas únicas interfaces Strategy de v1;
// funções e closures a satisfazem idiomaticamente, sem hierarquias de
// classes nem factories.
//
// Placer devolve somente RoomPlacements: não atribui Plants, Doors nem IDs e
// não muta Layout. O Generator embutido usa poisson_disk_rooms_v1. Uma
// implementação injetada pelo jogo é responsabilidade de quem a escreveu:
// determinismo, cancelamento via PlacementRequest.Context e segurança
// concorrente cabem ao plugin; um Placer compartilhado entre goroutines
// precisa ser seguro para chamadas simultâneas.
type Placer interface {
	// Place propõe as Rooms da solicitação. Deve devolver placements válidos
	// dentro dos limites de Grid do pedido, ou um erro. Deve
	// observar req.Context e abandonar o trabalho ao ser cancelado.
	Place(req PlacementRequest) ([]RoomPlacement, error)
}

// RoomPlacement descreve uma Room proposta, com Cells locais relativas à
// Origin e ordenadas pela máscara canônica.
type RoomPlacement struct {
	// Shape é a máscara discreta canônica do footprint.
	Shape RoomShape
	// Origin é o canto superior esquerdo da bounding box.
	Origin Cell
	// Width é a largura da bounding box, em Cells.
	Width uint32
	// Height é a altura da bounding box, em Cells.
	Height uint32
	// Cells contém os offsets locais canônicos da máscara, em ordem Y/X.
	Cells []Cell
}

// PlacementRequest carrega todos os dados variáveis de uma solicitação de
// posicionamento de Rooms.
type PlacementRequest struct {
	// Context carrega o cancelamento e o deadline do chamador. O Placer
	// deve verificá-lo nas fronteiras de fase e em laços longos, e nunca
	// devolver resultado parcial após cancelamento.
	Context context.Context
	// Width é a largura do Grid de destino, em Cells.
	Width uint32
	// Height é a altura do Grid de destino, em Cells.
	Height uint32
	// MinDistance é a distância euclidiana mínima, em Cells, entre centros
	// de Rooms fora de toda DensityRegion.
	MinDistance float64
	// DensityRegions lista as regiões de densidade já validadas da Config;
	// vazio significa MinDistance uniforme.
	DensityRegions []DensityRegion
	// RoomGeometry contém a geometria já normalizada para esta solicitação.
	RoomGeometry RoomGeometry
	// MaxAttempts é o máximo de candidatos por ponto ativo.
	MaxAttempts uint32
	// MaxRooms é o máximo de Rooms aceitas; encerra o posicionamento
	// quando atingido.
	MaxRooms uint32
	// Seed é a fonte do stream aleatório de posicionamento da solicitação.
	Seed Seed
}

// Connector é o algoritmo que escolhe as arestas Room-a-Room de uma
// solicitação de geração. É uma das duas únicas interfaces Strategy de v1;
// funções e closures a satisfazem idiomaticamente.
//
// Connector devolve somente arestas: não roteia Cells e não muta Layout. O
// resultado deve ser um grafo simples que torna toda Room alcançável; o
// Generator rejeita Connections duplicadas, próprias, desconhecidas ou
// desconectadas. O resultado final contém entre n-1 e
// n-1+ConnectionRequest.ExtraEdgeCount arestas; com ExtraEdgeCount == 0 o
// Generator rejeita qualquer ciclo e exige uma árvore. O Connector embutido
// é prim_rooms_v1, que devolve a árvore de backbone; os atalhos de
// ExtraEdgeCount são acrescentados pelo Generator na fase de ciclos, não pelo
// Connector.
// Uma implementação injetada pelo jogo responde por seu determinismo,
// cancelamento e segurança concorrente.
type Connector interface {
	// Connect escolhe as arestas Room-a-Room da solicitação, ou devolve um
	// erro. Deve observar req.Context e abandonar o trabalho ao ser
	// cancelado.
	Connect(req ConnectionRequest) ([]Connection, error)
}

// PlacedRoom descreve uma Room já posicionada, incluindo âncora, máscara,
// bounding box e footprint absoluto. A ordem da sequência é a ordem canônica
// de RoomID.
type PlacedRoom struct {
	// ID é o identificador estável da Room, na ordem de criação.
	ID RoomID
	// At é a primeira Cell ocupada do footprint em ordem canônica Y/X.
	At Cell
	// Shape é a máscara discreta canônica do footprint.
	Shape RoomShape
	// Origin é o canto superior esquerdo da bounding box.
	Origin Cell
	// Width é a largura da bounding box, em Cells.
	Width uint32
	// Height é a altura da bounding box, em Cells.
	Height uint32
	// Cells contém o footprint absoluto, ordenado por Y e depois X.
	Cells []Cell
}

// ConnectionRequest carrega todos os dados variáveis de uma solicitação de
// conexão de Rooms.
type ConnectionRequest struct {
	// Context carrega o cancelamento e o deadline do chamador. O Connector
	// deve verificá-lo nas fronteiras de fase e em laços longos, e nunca
	// devolver resultado parcial após cancelamento.
	Context context.Context
	// Rooms lista as Rooms posicionadas em ordem canônica de RoomID.
	Rooms []PlacedRoom
	// Width é a largura do Grid de destino, em Cells.
	Width uint32
	// Height é a altura do Grid de destino, em Cells.
	Height uint32
	// ExtraEdgeCount é a quantidade máxima de arestas curtas descartadas
	// que o Generator pode reintroduzir após o backbone; 0 exige que o
	// resultado seja uma árvore.
	ExtraEdgeCount uint32
	// Seed é a fonte do stream aleatório de conexão da solicitação. O
	// Connector embutido prim_rooms_v1 a recebe, mas não consome sorteio.
	Seed Seed
}

// Connection é uma aresta topológica escolhida pelo Connector entre duas
// Rooms distintas.
type Connection struct {
	// FromRoomID é a Room de origem da aresta; sempre distinta de
	// ToRoomID.
	FromRoomID RoomID
	// ToRoomID é a Room de destino da aresta; sempre distinta de
	// FromRoomID.
	ToRoomID RoomID
}
