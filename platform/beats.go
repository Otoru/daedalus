package platform

import "github.com/Otoru/daedalus/core"

// The rhythm vocabulary is closed. BeatKind is the whole of it, and this file
// is the only place that knows how a kind becomes a stretch of geometry and
// how Difficulty paces a draw. Weights stay the caller's, in declaration
// order, the same way a terrain distribution is walked.

const (
	// canonicalMinRunBeats is the run length used when a distribution leaves
	// both bounds at zero. Four beats is the shortest stretch that can rise,
	// fold and fall.
	canonicalMinRunBeats uint32 = 4
	// canonicalMaxRunBeats is the upper end of that default span.
	canonicalMaxRunBeats uint32 = 8

	// cellsPerAltitude is how many world cells one abstract altitude step
	// stands for. Two cells is inside a single jump of DefaultProfile, whose
	// apex is four; a taller step is several of these, and the oracle — not
	// this constant — decides whether the moveset can pay for them.
	cellsPerAltitude int32 = 2

	// platformGap is the horizontal opening, in cells, between the departure
	// platform and the arrival platform. It is deliberately smaller than a
	// same-height jump of DefaultProfile. A beat's MinCells/MaxCells size the
	// platforms, not this opening: widening the opening until the oracle
	// refuses it would make every wide beat look like a rejected climb.
	platformGap uint32 = 2

	// floorPad is the world height of the lower platform's top, in cells, so
	// a grid always has ground under the feet and air above the body.
	floorPad int32 = 2

	// headroom is the air, in cells, left above the higher platform.
	headroom int32 = 4
)

// runBounds returns the inclusive run length. Both bounds zero means the
// canonical span; validation has already rejected a distribution that set
// only one of them.
func runBounds(distribution *BeatDistribution) (uint32, uint32) {
	if distribution.MinRunBeats == 0 && distribution.MaxRunBeats == 0 {
		return canonicalMinRunBeats, canonicalMaxRunBeats
	}
	return distribution.MinRunBeats, distribution.MaxRunBeats
}

// canonicalCells is the horizontal extent of a kind, in cells, when the
// definition leaves both MinCells and MaxCells at zero.
func canonicalCells(kind BeatKind) (uint32, uint32) {
	switch kind {
	case BeatKindRest, BeatKindCheckpoint:
		return 4, 6
	case BeatKindTraverse:
		return 6, 12
	case BeatKindGap, BeatKindHazard:
		return 4, 8
	case BeatKindClimb, BeatKindDescend, BeatKindSecret:
		return 3, 6
	case BeatKindShaft, BeatKindGate:
		return 2, 4
	case BeatKindPrecision:
		return 2, 3
	default:
		return 4, 6
	}
}

// cellSpan returns the inclusive horizontal bounds for a kind. A definition
// that set both bounds uses those; both zero uses the canonical span.
func cellSpan(definition BeatDefinition) (uint32, uint32) {
	if definition.MinCells == 0 && definition.MaxCells == 0 {
		return canonicalCells(definition.Kind)
	}
	return definition.MinCells, definition.MaxCells
}

// paceTarget is the demand, in 0..255, that position index of a stretch of
// count beats is asking for. The curve opens at rest, climbs to the
// penultimate beat and exhales on the last one. Difficulty on a definition
// is where that kind sits on this curve. It is not a claim about players.
func paceTarget(index, count int) uint8 {
	if count <= 1 || index <= 0 || index >= count-1 {
		return 0
	}
	peakAt := count - 2
	if peakAt < 1 || index >= peakAt {
		return 255
	}
	return uint8((255 * index) / peakAt)
}

// pacedWeight scales a declared weight by how close the kind's difficulty
// sits to the target. The caller's weight is the base; difficulty only
// multiplies it. A perfect match keeps weight×256 and a total miss keeps
// weight×1, so a kind that holds all of the weight is still selected when
// the curve would rather have something else.
func pacedWeight(weight uint32, difficulty, target uint8) uint64 {
	distance := int(difficulty) - int(target)
	if distance < 0 {
		distance = -distance
	}
	return uint64(weight) * uint64(256-distance)
}

// suits reports whether a kind is the natural vocabulary of a spine
// direction. A climb is not how a descent is spelled, and a secret is not a
// main-path beat. The draw prefers a suited kind and relaxes only when the
// distribution has none.
func suits(direction SpineDirection, kind BeatKind) bool {
	switch direction {
	case SpineDirectionUp:
		return kind == BeatKindClimb || kind == BeatKindShaft || kind == BeatKindPrecision || kind == BeatKindGate
	case SpineDirectionDown:
		return kind == BeatKindDescend || kind == BeatKindGap || kind == BeatKindHazard || kind == BeatKindPrecision
	case SpineDirectionSecret:
		return kind == BeatKindSecret
	case SpineDirectionBranch:
		return kind != BeatKindSecret && kind != BeatKindClimb && kind != BeatKindShaft
	default:
		return kind != BeatKindSecret && kind != BeatKindClimb && kind != BeatKindShaft && kind != BeatKindDescend
	}
}

