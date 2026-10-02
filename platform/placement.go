package platform

import "github.com/Otoru/daedalus/core"

// Placement is the seam between the rhythm front and the macro front. The
// rhythm front produces one LOCAL grid per beat, with the departure platform
// on the left; the macro front produces rooms as hollow shells with openings
// punched in the border ring. This file stamps the former into the latter.
//
// Two rules make the stamp safe:
//
//  1. Only the room's INTERIOR is written. The border ring carries the
//     openings the macro front punched and the pairing ValidatePlane checks,
//     so a stamp that touched it could silently wall a transition shut. This
//     one is load-bearing today and its test was watched failing: with the
//     guard removed, a stamp fills in a punched floor opening.
//  2. Only non-empty cells are written. A beat's grid is mostly air, and
//     copying that air would erase whatever is already there.
//
// Rule 2 is defensive rather than load-bearing as this file stands: beats are
// laid in disjoint column ranges over an interior that starts empty, so there
// is nothing for a beat's air to erase, and removing the rule changes no map
// this package currently produces. That was measured, not assumed — removing
// it broke no generator test. It is kept because it is what makes
// stampInterior safe to call twice over the same cells, and it is therefore
// tested directly rather than through the generator, where it would pass for
// free.
//
// The run is laid left to right and chained vertically: beat i's ARRIVAL
// platform is beat i+1's DEPARTURE platform, at the same height, so the two
// halves merge into one wider ledge. That chaining is the whole reason this
// pass can produce a map the per-beat certificates do not cover: the rhythm
// front proved each step crossable in its own grid, and nothing it did proves
// the steps compose once they share a room, a ceiling and two side walls.

const (
	// synthMaxLeadingPad is the largest empty margin, in cells, left between
	// the room's left wall and the run's first platform.
	synthMaxLeadingPad uint32 = 2
	// synthMaxFloorReserve is the largest amount of air, in cells, left under
	// the run's lowest platform. A reserve lifts the whole run, which changes
	// how much headroom the room's ceiling leaves and is therefore a question
	// the oracle answers differently.
	synthMaxFloorReserve uint32 = 2
	// synthGroundFeet is the world height of the run's lowest platform when
	// the reserve is zero: the top of the room's floor shell.
	synthGroundFeet int32 = 2
)

// BeatPlacement records where one realized beat landed inside a room's grid,
// and the two standing positions the composition check uses. Coordinates are
// the room's own: Origin is a cell in the room grid, and the heights are world
// heights in that grid's frame, as WorldY defines them.
type BeatPlacement struct {
	// Beat is the index into the room's Rhythm.Beats.
	Beat int
	// Kind is the beat kind that was stamped.
	Kind BeatKind
	// Origin is the top-left cell of the stamped block in the room's grid.
	Origin Cell
	// Width and Height are the stamped block's size in cells.
	Width  uint32
	Height uint32
	// DepartureX and DepartureHeight are the centre and the world foot height
	// of the departure platform.
	DepartureX      float64
	DepartureHeight float64
	// ArrivalX and ArrivalHeight are the same for the arrival platform.
	ArrivalX      float64
	ArrivalHeight float64
}

// placeRhythm stamps a room's run of beats into the room's grid and returns
// where each one landed. It mutates room.Grid in place.
//
// It draws exactly two numbers from stream, in this order: the leading pad
// and the floor reserve. Both are drawn before anything is stamped, so a beat
// that turns out not to fit cannot change what a later room sees.
//
// It stamps the LONGEST PREFIX of the run that fits, and nothing when none
// does. A prefix rather than a greedy walk, because the run is anchored by
// its lowest platform: a descent late in the run pushes every earlier beat
// upward, so a run whose tail falls out of the room is not fixed by stopping
// at the tail — it is fixed by not promising the tail in the first place.
// Beats that did not fit are dropped and counted, never clipped: a clipped
// beat is geometry nobody certified.
//
// The run is also cut at MaxBeatsPerRoom.
func placeRhythm(room *Room, rhythm Rhythm, stream *core.SplitMix64) ([]BeatPlacement, int, error) {
	pad := int32(stream.UniformInt(0, uint64(synthMaxLeadingPad)))
	reserve := int32(stream.UniformInt(0, uint64(synthMaxFloorReserve)))

	run := mainPathBeats(rhythm)
	if len(run) > MaxBeatsPerRoom {
		run = run[:MaxBeatsPerRoom]
	}

	for length := len(run); length > 0; length-- {
		placements, fits, err := layoutRun(*room, rhythm, run[:length], pad, reserve)
		if err != nil {
			return nil, 0, err
		}
		if !fits {
			continue
		}
		for _, placement := range placements {
			stampInterior(&room.Grid, rhythm.Beats[placement.Beat].Grid, placement.Origin.X, placement.Origin.Y)
		}
		return placements, len(run) - length, nil
	}
	return nil, len(run), nil
}

