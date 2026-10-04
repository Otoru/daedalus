# Terraced synthesis

Platform synthesis uses one generation path in the SDK and the debug interface.
It needs no algorithm selector.

Adjacent beats share their common landing, so the room can hold more of the
planned run. The room gains short foundations, broad solid outcrops and shaped
stepping stones, spaced according to the movement profile's jump height and body
clearance. An eligible wall-jump room gains an optional opposing-face chimney;
an eligible climb room gains a floor-anchored rope. Existing geometry and
border openings are preserved, including body-height air above beat ledges.

Seating, the final motion graph, run composition and progression audits run
after placement. A verdict still describes those checks, not a promise that
every optional outcrop participates in the main route. Profiles without enough
jump height for an additional storey receive no extra relief.

The debug graph is an inspection overlay: enable **Surface connections**, then
select a platform, wall or rope. The ability switches expose gated links; the
preview shows a bounded mix of movement kinds while the text reports the full
outgoing count. Dashed arrows indicate connectivity, not physical trajectories;
the wire graph does not include motion witnesses.

Regression checks:

```sh
go test ./platform -run 'TestTerracedSynthesis|TestCompactRun|TestTerracesPreserve|TestGeneratedClimb|TestWallChimney|TestLadderMotif|TestOutcrop' -count=1
node internal/httpdebug/ui_logic_test.js
```
