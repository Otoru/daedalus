# Especificação de Engenharia do Daedalus

**Status:** especificação normativa v1 refinada — salas dinâmicas e HTTP local de desenvolvimento
**Escopo:** biblioteca Go importável, subprocesso gRPC sob demanda e servidor HTTP local opcional para desenvolvimento/depuração
**Idioma:** este documento de especificação está em português. Todo o restante — documentação de pacote, comentários de documentação, comentários inline e de protocolo, textos de erro, nomes de testes, comentários de build e CI e textos da interface de desenvolvimento — está em inglês. Os identificadores de domínio e de API permanecem em inglês.
**Fonte de produto:** transcrição de vídeo educativo, qualificada no Apêndice D; é inspiração, não autoridade de implementação.

## 1. Finalidade e não-objetivos

Daedalus cria uma dungeon 2D discreta sob demanda quando um jogo solicita um novo andar ou dungeon. O jogo pode usar o SDK importável em processo ou o subprocesso gRPC fornecido. Durante desenvolvimento, uma flag pode habilitar um servidor HTTP adicional, com uma interface local para montar solicitações, gerar Layouts e inspecioná-los visualmente. Dada uma **Config** e uma **Seed**, devolve um **Layout** completo: Grid lógico, Rooms com footprints discretos de tamanho e forma variáveis, Corridors e Doors posicionadas nas bordas das Rooms.

O pacote raiz possui somente a geração determinística de Layout. Ele não renderiza, instancia objetos, carrega cenas, conhece Godot, Unity ou Bevy, calcula posições em pixel, escolhe assets de engine, constrói navmesh nem possui estado de jogo. **CellSize** é uma unidade opaca definida pelo chamador; nenhum algoritmo a converte.

O pipeline v1 é:

1. validar e normalizar Config;

1. pedir RoomPlacements de tamanho e forma configuráveis a um **Placer**, reservando todas as Cells de cada footprint;

1. pedir arestas topológicas a um **Connector**;

1. atribuir papéis temáticos solicitados sobre a árvore de backbone;

1. reintroduzir atalhos opcionais como arestas curtas descartadas;

1. traçar cada aresta como um Corridor ortogonal;

1. derivar Doors pelas extremidades da rota;

1. resolver metadados opcionais de plantas e materializar um Layout imutável.

O gerenciador central com autoridade exclusiva de mutação da transcrição não é copiado literalmente. Essa regra protegia uma árvore de cena Godot mutável. Daedalus constrói valores locais por fase e publica um Layout novo somente após sucesso; não existe estado parcial público para um especialista mutar. [PREMISSA P16]

### 1.1 Não-objetivos

- Renderização, instanciação de cena/prefab, carregamento de asset, matemática em pixel ou dependências de engine.

- Colisão física contínua, navmesh, entidades, loot, inimigos ou gameplay. A biblioteca gera footprints lógicos de Rooms em Cells discretas; o chamador continua responsável por renderização, pixels e resolução de assets de engine.

- Um roteador de Corridor globalmente ótimo, estética arquitetural ou proibição de Cells de Corridor compartilhadas.

- Carregamento dinâmico de código arbitrário de algoritmo pelo processo gRPC.

- Arquivos persistentes de configuração, presets editáveis, YAML de Config e JSON Schema. Config do SDK continua sendo valor Go, e a geração de jogo usa mensagens protobuf gRPC. O servidor HTTP de desenvolvimento definido na seção 2.3 aceita JSON transitório em memória para debug; isso não cria arquivo de configuração nem muda a API do jogo.

- Renderização em lote de engine (seção 17).

## 2. Estrutura do repositório, fronteira de dependências e runtime

Daedalus segue a forma da família Geppetto, com modelo de dados menor e adequado a uma solicitação de dungeon.

| Local | Responsabilidade |
| --- | --- |
| pacote raiz **daedalus** | SDK público, modelo Config/Layout, algoritmos embutidos, roteamento, validação, geração determinística e doc.go. Importa apenas a biblioteca padrão Go. |
| testes da raiz | Testes unitários, de propriedade, golden, benchmark, race e de pureza de imports via AST do SDK. |
| **cmd/daedalus** | Entrada do subprocesso; flags, sinais, composição fx e handshake de uma linha no stdout. |
| **internal/config** | Flags de processo, inclusive HTTP de debug, e limites do serviço; nunca a Config do gerador raiz. |
| **internal/service** | Validação gRPC, cancelamento/deadline, concorrência limitada, adaptação do SDK e mapeamento de status. |
| **internal/httpdebug** | Servidor HTTP opcional, endpoints locais de geração/health, UI estática de debug e adaptação JSON. |
| **internal/transport** | Propriedade de listener Unix socket/named pipe e TCP loopback. |
| **internal/logging** | Configuração zap que escreve somente em stderr. |
| **internal/gen/go/daedalus/v1** | Bindings protobuf gerados; nunca editados manualmente. |
| **proto/daedalus/v1** | Fonte protobuf versionada. |
| **buf.yaml**, **buf.gen.yaml** | Configuração de lint e geração de proto. |
| **.github/workflows** | Verificação de CI e release. |

O pacote raiz de produção tem teste de pureza de imports por AST equivalente ao do Geppetto: analisa arquivos não-teste da raiz, resolve cada import por go/build e falha se um import estiver fora de GOROOT. grpc, protobuf, fx, zap, pflag, testify, bindings gerados e configuração de processo ficam portanto fora da raiz. [PREMISSA P17]

O comando não tem estado entre gerações. Ele recebe uma solicitação completa, cria um Layout, devolve-o e não retém Layout, Seed ou stream aleatório para a próxima solicitação.

### 2.1 Subprocesso gRPC e handshake

O subprocesso existe porque jogos precisam de mapas em runtime quando o jogador entra em uma dungeon ou desce um andar; não é simetria pela simetria. O SDK em processo continua sendo a integração de menor latência, enquanto gRPC permite isolamento de processo e linguagem.

**cmd/daedalus** usa fx para construção/ciclo de vida, zap para logs, pflag para flags, grpc/protobuf para transporte e buf para bindings. O ciclo fx inicia listener e servidor gRPC e então emite exatamente um objeto JSON seguido de uma quebra de linha no stdout:

```
{"transport":"uds-or-tcp","addr":"resolved-address","pid":1234,"version":"vX.Y.Z"}
```

O cliente analisa essa primeira linha antes de conectar. stdout é contrato de fio: fx usa logger nulo e logs, diagnósticos e erros de flags vão para stderr. O desligamento marca o serviço como não atendendo, interrompe admissão, deixa o trabalho observar cancelamento/deadline, encerra gRPC graciosamente e limpa o listener. Se o HTTP de debug estiver habilitado, ele é iniciado no mesmo ciclo de vida e recebe desligamento gracioso junto ao gRPC. Falha ao vincular qualquer listener solicitado impede o startup completo; nenhum serviço parcialmente iniciado é anunciado. [PREMISSA P18 revisada]

O serviço v1 expõe **Generate** unário. A solicitação carrega todos os dados de Config, inclusive ExtraEdgeCount, RoomRoleRequests, DensityRegions e RoomGeometry; a resposta carrega Layout completo, com footprints de Room, ou status gRPC. O transporte sempre usa algoritmos embutidos; implementações Go injetadas pelo jogo existem apenas no SDK. Não há configuração global nem estado entre solicitações para essas capacidades.

### 2.2 Array-of-structs, não SoA

Protobuf usa mensagens aninhadas repetidas que espelham **Room**, **Corridor**, **Door** e **CellState**: array-of-structs (AoS ). Os campos de fio usam snake_case, por exemplo min_distance, max_attempts, placer_id e corridor_ids.

O payload SoA do Geppetto resolve um problema demonstrado de serialização/alocação para 50.000 agentes por tick. Daedalus devolve um Layout por solicitação, normalmente com dezenas ou centenas de Rooms, e gasta latência gerando topologia, não codificando arrays paralelos. SoA acrescentaria validação de offsets, diagnósticos piores e código sem remover o trabalho dominante. Solicitações concorrentes aumentam exigência de throughput do serviço, não o formato de cada Layout. Só uma medição que prove que alocação ou serialização protobuf viola materialmente o orçamento de latência pode justificar redesenho de fio versionado. [PREMISSA P19]

### 2.3 Servidor HTTP opcional de desenvolvimento

O processo pode iniciar, além do gRPC, um servidor HTTP local dedicado a desenvolvimento e depuração. O HTTP usa o mesmo Generator, validação, limites, cancelamento e semântica determinística do SDK/gRPC; ele não implementa um segundo gerador, não mantém estado entre solicitações e não grava Layouts em disco.

**Flags de processo** (em `internal/config`, nunca na Config raiz):

| Flag | Default | Regra |
| --- | --- | --- |
| `--http-debug-enabled` | `false` | Desligado por padrão. Se falso, nenhum listener HTTP é criado e as rotas de debug não existem. |
| `--http-debug-addr` | `127.0.0.1:8090` | Endereço IP literal de loopback e porta 1..65535; só é lido quando o servidor está habilitado. Rejeitar wildcard, IP não-loopback, hostname não literal e porta zero. |

O HTTP é independente do listener gRPC: habilitar ou desabilitar debug não muda o handshake de stdout, endereço, protocolo nem disponibilidade do gRPC. O endereço efetivo e a indicação de servidor ativo são registrados em stderr; payloads Config/Seed não são escritos em logs. Conflito de porta ou falha de bind, quando habilitado, falha o startup completo. Sinais de desligamento param admissão, cancelam gerações em curso e fecham o servidor HTTP graciosamente junto ao gRPC.

**Rotas v1**:

| Método e rota | Contrato |
| --- | --- |
| `GET /healthz` | `200 application/json`, por exemplo `{"status":"ok","version":"vX.Y.Z"}` quando atendendo; `503` durante shutdown. Não inicia geração. |
| `GET /` | Redireciona para `/debug/` na mesma origem. |
| `GET /debug/` | Interface de desenvolvimento para editar/enviar solicitação JSON e inspecionar o mapa. Conteúdo servido localmente, sem CDN, analytics ou recursos externos. |
| `POST /api/v1/generate` | Recebe uma solicitação equivalente à mensagem protobuf `GenerateRequest`, no formato JSON canônico ProtoJSON; retorna o Layout completo em ProtoJSON. |