// layoutRun decides where every beat of one run would go, without writing
// anything. It reports false when any beat of the run falls outside the
// room's interior.
func layoutRun(room Room, rhythm Rhythm, run []int, pad, reserve int32) ([]BeatPlacement, bool, error) {
	feet, err := runFeet(rhythm, run, reserve)
	if err != nil {
		return nil, false, err
	}

	width := int32(room.Grid.Width)
	height := int32(room.Grid.Height)
	cursorX := 1 + pad

	placements := make([]BeatPlacement, 0, len(run))
	for position, index := range run {
		beat := rhythm.Beats[index]
		grid := beat.Grid
		if grid.Width == 0 || grid.Height == 0 {
			return nil, false, nil
		}
		// lift is how far the beat's own bottom row sits above the room's
		// bottom row. It is fixed by the lower of the two platforms, which is
		// the one whose feet the beat's local frame puts on floorPad.
		lift := minInt32(feet[position], feet[position+1]) - synthGroundFeet
		offsetY := height - int32(grid.Height) - lift
		if lift < 0 || offsetY < 1 {
			return nil, false, nil
		}
		if cursorX+int32(grid.Width) > width-1 {
			return nil, false, nil
		}

		span := departureSpan(grid.Width)
		placements = append(placements, BeatPlacement{
			Beat:            index,
			Kind:            beat.Kind,
			Origin:          Cell{X: cursorX, Y: offsetY},
			Width:           grid.Width,
			Height:          grid.Height,
			DepartureX:      float64(cursorX) + float64(span)/2,
			DepartureHeight: float64(feet[position]),
			ArrivalX:        float64(cursorX+int32(span)+int32(platformGap)) + float64(span)/2,
			ArrivalHeight:   float64(feet[position+1]),
		})
		cursorX += int32(grid.Width)
	}
	return placements, true, nil
}

// mainPathBeats returns the indices of the beats on the spine's main path.
// Spur beats — a secret, a side pocket — are not stamped: they leave the
// run's column and altitude, and placing them beside the run would put
// geometry nobody planned between two platforms that have to chain.
func mainPathBeats(rhythm Rhythm) []int {
	out := make([]int, 0, len(rhythm.Beats))
	for index, beat := range rhythm.Beats {
		from, okFrom := rhythm.Spine.Node(beat.From)
		to, okTo := rhythm.Spine.Node(beat.To)
		if !okFrom || !okTo {
			break
		}
		if from.Role != SpineRoleMain || to.Role != SpineRoleMain {
			break
		}
		out = append(out, index)
	}
	return out
}

// runFeet returns the world foot height of every platform in the run: one per
// beat boundary, so the slice is one longer than the run. The run is shifted
// as a whole so its LOWEST platform sits on synthGroundFeet plus the reserve.
// Shifting the whole run rather than starting at the floor is what lets a
// descent exist at all: the spine falls twice as far as it climbs, so a run
// anchored at its first beat would walk straight out of the room's floor.
func runFeet(rhythm Rhythm, run []int, reserve int32) ([]int32, error) {
	relative := make([]int32, len(run)+1)
	lowest := int32(0)
	for position, index := range run {
		beat := rhythm.Beats[index]
		rise := worldRise(beat.FromAltitude, beat.ToAltitude)
		if err := checkBeatGrid(beat, rise); err != nil {
			return nil, err
		}
		relative[position+1] = relative[position] + rise
		if relative[position+1] < lowest {
			lowest = relative[position+1]
		}
	}
	base := synthGroundFeet + reserve - lowest
	for i := range relative {
		relative[i] += base
	}
	return relative, nil
}