// eligible reports whether the moveset may be offered this kind. A gate is
// emitted even when the character lacks its key — the lock is the beat.
// Every other kind treats Requires as a floor.
func eligible(definition BeatDefinition, abilities AbilitySet) bool {
	if definition.Kind == BeatKindGate {
		return true
	}
	return abilities.Contains(definition.Requires)
}

type beatCandidate struct {
	kind   BeatKind
	weight uint64
}

// selectBeat draws one kind from distribution. Declaration order is the
// walk order, matching terrain selection: the first ticket is 1 and the
// draw is an inclusive UniformInt over the total paced weight. suited
// restricts the draw; an empty suited set falls back to every eligible kind
// except Secret, which only a secret direction may emit.
func selectBeat(config BeatConfig, distribution *BeatDistribution, direction SpineDirection, abilities AbilitySet, target uint8, stream *core.SplitMix64) (BeatKind, uint8, error) {
	candidates := beatCandidates(config, distribution, abilities, target, func(kind BeatKind) bool {
		return suits(direction, kind)
	})
	if len(candidates) == 0 {
		candidates = beatCandidates(config, distribution, abilities, target, func(kind BeatKind) bool {
			return direction == SpineDirectionSecret || kind != BeatKindSecret
		})
	}
	if len(candidates) == 0 {
		return BeatKindUnspecified, 0, beatsError("no beat in the %s distribution is emitted under the current moveset", direction)
	}
	var total uint64
	for _, candidate := range candidates {
		total += candidate.weight
	}
	draw := stream.UniformInt(1, total)
	chosen := candidates[len(candidates)-1]
	for _, candidate := range candidates {
		if draw <= candidate.weight {
			chosen = candidate
			break
		}
		draw -= candidate.weight
	}
	definition, _ := config.Definition(chosen.kind)
	return chosen.kind, definition.Difficulty, nil
}

func beatCandidates(config BeatConfig, distribution *BeatDistribution, abilities AbilitySet, target uint8, keep func(BeatKind) bool) []beatCandidate {
	candidates := make([]beatCandidate, 0, len(distribution.Beats))
	for _, weight := range distribution.Beats {
		if !keep(weight.Kind) {
			continue
		}
		definition, ok := config.Definition(weight.Kind)
		if !ok || !eligible(definition, abilities) {
			continue
		}
		candidates = append(candidates, beatCandidate{
			kind:   weight.Kind,
			weight: pacedWeight(weight.Weight, definition.Difficulty, target),
		})
	}
	return candidates
}

// substituteKind picks a declared kind that suits direction, walking the
// spine distribution in declaration order and not touching a random stream.
// A reaction that reshuffled would make a rejected beat move every later
// draw. The current kind is kept when it already suits, or when nothing
// else in the distribution does.
func substituteKind(config BeatConfig, direction SpineDirection, abilities AbilitySet, current BeatKind) (BeatKind, uint8) {
	if suits(direction, current) {
		definition, _ := config.Definition(current)
		return current, definition.Difficulty
	}
	if config.Spine != nil {
		for _, weight := range config.Spine.Beats {
			if !suits(direction, weight.Kind) {
				continue
			}
			definition, ok := config.Definition(weight.Kind)
			if !ok || !eligible(definition, abilities) {
				continue
			}
			return weight.Kind, definition.Difficulty
		}
	}
	definition, _ := config.Definition(current)
	return current, definition.Difficulty
}

// drawSpan consumes one integer from stream and returns the beat's
// horizontal extent. The draw happens once per beat, before the oracle is
// asked, so a rejection cannot change how many numbers later beats see.
func drawSpan(config BeatConfig, kind BeatKind, stream *core.SplitMix64) uint32 {
	definition, ok := config.Definition(kind)
	if !ok {
		definition.Kind = kind
	}
	lower, upper := cellSpan(definition)
	// A platform needs a cell to stand on. An extent of one cell is lifted
	// to two so the draw and the grid agree; both bounds equal still does
	// not consume a random number.
	if lower < 2 {
		lower = 2
	}
	if upper < lower {
		upper = lower
	}
	return uint32(stream.UniformInt(uint64(lower), uint64(upper)))
}

// worldRise converts an altitude delta into cells. The sign is the whole of
// the asymmetry at this layer: a positive value has to be climbed, a
// negative one is a fall.
func worldRise(fromAltitude, toAltitude int32) int32 {
	return (toAltitude - fromAltitude) * cellsPerAltitude
}

// ascentQuantum is how many abstract altitude steps one beat may climb
// under the moveset the character holds right now. The base jump pays for
// one step. A double jump pays for two. A wall jump or a climb pays for
// three. These are planning budgets. The oracle still has to certify the
// cells, and a budget the profile does not even parameterise is not spent.
func ascentQuantum(profile MovementProfile, abilities AbilitySet) int32 {
	quantum := int32(1)
	if abilities.Has(AbilityDoubleJump) && profile.DoubleJump != nil && profile.DoubleJump.Charges > 0 {
		quantum = 2
	}
	if abilities.Has(AbilityWallJump) && profile.WallJump != nil {
		quantum = 3
	}
	if abilities.Has(AbilityClimb) && profile.Climb != nil && quantum < 3 {
		quantum = 3
	}
	return quantum
}

