package platform

import (
	"math"
	"strconv"
	"strings"

	"github.com/Otoru/daedalus/core"
)

// Cell is an integer (X, Y) grid coordinate, never a pixel and never a world
// position. It is core.Cell, not a convertible copy of it: the grid frame is
// shared with every other Daedalus generator. Y grows downward and the
// canonical cell order is Y then X.
type Cell = core.Cell

// Seed is the source of a generation request's deterministic random streams.
// It is core.Seed. This package consumes none; the type appears in Config so
// that a platform request is reproducible the same way a dungeon request is.
type Seed = core.Seed

// Direction is one of the four cardinal grid directions, in the core
// convention: North points toward decreasing Y, which in the grid frame is
// upward on screen.
type Direction = core.Direction

// Span is a closed interval of world coordinates, in cells or in cells per
// second depending on what holds it. Lo greater than Hi is the empty span; a
// span containing a NaN bound is also empty, so every predicate below fails
// closed rather than silently accepting a corrupt value.
//
// A Span is a SET of states, and the quantifier matters. Where a Span bounds
// the states a MotionNode stands for, every claim attached to that node must
// hold for EVERY value in the span, not for some value in it. An abstraction
// that only promises "some velocity in this band works" is optimistic, and an
// optimistic node is how a jump graph comes to certify a route a player cannot
// walk.
type Span struct {
	// Lo is the inclusive lower bound.
	Lo float64
	// Hi is the inclusive upper bound.
	Hi float64
}

// Point returns the degenerate span containing exactly v.
func Point(v float64) Span { return Span{Lo: v, Hi: v} }

// IsEmpty reports whether the span contains no value. A NaN bound is empty.
func (s Span) IsEmpty() bool { return math.IsNaN(s.Lo) || math.IsNaN(s.Hi) || s.Lo > s.Hi }

// IsPoint reports whether the span contains exactly one value.
func (s Span) IsPoint() bool { return s.Lo == s.Hi }

// Length returns Hi-Lo, or zero for an empty span.
func (s Span) Length() float64 {
	if s.IsEmpty() {
		return 0
	}
	return s.Hi - s.Lo
}

// Contains reports whether v lies in the closed span.
func (s Span) Contains(v float64) bool { return !s.IsEmpty() && v >= s.Lo && v <= s.Hi }

// ContainsSpan reports whether every value of other lies in s. An empty other
// is contained by every non-empty span, because it demands nothing.
func (s Span) ContainsSpan(other Span) bool {
	if other.IsEmpty() {
		return !s.IsEmpty()
	}
	return !s.IsEmpty() && other.Lo >= s.Lo && other.Hi <= s.Hi
}

// Overlaps reports whether the two spans share at least one value.
func (s Span) Overlaps(other Span) bool {
	if s.IsEmpty() || other.IsEmpty() {
		return false
	}
	return s.Lo <= other.Hi && other.Lo <= s.Hi
}

// Intersect returns the common part of two spans and whether it is non-empty.
func (s Span) Intersect(other Span) (Span, bool) {
	if !s.Overlaps(other) {
		return Span{Lo: 1, Hi: 0}, false
	}
	return Span{Lo: math.Max(s.Lo, other.Lo), Hi: math.Min(s.Hi, other.Hi)}, true
}

// Shift returns the span translated by delta.
func (s Span) Shift(delta float64) Span { return Span{Lo: s.Lo + delta, Hi: s.Hi + delta} }

// String formats the span as [lo, hi], or as the literal "empty".
func (s Span) String() string {
	if s.IsEmpty() {
		return "empty"
	}
	return "[" + strconv.FormatFloat(s.Lo, 'g', -1, 64) + ", " + strconv.FormatFloat(s.Hi, 'g', -1, 64) + "]"
}

// WorldY converts a grid row to the world height of that row's TOP edge, for
// a grid of the given height. Row 0 is the topmost row and its top edge is at
// world height h. A character standing on a solid cell in row y has its feet
// at WorldY(y, h), which is also the bottom edge of row y-1.
func WorldY(row int32, height uint32) float64 { return float64(int64(height) - int64(row)) }

// GridRow converts a world height to the grid row whose band contains it, for
// a grid of the given height. It is the inverse of WorldY up to the half-open
// convention: a y exactly on a row boundary belongs to the lower row, the one
// with the larger index. The result is not clamped; a y outside the plane
// returns a row outside the grid, and the caller decides the policy for that.
func GridRow(y float64, height uint32) int32 {
	return int32(int64(height) - int64(math.Ceil(y)))
}

// RoomID identifies a room within a Plane. IDs are stable indices in creation
// order starting at zero; ascending numeric order is canonical.
type RoomID uint32

// TransitionID identifies a Transition within a Plane. IDs are stable indices
// in creation order starting at zero; ascending numeric order is canonical.
type TransitionID uint32

// SurfaceID identifies a Surface within a JumpGraph. IDs are stable indices in
// creation order starting at zero; ascending numeric order is canonical.
type SurfaceID uint32

// MotionNodeID identifies a MotionNode within a JumpGraph. IDs are stable
// indices in creation order starting at zero; ascending numeric order is
// canonical.
type MotionNodeID uint32

// MotionEdgeID identifies a MotionEdge within a JumpGraph. IDs are stable
// indices in creation order starting at zero; ascending numeric order is
// canonical.
type MotionEdgeID uint32

// PassageID groups the two senses of one piece of traversable geometry. Zero
// means the edge belongs to no passage. Two MotionEdges sharing a non-zero
// PassageID are the forward and backward traversal of the same opening, which
// is the cheap way to express the dominant metroidvania shortcut: both senses
// exist and differ only in what they require.
type PassageID uint32

// CellKind is the physical state of one grid cell in a platform map. The zero
// value is CellKindEmpty, which is air: a map under construction is air.
//
// Unlike the dungeon vocabulary, these kinds are not interchangeable with each
// other across a wire boundary. A semi-solid platform silently decoded as air
// produces a map that still looks valid and is not: a converter that meets an
// unknown kind must reject it, never default it.
type CellKind int

