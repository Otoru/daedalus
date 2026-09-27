package daedalus

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMix64PreservesFrozenVectors(t *testing.T) {
	tests := []struct {
		name string
		seed uint64
		want uint64
	}{
		{name: "zero", seed: 0x0000000000000000, want: 0xe220a8397b1dcdaf},
		{name: "one", seed: 0x0000000000000001, want: 0x910a2dec89025cc1},
		{name: "large value", seed: 0xfedcba9876543210, want: 0x7ae893b5e32fee86},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, mix64(tc.seed))
		})
	}
}

func TestDerivedStreamsAreDistinct(t *testing.T) {
	streams := newRNGStreams(Seed(0x0123456789abcdef))
	states := []uint64{
		streams.placement.state,
		streams.connector.state,
		streams.roomPlant.state,
		streams.roomGeometry.state,
		streams.corridorPlant.state,
	}
	want := []uint64{
		0xc28d5fdbc9ad2973,
		0x72719cb576c22598,
		0x1f733cd593b32ebe,
		0x094e8a43ce39fbe0,
		0x7a76321e37168f90,
	}
	assert.Equal(t, want, states, "frozen derivation of the five streams")

	for i := range states {
		for j := i + 1; j < len(states); j++ {
			assert.NotEqual(t, states[i], states[j], "streams %d e %d", i, j)
		}
	}
}

func TestSameSeedReproducesSequence(t *testing.T) {
	first := newRNGStreams(Seed(42))
	second := newRNGStreams(Seed(42))

	for draw := 0; draw < 128; draw++ {
		assert.Equal(t, first.placement.next(), second.placement.next(), "draw %d", draw)
	}
}

func TestUniform01RespectsHalfOpenInterval(t *testing.T) {
	const stateWhoseNextValueIsZero = uint64(0x61c8864680b583eb)
	zeroStream := splitMix64{state: stateWhoseNextValueIsZero}
	assert.Equal(t, 0.0, zeroStream.uniform01())

	stream := newSplitMix64(0)
	for draw := 0; draw < 100_000; draw++ {
		got := stream.uniform01()
		assert.GreaterOrEqual(t, got, 0.0, "draw %d", draw)
		assert.Less(t, got, 1.0, "draw %d", draw)
	}
}

func TestUniformIntRespectsInclusiveBounds(t *testing.T) {
	stream := newSplitMix64(1234)
	seenLower := false
	seenUpper := false
	for draw := 0; draw < 10_000; draw++ {
		got := stream.uniformInt(3, 7)
		assert.GreaterOrEqual(t, got, uint64(3), "draw %d", draw)
		assert.LessOrEqual(t, got, uint64(7), "draw %d", draw)
		seenLower = seenLower || got == 3
		seenUpper = seenUpper || got == 7
	}
	assert.True(t, seenLower, "lower bound was not observed")
	assert.True(t, seenUpper, "upper bound was not observed")
}

func TestUniformIntWithEqualBoundsConsumesNoDraw(t *testing.T) {
	stream := newSplitMix64(5678)
	stateBefore := stream.state

	got := stream.uniformInt(9, 9)

	assert.Equal(t, uint64(9), got)
	assert.Equal(t, stateBefore, stream.state)
}

func TestUniformIntDoesNotFavorFirstValues(t *testing.T) {
	const (
		sampleCount = 200_000
		bucketCount = 10
		tolerance   = 0.03
	)
	stream := newSplitMix64(0xdecafbad12345678)
	counts := make([]int, bucketCount)
	for range sampleCount {
		counts[stream.uniformInt(0, bucketCount-1)]++
	}

	expected := float64(sampleCount) / float64(bucketCount)
	for value, count := range counts {
		assert.InDelta(t, expected, float64(count), expected*tolerance, "value %d", value)
	}
}

func TestUniformIntRejectsIncompletePrefix(t *testing.T) {
	const (
		initialState      = uint64(3)
		upper             = uint64(1 << 63)
		wantAfterRejected = uint64(0x33466f8a7b81a988)
		wantFinalState    = uint64(0x3c6ef372fe94f82d)
	)
	stream := newSplitMix64(initialState)

	got := stream.uniformInt(0, upper)

	assert.Equal(t, wantAfterRejected, got)
	assert.Equal(t, wantFinalState, stream.state,
		"a value in the incomplete prefix must be rejected before the result")
}

func TestConsumingOneStreamDoesNotChangeOthers(t *testing.T) {
	consumed := newRNGStreams(Seed(987654321))
	baseline := newRNGStreams(Seed(987654321))
	connectorState := consumed.connector.state

	for range 1_000 {
		consumed.placement.next()
	}

	require.Equal(t, connectorState, consumed.connector.state,
		"the Connector stream must be receivable without consuming a draw")
	assert.Equal(t, baseline.connector.next(), consumed.connector.next())
	assert.Equal(t, baseline.roomPlant.next(), consumed.roomPlant.next())
	assert.Equal(t, baseline.roomGeometry.next(), consumed.roomGeometry.next())
	assert.Equal(t, baseline.corridorPlant.next(), consumed.corridorPlant.next())
}
