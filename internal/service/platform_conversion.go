package service

import (
	"fmt"

	daedalusv1 "github.com/Otoru/daedalus/internal/gen/go/daedalus/v1"
	"github.com/Otoru/daedalus/platform"
)

func platformConfigFromProto(source *daedalusv1.PlatformConfig) (platform.Config, error) {
	if source == nil {
		return platform.Config{}, fmt.Errorf("%w: missing platform config", platform.ErrInvalidConfig)
	}
	target := platform.Config{Seed: platform.Seed(source.Seed), Width: source.Width, Height: source.Height, MaxRooms: source.MaxRooms, Profile: platform.DefaultProfile()}
	target.Beats.Definitions = make([]platform.BeatDefinition, len(source.BeatDefinitions))
	for i, definition := range source.BeatDefinitions {
		if definition == nil {
			return platform.Config{}, fmt.Errorf("%w: beat definition %d missing", platform.ErrInvalidBeats, i)
		}
		target.Beats.Definitions[i] = platform.BeatDefinition{Kind: platform.BeatKind(definition.Kind), MinCells: definition.MinCells, MaxCells: definition.MaxCells, Difficulty: uint8(definition.Difficulty), Requires: platform.AbilitySet(definition.Requires)}
	}
	var err error
	if target.Beats.Spine, err = platformBeatDistributionFromProto(source.Spine); err != nil {
		return platform.Config{}, err
	}
	if source.Branches != nil {
		if target.Beats.Branches, err = platformBeatDistributionFromProto(source.Branches); err != nil {
			return platform.Config{}, err
		}
	}
	return target, nil
}

func platformBeatDistributionFromProto(source *daedalusv1.PlatformBeatDistribution) (*platform.BeatDistribution, error) {
	if source == nil {
		return nil, fmt.Errorf("%w: spine distribution missing", platform.ErrInvalidBeats)
	}
	target := &platform.BeatDistribution{MinRunBeats: source.MinRunBeats, MaxRunBeats: source.MaxRunBeats, Beats: make([]platform.BeatWeight, len(source.Beats))}
	for i, weight := range source.Beats {
		if weight == nil {
			return nil, fmt.Errorf("%w: beat weight %d missing", platform.ErrInvalidBeats, i)
		}
		target.Beats[i] = platform.BeatWeight{Kind: platform.BeatKind(weight.Kind), Weight: weight.Weight}
	}
	return target, nil
}

func platformLayoutToProto(source PlatformLayout) *daedalusv1.PlatformLayout {
	result := &daedalusv1.PlatformLayout{Seed: uint64(source.Config.Seed), Width: source.Plane.Width, Height: source.Plane.Height, Judgement: platformJudgementToProto(source.Judgement), JumpGraph: platformJumpGraphToProto(source.JumpGraph), Rooms: make([]*daedalusv1.PlatformRoom, len(source.Plane.Rooms))}
	for i, room := range source.Plane.Rooms {
		cells := make([]daedalusv1.PlatformCellKind, len(room.Grid.Cells))
		for j, kind := range room.Grid.Cells {
			cells[j] = platformCellKindToProto(kind)
		}
		result.Rooms[i] = &daedalusv1.PlatformRoom{Id: uint32(room.ID), Origin: &daedalusv1.Cell{X: room.Origin.X, Y: room.Origin.Y}, Grid: &daedalusv1.PlatformGrid{Width: room.Grid.Width, Height: room.Grid.Height, Cells: cells}}
	}
	return result
}

func platformCellKindToProto(kind platform.CellKind) daedalusv1.PlatformCellKind {
	return daedalusv1.PlatformCellKind(kind + 1)
}
func platformJudgementToProto(j platform.Judgement) *daedalusv1.PlatformJudgement {
	return &daedalusv1.PlatformJudgement{Verdict: daedalusv1.PlatformVerdict(j.Verdict), Reason: daedalusv1.PlatformVerdictReason(j.Reason), Detail: j.Detail, Model: j.Model, ProfileVersion: j.ProfileVersion, Budget: &daedalusv1.PlatformBudgetReport{CandidateEdges: j.Budget.CandidateEdges, ExpandedNodes: j.Budget.ExpandedNodes, CollisionTests: j.Budget.CollisionTests, Exhausted: j.Budget.Exhausted}}
}
func platformJumpGraphToProto(graph platform.JumpGraph) *daedalusv1.PlatformJumpGraph {
	result := &daedalusv1.PlatformJumpGraph{Model: graph.Model, ProfileVersion: graph.ProfileVersion, Abilities: uint32(graph.Abilities), Nodes: make([]*daedalusv1.PlatformMotionNode, len(graph.Nodes)), Edges: make([]*daedalusv1.PlatformMotionEdge, len(graph.Edges))}
	for i, node := range graph.Nodes {
		result.Nodes[i] = &daedalusv1.PlatformMotionNode{Id: uint32(node.ID), Surface: uint32(node.Surface), Interval: node.Interval, Height: node.Height, FootingLo: node.Footing.Lo, FootingHi: node.Footing.Hi, VelocityLo: node.Velocity.Lo, VelocityHi: node.Velocity.Hi, Mode: uint32(node.Mode)}
	}
	for i, edge := range graph.Edges {
		result.Edges[i] = &daedalusv1.PlatformMotionEdge{Id: uint32(edge.ID), From: uint32(edge.From), To: uint32(edge.To), Kind: uint32(edge.Kind), Requires: uint32(edge.Requires), Passage: uint32(edge.Passage), Transition: uint32(edge.Transition), Duration: edge.Duration}
	}
	return result
}