const (
	// CellKindEmpty is air. It can be crossed in flight and cannot be stood on
	// or waited in. It is not navigable on foot and carries no cost.
	CellKindEmpty CellKind = iota
	// CellKindSolid is a full blocking cell. It blocks from every direction and
	// its top edge supports a character.
	CellKindSolid
	// CellKindSemiSolid is a one-way platform. It supports a character whose
	// feet cross its top edge downward and is transparent to motion from below
	// and from the sides. On the way up it must NOT be expanded as a solid
	// block; doing so forbids passing through it, which is the move it exists
	// for. Leaving it downward requires a declared drop-through command.
	CellKindSemiSolid
	// CellKindClimbable is a ladder or a static rope: a capture corridor that
	// allows vertical motion at the profile's climb speed. It does not block
	// horizontal motion. A swinging rope is a dynamic body and is not this.
	CellKindClimbable
	// CellKindHazard damages or kills on contact. It does not block motion. A
	// hazard overlapping a solid cell is a spiked floor and is expressed as
	// terrain on that cell, not as this kind.
	CellKindHazard
)

// IsKnown reports whether k is one of the declared kinds. A converter uses it
// to reject an unknown kind instead of defaulting it to air.
func (k CellKind) IsKnown() bool { return k >= CellKindEmpty && k <= CellKindHazard }

// Blocks reports whether the kind blocks motion from every direction.
func (k CellKind) Blocks() bool { return k == CellKindSolid }

// Supports reports whether the kind's top edge can carry a standing character.
func (k CellKind) Supports() bool { return k == CellKindSolid || k == CellKindSemiSolid }

// String returns the kind's lowercase name.
func (k CellKind) String() string {
	switch k {
	case CellKindEmpty:
		return "empty"
	case CellKindSolid:
		return "solid"
	case CellKindSemiSolid:
		return "semi-solid"
	case CellKindClimbable:
		return "climbable"
	case CellKindHazard:
		return "hazard"
	}
	return "cellkind(" + strconv.Itoa(int(k)) + ")"
}

// TerrainID is an open, caller-defined terrain identifier. Daedalus assigns no
// meaning to an ID.
//
// This mirrors the root package's terrain vocabulary field for field and on
// purpose: a platform map wants the same palette-indexed overlay a dungeon
// has. It is duplicated rather than imported because the root type lives in a
// package whose remaining vocabulary is dungeon-specific, and platform does
// not import the root. Promoting the three terrain types into core would
// remove the duplication and is left as a deliberate, separately decided
// change, not something this contract performs in passing.
type TerrainID string

// TerrainDefinition describes one caller-defined terrain. EntryCost is zero
// for an impassable terrain and 1..255 for an enterable one. Transparency is
// independent of entry cost. Lethal marks a terrain that damages on contact,
// which is how a spiked floor is expressed: a solid cell carrying a lethal
// terrain, rather than a cell kind that is both solid and hazard.
type TerrainDefinition struct {
	// ID is the caller's identifier, compared byte by byte.
	ID TerrainID
	// EntryCost is zero for impassable and 1..255 for enterable.
	EntryCost uint8
	// Transparent reports whether sight passes through.
	Transparent bool
	// Lethal reports whether contact damages the character.
	Lethal bool
}

// TerrainLayer is a compact, palette-indexed overlay in grid cell order.
// Indices are row-major: index y*Width+x is zero for no terrain and otherwise
// addresses Palette[index-1].
type TerrainLayer struct {
	// Palette is strictly sorted by the byte order of TerrainDefinition.ID.
	Palette []TerrainDefinition
	// Indices holds exactly Width*Height bytes when the layer is present.
	Indices []byte
}

// Grid is one room's canonical geometry: the authoritative store and the thing
// the debug renderer draws. Surfaces and the jump graph are derived indices
// over it, never a second source of truth.
type Grid struct {
	// Width is the grid width in cells, 1..MaxRoomSide.
	Width uint32
	// Height is the grid height in cells, 1..MaxRoomSide.
	Height uint32
	// Cells holds exactly Width*Height kinds in row-major order.
	Cells []CellKind
	// Terrain is the optional palette-indexed overlay, or nil when absent.
	Terrain *TerrainLayer
}

// At returns the kind at a cell and whether the cell is inside the grid.
// Lookup fails closed: a coordinate outside the grid, or a short Cells slice,
// reports false, and the caller decides what lies outside the room. The oracle
// treats outside-the-plane as solid; a renderer treats it as nothing.
func (g Grid) At(at Cell) (CellKind, bool) {
	if at.X < 0 || at.Y < 0 || uint32(at.X) >= g.Width || uint32(at.Y) >= g.Height {
		return CellKindEmpty, false
	}
	index := uint64(at.Y)*uint64(g.Width) + uint64(at.X)
	if index >= uint64(len(g.Cells)) {
		return CellKindEmpty, false
	}
	return g.Cells[index], true
}

// TerrainAt returns the terrain selected at a cell. Lookup fails closed: a nil
// layer, a zero index, a coordinate outside the grid, a short index slice and
// an out-of-range palette index all mean no terrain.
func (g Grid) TerrainAt(at Cell) (TerrainDefinition, bool) {
	if g.Terrain == nil {
		return TerrainDefinition{}, false
	}
	if at.X < 0 || at.Y < 0 || uint32(at.X) >= g.Width || uint32(at.Y) >= g.Height {
		return TerrainDefinition{}, false
	}
	index := uint64(at.Y)*uint64(g.Width) + uint64(at.X)
	if index >= uint64(len(g.Terrain.Indices)) {
		return TerrainDefinition{}, false
	}
	selector := g.Terrain.Indices[index]
	if selector == 0 || int(selector) > len(g.Terrain.Palette) {
		return TerrainDefinition{}, false
	}
	return g.Terrain.Palette[selector-1], true
}

// CellCount returns the grid's cell count as a 64-bit product, which cannot
// overflow for any grid this package admits.
func (g Grid) CellCount() uint64 { return uint64(g.Width) * uint64(g.Height) }

// TransitionSide names the room edge an opening sits on. The zero value is
// TransitionSideUnspecified, because an opening whose side was never set is a
// bug and must not quietly become a left door.
type TransitionSide int

const (
	// TransitionSideUnspecified is the invalid zero value.
	TransitionSideUnspecified TransitionSide = iota
	// TransitionSideLeft is the room's west edge; Offset is a height.
	TransitionSideLeft
	// TransitionSideRight is the room's east edge; Offset is a height.
	TransitionSideRight
	// TransitionSideTop is the room's north edge; Offset is a horizontal
	// position.
	TransitionSideTop
	// TransitionSideBottom is the room's south edge; Offset is a horizontal
	// position.
	TransitionSideBottom
	// TransitionSideDoor is an opening that is not on an edge: a warp, a lift,
	// a stag station. Its geometry is a point and Exit states which way the
	// character leaves.
	TransitionSideDoor
)

// IsCardinal reports whether the side is one of the four room edges.
func (s TransitionSide) IsCardinal() bool {
	return s >= TransitionSideLeft && s <= TransitionSideBottom
}

