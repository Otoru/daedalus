package platform

import (
	"context"
	"math"

	"github.com/Otoru/daedalus/core"
)

// Salts split the macro generator's streams. core owns the arithmetic; the
// catalogue of streams belongs to the caller, which here is this generator.
// saltRooms derives the room-placement stream from the config seed.
const saltRooms uint64 = 0x6D6163726F5F726D // "macro_rm"

// GateBandCells is the height quantum of a vertical gate, in cells.
//
// The reference game does not publish where on a wall an opening sits. This
// package decides: openings are aligned to bands of GateBandCells, measured
// from the bottom of the overlap of the two rooms. Band 0 is the floor of
// that overlap. The next opening on the same side, when the overlap is tall
// enough, sits in the highest band that still holds a landing and a clearance,
// so leaving through the high gate is a different event from leaving along
// the floor. Offset remains the height of the air gap's bottom above the
// room's own bottom edge, which is what Transition records; rooms whose
// bottoms do not line up therefore carry different Offsets for one shared gap.
const GateBandCells = 4

// OpeningExtent is the clear size of a generated opening, in cells. It is
// larger than the default body's height, so a character can walk a vertical
// gate and fall through a horizontal one.
const OpeningExtent = 3

// minOverlapCells is the smallest shared run, in cells, that can host a floor
// gate and a high gate in different bands: one landing plus two bands.
const minOverlapCells = GateBandCells * 2

// openingLandingCells is the solid cell a generated opening keeps under its
// air. floorSpan starts that air at one cell above the bottom of the overlap.
const openingLandingCells uint32 = 1

// decentMapRooms is the shortest spine this generator will fold. placeRooms
// raises a shorter chain to this, and a map of two rooms is what a plane too
// small for that fold used to return without saying why.
const decentMapRooms = 4

// MacroConfig is the input to the macro generator. Zero-value sizes request
// the defaults applied by Normalize. Seed, Width, Height and Rooms are required.
type MacroConfig struct {
	// Seed feeds the room-placement stream. Every uint64 is accepted.
	Seed Seed
	// Width and Height are the plane dimensions in cells.
	Width  uint32
	Height uint32
	// Rooms is how many rooms to place. They are not a uniform grid.
	Rooms uint32
	// MinWidth, MaxWidth, MinHeight and MaxHeight bound each room's grid.
	// Zero requests the default. A room is never a single screen-cell.
	MinWidth  uint32
	MaxWidth  uint32
	MinHeight uint32
	MaxHeight uint32
	// Steps is the grant order. Empty builds a map with no ability gates.
	// Each step should grant one ability; a step that grants several gates
	// one edge on the whole set.
	Steps []ProgressionStep
}

// Macro is a generated plane plus the progression bound onto its directed
// room graph. The graph has one rest node per room, in RoomID order, and one
// directed edge per traversable sense of a transition. It is the graph the
// audits walk. It is not an oracle certificate: edges carry no witness.
type Macro struct {
	Plane   Plane
	Plan    ProgressionPlan
	Grants  []AbilityGrant
	Locks   []RegionLock
	Graph   JumpGraph
	Profile MovementProfile
}

// canonicalBeatCells is the width of the widest canonical beat: two platforms
// of the widest canonical span with the gap between them. Traverse is that
// span today (12), so the beat is 12+2+12 = 26.
func canonicalBeatCells() uint32 {
	var widest uint32
	for kind := BeatKindRest; kind <= BeatKindCheckpoint; kind++ {
		_, span := canonicalCells(kind)
		if span > widest {
			widest = span
		}
	}
	return widest + platformGap + widest
}

// defaultMinRoomSide fits one canonical beat plus the opening's clearance and
// the landing under it: 26 + OpeningExtent + openingLandingCells = 30.
func defaultMinRoomSide() uint32 {
	return canonicalBeatCells() + OpeningExtent + openingLandingCells
}

