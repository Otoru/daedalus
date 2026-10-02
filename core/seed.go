package core

// Seed is the source of all deterministic random streams for a generation
// request. It is an unsigned 64-bit integer and all values are accepted: zero
// is valid and does not mean "random". The same Seed combined with the same
// effective Config produces the same Layout throughout major version v1 on
// every supported platform.
type Seed uint64