// IsVertical reports whether the side runs vertically, which is exactly when
// Offset means a height above the room's floor.
func (s TransitionSide) IsVertical() bool {
	return s == TransitionSideLeft || s == TransitionSideRight
}

// String returns the side's lowercase name, matching the naming the reference
// game's scene data uses: left, right, top, bot, door.
func (s TransitionSide) String() string {
	switch s {
	case TransitionSideLeft:
		return "left"
	case TransitionSideRight:
		return "right"
	case TransitionSideTop:
		return "top"
	case TransitionSideBottom:
		return "bot"
	case TransitionSideDoor:
		return "door"
	}
	return "unspecified"
}

// Traversal is one sense of a Transition: what the character must already have
// to pass through it that way. A nil *Traversal means the sense does not
// exist, which is genuinely irreversible geometry and is rare. Both senses
// present with different Requires is the common metroidvania shortcut and
// costs one extra field, not a second piece of geometry.
type Traversal struct {
	// Requires is the AbilitySet the character must hold. The zero value is the
	// base moveset, so a zero Traversal is an unconditional passage.
	Requires AbilitySet
	// Note is free-form provenance for debugging and reports. It never affects
	// a verdict.
	Note string
}

// Transition is one opening in a room's boundary. It carries the side, the
// index of the opening on that side, its position and extent along that side,
// the direction of travel leaving through it, the opening it pairs with, and
// one Traversal per sense.
//
// Rooms are not a uniform grid of screens. They have unequal sizes, a side can
// carry more than one opening — left1 and left2 — and the set embeds in a
// continuous plane. That is why the opening needs an index and a position and
// not merely a side.
type Transition struct {
	// ID is this transition's stable identifier within the Plane.
	ID TransitionID
	// Room is the room this opening belongs to.
	Room RoomID
	// Side is the edge the opening sits on.
	Side TransitionSide
	// Index distinguishes several openings on the same side, ascending in the
	// canonical order of Offset. It is the number in names like left2.
	Index uint32
	// Offset is the opening's position along Side, in cells, measured from the
	// room's minimum corner on that side's axis. For TransitionSideLeft and
	// TransitionSideRight the side runs vertically and Offset is the opening's
	// HEIGHT above the room's bottom edge, in the world frame. For
	// TransitionSideTop and TransitionSideBottom it is a horizontal position.
	Offset uint32
	// Extent is the opening's size along Side, in cells, and is at least one.
	// A character whose body is taller than Extent cannot use a vertical
	// opening at all.
	Extent uint32
	// Exit is the direction of travel when leaving the room through this
	// opening, in the core grid convention. For a cardinal side it must agree
	// with that side; for a door it is the only thing that states the sense.
	Exit Direction
	// To is the opening this one pairs with, in another room or, for a loop,
	// in the same one. A transition always pairs; an opening with no partner is
	// a wall.
	To TransitionID
	// Outbound is the sense that leaves this room through the opening, or nil
	// when that sense does not exist.
	Outbound *Traversal
	// Inbound is the sense that enters this room through the opening, or nil
	// when that sense does not exist.
	Inbound *Traversal
}

// IsOneWay reports whether exactly one sense of the transition exists. A
// transition with both senses present is two-way even when the two senses
// require different abilities, which is the usual case and is NOT one-way
// geometry.
func (t Transition) IsOneWay() bool {
	return (t.Outbound == nil) != (t.Inbound == nil)
}

// IsConditional reports whether both senses exist but demand different
// abilities. This is the dominant shape of a real metroidvania shortcut.
func (t Transition) IsConditional() bool {
	return t.Outbound != nil && t.Inbound != nil && t.Outbound.Requires != t.Inbound.Requires
}

// Anchor is a position in the plane: a room and a cell inside it.
type Anchor struct {
	// Room is the room the position belongs to.
	Room RoomID
	// At is the cell inside that room's grid.
	At Cell
}

// Room is one unit of authored and streamed geometry, placed in a plane. It is
// not a screen in a uniform grid: rooms have unequal sizes and a plane embeds
// them at explicit origins.
type Room struct {
	// ID is this room's stable identifier within the Plane.
	ID RoomID
	// Origin is the room's top-left cell in the plane's cell frame.
	Origin Cell
	// Grid is the room's canonical geometry.
	Grid Grid
	// Transitions lists this room's openings in ascending TransitionID order.
	Transitions []Transition
}

// Plane is a whole platform map: rooms embedded at explicit origins, with a
// spawn and a goal.
type Plane struct {
	// Width is the plane's width in cells.
	Width uint32
	// Height is the plane's height in cells.
	Height uint32
	// Rooms lists the rooms in ascending RoomID order.
	Rooms []Room
	// Spawn is where the character starts.
	Spawn Anchor
	// Goal is the position the progression must reach.
	Goal Anchor
}

// Ability is one acquired movement upgrade. It is a bit index into an
// AbilitySet, and the vocabulary is closed: Daedalus certifies only geometry
// whose moveset it can model.
//
// Walking, the ground jump, falling and drop-through are the BASE moveset.
// They are always available and are therefore not abilities; the empty
// AbilitySet is a character who can already run and jump.
//
// Crystal Heart-style charged dashes are deliberately absent. The move has no
// published distance and terminates by interruption rather than by a duration,
// so it cannot be certified by the analytic model. A profile that wants it
// gets VerdictUnknown with ReasonUnsupportedMoveset, which is the honest
// answer, not a silent approximation.
type Ability uint8

const (
	// AbilityDash is a fixed-velocity burst of declared duration.
	AbilityDash Ability = iota
	// AbilityDoubleJump is one or more mid-air jumps.
	AbilityDoubleJump
	// AbilityWallJump is clinging to a vertical surface and jumping off it.
	AbilityWallJump
	// AbilityClimb is using ladders and static ropes.
	AbilityClimb
	// AbilityShadowDash upgrades the dash: a higher speed and, when the profile
	// says so, immunity to hazards crossed during the burst.
	AbilityShadowDash
	// abilityCount bounds the vocabulary and is not an ability.
	abilityCount
)

// MaxAbilities is the number of declared abilities.
const MaxAbilities = int(abilityCount)

// IsKnown reports whether a is one of the declared abilities.
func (a Ability) IsKnown() bool { return a < abilityCount }

// String returns the ability's lowercase name.
func (a Ability) String() string {
	switch a {
	case AbilityDash:
		return "dash"
	case AbilityDoubleJump:
		return "double-jump"
	case AbilityWallJump:
		return "wall-jump"
	case AbilityClimb:
		return "climb"
	case AbilityShadowDash:
		return "shadow-dash"
	}
	return "ability(" + strconv.Itoa(int(a)) + ")"
}

