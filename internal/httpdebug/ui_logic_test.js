const assert = require("node:assert/strict");
const ui = require("./assets/ui-logic.js");

test("graph controls choose a usable surface and honor selected abilities", () => {
  const graph = {nodes: [{id: 1, surface: 10}, {id: 2, surface: 20}, {id: 3, surface: 30}, {id: 4, surface: 40, mode: 4}], edges: [
    {from: 1, to: 2, kind: "jump", requires: []},
    {from: 3, to: 2, kind: "dash", requires: ["dash"]},
    {from: 1, to: 4, kind: "wall-cling", requires: ["wall-jump"]},
  ]};
  assert.deepEqual(Array.from(ui.initialGraphSurface(graph, new Set())), [10]);
  assert.deepEqual(Array.from(ui.initialGraphSurface(graph, new Set(["dash"]))), [30]);
  assert.deepEqual(Array.from(ui.initialGraphSurface(graph, new Set(["wall-jump"]), "wall-jump")), [10]);
  assert.equal(ui.selectGraphEdges(graph, [10], new Set(["wall-jump"])).some(edge => edge.kind === "wall-cling"), true);
  assert.deepEqual(Array.from(ui.initialGraphSurface({}, new Set())), []);
});

test("clicking a tile intersects a footing inside it, not only its left edge", () => {
  const nodes = [{room: 0, surface: 7, mode: 1, height: 4, footing: {lo: 3.4, hi: 3.6}}];
  assert.deepEqual(Array.from(ui.surfaceIDsAt(nodes, 0, 3, 4)), [7]);
  assert.equal(ui.surfaceIDsAt(nodes, 0, 2, 4).length, 0);
  assert.equal(ui.surfaceIDsAt(nodes, 1, 3, 4).length, 0);
});

test("clicking a solid wall selects its fractional-height cling surface", () => {
  const nodes = [
    {room: 0, surface: 28, mode: 4, height: 17.23, footing_lo: 37.6, footing_hi: 37.6},
    {room: 0, surface: 1, mode: 1, height: 17, footing_lo: 37.4, footing_hi: 37.4},
  ];
  assert.deepEqual(Array.from(ui.surfaceIDsAt(nodes, 0, 38, 17, "solid")), [28]);
  assert.equal(ui.surfaceIDsAt(nodes, 0, 35, 17, "solid").length, 0);
  assert.equal(ui.surfaceIDsAt(nodes, 1, 38, 17, "solid").length, 0);
});

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

test("connections require a selection and show only outgoing reaches", () => {
  const graph = {
    nodes: [
      {id: 1, surface: 10, height: 0, footing: {lo: 0, hi: 1}},
      {id: 2, surface: 20, height: 3, footing: {lo: 2, hi: 3}},
      {id: 3, surface: 30, height: 16, footing: {lo: 0, hi: 1}},
    ],
    edges: [
      {id: 1, from: 1, to: 2, kind: "jump", requires: []},
      {id: 2, from: 1, to: 3, kind: "jump", requires: []},
      {id: 3, from: 1, to: 3, kind: "dash", requires: ["dash"]},
    ],
  };
  const base = ui.selectGraphEdges(graph, [], new Set());
  assert.equal(base.length, 0, "overview leaves the geometry unobscured");
  const dash = ui.selectGraphEdges(graph, [], new Set(["dash"]));
  assert.equal(dash.length, 0, "abilities do not add noise before selection");
  const focused = ui.selectGraphEdges(graph, ["10"], new Set(["dash"]));
  assert.equal(focused.length, 3, "selecting a surface reveals long reaches");
  assert.equal(ui.selectGraphEdges(graph, ["20"], new Set(["dash"])).length, 0, "incoming edges are not outgoing jumps");
});

test("a selected rope shows climb links only when climb is enabled", () => {
  const graph = {nodes: [
    {id: 1, surface: 11, mode: 1},
    {id: 2, surface: 12, mode: 3},
    {id: 3, surface: 13, mode: 3},
  ], edges: [
    {from: 1, to: 2, kind: "climb", requires: ["climb"]},
    {from: 2, to: 3, kind: "climb", requires: ["climb"]},
  ]};
  assert.equal(ui.selectGraphEdges(graph, [11], new Set()).length, 0);
  assert.equal(ui.selectGraphEdges(graph, [11], new Set(["climb"])).length, 1);
  assert.deepEqual(Array.from(ui.initialGraphSurface(graph, new Set(["climb"]), "climb")), [11]);
  assert.deepEqual(Array.from(ui.surfaceIDsAt([{room: 0, surface: 12, mode: 6, height: 8, footing_lo: 5.5}], 0, 5, 8, "climbable")), [12]);
});

test("graph preview keeps movement variety without covering the map", () => {
  const nodes = [{id: 0, surface: 0, height: 1, footing: {lo: 0, hi: 1}}];
  const edges = [];
  for (let i = 1; i <= 30; i++) {
    nodes.push({id: i, surface: i, height: i % 4, footing: {lo: i, hi: i + 1}});
    edges.push({from: 0, to: i, kind: i === 30 ? "wall-cling" : "jump", requires: []});
  }
  const preview = ui.previewGraphEdges(edges, nodes, 12);
  assert.equal(preview.length, 12);
  assert.equal(preview.some(edge => edge.kind === "wall-cling"), true);
});
