package daedalus

import "github.com/Otoru/daedalus/core"

const (
	// placementStreamSalt separates the placement stream from the other streams.
	placementStreamSalt uint64 = 0xA0B1C2D3E4F56789
	// connectorStreamSalt separates the connection stream from the other streams.
	connectorStreamSalt uint64 = 0x1F2E3D4C5B6A7988
	// roomPlantStreamSalt separates the Room Plant stream from the other streams.
	// Its value is the same odd increment Mix64 adds, 0x9E3779B97F4A7C15. The
	// repetition is intentional and frozen: replacing it would change every Layout.
	roomPlantStreamSalt uint64 = 0x9E3779B97F4A7C15
	// roomGeometryStreamSalt separates the Room geometry stream from the other streams.
	roomGeometryStreamSalt uint64 = 0x6C8E9CF570932BD5
	// corridorPlantStreamSalt separates the Corridor Plant stream from the other streams.
	corridorPlantStreamSalt uint64 = 0xD1B54A32D192ED03
	// corridorWidthStreamSalt separates the Corridor width stream from the other streams.
	// The five salts above are frozen; adding this sixth salt must not change them.
	corridorWidthStreamSalt uint64 = 0xC3D4E5F60718293A
	// terrainStreamSalt is the seventh independent stream. Its value is frozen;
	// terrain placement must never consume one of the six existing streams.
	terrainStreamSalt uint64 = 0x7A6B5C4D3E2F1A09
)

// rngStreams contains a request's seven independent streams. The catalogue is
// dungeon vocabulary — Room Plant, Corridor width, terrain — so it lives with
// the generator that owns that vocabulary, not in core. core supplies the
// arithmetic; the salts and the names of the streams are this package's.
type rngStreams struct {
	placement     core.SplitMix64
	connector     core.SplitMix64
	roomPlant     core.SplitMix64
	roomGeometry  core.SplitMix64
	corridorPlant core.SplitMix64
	corridorWidth core.SplitMix64
	terrain       core.SplitMix64
}

// newRNGStreams derives every stream directly from the request Seed.
func newRNGStreams(seed Seed) rngStreams {
	return rngStreams{
		placement:     core.NewSplitMix64(seed, placementStreamSalt),
		connector:     core.NewSplitMix64(seed, connectorStreamSalt),
		roomPlant:     core.NewSplitMix64(seed, roomPlantStreamSalt),
		roomGeometry:  core.NewSplitMix64(seed, roomGeometryStreamSalt),
		corridorPlant: core.NewSplitMix64(seed, corridorPlantStreamSalt),
		corridorWidth: core.NewSplitMix64(seed, corridorWidthStreamSalt),
		terrain:       core.NewSplitMix64(seed, terrainStreamSalt),
	}
}