// AbilitySet is a set of acquired abilities, held as a bitset so it is
// comparable, usable as a map key and cheap to carry on every graph edge. The
// zero value is the base moveset: able to run, jump and fall, and nothing
// more.
type AbilitySet uint32

// NewAbilitySet returns the set containing exactly the given abilities.
func NewAbilitySet(abilities ...Ability) AbilitySet {
	return AbilitySet(0).With(abilities...)
}

// Has reports whether the set contains a.
func (s AbilitySet) Has(a Ability) bool {
	if !a.IsKnown() {
		return false
	}
	return s&(1<<a) != 0
}

// With returns the set extended with the given abilities. An ability outside
// the declared vocabulary is ignored rather than corrupting the set.
func (s AbilitySet) With(abilities ...Ability) AbilitySet {
	for _, a := range abilities {
		if a.IsKnown() {
			s |= 1 << a
		}
	}
	return s
}

// Without returns the set with the given abilities removed.
func (s AbilitySet) Without(abilities ...Ability) AbilitySet {
	for _, a := range abilities {
		if a.IsKnown() {
			s &^= 1 << a
		}
	}
	return s
}

// Union returns the abilities held by either set.
func (s AbilitySet) Union(other AbilitySet) AbilitySet { return s | other }

// Contains reports whether s holds every ability in other. This is the
// precondition test every gate and every MotionEdge uses.
func (s AbilitySet) Contains(other AbilitySet) bool { return s&other == other }

// Count returns the number of abilities in the set.
func (s AbilitySet) Count() int {
	count := 0
	for a := Ability(0); a < abilityCount; a++ {
		if s.Has(a) {
			count++
		}
	}
	return count
}

// Abilities returns the set's abilities in ascending order.
func (s AbilitySet) Abilities() []Ability {
	var out []Ability
	for a := Ability(0); a < abilityCount; a++ {
		if s.Has(a) {
			out = append(out, a)
		}
	}
	return out
}

// String lists the abilities in ascending order, comma separated, or "base"
// for the empty set.
func (s AbilitySet) String() string {
	names := make([]string, 0, MaxAbilities)
	for _, a := range s.Abilities() {
		names = append(names, a.String())
	}
	if len(names) == 0 {
		return "base"
	}
	return strings.Join(names, ",")
}

// ProgressionStep is one acquisition in a ProgressionPlan: the abilities
// obtained together at that point in the intended order.
type ProgressionStep struct {
	// Name is the caller's label for the step, used in reports. It never
	// affects a verdict.
	Name string
	// Grants is the set obtained at this step and must be non-empty.
	Grants AbilitySet
}

// ProgressionPlan declares which abilities exist in a map and the order in
// which they may be obtained. It only grants: there is no step that removes an
// ability.
//
// That restriction is a modelling choice with a consequence worth stating. A
// grant-only plan makes the stage movesets a chain under inclusion, so the
// reachable sets are expected to grow monotonically with progress. That is a
// HYPOTHESIS about the geometry, not a law: a mandatory upgrade that changed
// speed or body size could remove a move. Because this plan cannot express
// such an upgrade, a generator that needs one must extend the type rather than
// assume monotonicity holds anyway.
type ProgressionPlan struct {
	// Base is the set the character starts with. The zero value is the base
	// moveset.
	Base AbilitySet
	// Steps lists the acquisitions in the intended order.
	Steps []ProgressionStep
}

// Stages returns the cumulative moveset after each point of the plan: the
// first entry is Base and entry i+1 adds Steps[i].Grants. The result always
// has len(Steps)+1 entries, and reachability must be verified against every
// one of them, not only the last.
func (p ProgressionPlan) Stages() []AbilitySet {
	stages := make([]AbilitySet, 0, len(p.Steps)+1)
	current := p.Base
	stages = append(stages, current)
	for _, step := range p.Steps {
		current = current.Union(step.Grants)
		stages = append(stages, current)
	}
	return stages
}

// Final returns the moveset after every step.
func (p ProgressionPlan) Final() AbilitySet {
	stages := p.Stages()
	return stages[len(stages)-1]
}

// SurfaceKind is the role a surface plays for the character.
type SurfaceKind int

const (
	// SurfaceKindUnspecified is the invalid zero value.
	SurfaceKindUnspecified SurfaceKind = iota
	// SurfaceKindFloor is a solid top edge that supports standing.
	SurfaceKindFloor
	// SurfaceKindSemiSolid is a one-way top edge: it supports standing and can
	// be crossed from below.
	SurfaceKindSemiSolid
	// SurfaceKindWallLeft is a vertical face whose outward normal points left,
	// so the character clings to it from its left side.
	SurfaceKindWallLeft
	// SurfaceKindWallRight is a vertical face whose outward normal points
	// right.
	SurfaceKindWallRight
	// SurfaceKindCeiling is a solid bottom edge that stops a rising body.
	SurfaceKindCeiling
	// SurfaceKindClimbable is a ladder or static rope corridor.
	SurfaceKindClimbable
)

// Supports reports whether the kind can carry a standing character.
func (k SurfaceKind) Supports() bool {
	return k == SurfaceKindFloor || k == SurfaceKindSemiSolid
}

// String returns the kind's lowercase name.
func (k SurfaceKind) String() string {
	switch k {
	case SurfaceKindFloor:
		return "floor"
	case SurfaceKindSemiSolid:
		return "semi-solid"
	case SurfaceKindWallLeft:
		return "wall-left"
	case SurfaceKindWallRight:
		return "wall-right"
	case SurfaceKindCeiling:
		return "ceiling"
	case SurfaceKindClimbable:
		return "climbable"
	}
	return "unspecified"
}

// SurfaceInterval is a contiguous run of a surface over which the support
// conditions do not change. A surface is split into intervals wherever
// headroom, hazard or support changes, so that every point of one interval is
// interchangeable for the purposes of a verdict.
//
// Footing is the set of FOOT positions, not the geometric extent of the
// surface: for a top edge spanning [a, b] under a full-support policy it is
// [a+r+ε, b-r-ε], and an empty Footing means the surface cannot be stood on at
// all under that policy. A low ceiling fragments one edge into several
// intervals.
type SurfaceInterval struct {
	// Footing is the span of legal foot x positions, in world cells.
	Footing Span
	// Headroom is the free vertical space above the footing, in cells. A value
	// below the profile's body height means the interval cannot be occupied.
	Headroom float64
	// Hazard reports whether standing here damages the character.
	Hazard bool
}