// descentQuantum is how many abstract steps one beat may drop. It is twice
// the climb of the same moveset. Falling is free: a descent is not the
// inverse of an ascent, and the spine plans it that way on purpose.
func descentQuantum(ascent int32) int32 { return ascent * 2 }

// stampBeat builds the local grid of one beat. The departure platform sits
// on the left and the arrival platform on the right, separated by
// platformGap cells of air. rise is the arrival height minus the departure
// height, in world cells; negative means the arrival is lower.
func stampBeat(kind BeatKind, span uint32, rise int32) Grid {
	if span < 2 {
		span = 2
	}
	width := span + platformGap + span
	fromFeet, toFeet := feetPair(rise)
	top := fromFeet
	if toFeet > top {
		top = toFeet
	}
	height := uint32(top + headroom)
	cells := make([]CellKind, int(width)*int(height))
	fillPlatform(cells, width, height, 0, span, fromFeet)
	fillPlatform(cells, width, height, span+platformGap, span, toFeet)
	switch kind {
	case BeatKindHazard:
		paintGap(cells, width, height, span, toFeet, fromFeet, CellKindHazard)
	case BeatKindClimb, BeatKindShaft:
		if rise != 0 {
			paintClimb(cells, width, height, span, fromFeet, toFeet)
		}
	case BeatKindRest, BeatKindTraverse, BeatKindCheckpoint:
		if rise == 0 {
			fillPlatform(cells, width, height, span, platformGap, fromFeet)
		}
	}
	return Grid{Width: width, Height: height, Cells: cells}
}

// feetPair returns the local world height of the departure feet and the
// arrival feet. The lower of the two sits on floorPad.
func feetPair(rise int32) (int32, int32) {
	if rise >= 0 {
		return floorPad, floorPad + rise
	}
	return floorPad - rise, floorPad
}

func fillPlatform(cells []CellKind, width, height, x0, span uint32, feet int32) {
	row := int32(height) - feet
	if row < 0 || uint32(row) >= height {
		return
	}
	for x := x0; x < x0+span && x < width; x++ {
		cells[uint32(row)*width+x] = CellKindSolid
	}
}

func paintGap(cells []CellKind, width, height, span uint32, toFeet, fromFeet int32, kind CellKind) {
	lower := toFeet
	if fromFeet < lower {
		lower = fromFeet
	}
	row := int32(height) - lower
	if row < 0 || uint32(row) >= height {
		return
	}
	for x := span; x < span+platformGap && x < width; x++ {
		cells[uint32(row)*width+x] = kind
	}
}

func paintClimb(cells []CellKind, width, height, span uint32, fromFeet, toFeet int32) {
	if span == 0 || span-1 >= width {
		return
	}
	low, high := fromFeet, toFeet
	if high < low {
		low, high = high, low
	}
	x := span - 1
	// The foot rows stay solid: climbable does not support a standing
	// character, and overwriting the lip would remove the floor.
	for feet := low + 1; feet < high; feet++ {
		row := int32(height) - feet
		if row < 0 || uint32(row) >= height {
			continue
		}
		cells[uint32(row)*width+x] = CellKindClimbable
	}
}

// restNodes builds the departure and arrival states a beat asks the oracle
// about. Both are rest states. A continuous floor shares one surface, so
// the question can be answered by a walk; a gap, a step or a drop uses two
// surfaces, so the question is a jump or a fall. Which of those the moveset
// can actually do is the oracle's answer.
func restNodes(profile MovementProfile, kind BeatKind, span uint32, rise int32) (MotionNode, MotionNode) {
	if span < 2 {
		span = 2
	}
	fromFeet, toFeet := feetPair(rise)
	toSurface := SurfaceID(1)
	if rise == 0 && sharesSurface(kind) {
		toSurface = 0
	}
	departure := MotionNode{
		ID:        0,
		Surface:   0,
		Height:    float64(fromFeet),
		Footing:   Span{Lo: 0, Hi: float64(span)},
		Velocity:  Point(0),
		Mode:      MotionModeGrounded,
		Resources: profile.FullResources(),
	}
	arrival := MotionNode{
		ID:        1,
		Surface:   toSurface,
		Height:    float64(toFeet),
		Footing:   Span{Lo: float64(span + platformGap), Hi: float64(span + platformGap + span)},
		Velocity:  Point(0),
		Mode:      MotionModeGrounded,
		Resources: profile.FullResources(),
	}
	return departure, arrival
}

// sharesSurface reports whether a flat beat is one floor. A gap, a hazard
// and a gate are openings even when the two lips are at the same height, so
// they must not be asked as a walk.
func sharesSurface(kind BeatKind) bool {
	switch kind {
	case BeatKindGap, BeatKindHazard, BeatKindPrecision, BeatKindGate, BeatKindSecret:
		return false
	default:
		return true
	}
}
