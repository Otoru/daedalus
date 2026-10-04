package platform

import (
	"bytes"
	"context"
	"testing"
)

func TestSpineRisesAndFalls(t *testing.T) {
	spine, err := GenerateSpine(context.Background(), rhythmConfig(), 0)
	if err != nil {
		t.Fatalf("GenerateSpine: %v", err)
	}
	var up, down bool
	var upDelta, downDelta int32
	var maxColumn, lastMain int32
	var sawMain bool
	for _, node := range spine.Nodes {
		if node.Role != SpineRoleMain {
			continue
		}
		if !sawMain || node.Column > maxColumn {
			maxColumn = node.Column
		}
		lastMain = node.Column
		sawMain = true
	}
	up, upDelta, down, downDelta = checkMainSpineEdges(t, spine, up, upDelta, down, downDelta)
	if !up || !down {
		t.Fatal("spine only advanced sideways")
	}
	if downDelta <= upDelta {
		t.Fatalf("descent of %d should outrun ascent of %d: falling is free, climbing costs", downDelta, upDelta)
	}
	if !sawMain || lastMain >= maxColumn {
		t.Fatalf("main path never folded: last column %d, furthest column %d", lastMain, maxColumn)
	}
}

func checkMainSpineEdges(t *testing.T, spine Spine, up bool, upDelta int32, down bool, downDelta int32) (bool, int32, bool, int32) {
	for _, edge := range spine.Edges {
		if edge.Direction == SpineDirectionBranch || edge.Direction == SpineDirectionSecret {
			continue
		}
		from, ok := spine.Node(edge.From)
		to, okTo := spine.Node(edge.To)
		if !ok || !okTo {
			t.Fatalf("edge %d -> %d leaves the spine", edge.From, edge.To)
		}
		delta := to.Altitude - from.Altitude
		checkMainEdgeDirection(t, edge, delta)
		switch edge.Direction {
		case SpineDirectionUp:
			up = true
			upDelta = delta
		case SpineDirectionDown:
			down = true
			downDelta = -delta
		}
	}
	return up, upDelta, down, downDelta
}

func checkMainEdgeDirection(t *testing.T, edge SpineEdge, delta int32) {
	t.Helper()
	switch edge.Direction {
	case SpineDirectionUp:
		if delta <= 0 {
			t.Fatalf("up edge %d -> %d changes altitude by %d", edge.From, edge.To, delta)
		}
	case SpineDirectionDown:
		if delta >= 0 {
			t.Fatalf("down edge %d -> %d changes altitude by %d", edge.From, edge.To, delta)
		}
	case SpineDirectionLateral:
		if delta != 0 {
			t.Fatalf("lateral edge %d -> %d changes altitude by %d", edge.From, edge.To, delta)
		}
	default:
		t.Fatalf("main path has direction %s", edge.Direction)
	}
}

func TestSpineBranchesIntoASecret(t *testing.T) {
	spine, err := GenerateSpine(context.Background(), rhythmConfig(), 0)
	if err != nil {
		t.Fatalf("GenerateSpine: %v", err)
	}
	var branch, secret bool
	branch, secret = checkSpineBranchEdges(t, spine, branch, secret)
	if !branch || !secret {
		t.Fatalf("branch=%v secret=%v, want both a side path and a secret deviation", branch, secret)
	}
}

func checkSpineBranchEdges(t *testing.T, spine Spine, branch bool, secret bool) (bool, bool) {
	for _, edge := range spine.Edges {
		from, ok := spine.Node(edge.From)
		to, okTo := spine.Node(edge.To)
		if !ok || !okTo {
			t.Fatalf("edge %d -> %d leaves the spine", edge.From, edge.To)
		}
		switch edge.Direction {
		case SpineDirectionBranch:
			branch = true
			checkBranchEdge(t, edge, from, to)
		case SpineDirectionSecret:
			secret = true
			checkSecretEdge(t, edge, from, to)
		}
	}
	return branch, secret
}

func checkBranchEdge(t *testing.T, edge SpineEdge, from, to SpineNode) {
	t.Helper()
	if from.Role != SpineRoleMain || to.Role != SpineRoleBranch {
		t.Fatalf("branch %d -> %d joins %s to %s", edge.From, edge.To, from.Role, to.Role)
	}
	if to.Column == from.Column && to.Altitude == from.Altitude {
		t.Fatal("branch did not leave its parent")
	}
}