// Surface is a maximal run of one kind at one world coordinate, derived from
// the grid. It is an index over the geometry and is invalidated whenever the
// geometry changes; it is never a second source of truth.
type Surface struct {
	// ID is this surface's stable identifier within the JumpGraph.
	ID SurfaceID
	// Room is the room the surface belongs to.
	Room RoomID
	// Kind is the surface's role.
	Kind SurfaceKind
	// Extent is the surface's geometric span along its own axis, in world
	// cells: horizontal for a floor, semi-solid or ceiling, vertical for a
	// wall or a climbable.
	Extent Span
	// At is the surface's position on the other axis, in world cells: the
	// height of a floor's top edge, or the x of a wall's face.
	At float64
	// Intervals lists the surface's support intervals in ascending order of
	// Footing.Lo. A surface with no interval exists geometrically and cannot
	// be used.
	Intervals []SurfaceInterval
}

// MotionMode is the character's control regime at a node. The zero value is
// MotionModeUnspecified because a node whose mode was never set is a bug.
type MotionMode int

const (
	// MotionModeUnspecified is the invalid zero value.
	MotionModeUnspecified MotionMode = iota
	// MotionModeGrounded is standing or running on a supporting surface.
	MotionModeGrounded
	// MotionModeAirborne is free ballistic motion under gravity.
	MotionModeAirborne
	// MotionModeCoyote is the window after leaving a ledge during which the
	// ground jump is still accepted. It is its own mode, not a longer
	// grounded mode and not a shorter airborne one, because the jump taken
	// from it starts lower than the ledge.
	MotionModeCoyote
	// MotionModeWallCling is held against a vertical face.
	MotionModeWallCling
	// MotionModeDashing is inside a dash burst, where the profile may suspend
	// gravity and fix the velocity.
	MotionModeDashing
	// MotionModeClimbing is on a ladder or static rope.
	MotionModeClimbing
)

// String returns the mode's lowercase name.
func (m MotionMode) String() string {
	switch m {
	case MotionModeGrounded:
		return "grounded"
	case MotionModeAirborne:
		return "airborne"
	case MotionModeCoyote:
		return "coyote"
	case MotionModeWallCling:
		return "wall-cling"
	case MotionModeDashing:
		return "dashing"
	case MotionModeClimbing:
		return "climbing"
	}
	return "unspecified"
}

// WallSide names which side of the character a wall was on. It exists so a
// profile that forbids reusing the same wall can be enforced.
type WallSide int

const (
	// WallSideNone means no wall was touched since the last reset.
	WallSideNone WallSide = iota
	// WallSideLeft means the wall was to the character's left.
	WallSideLeft
	// WallSideRight means the wall was to the character's right.
	WallSideRight
)

// String returns the side's lowercase name.
func (w WallSide) String() string {
	switch w {
	case WallSideLeft:
		return "left"
	case WallSideRight:
		return "right"
	}
	return "none"
}

// Resources is the consumable state the character carries between manoeuvres.
// It is deliberately all scalars so that it is comparable and can be a map
// key: the search explores a state space whose coordinate includes it.
//
// The zero value is the EXHAUSTED state, not the fresh one: no air jumps, no
// dash charges, no wall contact. What "full" means depends on the profile, so
// a fresh value comes from MovementProfile.FullResources and never from a
// composite literal that forgot a field.
type Resources struct {
	// AirJumps is the number of mid-air jumps still available.
	AirJumps uint8
	// DashCharges is the number of dash bursts still available.
	DashCharges uint8
	// DashCooldown is the time, in seconds, before the next dash is accepted.
	DashCooldown float64
	// WallJumpsSinceGround counts wall jumps performed since the last ground
	// contact, for a profile that caps them.
	WallJumpsSinceGround uint8
	// LastWall is the side of the wall most recently used, for a profile that
	// forbids reusing the same one.
	LastWall WallSide
	// ClingRemaining is the wall-cling stamina left, in seconds. A profile
	// with unlimited stamina leaves it at the profile's sentinel.
	ClingRemaining float64
}

// MotionNode is one state of the character in the jump graph. It is REFINED:
// it carries position, velocity, mode and resources, not merely a platform.
//
// A node is a SET of concrete states — every foot position in Footing, every
// horizontal velocity in Velocity, with that mode and exactly those resources.
// Every edge leaving the node must be valid for the whole set. Abstracting a
// set and then justifying an edge by one convenient member of it is how a
// graph comes to lie.
//
// A node whose mode is grounded, whose velocity span contains zero and whose
// resources are the profile's full value is a REST node, and the set of rest
// nodes is exactly the coarse "one node per platform" graph. The refinement
// therefore contains the aggregation; the reverse does not hold, which is why
// this is the canonical shape. See NodeDiscipline.
type MotionNode struct {
	// ID is this node's stable identifier within the JumpGraph.
	ID MotionNodeID
	// Surface is the surface the state is attached to. For an airborne state
	// it is the surface the state departed from.
	Surface SurfaceID
	// Interval is the index into that surface's Intervals.
	Interval uint32
	// Height is the world height of the feet, in cells. For a grounded node
	// it is the surface's At. It is stored on the node, rather than only on
	// the surface, so that a node is self-describing: an oracle query, a debug
	// overlay and a witness check all need the position and none of them
	// should have to carry a surface table to find it.
	Height float64
	// Footing is the span of foot x positions the node stands for, in world
	// cells. It is contained in the interval's own Footing.
	Footing Span
	// Velocity is the span of horizontal velocities the node stands for, in
	// cells per second.
	Velocity Span
	// Mode is the control regime.
	Mode MotionMode
	// Resources is the consumable state.
	Resources Resources
}

// IsRest reports whether the node is a certified rest state under profile:
// grounded, able to be standing still, and holding the profile's full
// resources. Only rest nodes may be collapsed to one node per platform.
func (n MotionNode) IsRest(profile MovementProfile) bool {
	return n.Mode == MotionModeGrounded &&
		n.Velocity.Contains(0) &&
		n.Resources == profile.FullResources()
}

// NodeDiscipline records which node model a JumpGraph was built with, so a
// consumer can tell whether the graph is allowed to be as small as it is.
type NodeDiscipline int

const (
	// NodeDisciplineUnspecified is the invalid zero value.
	NodeDisciplineUnspecified NodeDiscipline = iota
	// NodeDisciplineRefined is the canonical model: nodes carry position,
	// velocity, mode and resources, and no two states are merged unless the
	// merged node's claims hold for all of them.
	NodeDisciplineRefined
	// NodeDisciplineRestOnly is the aggregated model: every node is a rest
	// state, so composition is justified by the reset property rather than by
	// carrying the state. A graph may declare it only when every node
	// satisfies MotionNode.IsRest. It is smaller and it loses every route that
	// needs to arrive somewhere moving or with a resource already spent.
	NodeDisciplineRestOnly
)

