// Package platform is the contract for Daedalus's side-view platform maps. It
// declares the vocabulary — movement profile, geometry, directed jump graph,
// three-valued verdict, rhythm beats — and the oracle interface that answers
// movement questions about that geometry. It implements no generator and no
// reachability algorithm; those arrive in sibling files owned by later work.
//
// # Why a separate package
//
// A top-down dungeon and a side-view platform map share deterministic random
// streams and a coordinate vocabulary, and nothing else. They share no notion
// of a Room's role, of a Corridor, or of what "connected" means. platform
// therefore imports core and the standard library, and nothing else: not the
// root package, not utils, not a physics engine.
// TestRootPackageImportsOnlyStandardLibrary enforces it, with an entry for
// this directory.
//
// # Two coordinate frames, declared once
//
// The grid keeps the core convention: cells are integers, row-major, Y grows
// downward, and the canonical cell order is Y then X. The renderer, the
// palette-indexed terrain layer and the golden fixtures all assume it.
//
// Physics does not work in that frame. Every quantity in a MovementProfile,
// every coordinate in a Witness and every Span in a Surface lives in the world
// frame: x grows to the right, y grows UPWARD, the origin is the plane's
// bottom-left corner, the spatial unit is one cell and the time unit is one
// second. The character's reference point is the centre of its feet, with an
// axis-aligned body of width 2·BodyHalfWidth and height BodyHeight above that
// point.
//
// The two frames are related by the grid's height alone. For a Grid of height
// H, grid row y covers world band [H-1-y, H-y], so the floor a character
// stands on when the cell below it is solid sits at world height H-1-y. WorldY
// and GridRow convert; nothing in this package converts implicitly.
//
// Mixing the frames is the single most likely bug in anything built on this
// contract, which is why the conversion is a named function with a test rather
// than an inline subtraction.
//
// # Sound but incomplete
//
// Every answer this package's oracle gives is a Verdict with three values.
// VerdictCertified means a witness exists: a trajectory and a command sequence
// that realise the motion inside the declared model. VerdictRejected means the
// model excludes it. VerdictUnknown means the question was not decided — the
// search budget ran out, or the manoeuvre is outside the supported moveset.
//
// The asymmetry is deliberate and is the whole point of the design. A
// certified route is real in the model. The absence of a certificate proves
// nothing: a finite enumeration of launch positions and jump timings can miss
// a jump that exists. Never report VerdictRejected or VerdictUnknown to a
// player-facing surface as "impossible".
//
// The model itself is an approximation of a real game's controller. A verdict
// is sound relative to the MovementProfile it names, not relative to any
// engine. Judgement.Model records which movement model produced the answer so
// a consumer can tell a real certificate from a fake one.
//
// # The defaults are this project's choice, not Hollow Knight's
//
// The reference point for this work is a Hollow Knight-shaped metroidvania,
// and the default profile is NOT a calibration of that game. The game's
// physical constants are not public: jump height, time to apex, whether rise
// and fall use different gravities, dash duration, wall-jump impulse, whether
// the double jump resets or adds to vertical velocity, and — decisively — the
// pixels-per-unit, are all unpublished. Without the last one no figure in
// engine units converts to a cell at all.
//
// What is published and agrees across independent sources is a handful of
// speeds in engine units per second (run 8.3, dash 20.0, Sharp Shadow 28.0)
// and two durations (dash cooldown 0.6 s, Crystal Heart charge 0.8 s). From
// those the honest inheritance is the RATIOS, which survive the missing scale:
// dash is 2.41× the run, the shadow upgrade is 1.4× the dash. DefaultProfile
// uses those two ratios and the two durations, and chooses everything else.
// Each chosen value says so in its comment. Nothing here claims parity.
//
// # Coyote time trades height for distance
//
// Coyote time is not extra reach. Leaving a ledge at τ into the window costs
// g·τ²/2 of apex height and buys V·τ of distance: the loss is quadratic and
// the gain is linear. Modelling it as "+V·Tc on the envelope" would certify
// jumps a player cannot make, which is the worst failure this project can
// produce. CoyoteHeightLoss and CoyoteHorizontalGain state the trade, and τ is
// a planning variable of the search, not a constant of the profile.
//
// # One-way is rare; conditional both-ways is the norm
//
// In the reference game's transition graph only 16 of 445 transition pairs are
// one-way, and the dominant shape among them is not irreversible geometry: it
// is the same opening traversable in both senses with a different ability
// precondition in each. Transition models that directly — Outbound and Inbound
// are separate Traversal values, each with its own required AbilitySet, and a
// nil sense is the rare genuinely irreversible case.
//
// # Determinism
//
// Everything in this package is plain data with a canonical order. IDs are
// stable indices in creation order starting at zero, and ascending numeric
// order is canonical for all of them. Nothing here iterates a map. Nothing
// here consumes randomness: the deterministic streams belong to the generator
// that uses this contract.
package platform
