// Package daedalus generates discrete, deterministic 2D dungeons on demand.
//
// A game requests a floor by passing a Config and a Seed to Generator.Generate,
// or to GenerateContext when it already holds a deadline. The zero Generator is
// ready to use: a nil Placer selects poisson_disk_rooms_v1 and a nil Connector
// selects prim_rooms_v1. The call returns one immutable Layout, or an error and
// the zero Layout. No Seed, stream, or Layout is kept for a later request.
//
// The package owns validation, placement, connection, thematic roles, optional
// shortcuts, orthogonal routing, door derivation, plant metadata, and
// materialization. It does not render, instantiate scenes or prefabs, load
// assets, know Godot, Unity, or Bevy, compute pixel positions, build a
// navmesh, or hold game state. CellSize is an opaque unit defined by the
// caller and is only copied onto the Layout; no algorithm converts it. There
// is no configuration file: Config is a Go value here, and a process outside
// this package may carry the same request as a protobuf message. Each phase
// builds local values and publishes a Layout only after every phase succeeds,
// so there is no partial public result for a caller to patch. The root package
// imports the Go standard library and the daedalus/core package, and nothing
// else. core holds the perspective-agnostic primitives — the SplitMix64
// streams and their frozen salts, Seed, Cell, and Direction — and itself
// imports only the standard library, so a sibling generator can share them
// without taking on the dungeon vocabulary.
//
// The names Layout, Room, Corridor, Door, Grid, Cell, Config, Seed, Placer,
// and Connector are normative public vocabulary. Renaming any of them is a
// breaking API change.
//
// # Vocabulary
//
// A Cell is an integer (X, Y) coordinate on the Grid, never a pixel. A Grid is
// a rectangle of Width by Height Cells, with 0 ≤ X < Width and 0 ≤ Y < Height.
// Grid.Cells is that rectangle in row-major order, Y then X, and each CellState
// is Empty, Room, or Corridor. A Room Cell names exactly one Room. A Corridor
// Cell names exactly one Corridor. Two Corridors never share a Cell. They also
// never sit within Chebyshev distance 1 of each other, diagonal contact
// included, beside a Room wall the same as in open ground. The measure is the
// one MinRoomGap uses between Rooms. An Empty Cell names neither.
//
// A Room is a topological vertex whose footprint is a finite, non-empty,
// 4-connected set of Cells. The footprint does not include Doors or Corridors.
// Room.At, the anchor, is the first occupied Cell in canonical Y-then-X order.
// Room.Origin is the top-left corner of the bounding box and may itself be
// empty, as it is for a Cross. Width and Height are that box, in Cells.
// Room.Cells is the absolute footprint, copied into the Layout.
//
// A Corridor is both a topological edge and the orthogonal Cells walked
// outside every footprint, from the exterior neighbour of the source Door to
// the exterior neighbour of the destination Door. Those Cells are omitted when
// two Doors face each other directly and no Room Cell is crossed. Centerline
// is that route. When CorridorGeometry is nil, Cells is the same route and
// every Door spans one Cell. When it is set, Cells is the occupied band. A
// Door is a logical opening on a boundary Cell of a Room, identified by
// RoomID, At, and a cardinal Direction that points out of the Room. Door.Span
// is the routed width and is never zero; At is the Cell of that span with the
// smallest (Y, X). Door.CorridorIDs lists the edges that use the opening, in
// ascending ID order. A later Corridor cannot enter an opening whose exterior
// Cells already belong to an earlier band, so the list has one ID except when
// two edges both have an empty band because their Doors face each other: there
// is then no Cell for the halo to claim, and the same Door serves both.
// Diagonals are invalid. The canonical Direction order is North, East, South, West.
//
// A Layout is the complete successful result: the Seed that produced it, the
// Grid, and the Rooms, Corridors, and Doors in creation order. IDs start at 0
// and stay stable. A PlantID is an opaque UTF-8 token the game resolves to a
// scene, prefab, tile, or mesh; this package stores the token and its tags and
// never interprets them. An absent catalog leaves PlantID and Tags empty.
//
// Config is the whole request. Width, Height, and Seed are required and have
// no safe default. Seed is a uint64; every value is accepted, and zero is a
// real seed rather than a request for entropy. The remaining fields document
// their own defaults. The zero Config is not valid.
//
// # Room geometry
//
// Rooms vary in size and shape. RoomShape names five canonical masks, with no
// implicit rotation. Rectangle fills its bounding box. L joins the full top
// row to the full left column and needs both dimensions at least 2. T joins
// the full top row to the center column, needs width at least 3 and height at
// least 2, and places that column at (Width-1)/2. Cross joins the center row
// to the center column and needs both dimensions at least 3; an even dimension
// uses the same (N-1)/2 index, so the geometric middle of an even span is the
// lower of the two central lines. Circle is a square of odd diameter D at
// least 5. With r = (D-1)/2 and center (r, r), a Cell is occupied exactly when
// (x-r)^2 + (y-r)^2 <= r^2, compared in integers with no rounding. Every mask
// is a set, so the union does not duplicate a Cell, and every mask is
// 4-connected.
// Offsets are relative to Origin and are materialized in Y-then-X order. The
// first occupied offset is (0, 0) for Rectangle, L, and T, ((Width-1)/2, 0)
// for Cross, and (r, 0) for Circle, which is what lets an anchor be turned
// back into an Origin without ambiguity.
//
// RoomGeometry sets the maximum footprint area, the minimum gap between
// footprints, and one positive weight per shape. Width and height are not
// fields of RoomGeometry; each RoomShapeWeight carries its own inclusive
// DimensionRange for width and for height. A nil RoomGeometry does not mean
// one-Cell Rooms. After validation it normalizes to the dynamic profile, and
// that normalized profile is part of the effective Config. Each axis is
// clamped to the Grid as min(profile minimum, side)..min(profile maximum,
// side). A shape whose clamped range admits no legal mask is omitted.
// Rectangle always remains, including the 1×1 mask, so a 1×1 Grid yields one
// Rectangle Room and no Corridor or Door.
//
// The dynamic profile, before that clamp, is:
//
//	Rectangle  width 3..9  height 3..9  weight 4
//	L          width 3..9  height 3..9  weight 2
//	T          width 3..9  height 3..9  weight 2
//	Cross      width 3..9  height 3..9  weight 1
//	Circle     width 5..9  height 5..9  weight 2
//
// MaxFootprintCells is 81 and MinRoomGap is 1. Circle draws only odd equal
// diameters inside its range, so 5, 7, and 9 are the candidates when the Grid
// allows them. L accepts a side of 2 and T accepts a height of 2, but the
// profile starts at 3, the same lower bound Rectangle and Cross use. The
// profile drops L, T, or Cross only when the Grid itself is below that
// shape's mask minimum. Circle starts at 5 because a disc on a square Grid
// needs an odd diameter of at least 5 with a centre Cell. A 6×6 circle is
// not a size the mask can take.
//
// An explicit RoomGeometry requires MaxFootprintCells (1..4096), MinRoomGap
// (0..256), and a non-empty, duplicate-free Shapes list whose weights are at
// least 1. On the Go value every shape carries a width range and a height
// range. Three failures are distinct. A range whose Max is 0, or whose Min
// exceeds its Max, is ErrInvalidConfig: the span itself is empty, and the
// message says the maximum is 0 or the minimum exceeds the maximum. A span
// that is well formed but contains no size ValidRoomShapeDimensions accepts
// for that shape is also ErrInvalidConfig, and the Grid is not consulted;
// Circle with width 6..6 is the case, because 6 is even, and the message
// says the range admits no legal size. A span that is legal for the shape
// but has no size that fits this Grid drops that shape. It is not that
// error. Rectangle stays when its own range still fits, which is what keeps
// the default profile from handing placement an empty catalog. If every
// shape is dropped, or none of the remaining sizes fit MaxFootprintCells,
// the Config is ErrInvalidConfig and the message says the geometry has no
// valid combination within the area.
//
// An omitted width or height on the wire is not a zero DimensionRange.
// Conversion fills DefaultDimensionRanges for that shape and that Grid: the
// clamped profile when that profile is a legal mask on the Grid, and the
// unclamped profile otherwise, so validation can drop a shape the Grid
// cannot hold instead of rejecting {0, 0}. A present range, including an
// explicit 0..0, is kept and validated as written. Width and height are
// independent: omitting one fills only that axis.
//
// L, T, Cross, and Circle still obey the mask minima above, and an even
// Circle diameter inside an otherwise legal range is simply not a candidate.
// A Room is accepted only when its anchors satisfy MinDistance and its
// footprints satisfy MinRoomGap at the same time. Neither check replaces the
// other.
//
// MinRoomGap counts empty layers by Chebyshev distance between occupied Cells
// of different Rooms. Gap 0 forbids overlap and still allows edge contact,
// including a diagonal touch. Gap 1 demands a full empty layer, diagonal
// included. The default profile uses 1. The dynamic profile keeps MinRoomGap
// 1. A Corridor wider than the gap between two Rooms cannot pass between them
// and degrades, so a caller asking for wide Corridors should raise MinRoomGap
// to match. That is documentation, not validation: Rooms are not everywhere,
// and a wide Corridor can often route around.
//
// # Corridor geometry
//
// CorridorGeometry sets the width distribution for Corridors. A nil
// CorridorGeometry means every Corridor is one Cell wide, which is the
// historical behaviour: the width stream is never consumed, Centerline and
// Cells are the same ordered route, and every Door spans one Cell. An explicit
// value requires Widths to be non-empty and duplicate-free. Each Width is
// 1..64 and each Weight is 1..2^32-1. A width above 64 is ErrInvalidConfig,
// not ErrLimitExceeded: it is a nonsense value, not a product limit.
//
// A non-empty catalog must declare width 1. Degradation walks declared widths
// and nothing else, so a catalog without 1 gives a Room whose perimeter admits
// no wider Corridor nowhere to fall back to: every one of its edges is
// unroutable and the whole call ends in ErrUnconnectablePlacement naming that
// Room, when the fault is the catalog. The catalog is rejected up front with
// ErrInvalidConfig instead. Declaring 1 removes that one failure mode. It does
// not promise the request generates: corridor separation can still leave a
// Room unconnectable.
//
// One draw is taken per Corridor, in creation order, immediately before that
// Corridor is routed. Candidates are sorted by Width ascending before the
// draw, so the order of Widths in the request cannot change the result. The
// draw uses the same weighted selection as the Plant catalog: a ticket from 1
// to the total weight, then the first candidate whose cumulative weight
// reaches that ticket. If the drawn width W cannot be routed, degradation
// walks the declared widths that are strictly narrower than W, descending, and
// nothing else. With [1, 3], a 3 that does not fit falls straight to 1, never
// to an undeclared 2. The first declared width that routes wins. Because 1 is
// always declared, the walk always ends at a one-Cell attempt. Only when every
// remaining declared width fails, 1 included, is the error ErrUnroutableEdge.
// Degradation consumes no further randomness.
//
// The centerline is the route as computed for a one-Cell Corridor. The band is
// the centerline dilated perpendicular to the direction of travel. Odd W:
// symmetric, (W-1)/2 Cells each side; even W: the extra Cell goes to the +X
// side for vertical travel and the +Y side for horizontal travel. At every
// bend, the band includes the full W×W block centred by the same rule, so the
// corner does not leave a diagonal pinch and 4-connectivity holds at the
// vertex. A W-wide route is legal only when the entire band is inside the Grid
// and crosses no Room footprint. Corridors are routed in connection order.
// Once a band is placed, that band is closed to every later Corridor, and so
// is every free Cell within Chebyshev distance 1 of it — the same measure
// MinRoomGap uses between Rooms — including Cells that sit on a Room wall.
// Two Corridors therefore never share a Cell and never touch, diagonally
// included. The clearance map and the breadth-first fallback both treat that
// set as obstacles.
//
// Cells is that band in row-major order. Centerline is the ordered
// 4-connected route from the From side to the To side. A W-wide Corridor
// meeting a Room opens a W-wide doorway. Door.Span is the number of boundary
// Cells, and Door.At is the Cell with the smallest (Y, X) of the span. If the
// Room's usable boundary run is shorter than W, the Corridor degrades along
// the declared list, not along the integers. A Door with Span 1 is the
// one-Cell doorway.
//
// # Pipeline
//
// Generation always runs in the same order. Validation and normalization come
// first, before any stream is drawn and before any Grid is allocated. The
// Placer then proposes RoomPlacements, and every Cell of an accepted footprint
// is reserved. The Connector proposes topological edges. Requested thematic
// roles are assigned on that backbone. A RoomRoleRequest.MaxRoomEdges value
// greater than zero filters candidates by their degree in the Connector graph;
// Boss and Treasure therefore mean the farthest eligible Room. Optional
// shortcuts are reintroduced
// from the short edges the backbone discarded. Each edge is traced as an
// orthogonal Corridor. Doors are derived from the ends of those routes. Only
// then are optional plant metadata resolved and an immutable Layout
// materialized. A failure in any phase discards the private work and returns
// the zero Layout.
// A non-zero RoomRoleRequest.MaxRoomEdges is a per-role ceiling on the number
// of Corridors reaching an assigned Room; zero leaves the geometric opening
// budget in charge. NewLoopConnector is an opt-in SDK-only Connector that
// starts with a ring and grows branches, and costs about six times the default
// Prim generation at the largest measured load. It never changes the nil
// Connector default.
//
// The built-in Placer is a Poisson disk over anchors, not over footprint
// Cells. One geometry draw selects the first Room. Normalization has already
// dropped every shape and size that cannot sit on the Grid, so that draw has
// a legal placement. Among anchors where the mask lies inside the Grid, it
// keeps the one whose squared distance to the geometric center is smallest,
// breaking ties by Y then X. That center is the discrete point (Width-1)/2,
// (Height-1)/2, compared in doubled integer coordinates so the choice does not
// depend on floating-point rounding. Later Rooms grow from active anchors. One
// draw picks the active index once per outer iteration. Each of up to
// MaxAttempts attempts then draws offsetX and offsetY in the annulus around
// that anchor and quantizes with floor. Geometry comes from a separate stream
// and is drawn only after the anchor lands inside the Grid. The candidate is
// committed only when, in order, the mask is valid, the bounding box lies
// inside the Grid, the area is within MaxFootprintCells, the anchor is far
// enough from every accepted anchor, the footprint does not overlap, and every
// pair of occupied Cells respects MinRoomGap. Acceptance is atomic: a rejected
// attempt occupies nothing and consumes at most one geometry draw. An anchor
// that falls outside the Grid consumes no geometry draw.
//
// The annulus is uniform by area. Each axis is uniform01()*4r-2r, because the
// square that encloses the ring is [-2r, 2r] and uniform01 lies in [0, 1),
// which matches the exclusive outer bound. The point is kept only when
// r^2 <= offsetX^2 + offsetY^2 < 4r^2. Because offset = r*(4u-2), dividing
// that test through by r^2 makes it independent of r, which is why a
// DensityRegion can change the accepted distance without moving a single
// draw. Pairs that fall outside the ring are rejected inside the sampler and
// do not consume a geometry attempt or a MaxAttempts slot. The multiply and
// the subtract stay in separate statements, so a fused multiply-add cannot
// move the candidate between architectures. There is no sine or cosine.
// Distance checks compare squares of int64 Cell deltas, so they do not call a
// square root either.
//
// Without DensityRegions the required distance is MinDistance everywhere, and
// the temporary acceleration grid uses the classic 5×5 Bridson neighbourhood.
// A DensityRegion replaces that distance inside a half-open rectangle: Min is
// inclusive and Max is exclusive, covering X in [Min.X, Max.X) and Y in
// [Min.Y, Max.Y). Max may therefore equal the Grid dimension, a larger Max is
// invalid, and a one-Cell region is written Max = Min + (1, 1). Regions do
// not overlap. An anchor outside every region keeps MinDistance. The distance
// required between two anchors is the maximum of their two local distances, so
// a Room sitting on the sparse side of a boundary does not crowd its
// neighbour. Empty regions are not consulted at all and add no draws; the
// uniform Poisson path runs unchanged.
//
// MaxAttempts bounds complete proposals per active point, including those
// rejected for distance, shape, bounds, or collision. After that many failures
// the active point is removed. MaxRooms is a ceiling, not a request to fill
// the Grid. Each iteration either accepts one new anchor or retires one, and
// anchors are unique Cells, so placement terminates. A footprint is never
// truncated to make it fit.
//
// The built-in Connector builds a spanning tree with Prim, starting at
// RoomID 0. The candidate graph is complete. Edge weight is Euclidean distance
// between bounding-box centers, even though routes are orthogonal. The center
// is the centroid of the box's Cells, Origin + (dimension-1)/2, not the area
// center Origin + dimension/2. A 1×1 Room weighs the same as the distance
// between anchors only with that offset, which is zero when the dimension is
// 1. The two formulas disagree as soon as a dimension is greater than 1, and
// they produce different trees. Equal weights break ties by
// FromRoomID, then ToRoomID, then the destination Room's anchor in canonical
// Cell order. Comparing squared distances preserves that order, because the
// square root is strictly increasing on non-negative values, and it avoids
// another rounding step on the frozen path. Prim receives its stream and
// consumes no draw.
//
// Generator installs ConnectionRequest.TryRoute and RewireRoute before Connect.
// TryRoute routes the pair against corridors already reserved and, on success,
// reserves that band and its one-Cell Chebyshev halo, beside a Room wall the
// same as in open ground. Failure reserves nothing and consumes no width draw.
// prim_rooms_v1 still considers candidates cheapest first, with the tie-break
// above, and accepts an edge only after TryRoute succeeds. A pair that does
// not fit is marked infeasible, in both orientations, and the next candidate
// is taken. That loss is not an error. The tree is greedy under this
// routability constraint. It is not a minimum spanning tree: a cheaper edge
// that cannot be routed gives way to a longer one that can, and the total
// weight is not the minimum. It does not minimize routed Cells either. A Room
// with no routable edge left returns ErrUnconnectablePlacement, naming that
// Room. An edge that only lost to another candidate must not surface as
// ErrUnroutableEdge.
//
// Before the first edge, each Room's opening budget is computed once from its
// footprint and from ConnectionRequest.MaxCorridorWidth. Generator sets that
// field to the narrowest declared Corridor width, or 1 when CorridorGeometry
// is nil. The budget answers whether the Room can host a Corridor at a width
// degradation can always reach. Using the widest declared width instead
// reports a circle, and a cross smaller than 6, as having no opening at all:
// their stepped perimeters have no straight run of three Cells facing one
// direction, so Prim would refuse every edge even though width 1 fits and is
// declared. An opening claims that many contiguous boundary Cells on one
// straight side. Two openings on the
// same side need at least two unused boundary Cells between them, and openings
// on adjacent sides are incompatible when their outside Cells would lie within
// Chebyshev distance 1, so corners count. Config.MaxRoomEdges, copied onto
// the same request, lowers that budget when it is set: the number used is the
// smaller of the geometric count and the caller's ceiling. The ceiling never
// replaces the perimeter, so a Room that can host two openings still hosts two
// when the caller asks for six. Zero means unlimited, and the geometric count
// is then the only ceiling, which is why an omitted field and an explicit zero
// produce the same Layout. Prim still takes the cheapest edge
// under the tie-break above, but only among edges that would not exceed either
// endpoint's remaining budget and that TryRoute accepts. A greedy choice can
// fill every Room already in the tree and leave another Room unvisited. That
// Room is spliced into the tree: the nearest in-tree Room is chosen with the
// same ordering, one of its existing edges is removed, and the stranded Room —
// which must be able to host two openings — is inserted between those two
// endpoints. The two Rooms that were already joined keep the same degree.
// RewireRoute releases the removed corridor and reserves the two replacements;
// if they do not fit, that splice is skipped and the next candidate in the
// same order is tried. If no splice reserves, Connect returns
// ErrUnconnectablePlacement. The tree is always the backbone. The Layout is a
// tree only when ExtraEdgeCount is 0.
//
// TryRoute is nil when Connect is invoked outside Generator. prim_rooms_v1
// then keeps the opening-budget tree and does not consult the router. A
// game-supplied Connector may call TryRoute or ignore it. Ignoring it keeps
// propose-then-route: Generator routes each returned edge in order and returns
// ErrUnroutableEdge when one does not fit, because that Connector never
// negotiated. Edges it did reserve stay reserved, in call order.
//
// Thematic roles are a projection onto that tree, not a third plugin. If Start
// is requested it is RoomID 0, the first Room accepted, which is also the one
// nearest the center. Boss, which requires a Start because distance without an
// origin is undefined, is the unassigned Room with the greatest weighted path
// distance from Start; equal distances take the smaller RoomID. Each Treasure
// request, in request order, places up to Count Rooms by farthest-point
// sampling, so they sit far from Start, far from Boss, and far from each
// other. The anchors begin as the Start and Boss Rooms already assigned —
// request order is what decides which of those exist — and grow with every
// Treasure just placed. The next Treasure is the unassigned Room whose minimum
// weighted path distance to any anchor is greatest. That distance is the same
// weighted backbone path Boss uses. Equal distances take the smaller RoomID.
// When neither Start nor Boss has been assigned, the anchor set is empty and
// the first Treasure is the unassigned Room farthest from RoomID 0, the first
// accepted Room; each later Treasure then disperses from the Treasures already
// placed. A Room that already holds a role is never chosen again. If Count
// exceeds the Rooms still free, the request fills those Rooms and stops, and
// the shortfall is not an error. RequiredTags restrict the Plant chosen later
// and do not move the Room. Roles are assigned before shortcuts so that "far
// from the start" still means the exploration tree the backbone planned.
// Later cycles must not pull the Boss into an artificially short hop. An
// empty role list leaves every Role absent and consumes no draw.
//
// When ExtraEdgeCount is greater than zero, the Generator takes every edge of
// the complete graph that is not already in the tree, orders those edges by
// increasing Euclidean distance and the same Prim tie-break, and appends
// candidates in that order until ExtraEdgeCount of them have been kept or the
// list runs out. A candidate that would put either Room past its opening
// budget is skipped, and the next candidate is considered. A candidate that
// TryRoute refuses is skipped the same way. The shortcut budget does not
// shrink because an earlier candidate did not fit, and that miss is not
// ErrUnroutableEdge. The choice is ordered, not random. Zero skips the phase
// entirely. A width draw happens only for a shortcut that is actually reserved.
// The graph stays connected either way; extra edges are the only way it gains
// a cycle. Fewer shortcuts than requested is a successful Layout when the
// budget runs out. Shortcuts spend the same opening budget as the backbone,
// including a MaxRoomEdges ceiling. A role ceiling of exactly 1 is legal and
// selects a leaf when one is available; the global Config.MaxRoomEdges still
// retains its separate minimum of 2. A ceiling of exactly 2 is legal and
// worth noticing: every Room holds at most two Corridors, so the backbone is
// a single unbranched chain. That is a usable floor. The only shortcut that
// can still fit joins the two ends of that chain; any further cycle does not
// fit, and the call still succeeds with fewer cycles than ExtraEdgeCount
// asked for.
//
// Routing then enumerates, for each edge, every opening (RoomID, At,
// Direction) whose Cell belongs to the footprint, whose Direction is cardinal,
// and whose neighbour Cell lies inside the Grid and outside that same
// footprint. It keeps the pair whose route has the fewest Corridor Cells,
// then breaks ties by the source Door and the destination Door in
// (At.Y, At.X, Direction), then by the route in lexicographic (Y, X) order.
// A pair is pruned only when its lower bound is strictly greater than the
// best cost so far. A bound that equals that cost can still tie and win on
// the origin-Door tie-break, so greater-or-equal is not a reason to stop.
// The preferred bend is the configured CorridorOrder, X-then-Y by default; the
// other bend is the alternative. If both L-routes cross a footprint or leave
// the Grid, a deterministic breadth-first search runs over free Cells.
// Neighbours expand North, East, South, West. The queue is FIFO. A Cell is
// marked when it is inserted, not when it is removed, and the parent is the
// one from first discovery. Storage is row-major. There is no map iteration,
// so the path does not depend on hash order. When CorridorGeometry is set and
// the width is greater than one, both L-routes and the breadth-first fallback
// keep a Cell only when its clearance admits that width, so the whole band
// stays inside the Grid and off every Room footprint. Width 1 uses the same
// one-Cell search. A placed band is an obstacle for both searches, and so is
// its one-Cell Chebyshev halo, with no exception for a Room wall. The
// same opening is reused only when an earlier Corridor left no Cells to
// reserve, which is the adjacent facing case; otherwise each Door names one
// Corridor. Door.At need not equal Room.At. Only when no pair
// has a route does the call fail. The router is not a public Strategy: edge
// selection is the extension point, and tracing a route is a fixed reading of
// those edges.
//
// Plant selection runs after Doors exist, because a Room plant must list every
// Direction the Room actually uses. Several Doors in one Direction count once.
// If the Room has a role, the plant's tags must also contain that role's
// RequiredTags. Candidate IDs are sorted before the weighted draw, so the
// order of the catalog in the request does not change the result. Corridor
// plants are chosen the same way, without a direction constraint. The game
// may ignore PlantID.
//
// # Placer and Connector
//
// Placer and Connector are the only two Strategy interfaces in v1. Each has a
// function adapter, PlacerFunc and ConnectorFunc, so a plain function or a
// closure is injectable without declaring a type. Placer returns
// RoomPlacements and nothing
// else: no Plants, no Doors, no IDs, and no mutation of a Layout. Connector
// returns edges and does not write Corridor Cells onto the Layout. The built-in
// asks TryRoute to reserve a route before it accepts an edge. An injected
// Connector that leaves TryRoute untouched still returns edges, and Generator
// routes those edges afterwards. Generator checks
// every placement against the normalized geometry and the Grid, and it rejects
// a connection that is duplicated, a self-loop, unknown, or disconnected.
// With ExtraEdgeCount 0 the accepted graph must be a tree. Above that,
// Connector may return extra edges up to the budget. Each edge beyond the
// n-1 backbone consumes one shortcut, and Generator adds only the remaining
// discarded short edges, in the same deterministic order. prim_rooms_v1
// returns the tree alone, so the whole budget is filled in that later phase.
//
// poisson_disk_rooms_v1 and prim_rooms_v1 are the built-ins, and they are what
// the gRPC service and the debug HTTP server run. An injected plugin exists
// only in the SDK. The plugin owns its determinism, its observation of
// Context, and its safety under concurrent calls. This package does not
// serialize a shared plugin, and it does not register, version, or detect one.
// A caller that needs mutable plugin state should use separate Generators or
// synchronize inside the plugin. A plugin that is not deterministic makes the
// Layout not deterministic; that is the plugin's contract, not a failure of
// the built-in freeze.
//
// # Determinism
//
// For the built-in algorithms, the same effective Config and the same Seed
// reproduce a Layout bit for bit on every supported platform, for the whole of
// major version v1, in the SDK and over gRPC. Effective includes the
// normalized RoomGeometry, so an omitted geometry and an explicit copy of the
// dynamic profile are the same request. amd64 and arm64 are both in that
// promise. CellSize is frozen only as the value copied into the Layout; it
// does not move a Room.
//
// Six independent SplitMix64 streams are derived from the Seed, each as
// Mix64 of the Seed xor a frozen salt: PlacementSeed, ConnectorSeed,
// RoomPlantSeed, RoomGeometrySeed, CorridorPlantSeed, and CorridorWidthSeed.
// Consuming one does not advance the others. A nil CorridorGeometry consumes
// no draw from CorridorWidthSeed, so a Config without that field reproduces
// the Layout of the one-Cell routes. Placement draws the active index once per outer
// iteration, then offsetX and offsetY on each attempt. Geometry is a separate
// stream, drawn only after the anchor lands inside the Grid: the shape, by
// positive integer weights in canonical shape order, and then a dimension pair
// from that shape's combinations sorted by width then height. Plant draws run only when a catalog is present. Prim,
// role assignment, and shortcut selection consume nothing, so enabling roles
// or shortcuts does not shift the placement sequence. uniformInt is
// inclusive and rejects samples to avoid modulo bias. uniform01 lies in
// [0, 1).
//
// The generation path does not call sine, cosine, or any other libm
// trigonometry. Products and sums that affect a candidate, a distance, a
// weight, a priority, or a tie-break are separate statements, with an explicit
// float64 conversion of the intermediate where a fused multiply-add would otherwise
// differ between architectures. Discrete coordinates and squared Cell
// distances use int64, which covers the v1 Grid limits. What stays frozen is
// the observable result: masks, dimensions, footprints, door positions,
// corridor Cells, role assignment, field values, and sequence order. Allocation
// strategy, the private acceleration grid, logs, and wire layout may change
// inside v1 when none of those results change.
//
// A calibration or a bugfix that changes frozen output is a new major version,
// or a new algorithm ID such as a future poisson_disk_v2 while
// poisson_disk_rooms_v1 stays put. Silent updates of the built-in Layout are
// not part of the contract. Pinning the module version is what keeps a replay
// stable across releases.
//
// # Errors
//
// Five sentinels name the SDK failure categories. They are wrapped with
// context, and callers distinguish them with errors.Is. None of them is
// accompanied by a partial Layout.
//
// ErrInvalidConfig reports a request that violates a range, numeric
// finiteness, catalog shape, density region, role request, RoomGeometry, or
// CorridorGeometry. A Corridor width outside 1..64 is this error, not
// ErrLimitExceeded. MaxRoomEdges of 1 is this error as well: one Corridor per
// Room spans exactly two Rooms, so a larger floor cannot be connected. Zero
// is unlimited and is not an error.
// ErrLimitExceeded reports a request past a v1 product limit: a Grid dimension
// above 256, a Width×Height above MaxCells, a MaxRooms above MaxRooms, or a
// RoomGeometry.MaxFootprintCells above MaxFootprintCells. Both are detected
// before allocation, generation, or any stream consumption, and the request is
// never silently truncated. ErrNoCompatiblePlant reports a valid Config whose
// catalog has no Room plant that supports a Room's Directions and the assigned
// role's required tags. ErrUnconnectablePlacement reports a footprint set that
// cannot host a spanning tree once every Corridor keeps a one-Cell Chebyshev
// gap. The built-in Connector returns it when a Room has no routable edge
// left. ErrUnroutableEdge reports an edge the Connector returned without a
// reservation and for which both L-routes are blocked and the breadth-first
// search finds no orthogonal path. A candidate the built-in skipped is not
// this error.
//
// Cancellation and deadlines are not sentinels. GenerateContext returns
// context.Canceled or context.DeadlineExceeded, still with the zero Layout.
// The gRPC service maps ErrInvalidConfig to InvalidArgument, ErrLimitExceeded
// to ResourceExhausted, a deadline to DeadlineExceeded, and caller
// cancellation to Canceled.
//
// # Limits and latency
//
// A player is waiting for the floor, so the limits are part of the product
// rather than a later throughput exercise. MaxCells is 65,536, the largest
// Width×Height, and each side stops at 256. MaxRooms is 256 accepted Rooms,
// not 256 occupied Cells. MaxFootprintCells is 4,096 Cells in one Room when
// the caller sends a RoomGeometry; the default profile uses 81, which is the
// 9×9 rectangle. Exceeding a product limit returns ErrLimitExceeded.
//
// Three release budgets describe the loads the built-in path is sized for, on
// the declared reference hardware. A small load is 64×64 with MinDistance 6,
// MaxAttempts 30, and MaxRooms 128, and its SDK generation p95 is at most
// 20 ms. A typical load is 128×128 with the same distance and attempt cap and
// MaxRooms 256; p95 is at most 50 ms and p99 at most 100 ms. The maximum v1
// load is 256×256 with MinDistance 1, MaxAttempts 1,024, MaxRooms 256, dynamic
// geometry, footprints, gap, cycles, roles, and DensityRegions all enabled;
// p95 is at most 500 ms and p99 at most 1 s. Shared CI records regressions. It
// is not the authority for those absolute numbers.
//
// The built-ins observe Context at phase boundaries and at least every 256
// candidate attempts or Prim key updates. An expired Context discards private
// work and returns the cancellation or deadline error.
//
// # Concurrency
//
// The zero Generator, the built-in Placer and Connector, and GenerateContext
// are safe for simultaneous calls. Every invocation allocates its own streams,
// acceleration grid, visited sets, scratch buffers, and result slices. No
// Seed, active point, catalog draw, or partial Layout is shared, and mutating
// a returned slice cannot change another request's Layout. Cancelling one call
// does not disturb another. A caller-supplied plugin that is shared across
// goroutines has to provide that same guarantee itself.
//
// # Tuning
//
// Defaults are chosen so that an ordinary floor has separated, multi-Cell
// Rooms and a tree of corridors, and so that zero means "leave the documented
// default", not "disable the mechanism", except where a count really is a
// count. MinDistance defaults to 6 Cells between anchors: raising it spreads
// Rooms out and usually accepts fewer of them; lowering it packs anchors and
// spends more time rejecting collisions. MaxAttempts defaults to 30 proposals
// per active point: raising it searches harder locally, lowering it gives up
// sooner and leaves a sparser floor. MaxRooms defaults to the product ceiling
// of 256 and stops placement early when set lower. CellSize defaults to 1 and
// never changes topology.
//
// CorridorOrder defaults to X-then-Y. The other value swaps the preferred
// elbow wherever both L-routes are legal; it does not change the Rooms.
// CorridorGeometry defaults to nil, which keeps every Corridor one Cell wide.
// Naming a width distribution draws one width per Corridor and, when that
// width does not fit, degrades only through the other declared widths. The
// dynamic profile keeps MinRoomGap 1. A Corridor wider than the gap between
// two Rooms cannot pass between them. Once placed, a band keeps a one-Cell
// Chebyshev halo clear of every later Corridor, beside a Room wall as well as
// in open ground. A caller asking for wide Corridors should raise MinRoomGap
// to match. The Connector budgets openings at the narrowest declared width,
// so a Room that can host that width is not refused; the router still places
// only a declared width and degrades downward when a wider one does not fit.
// On a crowded Grid the built-in may return ErrUnconnectablePlacement when a
// Room has no routable edge. An injected Connector that returns an edge it did
// not reserve may still receive ErrUnroutableEdge. Neither miss is a cue to
// invent a narrower width.
// ExtraEdgeCount defaults to 0, which keeps the backbone a tree. Raising it
// adds the shortest discarded edges and the cycles those edges create.
// MaxRoomEdges defaults to 0, which means unlimited: the perimeter is the only
// ceiling. Setting it to 2 forces the unbranched chain described above.
// Shortcuts draw on that same ceiling, so a low value with a large
// ExtraEdgeCount simply leaves the cycles that do not fit unplaced.
// RoomRoleRequests defaults to empty. A Treasure count of zero asks for no
// treasure Rooms. DensityRegions defaults to empty, which is the uniform
// Poisson path; a region with a smaller local distance packs that rectangle,
// and a larger one opens it up, while a pair that straddles the boundary
// still obeys the greater of the two local distances.
//
// Shape weights in the default profile are 4, 2, 2, 1, and 2 for Rectangle,
// L, T, Cross, and Circle. A higher weight makes that mask more common among
// geometry draws. Zero is invalid. Plant weights behave the same way inside a
// catalog. None of these knobs, and none of the role, shortcut, or density
// options, shifts the draws of another stream.
//
// # Companion utilities
//
// The root package owns generation, not gameplay queries. utils/pathfinding
// computes entry-cost distance fields, utils/vision computes visibility with
// symmetric shadowcasting, and utils/gating builds and validates a solvable
// progression plan over a completed Layout. The service exposes the latter as
// BuildGatingPlan; all three utilities are detached and leave the Layout alone.
package daedalus