// defaultMaxRoomSide is the same beat plus the overlap that hosts both a
// floor opening and a high opening: 26 + minOverlapCells = 34. The four cells
// between 30 and 34 are one gate band, so rooms stay unequal without a second
// guess at the mean degree.
func defaultMaxRoomSide() uint32 {
	return canonicalBeatCells() + minOverlapCells
}

// Normalize fills the size defaults. It does not validate.
func (c MacroConfig) Normalize() MacroConfig {
	out := c
	if out.MinWidth == 0 {
		out.MinWidth = defaultMinRoomSide()
	}
	if out.MaxWidth == 0 {
		out.MaxWidth = defaultMaxRoomSide()
	}
	if out.MinHeight == 0 {
		out.MinHeight = defaultMinRoomSide()
	}
	if out.MaxHeight == 0 {
		out.MaxHeight = defaultMaxRoomSide()
	}
	return out
}

// Validate checks the config before any stream is consumed.
func (c MacroConfig) Validate() error {
	cfg := c.Normalize()
	if cfg.Width == 0 || cfg.Height == 0 {
		return configError("plane dimensions %dx%d include a zero", cfg.Width, cfg.Height)
	}
	if cfg.Width > math.MaxInt32 || cfg.Height > math.MaxInt32 {
		return limitError("plane dimensions %dx%d exceed the coordinate ceiling", cfg.Width, cfg.Height)
	}
	if cfg.Rooms == 0 {
		return configError("room count is zero")
	}
	if cfg.Rooms > MaxRooms {
		return limitError("room count %d is above the ceiling of %d", cfg.Rooms, MaxRooms)
	}
	if cfg.MinWidth > cfg.MaxWidth || cfg.MinHeight > cfg.MaxHeight {
		return configError("room size range %dx%d..%dx%d is inverted", cfg.MinWidth, cfg.MinHeight, cfg.MaxWidth, cfg.MaxHeight)
	}
	if cfg.MinWidth < OpeningExtent+1 || cfg.MinHeight < OpeningExtent+1 {
		return configError("room size minimum %dx%d cannot hold an opening of %d", cfg.MinWidth, cfg.MinHeight, OpeningExtent)
	}
	if cfg.MaxWidth > MaxRoomSide || cfg.MaxHeight > MaxRoomSide {
		return limitError("room size maximum %dx%d exceeds the per-axis ceiling of %d", cfg.MaxWidth, cfg.MaxHeight, MaxRoomSide)
	}
	if uint64(cfg.MaxWidth)*uint64(cfg.MaxHeight) > MaxRoomCells {
		return limitError("room size maximum %dx%d exceeds the cell ceiling of %d", cfg.MaxWidth, cfg.MaxHeight, MaxRoomCells)
	}
	if cfg.Width < cfg.MaxWidth || cfg.Height < cfg.MaxHeight {
		return configError("plane %dx%d cannot hold a room of %dx%d", cfg.Width, cfg.Height, cfg.MaxWidth, cfg.MaxHeight)
	}
	if err := decentPlane(cfg); err != nil {
		return err
	}
	plan := ProgressionPlan{Steps: cfg.Steps}
	if err := plan.Validate(DefaultProfile()); err != nil {
		return err
	}
	return nil
}

// decentPlane rejects a plane that cannot tile a folded map. The count is how
// many rooms of the maximum side fit on a grid: width/max by height/max.
// Fewer than decentMapRooms is the two-or-three-room map a small plane used
// to return. The minimum side in the message is the beat arithmetic, so the
// rejection names both numbers that set the floor.
func decentPlane(cfg MacroConfig) error {
	cols := uint64(cfg.Width) / uint64(cfg.MaxWidth)
	rows := uint64(cfg.Height) / uint64(cfg.MaxHeight)
	slots := cols * rows
	if slots >= uint64(decentMapRooms) {
		return nil
	}
	beat := canonicalBeatCells()
	return configError(
		"plane %dx%d tiles %d rooms of %dx%d (%d/%d × %d/%d), below %d; a decent map folds %d rooms and the minimum side %d is a canonical beat of %d plus an opening of %d and a landing of %d",
		cfg.Width, cfg.Height, slots, cfg.MaxWidth, cfg.MaxHeight,
		cfg.Width, cfg.MaxWidth, cfg.Height, cfg.MaxHeight,
		decentMapRooms, decentMapRooms, cfg.MinWidth, beat, OpeningExtent, openingLandingCells,
	)
}

