package platform

import (
	"context"
	"encoding/binary"

	"github.com/Otoru/daedalus/core"
)

// SpineDirection is the sense of one step of the rhythm spine. It is not a
// grid cardinal. Up and Down are opposite answers to gravity, and they are
// not symmetric: Down is a fall, which the base moveset can do, and Up is a
// climb, which the moveset may be unable to pay for. Lateral stays at the
// same altitude. Branch and Secret leave the main path; they are how the
// spine forks, not how it advances.
type SpineDirection int

const (
	// SpineDirectionUnspecified is the invalid zero value.
	SpineDirectionUnspecified SpineDirection = iota
	// SpineDirectionUp climbs. The destination altitude is higher, and the
	// moveset has to pay for it.
	SpineDirectionUp
	// SpineDirectionDown falls. The destination altitude is lower. A fall is
	// the cheap direction and is planned to outrun a climb.
	SpineDirectionDown
	// SpineDirectionLateral stays at the same altitude.
	SpineDirectionLateral
	// SpineDirectionBranch leaves the main path onto an optional side beat.
	SpineDirectionBranch
	// SpineDirectionSecret leaves the main path onto a hidden reward beat.
	SpineDirectionSecret
)

// String returns the direction's lowercase name.
func (d SpineDirection) String() string {
	switch d {
	case SpineDirectionUp:
		return "up"
	case SpineDirectionDown:
		return "down"
	case SpineDirectionLateral:
		return "lateral"
	case SpineDirectionBranch:
		return "branch"
	case SpineDirectionSecret:
		return "secret"
	}
	return "unspecified"
}

// SpineRole says which run a node belongs to. The main path is the
// progression. A branch and a secret are spurs off it, and a spur is not a
// later main beat that happens to sit nearby.
type SpineRole int

const (
	// SpineRoleUnspecified is the invalid zero value.
	SpineRoleUnspecified SpineRole = iota
	// SpineRoleMain is the progression path.
	SpineRoleMain
	// SpineRoleBranch is an optional side path.
	SpineRoleBranch
	// SpineRoleSecret is a hidden spur.
	SpineRoleSecret
)

// String returns the role's lowercase name.
func (r SpineRole) String() string {
	switch r {
	case SpineRoleMain:
		return "main"
	case SpineRoleBranch:
		return "branch"
	case SpineRoleSecret:
		return "secret"
	}
	return "unspecified"
}

// BeatID identifies a node of a Spine. IDs are stable indices in creation
// order starting at zero. Main-path nodes come first, then the secret spur,
// then the side branch, when those spurs exist.
type BeatID uint32

// SpineNode is one beat placed in the abstract plane. Column grows to the
// right on the way out and back to the left after the fold, so the main
// path is not a monotone rightward queue. Altitude grows upward in the
// world frame; it is not a grid row.
type SpineNode struct {
	// ID is this node's stable identifier within the spine.
	ID BeatID
	// Kind is the beat played to arrive here. The origin's kind is where
	// the run starts.
	Kind BeatKind
	// Altitude is the abstract height. A larger value is higher. Negative
	// is legal: a descent is allowed to fall past the origin.
	Altitude int32
	// Column is the abstract horizontal station. The fold is a column that
	// stops growing and starts shrinking.
	Column int32
	// Role says whether the node is on the main path or on a spur.
	Role SpineRole
	// Difficulty is the definition's demand, copied so a consumer can read
	// the pacing without going back to the config.
	Difficulty uint8
}

// SpineEdge is a directed step between two spine nodes. The direction is
// the step's meaning — climb, fall, fork — and not a summary of the
// coordinates that a later pass is free to ignore.
type SpineEdge struct {
	// From is the tail node.
	From BeatID
	// To is the head node.
	To BeatID
	// Direction is the sense of the step.
	Direction SpineDirection
}

// Spine is the abstract rhythm: beat nodes joined by directed steps that
// carry a direction. It is not a geometry and not a jump graph. Column and
// Altitude place it in a plane; the per-beat grids are produced later, by
// asking the oracle whether each step fits the moveset.
type Spine struct {
	// Nodes lists the beats in ascending ID order.
	Nodes []SpineNode
	// Edges lists the steps in creation order: the main path first, then
	// the secret spur, then the side branch.
	Edges []SpineEdge
}

// Node returns the node with the given ID and whether it exists. IDs are
// the indices, so the lookup does not search.
func (s Spine) Node(id BeatID) (SpineNode, bool) {
	if int(id) >= len(s.Nodes) {
		return SpineNode{}, false
	}
	node := s.Nodes[id]
	if node.ID != id {
		return SpineNode{}, false
	}
	return node, true
}

