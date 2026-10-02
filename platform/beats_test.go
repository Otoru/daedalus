package platform

import "testing"

// TestChallengeBeatPlatformsAreSuspended keeps challenge surfaces as ledges,
// rather than columns that silently reach the local floor. The room placer
// owns any floor beneath a beat; the beat only contributes its surface.
func TestChallengeBeatPlatformsAreSuspended(t *testing.T) {
	grid := stampBeat(BeatKindClimb, 4, cellsPerAltitude)

	suspended := 0
	for y := uint32(1); y+1 < grid.Height; y++ {
		for x := uint32(0); x < grid.Width; x++ {
			if grid.Cells[y*grid.Width+x] != CellKindSolid {
				continue
			}
			if grid.Cells[(y-1)*grid.Width+x] == CellKindEmpty && grid.Cells[(y+1)*grid.Width+x] == CellKindEmpty {
				suspended++
			}
		}
	}
	if suspended == 0 {
		t.Fatal("a climbing beat has no suspended platform cells")
	}
}