func checkSecretEdge(t *testing.T, edge SpineEdge, from, to SpineNode) {
	t.Helper()
	if from.Role != SpineRoleMain || to.Role != SpineRoleSecret {
		t.Fatalf("secret %d -> %d joins %s to %s", edge.From, edge.To, from.Role, to.Role)
	}
	if to.Kind != BeatKindSecret {
		t.Fatalf("secret node kind = %s", to.Kind)
	}
	if to.Altitude == from.Altitude && to.Column == from.Column {
		t.Fatal("secret did not deviate from the main path")
	}
	if to.Altitude >= from.Altitude {
		t.Fatalf("secret altitude %d is not below its parent %d: a hidden drop is the free direction", to.Altitude, from.Altitude)
	}
}

func TestSameSeedBuildsTheSameSpine(t *testing.T) {
	config := rhythmConfig()
	first, err := GenerateSpine(context.Background(), config, 0)
	if err != nil {
		t.Fatalf("first GenerateSpine: %v", err)
	}
	second, err := GenerateSpine(context.Background(), config, 0)
	if err != nil {
		t.Fatalf("second GenerateSpine: %v", err)
	}
	if !bytes.Equal(first.Canonical(), second.Canonical()) {
		t.Fatal("the same seed produced two different spines")
	}
	config.Seed++
	third, err := GenerateSpine(context.Background(), config, 0)
	if err != nil {
		t.Fatalf("third GenerateSpine: %v", err)
	}
	if bytes.Equal(first.Canonical(), third.Canonical()) {
		t.Fatal("a different seed produced the same spine")
	}
}

func TestRhythmSaltsAreNotDungeonSalts(t *testing.T) {
	dungeon := []uint64{
		0xA0B1C2D3E4F56789,
		0x1F2E3D4C5B6A7988,
		0x9E3779B97F4A7C15,
		0x6C8E9CF570932BD5,
		0xD1B54A32D192ED03,
		0xC3D4E5F60718293A,
		0x7A6B5C4D3E2F1A09,
	}
	salts := []uint64{spineShapeSalt, beatChoiceSalt, geometrySpanSalt}
	seen := map[uint64]bool{}
	for _, salt := range salts {
		if seen[salt] {
			t.Fatalf("salt %x is used twice", salt)
		}
		seen[salt] = true
		for _, used := range dungeon {
			if salt == used {
				t.Fatalf("salt %x reuses a dungeon stream", salt)
			}
		}
	}
}

func TestDifficultyScalesWeightTowardTheCurve(t *testing.T) {
	if paceTarget(0, 5) != 0 || paceTarget(4, 5) != 0 {
		t.Fatalf("curve ends = %d and %d, want rest", paceTarget(0, 5), paceTarget(4, 5))
	}
	if paceTarget(3, 5) != 255 {
		t.Fatalf("penultimate target = %d, want 255", paceTarget(3, 5))
	}
	if paceTarget(1, 5) >= paceTarget(2, 5) {
		t.Fatal("the curve should climb through the approach")
	}
	onCurve := pacedWeight(10, 0, 0)
	offCurve := pacedWeight(10, 255, 0)
	if onCurve <= offCurve {
		t.Fatalf("weight on the curve = %d, off the curve = %d", onCurve, offCurve)
	}
}

func TestRealizationCertifiesClimbsAndFalls(t *testing.T) {
	rhythm, err := GenerateRhythm(context.Background(), &FakeOracle{}, rhythmConfig(), 0)
	if err != nil {
		t.Fatalf("GenerateRhythm: %v", err)
	}
	var jump, fall bool
	jump, fall = checkRealizedBeats(t, rhythm, jump, fall)
	if !jump || !fall {
		t.Fatalf("jump=%v fall=%v, want the oracle to certify both a climb and a drop", jump, fall)
	}
}

func checkRealizedBeats(t *testing.T, rhythm Rhythm, jump bool, fall bool) (bool, bool) {
	for _, beat := range rhythm.Beats {
		checkRealizedBeat(t, beat)
		if beat.Direction == SpineDirectionUp {
			jump = true
		}
		if beat.Arrival.Height < beat.Departure.Height {
			fall = true
		}
	}
	return jump, fall
}