// Canonical appends the spine's identity as a big-endian byte string. Two
// spines produced from the same seed encode to the same bytes, and the
// encoding does not visit a map. Node order and edge order are creation
// order.
func (s Spine) Canonical() []byte {
	out := make([]byte, 0, 16+len(s.Nodes)*21+len(s.Edges)*12)
	out = appendU32(out, uint32(len(s.Nodes)))
	for _, node := range s.Nodes {
		out = appendU32(out, uint32(node.ID))
		out = appendI32(out, int32(node.Kind))
		out = appendI32(out, node.Altitude)
		out = appendI32(out, node.Column)
		out = appendU32(out, uint32(node.Role))
		out = append(out, node.Difficulty)
	}
	out = appendU32(out, uint32(len(s.Edges)))
	for _, edge := range s.Edges {
		out = appendU32(out, uint32(edge.From))
		out = appendU32(out, uint32(edge.To))
		out = appendU32(out, uint32(edge.Direction))
	}
	return out
}

func appendU32(dst []byte, value uint32) []byte {
	return binary.BigEndian.AppendUint32(dst, value)
}

func appendI32(dst []byte, value int32) []byte {
	return binary.BigEndian.AppendUint32(dst, uint32(value))
}

// GenerateSpine builds the abstract spine for config under the moveset the
// character holds in abilities. The shape — where the path folds, where a
// secret leaves — comes from spineShapeSalt. The beat kinds come from
// beatChoiceSalt. Neither salt is a dungeon salt.
//
// The shape is a decision, not a noise residual. For a run of three or more
// beats the path climbs to an interior peak and then falls back, and each
// falling step drops twice what a climbing step gained. A two-beat run
// climbs rather than staying flat. A secret listed in the branch
// distribution becomes a spur; it is not left to the chance that a weighted
// draw happens to name it.
func GenerateSpine(ctx context.Context, config Config, abilities AbilitySet) (Spine, error) {
	if err := ctx.Err(); err != nil {
		return Spine{}, err
	}
	config = config.Normalize()
	if err := config.Validate(); err != nil {
		return Spine{}, err
	}
	shape := core.NewSplitMix64(config.Seed, spineShapeSalt)
	choice := core.NewSplitMix64(config.Seed, beatChoiceSalt)
	return buildSpine(config, abilities, &shape, &choice)
}

// buildSpine consumes the two streams in a fixed order: every shape draw
// (run length, peak, spur attachments), then every kind draw (one per main
// node, then the branch kind). Interleaving them would make a vocabulary
// change move the fold.
func buildSpine(config Config, abilities AbilitySet, shape, choice *core.SplitMix64) (Spine, error) {
	lower, upper := runBounds(config.Beats.Spine)
	run := int(shape.UniformInt(uint64(lower), uint64(upper)))
	if run < 1 {
		return Spine{}, beatsError("the spine run is empty")
	}
	if run > MaxBeatsPerRoom {
		return Spine{}, limitError("the spine run is %d beats, above the ceiling of %d", run, MaxBeatsPerRoom)
	}

	climb := ascentQuantum(config.Profile, abilities)
	drop := descentQuantum(climb)
	altitudes := make([]int32, run)
	columns := make([]int32, run)
	peak := 0
	if run >= 3 {
		peak = 1
		if run > 3 {
			peak = int(shape.UniformInt(1, uint64(run-2)))
		}
		for index := 1; index < run; index++ {
			if index <= peak {
				altitudes[index] = altitudes[index-1] + climb
				columns[index] = columns[index-1] + 1
				continue
			}
			altitudes[index] = altitudes[index-1] - drop
			columns[index] = columns[index-1] - 1
		}
	} else if run == 2 {
		altitudes[1] = climb
		columns[1] = 1
	}

	branches := config.Beats.Branches
	if branches == nil {
		branches = config.Beats.Spine
	}
	secretParent, wantSecret := spurParent(shape, branches, BeatKindSecret, abilities, config, run)
	branchParent, wantBranch := otherSpurParent(shape, branches, abilities, config, run, wantSecret)

	nodes := make([]SpineNode, run)
	for index := 0; index < run; index++ {
		direction := SpineDirectionLateral
		if index > 0 {
			direction = directionBetween(altitudes[index-1], altitudes[index])
		}
		kind, difficulty, err := selectBeat(config.Beats, config.Beats.Spine, direction, abilities, paceTarget(index, run), choice)
		if err != nil {
			return Spine{}, err
		}
		nodes[index] = SpineNode{
			ID:         BeatID(index),
			Kind:       kind,
			Altitude:   altitudes[index],
			Column:     columns[index],
			Role:       SpineRoleMain,
			Difficulty: difficulty,
		}
	}

	edges := make([]SpineEdge, 0, run+1)
	for index := 1; index < run; index++ {
		edges = append(edges, SpineEdge{
			From:      BeatID(index - 1),
			To:        BeatID(index),
			Direction: directionBetween(altitudes[index-1], altitudes[index]),
		})
	}

	if wantSecret {
		parent := nodes[secretParent]
		nodes = append(nodes, SpineNode{
			ID:         BeatID(len(nodes)),
			Kind:       BeatKindSecret,
			Altitude:   parent.Altitude - drop,
			Column:     parent.Column,
			Role:       SpineRoleSecret,
			Difficulty: definitionDifficulty(config, BeatKindSecret),
		})
		edges = append(edges, SpineEdge{
			From:      parent.ID,
			To:        nodes[len(nodes)-1].ID,
			Direction: SpineDirectionSecret,
		})
	}
	if wantBranch {
		parent := nodes[branchParent]
		kind, difficulty, err := selectBeat(config.Beats, branches, SpineDirectionBranch, abilities, paceTarget(branchParent, run), choice)
		if err != nil {
			return Spine{}, err
		}
		nodes = append(nodes, SpineNode{
			ID:         BeatID(len(nodes)),
			Kind:       kind,
			Altitude:   parent.Altitude,
			Column:     parent.Column + 2,
			Role:       SpineRoleBranch,
			Difficulty: difficulty,
		})
		edges = append(edges, SpineEdge{
			From:      parent.ID,
			To:        nodes[len(nodes)-1].ID,
			Direction: SpineDirectionBranch,
		})
	}
	return Spine{Nodes: nodes, Edges: edges}, nil
}