// String returns the discipline's lowercase name.
func (d NodeDiscipline) String() string {
	switch d {
	case NodeDisciplineRefined:
		return "refined"
	case NodeDisciplineRestOnly:
		return "rest-only"
	}
	return "unspecified"
}

// MotionEdgeKind is the manoeuvre an edge realises. The zero value is invalid.
type MotionEdgeKind int

const (
	// MotionEdgeKindUnspecified is the invalid zero value.
	MotionEdgeKindUnspecified MotionEdgeKind = iota
	// MotionEdgeKindWalk moves along one supporting surface.
	MotionEdgeKindWalk
	// MotionEdgeKindFall leaves a ledge without jumping.
	MotionEdgeKindFall
	// MotionEdgeKindDropThrough leaves a semi-solid downward.
	MotionEdgeKindDropThrough
	// MotionEdgeKindJump is the ground jump, including one taken inside the
	// coyote window.
	MotionEdgeKindJump
	// MotionEdgeKindDoubleJump is a mid-air jump.
	MotionEdgeKindDoubleJump
	// MotionEdgeKindDash is a dash burst.
	MotionEdgeKindDash
	// MotionEdgeKindWallJump leaves a wall cling.
	MotionEdgeKindWallJump
	// MotionEdgeKindWallCling attaches to a wall.
	MotionEdgeKindWallCling
	// MotionEdgeKindClimb moves along a ladder or rope.
	MotionEdgeKindClimb
	// MotionEdgeKindTransition crosses a room boundary through a Transition.
	MotionEdgeKindTransition
)

// String returns the kind's lowercase name.
func (k MotionEdgeKind) String() string {
	switch k {
	case MotionEdgeKindWalk:
		return "walk"
	case MotionEdgeKindFall:
		return "fall"
	case MotionEdgeKindDropThrough:
		return "drop-through"
	case MotionEdgeKindJump:
		return "jump"
	case MotionEdgeKindDoubleJump:
		return "double-jump"
	case MotionEdgeKindDash:
		return "dash"
	case MotionEdgeKindWallJump:
		return "wall-jump"
	case MotionEdgeKindWallCling:
		return "wall-cling"
	case MotionEdgeKindClimb:
		return "climb"
	case MotionEdgeKindTransition:
		return "transition"
	}
	return "unspecified"
}

// MotionEdge is a directed transition between two MotionNodes. The graph is
// directed and the asymmetry is the genre: falling is free, climbing back is
// not. A→B existing says nothing about B→A.
type MotionEdge struct {
	// ID is this edge's stable identifier within the JumpGraph.
	ID MotionEdgeID
	// From is the tail node.
	From MotionNodeID
	// To is the head node.
	To MotionNodeID
	// Kind is the manoeuvre.
	Kind MotionEdgeKind
	// Requires is the AbilitySet the character must hold to use the edge. This
	// is the field that makes progression work without rebuilding the graph:
	// the stage filter is a predicate over it, not a second graph.
	Requires AbilitySet
	// Passage groups this edge with the edge that traverses the same geometry
	// in the opposite sense, or is zero when the edge belongs to no passage.
	Passage PassageID
	// Transition is the opening this edge crosses, for a transition edge. It
	// is meaningless for every other kind.
	Transition TransitionID
	// Duration is the manoeuvre's time in seconds.
	Duration float64
	// Witness is the proof the edge exists: the trajectory and the commands
	// that realise it. A certified edge without a witness is not certified;
	// the field is a pointer only so that a graph built with WantWitness false
	// can omit it and say so in its Judgement.
	Witness *Witness
}

// Input is a bitset of the controller inputs a command can assert.
type Input uint8

const (
	// InputLeft holds the left direction.
	InputLeft Input = 1 << iota
	// InputRight holds the right direction.
	InputRight
	// InputUp holds the up direction.
	InputUp
	// InputDown holds the down direction.
	InputDown
	// InputJump holds the jump button.
	InputJump
	// InputDash holds the dash button.
	InputDash
)

// Command is one controller state change in a witness: from At seconds after
// the manoeuvre's start, the asserted inputs are exactly Hold.
//
// A command is a STATE, not an event, so a witness is a step function over the
// manoeuvre and there is no ambiguity about what is held between two commands.
type Command struct {
	// At is the time of the change, in seconds from the manoeuvre's start.
	At float64
	// Hold is the set of inputs asserted from At until the next command.
	Hold Input
}

// MotionState is a concrete point of a trajectory in the world frame: the
// centre of the feet and the velocity there.
type MotionState struct {
	// X is the foot centre's horizontal position, in world cells.
	X float64
	// Y is the foot centre's height, in world cells.
	Y float64
	// VX is the horizontal velocity, in cells per second.
	VX float64
	// VY is the vertical velocity, in cells per second, positive upward.
	VY float64
}

// Phase is one constant-acceleration segment of a witness. A manoeuvre is a
// finite sequence of phases with events between them; there is no single
// closed form for a dash that suspends gravity followed by a fall that does
// not.
type Phase struct {
	// Mode is the control regime during the phase.
	Mode MotionMode
	// Start is the state at the phase's beginning.
	Start MotionState
	// End is the state at the phase's end.
	End MotionState
	// Duration is the phase's length in seconds.
	Duration float64
	// AccelX is the constant horizontal acceleration, in cells per second
	// squared.
	AccelX float64
	// AccelY is the constant vertical acceleration, in cells per second
	// squared, positive upward. A phase under gravity has a negative value; a
	// dash that suspends gravity has zero.
	AccelY float64
}

// Witness is the evidence that a MotionEdge is real inside the model: the
// trajectory as a phase sequence and the commands that produce it. It is what
// makes a certificate checkable by something other than the code that issued
// it, and an invariant test re-validates it independently.
type Witness struct {
	// Phases lists the trajectory segments in order.
	Phases []Phase
	// Commands lists the controller states in ascending At order. The first
	// command is at time zero.
	Commands []Command
	// ControlRate is the fixed step, in hertz, the commands are expressed
	// against, or zero when they are continuous. A consumer whose controller
	// runs at a different rate cannot replay the witness verbatim.
	ControlRate float64
}

// Duration returns the witness's total time, the sum of its phases.
func (w *Witness) Duration() float64 {
	if w == nil {
		return 0
	}
	total := 0.0
	for _, phase := range w.Phases {
		total += phase.Duration
	}
	return total
}

