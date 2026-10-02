// Package core holds the perspective-agnostic primitives shared by every
// Daedalus generator: the deterministic random stream and the coordinate
// vocabulary that a top-down dungeon and a side-view platform map both need.
//
// Nothing here knows what a Room, a Corridor, or a Door is, and nothing here
// knows how many streams a generator keeps or what it calls them. core owns
// the arithmetic; the catalogue of streams and the salts that separate them
// belong to the generator that has the vocabulary. A caller derives one
// independent stream with NewSplitMix64(seed, salt) and keeps its own salts.
//
// What is frozen for the whole of major version v1 is the arithmetic itself:
// the SplitMix64 finalization constants and shifts, the half-open interval of
// Uniform01, and the inclusive bounds and rejection rule of UniformInt. A
// stream's raw state is unexported. A caller that must rewind a stream — after
// a probe that failed, for instance — copies the whole SplitMix64 value, which
// is the only state there is; there is no way to reseed one in place by
// accident.
//
// This code was moved verbatim out of the root package. The arithmetic, the
// constants, and the order of operations are the frozen contract; splitting
// the per-stream derivation into an explicit salt argument moves no draw and
// changes no value.
//
// Like the root package, core imports only the Go standard library, and
// TestRootPackageImportsOnlyStandardLibrary enforces it. core is the bottom of
// the dependency order: core imports nothing in the module, the root package
// may import core, and the utils subpackages may import both.
package core