A página `/debug/` oferece editor de JSON da solicitação, exemplo carregável que inclui dimensões, Seed e RoomGeometry, ação **Gerar mapa**, visualização de erro e resposta, cópia da solicitação/resposta e visualização interativa. A visualização usa Canvas/SVG local no navegador, sem alterar o Layout: pinta Cells Empty/Room/Corridor, diferencia RoomShape/RoomID, destaca Doors e rotas, e mostra coordenadas e metadados da Cell selecionada. O editor aceita o corpo completo, portanto campos avançados como DensityRegions, papéis e catálogo não ficam limitados a controles visuais pré-definidos. A UI não instancia assets de engine nem altera Cells ou Layout devolvidos.

**Contrato HTTP**:

- A requisição segue o ProtoJSON de `GenerateRequest`, com nomes snake_case, mesmos campos e regras de presença/default do protobuf. Inteiros `uint64` como Seed usam a representação ProtoJSON decimal em string. Config inválida é rejeitada; o servidor não completa silenciosamente campos requeridos diferentes dos defaults definidos pela spec.

- `Content-Type` deve ser `application/json`; corpo limitado a `MaxHTTPDebugBodyBytes=1 MiB`. Tamanho excedido retorna `413`. A resposta bem-sucedida é `200 application/json` com Layout inteiro e Content-Length limitado pela serialização do Grid/Layout dentro de MaxCells/MaxRooms.

- Status: `400` JSON/Config inválidos; `413` corpo ou limite de recursos excedido; `422` ErrNoCompatiblePlant ou ErrUnroutableEdge; `504` deadline excedido; `500` falha interna sem stack trace ou dados sensíveis. Erros são JSON estruturado com `code`, `message` em português e `request_id` opaco.

- Cada POST respeita Context/timeout da conexão e usa a mesma admissão `MaxConcurrentGenerations` do serviço; RPCs e pedidos HTTP competem pelo mesmo limite, sem fila HTTP ilimitada. Cliente que desconecta cancela a geração HTTP correspondente.

- Acesso é somente loopback: o handler valida `RemoteAddr` como loopback mesmo se houver proxy local. CORS não é habilitado; a UI faz chamadas same-origin. Não se fornece autenticação na v1 porque o bind é restrito a loopback. Exposição remota, bind wildcard ou proxy reverso não são suportados.

- Endpoints de debug aceitam somente algoritmos embutidos; não carregam plugins, executam comandos, leem arquivos de usuário, salvam Fixtures ou expõem filesystem. Logs registram request_id, status e duração, nunca o corpo.

A rota HTTP existe para debug local e não substitui gRPC nem cria obrigação de disponibilizar endpoint web em release distribuído. O empacotamento pode desativar a flag por padrão; assets da UI são embedados no executável e nenhuma dependência HTTP entra no pacote raiz. [DECISÕES D6-D8]

## 3. Convenções de nome e documentação

- Este documento de especificação está em português. Todo o restante — documentação de pacote, comentários de documentação, comentários inline e de protocolo, textos de erro, nomes de testes, comentários de build e CI e textos da interface de desenvolvimento — está em inglês. Os identificadores de domínio e de API permanecem em inglês. Conteúdo de jogo exibido ao jogador não é restringido.

- Tipos, funções, métodos e campos Go exportados usam PascalCase. Nomes de pacote são palavras curtas em minúsculas. Nomes não exportados usam lowerCamelCase idiomático.

- Campos JSON e protobuf usam snake_case. Nomes de fio não tornam identificadores Go snake_case.

- Constantes de calibração não são exportadas e têm nome. Nenhum literal sem explicação é permitido em produção: números configuráveis pertencem a Config; invariantes fixos pertencem a constantes não exportadas nomeadas e comentadas.

- Estes nomes de domínio são vocabulário normativo de API pública: **Layout**, **Room**, **Corridor**, **Door**, **Grid**, **Cell**, **Config**, **Seed**, **Placer**, **Connector**. Renomeá-los é mudança incompatível de API.

- IDs, IDs de algoritmo, tags e campos de fio são ASCII salvo campo que declare UTF-8; PlantID e tags são strings UTF-8.

A regra evita a custosa migração de vocabulário público já paga pelo projeto irmão. [PREMISSA P20]

## 4. Glossário

| Termo | Significado normativo |
| --- | --- |
| **Cell** | Coordenada inteira (X,Y ) do Grid, nunca posição em pixel. |
| **Grid** | Espaço retangular de Width × Height Cells, onde 0 ≤ X < Width e 0 ≤ Y < Height. |
| **CellState** | Estado físico de uma Cell: Empty, Room ou Corridor. |
| **Room** | Vértice topológico com footprint finito, não vazio e 4-conexo de Cells. |
| **Room footprint** | Conjunto de Cells ocupadas por uma Room, sem incluir Doors nem Corridors. |
| **RoomShape** | Máscara discreta de footprint: Rectangle, L, T, Cross ou Circle. |
| **Room bounds** | Menor retângulo alinhado ao Grid que contém todo o footprint da Room. |
| **Room anchor** | Cell ocupada `Room.At`, usada para posicionamento, distâncias e desempates. |
| **Corridor** | Aresta topológica e suas Cells ortogonais externas aos footprints, em ordem. |
| **Door** | Abertura lógica numa Cell de borda da Room, identificada por At e Direction cardinal. |
| **Direction** | North, East, South ou West; diagonais são inválidas. |
| **Layout** | Resultado completo, imutável e gerado com sucesso. |
| **Plant** | Metadado engine-agnóstico para asset externo. |
| **PlantID** | Identificador opaco de asset resolvido pelo jogo. |
| **Seed** | Fonte uint64 de streams aleatórios determinísticos por solicitação. |
| **MinDistance** | Distância euclidiana mínima, em Cells, entre âncoras de Rooms. |
| **MinRoomGap** | Número de camadas vazias mínimas entre footprints, medido por distância de Chebyshev entre Cells ocupadas. |
| **MaxAttempts** | Máximo de RoomPlacements tentados por ponto ativo Poisson. |
| **MaxRooms** | Máximo de Rooms aceitas, limitando trabalho interativo; não limita Cells ocupadas. |
| **ExtraEdgeCount** | Quantidade máxima de arestas curtas descartadas reintroduzidas após o backbone. |
| **RoomRole** | Papel temático opcional de Room: Start, Boss ou Treasure. |
| **RoomRoleRequest** | Pedido declarativo de atribuição de um RoomRole e de tags de Plant exigidas. |
| **DensityRegion** | Retângulo de Grid que substitui MinDistance por uma distância local entre âncoras. |
| **RoomGeometry** | Parâmetros de faixa de dimensões, área, espaçamento e pesos de RoomShape. |
| **Placer** | Algoritmo que propõe RoomPlacements válidos, incluindo footprint e dimensões. |
| **Connector** | Algoritmo que escolhe arestas Room-a-Room. |
| **MST** | Conjunto de arestas conexo e acíclico com menor peso total escolhido. |

## 5. Contrato público de dados

Nomes e semântica desta seção são normativos. T? significa opcional e []T significa sequência ordenada. Dados ausentes nunca são inferidos de asset de engine.

### 5.1 Tipos escalares e enumerações

| Tipo | Valores e regra |
| --- | --- |
| Seed | inteiro sem sinal de 64 bits, 0..2^64-1; obrigatório. Zero é válido e não significa aleatório. |
| RoomID, CorridorID, DoorID | índices uint32, estáveis na ordem de criação, iniciando em 0. |
| Cell | { X: int32, Y: int32 }; válida somente dentro de Grid. |
| Direction | North=(0,-1), East=(1,0), South=(0,1), West=(-1,0). |
| CellKind | Empty, Room, Corridor. |
| PlantID | string UTF-8 não vazia quando presente; comparação byte a byte e sensível a maiúsculas. |

A ordem canônica de Direction é North, East, South, West. A ordem canônica de Cell é Y e depois X. A ordem canônica de ID é numérica ascendente.

### 5.2 Layout

```go
Layout {
  Seed: Seed
  Grid: Grid
  Rooms: []Room
  Corridors: []Corridor
  Doors: []Door
}

Grid {
  Width: uint32
  Height: uint32
  CellSize: float64
  Cells: []CellState              // exatamente Width × Height, Y depois X
}

CellState {
  At: Cell
  Kind: CellKind
  RoomID: RoomID?                 // presente sse Kind == Room
  CorridorIDs: []CorridorID       // crescente; não vazio sse Kind == Corridor
}

Room {
  ID: RoomID
  At: Cell                        // âncora ocupada do footprint
  Shape: RoomShape
  Origin: Cell                    // canto superior esquerdo da bounding box
  Width: uint32                   // largura da bounding box, em Cells
  Height: uint32                  // altura da bounding box, em Cells
  Cells: []Cell                   // footprint absoluto, ordem Y depois X
  Role: RoomRole?
  PlantID: PlantID?
  Tags: []string
  DoorIDs: []DoorID               // ordem At (Y, X), depois Direction
}

Corridor {
  ID: CorridorID
  FromRoomID: RoomID
  ToRoomID: RoomID
  FromDoorID: DoorID
  ToDoorID: DoorID
  Cells: []Cell                   // somente Cells externas às Rooms, From até To
  PlantID: PlantID?
  Tags: []string
}

Door {
  ID: DoorID
  RoomID: RoomID
  At: Cell                        // Cell de borda pertencente ao footprint
  Direction: Direction            // aponta da Room para fora
  CorridorIDs: []CorridorID       // uma ou mais arestas usando a abertura
}

RoomShape = Rectangle | L | T | Cross | Circle
```

As máscaras de forma são canônicas, sem rotação aleatória implícita:

- `Rectangle`: todas as Cells da bounding box.

