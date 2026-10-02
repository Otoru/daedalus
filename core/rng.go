package core

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

	// uniformMantissaBits is the number of random bits exactly representable in
	// the mantissa used by uniform01.
	uniformMantissaBits = 53
	// uniformDiscardedBits removes low bits exceeding the mantissa.
	uniformDiscardedBits = 64 - uniformMantissaBits
	// uniformDenominator is 2^53 and keeps uniform01's upper bound exclusive.
	uniformDenominator = float64(uint64(1) << uniformMantissaBits)
)

// SplitMix64 is a mutable stream private to a single request. Instances must not
// be shared between goroutines. The state is unexported: a caller that needs to
// save and restore a stream copies the whole value, which is the only way to
// rewind one and cannot be confused with reseeding it.
type SplitMix64 struct {
	state uint64
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

// NewSplitMix64 derives one independent stream from a Seed and a salt, as
// mix64 of the Seed xor the salt. The salt is the caller's: core owns the
// arithmetic, not the catalogue of streams a generator chooses to keep.
// Two distinct salts give two streams that advance independently.
func NewSplitMix64(seed Seed, salt uint64) SplitMix64 {
	return SplitMix64{state: mix64(uint64(seed) ^ salt)}
}

// Next returns the stream's next value and advances its state exactly once.
func (stream *SplitMix64) Next() uint64 {
	value := mix64(stream.state)
	stream.state += splitMixGamma
	return value
}

// Uniform01 returns a value uniformly distributed in the interval [0, 1).
func (stream *SplitMix64) Uniform01() float64 {
	value := stream.Next() >> uniformDiscardedBits
	return float64(value) / uniformDenominator
}

// UniformInt returns an integer uniformly distributed between lower and upper,
// inclusive. Rejecting the incomplete prefix eliminates modulo bias.
func (stream *SplitMix64) UniformInt(lower, upper uint64) uint64 {
	if lower > upper {
		panic("uniformInt lower bound exceeds upper bound")
	}
	if lower == upper {
		return lower
	}

	rangeSize := upper - lower + 1
	if rangeSize == 0 {
		// Overflow represents exactly the entire uint64 domain.
		return stream.Next()
	}

	// In uint64 arithmetic, -rangeSize is 2^64-rangeSize, so this expression
	// computes 2^64 mod rangeSize: the number of values at the start of the
	// domain that must be discarded so the remainder is an exact multiple of
	// rangeSize. Without this rejection, lower values would be more probable.
	rejectionThreshold := -rangeSize % rangeSize
	for {
		value := stream.Next()
		if value >= rejectionThreshold {
			return lower + value%rangeSize
		}
	}
}
