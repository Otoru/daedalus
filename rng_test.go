package daedalus

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Otoru/daedalus/core"
)

// firstDraw reports the first value a stream would produce. core keeps the raw
// state unexported, so the way to observe a stream is to make it speak: the
// argument is a copy, so the caller's stream is left untouched. mix64 is a
// bijection, so two streams agree on this value exactly when their states agree.
func firstDraw(stream core.SplitMix64) uint64 {
	return stream.Next()
}

func TestDerivedStreamsAreDistinct(t *testing.T) {
	streams := newRNGStreams(Seed(0x0123456789abcdef))
	states := []uint64{
		firstDraw(streams.placement),
		firstDraw(streams.connector),
		firstDraw(streams.roomPlant),
		firstDraw(streams.roomGeometry),
		firstDraw(streams.corridorPlant),
		firstDraw(streams.corridorWidth),
	}
	want := []uint64{
		mix64Reference(0xc28d5fdbc9ad2973),
		mix64Reference(0x72719cb576c22598),
		mix64Reference(0x1f733cd593b32ebe),
		mix64Reference(0x094e8a43ce39fbe0),
		mix64Reference(0x7a76321e37168f90),
		mix64Reference(0xc5b661f1f596eb97),
	}
	assert.Equal(t, want, states, "frozen derivation of the six streams")

	for i := range states {
		for j := i + 1; j < len(states); j++ {
			assert.NotEqual(t, states[i], states[j], "streams %d e %d", i, j)
		}
	}
}

// mix64Reference repeats the SplitMix64 finalization with literal constants, so
// the frozen derived states above stay asserted against an independent copy of
// the arithmetic rather than against core's own mix64.
func mix64Reference(value uint64) uint64 {
	value += 0x9E3779B97F4A7C15
	value = (value ^ (value >> 30)) * 0xBF58476D1CE4E5B9
	value = (value ^ (value >> 27)) * 0x94D049BB133111EB
	return value ^ (value >> 31)
}

func TestSameSeedReproducesSequence(t *testing.T) {
	first := newRNGStreams(Seed(42))
	second := newRNGStreams(Seed(42))

	for draw := 0; draw < 128; draw++ {
		assert.Equal(t, first.placement.Next(), second.placement.Next(), "draw %d", draw)
	}
}

func TestConsumingOneStreamDoesNotChangeOthers(t *testing.T) {
	consumed := newRNGStreams(Seed(987654321))
	baseline := newRNGStreams(Seed(987654321))
	connectorStream := consumed.connector

	for range 1_000 {
		consumed.placement.Next()
	}

	require.Equal(t, connectorStream, consumed.connector,
		"the Connector stream must be receivable without consuming a draw")
	assert.Equal(t, baseline.connector.Next(), consumed.connector.Next())
	assert.Equal(t, baseline.roomPlant.Next(), consumed.roomPlant.Next())
	assert.Equal(t, baseline.roomGeometry.Next(), consumed.roomGeometry.Next())
	assert.Equal(t, baseline.corridorPlant.Next(), consumed.corridorPlant.Next())
	assert.Equal(t, baseline.corridorWidth.Next(), consumed.corridorWidth.Next())
}