// GenerateMacro places unequal rooms, wires cardinal transitions and binds
// the progression. It fails closed: a map that misses its shape or fails an
// audit is not returned.
func GenerateMacro(ctx context.Context, cfg MacroConfig) (Macro, error) {
	var zero Macro
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := cfg.Validate(); err != nil {
		return zero, err
	}
	cfg = cfg.Normalize()
	plane, parents, err := placeRooms(ctx, cfg)
	if err != nil {
		return zero, err
	}
	plane, asm, err := wireTransitions(ctx, plane, parents, cfg.Steps)
	if err != nil {
		return zero, err
	}
	macro, err := bindProgression(ctx, plane, asm, cfg.Steps, DefaultProfile())
	if err != nil {
		return zero, err
	}
	if err := ValidatePlane(macro.Plane); err != nil {
		return zero, err
	}
	audit, err := AuditMacro(ctx, macro)
	if err != nil {
		return zero, err
	}
	if !audit.Route.Exists {
		return zero, progressionError("generated map has no route across the stage objectives")
	}
	if !audit.Grants.Obtainable {
		return zero, progressionError("generated map grants an ability behind the gate that requires it")
	}
	if !audit.Locks.Held {
		return zero, progressionError("generated map leaks a locked region")
	}
	if !audit.Softlocks.Clear {
		return zero, progressionError("generated map contains a softlock")
	}
	return macro, nil
}

type roomDraft struct {
	origin Cell
	w, h   uint32
	parent int
}

func placeRooms(ctx context.Context, cfg MacroConfig) (Plane, []int, error) {
	var zero Plane
	n := int(cfg.Rooms)
	rng := core.NewSplitMix64(cfg.Seed, saltRooms)
	drafts := make([]roomDraft, 0, n)
	rootW, rootH := drawSize(&rng, cfg)
	drafts = append(drafts, roomDraft{
		origin: Cell{
			X: int32(cfg.Width-rootW) / 2,
			Y: int32(cfg.Height-rootH) / 2,
		},
		w: rootW, h: rootH, parent: -1,
	})
	// The first steps+1 attachments extend the previous room, so the tree
	// contains a path long enough to host one gate per step. Later rooms
	// prefer a fold (a side that turns) and fall back to any parent that fits.
	chain := len(cfg.Steps) + 1
	if chain < 4 {
		chain = 4
	}
	if chain > n {
		chain = n
	}
	for i := 1; i < n; i++ {
		if err := ctx.Err(); err != nil {
			return zero, nil, err
		}
		var (
			placed roomDraft
			ok     bool
		)
		if i < chain {
			placed, ok = attachTo(&rng, drafts, i-1, cfg, true)
			if !ok {
				return zero, nil, geometryError("could not extend the spine at room %d inside %dx%d", i, cfg.Width, cfg.Height)
			}
		} else {
			placed, ok = attachAnywhere(&rng, drafts, cfg)
			if !ok {
				return zero, nil, geometryError("could not place room %d without overlap inside %dx%d", i, cfg.Width, cfg.Height)
			}
		}
		drafts = append(drafts, placed)
	}
	plane := Plane{
		Width:  cfg.Width,
		Height: cfg.Height,
		Rooms:  make([]Room, n),
	}
	parents := make([]int, n)
	for i, draft := range drafts {
		plane.Rooms[i] = Room{
			ID:     RoomID(i),
			Origin: draft.origin,
			Grid:   shellGrid(draft.w, draft.h),
		}
		parents[i] = draft.parent
	}
	plane.Spawn = Anchor{Room: 0, At: standingCell(plane.Rooms[0])}
	plane.Goal = plane.Spawn
	return plane, parents, nil
}