- `L`: linha superior completa unida à coluna esquerda completa; Width e Height ≥2.

- `T`: linha superior completa unida à coluna central completa; Width ≥3 e Height ≥2. Coluna central `(Width-1) div 2`.

- `Cross`: linha central completa unida à coluna central completa; Width e Height ≥3. Para dimensão par, índice central `(N-1) div 2`.

- `Circle`: bounding box quadrada de diâmetro ímpar `D=Width=Height`, D≥5. Seja `r=(D-1)/2` e centro `c=(r,r)`; ocupa exatamente as Cells cujo `(x-c.X)^2+(y-c.Y)^2 ≤ r^2`. A comparação é inteira, sem arredondamento.

As máscaras são conjuntos (a união não duplica Cells), 4-conexas e ordenadas por Y e depois X ao materializar `Room.Cells`. `Room.At` é a primeira Cell ocupada em ordem canônica Y/X. `Room.Origin`, Width e Height descrevem a bounding box; sua origem pode não ser ocupada, como no Cross. Todos os offsets da máscara são relativos à Origin. O offset da primeira Cell ocupada é (0,0) para Rectangle/L/T, `((Width-1) div 2, 0)` para Cross e `(r,0)` para Circle. Essas regras permitem converter Room.At em Origin sem ambiguidade.

`Grid.Cells` é o espaço físico: cada Cell do footprint tem `Kind == Room` e `RoomID` correspondente. `Corridor.Cells` contém apenas Cells externas a todos os footprints. Uma Cell de Corridor pode ocorrer em mais de um Corridor.Cells; CorridorIDs contém todos os IDs crescentes. `Room.Cells` e `Grid.Cells` são cópias sem alias mutável entre resultados.

**Invariantes de Layout**

- Existe pelo menos uma Room. Cada footprint é não vazio, único, 4-conexo, inteiramente dentro do Grid, e possui exatamente a máscara declarada por Shape/Origin/Width/Height.

- `Room.At` pertence a `Room.Cells`; Width e Height são exatamente as dimensões da bounding box.

- Footprints de Rooms diferentes não se sobrepõem e respeitam MinRoomGap quando configurado. Toda Cell de Room referencia exatamente um RoomID válido; nenhuma Cell Empty/Corridor referencia RoomID.

- Com n Rooms, existem ao menos n-1 Corridors se n>1 e zero se n=1; há exatamente n-1 quando ExtraEdgeCount=0.

- Todo Corridor possui Rooms de extremidade distintas, duas Doors e sequência de Cells 4-conexa, sem cruzar footprint de nenhuma Room.

- Todo CellState de Corridor referencia ao menos um Corridor que o contém; Empty não referencia nenhum.

- Door.At pertence ao footprint e está na borda; At+Direction está dentro do Grid e fora do mesmo footprint. Door é única por (RoomID, At, Direction).

- O grafo Room/Corridor é conectado. Ele é uma árvore se ExtraEdgeCount=0; com ExtraEdgeCount>0 pode conter ciclos.

- Role está ausente quando RoomRoleRequests está vazio; fora disso, cada Role solicitado respeita Count e nenhuma Room recebe mais de um Role em v1.

- Todos os slices retornados, inclusive aninhados, são alocados por solicitação. Mutar um Layout não pode mutar resultado de outra solicitação. [PREMISSA P21]

### 5.3 Config

```go
Config {
  Width: uint32
  Height: uint32
  CellSize: float64
  Seed: Seed
  MinDistance: float64
  MaxAttempts: uint32
  MaxRooms: uint32
  CorridorOrder: CorridorOrder
  ExtraEdgeCount: uint32
  RoomRoleRequests: []RoomRoleRequest
  DensityRegions: []DensityRegion
  RoomGeometry: RoomGeometry?
  PlantCatalog: PlantCatalog?
}

RoomGeometry {
  MinWidth: uint32
  MaxWidth: uint32
  MinHeight: uint32
  MaxHeight: uint32
  MaxFootprintCells: uint32
  MinRoomGap: uint32
  Shapes: []RoomShapeWeight
}

RoomShapeWeight {
  Shape: RoomShape
  Weight: uint32
}

CorridorOrder = XThenY | YThenX
RoomRole = Start | Boss | Treasure

RoomRoleRequest {
  Role: RoomRole
  Count: uint32
  RequiredTags: []string
}

DensityRegion {                 // retângulo semiaberto: Min inclusivo, Max exclusivo
  Min: Cell                     // incluso; cobre X em [Min.X, Max.X) e Y em [Min.Y, Max.Y)
  Max: Cell                     // exclusivo; pode igualar a dimensão do Grid; estritamente maior é inválido; uma Cell é Max = Min + (1,1)
  MinDistance: float64
}

PlantCatalog {
  Rooms: []RoomPlant
  Corridors: []CorridorPlant
}

RoomPlant {
  ID: PlantID
  Tags: []string
  Weight: uint32
  DoorDirections: []Direction
}

CorridorPlant {
  ID: PlantID
  Tags: []string
  Weight: uint32
}
```

| Campo | Obrigatório / padrão | Faixa e unidade | Regra |
| --- | --- | --- | --- |
| Width, Height | obrigatórios | 1..256 Cells | produto ≤ MaxCells=65.536. |
| CellSize | padrão 1.0 | finito, >0 | copiado para Layout; não altera topologia. |
| Seed | obrigatório | uint64 | todos os valores aceitos. |
| MinDistance | padrão 6.0 | finito, ≥1.0 Cells | distância mínima entre âncoras. |
| MaxAttempts | padrão 30 | 1..1024 por ponto ativo | limita tentativas completas, inclusive validação do footprint. |
| MaxRooms | padrão 256 | 1..256 Rooms | teto de Rooms aceitas, não de Cells ocupadas. |
| CorridorOrder | padrão XThenY | enum | cotovelo preferido quando a geometria permitir rota L. |
| ExtraEdgeCount | padrão 0 | 0..MaxRooms×(MaxRooms-1)/2 | atalhos determinísticos. |
| RoomRoleRequests | padrão [] | conforme regra existente | no máximo uma entrada por RoomRole. |
| DensityRegions | padrão [] | retângulos não vazios e sem sobreposição, semiabertos: Min inclusivo e Max exclusivo, X em [Min.X, Max.X) e Y em [Min.Y, Max.Y); Max pode igualar a dimensão do Grid, Max estritamente maior é inválido, e uma região de uma Cell escreve-se Max = Min + (1,1) | substituem MinDistance local entre âncoras. |
| RoomGeometry | padrão dinâmico | dimensões/área em Cells; MinRoomGap 0..256 | ausência aplica o perfil dinâmico padrão descrito abaixo. |
| PlantCatalog | ausente | opcional | se presente, ambas listas não vazias; IDs únicos, Weight ≥1. |

`RoomGeometry` ausente **não** significa Room 1×1: após validação, normaliza para o perfil dinâmico padrão. O perfil base usa `MinWidth=min(3, Width)`, `MaxWidth=min(9, Width)`, `MinHeight=min(3, Height)`, `MaxHeight=min(9, Height)`, `MaxFootprintCells=81`, `MinRoomGap=1` e pesos Rectangle=4, L=2, T=2, Cross=1, Circle=2. Para Circle, somente diâmetros ímpares de 5 até o menor máximo dimensional são elegíveis. Formas que não tenham nenhuma dimensão válida no Grid são excluídas do perfil normalizado; pelo menos Rectangle sempre permanece, incluindo 1×1. O perfil normalizado efetivo integra os dados determinísticos da solicitação.

Se o chamador enviar `RoomGeometry`, todos os campos são obrigatórios, salvo regra explicitamente indicada; dimensões mínimas ≥1, máximas ≥ mínimas e ≤ dimensão do Grid; `MaxFootprintCells` entre 1 e 4096; `MinRoomGap` entre 0 e 256; `Shapes` não vazia, sem Shape duplicada, pesos ≥1. Deve existir pelo menos uma combinação forma/dimensões com área ≤ MaxFootprintCells; caso contrário, Config inválida. Formas L/T/Cross respeitam dimensões mínimas do contrato de máscaras. Circle exige Width=Height ímpar e ≥5; pares dimensão sem máscara válida e são excluídos da lista de combinações permitidas. Soma de pesos é verificada contra overflow. Uma Room aceita precisa satisfazer simultaneamente a distância `MinDistance` entre âncoras e o espaçamento de footprints.

Width, Height, Seed e as configurações requeridas não têm defaults seguros. Layout não repete tuning, pois descreve geometria, não entrada do processo. `MaxRooms` é proteção de latência, não pedido para preencher Grid. [PREMISSA P37 revisada]

Cada Plant ID é obrigatório; Tags é sequência possivelmente vazia, sem duplicatas, de strings UTF-8 não vazias; Weight é 1..2^32-1. `RoomPlant.DoorDirections` não é vazio, não tem duplicatas e contém Direction. Cada RoomRoleRequest tem Role válido, RequiredTags sem duplicatas e Count válido; Boss exige solicitação Start. DensityRegions não podem se sobrepor. Geometria inválida é detectada antes de alocação proporcional, geração ou consumo de RNG.

Config válida pode falhar deterministicamente com ErrNoCompatiblePlant se nenhuma RoomPlant suporta Directions necessárias, ou ErrUnroutableEdge quando não existe caminho entre bordas das Rooms. Nunca devolve Layout parcial.

### 5.4 Catálogo de plantas engine-agnóstico

PlantCatalog armazena somente metadados. O jogo possui a resolução PlantID para cena, prefab, tile ou mesh e pode ignorar PlantID. Após topologia, papéis e Doors serem conhecidos, seleciona-se RoomPlant cujo DoorDirections contém todas as direções usadas e cujas Tags contêm RequiredTags do Role atribuído; múltiplas portas da mesma direção contam como uma direção requerida para esta seleção. CorridorPlant é selecionado por Corridor. IDs candidatos são ordenados antes da seleção ponderada. Tags são copiadas para Layout. [PREMISSA P3][PREMISSA P23]

