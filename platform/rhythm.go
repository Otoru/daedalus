package platform

import (
	"context"

	"github.com/Otoru/daedalus/core"
)

// Platform rhythm keeps its own SplitMix64 salts. They are not the dungeon
// generator's seven streams, and they must not become those streams: a salt
// shared with placement or terrain would couple a platform map to a dungeon
// layout that happens to use the same seed.
const (
	// spineShapeSalt feeds the stream that decides the spine's shape: the
	// run length, where the path folds, and which node a spur leaves from.
	spineShapeSalt uint64 = 0x51A7E0D4B3C2916F
	// beatChoiceSalt feeds the stream that draws beat kinds. It is separate
	// from the shape stream so a change in the vocabulary does not move the
	// fold.
	beatChoiceSalt uint64 = 0x2E8B4C17A9D05F63
	// geometrySpanSalt feeds the stream that draws a beat's horizontal
	// extent. Realization consumes it. The abstract spine does not.
	geometrySpanSalt uint64 = 0x8F03D6A1C4B27E50
)

// BeatReaction records what realization did with a beat the oracle was
// asked about. The zero value means the proposed step was kept.
type BeatReaction int

const (
	// BeatReactionNone means the proposed step stands. The usual reason is
	// that the oracle certified it. A refusal that no cheaper direction
	// could repair is also kept, and its Judgement says so.
	BeatReactionNone BeatReaction = iota
	// BeatReactionRewritten means the oracle refused the proposed step and
	// the spine changed the step into one the moveset could realise. A
	// refused climb is rewritten toward a flat crossing or a fall. A refused
	// fall is shortened. It is never rewritten into a climb: that is the
	// direction that costs.
	BeatReactionRewritten
)

// String returns the reaction's lowercase name.
func (r BeatReaction) String() string {
	switch r {
	case BeatReactionRewritten:
		return "rewritten"
	case BeatReactionNone:
		return "none"
	}
	return "unspecified"
}

// RealizedBeat is one spine step after the oracle has been asked whether
// the moveset can cross it. Grid is the beat's own geometry, with the
// departure platform on the left. It is not a cell of the plane: packing
// beats into rooms belongs to a later front. Column and Altitude on the
// spine are what place the beat in the run.
type RealizedBeat struct {
	// From is the tail node.
	From BeatID
	// To is the head node.
	To BeatID
	// Proposed is the direction the abstract spine asked for.
	Proposed SpineDirection
	// Direction is the direction that survived the oracle. It equals
	// Proposed when the step was kept.
	Direction SpineDirection
	// Kind is the beat kind that survived. A rewrite may substitute a kind
	// the new direction can actually be.
	Kind BeatKind
	// Difficulty is the surviving kind's demand.
	Difficulty uint8
	// FromAltitude and ToAltitude are the abstract altitudes after the
	// oracle's answer. A rewritten climb lands at FromAltitude.
	FromAltitude int32
	ToAltitude   int32
	// Reaction says whether the spine changed the step.
	Reaction BeatReaction
	// Grid is the local geometry the oracle was shown for the surviving step.
	Grid Grid
	// Departure and Arrival are the rest states the surviving query used.
	Departure MotionNode
	Arrival   MotionNode
	// Judgement is the oracle's answer for the surviving step.
	Judgement Judgement
	// Maneuver is the edge the oracle certified. It is meaningful only when
	// Judgement certifies.
	Maneuver MotionEdge
}

// Rhythm is an abstract spine whose steps have been shown to an oracle.
// Spine is the spine after reactions: a climb the moveset refused is no
// longer stored as a climb. Beats holds one entry per step, in edge order,
// including the proposal that was rewritten.
type Rhythm struct {
	// Spine is the spine that realization committed to.
	Spine Spine
	// Beats is one entry per spine edge, in edge order.
	Beats []RealizedBeat
}

// GenerateRhythm runs the two passes in order. First the abstract spine,
// then one oracle question per step. abilities is the moveset the character
// holds now, which is not a field of Config: the same configuration answers
// every stage of a progression, and the stage is this argument.
//
// checker is the front-B edge oracle. A nil checker is a malformed request.
// The generator does not implement a movement model of its own; a step it
// cannot get a certificate for is a step it rewrites or reports, never one
// it silently declares possible.
func GenerateRhythm(ctx context.Context, checker EdgeChecker, config Config, abilities AbilitySet) (Rhythm, error) {
	if checker == nil {
		return Rhythm{}, queryError("rhythm requires an edge checker")
	}
	spine, err := GenerateSpine(ctx, config, abilities)
	if err != nil {
		return Rhythm{}, err
	}
	config = config.Normalize()
	beats, spine, err := realizeSpine(ctx, checker, config, abilities, spine)
	if err != nil {
		return Rhythm{}, err
	}
	return Rhythm{Spine: spine, Beats: beats}, nil
}

type realizer struct {
	checker   EdgeChecker
	config    Config
	abilities AbilitySet
	spans     []uint32
}