func drawSize(rng *core.SplitMix64, cfg MacroConfig) (uint32, uint32) {
	w := uint32(rng.UniformInt(uint64(cfg.MinWidth), uint64(cfg.MaxWidth)))
	h := uint32(rng.UniformInt(uint64(cfg.MinHeight), uint64(cfg.MaxHeight)))
	return w, h
}

func attachTo(rng *core.SplitMix64, drafts []roomDraft, parent int, cfg MacroConfig, extend bool) (roomDraft, bool) {
	order := shuffledSides(rng)
	if extend {
		// Prefer a clockwise turn so a chain folds through the plane instead
		// of running off one edge. The previous attachment's side is not
		// stored; the shuffle plus the fit check is what keeps it inside.
		order = preferTurn(drafts, parent, order)
	}
	sizes := [][2]uint32{drawSizePair(rng, cfg), {cfg.MinWidth, cfg.MinHeight}, {cfg.MaxWidth, cfg.MinHeight}, {cfg.MinWidth, cfg.MaxHeight}}
	for _, size := range sizes {
		if !legalSize(size[0], size[1]) {
			continue
		}
		for _, side := range order {
			if draft, ok := fitSide(rng, drafts, parent, side, size[0], size[1], cfg); ok {
				return draft, true
			}
		}
	}
	return roomDraft{}, false
}

func drawSizePair(rng *core.SplitMix64, cfg MacroConfig) [2]uint32 {
	w, h := drawSize(rng, cfg)
	return [2]uint32{w, h}
}

func attachAnywhere(rng *core.SplitMix64, drafts []roomDraft, cfg MacroConfig) (roomDraft, bool) {
	// Newest parents first: a branch grows from the frontier, which is where
	// the plane still has space.
	for parent := len(drafts) - 1; parent >= 0; parent-- {
		if draft, ok := attachTo(rng, drafts, parent, cfg, false); ok {
			return draft, true
		}
		if draft, ok := fitSide(rng, drafts, parent, TransitionSideRight, cfg.MinWidth, cfg.MinHeight, cfg); ok {
			return draft, true
		}
		for _, side := range []TransitionSide{TransitionSideBottom, TransitionSideLeft, TransitionSideTop} {
			if draft, ok := fitSide(rng, drafts, parent, side, cfg.MinWidth, cfg.MinHeight, cfg); ok {
				return draft, true
			}
		}
	}
	return roomDraft{}, false
}

func preferTurn(drafts []roomDraft, parent int, order []TransitionSide) []TransitionSide {
	if parent < 0 || drafts[parent].parent < 0 {
		return order
	}
	came := approachSide(drafts[drafts[parent].parent], drafts[parent])
	turned := clockwise(came)
	out := make([]TransitionSide, 0, len(order))
	out = append(out, turned)
	for _, side := range order {
		if side != turned {
			out = append(out, side)
		}
	}
	return out
}

func approachSide(parent, child roomDraft) TransitionSide {
	if child.origin.X >= parent.origin.X+int32(parent.w) {
		return TransitionSideRight
	}
	if child.origin.X+int32(child.w) <= parent.origin.X {
		return TransitionSideLeft
	}
	if child.origin.Y >= parent.origin.Y+int32(parent.h) {
		return TransitionSideBottom
	}
	return TransitionSideTop
}

func clockwise(side TransitionSide) TransitionSide {
	switch side {
	case TransitionSideRight:
		return TransitionSideBottom
	case TransitionSideBottom:
		return TransitionSideLeft
	case TransitionSideLeft:
		return TransitionSideTop
	default:
		return TransitionSideRight
	}
}

func shuffledSides(rng *core.SplitMix64) []TransitionSide {
	sides := []TransitionSide{TransitionSideRight, TransitionSideBottom, TransitionSideLeft, TransitionSideTop}
	for i := len(sides) - 1; i > 0; i-- {
		j := int(rng.UniformInt(0, uint64(i)))
		sides[i], sides[j] = sides[j], sides[i]
	}
	return sides
}