## 6. Algoritmos de geração plugáveis

Algoritmos são extensíveis desde v1. O Placer e Connector recebem geometria de Rooms explicitamente; não podem inferi-la de assets de engine.

```go
Placer
  Place(PlacementRequest) -> []RoomPlacement ou erro

RoomPlacement {
  Shape: RoomShape
  Origin: Cell                    // origem da bounding box; At é derivado da primeira Cell ocupada
  Width: uint32
  Height: uint32
  Cells: []Cell                   // offsets locais canônicos da máscara
}

PlacementRequest {
  Context
  limites de Grid
  MinDistance
  DensityRegions
  RoomGeometry normalizada
  MaxAttempts
  MaxRooms
  Seed
}

Connector
  Connect(ConnectionRequest) -> []Connection ou erro

ConnectionRequest {
  Context
  Rooms com RoomID, âncora, shape, bounds e footprint
  limites de Grid
  ExtraEdgeCount
  Seed
}

Connection { FromRoomID, ToRoomID }
```

O Generator padrão usa **poisson_disk_rooms_v1** e **prim_rooms_v1**, com geometria dinâmica aplicada mesmo quando RoomGeometry não é enviado. As máscaras v1 incluem Rectangle, L, T, Cross e Circle. Chamadores SDK podem injetar Placer/Connector próprios; gRPC usa somente estes algoritmos embutidos.

Offsets de RoomPlacement são relativos a Origin, únicos e devem coincidir exatamente com máscara da Shape e dimensão declaradas. Generator valida todos os placements antes de atribuir IDs ou publicar estado. A âncora materializada é a primeira Cell ocupada em ordem Y/X. Cada placement deve estar no Grid, respeitar MinRoomGap e MinDistance, e não colidir com Rooms previamente aceitas.

Placer não atribui Plants, Doors, IDs nem muta Layout. Connector devolve somente arestas, não roteia Cells nem muta Layout. Resultado deve ser grafo simples conectado; Generator rejeita Connections duplicadas, próprias, desconhecidas ou desconectadas. Arestas são roteadas depois, considerando footprints completos. Generator possui IDs, roteamento, seleção de catálogo e materialização final.

`prim_rooms_v1` usa distância euclidiana entre centros das bounding boxes de Rooms como peso quando há footprints multi-Cell; para 1×1, coincide com distância entre âncoras. Desempates canônicos seguem RoomID e coordenadas Y/X. [PREMISSA P8 revisada]

Chamadores SDK podem fornecer Placer e Connector próprios. Plugins devem ser determinísticos, respeitar Context e ser concorrentes quando compartilhados. gRPC usa apenas algoritmos embutidos. Alterar o contrato público de Placer para fornecer máscaras multi-Cell é mudança incompatível em relação à interface anterior; esta versão refinada define o contrato normativo antes do release v0.1.0.

## 7. Posicionamento embutido: Poisson Disk

O Poisson Disk embutido propõe âncoras para Rooms. Cada âncora só é aceita depois que forma, dimensões e footprint completos forem selecionados e validados. A primeira Room é a primeira forma/dimensão selecionada pelo stream de geometria que caiba no Grid; entre âncoras admissíveis `Room.At`, escolhe-se a de menor distância quadrada ao centro geométrico, empate Y/X. Para cada candidata, `BuildPlacement` deriva Origin subtraindo o offset da primeira Cell ocupada definido na seção 5. `Room.At` é a âncora usada pelo Poisson Disk e a primeira Cell ocupada em ordem canônica.

A distância Poisson é entre âncoras. Coordenadas são Cells inteiras e usam aritmética int64:

```
dx = int64(a.X) - int64(b.X)
dy = int64(a.Y) - int64(b.Y)
squaredDistance(a,b) = dx*dx + dy*dy
aceite âncoras somente se squaredDistance(a,b) >= requiredDistance(a,b)^2
requiredDistance(a,b) = max(LocalMinDistance(a), LocalMinDistance(b))
```

Sem DensityRegions, o Grid de aceleração Bridson temporário tem lado MinDistance/sqrt(2); a inspeção 5×5 é suficiente. Com DensityRegions, LocalMinDistance é MinDistance fora de todas as regiões ou a distância da região que contém a âncora. A pertinência da âncora segue o retângulo semiaberto da seção 5.3: Min inclusivo e Max exclusivo. A célula de aceleração usa minLocalDistance/sqrt(2), e o alcance de busca é `ceil(maxLocalDistance/(minLocalDistance/sqrt(2)))+1` em cada eixo. Regiões não se sobrepõem. O teste de footprints é adicional e não substitui a distância Poisson entre âncoras.

Para cada ponto ativo, as tentativas Poisson mantêm a amostragem uniforme por rejeição no quadrado do anel: dois draws uniformes offsetX/offsetY por tentativa geométrica. Cada eixo é `uniform01()*4r - 2r`, porque o quadrado que circunscreve o anel externo é `[-2r, 2r]`. Como `offset = r*(4u-2)`, o teste `r² ≤ offsetX²+offsetY² < 4r²` divide-se por `r²` e torna-se independente de `r`: por isso DensityRegions alteram a distância aceita sem deslocar um único draw. `SampleUniformAnnulusByRejection` reamostra internamente até o par cair no anel; um par rejeitado não consome uma de `MaxAttempts`. Aceite apenas quando `r² ≤ offsetX²+offsetY² < 4r²`, quantize por `floor`, rejeite fora do Grid. Não se usa sin/cos ou trigonometria libm. Ordem de vizinhos, quantização, append/remoção de ativos e limites são determinísticos.

```
função PlaceRooms(request, placementRNG, geometryRNG):
    primeiraGeometria = SampleGeometry(geometryRNG)
    first = NearestValidPlacementToGridCenter(primeiraGeometria) // busca por Room.At; deriva Origin pela máscara
    accepted = [first]
    active = [first.anchor]
    accelerationGrid.insert(first.anchor)

    enquanto active não vazio e length(accepted) < MaxRooms:
        verificar cancelamento de request.Context
        i = placementRNG.uniformInt(0, length(active)-1)
        base = active[i]
        acceptedOne = falso

        repetir MaxAttempts vezes:
            r = LocalMinDistance(base)
            offsetX, offsetY = SampleUniformAnnulusByRejection(placementRNG, r)
            anchor = Cell(floor(base.X+offsetX), floor(base.Y+offsetY))
            se anchor fora do Grid: continuar
            shape, width, height = SampleGeometry(geometryRNG)
            placement = BuildPlacementFromAt(anchor, shape, width, height) // deriva Origin pelo offset da primeira Cell ocupada
            se bounds fora do Grid ou área > MaxFootprintCells: continuar
            se distância entre anchor e qualquer âncora aceita for insuficiente: continuar
            se footprint sobrepõe outro ou viola MinRoomGap: continuar
            accepted.append(placement)       // commit atômico
            active.append(anchor)
            accelerationGrid.insert(anchor)
            acceptedOne = verdadeiro
            interromper

        se não acceptedOne: active.removeAt(i)

    retornar accepted
```

`SampleGeometry` escolhe primeiro Shape por pesos inteiros positivos; depois escolhe uniformemente entre os pares (Width,Height) válidos daquela Shape, ordenados por Width e Height. A seleção por peso usa `uniformInt(1, somaDosPesos)` e intervalos cumulativos em ordem canônica de Shape; a seleção do par usa `uniformInt` sobre a lista ordenada. `BuildPlacement` aplica exatamente as máscaras canônicas da seção 5. Uma tentativa que falhe em qualquer validação não ocupa Cells e consome no máximo uma seleção de geometria. Shape e dimensões usam `RoomGeometrySeed`; índice ativo e offsets usam `PlacementSeed`, mantendo os streams isolados.

Para quaisquer Cells `a` e `b` de footprints diferentes, gap válido significa `max(abs(a.X-b.X), abs(a.Y-b.Y)) > MinRoomGap`. Assim, gap=0 impede sobreposição; gap=1 exige uma camada vazia inclusive na diagonal. Um placement é aceito somente quando, nesta ordem: é máscara válida; bounds dentro do Grid; área ≤ MaxFootprintCells; âncora respeita MinDistance/LocalMinDistance contra todas as âncoras; não há sobreposição; e todas as Cells dos footprints respeitam MinRoomGap. A aceitação é atômica.

`MaxAttempts` conta propostas completas por ponto ativo, inclusive as rejeitadas por distância, forma, bounds ou colisão. O anel não entra nessa lista de propósito: a rejeição fica dentro do sampler e não encerra a tentativa. Se contasse, seria o motivo mais frequente, pois o anel cobre `3πr²` de um quadrado de `16r²` e cerca de 41% dos pares caem fora. Cada iteração aceita um placement ou remove o ponto ativo após MaxAttempts finito; há no máximo Width×Height âncoras únicas. Portanto o algoritmo termina. `MaxRooms` é teto, não meta. Grid 1×1 normaliza para Rectangle 1×1. Nenhum footprint é truncado. [PREMISSAS P4-P7, P22, P39, P42 revisadas]

## 8. Conexão embutida: Prim MST

O grafo candidato é completo sobre Rooms. Peso é distância euclidiana entre centros geométricos das bounding boxes mesmo que as rotas sejam ortogonais. O centro é o centroide das Cells ocupadas pela bounding box, `Origin + (dimensão-1)/2`, não o centro da área `Origin + dimensão/2`. Para Rooms 1×1 o peso coincide com a distância entre âncoras, o que só é verdade quando o deslocamento se anula na dimensão 1; com dimensão maior as duas fórmulas divergem e produzem árvores diferentes. Empate segue IDs e coordenadas canônicas. [PREMISSA P8] **prim_rooms_v1** começa em RoomID 0. Pesos iguais são desempatados por FromRoomID, ToRoomID e depois Cell de destino em ordem canônica. [PREMISSA P9]