// checkBeatGrid verifies that a realized beat's grid is the one the rhythm
// front's own stamp would have produced for that rise. Placement reads the
// grid's two platforms by arithmetic rather than by searching it, so a grid
// built another way would be stamped with the wrong feet, silently. This is
// the assumption written down as a check.
func checkBeatGrid(beat RealizedBeat, rise int32) error {
	fromFeet, toFeet := feetPair(rise)
	top := fromFeet
	if toFeet > top {
		top = toFeet
	}
	if want := uint32(top + headroom); beat.Grid.Height != want {
		return geometryError("beat %d..%d has grid height %d; a rise of %d cells makes it %d", beat.From, beat.To, beat.Grid.Height, rise, want)
	}
	if beat.Grid.Width < platformGap+2 || (beat.Grid.Width-platformGap)%2 != 0 {
		return geometryError("beat %d..%d has grid width %d, which is not two platforms around a gap of %d", beat.From, beat.To, beat.Grid.Width, platformGap)
	}
	return nil
}

// departureSpan is the width of one of a beat grid's two platforms.
func departureSpan(width uint32) uint32 { return (width - platformGap) / 2 }

// widestBeatCells is the width, in cells, of the widest beat the vocabulary
// can produce: two platforms of the largest declared span around the gap.
// A room narrower than this holds no beat at all.
func widestBeatCells(config Config) uint32 {
	widest := uint32(2)
	for _, definition := range config.Beats.Definitions {
		_, upper := cellSpan(definition)
		if upper < 2 {
			upper = 2
		}
		if upper > widest {
			widest = upper
		}
	}
	return 2*widest + platformGap
}

// tallestBeatCells is the height, in cells, of the tallest beat the
// vocabulary can produce. The tallest step is the longest DESCENT, because a
// descent drops twice what the same moveset climbs, and the drop is what
// makes the grid tall whichever way it points: feetPair puts the lower
// platform on floorPad and the higher one a rise above it.
//
// The moveset used is the plan's final one, because the budget grows with the
// moveset and a room has to hold the tallest run it will ever be asked for.
func tallestBeatCells(config Config) uint32 {
	rise := cellsPerAltitude * descentQuantum(ascentQuantum(config.Profile, config.Progression.Final()))
	if rise < 0 {
		rise = -rise
	}
	return uint32(floorPad + rise + headroom)
}

// clampRoomSide keeps a derived room side inside both the per-axis product
// ceiling and the given ceiling, which is the plane's side for a maximum and
// the product ceiling again for a minimum. The result is never zero.
func clampRoomSide(side, ceiling uint32) uint32 {
	if side > MaxRoomSide {
		side = MaxRoomSide
	}
	if side > ceiling {
		side = ceiling
	}
	if side == 0 {
		side = 1
	}
	return side
}

// stampInterior copies source into destination at the given offset, writing
// only cells that are not air and only cells inside the destination's border
// ring. Both restrictions are load-bearing; see the file comment.
func stampInterior(destination *Grid, source Grid, offsetX, offsetY int32) {
	for y := int32(0); y < int32(source.Height); y++ {
		for x := int32(0); x < int32(source.Width); x++ {
			kind := source.Cells[y*int32(source.Width)+x]
			if kind == CellKindEmpty {
				continue
			}
			at := Cell{X: offsetX + x, Y: offsetY + y}
			if at.X < 1 || at.Y < 1 {
				continue
			}
			if at.X+1 >= int32(destination.Width) || at.Y+1 >= int32(destination.Height) {
				continue
			}
			setCell(destination, at.X, at.Y, kind)
		}
	}
}