func legalSize(w, h uint32) bool {
	if w == 0 || h == 0 || w > MaxRoomSide || h > MaxRoomSide {
		return false
	}
	return uint64(w)*uint64(h) <= MaxRoomCells
}

func fitSide(rng *core.SplitMix64, drafts []roomDraft, parent int, side TransitionSide, w, h uint32, cfg MacroConfig) (roomDraft, bool) {
	p := drafts[parent]
	lo, hi, _, ok := slideRange(p, side, w, h)
	if !ok {
		return roomDraft{}, false
	}
	lo, hi, ok = clampSlide(lo, hi, side, w, h, cfg)
	if !ok {
		return roomDraft{}, false
	}
	candidates := []int32{lo, (lo + hi) / 2, hi}
	if hi > lo {
		candidates = append(candidates, int32(rng.UniformInt(uint64(lo), uint64(hi))))
	}
	seen := map[int32]bool{}
	for _, slide := range candidates {
		if seen[slide] {
			continue
		}
		seen[slide] = true
		origin := originOnSide(p, side, w, h, slide)
		if !insidePlane(origin, w, h, cfg) {
			continue
		}
		if overlapsAny(drafts, origin, w, h) {
			continue
		}
		if sharedRun(p, side, origin, w, h) < minOverlapCells {
			continue
		}
		return roomDraft{origin: origin, w: w, h: h, parent: parent}, true
	}
	return roomDraft{}, false
}

func slideRange(p roomDraft, side TransitionSide, w, h uint32) (lo, hi int32, alongX bool, ok bool) {
	child := int32(h)
	parentSpan := int32(p.h)
	origin := p.origin.Y
	if side == TransitionSideTop || side == TransitionSideBottom {
		child = int32(w)
		parentSpan = int32(p.w)
		origin = p.origin.X
		alongX = true
	}
	lo = origin + minOverlapCells - child
	hi = origin + parentSpan - minOverlapCells
	return lo, hi, alongX, lo <= hi
}

func clampSlide(lo, hi int32, side TransitionSide, w, h uint32, cfg MacroConfig) (int32, int32, bool) {
	span := int32(h)
	limit := int32(cfg.Height)
	if side == TransitionSideTop || side == TransitionSideBottom {
		span = int32(w)
		limit = int32(cfg.Width)
	}
	if lo < 0 {
		lo = 0
	}
	if hi > limit-span {
		hi = limit - span
	}
	return lo, hi, lo <= hi
}

func originOnSide(p roomDraft, side TransitionSide, w, h uint32, slide int32) Cell {
	switch side {
	case TransitionSideRight:
		return Cell{X: p.origin.X + int32(p.w), Y: slide}
	case TransitionSideLeft:
		return Cell{X: p.origin.X - int32(w), Y: slide}
	case TransitionSideBottom:
		return Cell{X: slide, Y: p.origin.Y + int32(p.h)}
	default:
		return Cell{X: slide, Y: p.origin.Y - int32(h)}
	}
}

func insidePlane(origin Cell, w, h uint32, cfg MacroConfig) bool {
	if origin.X < 0 || origin.Y < 0 {
		return false
	}
	return uint32(origin.X)+w <= cfg.Width && uint32(origin.Y)+h <= cfg.Height
}

func overlapsAny(drafts []roomDraft, origin Cell, w, h uint32) bool {
	for _, other := range drafts {
		if rectsOverlap(origin.X, origin.Y, int32(w), int32(h), other.origin.X, other.origin.Y, int32(other.w), int32(other.h)) {
			return true
		}
	}
	return false
}

func rectsOverlap(ax, ay, aw, ah, bx, by, bw, bh int32) bool {
	return ax < bx+bw && bx < ax+aw && ay < by+bh && by < ay+ah
}