```
função BuildMST(rooms):
    se length(rooms) <= 1: retornar []
    visited = { rooms[0] }
    unvisited = rooms menos visited
    edges = []

    enquanto unvisited não vazio:
        verificar cancelamento de request.Context
        e = menor aresta de visited até unvisited
            por peso euclidiano e desempate canônico
        edges.append(e)
        visited.add(e.to)
        unvisited.remove(e.to)

    retornar edges
```

Implementação pode manter melhores chaves para tempo O(n²) e memória extra O(n), sem materializar candidatos, desde que preserve resultado exato. [PREMISSA P10]

MST garante n-1 links, alcance e menor soma de pesos selecionados entre árvores. Não minimiza Cells roteadas. Ela é sempre o backbone embutido, mas caminho único no Layout final existe somente quando ExtraEdgeCount=0.

### 8.1 Rooms temáticas e ciclos opcionais

Após prim_rooms_v1 devolver a MST, Generator atribui RoomRoles solicitados **antes** de acrescentar ciclos. Se houver solicitação Start, ela recebe RoomID 0, a primeira Room aceita. Se houver Boss, ela recebe a Room não atribuída com maior distância de caminho ponderado na MST a partir de Start; empates usam RoomID crescente. Cada solicitação Treasure recebe, em ordem, as Rooms ainda não atribuídas mais distantes de Start pela mesma regra. RequiredTags restringe a Plant posteriormente selecionada, mas não muda a escolha topológica de Room. Boss exige Start porque a distância sem origem não é definida. [PREMISSA P38]

Essa fase não merece terceiro estágio plugável: ela é projeção determinística e dirigida por Config sobre uma árvore já produzida, sem família conhecida de algoritmos alternativos que justifique interface pública. O chamador escolhe exatamente quais RoomRoles, Counts e RequiredTags deseja por solicitação. O cálculo antes dos ciclos preserva a semântica “mais distante da inicial” da árvore de exploração planejada; atalhos posteriores não transformam artificialmente o Boss em Room próxima. [PREMISSA P41]

Em seguida, se ExtraEdgeCount>0, Generator forma o conjunto de todas as arestas completas que não pertencem à MST, ordena-o por distância euclidiana crescente e pelo mesmo desempate canônico de Prim, e acrescenta as primeiras até ExtraEdgeCount. Essas arestas curtas descartadas criam atalhos e ciclos. Se ExtraEdgeCount=0, essa fase é saltada integralmente. A escolha é determinística, não aleatória. [PREMISSA P40]

## 9. Corridors e Doors

### 9.1 Seleção de Doors nas bordas

Para cada aresta Room-a-Room, Generator enumera todas as aberturas candidatas `(RoomID, At, Direction)`: At pertence ao footprint, Direction é cardinal e `At+Direction` está dentro do Grid e fora do footprint da mesma Room. A seleção procura pares de Doors que admitam rota; usa a menor quantidade total de Corridor.Cells, com desempate por Door de origem e destino em `(At.Y, At.X, Direction)` e, por fim, rota lexicográfica `(Y,X)`. A mesma abertura é reutilizada quando outro Corridor usa o mesmo triplo `(RoomID, At, Direction)`.

### 9.2 Roteamento

`Corridor.Cells` é a sequência de Cells externas percorrida da vizinhança da Door de origem até a vizinhança da Door de destino. Não inclui Cells de Room. Toda Cell precisa estar dentro do Grid e não pode pertencer ao footprint de nenhuma Room. Corridors existentes podem ser compartilhados; `CellState.CorridorIDs` registra todas as arestas em ordem crescente.

Para preservar a preferência existente, Router tenta rotas em cotovelo XThenY e YThenX entre os pontos externos das Doors, na ordem configurada. Se as duas rotas cruzarem footprint ou saírem do Grid, executa BFS determinística multi-origem/multi-destino sobre Cells livres. Vizinhos são expandidos North, East, South, West; a fila é FIFO e armazenamento row-major, sem iteração de map. A escolha de portas e o desempate global seguem a ordem canônica da seção 9.1. Somente se não houver par de Doors com rota retorna `ErrUnroutableEdge`.

`Door.Direction` aponta da Cell de Room para a primeira Cell externa do Corridor. Para Rooms adjacentes com Cells externas inexistentes, o Corridor pode ter Cells vazias apenas se as duas Doors se apontarem diretamente uma para a outra e nenhuma Cell de Room for atravessada. `Door.At` não precisa coincidir com `Room.At`.

RoomPlant.DoorDirections permanece o equivalente engine-agnóstico de markers e valida o conjunto de direções usadas. Posições artísticas de abertura ficam representadas pela coordenada de Door no Layout; o jogo pode mapear essa Cell à posição visual correspondente. [PREMISSA P11 revisada][PREMISSA P26][PREMISSA P43 revisada]

## 10. Determinismo, concorrência e compatibilidade

### 10.1 Concorrência por solicitação

Generator raiz padrão, Placer/Connector embutidos e GenerateContext são seguros para chamadas simultâneas de múltiplas goroutines. Cada invocação possui streams RNG derivados, Grid de aceleração temporário, conjuntos visitados, buffers e slices de resultado próprios. Nenhuma Seed, ponto ativo, seleção de catálogo ou Layout parcial é compartilhado. Cancelamento ou erro de uma solicitação não altera outra. [PREMISSA P21][PREMISSA P27]

Placer/Connector do chamador precisam ser concorrentes se compartilharem Generator. SDK não serializa plugin mutável. Chamadores que precisam de estado mutável fornecem Generators separados ou sincronizam no plugin; essa garantia menor pertence ao plugin.

O serviço limita trabalho em **MaxConcurrentGenerations**, cujo padrão é número de CPUs lógicas. RPCs excedentes esperam enquanto Context estiver vivo; deadline antes de admissão retorna DeadlineExceeded. Isso evita goroutines de geração ilimitadas e protege latência de cauda. [PREMISSA P28]

### 10.2 Comportamento v1 congelado

Para **poisson_disk_rooms_v1** e **prim_rooms_v1** embutidos, Config efetiva e Seed idênticas produzem Layout idêntico durante toda a major v1, em SDK e gRPC. Estão congelados:

- Constantes de derivação de streams e aritmética SplitMix64.

- Ordem de RNG: índice ativo, offsetX e offsetY no quadrado do anel, repetida por tentativa.

- Fórmula de amostragem por rejeição no quadrado, quantização floor, regras de rejeição, varredura de vizinhos, append/remoção de ativos e parada MaxRooms.

- Quando DensityRegions não está vazio, LocalMinDistance, a fórmula max entre duas distâncias locais, tamanho/alcance do Grid de aceleração e o mesmo ponto de consumo de offsetX/offsetY.

- Regra de centro, ordem Cell/Direction, ordem de IDs, ordenação de candidatos Plant e draws ponderados.

- Pesos euclidianos Prim, RoomID inicial, todos desempates, ordem de arestas, preferência L, cotovelo alternativo e derivação Door.

- Fallback DeterministicBFS: ordem North, East, South, West, fila FIFO, marcação na inserção, parent da primeira descoberta e armazenamento row-major sem iteração de map.

- Atribuição de RoomRole sobre a MST, inclusive ordem de solicitações, regra de distância e desempates; e a seleção crescente das ExtraEdgeCount arestas descartadas.

- Valores de campos Layout e ordem de sequências.

- Máscaras RoomShape, distribuição de dimensões, ordem dos offsets, RoomGeometrySeed, verificações de footprint, MinRoomGap e desempates de Door/rota.

CellSize continua neutro para topologia, mas é congelado como valor copiado na saída. Performance, estratégia de alocação, representação privada de aceleração, logs, diagnósticos e internos de fio não são congelados se todos os resultados observáveis listados permanecerem idênticos. [PREMISSA P29]

O congelamento vale somente para **poisson_disk_rooms_v1**, **prim_rooms_v1**, catálogo e roteamento embutidos. Placer ou Connector injetado pelo jogo é responsabilidade de quem o escreveu: se não for determinista, o Layout também não será. Daedalus não cria mecanismo de registro, versionamento ou detecção para plugins. gRPC usa somente os embutidos. A compatibilidade congelada aplica-se à versão refinada a partir da publicação dos novos goldens. Mudanças posteriores nas máscaras, distribuição de tamanhos, posição de portas ou desempates exigem novo ID/major, nunca atualização silenciosa de saída.

Mudança de calibração que altera saída congelada é v2. Comportamento alternativo pode surgir sob ID explícito novo, como poisson_disk_v2, enquanto poisson_disk_rooms_v1 permanece estável. Correção que altera saída é incompatível e sai apenas em v2, com goldens afetados e guia de migração. [PREMISSA P30]

### 10.3 Definição de RNG

```
PlacementSeed     = Mix64(Seed xor 0xA0B1C2D3E4F56789)
ConnectorSeed     = Mix64(Seed xor 0x1F2E3D4C5B6A7988)
RoomPlantSeed     = Mix64(Seed xor 0x9E3779B97F4A7C15)
RoomGeometrySeed  = Mix64(Seed xor 0x6C8E9CF570932BD5)
CorridorPlantSeed = Mix64(Seed xor 0xD1B54A32D192ED03)
```

Mix64 é finalização SplitMix64: adicionar 0x9E3779B97F4A7C15; z=(z xor (z>>30))*0xBF58476D1CE4E5B9; z=(z xor (z>>27))*0x94D049BB133111EB; retornar z xor (z>>31), módulo 2^64. Cada stream usa SplitMix64. uniform01 está em [0,1); uniformInt(a,b) é inclusivo e usa rejeição sem viés de módulo. Prim recebe ConnectorSeed, mas não consome draw. [PREMISSA P12]

O stream RoomGeometrySeed é independente de PlacementSeed, ConnectorSeed e streams de Plant. Config sem RoomGeometry normaliza para os defaults dinâmicos especificados na seção 5.3. ExtraEdgeCount e RoomRoleRequests não consomem sorteio: seleção de atalhos e papéis é inteiramente ordenada e determinística. DensityRegions não possui stream novo nem draw adicional: quando presente, ela só substitui o valor de distância usado entre os mesmos draws de índice ativo, offsetX e offsetY; quando vazia, o caminho Poisson uniforme legado é executado sem sequer consultar região. A geometria dinâmica altera os Layouts e goldens da especificação anterior; os goldens normativos devem ser regenerados antes do release refinado. Config efetiva e Seed iguais, incluindo RoomGeometry normalizada, produzem o mesmo Layout bit a bit em amd64 e arm64. Os goldens da especificação anterior de Rooms 1×1 não são prometidos como saída padrão nesta revisão. [PREMISSA P37][PREMISSA P39][PREMISSA P41]

