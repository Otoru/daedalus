package platform

import "github.com/Otoru/daedalus/core"

// Synthesis owns four random streams. They are derived with salts of this
// package, never with the dungeon generator's: those seven salts are private
// to the root on purpose, because they are the vocabulary of another
// generator — Room Plant, Corridor width, terrain — and borrowing one would
// tie a platform map's identity to a dungeon's.
//
// The consumption contract is the root's, unchanged: UniformInt(lo, hi) is
// inclusive, lo == hi consumes nothing, and rejection sampling may call Next
// several times for one logical draw.
//
// # Order of consumption
//
// One request walks the streams in exactly this order, and the order is the
// thing a golden would freeze:
//
//  1. The attempt stream, built once per request from the request Seed.
//     Exactly one Next per attempt, drawn BEFORE the attempt starts. Attempt
//     k therefore depends on every attempt before it having been drawn, which
//     is what makes a retry a re-seed rather than a different question.
//  2. Per attempt, the attempt's seed derives the macro seed, the rhythm
//     stream and the placement stream, each with its own salt. The three are
//     independent: a change in how many numbers placement draws cannot move
//     the rooms.
//  3. The rhythm stream yields exactly one seed per room, in ascending RoomID
//     order, drawn before that room's rhythm is generated. The rhythm front
//     then consumes its own three streams from that seed.
//  4. The placement stream yields exactly two numbers per room, in ascending
//     RoomID order: the run's leading horizontal pad and the air reserved
//     under the run's lowest platform. Both are drawn before anything is
//     stamped, so a beat that does not fit cannot shift the next room.
const (
	// synthAttemptSalt separates the stream that draws one seed per attempt
	// from every other stream. It is the only stream that survives a retry.
	synthAttemptSalt uint64 = 0x3B5C1E97A2D4608F
	// synthMacroSalt separates the seed handed to the macro front from the
	// attempt seed, so that room placement and rhythm cannot shadow one
	// another.
	synthMacroSalt uint64 = 0xE47A0C3915B6D82F
	// synthRhythmSalt separates the stream that seeds each room's rhythm.
	synthRhythmSalt uint64 = 0x0D9F62B4E7813CA5
	// synthPlacementSalt separates the stream that decides where a room's run
	// of beats is stamped inside the room.
	synthPlacementSalt uint64 = 0xB82E5D017F3A94C6
)

// synthStreams is one attempt's randomness. The macro front takes a Seed
// rather than a stream, so that stream is collapsed to the single number it
// would have produced.
type synthStreams struct {
	macro     Seed
	rhythm    core.SplitMix64
	placement core.SplitMix64
}

// newSynthStreams derives one attempt's streams from that attempt's seed.
func newSynthStreams(attempt Seed) synthStreams {
	return synthStreams{
		macro:     deriveSeed(attempt, synthMacroSalt),
		rhythm:    core.NewSplitMix64(attempt, synthRhythmSalt),
		placement: core.NewSplitMix64(attempt, synthPlacementSalt),
	}
}

// deriveSeed turns a seed and a salt into another seed by taking the first
// value of the stream they name. It exists for the fronts that accept a Seed
// instead of a stream: handing them the request seed directly would make two
// fronts walk the same numbers.
func deriveSeed(seed Seed, salt uint64) Seed {
	stream := core.NewSplitMix64(seed, salt)
	return Seed(stream.Next())
}