func sharedRun(p roomDraft, side TransitionSide, origin Cell, w, h uint32) int32 {
	if side == TransitionSideLeft || side == TransitionSideRight {
		y0 := max(p.origin.Y, origin.Y)
		y1 := min(p.origin.Y+int32(p.h), origin.Y+int32(h))
		return y1 - y0
	}
	x0 := max(p.origin.X, origin.X)
	x1 := min(p.origin.X+int32(p.w), origin.X+int32(w))
	return x1 - x0
}

func shellGrid(w, h uint32) Grid {
	cells := make([]CellKind, int(w)*int(h))
	for y := uint32(0); y < h; y++ {
		for x := uint32(0); x < w; x++ {
			border := x == 0 || y == 0 || x+1 == w || y+1 == h
			kind := CellKindEmpty
			if border {
				kind = CellKindSolid
			}
			cells[int(y)*int(w)+int(x)] = kind
		}
	}
	return Grid{Width: w, Height: h, Cells: cells}
}

func standingCell(room Room) Cell {
	x := int32(room.Grid.Width / 2)
	y := int32(room.Grid.Height) - 2
	if y < 0 {
		y = 0
	}
	if x >= int32(room.Grid.Width) {
		x = int32(room.Grid.Width) - 1
	}
	return Cell{X: x, Y: y}
}

func setCell(grid *Grid, x, y int32, kind CellKind) {
	if x < 0 || y < 0 || uint32(x) >= grid.Width || uint32(y) >= grid.Height {
		return
	}
	grid.Cells[int(y)*int(grid.Width)+int(x)] = kind
}

// ValidatePlane checks that a plane describes space: rooms inside the plane,
// no overlap, grids that validate, spawn and goal inside their rooms, and
// transitions that pair, agree with their side, and meet in the plane.
func ValidatePlane(plane Plane) error {
	if plane.Width == 0 || plane.Height == 0 {
		return geometryError("plane dimensions %dx%d include a zero", plane.Width, plane.Height)
	}
	if len(plane.Rooms) == 0 {
		return geometryError("plane has no rooms")
	}
	if len(plane.Rooms) > MaxRooms {
		return limitError("plane has %d rooms, above the ceiling of %d", len(plane.Rooms), MaxRooms)
	}
	for i := range plane.Rooms {
		room := plane.Rooms[i]
		if room.ID != RoomID(i) {
			return geometryError("room %d has id %d; ids are creation indices", i, room.ID)
		}
		if err := validateGrid(room.Grid); err != nil {
			return err
		}
		if room.Origin.X < 0 || room.Origin.Y < 0 {
			return geometryError("room %d origin %v is outside the plane", room.ID, room.Origin)
		}
		if uint32(room.Origin.X)+room.Grid.Width > plane.Width || uint32(room.Origin.Y)+room.Grid.Height > plane.Height {
			return geometryError("room %d extends outside the plane", room.ID)
		}
		for j := 0; j < i; j++ {
			other := plane.Rooms[j]
			if rectsOverlap(room.Origin.X, room.Origin.Y, int32(room.Grid.Width), int32(room.Grid.Height), other.Origin.X, other.Origin.Y, int32(other.Grid.Width), int32(other.Grid.Height)) {
				return geometryError("room %d overlaps room %d", room.ID, other.ID)
			}
		}
	}
	if err := anchorInside(plane, plane.Spawn, "spawn"); err != nil {
		return err
	}
	if err := anchorInside(plane, plane.Goal, "goal"); err != nil {
		return err
	}
	return validateTransitions(plane)
}

func anchorInside(plane Plane, anchor Anchor, name string) error {
	if int(anchor.Room) >= len(plane.Rooms) {
		return geometryError("%s names room %d, which is not in the plane", name, anchor.Room)
	}
	if _, ok := plane.Rooms[anchor.Room].Grid.At(anchor.At); !ok {
		return geometryError("%s cell %v is outside room %d", name, anchor.At, anchor.Room)
	}
	return nil
}
