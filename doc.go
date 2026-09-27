// Package daedalus gera dungeons 2D discretas e determinísticas sob demanda.
//
// Dada uma Config e uma Seed, o pacote produz um Layout completo e imutável:
// Grid lógico, Rooms, Corridors e Doors direcionais. O pacote raiz contém
// somente o modelo de dados e a geração determinística; ele não renderiza,
// não conhece engines de jogo, não calcula posições em pixel e importa
// apenas a biblioteca padrão Go — invariante verificada por teste de pureza
// de imports via AST.
//
// O vocabulário público (Layout, Room, Corridor, Door, Grid, Cell, Config,
// Seed, Placer, Connector) é normativo: renomeá-lo é mudança incompatível
// de API. A documentação deste pacote está em português, enquanto os
// identificadores de domínio permanecem em inglês, conforme a especificação
// de engenharia em docs/spec.md.
package daedalus