// Route is a path through a JumpGraph: the edges in order, and the abilities
// their union demands.
type Route struct {
	// From is the first node.
	From MotionNodeID
	// To is the last node.
	To MotionNodeID
	// Edges lists the traversed edges in order. An empty slice with From equal
	// to To is the trivial route.
	Edges []MotionEdgeID
	// Requires is the union of the edges' requirements.
	Requires AbilitySet
	// Duration is the sum of the edges' durations, in seconds.
	Duration float64
}

// JumpGraph is the directed navigation semantics of one room or one plane
// under one profile and one moveset. It is derived from the grid and is
// invalidated by any change to it.
type JumpGraph struct {
	// Model names the movement model that produced the graph. A consumer
	// checks it before trusting a certificate.
	Model string
	// ProfileVersion is the version of the MovementProfile the graph was built
	// for. A graph is meaningless under a different profile.
	ProfileVersion string
	// Abilities is the moveset the graph was built for. Edges requiring more
	// than this set must not be present.
	Abilities AbilitySet
	// Discipline is the node model used.
	Discipline NodeDiscipline
	// Surfaces lists the derived surfaces in ascending SurfaceID order.
	Surfaces []Surface
	// Nodes lists the states in ascending MotionNodeID order, so that
	// Nodes[i].ID == MotionNodeID(i).
	Nodes []MotionNode
	// Edges lists the transitions in ascending MotionEdgeID order, so that
	// Edges[i].ID == MotionEdgeID(i).
	Edges []MotionEdge
}

// Node returns the node with the given ID and whether it exists.
func (g *JumpGraph) Node(id MotionNodeID) (MotionNode, bool) {
	if g == nil || uint64(id) >= uint64(len(g.Nodes)) {
		return MotionNode{}, false
	}
	return g.Nodes[id], true
}

// Edge returns the edge with the given ID and whether it exists.
func (g *JumpGraph) Edge(id MotionEdgeID) (MotionEdge, bool) {
	if g == nil || uint64(id) >= uint64(len(g.Edges)) {
		return MotionEdge{}, false
	}
	return g.Edges[id], true
}

// Surface returns the surface with the given ID and whether it exists.
func (g *JumpGraph) Surface(id SurfaceID) (Surface, bool) {
	if g == nil || uint64(id) >= uint64(len(g.Surfaces)) {
		return Surface{}, false
	}
	return g.Surfaces[id], true
}

// OutEdges returns the edges leaving a node, in ascending MotionEdgeID order.
// It scans the edge list, which is O(len(Edges)); a builder that needs an
// index builds one, because a map here would put iteration order into the
// output.
func (g *JumpGraph) OutEdges(from MotionNodeID) []MotionEdge {
	if g == nil {
		return nil
	}
	var out []MotionEdge
	for _, edge := range g.Edges {
		if edge.From == from {
			out = append(out, edge)
		}
	}
	return out
}

// Passage returns the edges belonging to a passage, in ascending
// MotionEdgeID order. A passage with two edges is traversable in both senses,
// possibly under different requirements; a passage with one edge is one-way
// geometry. The zero PassageID always returns nothing.
func (g *JumpGraph) Passage(id PassageID) []MotionEdge {
	if g == nil || id == 0 {
		return nil
	}
	var out []MotionEdge
	for _, edge := range g.Edges {
		if edge.Passage == id {
			out = append(out, edge)
		}
	}
	return out
}

// Reachable filters the graph's edges by a moveset: it reports whether an edge
// is usable by a character holding abilities. It is the whole of progression
// filtering, and it is a predicate rather than a second graph on purpose.
func (e MotionEdge) Reachable(abilities AbilitySet) bool {
	return abilities.Contains(e.Requires)
}

// BeatKind is one unit of the rhythm vocabulary. The vocabulary is CLOSED:
// Daedalus guarantees geometry only for beats it knows how to build and to
// check. The weights and the parameters are the caller's; the vocabulary is
// not. The zero value is invalid, so a distribution that forgot to name a kind
// is rejected rather than silently becoming a rest.
type BeatKind int

const (
	// BeatKindUnspecified is the invalid zero value.
	BeatKindUnspecified BeatKind = iota
	// BeatKindRest is a stretch with no demand: somewhere to stand, breathe
	// and read the next beat. Alternating demand with rest is the one thing
	// every rhythm-based generator agrees on.
	BeatKindRest
	// BeatKindTraverse is a corridor: ground to walk, with no gap and no
	// hazard.
	BeatKindTraverse
	// BeatKindGap is a horizontal gap crossed by a single jump.
	BeatKindGap
	// BeatKindClimb is a staircase of platforms gaining height.
	BeatKindClimb
	// BeatKindDescend is a controlled drop to a lower platform.
	BeatKindDescend
	// BeatKindShaft is a vertical passage climbed by alternating wall jumps.
	BeatKindShaft
	// BeatKindHazard is a crossing whose failure state is damage rather than a
	// missed landing.
	BeatKindHazard
	// BeatKindPrecision is a sequence whose tolerance is deliberately tight.
	BeatKindPrecision
	// BeatKindGate is a passage that an ability opens: the beat exists to be
	// impossible before the ability and routine after it.
	BeatKindGate
	// BeatKindSecret is an optional branch that terminates in a reward.
	BeatKindSecret
	// BeatKindCheckpoint is a safe node: a bench, a save, a respawn anchor.
	BeatKindCheckpoint
)

// IsKnown reports whether k is one of the declared kinds.
func (k BeatKind) IsKnown() bool { return k >= BeatKindRest && k <= BeatKindCheckpoint }

// IsDemanding reports whether the beat asks something of the player. Rest and
// checkpoint do not; everything else does.
func (k BeatKind) IsDemanding() bool {
	return k.IsKnown() && k != BeatKindRest && k != BeatKindCheckpoint
}

// String returns the kind's lowercase name.
func (k BeatKind) String() string {
	switch k {
	case BeatKindRest:
		return "rest"
	case BeatKindTraverse:
		return "traverse"
	case BeatKindGap:
		return "gap"
	case BeatKindClimb:
		return "climb"
	case BeatKindDescend:
		return "descend"
	case BeatKindShaft:
		return "shaft"
	case BeatKindHazard:
		return "hazard"
	case BeatKindPrecision:
		return "precision"
	case BeatKindGate:
		return "gate"
	case BeatKindSecret:
		return "secret"
	case BeatKindCheckpoint:
		return "checkpoint"
	}
	return "unspecified"
}