func realizeSpine(ctx context.Context, checker EdgeChecker, config Config, abilities AbilitySet, spine Spine) ([]RealizedBeat, Spine, error) {
	stream := core.NewSplitMix64(config.Seed, geometrySpanSalt)
	spans := make([]uint32, len(spine.Nodes))
	for id := 1; id < len(spine.Nodes); id++ {
		spans[id] = drawSpan(config.Beats, spine.Nodes[id].Kind, &stream)
	}
	worker := realizer{checker: checker, config: config, abilities: abilities, spans: spans}
	altitudes := make([]int32, len(spine.Nodes))
	for index, node := range spine.Nodes {
		altitudes[index] = node.Altitude
	}
	beats := make([]RealizedBeat, 0, len(spine.Edges))
	for index := range spine.Edges {
		if err := ctx.Err(); err != nil {
			return nil, Spine{}, err
		}
		edge := spine.Edges[index]
		beat, err := worker.attempt(ctx, edge, altitudes[edge.From], altitudes[edge.To], edge.Direction, spine.Nodes[edge.To].Kind)
		if err != nil {
			return nil, Spine{}, err
		}
		if !beat.Judgement.Certified() {
			beat, err = worker.react(ctx, beat, altitudes[edge.From], altitudes[edge.To])
			if err != nil {
				return nil, Spine{}, err
			}
		}
		altitudes[edge.To] = beat.ToAltitude
		spine.Nodes[edge.To].Altitude = beat.ToAltitude
		spine.Nodes[edge.To].Kind = beat.Kind
		spine.Nodes[edge.To].Difficulty = beat.Difficulty
		spine.Edges[index].Direction = beat.Direction
		beats = append(beats, beat)
	}
	return beats, spine, nil
}

func (r realizer) attempt(ctx context.Context, edge SpineEdge, fromAltitude, toAltitude int32, direction SpineDirection, kind BeatKind) (RealizedBeat, error) {
	surviving := direction
	if direction == SpineDirectionUp || direction == SpineDirectionDown || direction == SpineDirectionLateral {
		surviving = directionBetween(fromAltitude, toAltitude)
	}
	kind, difficulty := substituteKind(r.config.Beats, surviving, r.abilities, kind)
	span := r.spans[edge.To]
	if span < 2 {
		span = 2
	}
	rise := worldRise(fromAltitude, toAltitude)
	grid := stampBeat(kind, span, rise)
	departure, arrival := restNodes(r.config.Profile, kind, span, rise)
	result, err := r.checker.CheckEdge(ctx, EdgeQuery{
		Grid:                grid,
		Profile:             r.config.Profile,
		Abilities:           r.abilities,
		From:                departure,
		To:                  arrival,
		AnyArrivalResources: true,
		Budget:              r.config.Budget,
		OmitWitness:         r.config.OmitWitness,
	})
	if err != nil {
		return RealizedBeat{}, err
	}
	return RealizedBeat{
		From:         edge.From,
		To:           edge.To,
		Proposed:     edge.Direction,
		Direction:    surviving,
		Kind:         kind,
		Difficulty:   difficulty,
		FromAltitude: fromAltitude,
		ToAltitude:   toAltitude,
		Grid:         grid,
		Departure:    departure,
		Arrival:      arrival,
		Judgement:    result.Judgement,
		Maneuver:     result.Edge,
	}, nil
}

// react answers a step the oracle refused. Climbing and falling are not
// the same repair. A refused climb spends its budget downward: one altitude
// step, then a flat crossing, then a drop. A refused fall is only shortened.
// Nothing in here turns a fall into a climb, because that is the direction
// the moveset may be unable to pay for.
func (r realizer) react(ctx context.Context, beat RealizedBeat, fromAltitude, toAltitude int32) (RealizedBeat, error) {
	if err := ctx.Err(); err != nil {
		return RealizedBeat{}, err
	}
	edge := SpineEdge{From: beat.From, To: beat.To, Direction: beat.Proposed}
	if beat.Proposed == SpineDirectionUp || beat.Arrival.Height > beat.Departure.Height {
		if toAltitude > fromAltitude+1 {
			rewritten, ok, err := r.repair(ctx, beat, edge, fromAltitude, fromAltitude+1, SpineDirectionUp)
			if err != nil || ok {
				return rewritten, err
			}
		}
		rewritten, ok, err := r.repair(ctx, beat, edge, fromAltitude, fromAltitude, SpineDirectionLateral)
		if err != nil || ok {
			return rewritten, err
		}
		drop := descentQuantum(ascentQuantum(r.config.Profile, r.abilities))
		rewritten, ok, err = r.repair(ctx, beat, edge, fromAltitude, fromAltitude-drop, SpineDirectionDown)
		if err != nil || ok {
			return rewritten, err
		}
		return beat, nil
	}
	if toAltitude < fromAltitude-1 {
		rewritten, ok, err := r.repair(ctx, beat, edge, fromAltitude, fromAltitude-1, beat.Direction)
		if err != nil || ok {
			return rewritten, err
		}
	}
	return beat, nil
}

func (r realizer) repair(ctx context.Context, beat RealizedBeat, edge SpineEdge, fromAltitude, toAltitude int32, direction SpineDirection) (RealizedBeat, bool, error) {
	next, ok, err := r.try(ctx, edge, fromAltitude, toAltitude, direction, beat.Kind)
	if err != nil || !ok {
		return RealizedBeat{}, false, err
	}
	return markRewritten(beat, next), true, nil
}

func (r realizer) try(ctx context.Context, edge SpineEdge, fromAltitude, toAltitude int32, direction SpineDirection, kind BeatKind) (RealizedBeat, bool, error) {
	next, err := r.attempt(ctx, edge, fromAltitude, toAltitude, direction, kind)
	if err != nil {
		return RealizedBeat{}, false, err
	}
	if !next.Judgement.Certified() {
		return RealizedBeat{}, false, nil
	}
	return next, true, nil
}

func markRewritten(original, next RealizedBeat) RealizedBeat {
	next.Proposed = original.Proposed
	next.Reaction = BeatReactionRewritten
	return next
}