O determinismo é multiplataforma obrigatoriamente: mesma Seed e Config nos sistemas suportados produzem o mesmo Layout bit a bit. O caminho de geração não chama sin, cos ou qualquer trigonometria de libm; a amostragem por rejeição elimina essa dependência. math.Sqrt é corretamente arredondada por IEEE-754 e é permitida quando necessária, mas a validação de distância do gerador compara quadrados e não precisa chamá-la. [PREMISSA P13][PREMISSA P31][PREMISSA P42]

Em Go, expressões do formato a*b+c não podem depender de uma única expressão: o compilador pode fundi-las em FMA em arm64, ppc64 e s390x, mas não em amd64. Todo produto e soma que afete candidato, distância, peso, prioridade ou desempate deve ocorrer em statements separados, com conversão explícita float64() no valor intermediário quando necessário para forçar arredondamento. Coordenadas de Cell, diferenças dx/dy após quantização, indexação e squaredDistance usam inteiros int64, cujo limite é seguro para os máximos de Grid v1; ponto flutuante fica restrito a MinDistance e ao candidato contínuo antes de floor. [PREMISSA P44]

## 11. Latência de runtime, limites e cancelamento

Um jogador espera pelo mapa sob demanda. Latência é requisito de produto, não reflexão tardia sobre throughput.

| Carga | Config efetiva | Orçamento de release no hardware de referência declarado |
| --- | --- | --- |
| Pequena | 64×64, MinDistance 6, MaxAttempts 30, MaxRooms 128 | p95 de geração SDK ≤20 ms |
| Típica | 128×128, MinDistance 6, MaxAttempts 30, MaxRooms 256 | p95 ≤50 ms; p99 ≤100 ms |
| Máxima v1 | 256×256, MinDistance 1, MaxAttempts 1024, MaxRooms 256, RoomGeometry dinâmica, footprints, gap, ciclos, papéis e DensityRegions habilitados | p95 ≤500 ms; p99 ≤1 s |

O release registra modelo de CPU, versão Go e comando benchmark com os resultados. O protocolo protobuf v1 inclui `room_geometry` opcional na Config, enumeração `room_shape` com `CIRCLE`, campos Shape/Origin/Width/Height e Cells repetidas na Room, e mantém Grid.Cells com RoomID por Cell. Os novos campos são aditivos antes do primeiro release; bindings são regenerados, nunca editados manualmente. BenchmarkGenerate usa Seeds conhecidas fixas por carga, reporta alocações e deriva distribuição de amostras repetidas isoladas por processo. A carga Máxima v1 exercita o pior caso permitido de Grid, MaxRooms, MaxAttempts, tamanhos/área de Room, MinRoomGap e features opt-in; o orçamento de latência foi dimensionado para ela. GitHub CI compartilhado registra evidência de regressão, mas não é autoridade de SLA; orçamentos absolutos executam em runner de referência fixado. [PREMISSA P32]

Antes de alocar, SDK e internal/service aplicam MaxCells=65.536, MaxRooms=256 e MaxFootprintCells=4096; excedê-los retorna ErrLimitExceeded no SDK e ResourceExhausted no gRPC, sem truncamento. SDK mantém deadline Context do chamador. Embutidos verificam Context nas fronteiras de fase e no máximo a cada 256 tentativas candidatas ou atualizações de chave Prim; expiração descarta trabalho privado e retorna cancelamento/deadline, nunca Layout parcial. [DECISÃO D1][DECISÃO D2][PREMISSA P28][PREMISSA P33]

Isso não justifica SoA: trabalho topológico domina; codificação orientada a objetos de Layout moderado continua mais clara e barata de validar. Reavaliar somente com evidência medida de serialização.

## 12. Padrões de projeto, ponto a ponto

| Ponto do sistema | Decisão | Justificativa |
| --- | --- | --- |
| Orquestração | Nenhum orquestrador GoF; um pipeline Generator. | Valores puros removem risco de mutação de árvore de cena. |
| Posicionamento de Room | Strategy: Placer pequeno definido pelo consumidor; adaptador de função/closure. | BSP, cavernas, CA e Poisson são alternativas reais. |
| Conexão de Room | Strategy: Connector pequeno definido pelo consumidor; adaptador de função/closure. | Prim e seleção Delaunay diferem, mas devolvem arestas. |
| Ciclos e rotas alternativas | Aumento determinístico pós-backbone, dirigido por ExtraEdgeCount. | Atalhos são política de Config sobre arestas descartadas, não nova família de Connector. |
| Rooms temáticas | Atribuição determinística dirigida por RoomRoleRequests, sem interface nova. | É projeção sobre a MST e Config, não variabilidade algorítmica comprovada. |
| Densidade e biomas | Extensão de dados do Placer embutido via DensityRegions. | A regra espacial muda parâmetros de Poisson; Placer continua a fronteira de extensão. |
| Roteamento de Corridor | Sem Strategy pública; Doors de borda, cotovelo L e BFS determinística. | Resolve footprints variáveis sem expor uma interface desnecessária. |
| Resolução de Door | Abertura por (RoomID, At, Direction), get-or-create. | Door identifica ponto real da borda e pode ser compartilhada por rotas. |
| Catálogo de Plant | Consulta de dados, não Factory ou Resolver. | Daedalus não cria objeto de engine. |
| Resposta gRPC | Adapter em internal/service. | Mantém fio fora da raiz pura. |
| HTTP de debug | Servidor opcional internal/httpdebug, mesma geração e admissão. | Ferramenta local, desligada por padrão, sem duplicar algoritmo nem estado. |
| Ciclo de vida | Composition root fx gerencia gRPC e HTTP opcional. | Listeners iniciam/encerram juntos; falha em bind impede serviço parcial. |
| Logging | zap injetado em pacotes internal; nenhum na raiz. | Logging não pode contaminar pureza ou stdout. |

### 12.1 Considerados e rejeitados

| Padrão / escolha | Motivo da rejeição |
| --- | --- |
| Autoridade mutável estilo Godot | Valores puros por solicitação não têm risco de árvore de cena; acrescenta estado compartilhado. |
| Strategy para toda fase | Somente posicionamento e conexão têm algoritmos alternativos conhecidos. |
| Terceira Strategy para papéis temáticos | RoomRoleRequests é regra declarativa sobre MST; interface por simetria não paga a própria complexidade. |
| Abstract Factory para Plant | PlantID é metadado; biblioteca não cria assets de engine. |
| Visitor sobre Layout | Nenhuma família de operações requer double dispatch; iteração é mais clara. |
| Repository/service layer na raiz | Raiz não possui persistência nem I/O de rede. |
| Event bus ou observer | Fases síncronas e resultado único não precisam eventos. |
| Singleton RNG ou Generator | Quebra Seeds independentes por solicitação e concorrência. |
| Pool mutável compartilhado na raiz | Adiciona risco de aliasing antes de profiling provar necessidade. |
| SoA protobuf | Adequado aos 50k batches do Geppetto, não a Layout moderado único. |
| Plugins gRPC em runtime | Executa código desconhecido e quebra reprodutibilidade de deploy. |
| JSON Schema | Não selecionado; validação SDK/proto basta em v1. |
| Builder público de Layout | Expõe estados intermediários inválidos sem caso de uso. |

Função idiomática, closure, interface pequena, struct simples ou iteração direta vencem salvo variabilidade ou dor de ciclo de vida concreta. [PREMISSA P16][PREMISSA P23][PREMISSA P24][PREMISSA P26]

## 13. Testes, gates de qualidade e automação

Testes usam testify require para pré-condições/setup fatal, assert para observações independentes e InDelta para expectativas explicitamente float. Igualdade de Layout congelado é exata; InDelta nunca enfraquece goldens de Seed.

Testes golden usam casos nomeados de Config/Seed e fixtures Layout normalizadas legíveis. Falha reporta primeiro caminho de campo divergente, escalar esperado/real, IDs ausentes/extras e Cells alteradas; nunca comparação somente por hash. Casos cobrem formas Rectangle/L/T/Cross/Circle, footprints no limite do Grid, gap, âncoras, Doors de borda, Corridor compartilhado, catálogo, desempate, busca serpenteante e amostragem por rejeição. O mesmo arquivo esperado é executado em mais de uma arquitetura e toda divergência é falha de compatibilidade. [PREMISSA P34][PREMISSA P45]

“Prove que morde” é obrigatório para teste de segurança. Antes de creditar proteção, mantenedores demonstram violação controlada falhando e registram comando/saída na evidência da mudança; então restauram proteção e demonstram passagem. Exemplos: import não-stdlib temporário na raiz, escrita extra em stdout, Cell golden alterada e contaminação RNG compartilhada concorrente. A violação nunca vai para release. [PREMISSA P35]

| Alvo | Comportamento exigido |
| --- | --- |
| make generate | Resolver override, depois GOBIN (ou GOPATH/bin se GOBIN vazio ), depois PATH; executar geração buf. Ferramenta ausente/incompatível falha alto com instrução de instalação. |
| make lint | Executar buf lint e golangci-lint em versão mínima. |
| make test | Executar go test em todos os pacotes; CI também race detector. |
| make bench | Executar benchmarks de geração na raiz com dados de alocação. |
| make build / build-all | Construir comando com versão; gerar artefatos Linux amd64, Darwin arm64 e Windows amd64. |
| CI em push e pull request | Verificar bindings limpos, buf lint/breaking, race, golangci-lint, build, pureza, goldens congelados e registro de benchmark. Executar a suite golden contra o mesmo arquivo esperado em runners nativos amd64 e arm64; emulação não prova determinismo de arquitetura. |
| Release em tag exata vX.Y.Z | Reexecutar race, construir artefatos, criar checksums SHA-256 e publicar release. Tags v-star inválidas são ignoradas. |

