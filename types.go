package daedalus

// Seed é a fonte de todos os streams aleatórios determinísticos de uma
// solicitação de geração. É um inteiro sem sinal de 64 bits e todos os
// valores são aceitos: zero é válido e não significa "aleatório". A mesma
// Seed combinada à mesma Config efetiva produz o mesmo Layout durante toda
// a major v1, em qualquer plataforma suportada.
type Seed uint64

// RoomID identifica uma Room dentro de um Layout. IDs são índices estáveis
// na ordem de criação, iniciando em 0, e sua ordem canônica é numérica
// ascendente.
type RoomID uint32

// CorridorID identifica um Corridor dentro de um Layout. IDs são índices
// estáveis na ordem de criação, iniciando em 0, e sua ordem canônica é
// numérica ascendente.
type CorridorID uint32

// DoorID identifica uma Door dentro de um Layout. IDs são índices estáveis
// na ordem de criação, iniciando em 0, e sua ordem canônica é numérica
// ascendente.
type DoorID uint32

// PlantID é um identificador opaco de asset resolvido pelo jogo chamador;
// Daedalus nunca o interpreta. É uma string UTF-8 não vazia quando presente,
// comparada byte a byte e sensível a maiúsculas. O valor zero (string vazia)
// significa ausência de Plant associada.
type PlantID string

// Cell é uma coordenada inteira (X, Y) do Grid, nunca uma posição em pixel.
// Uma Cell é válida somente dentro de um Grid: 0 ≤ X < Width e
// 0 ≤ Y < Height. A ordem canônica de Cell é Y e depois X.
type Cell struct {
	// X é a coordenada horizontal da Cell, em Cells, a partir de zero.
	X int32
	// Y é a coordenada vertical da Cell, em Cells, a partir de zero.
	Y int32
}

// Direction é uma das quatro direções cardinais do Grid. Diagonais são
// inválidas. A ordem canônica é North, East, South, West, com valores
// crescentes a partir de zero; o valor zero é portanto DirectionNorth.
type Direction int

const (
	// DirectionNorth aponta para Y decrescente: vetor (0, -1).
	DirectionNorth Direction = iota
	// DirectionEast aponta para X crescente: vetor (1, 0).
	DirectionEast
	// DirectionSouth aponta para Y crescente: vetor (0, 1).
	DirectionSouth
	// DirectionWest aponta para X decrescente: vetor (-1, 0).
	DirectionWest
)

// Delta devolve o vetor unitário da Direction em coordenadas de Cell:
// North=(0,-1), East=(1,0), South=(0,1), West=(-1,0). Uma Direction fora
// dos valores declarados devolve a Cell zero; o chamador deve usar somente
// as quatro constantes do tipo.
func (d Direction) Delta() Cell {
	switch d {
	case DirectionNorth:
		return Cell{X: 0, Y: -1}
	case DirectionEast:
		return Cell{X: 1, Y: 0}
	case DirectionSouth:
		return Cell{X: 0, Y: 1}
	case DirectionWest:
		return Cell{X: -1, Y: 0}
	}
	return Cell{}
}

// Opposite devolve a Direction oposta: North↔South e East↔West. Aplicada
// duas vezes, devolve a Direction original. Uma Direction fora dos valores
// declarados devolve DirectionNorth; o chamador deve usar somente as quatro
// constantes do tipo.
func (d Direction) Opposite() Direction {
	switch d {
	case DirectionNorth:
		return DirectionSouth
	case DirectionSouth:
		return DirectionNorth
	case DirectionEast:
		return DirectionWest
	case DirectionWest:
		return DirectionEast
	}
	return DirectionNorth
}

// CellKind é o estado físico de uma Cell do Grid. O valor zero é
// CellKindEmpty.
type CellKind int

const (
	// CellKindEmpty marca uma Cell livre, sem Room nem Corridor.
	CellKindEmpty CellKind = iota
	// CellKindRoom marca uma Cell ocupada por exatamente uma Room.
	CellKindRoom
	// CellKindCorridor marca uma Cell interna de um ou mais Corridors.
	CellKindCorridor
)

// CellState é o estado físico de uma Cell do Grid gerado.
type CellState struct {
	// At é a coordenada desta Cell no Grid.
	At Cell
	// Kind é o estado físico da Cell: Empty, Room ou Corridor.
	Kind CellKind
	// RoomID referencia a Room que ocupa a Cell. É não nil se e somente se
	// Kind == CellKindRoom; nos demais casos é nil e não deve ser lido.
	RoomID *RoomID
	// CorridorIDs lista, em ordem crescente, todos os Corridors que
	// contêm esta Cell. É não vazio se e somente se Kind ==
	// CellKindCorridor; uma Cell de Corridor pode ser compartilhada por
	// mais de um Corridor.
	CorridorIDs []CorridorID
}

// CorridorOrder escolhe o cotovelo preferido do traçado ortogonal em L de
// um Corridor. O valor zero é CorridorOrderXThenY, que é também o padrão
// de Config.
type CorridorOrder int

const (
	// CorridorOrderXThenY traça primeiro o eixo X e depois o eixo Y: para
	// uma conexão de A a B, o cotovelo é (B.X, A.Y).
	CorridorOrderXThenY CorridorOrder = iota
	// CorridorOrderYThenX traça primeiro o eixo Y e depois o eixo X: para
	// uma conexão de A a B, o cotovelo é (A.X, B.Y).
	CorridorOrderYThenX
)

// RoomRole é o papel temático opcional de uma Room, atribuído
// deterministicamente sobre a árvore de backbone a partir de
// Config.RoomRoleRequests.
type RoomRole int

const (
	// RoomRoleStart marca a Room inicial. Recebe sempre RoomID 0, a
	// primeira Room aceita pelo posicionamento.
	RoomRoleStart RoomRole = iota
	// RoomRoleBoss marca a Room de chefe: a Room não atribuída mais
	// distante de Start pela distância de caminho ponderado na árvore.
	// Exige uma solicitação RoomRoleStart na mesma Config.
	RoomRoleBoss
	// RoomRoleTreasure marca Rooms de tesouro: as Rooms ainda não
	// atribuídas mais distantes de Start, em ordem, até Count.
	RoomRoleTreasure
)