// Terrain patch algorithm (normative v1 contract)
//
// When TerrainConfig is non-nil, Daedalus visits eligible Room and Corridor
// Cells in row-major order and makes a seed slot every MaxPatchCells Cells of
// that base kind. A seed slot draws one weighted terrain label; a zero label
// makes no patch. A non-zero label draws an integer target in the inclusive
// MinPatchCells..MaxPatchCells range and grows by FIFO breadth-first search
// until that target is reached. Expansion enqueues every eligible neighbour
// in strict North, East, South, West order; ties are therefore deterministic.
// It stops at the Grid boundary, a claimed Cell, a different base kind, or a
// protected connectivity-spine Cell. A protected seed may receive passable
// terrain but cannot expand. The seed schedule, target draw, queue order, and
// clipping rule fully determine each patch from Config and the terrain stream.
// All weights, totals, and decisions use integer arithmetic. A protected Cell
// never draws an impassable candidate. There is one terrain byte per Cell and
// no stacking; callers wanting combined effects must declare one composite ID.
// The wire representation mirrors this exactly: Config.terrain is field 16,
// Grid.terrain is field 5, and palette plus dense indices are omitted when
// the SDK Terrain pointer is nil. This keeps unset-terrain Layout JSON and
// GenerateResponse bytes unchanged; the service copies the layer instead of
// inferring it from CellState.
