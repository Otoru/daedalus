# `platform/testdata`

Empty on purpose. No golden and no portrait of a platform map exists yet:
those belong to front I, which also owns running the old suites without
re-recording a fixture (`CONTRIBUTING.md:63`). Front H wrote the thing a
golden would be taken of, not the golden.

What a golden here would freeze is **the order in which synthesis consumes
randomness**, because that order — not the arithmetic, which is `core`'s — is
what decides which map a seed names. Changing any line of the list below
changes every map, with no compile error and no test failure outside a
golden. `PlatformLayout.Canonical()` is the encoding to take the golden of;
`TestSameConfigAndSeedProduceTheSameLayout` already compares two runs with it,
and `TestCanonicalCoversWhatItClaimsTo` is the guard that the encoding is not
hollow.

The order, as `platform/rng.go` documents it and `platform/generator.go`
implements it:

1. **The attempt stream** — `core.NewSplitMix64(Config.Seed, synthAttemptSalt)`,
   built once per request. Exactly one `Next` per attempt, drawn **before**
   the attempt starts. Attempt *k* therefore depends on attempts 0..*k*-1
   having been drawn: that is what makes a retry a re-seed of the same
   question rather than a different question.
   `TestAttemptSeedsAreTheAttemptStreamInOrder` freezes this one already.
2. **Per attempt**, the attempt's seed derives three independent things, each
   with its own salt: the macro seed, the rhythm stream, the placement
   stream. A change in how many numbers placement draws cannot move the
   rooms.
3. **The rhythm stream** — one `Next` per room, in ascending `RoomID` order,
   drawn before that room's rhythm. Front D's three streams are derived from
   the value drawn here, and their own order is front D's contract.
4. **The placement stream** — exactly two numbers per room, in ascending
   `RoomID` order: the run's leading horizontal pad, then the air reserved
   under the run's lowest platform. Both are drawn before anything is
   stamped, so a beat that turns out not to fit cannot shift a later room.

The consumption contract inside a draw is the root's, unchanged:
`UniformInt(lo, hi)` is inclusive, `lo == hi` consumes nothing, and rejection
sampling may call `Next` several times for one logical draw.