Mecânica de Makefile e workflows segue forma operacional do Geppetto, com targets Daedalus. [PREMISSA P36]

## 14. Tuning consolidado

| Parâmetro | Default | Ao aumentar | Ao diminuir |
| --- | --- | --- | --- |
| Width | obrigatório | mais espaço/memória horizontal | espaço restrito, possivelmente uma Room |
| Height | obrigatório | mais espaço/memória vertical | espaço restrito, possivelmente uma Room |
| CellSize | 1.0 | somente escala do chamador | igual; topologia não muda |
| MinDistance | 6.0 Cells | âncoras mais afastadas, menos Rooms | âncoras mais densas, mais trabalho |
| RoomGeometry.MaxWidth/Height | 9 Cells | Rooms maiores e menos placements válidos | Rooms menores e mais placements |
| RoomGeometry.MaxFootprintCells | 81 Cells | máscaras maiores e mais custo de validação | restringe área e formas possíveis |
| RoomGeometry.MinRoomGap | 1 Cell | mais separação entre footprints, menos Rooms | mais compactação; zero permite contato sem sobreposição |
| RoomGeometry.Shapes.Weight | 4/2/2/1/2 | aumenta frequência relativa da forma correspondente | diminui frequência; zero inválido |
| DensityRegions.MinDistance | sem regiões | cria região mais esparsa e aumenta alcance de busca | cria região mais apertada; a fronteira ainda respeita o maior valor local |
| MaxAttempts | 30 | mais exploração local e CPU | preenchimento ralo e menos CPU |
| MaxRooms | 256 | mais topologia e pior latência | limita trabalho antes |
| ExtraEdgeCount | 0 | mais atalhos/ciclos e mais Corridors a rotear | menos rotas alternativas; zero preserva árvore |
| RoomRoleRequests.Treasure.Count | 0 | mais Rooms Treasure, reduz candidatos disponíveis a outros papéis | menos Rooms temáticas; zero desliga Treasure |
| CorridorOrder | XThenY | YThenX muda cotovelo preferido | n/a |
| Plant Weight | 1 | aumenta frequência relativa | diminui; zero inválido |
| MaxCells de serviço | 65.536 | pedidos maiores, risco de latência/alocação | rejeição antecipada |
| MaxConcurrentGenerations | CPUs lógicas | throughput até contenção prejudicar cauda | mais isolamento e fila |

## 15. Critérios de aceitação

| ID | Dado | Resultado observável |
| --- | --- | --- |
| AC-01 | Config bem-sucedida | Grid possui Width×Height estados; IDs/coordenadas existem; invariantes da seção 5 valem. |
| AC-02 | Layout com ao menos duas Rooms | BFS alcança toda Room; quantidade Corridor é Rooms menos um quando ExtraEdgeCount=0. |
| AC-03 | Cada par de Rooms | Distância entre âncoras ≥MinDistance/LocalMinDistance; footprints não se sobrepõem e respeitam MinRoomGap. |
| AC-04 | Mesma Config/Seed embutida | Todos campos, footprints e ordens são iguais, não apenas o conjunto de âncoras. |
| AC-05 | Solicitações concorrentes intercaladas | Cada uma igual ao baseline isolado; race detector limpo; cancelamento não afeta outra. |
| AC-06 | Cada Corridor | Ortogonal, dentro do Grid, externo a todos os footprints; Doors de borda correspondem às extremidades. |
| AC-07 | Grid 1×1, MinDistance 6 | Uma Room Rectangle 1×1 em (0,0), zero Corridor/Door. |
| AC-08 | Grid pequeno ou MinDistance acima da diagonal | Primeira Room válida perto do centro; sem loop nem footprint truncado. |
| AC-09 | Execução Poisson | Tentativas ≤MaxAttempts por ponto ativo; Rooms aceitas ≤MaxRooms; termina. |
| AC-09a | DensityRegions não vazias | Para cada par de âncoras, distância ≥max das duas LocalMinDistance; regiões terminam sob limites. |
| AC-09b | DensityRegions vazias | Mesma Config/Seed e RoomGeometry produzem golden igual entre âncoras; sem draws adicionais de regiões. |
| AC-10 | Catálogo compatível | Cada RoomPlant suporta todas as direções de Door; IDs/tags correspondem ao catálogo. |
| AC-11 | Catálogo incompatível | ErrNoCompatiblePlant e nenhum Layout parcial. |
| AC-12 | Nenhum par de Doors roteável | ErrUnroutableEdge determinístico e nenhum Layout parcial. |
| AC-12a | ExtraEdgeCount>0 | Layout conectado; atalhos adicionais são as primeiras arestas curtas ordenadas disponíveis. |
| AC-12b | ExtraEdgeCount=0 | Grafo Room/Corridor é árvore; Config/Seed produzem golden refinado exatamente. |
| AC-12c | RoomRoleRequests com Start, Boss e Treasure | Start é RoomID 0; Boss/Treasure seguem distância MST e desempate; Plant de papel contém RequiredTags. |
| AC-12d | RoomRoleRequests vazias | Todas Rooms têm Role ausente; não há draws nos streams de posicionamento/geometria por esta opção. |
| AC-12e | ExtraEdgeCount=0, papéis vazios, sem DensityRegions | Config/Seed com RoomGeometry normalizada reproduzem o golden refinado, sem draws extras dessas opções. |
| AC-13 | Suite golden congelada | Seeds nomeadas correspondem exatamente; diagnóstico mostra caminho, não somente hash. |
| AC-14 | Limite de serviço excedido | ResourceExhausted ou InvalidArgument antes de alocar/gerar Layout. |
| AC-15 | Benchmark de release | Cargas Pequena/Típica atendem p95/p99 da seção 11 no hardware de referência. |
| AC-16 | Início do processo | Exatamente uma linha handshake válida em stdout antes da conexão; logs ausentes. |
| AC-17 | Teste de pureza | Raiz importa somente stdlib e violação controlada registrada como falha. |
| AC-18 | Golden multiplataforma | Mesma suite e fixture passam em runners amd64 e arm64. |
| AC-19 | Config sem RoomGeometry | Normaliza para perfil dinâmico padrão; em Grid suficiente, Layout contém Rooms multi-Cell e footprints explícitos. |
| AC-20 | RoomShape e dimensões válidas | Room.Cells corresponde exatamente à máscara Rectangle/L/T/Cross/Circle e bounds declarados. |
| AC-21 | Qualquer Room | Footprint não vazio, 4-conexo, sem duplicatas, dentro do Grid; At pertence às Cells; área respeita limite. |
| AC-20a | RoomShape=Circle | Width=Height ímpar ≥5; exatamente as Cells dentro do raio inteiro; máscara 4-conexa e determinística. |
| AC-22 | Par de Rooms aceitas | Footprints distintos respeitam MinRoomGap; âncoras respeitam MinDistance e DensityRegions. |
| AC-23 | Corridor entre Rooms multi-Cell | Doors são Cells de borda válidas; rota externa, ortogonal e conectada às duas Rooms. |
| AC-24 | Duas rotas compartilham abertura | Door única por (RoomID, At, Direction); CorridorIDs contém todas as arestas em ordem crescente. |
| AC-25 | Mesmo Config efetivo/Seed em amd64 e arm64 | Shape, dimensões, footprints, Doors, Corridors e Layout idênticos. |
| AC-26 | RoomGeometry inválida | Falha de validação antes do consumo de RNG ou materialização parcial. |
| AC-27 | Nenhuma posição cabe no Grid | Geração termina sem truncar footprint, sem pânico e sem Layout inválido; 1×1 admite Rectangle 1×1. |
| AC-28 | Rotas L bloqueadas por footprints | BFS determinística encontra rota exterior ou retorna ErrUnroutableEdge. |
| AC-29 | Placer SDK inválido | Generator rejeita máscara duplicada, desconexa, fora do Grid ou incompatível com Shape/Config. |
| AC-30 | HTTP de debug desabilitado | Nenhuma porta HTTP abre; gRPC e handshake permanecem iguais ao baseline. |
| AC-31 | `--http-debug-enabled=true` com addr padrão | Listener responde apenas em 127.0.0.1:8090; `GET /healthz` e `GET /debug/` funcionam; sem CDN/recursos externos. |
| AC-32 | POST ProtoJSON válida | Layout corresponde a Generate do SDK para a mesma Config/Seed, incluindo RoomGeometry, footprints, Doors e Cells. |
| AC-33 | UI gera Layout válido | Pedido pode ser editado/enviado; Grid, Rooms, Shapes, Doors e Corridors são visualizados; copiar request/response preserva ProtoJSON. |
| AC-34 | JSON, Content-Type ou Config inválidos | Resposta 400 estruturada em português, sem panic, stack trace ou Layout parcial. |
| AC-35 | Corpo excede 1 MiB ou limite de Grid/Rooms | Resposta 413; rejeição ocorre antes de alocação proporcional/geração. |
| AC-36 | Cliente cancela POST ou deadline expira | Geração observa cancelamento; não publica nem persiste Layout parcial; outra solicitação não é afetada. |
| AC-37 | HTTP e gRPC simultâneos | Ambos compartilham MaxConcurrentGenerations; race detector limpo e cada resposta determinística. |
| AC-38 | Bind HTTP em endereço não-loopback/porta em uso | Configuração inválida ou falha de bind aborta startup; nenhum serviço parcialmente iniciado é anunciado. |
| AC-39 | Shutdown com HTTP habilitado | Health passa a 503, admissão para, trabalho é cancelado/encerrado e listener HTTP fecha graciosamente. |
| AC-40 | Logs e headers da UI | Corpo não aparece em logs; CORS/Host/Origin são restritos; UI é same-origin e sem dependências externas. |

## 16. Escopo do primeiro release: v0.1.0

