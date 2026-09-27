package daedalus

// Layout é o resultado completo, imutável e gerado com sucesso de uma
// solicitação de dungeon. Ele descreve geometria, não a entrada do processo:
// não repete o tuning de Config, apenas a Seed e o Grid resultante.
//
// Invariantes garantidas pelo gerador: existe pelo menos uma Room; todo
// Room.At é distinto; com n Rooms existem ao menos n-1 Corridors se n > 1 e
// zero se n == 1; o grafo Room/Corridor é conectado (uma árvore quando
// Config.ExtraEdgeCount == 0); todos os slices, inclusive aninhados, são
// alocados por solicitação, de modo que mutar um Layout nunca muta o
// resultado de outra solicitação.
type Layout struct {
	// Seed é a Seed efetiva que originou este Layout.
	Seed Seed
	// Grid é o espaço físico de Cells do Layout.
	Grid Grid
	// Rooms lista as Rooms em ordem canônica de RoomID.
	Rooms []Room
	// Corridors lista os Corridors em ordem canônica de CorridorID.
	Corridors []Corridor
	// Doors lista as Doors em ordem canônica de DoorID.
	Doors []Door
}

// Grid é o espaço retangular de Width × Height Cells de um Layout, onde
// 0 ≤ X < Width e 0 ≤ Y < Height.
type Grid struct {
	// Width é a largura do Grid, em Cells, na faixa 1..256.
	Width uint32
	// Height é a altura do Grid, em Cells, na faixa 1..256.
	Height uint32
	// CellSize é o tamanho de uma Cell em unidades opacas definidas pelo
	// chamador, copiado de Config somente para saída; nenhum algoritmo o
	// converte para pixels.
	CellSize float64
	// Cells contém exatamente Width × Height estados, em ordem canônica
	// (Y e depois X): o estado de (x, y) está em Cells[y*Width+x].
	Cells []CellState
}

// Room é um vértice topológico do Layout e contém o footprint absoluto da
// máscara da Room, em ordem canônica Y/X.
type Room struct {
	// ID é o identificador estável da Room, na ordem de criação.
	ID RoomID
	// At é a primeira Cell ocupada do footprint em ordem canônica Y/X.
	At Cell
	// Shape é a máscara canônica da Room.
	Shape RoomShape
	// Origin é o canto superior esquerdo da bounding box da Room.
	Origin Cell
	// Width é a largura da bounding box, em Cells.
	Width uint32
	// Height é a altura da bounding box, em Cells.
	Height uint32
	// Cells é o footprint absoluto da Room, em ordem Y e depois X.
	Cells []Cell
	// Role é o papel temático da Room, ou nil quando
	// Config.RoomRoleRequests está vazio ou nenhum papel foi atribuído a
	// esta Room. Nenhuma Room recebe mais de um Role em v1.
	Role *RoomRole
	// PlantID é o metadado de asset selecionado do catálogo, ou a string
	// vazia quando Config.PlantCatalog está ausente. Nunca altera
	// topologia.
	PlantID PlantID
	// Tags é a cópia canônica das tags da Plant selecionada; vazio quando
	// não há catálogo ou a Plant não declara tags.
	Tags []string
	// DoorIDs lista as aberturas da Room em ordem de Cell (Y, X) e depois
	// Direction; pode ser vazio quando a Room não possui Corridors.
	DoorIDs []DoorID
}

// Corridor é uma aresta topológica entre duas Rooms e suas Cells ortogonais
// internas ordenadas. Corridors são arestas lógicas sobre o espaço físico
// de Grid.Cells; Cells de Corridor podem ser compartilhadas entre Corridors.
type Corridor struct {
	// ID é o identificador estável do Corridor, na ordem de criação.
	ID CorridorID
	// FromRoomID é a Room de origem da aresta; sempre distinta de
	// ToRoomID.
	FromRoomID RoomID
	// ToRoomID é a Room de destino da aresta; sempre distinta de
	// FromRoomID.
	ToRoomID RoomID
	// FromDoorID é a Door usada na Room de origem.
	FromDoorID DoorID
	// ToDoorID é a Door usada na Room de destino.
	ToDoorID DoorID
	// Cells são as Cells internas do traçado, ordenadas do lado From até
	// o lado To, excluindo as Cells das Rooms de extremidade. É vazio
	// quando as Rooms são adjacentes. A sequência é sempre 4-conexa.
	Cells []Cell
	// PlantID é o metadado de asset selecionado do catálogo, ou a string
	// vazia quando Config.PlantCatalog está ausente. Nunca altera
	// topologia.
	PlantID PlantID
	// Tags é a cópia canônica das tags da Plant selecionada; vazio quando
	// não há catálogo ou a Plant não declara tags.
	Tags []string
}

// Door é a abertura lógica de uma Room identificada por Cell e Direction
// cardinal. Door é única por (RoomID, At, Direction), mesmo quando múltiplas
// arestas a utilizam.
type Door struct {
	// ID é o identificador estável da Door, na ordem de criação.
	ID DoorID
	// RoomID é a Room à qual esta abertura pertence.
	RoomID RoomID
	// At é uma Cell de borda pertencente ao footprint da Room dona.
	At Cell
	// Direction é a direção do primeiro passo interno do Corridor a
	// partir da Room; com Cells vazias, é a direção entre as extremidades.
	Direction Direction
	// CorridorIDs lista, em ordem crescente, uma ou mais arestas que usam
	// esta abertura.
	CorridorIDs []CorridorID
}
