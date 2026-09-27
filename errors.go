package daedalus

import "errors"

// Erros sentinela das categorias de falha do SDK (Apêndice B da
// especificação). Eles definem categorias, não a representação concreta:
// o gerador e a validação os devolvem envolvidos com contexto via
// fmt.Errorf e %w, e o chamador deve testá-los exclusivamente com
// errors.Is. Nenhuma falha devolve Layout parcial.
//
// No serviço gRPC, o mapeamento é: ErrInvalidConfig → InvalidArgument,
// ErrLimitExceeded → ResourceExhausted, expiração de deadline →
// DeadlineExceeded e cancelamento do chamador → Canceled. Cancelamento e
// deadline usam diretamente context.Canceled e context.DeadlineExceeded,
// sem sentinelas próprios.
var (
	// ErrInvalidConfig marca uma Config que viola faixas, finitude
	// numérica, formato de catálogo, regiões de densidade ou solicitações
	// de papel. A validação ocorre antes de qualquer stream aleatório.
	ErrInvalidConfig = errors.New("daedalus: configuração inválida")

	// ErrLimitExceeded marca uma Config que excede os limites v1 de
	// produto (MaxCells ou MaxRooms). É detectada antes de alocação,
	// geração ou consumo de RNG, e a Config nunca é truncada
	// silenciosamente.
	ErrLimitExceeded = errors.New("daedalus: limite de produto excedido")

	// ErrNoCompatiblePlant marca uma Config válida cujo catálogo não
	// contém nenhuma RoomPlant cujas DoorDirections suportam as
	// Directions necessárias de uma Room e cujas Tags contêm as
	// RequiredTags do papel atribuído.
	ErrNoCompatiblePlant = errors.New("daedalus: nenhuma plant compatível")

	// ErrUnroutableEdge marca uma aresta cujas duas rotas em L cruzam uma
	// terceira Room e para a qual a busca em largura determinística não
	// encontra caminho ortogonal entre as extremidades.
	ErrUnroutableEdge = errors.New("daedalus: aresta sem rota ortogonal")
)