func checkRealizedBeat(t *testing.T, beat RealizedBeat) {
	t.Helper()
	if !beat.Judgement.Certified() {
		t.Fatalf("beat %d -> %d (%s) judgement = %+v", beat.From, beat.To, beat.Direction, beat.Judgement)
	}
	if !beat.Departure.IsRest(rhythmConfig().Profile) {
		t.Fatal("departure is not a rest state")
	}
	if beat.Direction == SpineDirectionUp {
		if beat.Arrival.Height <= beat.Departure.Height {
			t.Fatalf("up beat %d -> %d did not gain height", beat.From, beat.To)
		}
		if beat.Maneuver.Kind != MotionEdgeKindJump {
			t.Fatalf("up beat realized as %s", beat.Maneuver.Kind)
		}
	}
	if beat.Arrival.Height < beat.Departure.Height {
		if beat.Maneuver.Kind != MotionEdgeKindFall {
			t.Fatalf("drop realized as %s", beat.Maneuver.Kind)
		}
	}
}

func TestRejectedClimbIsRewritten(t *testing.T) {
	var climbs int
	oracle := &FakeOracle{
		CheckEdgeFunc: func(ctx context.Context, query EdgeQuery) (EdgeResult, error) {
			if query.To.Height > query.From.Height {
				climbs++
				return EdgeResult{Judgement: Judgement{
					Verdict:        VerdictRejected,
					Reason:         ReasonOutOfEnvelope,
					Model:          ModelFake,
					ProfileVersion: query.Profile.Version,
					Detail:         "test: climb refused",
				}}, nil
			}
			return (&FakeOracle{}).CheckEdge(ctx, query)
		},
	}
	rhythm, err := GenerateRhythm(context.Background(), oracle, rhythmConfig(), 0)
	if err != nil {
		t.Fatalf("GenerateRhythm: %v", err)
	}
	if climbs == 0 {
		t.Fatal("the oracle was never asked to refuse a climb")
	}
	for _, beat := range rhythm.Beats {
		if beat.Arrival.Height > beat.Departure.Height && !beat.Judgement.Certified() {
			t.Fatalf("a climb the oracle rejected was kept: proposed %s direction %s reaction %s verdict %s",
				beat.Proposed, beat.Direction, beat.Reaction, beat.Judgement.Verdict)
		}
	}
	var rewritten bool
	rewritten = checkRewrittenClimbs(t, rhythm, rewritten)
	if !rewritten {
		t.Fatal("the spine did not rewrite a refused climb")
	}
}

func checkRewrittenClimbs(t *testing.T, rhythm Rhythm, rewritten bool) bool {
	for _, beat := range rhythm.Beats {
		if beat.Reaction == BeatReactionRewritten && beat.Proposed == SpineDirectionUp && beat.Direction != SpineDirectionUp {
			rewritten = true
			if beat.ToAltitude != beat.FromAltitude && beat.Direction == SpineDirectionLateral {
				t.Fatalf("flat rewrite still changes altitude %d -> %d", beat.FromAltitude, beat.ToAltitude)
			}
		}
	}
	return rewritten
}

func rhythmConfig() Config {
	return Config{
		Seed:    42,
		Width:   64,
		Height:  32,
		Profile: DefaultProfile(),
		Beats: BeatConfig{
			Definitions: []BeatDefinition{
				{Kind: BeatKindRest, Difficulty: 0, MinCells: 4, MaxCells: 6},
				{Kind: BeatKindTraverse, Difficulty: 20, MinCells: 4, MaxCells: 8},
				{Kind: BeatKindClimb, Difficulty: 200, MinCells: 3, MaxCells: 5},
				{Kind: BeatKindDescend, Difficulty: 80, MinCells: 3, MaxCells: 5},
				{Kind: BeatKindGap, Difficulty: 120, MinCells: 3, MaxCells: 6},
				{Kind: BeatKindSecret, Difficulty: 40, MinCells: 3, MaxCells: 5},
				{Kind: BeatKindCheckpoint, Difficulty: 0, MinCells: 4, MaxCells: 6},
			},
			Spine: &BeatDistribution{
				Beats: []BeatWeight{
					{Kind: BeatKindRest, Weight: 1},
					{Kind: BeatKindTraverse, Weight: 2},
					{Kind: BeatKindClimb, Weight: 4},
					{Kind: BeatKindDescend, Weight: 4},
					{Kind: BeatKindGap, Weight: 2},
					{Kind: BeatKindCheckpoint, Weight: 1},
				},
				MinRunBeats: 5,
				MaxRunBeats: 5,
			},
			Branches: &BeatDistribution{
				Beats: []BeatWeight{
					{Kind: BeatKindSecret, Weight: 1},
					{Kind: BeatKindRest, Weight: 1},
					{Kind: BeatKindGap, Weight: 1},
				},
				MinRunBeats: 1,
				MaxRunBeats: 1,
			},
		},
	}
}
