package daedalus

const (
	// splitMixGamma is the odd increment fixed by the SplitMix64 algorithm.
	splitMixGamma uint64 = 0x9E3779B97F4A7C15
	// splitMixFirstMultiplier is the first SplitMix64 finalization multiplier.
	splitMixFirstMultiplier uint64 = 0xBF58476D1CE4E5B9
	// splitMixSecondMultiplier is the second SplitMix64 finalization multiplier.
	splitMixSecondMultiplier uint64 = 0x94D049BB133111EB
	// splitMixFirstShift is the first logical shift in SplitMix64 finalization.
	splitMixFirstShift = 30
	// splitMixSecondShift is the second logical shift in SplitMix64 finalization.
	splitMixSecondShift = 27
	// splitMixFinalShift is the last logical shift in SplitMix64 finalization.
	splitMixFinalShift = 31

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

	// uniformMantissaBits is the number of random bits exactly representable in
	// the mantissa used by uniform01.
	uniformMantissaBits = 53
	// uniformDiscardedBits removes low bits exceeding the mantissa.
	uniformDiscardedBits = 64 - uniformMantissaBits
	// uniformDenominator is 2^53 and keeps uniform01's upper bound exclusive.
	uniformDenominator = float64(uint64(1) << uniformMantissaBits)
)

// splitMix64 is a mutable stream private to a single request. Instances must not
// be shared between goroutines.
type splitMix64 struct {
	state uint64
}

// rngStreams contains a request's seven independent streams.
type rngStreams struct {
	placement     splitMix64
	connector     splitMix64
	roomPlant     splitMix64
	roomGeometry  splitMix64
	corridorPlant splitMix64
	corridorWidth splitMix64
	terrain       splitMix64
}

// mix64 is SplitMix64 finalization. Add 0x9E3779B97F4A7C15, then
// z = (z xor (z>>30)) * 0xBF58476D1CE4E5B9, then
// z = (z xor (z>>27)) * 0x94D049BB133111EB, and return z xor (z>>31).
// Uint64 arithmetic wraps modulo 2^64 as defined by Go.
func mix64(value uint64) uint64 {
	value += splitMixGamma
	value = (value ^ (value >> splitMixFirstShift)) * splitMixFirstMultiplier
	value = (value ^ (value >> splitMixSecondShift)) * splitMixSecondMultiplier
	return value ^ (value >> splitMixFinalShift)
}

func newSplitMix64(seed uint64) splitMix64 {
	return splitMix64{state: seed}
}

// newRNGStreams derives every stream directly from the request Seed.
func newRNGStreams(seed Seed) rngStreams {
	seedValue := uint64(seed)
	return rngStreams{
		placement:     newSplitMix64(mix64(seedValue ^ placementStreamSalt)),
		connector:     newSplitMix64(mix64(seedValue ^ connectorStreamSalt)),
		roomPlant:     newSplitMix64(mix64(seedValue ^ roomPlantStreamSalt)),
		roomGeometry:  newSplitMix64(mix64(seedValue ^ roomGeometryStreamSalt)),
		corridorPlant: newSplitMix64(mix64(seedValue ^ corridorPlantStreamSalt)),
		corridorWidth: newSplitMix64(mix64(seedValue ^ corridorWidthStreamSalt)),
		terrain:       newSplitMix64(mix64(seedValue ^ terrainStreamSalt)),
	}
}

// next returns the stream's next value and advances its state exactly once.
func (stream *splitMix64) next() uint64 {
	value := mix64(stream.state)
	stream.state += splitMixGamma
	return value
}

// uniform01 returns a value uniformly distributed in the interval [0, 1).
func (stream *splitMix64) uniform01() float64 {
	value := stream.next() >> uniformDiscardedBits
	return float64(value) / uniformDenominator
}

// uniformInt returns an integer uniformly distributed between lower and upper,
// inclusive. Equal bounds return that value without advancing the stream.
// Otherwise rejection sampling may call next more than once to eliminate
// modulo bias; the full uint64 range calls next exactly once.
func (stream *splitMix64) uniformInt(lower, upper uint64) uint64 {
	if lower > upper {
		panic("uniformInt lower bound exceeds upper bound")
	}
	if lower == upper {
		return lower
	}

	rangeSize := upper - lower + 1
	if rangeSize == 0 {
		// Overflow represents exactly the entire uint64 domain.
		return stream.next()
	}

	// In uint64 arithmetic, -rangeSize is 2^64-rangeSize, so this expression
	// computes 2^64 mod rangeSize: the number of values at the start of the
	// domain that must be discarded so the remainder is an exact multiple of
	// rangeSize. Without this rejection, lower values would be more probable.
	rejectionThreshold := -rangeSize % rangeSize
	for {
		value := stream.next()
		if value >= rejectionThreshold {
			return lower + value%rangeSize
		}
	}
}