A v0.1.0 entrega integralmente esta especificação: biblioteca Go pura com Layout/Config e footprints dinâmicos de Rooms (tamanho e forma ), Placer e Connector plugáveis para uso SDK; Poisson Disk com colisão de footprints, Prim, ciclos e rotas por bordas, Rooms temáticas, DensityRegions e fallback DeterministicBFS; determinismo por Seed, por solicitação e multiplataforma; subprocesso gRPC; e servidor HTTP de desenvolvimento opcional por flag, com UI de request/visualização, ProtoJSON, acesso restrito a loopback e desligado por padrão. Não há faseamento implícito nem subconjunto planejado desse escopo.

### 16.1 Decisões de produto fechadas

| ID | Decisão |
| --- | --- |
| D1 | MaxCells=65.536 e Grid máximo de 256×256 são limites v1 de SDK e serviço; exceder falha claramente, sem truncamento. |
| D2 | MaxRooms=256 é limite v1 de SDK e serviço; o orçamento Máxima v1 da seção 11 é dimensionado para esse pior caso permitido. |
| D3 | Config de jogo chega por código Go no SDK ou mensagem protobuf gRPC. Não existem arquivos JSON/YAML de configuração, presets ou catálogos em disco. HTTP de debug aceita somente ProtoJSON transitório em memória; não cria formato persistente. |
| D4 | Geometria dinâmica de Room é comportamento padrão: na ausência de RoomGeometry aplica-se o perfil da seção 5.3, com dimensões variáveis e pesos Rectangle/L/T/Cross/Circle; Grid insuficiente reduz as formas sem truncar footprints. Grid 1×1 usa Rectangle 1×1. |
| D5 | MaxFootprintCells=4096 é o teto duro por Room; o orçamento de runtime e o serviço validam essa faixa antes de alocar ou gerar. |
| D6 | HTTP de debug é opt-in: `--http-debug-enabled=false`; quando habilitado, usa `--http-debug-addr=127.0.0.1:8090` e aceita somente loopback. |
| D7 | HTTP reutiliza Generate/validação/admissão existentes, recebe/retorna ProtoJSON e expõe `/debug/` e `/api/v1/generate`; não persiste solicitações nem Layouts. |
| D8 | Falha ao iniciar qualquer listener solicitado aborta startup completo; shutdown coordena gRPC, HTTP, admissão e cancelamento. |

## 17. Lacunas e premissas

A transcrição não define dados públicos, amostragem discreta, arquitetura de pacote, determinismo, concorrência, latência, erros nem transporte. Ela não é autoridade onde necessidades de biblioteca Go diferem.

| ID | Premissa e justificativa |
| --- | --- |
| P1 | Defaults dos campos não relacionados a limites tornam validação explícita; MaxCells e MaxRooms são decisões D1/D2. |
| P2 | Erros explícitos de catálogo/rota evitam Layout parcial e Corridor cruzando Room. |
| P3 | Catálogo usa IDs, tags, pesos e Directions, não Resources de engine. |
| P4 | Primeira Room fica o mais perto possível do centro geométrico; desempate Y/X; âncora é a primeira Cell ocupada da máscara. |
| P5 | Grid de aceleração Poisson opera sobre âncoras e é separado do Grid público/footprints. |
| P6 | Âncoras Poisson quantizam por floor; placement completo é validado antes do commit. |
| P7 | Anel Poisson uniforme por área, limite superior exclusivo; dimensões usam stream isolado. |
| P8 | Prim usa distância entre centros geométricos das bounding boxes das Rooms. |
| P9 | Início/desempates Prim canônicos. |
| P10 | Prim O(n² ) por chaves preserva semântica. |
| P11 | Rotas L/BFS evitam footprints; Corridors podem compartilhar Cells; Doors ficam nas bordas. |
| P12 | Streams/constantes SplitMix64 fixos. |
| P13 | Binary64, ausência de trigonometria libm e operações sem FMA implícita garantem portabilidade. |
| P14 | Validar antes de RNG; descartar trabalho após erro pós-início. |
| P15 | Português é o idioma da documentação pública desde o início. |
| P16 | Publicação de valor final substitui autoridade de mutação Godot. |
| P17 | Fronteira AST de pureza da raiz segue arquitetura irmã. |
| P18 | Ciclo fx e handshake JSON stdout definem contrato de processo. |
| P19 | AoS é adequado a respostas gRPC de Layout moderado. |
| P20 | Convenções de nomes evitam migração futura de vocabulário. |
| P21 | Dados/resultado locais da solicitação dão segurança concorrente. |
| P22 | Controle de admissão e cancelamento limita latência runtime; os valores MaxCells/MaxRooms são decisões D1/D2. |
| P23 | Consulta Plant é dado, não construção. |
| P24 | Placer/Connector são interfaces Strategy v1; Placer produz footprints completos. |
| P25 | Plugins são injeção SDK de responsabilidade do jogo; gRPC contém somente embutidos sem registry/versionamento/detecção. |
| P26 | Roteamento embutido seleciona Doors de borda e combina cotovelos L com BFS, sem interface Router pública. |
| P27 | Generator compartilhado só é seguro com plugins concorrentes. |
| P28 | Controle de admissão limita trabalho e filas. |
| P29 | Saída observável embutida congela por toda v1. |
| P30 | Calibração/correção que muda saída exige v2 ou ID opt-in. |
| P31 | Amostragem por rejeição elimina sin/cos de libm; math.Sqrt é permitida por correção IEEE-754. |
| P32 | Orçamentos interativos exigem cargas fixas/hardware referência. |
| P33 | Pedidos grandes/expirados falham sem resultado parcial. |
| P34 | Goldens diagnosticam estrutura, não hashes. |
| P35 | Testes de segurança precisam de falha controlada testemunhada. |
| P36 | Makefile/CI/release herdam convenções operacionais do projeto irmão. |
| P37 | RoomGeometry usa perfil dinâmico padrão e stream independente; outras opções não deslocam draws entre si. |
| P38 | RoomRoleRequests escolhem Start/Boss/Treasure pela MST antes de ciclos e restringem Plant por RequiredTags. |
| P39 | DensityRegions não sobrepostas usam distância por par igual ao máximo local e Grid de aceleração baseado no mínimo local. |
| P40 | ExtraEdgeCount seleciona deterministamente arestas curtas descartadas após o backbone; conectividade permanece obrigatória. |
| P41 | Papéis temáticos são atribuição de Config, não terceiro estágio plugável. |
| P42 | Poisson amostra anel por rejeição em quadrado, com dois draws e sem trigonometria. |
| P43 | BFS usa ordem cardinal, FIFO e armazenamento determinístico, nunca iteração de map. |
| P44 | Statements separados e float64 intermediário impedem FMA dependente de arquitetura; cálculos discretos usam int64. |
| P45 | CI multiarquitetura verifica o mesmo golden em amd64 e arm64. |
| P46 | HTTP de debug é loopback, opt-in, sem CORS/auth externa, sem persistência e sem dependências no pacote raiz. |
| P47 | Handler HTTP reutiliza Generate, Context, validação e admissão; não cria algoritmo separado. |
| P48 | HTTP e gRPC compartilham o ciclo de vida e a cota de concorrência do subprocesso. |
| P49 | DensityRegion é semiaberto: Min inclusivo e Max exclusivo, X em [Min.X, Max.X) e Y em [Min.Y, Max.Y); Max pode igualar a dimensão do Grid. |
| P50 | Cada eixo do quadrado do anel é uniform01()*4r-2r em [-2r, 2r]; o teste do anel independe de r, então DensityRegions não deslocam draws. |
| P51 | Par fora do anel é reamostrado dentro do sampler e não consome MaxAttempts. |
| P52 | Centro da bounding box no peso de Prim é o centroide das Cells, Origin+(dimensão-1)/2, não Origin+dimensão/2. |
| P53 | Saída inaceitável de Placer ou Connector injetado é uma única categoria de erro; a mensagem embrulhada distingue qual algoritmo e o motivo. |

## Apêndice A — exemplos mínimos

Width=1, Height=1, Seed=0, MinDistance=6, MaxRooms=256 normaliza a geometria para Rectangle 1×1, produz RoomID 0 ocupando Cell(0,0), nenhum Corridor nem Door. MST de um vértice é vazia.

Exemplo de roteamento: Room A é Rectangle 2×2 em Origin=(1,1), com Door em At=(2,1), Direction=East; Room B é Rectangle 2×2 em Origin=(5,3), com Door em At=(5,3), Direction=North. Com preferência XThenY, `Corridor.Cells` pode ser [(3,1), (4,1), (5,1), (5,2)], conectando as Cells externas às Doors sem incluir Cells dos footprints. Isso ilustra somente o roteamento, não fixa Seed nem saída do Poisson.

## Apêndice B — validação e erros

Antes dos streams, SDK valida faixas/finitude numérica, produto Grid, dimensões/área/Shape de RoomGeometry, regiões de densidade, solicitações de papel e formato de catálogo. O adaptador HTTP valida também ProtoJSON, Content-Type e limite de corpo antes de chamar a mesma Generate. Erro de validação não é Layout degenerado. Erro de rota, catálogo ou cancelamento após início descarta trabalho privado. [PREMISSA P14]

Erros nomeados definem categorias, não representação Go. Serviço mapeia Config inválida a InvalidArgument, limite de admissão a ResourceExhausted, expiração a DeadlineExceeded e cancelamento do chamador a Canceled. SDK possui erros correspondentes sem dependência grpc.

ErrInvalidPlugin classifica a saída que um Placer ou Connector injetado devolve quando o Generator não pode aceitá-la: máscara incompatível com a Shape declarada, offsets duplicados ou desconexos, placement fora do Grid, colisão, área ou distância inválida, contagem de arestas fora do intervalo, aresta própria, aresta duplicada, RoomID desconhecido ou grafo desconexo. As duas falhas são a mesma categoria para o chamador; a mensagem embrulhada indica qual algoritmo e o motivo. O serviço mapeia ErrInvalidPlugin a FailedPrecondition, junto de ErrNoCompatiblePlant e ErrUnroutableEdge.
