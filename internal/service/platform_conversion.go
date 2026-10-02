package service

import (
	"fmt"
	"math"

	daedalusv1 "github.com/Otoru/daedalus/internal/gen/go/daedalus/v1"
	"github.com/Otoru/daedalus/platform"
)

func platformConfigFromProto(source *daedalusv1.PlatformConfig) (platform.Config, error) {
	if source == nil {
		return platform.Config{}, fmt.Errorf("%w: missing platform config", platform.ErrInvalidConfig)
	}
	target := platform.Config{Seed: platform.Seed(source.Seed), Width: source.Width, Height: source.Height, MaxRooms: source.MaxRooms, Profile: platform.DefaultProfile()}
	if source.Profile != nil {
		profile, err := movementProfileFromProto(source.Profile)
		if err != nil {
			return platform.Config{}, err
		}
		target.Profile = profile
	}
	target.Beats.Definitions = make([]platform.BeatDefinition, len(source.BeatDefinitions))
	for i, definition := range source.BeatDefinitions {
		if definition == nil {
			return platform.Config{}, fmt.Errorf("%w: beat definition %d missing", platform.ErrInvalidBeats, i)
		}
		target.Beats.Definitions[i] = platform.BeatDefinition{Kind: platform.BeatKind(definition.Kind), MinCells: definition.MinCells, MaxCells: definition.MaxCells, Difficulty: uint8(definition.Difficulty), Requires: platform.AbilitySet(definition.Requires)}
	}
	if source.Progression != nil {
		target.Progression = platform.ProgressionPlan{Base: platform.AbilitySet(source.Progression.Base)}
		for i, step := range source.Progression.Steps {
			if step == nil {
				return platform.Config{}, fmt.Errorf("%w: progression step %d missing", platform.ErrInvalidConfig, i)
			}
			target.Progression.Steps = append(target.Progression.Steps, platform.ProgressionStep{Name: step.Name, Grants: platform.AbilitySet(step.Grants)})
		}
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

func movementProfileFromProto(source *daedalusv1.MovementProfile) (platform.MovementProfile, error) {
	if source == nil {
		return platform.MovementProfile{}, fmt.Errorf("%w: profile missing", platform.ErrInvalidProfile)
	}
	target := platform.MovementProfile{
		Version: source.Version, CellSize: source.CellSize, ControlRate: source.ControlRate,
		GravityUp: source.GravityUp, GravityDown: source.GravityDown, JumpVelocity: source.JumpVelocity,
		MaxRunSpeed: source.MaxRunSpeed, GroundAccel: source.GroundAccel, AirAccel: source.AirAccel,
		Braking: source.Braking, BodyHalfWidth: source.BodyHalfWidth, BodyHeight: source.BodyHeight,
		Margin: source.Margin, MaxFallSpeed: source.MaxFallSpeed, MaxSafeFallHeight: source.MaxSafeFallHeight,
	}
	var err error
	if target.DoubleJump, err = doubleJumpProfileFromProto(source.DoubleJump); err != nil {
		return platform.MovementProfile{}, err
	}
	if target.Dash, err = dashProfileFromProto(source.Dash); err != nil {
		return platform.MovementProfile{}, err
	}
	if source.WallJump != nil {
		maxConsecutive, err := profileUint8(source.WallJump.MaxConsecutive, "WallJump.MaxConsecutive")
		if err != nil {
			return platform.MovementProfile{}, err
		}
		target.WallJump = &platform.WallJumpProfile{ImpulseX: source.WallJump.ImpulseX, ImpulseY: source.WallJump.ImpulseY, SameWallReuse: source.WallJump.SameWallReuse, MaxConsecutive: maxConsecutive, Stamina: source.WallJump.Stamina, SlideSpeed: source.WallJump.SlideSpeed, InputLock: source.WallJump.InputLock, MinWallHeight: source.WallJump.MinWallHeight}
	}
	if source.Coyote != nil {
		target.Coyote = &platform.CoyoteProfile{Window: source.Coyote.Window, AppliesTo: platform.CoyoteSource(source.Coyote.AppliesTo)}
	}
	if source.VariableJump != nil {
		target.VariableJump = &platform.VariableJumpProfile{Mode: platform.VariableJumpMode(source.VariableJump.Mode), CutFactor: source.VariableJump.CutFactor, ReleaseGravity: source.VariableJump.ReleaseGravity, MinHoldTime: source.VariableJump.MinHoldTime}
	}
	if source.Climb != nil {
		target.Climb = &platform.ClimbProfile{Speed: source.Climb.Speed, CaptureHalfWidth: source.Climb.CaptureHalfWidth, CanJumpOff: source.Climb.CanJumpOff, ExitImpulseX: source.Climb.ExitImpulseX}
	}
	if err := target.Validate(); err != nil {
		return platform.MovementProfile{}, err
	}
	return target, nil
}

func doubleJumpProfileFromProto(source *daedalusv1.MovementProfile_DoubleJumpProfile) (*platform.DoubleJumpProfile, error) {
	if source == nil {
		return nil, nil
	}
	charges, err := profileUint8(source.Charges, "DoubleJump.Charges")
	if err != nil {
		return nil, err
	}
	return &platform.DoubleJumpProfile{Mode: platform.DoubleJumpMode(source.Mode), Velocity: source.Velocity, Charges: charges, RefillOn: platform.RefillCondition(source.RefillOn), MinDelay: source.MinDelay}, nil
}

func dashProfileFromProto(source *daedalusv1.MovementProfile_DashProfile) (*platform.DashProfile, error) {
	if source == nil {
		return nil, nil
	}
	charges, err := profileUint8(source.Charges, "Dash.Charges")
	if err != nil {
		return nil, err
	}
	if source.Cancel == 0 {
		return nil, fmt.Errorf("%w: Dash.Cancel must name at least one event", platform.ErrInvalidProfile)
	}
	return &platform.DashProfile{Speed: source.Speed, ShadowSpeed: source.ShadowSpeed, ShadowPassesHazard: source.ShadowPassesHazard, Duration: source.Duration, SuspendsGravity: source.SuspendsGravity, Exit: platform.DashExitMode(source.Exit), Charges: charges, Cooldown: source.Cooldown, RefillOn: platform.RefillCondition(source.RefillOn), Directions: platform.DashDirection(source.Directions), Cancel: platform.DashCancel(source.Cancel)}, nil
}

func profileUint8(value uint32, field string) (uint8, error) {
	if value > math.MaxUint8 {
		return 0, fmt.Errorf("%w: %s %d exceeds 255", platform.ErrInvalidProfile, field, value)
	}
	return uint8(value), nil
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
		transitions := make([]*daedalusv1.PlatformTransition, len(room.Transitions))
		for j, transition := range room.Transitions {
			entry := &daedalusv1.PlatformTransition{
				Id: uint32(transition.ID), Side: uint32(transition.Side), Index: transition.Index,
				Offset: transition.Offset, Extent: transition.Extent, To: uint32(transition.To),
			}
			if transition.Outbound != nil {
				entry.OutboundExists = true
				entry.OutboundRequires = uint32(transition.Outbound.Requires)
			}
			if transition.Inbound != nil {
				entry.InboundExists = true
				entry.InboundRequires = uint32(transition.Inbound.Requires)
			}
			transitions[j] = entry
		}
		result.Rooms[i] = &daedalusv1.PlatformRoom{Id: uint32(room.ID), Origin: &daedalusv1.Cell{X: room.Origin.X, Y: room.Origin.Y}, Grid: &daedalusv1.PlatformGrid{Width: room.Grid.Width, Height: room.Grid.Height, Cells: cells}, Transitions: transitions}
	}
	result.Spawn = platformAnchorToProto(source.Plane.Spawn)
	result.Goal = platformAnchorToProto(source.Plane.Goal)
	return result
}

// platformAnchorToProto carries spawn and goal across. They are room-local
// cells and the anchor names its room, so a reader adds that room's origin.
func platformAnchorToProto(anchor platform.Anchor) *daedalusv1.PlatformAnchor {
	return &daedalusv1.PlatformAnchor{Room: uint32(anchor.Room), X: anchor.At.X, Y: anchor.At.Y}
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
		// The node's coordinates are in its room's frame. Carry the room so a
		// reader can add the origin; see PlatformMotionNode.room.
		var room uint32
		if int(node.Surface) < len(graph.Surfaces) {
			room = uint32(graph.Surfaces[node.Surface].Room)
		}
		result.Nodes[i] = &daedalusv1.PlatformMotionNode{Id: uint32(node.ID), Surface: uint32(node.Surface), Interval: node.Interval, Height: node.Height, FootingLo: node.Footing.Lo, FootingHi: node.Footing.Hi, VelocityLo: node.Velocity.Lo, VelocityHi: node.Velocity.Hi, Mode: uint32(node.Mode), Room: room}
	}
	for i, edge := range graph.Edges {
		result.Edges[i] = &daedalusv1.PlatformMotionEdge{Id: uint32(edge.ID), From: uint32(edge.From), To: uint32(edge.To), Kind: uint32(edge.Kind), Requires: uint32(edge.Requires), Passage: uint32(edge.Passage), Transition: uint32(edge.Transition), Duration: edge.Duration}
	}
	return result
}
