package daedalus

// Limites v1 de produto (decisões D1 e D2 da especificação). Valem
// igualmente para o SDK e para o serviço gRPC: uma Config que exceda Width,
// Height, o produto de Cells ou MaxRooms falha com ErrLimitExceeded antes de
// qualquer alocação, geração ou consumo de RNG, e nunca é truncada
// silenciosamente.
const (
	// MaxCells é o produto máximo Width × Height de um Grid: 65.536 Cells,
	// correspondente a um Grid de até 256 × 256.
	MaxCells = 65536
	// MaxRooms é a quantidade máxima de Rooms por Layout: 256. É proteção
	// de latência para geração sob demanda, não pedido para preencher o
	// Grid.
	MaxRooms = 256
)

// Config é a entrada completa de uma solicitação de geração de dungeon.
// Ela existe somente como valor Go passado ao SDK ou como mensagem protobuf
// de uma solicitação gRPC; não há arquivo de configuração, JSON/YAML humano
// nem presets.
//
// Width, Height e Seed são obrigatórios e não têm defaults seguros. Os
// demais campos têm defaults documentados por campo; o valor zero de Config
// não é uma Config válida. Uma Config inválida falha com ErrInvalidConfig
// (ou ErrLimitExceeded quando excede os limites de produto) antes de qualquer
// geração.
type Config struct {
	// Width é a largura do Grid, em Cells: faixa 1..256, com
	// Width × Height ≤ MaxCells. Obrigatório.
	Width uint32
	// Height é a altura do Grid, em Cells: faixa 1..256, com
	// Width × Height ≤ MaxCells. Obrigatório.
	Height uint32
	// CellSize é o tamanho de uma Cell em unidades do chamador: finito e
	// > 0. Padrão 1.0. É copiado para Layout somente; nenhum algoritmo o
	// converte.
	CellSize float64
	// Seed é a fonte dos streams aleatórios da solicitação. Obrigatório;
	// todos os valores uint64 são aceitos e zero não significa aleatório.
	Seed Seed
	// MinDistance é a distância euclidiana mínima, em Cells, entre centros
	// de Rooms: finito e ≥ 1.0. Padrão 6.0.
	MinDistance float64
	// MaxAttempts é o máximo de candidatos Poisson por ponto ativo:
	// faixa 1..1024. Padrão 30.
	MaxAttempts uint32
	// MaxRooms é o máximo de Rooms aceitas: faixa 1..MaxRooms (limite de
	// produto). Padrão 256. Encerra o posicionamento quando atingido.
	MaxRooms uint32
	// CorridorOrder escolhe o cotovelo L preferido do roteamento.
	// Padrão CorridorOrderXThenY (valor zero).
	CorridorOrder CorridorOrder
	// ExtraEdgeCount é a quantidade máxima de arestas curtas descartadas
	// reintroduzidas após o backbone: faixa 0..MaxRooms×(MaxRooms-1)/2.
	// Padrão 0, que desliga ciclos e garante que o grafo é uma árvore.
	ExtraEdgeCount uint32
	// RoomRoleRequests lista pedidos declarativos de papéis temáticos, no
	// máximo uma entrada por RoomRole. Padrão vazio, que desliga Rooms
	// temáticas e deixa Role ausente em todas as Rooms.
	RoomRoleRequests []RoomRoleRequest
	// DensityRegions lista retângulos de Grid que substituem MinDistance
	// por uma distância local. As regiões devem ser não vazias, internas
	// ao Grid e sem sobreposição de Cells. Padrão vazio, que usa
	// MinDistance uniforme e desliga biomas.
	DensityRegions []DensityRegion
	// PlantCatalog é o catálogo opcional de metadados de assets, ou nil
	// quando ausente; nesse caso PlantID e Tags ficam vazios no Layout.
	PlantCatalog *PlantCatalog
}

// RoomRoleRequest é um pedido declarativo de atribuição de um RoomRole e
// das tags de Plant exigidas para as Rooms que o receberem.
type RoomRoleRequest struct {
	// Role é o papel temático solicitado: Start, Boss ou Treasure.
	Role RoomRole
	// Count é a quantidade de Rooms que recebem o papel: 1 para Start e
	// Boss; 0..MaxRooms para Treasure. Boss exige uma solicitação Start na
	// mesma Config, pois a distância sem origem não é definida.
	Count uint32
	// RequiredTags lista tags UTF-8 não vazias e sem duplicatas que a
	// Plant selecionada precisa conter. Restringe a seleção de catálogo,
	// mas não muda a escolha topológica da Room.
	RequiredTags []string
}

// DensityRegion é um retângulo de Grid que substitui Config.MinDistance por
// uma distância local, permitindo biomas mais densos ou mais esparsos.
type DensityRegion struct {
	// Min é o canto mínimo do retângulo, em Cells, inclusivo.
	Min Cell
	// Max é o canto máximo do retângulo, em Cells, exclusivo. Deve ser
	// estritamente maior que Min em ambos os eixos e interno ao Grid.
	Max Cell
	// MinDistance é a distância euclidiana mínima local, em Cells: finita
	// e ≥ 1.0.
	MinDistance float64
}

// PlantCatalog é o catálogo engine-agnóstico de metadados de assets. Ele
// armazena somente dados: a resolução de PlantID para cena, prefab, tile ou
// mesh pertence ao jogo chamador. Quando presente em Config, ambas as listas
// devem ser não vazias, com IDs únicos.
type PlantCatalog struct {
	// Rooms lista as Plants disponíveis para Rooms; não vazia quando o
	// catálogo está presente.
	Rooms []RoomPlant
	// Corridors lista as Plants disponíveis para Corridors; não vazia
	// quando o catálogo está presente.
	Corridors []CorridorPlant
}

// RoomPlant descreve os metadados de uma Plant de Room do catálogo.
type RoomPlant struct {
	// ID é o identificador opaco do asset: obrigatório, UTF-8 não vazio e
	// único dentro do catálogo.
	ID PlantID
	// Tags lista tags UTF-8 não vazias, sem duplicatas; pode ser vazia.
	Tags []string
	// Weight é o peso relativo de seleção ponderada: faixa 1..2^32-1.
	Weight uint32
	// DoorDirections declara as Directions de abertura que o asset
	// suporta: não vazio, sem duplicatas. Uma RoomPlant é compatível com
	// uma Room somente quando DoorDirections é superconjunto das
	// Directions usadas pelas Doors daquela Room.
	DoorDirections []Direction
}

// CorridorPlant descreve os metadados de uma Plant de Corridor do catálogo.
// Não declara geometria local porque ela deriva de Corridor.Cells.
type CorridorPlant struct {
	// ID é o identificador opaco do asset: obrigatório, UTF-8 não vazio e
	// único dentro do catálogo.
	ID PlantID
	// Tags lista tags UTF-8 não vazias, sem duplicatas; pode ser vazia.
	Tags []string
	// Weight é o peso relativo de seleção ponderada: faixa 1..2^32-1.
	Weight uint32
}
