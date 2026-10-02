const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const assert = require("node:assert/strict");

const source = fs.readFileSync(path.join(__dirname, "assets", "ui-logic.js"), "utf8");
const context = {};
vm.runInNewContext(source, context);
const ui = context.DebugUI;

function test(name, run) {
  try {
    run();
    console.log(`PASS ${name}`);
  } catch (error) {
    console.error(`FAIL ${name}: ${error.message}`);
    process.exitCode = 1;
  }
}

test("anchored zoom keeps the map point under the cursor", () => {
  for (const sample of [
    {oldScale: 8, newScale: 12, scroll: {x: 160, y: 80}, mouse: {x: 40, y: 25}},
    {oldScale: 24, newScale: 5, scroll: {x: 731, y: 418}, mouse: {x: 199, y: 101}},
    {oldScale: 3, newScale: 17, scroll: {x: 0, y: 0}, mouse: {x: 83, y: 47}},
  ]) {
    const next = ui.scrollForAnchoredZoom(sample);
    assert.equal((next.x + sample.mouse.x) / sample.newScale, (sample.scroll.x + sample.mouse.x) / sample.oldScale);
    assert.equal((next.y + sample.mouse.y) / sample.newScale, (sample.scroll.y + sample.mouse.y) / sample.oldScale);
  }
});

test("WASD pans from the body but never from an editor", () => {
  assert.equal(ui.shouldPanWithKey("w", {tagName: "TEXTAREA"}), false);
  assert.equal(ui.shouldPanWithKey("w", {tagName: "BODY"}), true);
  assert.equal(ui.shouldPanWithKey("ArrowUp", {tagName: "BODY"}), false);
});

test("each layout gets only its useful tool groups", () => {
  assert.deepEqual(Array.from(ui.toolGroupsForLayout({platform: true})), ["camera", "platform-summary", "platform-navigation"]);
  assert.deepEqual(Array.from(ui.toolGroupsForLayout({platform: false})), ["camera", "dungeon-field", "dungeon-vision", "dungeon-gating"]);
  assert.deepEqual(Array.from(ui.toolGroupsForLayout(null)), ["empty"]);
});

test("selected surface emits aggregated jumps and abilities change the count", () => {
  const graph = {
    nodes: [
      {id: "a", surface: "floor"}, {id: "b", surface: "floor"},
      {id: "c", surface: "ledge"}, {id: "d", surface: "wall"},
    ],
    edges: [
      {from: "a", to: "c", kind: "jump", requires: []},
      {from: "b", to: "c", kind: "jump", requires: []},
      {from: "a", to: "c", kind: "dash", requires: ["dash"]},
      {from: "a", to: "d", kind: "wall-jump", requires: ["wall-jump"]},
      {from: "a", to: "b", kind: "walk", requires: []},
    ],
  };
  const base = ui.selectGraphEdges(graph, ["floor"], new Set());
  assert.equal(base.length, 2, "walk edges and locked ability edges are omitted");
  assert.equal(ui.aggregateGraphEdges(base, graph.nodes).length, 1, "refined-node duplicates become one surface jump");
  const dash = ui.selectGraphEdges(graph, ["floor"], new Set(["dash"]));
  assert.equal(dash.length, 3);
  assert.equal(ui.aggregateGraphEdges(dash, graph.nodes).length, 2);
  console.log("COUNTS selected surface: base refined=2 arrows=1; dash refined=3 arrows=2");
});

test("an unselected platform graph shows every available manoeuvre", () => {
  const graph = {
    nodes: [{id: 1, surface: 10}, {id: 2, surface: 20}],
    edges: [{id: 1, from: 1, to: 2, kind: "jump", requires: []}],
  };
  const edges = ui.selectGraphEdges(graph, [], new Set());
  assert.equal(edges.length, 1);
});
