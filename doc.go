// Package daedalus gera dungeons 2D discretas e determinísticas sob demanda.
//
// Dada uma Config e uma Seed, o pacote produz um Layout completo e imutável:
// Grid lógico, Rooms com footprint discreto, Corridors ortogonais e Doors
// direcionais nas bordas das Rooms. O pacote raiz contém somente o modelo de
// dados e a geração determinística; ele não renderiza, não conhece engines de
// jogo, não calcula posições em pixel e importa apenas a biblioteca padrão
// Go — invariante verificada por teste de pureza de imports via AST.
//
// Rooms têm tamanho e forma variáveis. RoomShape define as cinco máscaras
// canônicas — Rectangle, L, T, Cross e Circle — e RoomGeometry controla
// faixas de dimensão, área máxima, espaçamento entre footprints e o peso
// relativo de cada forma. Config sem RoomGeometry não produz Rooms de uma
// única Cell: ela normaliza para um perfil dinâmico, reduzido às formas que
// cabem no Grid solicitado.
//
// O posicionamento e a conexão são pontos de extensão. Placer e Connector
// são as duas únicas interfaces Strategy de v1 e podem ser fornecidas pelo
// jogo; os algoritmos embutidos são poisson_disk_rooms_v1 e prim_rooms_v1,
// cuja saída observável é congelada por toda a major v1 para a mesma Config
// efetiva e Seed, em qualquer plataforma suportada.
//
// O vocabulário público (Layout, Room, Corridor, Door, Grid, Cell, Config,
// Seed, Placer, Connector) é normativo: renomeá-lo é mudança incompatível
// de API. A documentação deste pacote está em português, enquanto os
// identificadores de domínio permanecem em inglês, conforme a especificação
// de engenharia em docs/spec.md.
package daedalus