func directionBetween(from, to int32) SpineDirection {
	switch {
	case to > from:
		return SpineDirectionUp
	case to < from:
		return SpineDirectionDown
	default:
		return SpineDirectionLateral
	}
}

func definitionDifficulty(config Config, kind BeatKind) uint8 {
	definition, ok := config.Beats.Definition(kind)
	if !ok {
		return 0
	}
	return definition.Difficulty
}

// spurParent decides whether a spur of the requested kind leaves the main
// path, and from which node. The kind has to be listed in the branch
// distribution and eligible for the moveset. The attachment is drawn from
// every main node except the last, so a spur does not replace the end of
// the progression.
func spurParent(shape *core.SplitMix64, distribution *BeatDistribution, kind BeatKind, abilities AbilitySet, config Config, run int) (int, bool) {
	if run < 2 || !distributionLists(distribution, kind) {
		return 0, false
	}
	definition, ok := config.Beats.Definition(kind)
	if !ok || !eligible(definition, abilities) {
		return 0, false
	}
	if run >= MaxBeatsPerRoom {
		return 0, false
	}
	return int(shape.UniformInt(0, uint64(run-2))), true
}

// otherSpurParent places one side branch when the branch distribution has
// an eligible kind that is not the secret. wantSecret reserves one slot of
// the room's beat ceiling for the secret spur.
func otherSpurParent(shape *core.SplitMix64, distribution *BeatDistribution, abilities AbilitySet, config Config, run int, wantSecret bool) (int, bool) {
	if run < 2 {
		return 0, false
	}
	used := run
	if wantSecret {
		used++
	}
	if used >= MaxBeatsPerRoom {
		return 0, false
	}
	if !hasBranchKind(config, distribution, abilities) {
		return 0, false
	}
	return int(shape.UniformInt(0, uint64(run-2))), true
}

func distributionLists(distribution *BeatDistribution, kind BeatKind) bool {
	if distribution == nil {
		return false
	}
	for _, weight := range distribution.Beats {
		if weight.Kind == kind {
			return true
		}
	}
	return false
}

func hasBranchKind(config Config, distribution *BeatDistribution, abilities AbilitySet) bool {
	if distribution == nil {
		return false
	}
	for _, weight := range distribution.Beats {
		if weight.Kind == BeatKindSecret {
			continue
		}
		definition, ok := config.Beats.Definition(weight.Kind)
		if !ok || !eligible(definition, abilities) {
			continue
		}
		if suits(SpineDirectionBranch, weight.Kind) {
			return true
		}
	}
	return false
}