// Verdict is the three-valued answer to every question this package's oracle
// answers. The zero value is VerdictUnknown, because an answer nobody computed
// proves nothing.
type Verdict int

const (
	// VerdictUnknown means the question was not decided. The search budget ran
	// out, or the manoeuvre is outside the supported moveset, or the geometry
	// is outside the model. It is NOT a weak rejection.
	VerdictUnknown Verdict = iota
	// VerdictCertified means a witness exists inside the declared model. The
	// route is real under that model's assumptions; whether the model matches
	// a particular engine is a separate question the model's name answers.
	VerdictCertified
	// VerdictRejected means the model excludes the manoeuvre. The analysis is
	// sound but INCOMPLETE: a finite enumeration of launch positions and jump
	// timings can miss a manoeuvre that exists, so a rejection is this model's
	// refusal and not a proof of impossibility.
	VerdictRejected
)

// String returns the verdict's lowercase name.
func (v Verdict) String() string {
	switch v {
	case VerdictCertified:
		return "certified"
	case VerdictRejected:
		return "rejected"
	}
	return "unknown"
}

// VerdictReason is the structured motive behind a Verdict. Each reason belongs
// to exactly one verdict, which Verdict reports and Judgement.Validate
// enforces, so a Judgement cannot claim a rejection for a budget that ran out.
type VerdictReason int

const (
	// ReasonUnspecified is the invalid zero value.
	ReasonUnspecified VerdictReason = iota
	// ReasonWitnessFound certifies: a trajectory and commands were produced.
	ReasonWitnessFound
	// ReasonTrivial certifies: the question was answered without motion, as
	// when the origin and the destination are the same state.
	ReasonTrivial
	// ReasonOutOfEnvelope rejects: no candidate manoeuvre could reach the
	// destination even before obstacles were considered.
	ReasonOutOfEnvelope
	// ReasonObstructed rejects: every candidate trajectory collided.
	ReasonObstructed
	// ReasonLandingUnsupported rejects: the destination offers no footing for
	// the body, or the contact was tangent at the apex rather than descending.
	ReasonLandingUnsupported
	// ReasonResourceExhausted rejects: the manoeuvre needs a dash charge, an
	// air jump or a cooldown the arriving state does not have.
	ReasonResourceExhausted
	// ReasonAbilityMissing rejects: the moveset lacks an ability the manoeuvre
	// requires.
	ReasonAbilityMissing
	// ReasonDisconnected rejects: the graph holds no route between the two
	// nodes under the given moveset.
	ReasonDisconnected
	// ReasonBudgetExhausted is unknown: the search budget ran out before the
	// question was decided.
	ReasonBudgetExhausted
	// ReasonUnsupportedMoveset is unknown: the profile asks for a manoeuvre
	// the model does not analyse.
	ReasonUnsupportedMoveset
	// ReasonUnsupportedGeometry is unknown: the geometry is outside the model,
	// such as a moving platform under a static analysis.
	ReasonUnsupportedGeometry
	// ReasonNotImplemented is unknown: this build does not answer the
	// question yet.
	ReasonNotImplemented
)

// Verdict returns the only verdict the reason may accompany.
func (r VerdictReason) Verdict() Verdict {
	switch r {
	case ReasonWitnessFound, ReasonTrivial:
		return VerdictCertified
	case ReasonOutOfEnvelope, ReasonObstructed, ReasonLandingUnsupported,
		ReasonResourceExhausted, ReasonAbilityMissing, ReasonDisconnected:
		return VerdictRejected
	}
	return VerdictUnknown
}

// String returns the reason's lowercase, hyphenated name.
func (r VerdictReason) String() string {
	switch r {
	case ReasonWitnessFound:
		return "witness-found"
	case ReasonTrivial:
		return "trivial"
	case ReasonOutOfEnvelope:
		return "out-of-envelope"
	case ReasonObstructed:
		return "obstructed"
	case ReasonLandingUnsupported:
		return "landing-unsupported"
	case ReasonResourceExhausted:
		return "resource-exhausted"
	case ReasonAbilityMissing:
		return "ability-missing"
	case ReasonDisconnected:
		return "disconnected"
	case ReasonBudgetExhausted:
		return "budget-exhausted"
	case ReasonUnsupportedMoveset:
		return "unsupported-moveset"
	case ReasonUnsupportedGeometry:
		return "unsupported-geometry"
	case ReasonNotImplemented:
		return "not-implemented"
	}
	return "unspecified"
}

// BudgetReport is what a search actually spent, and whether it stopped because
// it ran out. A report with Exhausted set must accompany VerdictUnknown.
type BudgetReport struct {
	// CandidateEdges is the number of candidate manoeuvres enumerated.
	CandidateEdges uint64
	// ExpandedNodes is the number of graph nodes expanded.
	ExpandedNodes uint64
	// CollisionTests is the number of continuous collision tests performed.
	CollisionTests uint64
	// Exhausted reports whether a ceiling stopped the search.
	Exhausted bool
}

// Judgement is one answer: the verdict, why, what it cost, and which model
// produced it. Every result type in this package carries one.
//
// Model is not decoration. A judgement from the fake oracle and a judgement
// from the real one are the same Go value, and the only thing that
// distinguishes them is this field. A consumer that treats a certificate as
// binding checks it.
type Judgement struct {
	// Verdict is the three-valued answer.
	Verdict Verdict
	// Reason is the structured motive, and must agree with Verdict.
	Reason VerdictReason
	// Detail is free-form context for a human reading a report. It is never
	// parsed and never affects a decision.
	Detail string
	// Model names the movement model, such as ModelM1 or ModelFake.
	Model string
	// ProfileVersion is the version of the profile the answer is relative to.
	ProfileVersion string
	// Budget is what the search spent.
	Budget BudgetReport
}

// Certified reports whether the judgement certifies.
func (j Judgement) Certified() bool { return j.Verdict == VerdictCertified }

// Validate checks the judgement's internal agreement: the reason must belong
// to the verdict, a model must be named, and an exhausted budget may only
// accompany VerdictUnknown. It returns a wrapped ErrInvalidJudgement.
func (j Judgement) Validate() error {
	if j.Reason == ReasonUnspecified {
		return judgementError("reason is unspecified")
	}
	if want := j.Reason.Verdict(); want != j.Verdict {
		return judgementError("reason %s belongs to verdict %s, not %s", j.Reason, want, j.Verdict)
	}
	if j.Model == "" {
		return judgementError("model is empty")
	}
	if j.Budget.Exhausted && j.Verdict != VerdictUnknown {
		return judgementError("an exhausted budget cannot produce verdict %s", j.Verdict)
	}
	return nil
}
