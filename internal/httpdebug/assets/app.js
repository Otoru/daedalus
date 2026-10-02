"use strict";

const exampleRequest = `{
  "config": {
    "width": 64,
    "height": 64,
    "cell_size": 1,
    "seed": "4242",
    "min_distance": 6,
    "max_attempts": 30,
    "max_rooms": 128,
    "corridor_order": "CORRIDOR_ORDER_X_THEN_Y",
    "extra_edge_count": 8,
    "max_room_edges": 3,
    "room_role_requests": [
      {"role": "ROOM_ROLE_START", "count": 1},
      {"role": "ROOM_ROLE_BOSS", "count": 1},
      {"role": "ROOM_ROLE_TREASURE", "count": 3}
    ],
    "density_regions": [],
    "room_geometry": {
      "max_footprint_cells": 81,
      "min_room_gap": 1,
      "shapes": [
        {"shape": "ROOM_SHAPE_RECTANGLE", "weight": 4,
         "width": {"min": 3, "max": 9}, "height": {"min": 3, "max": 9}},
        {"shape": "ROOM_SHAPE_L", "weight": 2,
         "width": {"min": 3, "max": 9}, "height": {"min": 3, "max": 9}},
        {"shape": "ROOM_SHAPE_T", "weight": 2,
         "width": {"min": 3, "max": 9}, "height": {"min": 3, "max": 9}},
        {"shape": "ROOM_SHAPE_CROSS", "weight": 1,
         "width": {"min": 3, "max": 9}, "height": {"min": 3, "max": 9}},
        {"shape": "ROOM_SHAPE_CIRCLE", "weight": 2,
         "width": {"min": 5, "max": 9}, "height": {"min": 5, "max": 9}}
      ]
    },
    "corridor_geometry": {
      "widths": [
        {"width": 1, "weight": 5},
        {"width": 2, "weight": 2},
        {"width": 3, "weight": 1}
      ]
    },
    "terrain": {
      "definitions": [
        {"id": "grass", "entry_cost": 1, "transparent": true},
        {"id": "water", "entry_cost": 4, "transparent": true},
        {"id": "smoke", "entry_cost": 1, "transparent": false}
      ],
      "rooms": {
        "none_weight": 3,
        "terrains": [
          {"terrain_id": "grass", "weight": 4},
          {"terrain_id": "water", "weight": 2},
          {"terrain_id": "smoke", "weight": 1}
        ],
        "min_patch_cells": 8,
        "max_patch_cells": 24
      },
      "corridors": {
        "none_weight": 5,
        "terrains": [{"terrain_id": "water", "weight": 1}],
        "min_patch_cells": 8,
        "max_patch_cells": 16
      }
    }
  }
}`;

const examplePlatformRequest = `{
  "config": {
    "seed": "9",
    "width": 160,
    "height": 100,
    "progression": {
      "base": 0,
      "steps": [
        {"name": "Mothwing Cloak", "grants": 1},
        {"name": "Mantis Claw", "grants": 4},
        {"name": "Monarch Wings", "grants": 2}
      ]
    },
    "beat_definitions": [
      {"kind": 1, "difficulty": 0},
      {"kind": 2, "difficulty": 40},
      {"kind": 3, "difficulty": 120},
      {"kind": 4, "difficulty": 160},
      {"kind": 5, "difficulty": 90},
      {"kind": 6, "difficulty": 180, "requires": 4}
    ],
    "spine": {
      "beats": [
        {"kind": 1, "weight": 2}, {"kind": 2, "weight": 2},
        {"kind": 3, "weight": 2}, {"kind": 4, "weight": 2},
        {"kind": 5, "weight": 2}, {"kind": 6, "weight": 3}
      ],
      "min_run_beats": 4, "max_run_beats": 6
    }
  }
}`;

const elements = {
  requestEditor: document.querySelector("#request-editor"),
  requestLabel: document.querySelector("#request-label"),
  loadExample: document.querySelector("#load-example"),
  copyRequest: document.querySelector("#copy-request"),
  generate: document.querySelector("#generate"),
  generateMode: document.querySelector("#generate-mode"),
  requestActions: document.querySelector("#request-actions"),
  status: document.querySelector("#status"),
  zoom: document.querySelector("#zoom"),
  zoomValue: document.querySelector("#zoom-value"),
  fitMap: document.querySelector("#fit-map"),
  showJumpGraph: document.querySelector("#show-jump-graph"),
  abilityDash: document.querySelector("#ability-dash"),
  abilityDoubleJump: document.querySelector("#ability-double-jump"),
  abilityWallJump: document.querySelector("#ability-wall-jump"),
  platformVerdict: document.querySelector("#platform-verdict"),
  jumpGraphHint: document.querySelector("#jump-graph-hint"),
  mapMode: document.querySelector("#map-mode"),
  viewControls: document.querySelector("#view-controls"),
  showCenterline: document.querySelector("#show-centerline"),
  clickMode: document.querySelector("#click-mode"),
  flee: document.querySelector("#flee"),
  clearField: document.querySelector("#clear-field"),
  visibilityMode: document.querySelector("#visibility-mode"),
  visibilityRadius: document.querySelector("#visibility-radius"),
  visibilityRadiusValue: document.querySelector("#visibility-radius-value"),
  clearVisibility: document.querySelector("#clear-visibility"),
  buildGating: document.querySelector("#build-gating"),
  clearGating: document.querySelector("#clear-gating"),
  viewport: document.querySelector("#map-viewport"),
  canvas: document.querySelector("#map-canvas"),
  placeholder: document.querySelector("#map-placeholder"),
  inspector: document.querySelector("#cell-inspector"),
  cellDetails: document.querySelector("#cell-details"),
  errorBox: document.querySelector("#error-box"),
  responseViewer: document.querySelector("#response-viewer"),
  copyResponse: document.querySelector("#copy-response"),
  requestTrigger: document.querySelector("#request-trigger"),
  responseTrigger: document.querySelector("#response-trigger"),
  requestPane: document.querySelector("#request-pane"),
  responsePane: document.querySelector("#response-pane"),
};

const state = {
  layout: null,
  responseText: "",
  selectedCell: null,
  goal: null,
  position: null,
  field: null,
  visibility: null,
  gatingPlan: null,
  observer: null,
  visibilityRadius: Number(elements.visibilityRadius.value),
  fieldToken: 0,
  clickPhase: "goal",
  flee: false,
  origin: {x: 0, y: 0},
  scale: Number(elements.zoom.value),
  fitted: true,
  showCenterline: true,
  openPanels: {request: false, response: false},
  lastOpenedPanel: null,
  selectedSurfaceIDs: [],
};

const panels = {
  request: {trigger: elements.requestTrigger, pane: elements.requestPane},
  response: {trigger: elements.responseTrigger, pane: elements.responsePane},
};

let viewportSyncTimer = 0;
let lastViewport = {width: 0, height: 0, dpr: 0};
const panKeys = new Set();
let panFrame = 0;
let panLastTime = 0;

elements.requestEditor.value = exampleRequest;

elements.loadExample.addEventListener("click", () => {
  elements.requestEditor.value = exampleForSelectedMode();
  setStatus("Example loaded", "success");
});

elements.generateMode.addEventListener("change", () => {
  const platform = selectedGenerateMode() === "platform";
  elements.requestLabel.textContent = platform
    ? "GeneratePlatformRequest in ProtoJSON"
    : "GenerateRequest in ProtoJSON";
  const current = elements.requestEditor.value;
  if (current === exampleRequest || current === examplePlatformRequest) {
    elements.requestEditor.value = exampleForSelectedMode();
  }
  updateRequestMode();
});

elements.copyRequest.addEventListener("click", async () => {
  await copyExactText(elements.requestEditor.value, "Request copied");
});

elements.copyResponse.addEventListener("click", async () => {
  await copyExactText(state.responseText, "Response copied");
});

elements.generate.addEventListener("click", generateLayout);

elements.zoom.addEventListener("input", () => {
  state.fitted = false;
  state.scale = Number(elements.zoom.value);
  updateZoomLabel();
  renderLayout();
});

function zoomWithWheel(event) {
  if (!state.layout?.grid || event.deltaY === 0) return;
  const oldScale = state.scale;
  const newScale = clamp(oldScale + (event.deltaY < 0 ? 1 : -1), Number(elements.zoom.min), Number(elements.zoom.max));
  if (newScale === oldScale) return;
  const bounds = elements.viewport.getBoundingClientRect();
  const nextScroll = DebugUI.scrollForAnchoredZoom({
    oldScale,
    newScale,
    scroll: {x: elements.viewport.scrollLeft, y: elements.viewport.scrollTop},
    mouse: {x: event.clientX - bounds.left, y: event.clientY - bounds.top},
  });
  event.preventDefault();
  state.fitted = false;
  state.scale = newScale;
  elements.zoom.value = String(newScale);
  updateZoomLabel();
  renderLayout();
  requestAnimationFrame(() => {
    elements.viewport.scrollLeft = nextScroll.x;
    elements.viewport.scrollTop = nextScroll.y;
  });
}

function startCameraPan() {
  if (panFrame) return;
  panLastTime = performance.now();
  panFrame = requestAnimationFrame(panCamera);
}

function panCamera(now) {
  panFrame = 0;
  if (!state.layout?.platform || panKeys.size === 0) return;
  const seconds = Math.min((now - panLastTime) / 1000, .05);
  panLastTime = now;
  const speed = 560;
  const x = (panKeys.has("d") ? 1 : 0) - (panKeys.has("a") ? 1 : 0);
  const y = (panKeys.has("s") ? 1 : 0) - (panKeys.has("w") ? 1 : 0);
  if (x || y) {
    elements.viewport.scrollLeft += x * speed * seconds;
    elements.viewport.scrollTop += y * speed * seconds;
  }
  panFrame = requestAnimationFrame(panCamera);
}

elements.fitMap.addEventListener("click", fitMap);
elements.showCenterline.addEventListener("change", () => {
  state.showCenterline = elements.showCenterline.checked;
  renderLayout();
});
elements.flee.addEventListener("change", () => {
  state.flee = elements.flee.checked;
  if (state.goal) {
    void loadField();
  }
});
elements.clearField.addEventListener("click", clearField);
elements.visibilityRadius.addEventListener("input", () => {
  state.visibilityRadius = Number(elements.visibilityRadius.value);
  elements.visibilityRadiusValue.textContent = String(state.visibilityRadius);
  if (state.observer) {
    void loadVisibility();
  }
});
elements.clearVisibility.addEventListener("click", clearVisibility);
elements.buildGating.addEventListener("click", buildGatingPlan);
elements.clearGating.addEventListener("click", clearGatingPlan);
elements.canvas.addEventListener("click", selectCellFromPointer);
[elements.showJumpGraph, elements.abilityDash, elements.abilityDoubleJump, elements.abilityWallJump].forEach((control) => {
  control.addEventListener("change", () => {
    updateJumpGraphSummary();
    renderLayout();
  });
});
elements.canvas.addEventListener("keydown", moveSelectionWithKeyboard);
elements.viewport.addEventListener("wheel", zoomWithWheel, {passive: false});
elements.requestTrigger.addEventListener("click", () => togglePanel("request"));
elements.responseTrigger.addEventListener("click", () => togglePanel("response"));

document.addEventListener("keydown", (event) => {
  if (state.layout?.platform && DebugUI.shouldPanWithKey(event.key, event.target)) {
    event.preventDefault();
    panKeys.add(event.key.toLowerCase());
    startCameraPan();
    return;
  }
  if (event.key !== "Escape" || !anyPanelOpen()) {
    return;
  }
  event.preventDefault();
  closeForegroundPanel();
});

document.addEventListener("keyup", (event) => {
  panKeys.delete(event.key.toLowerCase());
});
window.addEventListener("blur", () => panKeys.clear());

async function generateLayout() {
  const requestText = elements.requestEditor.value;
  try {
    JSON.parse(requestText);
  } catch {
    showLocalError("The editor contains invalid JSON. Fix the syntax before generating.");
    setStatus("Invalid JSON", "failure");
    return;
  }

  setGenerating(true);
  clearError();
  setStatus("Generating map…", "busy");

  try {
    const endpoint = selectedGenerateMode() === "platform"
      ? "/api/v1/generate-platform"
      : "/api/v1/generate";
    const response = await fetch(endpoint, {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: requestText,
    });
    const responseText = await response.text();
    state.responseText = responseText;
    elements.responseViewer.textContent = responseText || "The server returned an empty response.";
    elements.copyResponse.disabled = responseText.length === 0;

    if (!response.ok) {
      showServerError(response.status, responseText);
      setStatus(`HTTP failure ${response.status}`, "failure");
      return;
    }

    const layout = layoutForRendering(JSON.parse(responseText));
    setLayoutForRendering(layout);
    state.selectedCell = null;
    clearFieldState();
    clearVisibilityState();
    clearGatingPlan();
    clearInspector();
    fitMap();
    if (announcePlatformResult()) return;
    const roomCount = Array.isArray(layout.rooms) ? layout.rooms.length : 0;
    const corridorCount = Array.isArray(layout.corridors) ? layout.corridors.length : 0;
    setStatus(`${roomCount} rooms · ${corridorCount} corridors`, "success");
  } catch (error) {
    showLocalError(`Could not complete the request: ${error.message}`);
    setStatus("Communication failure", "failure");
  } finally {
    setGenerating(false);
  }
}

// This is the single integration seam for GeneratePlatform: the RPC layout
// is passed to setLayoutForRendering. A wrapped {layout} body is unwrapped
// first; the layout itself is what the renderer stores.
function setLayoutForRendering(layout) {
  state.layout = normalizePlatformLayout(layout);
  state.roomOffsets = roomOffsets(state.layout);
  state.selectedSurfaceIDs = [];
  updateMapTools();
  updatePlatformVerdict();
  updateJumpGraphSummary();
}

function updateRequestMode() {
  elements.requestActions.dataset.requestMode = selectedGenerateMode();
}

function updateMapTools() {
  const mode = state.layout?.platform ? "platform" : state.layout ? "dungeon" : "empty";
  elements.viewControls.dataset.mapMode = mode;
  elements.mapMode.className = `map-mode ${mode}`;
  elements.mapMode.textContent = mode === "platform" ? "Platform map" : mode === "dungeon" ? "Dungeon map" : "No map";
}

function selectedGenerateMode() {
  return elements.generateMode.value === "platform" ? "platform" : "dungeon";
}

function exampleForSelectedMode() {
  return selectedGenerateMode() === "platform" ? examplePlatformRequest : exampleRequest;
}

function layoutForRendering(parsed) {
  if (parsed && parsed.layout && !parsed.grid && (parsed.layout.judgement || parsed.layout.jump_graph)) {
    return parsed.layout;
  }
  return parsed;
}

function announcePlatformResult() {
  const judgement = state.layout?.platform && state.layout.judgement;
  if (!judgement) return false;
  openPanel("response");
  const verdict = platformVerdictClass(judgement.verdict);
  const reason = platformReasonText(judgement.reason);
  const rooms = platformRoomCount(state.layout);
  setStatus(`${verdict.toUpperCase()} — ${reason} · ${rooms} rooms`, verdict === "certified" ? "success" : verdict);
  return true;
}

function platformRoomCount(layout) {
  if (Array.isArray(layout?.plane?.rooms)) return layout.plane.rooms.length;
  if (Array.isArray(layout?.rooms)) return layout.rooms.length;
  return 0;
}

function normalizePlatformLayout(layout) {
  const shaped = protoPlatformShape(layout) || layout;
  if (!shaped?.plane || !Array.isArray(shaped.plane.rooms)) return shaped;
  const plane = shaped.plane;
  const width = Number(plane.width) || 1;
  const height = Number(plane.height) || 1;
  const cells = Array.from({length: width * height}, (_, index) => ({
    at: {x: index % width, y: Math.floor(index / width)}, kind: "empty",
  }));
  (plane.rooms || []).forEach((room) => {
    const grid = room.grid || {};
    const origin = room.origin || {};
    const roomCells = grid.cells || [];
    for (let y = 0; y < Number(grid.height || 0); y += 1) for (let x = 0; x < Number(grid.width || 0); x += 1) {
      const px = Number(origin.x || 0) + x;
      const py = Number(origin.y || 0) + y;
      if (px >= 0 && py >= 0 && px < width && py < height) cells[py * width + px] = {
        at: {x: px, y: py}, kind: platformKindName(roomCells[y * grid.width + x]), room_id: room.id,
      };
    }
  });
  return {...shaped, platform: true, grid: {width, height, cells}};
}

// ProtoJSON PlatformLayout has rooms, judgement and jump_graph at the top.
// The renderer already understands the fixture's plane, so this is the only
// translation. A dungeon Layout has a grid and is left alone.
// The wire names a transition's side with a number; drawPlatformTransitions
// switches on the name. 1 left, 2 right, 3 top, 4 bottom, 5 door.
const transitionSides = ["unspecified", "left", "right", "top", "bottom", "door"];

function protoRoom(room) {
  if (!room || !Array.isArray(room.transitions)) return room;
  return {
    ...room,
    transitions: room.transitions.map((transition) => ({
      ...transition,
      side: typeof transition.side === "number"
        ? (transitionSides[transition.side] || "bottom")
        : transition.side,
    })),
  };
}

function protoAnchor(anchor) {
  if (!anchor) return null;
  return {room: anchor.room, at: {x: Number(anchor.x) || 0, y: Number(anchor.y) || 0}};
}

function protoPlatformShape(layout) {
  if (!layout || layout.plane || layout.grid) return null;
  if (!Array.isArray(layout.rooms) || !layout.rooms.some((room) => room?.grid)) return null;
  if (!layout.judgement && !layout.jump_graph) return null;
  return {
    ...layout,
    plane: {
      width: Number(layout.width) || 1,
      height: Number(layout.height) || 1,
      rooms: (layout.rooms || []).map(protoRoom),
      // The wire sends spawn and goal as {room, x, y}; platformPoint expects
      // {room, at:{x,y}}.
      spawn: protoAnchor(layout.spawn),
      goal: protoAnchor(layout.goal),
    },
    jump_graph: normalizeProtoJumpGraph(layout.jump_graph),
  };
}

const motionEdgeKinds = [
  "unspecified", "walk", "fall", "drop-through", "jump", "double-jump",
  "dash", "wall-jump", "wall-cling", "climb", "transition",
];
const platformAbilities = ["dash", "double-jump", "wall-jump", "climb", "shadow-dash"];

function platformKindName(kind) {
  const text = String(kind ?? "").toLowerCase();
  if (text.includes("semi")) return "semi-solid";
  if (text.includes("climb")) return "climbable";
  if (text.includes("hazard")) return "hazard";
  if (text.includes("solid") || text === "2") return "solid";
  return "empty";
}

function motionEdgeKindName(kind) {
  if (typeof kind === "string" && !/^\d+$/.test(kind)) return kind;
  return motionEdgeKinds[Number(kind)] || "unspecified";
}

function abilityNames(requires) {
  if (Array.isArray(requires)) return requires;
  if (typeof requires === "string") {
    if (requires === "" || requires === "base" || requires === "0") return [];
    if (/^\d+$/.test(requires)) return abilityNames(Number(requires));
    return requires.split(",").map((name) => name.trim()).filter(Boolean);
  }
  const bits = Number(requires) || 0;
  return platformAbilities.filter((_, index) => (bits & (1 << index)) !== 0);
}

function normalizeProtoJumpGraph(graph) {
  if (!graph) return graph;
  return {
    ...graph,
    nodes: (graph.nodes || []).map((node) => ({
      ...node,
      footing: node.footing || {lo: Number(node.footing_lo || 0), hi: Number(node.footing_hi || 0)},
    })),
    edges: (graph.edges || []).map((edge) => ({
      ...edge,
      kind: motionEdgeKindName(edge.kind),
      requires: abilityNames(edge.requires),
    })),
  };
}

function platformVerdictClass(value) {
  const text = String(value ?? "").trim().toLowerCase().replaceAll("-", "_");
  if (text === "1" || text === "certified" || text === "platform_verdict_certified") return "certified";
  if (text === "2" || text === "rejected" || text === "platform_verdict_rejected") return "rejected";
  return "unknown";
}

function platformReasonText(value) {
  const text = String(value ?? "").trim();
  if (!text || text === "0") return "unspecified";
  const reason = text.replace(/^PLATFORM_VERDICT_REASON_/i, "").replaceAll("_", "-").toLowerCase();
  return reason || "unspecified";
}

function showServerError(statusCode, responseText) {
  let error;
  try {
    error = JSON.parse(responseText);
  } catch {
    showLocalError(`The server returned HTTP ${statusCode} without a readable JSON error.`);
    return;
  }

  const lines = [
    `HTTP ${statusCode}`,
    `Code: ${error.code || "not provided"}`,
    `Message: ${error.message || "not provided"}`,
    `Request ID: ${error.request_id || "not provided"}`,
  ];
  elements.errorBox.textContent = lines.join("\n");
  elements.errorBox.hidden = false;
  openPanel("response");
}

function showLocalError(message) {
  elements.errorBox.textContent = message;
  elements.errorBox.hidden = false;
  openPanel("response");
}

function clearError() {
  elements.errorBox.hidden = true;
  elements.errorBox.textContent = "";
}

function setGenerating(isGenerating) {
  elements.generate.disabled = isGenerating;
  elements.loadExample.disabled = isGenerating;
  elements.generateMode.disabled = isGenerating;
  elements.generate.querySelector("span").textContent = isGenerating ? "Generating…" : "Generate map";
}

function setStatus(message, style) {
  elements.status.textContent = message;
  elements.status.className = `status ${style || ""}`.trim();
}

async function copyExactText(text, successMessage) {
  if (typeof navigator.clipboard?.writeText !== "function") {
    setStatus("Could not copy to the clipboard", "failure");
    return;
  }
  try {
    await navigator.clipboard.writeText(text);
    setStatus(successMessage, "success");
  } catch {
    setStatus("Could not copy to the clipboard", "failure");
  }
}

function anyPanelOpen() {
  return state.openPanels.request || state.openPanels.response;
}

function togglePanel(name) {
  if (state.openPanels[name]) {
    closePanel(name);
    return;
  }
  openPanel(name);
}

// The request and view panels sit on opposite edges, so each opens and closes
// on its own. Both may be open together. The map gives up width on each side
// and is measured again from the new viewport box.
function openPanel(name) {
  const entry = panels[name];
  state.openPanels[name] = true;
  state.lastOpenedPanel = name;
  entry.pane.hidden = false;
  entry.trigger.setAttribute("aria-expanded", "true");
  scheduleViewportSync();
  if (name === "request") {
    elements.requestEditor.focus();
    elements.requestEditor.setSelectionRange(0, 0);
    elements.requestEditor.scrollTop = 0;
    return;
  }
  if (!elements.copyResponse.disabled) {
    elements.copyResponse.focus();
    return;
  }
  elements.responsePane.focus();
}

function closePanel(name) {
  const entry = panels[name];
  if (!entry || !state.openPanels[name]) {
    return;
  }
  state.openPanels[name] = false;
  entry.pane.hidden = true;
  entry.trigger.setAttribute("aria-expanded", "false");
  scheduleViewportSync();
  entry.trigger.focus();
}

function closeForegroundPanel() {
  const active = document.activeElement;
  for (const name of ["request", "response"]) {
    const entry = panels[name];
    if (state.openPanels[name] && entry.pane.contains(active)) {
      closePanel(name);
      return;
    }
  }
  if (state.lastOpenedPanel && state.openPanels[state.lastOpenedPanel]) {
    closePanel(state.lastOpenedPanel);
    return;
  }
  for (const name of ["response", "request"]) {
    if (state.openPanels[name]) {
      closePanel(name);
      return;
    }
  }
}

function fitMap() {
  if (!state.layout?.grid) {
    return;
  }
  state.fitted = true;
  applyFitScale();
  renderLayout();
}

function applyFitScale() {
  const grid = state.layout?.grid;
  const width = elements.viewport.clientWidth;
  const height = elements.viewport.clientHeight;
  if (!grid?.width || !grid?.height || width < 1 || height < 1) {
    return;
  }
  // Fit is the largest scale that keeps every cell of the grid inside the
  // current viewport. Recompute it from that box whenever the viewport changes
  // while the map is fitted, so a side panel or window resize does not leave a
  // stale zoom.
  const horizontalScale = Math.floor(width / grid.width);
  const verticalScale = Math.floor(height / grid.height);
  state.scale = clamp(Math.min(horizontalScale, verticalScale), 2, 24);
  elements.zoom.value = String(state.scale);
  updateZoomLabel();
}

function updateZoomLabel() {
  elements.zoomValue.value = `${state.scale} px`;
  elements.zoomValue.textContent = `${state.scale} px`;
}

function renderLayout() {
  if (!state.layout?.grid) {
    return;
  }

  const grid = state.layout.grid;
  const scale = state.scale;
  const canvas = elements.canvas;
  const contentWidth = grid.width * scale;
  const contentHeight = grid.height * scale;
  // Keep a camera apron around the map. Without it panning stops the moment
  // the final row enters view, which makes inspection at the edge awkward.
  const cameraPad = Math.max(80, Math.round(scale * 6));
  const cssWidth = Math.max(elements.viewport.clientWidth, contentWidth + cameraPad * 2);
  const cssHeight = Math.max(elements.viewport.clientHeight, contentHeight + cameraPad * 2);
  state.origin = {
    x: cameraPad + Math.floor((cssWidth - contentWidth - cameraPad * 2) / 2),
    y: cameraPad + Math.floor((cssHeight - contentHeight - cameraPad * 2) / 2),
  };
  const dpr = window.devicePixelRatio || 1;
  // A canvas does not reflow with its container. Size the backing store from
  // the CSS box and the device pixel ratio, then redraw, or the picture
  // stretches when a side panel or the window changes the map.
  canvas.style.width = `${cssWidth}px`;
  canvas.style.height = `${cssHeight}px`;
  canvas.width = Math.max(1, Math.round(cssWidth * dpr));
  canvas.height = Math.max(1, Math.round(cssHeight * dpr));
  canvas.classList.add("visible");
  elements.placeholder.hidden = true;

  const context = canvas.getContext("2d");
  context.setTransform(dpr, 0, 0, dpr, 0, 0);
  context.imageSmoothingEnabled = false;
  context.fillStyle = cellColors.empty;
  context.fillRect(0, 0, cssWidth, cssHeight);
  context.save();
  context.translate(state.origin.x, state.origin.y);

  const cells = Array.isArray(grid.cells) ? grid.cells : [];
  cells.forEach((cell, index) => {
    const at = cell.at || {
      x: index % grid.width,
      y: Math.floor(index / grid.width),
    };
    context.fillStyle = colorForCell(cell);
    context.fillRect(at.x * scale, at.y * scale, scale, scale);
  });

  if (state.layout.platform) {
    drawPlatformTerrain(context, scale);
    drawPlatformTransitions(context, scale);
    drawPlatformAnchors(context, scale);
    drawJumpGraph(context, scale);
    drawSelection(context, scale);
    context.restore();
    return;
  }

  drawRoomBoundaries(context, scale);
  drawGrid(context, grid.width, grid.height, scale);
  drawCorridorBands(context, scale);
  drawTerrain(context, scale);
  drawField(context, scale);
  drawVisibility(context, scale);
  drawCenterlines(context, scale);
  if (state.field) {
    drawRoomBoundaries(context, scale);
  }
  drawDoors(context, scale);
  drawGatingPlan(context, scale);
  drawRoleMarkers(context, scale);
  drawRoute(context, scale);
  drawFieldMarkers(context, scale);
  drawSelection(context, scale);
  context.restore();
}

const cellColors = {
  empty: "hsl(210 8% 13%)",
  corridor: "hsl(40 14% 30%)",
  corridorRoute: "hsl(40 12% 18%)",
  door: "hsl(16 14% 78%)",
  doorMark: "hsl(16 8% 24%)",
  gateMain: "hsl(35 78% 62%)",
  gateKey: "hsl(205 70% 68%)",
};

function colorForCell(cell) {
  if (state.layout?.platform) return platformCellColor(cell.kind);
  if (cell.kind === "CELL_KIND_ROOM") {
    return roomColor(cell.room_id || 0);
  }
  if (cell.kind === "CELL_KIND_CORRIDOR") {
    return cellColors.corridor;
  }
  return cellColors.empty;
}

function platformCellColor(kind) {
  switch (platformKindName(kind)) {
    case "solid": return "#697586";
    case "semi-solid": return "#7a5a2b";
    case "climbable": return "#5a4630";
    case "hazard": return "#5d2630";
    default: return cellColors.empty;
  }
}

function drawPlatformTerrain(context, scale) {
  const cells = state.layout.grid.cells || [];
  context.save();
  cells.forEach((cell) => {
    const x = cell.at.x * scale;
    const y = cell.at.y * scale;
    const kind = platformKindName(cell.kind);
    if (kind === "semi-solid") {
      context.strokeStyle = "#f2c879";
      context.lineWidth = Math.max(1, scale * 0.16);
      context.beginPath(); context.moveTo(x, y + scale * 0.23); context.lineTo(x + scale, y + scale * 0.23); context.stroke();
    } else if (kind === "climbable") {
      context.strokeStyle = "#d0aa74"; context.lineWidth = Math.max(1, scale * 0.12);
      context.beginPath(); context.moveTo(x + scale * .32, y); context.lineTo(x + scale * .32, y + scale); context.moveTo(x + scale * .68, y); context.lineTo(x + scale * .68, y + scale); context.stroke();
    } else if (kind === "hazard") {
      context.fillStyle = "#ff8a8a";
      context.beginPath(); context.moveTo(x, y + scale); context.lineTo(x + scale * .5, y + scale * .22); context.lineTo(x + scale, y + scale); context.fill();
    }
  });
  context.restore();
}

function platformPoint(anchor) {
  const room = (state.layout.plane.rooms || []).find((candidate) => String(candidate.id) === String(anchor?.room));
  if (!room) return null;
  return {x: Number(room.origin?.x || 0) + Number(anchor.at?.x || 0) + .5, y: Number(room.origin?.y || 0) + Number(anchor.at?.y || 0) + .5};
}

function drawPlatformAnchors(context, scale) {
  const marks = [[state.layout.plane.spawn, "S", "#8ee6a2"], [state.layout.plane.goal, "G", "#ffd166"]];
  context.save(); context.font = `${Math.max(10, scale)}px ui-monospace`; context.textAlign = "center"; context.textBaseline = "middle";
  marks.forEach(([anchor, label, color]) => { const point = platformPoint(anchor); if (!point) return; context.fillStyle = color; context.beginPath(); context.arc(point.x * scale, point.y * scale, Math.max(4, scale * .32), 0, Math.PI * 2); context.fill(); context.fillStyle = "#111"; context.fillText(label, point.x * scale, point.y * scale + 1); });
  context.restore();
}

function drawPlatformTransitions(context, scale) {
  context.save();
  (state.layout.plane.rooms || []).forEach((room) => (room.transitions || []).forEach((transition) => {
    const origin = room.origin || {}; const grid = room.grid || {}; const offset = Number(transition.offset || 0); const extent = Math.max(1, Number(transition.extent || 1));
    let x = Number(origin.x || 0), y = Number(origin.y || 0), horizontal = false;
    if (transition.side === "right") { x += Number(grid.width || 0); y += Number(grid.height || 0) - offset - extent; }
    else if (transition.side === "left") { y += Number(grid.height || 0) - offset - extent; }
    else if (transition.side === "top") { x += offset; horizontal = true; }
    else { x += offset; y += Number(grid.height || 0); horizontal = true; }
    context.strokeStyle = transition.outbound && !transition.inbound ? "#ff8a8a" : "#a7d8ff"; context.lineWidth = Math.max(2, scale * .18);
    context.beginPath(); if (horizontal) { context.moveTo(x * scale, y * scale); context.lineTo((x + extent) * scale, y * scale); } else { context.moveTo(x * scale, y * scale); context.lineTo(x * scale, (y + extent) * scale); } context.stroke();
    context.fillStyle = "#f2f2f2"; context.font = `${Math.max(8, scale * .62)}px ui-monospace`; context.fillText(`h${offset}`, (x + .5) * scale, (y + .5) * scale);
  }));
  context.restore();
}

function selectedAbilities() {
  return new Set([elements.abilityDash.checked && "dash", elements.abilityDoubleJump.checked && "double-jump", elements.abilityWallJump.checked && "wall-jump"].filter(Boolean));
}

function edgeRequirements(edge) {
  return DebugUI.graphRequirements(edge);
}

// A node's footing and height are in its ROOM's frame, not the plane's: the
// merged graph is a disjoint union of per-room graphs. The node carries the
// room id so the reader can add that room's origin. Without this every room's
// graph draws on top of the first one, in the corner.
function roomOffsets(layout) {
  const plane = layout?.plane;
  if (!plane || !Array.isArray(plane.rooms)) return new Map();
  const planeHeight = Number(plane.height) || 1;
  return new Map(plane.rooms.map((room) => {
    const origin = room.origin || {};
    const grid = room.grid || {};
    const originY = Number(origin.y) || 0;
    const roomHeight = Number(grid.height) || 0;
    // WorldY measures upward from a grid's bottom, so the room's floor sits
    // this far above the plane's floor.
    return [String(room.id), {x: Number(origin.x) || 0, y: planeHeight - originY - roomHeight}];
  }));
}

function graphNodePoint(node) {
  // Footing and height are in the node's ROOM frame; roomOffsets carries them
  // into the plane. Then world y (upward) becomes Canvas y (downward).
  const span = node.footing || {}; let x = (Number(span.lo) + Number(span.hi)) / 2;
  let height = Number(node.height);
  const offset = state.roomOffsets?.get(String(node.room ?? ""));
  if (offset) { x += offset.x; height += offset.y; }
  return {x, y: Number(state.layout.grid.height) - height};
}

function drawArrow(context, from, to, scale, color, dashed) {
  const dx = to.x - from.x, dy = to.y - from.y, length = Math.hypot(dx, dy); if (length < .01) return;
  const ux = dx / length, uy = dy / length, endX = to.x * scale - ux * scale * .28, endY = to.y * scale - uy * scale * .28;
  context.save(); context.strokeStyle = color; context.fillStyle = color; context.lineWidth = Math.max(1.5, scale * .1); if (dashed) context.setLineDash([scale * .24, scale * .18]);
  context.beginPath(); context.moveTo(from.x * scale, from.y * scale); context.lineTo(endX, endY); context.stroke(); context.setLineDash([]);
  const wing = scale * .22; context.beginPath(); context.moveTo(endX, endY); context.lineTo(endX - ux * wing - uy * wing, endY - uy * wing + ux * wing); context.lineTo(endX - ux * wing + uy * wing, endY - uy * wing - ux * wing); context.closePath(); context.fill(); context.restore();
}

function drawJumpGraph(context, scale) {
  if (!state.layout?.platform || !elements.showJumpGraph.checked) return;
	if (state.selectedSurfaceIDs.length === 0) return;
  const graph = state.layout.jump_graph || {};
  const nodes = new Map((graph.nodes || []).map((node) => [String(node.id), node]));
  const visible = DebugUI.selectGraphEdges(graph, state.selectedSurfaceIDs, selectedAbilities());
  const aggregates = DebugUI.aggregateGraphEdges(visible, graph.nodes || []);
  const colors = {jump: "#7dd3fc", "double-jump": "#c4b5fd", dash: "#fb7185", "wall-jump": "#fbbf24", "wall-cling": "#a3e635"};
  aggregates.forEach((edge) => {
    const from = nodes.get(String(edge.from));
    const to = nodes.get(String(edge.to));
    if (from && to) drawArrow(context, graphNodePoint(from), graphNodePoint(to), scale, colors[edge.kind] || "#d1d5db", false);
  });
  context.save();
  const endpointIDs = new Set(aggregates.flatMap((edge) => [String(edge.from), String(edge.to)]));
  endpointIDs.forEach((id) => {
    const node = nodes.get(id); if (!node) return;
    const point = graphNodePoint(node); context.fillStyle = "#111827"; context.strokeStyle = "#f8fafc";
    context.lineWidth = Math.max(1, scale * .09); context.beginPath(); context.arc(point.x * scale, point.y * scale, Math.max(2.5, scale * .18), 0, Math.PI * 2); context.fill(); context.stroke();
  });
  context.restore();
}

function updateJumpGraphSummary() {
  if (!state.layout?.platform) return;
  const graph = state.layout.jump_graph || {};
  const hasAbilityEdges = (graph.edges || []).some((edge) => edgeRequirements(edge).length > 0);
  if (!hasAbilityEdges) {
    elements.jumpGraphHint.textContent = "This map has no ability-gated edges; the ability toggles have nothing to add.";
    return;
  }
  if (!elements.showJumpGraph.checked) {
    elements.jumpGraphHint.textContent = "Turn on Jump graph to inspect jumps from a selected surface.";
    return;
  }
  if (state.selectedSurfaceIDs.length === 0) {
    const visible = DebugUI.selectGraphEdges(graph, [], selectedAbilities());
    const aggregateCount = DebugUI.aggregateGraphEdges(visible, graph.nodes || []).length;
    elements.jumpGraphHint.textContent = `Showing ${aggregateCount} jump arrows across the map. Select a surface to focus them.`;
    return;
  }
  const visible = DebugUI.selectGraphEdges(graph, state.selectedSurfaceIDs, selectedAbilities());
  const aggregateCount = DebugUI.aggregateGraphEdges(visible, graph.nodes || []).length;
  elements.jumpGraphHint.textContent = `${visible.length} refined jump edges collapse into ${aggregateCount} surface-to-surface arrows.`;
}

function updatePlatformVerdict() {
  if (!state.layout?.platform) return;
  const judgement = state.layout.judgement || {verdict: "unknown", reason: "not evaluated"};
  const verdict = platformVerdictClass(judgement.verdict);
  const reason = platformReasonText(judgement.reason);
  elements.platformVerdict.className = `platform-verdict ${verdict}`;
  elements.platformVerdict.textContent = `${verdict.toUpperCase()} · ${reason}`;
  elements.platformVerdict.title = judgement.detail || "";
}

function roomColor(roomID) {
  // Neighbouring RoomIDs take a prime hue step and alternate lightness, so adjacent
  // Rooms stay separable by hue and by value. Saturation stays low so a Layout
  // with many Rooms does not read as confetti.
  const hue = (Number(roomID) * 137 + 211) % 360;
  const lightness = Number(roomID) % 2 === 0 ? 50 : 63;
  return `hsl(${hue} 16% ${lightness}%)`;
}

function drawRoomBoundaries(context, scale) {
  const rooms = Array.isArray(state.layout.rooms) ? state.layout.rooms : [];
  context.save();
  context.strokeStyle = "rgba(8, 13, 19, 0.72)";
  context.lineWidth = Math.max(1, scale * 0.13);
  rooms.forEach((room) => {
    (room.cells || []).forEach((cell) => {
      context.strokeRect(
        cell.x * scale + context.lineWidth / 2,
        cell.y * scale + context.lineWidth / 2,
        scale - context.lineWidth,
        scale - context.lineWidth,
      );
    });
  });
  context.restore();
}

function drawCorridorBands(context, scale) {
  const corridors = Array.isArray(state.layout.corridors) ? state.layout.corridors : [];
  // cells is the occupied band in row-major order, not a path. Refilling it
  // after the grid covers the internal lattice, so a wide hall reads as one
  // slab. Rooms stay gridded and outlined, and the lightness ladder stays
  // empty, then corridor, then room — separable without hue.
  const band = [];
  const seen = new Set();
  corridors.forEach((corridor) => {
    (corridor.cells || []).forEach((cell) => {
      const key = `${cell.x},${cell.y}`;
      if (seen.has(key)) {
        return;
      }
      seen.add(key);
      band.push(cell);
    });
  });
  context.save();
  context.fillStyle = cellColors.corridor;
  band.forEach((cell) => {
    context.fillRect(cell.x * scale, cell.y * scale, scale, scale);
  });
  context.restore();
}

function terrainAt(x, y) {
  const grid = state.layout?.grid;
  const terrain = grid?.terrain;
  if (!terrain) return null;
  const raw = terrain.indices;
  const bytes = typeof raw === "string" ? base64ToBytes(raw) : raw;
  const selected = bytes && bytes[y * grid.width + x];
  if (!selected || !Array.isArray(terrain.palette)) return null;
  return terrain.palette[selected - 1] || null;
}

function terrainColor(definition, index) {
  const text = `${definition?.id || "terrain"}:${index}`;
  let hash = 0;
  for (let offset = 0; offset < text.length; offset += 1) hash = (hash * 31 + text.charCodeAt(offset)) >>> 0;
  return `hsl(${hash % 360} 28% ${definition?.transparent ? 52 : 38}%)`;
}

function drawTerrain(context, scale) {
  const grid = state.layout?.grid;
  if (!grid?.terrain) return;
  const raw = grid.terrain.indices;
  const bytes = typeof raw === "string" ? base64ToBytes(raw) : raw;
  if (!bytes) return;
  context.save();
  for (let index = 0; index < bytes.length; index += 1) {
    const paletteIndex = bytes[index];
    if (!paletteIndex) continue;
    const definition = grid.terrain.palette?.[paletteIndex - 1];
    if (!definition) continue;
    context.fillStyle = terrainColor(definition, paletteIndex);
    context.globalAlpha = definition.transparent ? 0.42 : 0.58;
    context.fillRect((index % grid.width) * scale, Math.floor(index / grid.width) * scale, scale, scale);
  }
  context.restore();
}

function drawCenterlines(context, scale) {
  if (!state.showCenterline) {
    return;
  }
  const corridors = Array.isArray(state.layout.corridors) ? state.layout.corridors : [];
  context.save();
  context.strokeStyle = cellColors.corridorRoute;
  context.lineCap = "square";
  context.lineJoin = "miter";
  context.lineWidth = Math.max(1, scale * 0.18);
  corridors.forEach((corridor) => {
    const centerline = corridor.centerline || [];
    if (centerline.length === 0) {
      return;
    }
    context.beginPath();
    context.moveTo((centerline[0].x + 0.5) * scale, (centerline[0].y + 0.5) * scale);
    centerline.slice(1).forEach((cell) => {
      context.lineTo((cell.x + 0.5) * scale, (cell.y + 0.5) * scale);
    });
    context.stroke();
  });
  context.restore();
}

function drawDoors(context, scale) {
  const doors = Array.isArray(state.layout.doors) ? state.layout.doors : [];
  context.save();
  context.fillStyle = cellColors.door;
  context.strokeStyle = cellColors.doorMark;
  context.lineCap = "square";
  context.lineWidth = Math.max(1, scale * 0.1);
  doors.forEach((door) => {
    const span = doorSpan(door, scale);
    if (!span) {
      return;
    }
    const inset = Math.min(Math.max(1, scale * 0.18), (Math.min(span.width, span.height) - 1) / 2);
    context.fillRect(span.x + inset, span.y + inset, span.width - inset * 2, span.height - inset * 2);
    context.strokeRect(span.x + inset, span.y + inset, span.width - inset * 2, span.height - inset * 2);
    const direction = directionVector(door.direction);
    const centerX = span.x + span.width / 2;
    const centerY = span.y + span.height / 2;
    context.beginPath();
    context.moveTo(centerX, centerY);
    context.lineTo(centerX + direction.x * scale * 0.42, centerY + direction.y * scale * 0.42);
    context.stroke();
  });
  context.restore();
}

function drawGatingPlan(context, scale) {
  const plan = state.gatingPlan;
  if (!plan || !state.layout) {
    return;
  }
  const doors = new Map((state.layout.doors || []).map((door) => [String(door.id), door]));
  const rooms = new Map((state.layout.rooms || []).map((room) => [String(room.id), room]));
  context.save();
  context.lineCap = "square";
  plan.gates.forEach((gate) => {
    const door = doors.get(String(gate.door_id));
    if (door?.at) {
      const span = doorSpan(door, scale);
      const direction = directionVector(door.direction);
      const centerX = span.x + span.width / 2;
      const centerY = span.y + span.height / 2;
      context.strokeStyle = gate.kind === "GATE_KIND_OPTIONAL" ? cellColors.gateKey : cellColors.gateMain;
      context.lineWidth = Math.max(2, scale * 0.24);
      context.beginPath();
      context.moveTo(centerX - direction.x * scale * 0.32, centerY - direction.y * scale * 0.32);
      context.lineTo(centerX + direction.x * scale * 0.32, centerY + direction.y * scale * 0.32);
      context.stroke();
      drawGateLabel(context, `Lock ${gate.id} · Door ${gate.door_id}`, centerX, centerY, scale);
    }
    const room = rooms.get(String(gate.key_room_id));
    const anchor = room && roleAnchor(room);
    if (anchor) {
      context.fillStyle = cellColors.gateKey;
      context.strokeStyle = cellColors.doorMark;
      context.lineWidth = Math.max(1, scale * 0.12);
      context.beginPath();
      context.arc(anchor.x * scale, anchor.y * scale, Math.max(3, scale * 0.26), 0, Math.PI * 2);
      context.fill();
      context.stroke();
      drawGateLabel(context, `Key ${gate.id} · Room ${gate.key_room_id}`, anchor.x * scale, anchor.y * scale, scale);
    }
  });
  context.restore();
}

function drawGateLabel(context, text, x, y, scale) {
  context.save();
  const fontSize = Math.max(12, Math.min(16, scale * 1.2));
  context.font = `600 ${fontSize}px system-ui, sans-serif`;
  const padding = 7;
  const width = context.measureText(text).width + padding * 2;
  const height = fontSize + padding * 2;
  const mapWidth = state.layout.grid.width * scale;
  const labelX = Math.max(0, Math.min(x + scale, mapWidth - width));
  const labelY = Math.max(0, y - height - scale);
  context.fillStyle = "#181c20";
  context.fillRect(labelX, labelY, width, height);
  context.strokeStyle = "#e4c78b";
  context.lineWidth = 1;
  context.strokeRect(labelX, labelY, width, height);
  context.beginPath();
  context.moveTo(labelX + padding, labelY + height);
  context.lineTo(x, y);
  context.stroke();
  context.fillStyle = "#fff4dc";
  context.textBaseline = "top";
  context.fillText(text, labelX + padding, labelY + padding);
  context.restore();
}

// Door.at is the span Cell with the smallest (y, x). A north or south opening
// runs along X; an east or west opening runs along Y. Span counts those Cells
// and is never zero.
function doorSpan(door, scale) {
  if (!door.at) {
    return null;
  }
  const count = Math.max(1, Number(door.span) || 1);
  const alongX = door.direction === "DIRECTION_NORTH" || door.direction === "DIRECTION_SOUTH";
  return {
    x: door.at.x * scale,
    y: door.at.y * scale,
    width: (alongX ? count : 1) * scale,
    height: (alongX ? 1 : count) * scale,
  };
}

function doorCoversCell(door, x, y) {
  if (!door.at) {
    return false;
  }
  const count = Math.max(1, Number(door.span) || 1);
  const alongX = door.direction === "DIRECTION_NORTH" || door.direction === "DIRECTION_SOUTH";
  if (alongX) {
    return y === door.at.y && x >= door.at.x && x < door.at.x + count;
  }
  return x === door.at.x && y >= door.at.y && y < door.at.y + count;
}

// Role marks are a fixed screen size, clamped from 8px to 20px, on the occupied
// cell nearest the room centroid. A stroke scaled with the cell vanishes at
// 2px and turns crude at 24px. Below 14px only the outline is drawn: a circle
// with a centre dot, a heavier square, or a diamond, in the existing door
// tone. At 14px and above a system-font letter is added. Doors are pale bars
// on the room edge, so a closed shape in the interior does not read as one.
const roleMarkerMinPx = 8;
const roleGlyphMinPx = 14;
const roleMarkerMaxPx = 20;

function drawRoleMarkers(context, scale) {
  const rooms = Array.isArray(state.layout.rooms) ? state.layout.rooms : [];
  context.save();
  context.lineJoin = "miter";
  context.lineCap = "square";
  context.textAlign = "center";
  context.textBaseline = "middle";
  rooms.forEach((room) => {
    const kind = roleKind(room.role);
    const anchor = roleAnchor(room);
    if (!kind || !anchor) {
      return;
    }
    const diameter = roleMarkerDiameter(room, scale);
    const centerX = anchor.x * scale;
    const centerY = anchor.y * scale;
    drawRoleOutline(context, kind, centerX, centerY, diameter);
    if (diameter < roleGlyphMinPx) {
      return;
    }
    context.font = `600 ${Math.round(diameter * 0.42)}px ui-monospace, sans-serif`;
    context.lineWidth = 3;
    context.strokeStyle = cellColors.doorMark;
    context.strokeText(kind.glyph, centerX, centerY);
    context.fillStyle = cellColors.door;
    context.fillText(kind.glyph, centerX, centerY);
  });
  context.restore();
}

function roleKind(role) {
  if (role === "ROOM_ROLE_START") {
    return {shape: "circle", glyph: "S"};
  }
  if (role === "ROOM_ROLE_BOSS") {
    return {shape: "square", glyph: "B"};
  }
  if (role === "ROOM_ROLE_TREASURE") {
    return {shape: "diamond", glyph: "T"};
  }
  return null;
}

function roleAnchor(room) {
  const cells = room.cells || [];
  if (cells.length === 0) {
    return null;
  }
  let sumX = 0;
  let sumY = 0;
  cells.forEach((cell) => {
    sumX += cell.x;
    sumY += cell.y;
  });
  const centerX = sumX / cells.length;
  const centerY = sumY / cells.length;
  let best = cells[0];
  let bestDistance = Infinity;
  cells.forEach((cell) => {
    const dx = cell.x - centerX;
    const dy = cell.y - centerY;
    const distance = dx * dx + dy * dy;
    if (distance < bestDistance) {
      bestDistance = distance;
      best = cell;
    }
  });
  return {x: best.x + 0.5, y: best.y + 0.5};
}

function roleMarkerDiameter(room, scale) {
  const width = Math.max(1, Number(room.width) || 1);
  const height = Math.max(1, Number(room.height) || 1);
  return clamp(Math.min(width, height) * scale * 0.5, roleMarkerMinPx, roleMarkerMaxPx);
}

function drawRoleOutline(context, kind, centerX, centerY, diameter) {
  const radius = diameter / 2;
  traceRoleShape(context, kind.shape, centerX, centerY, radius);
  context.lineWidth = kind.shape === "square" ? 3.5 : 2.5;
  context.strokeStyle = cellColors.doorMark;
  context.stroke();
  context.lineWidth = kind.shape === "square" ? 1.75 : 1.25;
  context.strokeStyle = cellColors.door;
  context.stroke();
  if (kind.shape !== "circle" || diameter >= roleGlyphMinPx) {
    return;
  }
  context.beginPath();
  context.arc(centerX, centerY, Math.max(1.25, diameter * 0.16), 0, Math.PI * 2);
  context.fillStyle = cellColors.door;
  context.fill();
}

function traceRoleShape(context, shape, centerX, centerY, radius) {
  context.beginPath();
  if (shape === "circle") {
    context.arc(centerX, centerY, radius, 0, Math.PI * 2);
    return;
  }
  if (shape === "square") {
    context.rect(centerX - radius, centerY - radius, radius * 2, radius * 2);
    return;
  }
  context.moveTo(centerX, centerY - radius);
  context.lineTo(centerX + radius, centerY);
  context.lineTo(centerX, centerY + radius);
  context.lineTo(centerX - radius, centerY);
  context.closePath();
}

function directionVector(direction) {
  const vectors = {
    DIRECTION_NORTH: {x: 0, y: -1},
    DIRECTION_EAST: {x: 1, y: 0},
    DIRECTION_SOUTH: {x: 0, y: 1},
    DIRECTION_WEST: {x: -1, y: 0},
  };
  return vectors[direction] || {x: 0, y: 0};
}

function drawGrid(context, width, height, scale) {
  if (scale < 5) {
    return;
  }
  context.save();
  context.strokeStyle = "rgba(255, 255, 255, 0.13)";
  context.lineWidth = 1;
  context.beginPath();
  for (let x = 0; x <= width; x += 1) {
    context.moveTo(x * scale + 0.5, 0);
    context.lineTo(x * scale + 0.5, height * scale);
  }
  for (let y = 0; y <= height; y += 1) {
    context.moveTo(0, y * scale + 0.5);
    context.lineTo(width * scale, y * scale + 0.5);
  }
  context.stroke();
  context.restore();
}

function drawSelection(context, scale) {
  if (!state.selectedCell) {
    return;
  }
  context.save();
  context.strokeStyle = "#ffffff";
  context.lineWidth = Math.max(2, scale * 0.18);
  context.strokeRect(
    state.selectedCell.x * scale + context.lineWidth / 2,
    state.selectedCell.y * scale + context.lineWidth / 2,
    scale - context.lineWidth,
    scale - context.lineWidth,
  );
  context.restore();
}

function selectCellFromPointer(event) {
  if (!state.layout) {
    return;
  }
  const bounds = elements.canvas.getBoundingClientRect();
  const x = Math.floor((event.clientX - bounds.left - state.origin.x) / state.scale);
  const y = Math.floor((event.clientY - bounds.top - state.origin.y) / state.scale);
  selectCell(x, y);
}

function moveSelectionWithKeyboard(event) {
  const movement = {
    ArrowUp: {x: 0, y: -1},
    ArrowRight: {x: 1, y: 0},
    ArrowDown: {x: 0, y: 1},
    ArrowLeft: {x: -1, y: 0},
  }[event.key];
  if (!movement || !state.layout) {
    return;
  }
  event.preventDefault();
  const current = state.selectedCell || {x: 0, y: 0};
  selectCell(current.x + movement.x, current.y + movement.y);
}

function selectCell(x, y) {
  const grid = state.layout.grid;
  if (x < 0 || y < 0 || x >= grid.width || y >= grid.height) {
    return;
  }
  state.selectedCell = {x, y};
  if (state.layout.platform) {
    updateInspector(x, y);
    renderLayout();
    return;
  }
  if (elements.visibilityMode.checked) {
    if (!isTransparentCell(x, y)) {
      updateInspector(x, y);
      renderLayout();
      setStatus("An observer has to be a room or corridor cell", "failure");
      return;
    }
    state.observer = {x, y};
    updateInspector(x, y);
    renderLayout();
    void loadVisibility();
    return;
  }
  if (state.clickPhase === "goal") {
    if (!isPassableCell(x, y)) {
      updateInspector(x, y);
      renderLayout();
      setStatus("A goal has to be a room or corridor cell", "failure");
      return;
    }
    state.goal = {x, y};
    state.position = null;
    state.clickPhase = "position";
    updateClickMode();
    updateInspector(x, y);
    renderLayout();
    void loadField();
    return;
  }
  state.position = {x, y};
  state.clickPhase = "goal";
  updateClickMode();
  updateInspector(x, y);
  renderLayout();
  describePosition();
}

function clearInspector() {
  elements.inspector.hidden = true;
  elements.cellDetails.textContent = "No cell selected.";
}

function platformSurfaceIDsAt(graph, grid, x, y) {
	const cell = grid.cells[y * grid.width + x];
	const room = (state.layout?.plane?.rooms || []).find((candidate) => String(candidate.id) === String(cell?.room_id));
	if (room) {
		const localX = x - Number(room.origin?.x || 0);
		const localY = y - Number(room.origin?.y || 0);
		const feet = Number(room.grid?.height || 0) - localY;
		return [...new Set((graph.nodes || []).filter((node) => String(node.room) === String(room.id) && Math.floor(Number(node.height)) === feet && localX >= Number(node.footing?.lo ?? node.footing_lo ?? 0) && localX <= Number(node.footing?.hi ?? node.footing_hi ?? 0)).map((node) => node.surface))];
	}
  return (graph.surfaces || []).filter((surface) => {
    const extent = surface.extent || {}; const at = Number(surface.at); const worldY = Number(grid.height) - y;
    return (surface.kind === "wall-left" || surface.kind === "wall-right" || surface.kind === "climbable")
      ? Math.floor(at) === x && worldY >= Number(extent.lo) && worldY <= Number(extent.hi)
      : Math.floor(at) === worldY && x >= Number(extent.lo) && x <= Number(extent.hi);
  }).map((surface) => surface.id);
}

function updateInspector(x, y) {
  const grid = state.layout.grid;
  const index = y * grid.width + x;
  const cell = (grid.cells || [])[index] || {at: {x, y}, kind: "CELL_KIND_EMPTY"};
  if (state.layout.platform) {
    const graph = state.layout.jump_graph || {};
    const surfaceIDs = platformSurfaceIDsAt(graph, grid, x, y);
    state.selectedSurfaceIDs = surfaceIDs;
    updateJumpGraphSummary();
    const nodeIDs = (graph.nodes || []).filter((node) => surfaceIDs.map(String).includes(String(node.surface))).map((node) => node.id);
    const room = (state.layout.plane.rooms || []).find((candidate) => String(candidate.id) === String(cell.room_id));
    const details = {coordinates: {x, y}, kind: cell.kind, room_id: cell.room_id ?? null, surface_ids: surfaceIDs, motion_node_ids: nodeIDs, transitions: (room?.transitions || []).filter((transition) => transitionCoversPlatformCell(room, transition, x, y)).map((transition) => ({id: transition.id, side: transition.side, height: transition.offset, extent: transition.extent, one_way: Boolean(transition.outbound) !== Boolean(transition.inbound)}))};
    elements.cellDetails.textContent = `Platform cell\n${JSON.stringify(details, null, 2)}`;
    elements.inspector.hidden = false;
    return;
  }
  const rooms = (state.layout.rooms || []).filter((room) => room.id === cell.room_id);
  const doors = (state.layout.doors || []).filter((door) => doorCoversCell(door, x, y));
  const corridorIDs = [...(cell.corridor_ids || [])];
  doors.forEach((door) => {
    (door.corridor_ids || []).forEach((id) => {
      if (!corridorIDs.includes(id)) {
        corridorIDs.push(id);
      }
    });
  });
  corridorIDs.sort((first, second) => Number(first) - Number(second));
  const corridors = (state.layout.corridors || []).filter((corridor) => corridorIDs.includes(corridor.id));
  const step = stepAt(x, y);
  const visible = visibilityAt(x, y);
  const details = {
    coordinates: {x, y},
    kind: readableEnum(cell.kind, "CELL_KIND_"),
    distance: step ? step.distance : null,
    status: step ? statusLabel(step.status) : null,
    visible_to_observer: visible,
    observer: state.observer,
    vision_radius: state.observer ? state.visibilityRadius : null,
    corridor_ids: corridorIDs,
    rooms: rooms.map((room) => ({
      id: room.id,
      shape: readableEnum(room.shape, "ROOM_SHAPE_"),
      origin: room.origin,
      dimensions: {width: room.width, height: room.height},
      role: readableEnum(room.role, "ROOM_ROLE_") || null,
      plant_id: room.plant_id || null,
      tags: room.tags || [],
      door_ids: room.door_ids || [],
    })),
    corridors: corridors.map((corridor) => ({
      id: corridor.id,
      from_room: corridor.from_room_id,
      to_room: corridor.to_room_id,
      plant_id: corridor.plant_id || null,
      tags: corridor.tags || [],
    })),
    doors: doors.map((door) => ({
      id: door.id,
      room_id: door.room_id,
      direction: readableEnum(door.direction, "DIRECTION_"),
      span: Math.max(1, Number(door.span) || 1),
      corridor_ids: door.corridor_ids || [],
    })),
    terrain: terrainAt(x, y) ? {
      id: terrainAt(x, y).id,
      entry_cost: terrainAt(x, y).entry_cost,
      transparent: terrainAt(x, y).transparent,
    } : null,
  };
  const headline = state.observer
    ? `Vision: ${visible ? "Visible" : "Hidden"}${step ? ` · ${statusLabel(step.status)} · distance ${step.distance}` : ""}`
    : step
      ? `${statusLabel(step.status)} · distance ${step.distance}`
      : "No field on this cell yet.";
  elements.cellDetails.textContent = `${headline}\n${JSON.stringify(details, null, 2)}`;
  elements.inspector.hidden = false;
}

function transitionCoversPlatformCell(room, transition, x, y) {
  const origin = room.origin || {}; const grid = room.grid || {}; const offset = Number(transition.offset || 0); const extent = Math.max(1, Number(transition.extent || 1));
  if (transition.side === "left" || transition.side === "right") return y >= Number(origin.y || 0) + Number(grid.height || 0) - offset - extent && y < Number(origin.y || 0) + Number(grid.height || 0) - offset;
  return x >= Number(origin.x || 0) + offset && x < Number(origin.x || 0) + offset + extent;
}

function readableEnum(value, prefix) {
  return typeof value === "string" ? value.replace(prefix, "") : value;
}

function clamp(value, minimum, maximum) {
  return Math.max(minimum, Math.min(maximum, value));
}

function scheduleViewportSync() {
  window.clearTimeout(viewportSyncTimer);
  viewportSyncTimer = window.setTimeout(syncViewportToContainer, 80);
}

function syncViewportToContainer() {
  const width = elements.viewport.clientWidth;
  const height = elements.viewport.clientHeight;
  const dpr = window.devicePixelRatio || 1;
  if (width < 1 || height < 1) {
    return;
  }
  if (width === lastViewport.width && height === lastViewport.height && dpr === lastViewport.dpr) {
    return;
  }
  lastViewport = {width, height, dpr};
  if (!state.layout?.grid) {
    return;
  }
  if (state.fitted) {
    applyFitScale();
  }
  renderLayout();
}

updateZoomLabel();
updateRequestMode();
updateMapTools();
elements.visibilityRadiusValue.textContent = String(state.visibilityRadius);
updateClickMode();

// MaxStepsPerCall on the navigation contract. A larger positions list is
// rejected, so a 256×256 grid is posted as several legal ComputeSteps calls.
const maxStepsPerCall = 16384;

function updateClickMode() {
  elements.clickMode.textContent = state.clickPhase === "goal"
    ? "Next click sets the goal"
    : "Next click sets the position";
}

function clearField() {
  clearFieldState();
  if (state.selectedCell) {
    updateInspector(state.selectedCell.x, state.selectedCell.y);
  }
  renderLayout();
  setStatus("Field cleared", "success");
}

function clearFieldState() {
  state.goal = null;
  state.position = null;
  state.field = null;
  state.fieldToken += 1;
  state.clickPhase = "goal";
  updateClickMode();
}

function clearVisibility() {
  clearVisibilityState();
  if (state.selectedCell) {
    updateInspector(state.selectedCell.x, state.selectedCell.y);
  }
  renderLayout();
  setStatus("Vision cleared", "success");
}

function clearGatingPlan() {
  state.gatingPlan = null;
  renderLayout();
}

async function buildGatingPlan() {
  if (!state.layout) {
    setStatus("Generate a map before building gates", "failure");
    return;
  }
  if (state.layout.platform) {
    // BuildGatingPlan is the dungeon's key-and-door planner and wants rooms,
    // corridors and doors. A platform map has none of those: its progression
    // is abilities, and it is declared in the request rather than computed
    // here. Posting one produced a 400 that looked like a server fault.
    setStatus("Gates are a dungeon plan; a platform map gates on abilities, declared in the request", "failure");
    return;
  }
  const startRoom = (state.layout.rooms || []).find((room) => room.role === "ROOM_ROLE_START") || state.layout.rooms[0];
  if (!startRoom) {
    setStatus("The layout has no room for a gating start", "failure");
    return;
  }
  setStatus("Building the gating plan…", "busy");
  try {
    const response = await fetch("/api/v1/build-gating-plan", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({
        layout: state.layout,
        request: {seed: state.layout.seed || "0", start_room_id: startRoom.id, main_gate_count: 1},
      }),
    });
    const responseText = await response.text();
    if (!response.ok) {
      showServerError(response.status, responseText);
      setStatus(`Gating request failed · HTTP ${response.status}`, "failure");
      return;
    }
    const payload = JSON.parse(responseText);
    state.gatingPlan = payload.plan;
    renderLayout();
    setStatus(`Gating ready · ${state.gatingPlan.gates.length} gate`, "success");
  } catch (error) {
    showLocalError(`Could not complete the gating request: ${error.message}`);
    setStatus("Gating request failed", "failure");
  }
}

function clearVisibilityState() {
  state.observer = null;
  state.visibility = null;
}

function isPassableCell(x, y) {
  const kind = kindAt(x, y);
  if (kind !== "CELL_KIND_ROOM" && kind !== "CELL_KIND_CORRIDOR") return false;
  const terrain = terrainAt(x, y);
  return !terrain || Number(terrain.entry_cost) > 0;
}

function isTransparentCell(x, y) {
  const kind = kindAt(x, y);
  if (kind !== "CELL_KIND_ROOM" && kind !== "CELL_KIND_CORRIDOR") return false;
  const terrain = terrainAt(x, y);
  return !terrain || terrain.transparent === true;
}

function kindAt(x, y) {
  const grid = state.layout.grid;
  const cell = (grid.cells || [])[y * grid.width + x];
  return cell ? cell.kind : "CELL_KIND_EMPTY";
}

function stepAt(x, y) {
  if (!state.field || !state.layout?.grid) {
    return null;
  }
  return state.field[y * state.layout.grid.width + x] || null;
}

function visibilityAt(x, y) {
  if (!state.visibility || !state.layout?.grid) {
    return null;
  }
  const index = y * state.layout.grid.width + x;
  return (state.visibility[index >> 3] & (1 << (index & 7))) !== 0;
}

function statusLabel(status) {
  const raw = readableEnum(status, "STEP_STATUS_");
  if (typeof raw !== "string" || raw.length === 0) {
    return "Unspecified";
  }
  return raw.charAt(0) + raw.slice(1).toLowerCase();
}

// DefaultCostRule: a room or corridor cell costs 1, everything else is
// impassable (0). The page builds that grid and posts it; it does not ask
// the server to derive costs from the Layout.
function costBytes(layout) {
  const width = layout.grid.width;
  const height = layout.grid.height;
  const count = width * height;
  const costs = new Uint8Array(count);
  const cells = layout.grid.cells || [];
  for (let index = 0; index < count; index += 1) {
    const kind = cells[index] ? cells[index].kind : "CELL_KIND_EMPTY";
    if (kind === "CELL_KIND_ROOM" || kind === "CELL_KIND_CORRIDOR") {
      const terrain = terrainAt(index % width, Math.floor(index / width));
      costs[index] = terrain ? Number(terrain.entry_cost) : 1;
    }
  }
  return costs;
}

function bytesToBase64(bytes) {
  let binary = "";
  const chunk = 4096;
  for (let offset = 0; offset < bytes.length; offset += chunk) {
    const slice = bytes.subarray(offset, offset + chunk);
    binary += String.fromCharCode.apply(null, slice);
  }
  return btoa(binary);
}

function base64ToBytes(text) {
  const binary = atob(text || "");
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes;
}

function opacityBytes(layout) {
  const width = layout.grid.width;
  const height = layout.grid.height;
  const count = width * height;
  const transparent = new Uint8Array(Math.ceil(count / 8));
  const cells = layout.grid.cells || [];
  for (let index = 0; index < count; index += 1) {
    const cell = cells[index];
    if (cell && isTransparentCell(index % width, Math.floor(index / width))) {
      transparent[index >> 3] |= 1 << (index & 7);
    }
  }
  return transparent;
}

async function loadVisibility() {
  if (!state.layout?.grid || !state.observer) {
    return;
  }
  const layout = state.layout;
  setStatus("Computing the field of vision…", "busy");
  try {
    const response = await fetch("/api/v1/compute-visibility", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({
        opacity_grid: {
          width: layout.grid.width,
          height: layout.grid.height,
          transparent: bytesToBase64(opacityBytes(layout)),
        },
        queries: [{origin: state.observer, radius: state.visibilityRadius}],
      }),
    });
    const responseText = await response.text();
    if (!response.ok) {
      showServerError(response.status, responseText);
      setStatus(`Vision request failed · HTTP ${response.status}`, "failure");
      state.visibility = null;
      renderLayout();
      return;
    }
    const payload = JSON.parse(responseText);
    const encoded = payload.fields && payload.fields[0] ? payload.fields[0].visible : "";
    const visibility = base64ToBytes(encoded);
    if (visibility.length !== Math.ceil(layout.grid.width * layout.grid.height / 8)) {
      showLocalError("ComputeVisibility returned a field with an unexpected size.");
      setStatus("Vision response was incomplete", "failure");
      state.visibility = null;
      renderLayout();
      return;
    }
    state.visibility = visibility;
    renderLayout();
    if (state.selectedCell) {
      updateInspector(state.selectedCell.x, state.selectedCell.y);
    }
    setStatus(`Vision ready · radius ${state.visibilityRadius}`, "success");
  } catch (error) {
    showLocalError(`Could not complete the vision request: ${error.message}`);
    setStatus("Vision request failed", "failure");
  }
}

async function loadField() {
  if (!state.layout?.grid || !state.goal) {
    return;
  }
  const token = state.fieldToken + 1;
  state.fieldToken = token;
  const goal = {x: state.goal.x, y: state.goal.y};
  const flee = state.flee;
  const layout = state.layout;
  const width = layout.grid.width;
  const height = layout.grid.height;
  const total = width * height;
  setStatus(flee ? "Computing the flee field…" : "Computing the field…", "busy");

  try {
    const costs = bytesToBase64(costBytes(layout));
    const steps = new Array(total);
    for (let offset = 0; offset < total; offset += maxStepsPerCall) {
      const end = Math.min(total, offset + maxStepsPerCall);
      const positions = [];
      for (let index = offset; index < end; index += 1) {
        positions.push({x: index % width, y: Math.floor(index / width)});
      }
      const response = await fetch("/api/v1/compute-steps", {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({
          cost_grid: {width, height, costs},
          queries: [{
            sources: [{at: goal}],
            positions,
            flee,
          }],
        }),
      });
      const responseText = await response.text();
      if (token !== state.fieldToken) {
        return;
      }
      if (!response.ok) {
        showServerError(response.status, responseText);
        setStatus(`Field request failed · HTTP ${response.status}`, "failure");
        state.field = null;
        renderLayout();
        return;
      }
      const payload = JSON.parse(responseText);
      const batch = payload.results && payload.results[0] ? payload.results[0].steps : null;
      if (!Array.isArray(batch) || batch.length !== positions.length) {
        showLocalError("ComputeSteps returned a step list that does not match the positions.");
        setStatus("Field response was incomplete", "failure");
        state.field = null;
        renderLayout();
        return;
      }
      for (let index = 0; index < batch.length; index += 1) {
        steps[offset + index] = batch[index];
      }
    }
    if (token !== state.fieldToken) {
      return;
    }
    state.field = steps;
    renderLayout();
    if (state.selectedCell) {
      updateInspector(state.selectedCell.x, state.selectedCell.y);
    }
    if (state.position) {
      describePosition();
      return;
    }
    setStatus(flee ? "Flee field ready" : "Field ready", "success");
  } catch (error) {
    if (token !== state.fieldToken) {
      return;
    }
    showLocalError(`Could not complete the field request: ${error.message}`);
    setStatus("Field request failed", "failure");
  }
}

function describePosition() {
  if (!state.position || !state.field) {
    return;
  }
  const step = stepAt(state.position.x, state.position.y);
  if (!step) {
    return;
  }
  const label = statusLabel(step.status);
  if (step.status === "STEP_STATUS_UNREACHABLE") {
    setStatus("Position is Unreachable", "failure");
    return;
  }
  if (step.status === "STEP_STATUS_BLOCKED") {
    setStatus(state.flee ? "Flee holds here · Blocked" : "Position is Blocked", "success");
    return;
  }
  setStatus(state.flee ? `Flee route · ${label}` : `Route toward the goal · ${label}`, "success");
}

function fieldRange() {
  const grid = state.layout.grid;
  let min = null;
  let max = null;
  const count = grid.width * grid.height;
  for (let index = 0; index < count; index += 1) {
    const x = index % grid.width;
    const y = Math.floor(index / grid.width);
    if (!isPassableCell(x, y)) {
      continue;
    }
    const step = state.field[index];
    if (!isFiniteFieldStep(step)) {
      continue;
    }
    const distance = Number(step.distance);
    if (min === null || distance < min) {
      min = distance;
    }
    if (max === null || distance > max) {
      max = distance;
    }
  }
  return {min, max};
}

// Unreachable is the sentinel -1. It stays off the lightness ramp: folding it
// in would paint a walled-off cell as merely one step past the nearest floor.
function isFiniteFieldStep(step) {
  if (!step) {
    return false;
  }
  return step.status !== "STEP_STATUS_UNREACHABLE" && step.status !== "STEP_STATUS_OUTSIDE";
}

function lightnessFor(distance, range) {
  if (range.min === null || range.max === null || range.max === range.min) {
    return 58;
  }
  const span = range.max - range.min;
  const t = (distance - range.min) / span;
  return Math.round(74 - t * 38);
}

function drawField(context, scale) {
  if (!state.field || !state.layout?.grid) {
    return;
  }
  const grid = state.layout.grid;
  const range = fieldRange();
  const cells = grid.cells || [];
  for (let y = 0; y < grid.height; y += 1) {
    for (let x = 0; x < grid.width; x += 1) {
      const index = y * grid.width + x;
      const kind = cells[index] ? cells[index].kind : "CELL_KIND_EMPTY";
      if (kind !== "CELL_KIND_ROOM" && kind !== "CELL_KIND_CORRIDOR") {
        continue;
      }
      const step = state.field[index];
      if (!isFiniteFieldStep(step)) {
        drawUnreachableCell(context, x, y, scale);
        continue;
      }
      context.fillStyle = `hsl(0 0% ${lightnessFor(Number(step.distance), range)}%)`;
      context.fillRect(x * scale, y * scale, scale, scale);
    }
  }
}

function drawVisibility(context, scale) {
  if (!state.visibility || !state.layout?.grid) {
    return;
  }
  const grid = state.layout.grid;
  const cells = grid.cells || [];
  context.save();
  for (let y = 0; y < grid.height; y += 1) {
    for (let x = 0; x < grid.width; x += 1) {
      const index = y * grid.width + x;
      const visible = visibilityAt(x, y);
      const kind = cells[index] ? cells[index].kind : "CELL_KIND_EMPTY";
      if (visible) {
        context.fillStyle = kind === "CELL_KIND_EMPTY" ? "rgba(255,255,255,0.05)" : "rgba(255,255,255,0.16)";
      } else {
        context.fillStyle = "rgba(0,0,0,0.62)";
      }
      context.fillRect(x * scale, y * scale, scale, scale);
    }
  }
  context.restore();
}

function drawUnreachableCell(context, x, y, scale) {
  const left = x * scale;
  const top = y * scale;
  context.fillStyle = "hsl(0 0% 20%)";
  context.fillRect(left, top, scale, scale);
  if (scale < 4) {
    return;
  }
  context.save();
  context.beginPath();
  context.rect(left, top, scale, scale);
  context.clip();
  context.strokeStyle = "hsl(0 0% 78%)";
  context.lineWidth = 1;
  context.beginPath();
  context.moveTo(left, top + scale);
  context.lineTo(left + scale, top);
  context.stroke();
  context.restore();
}

function routeFrom(origin) {
  const grid = state.layout.grid;
  const path = [];
  let x = origin.x;
  let y = origin.y;
  const seen = new Set();
  const limit = grid.width * grid.height;
  for (let stepCount = 0; stepCount <= limit; stepCount += 1) {
    const key = `${x},${y}`;
    if (seen.has(key)) {
      break;
    }
    seen.add(key);
    path.push({x, y});
    const step = stepAt(x, y);
    if (!step || step.status !== "STEP_STATUS_MOVED") {
      break;
    }
    const delta = directionVector(step.direction);
    const nextX = x + delta.x;
    const nextY = y + delta.y;
    if (nextX < 0 || nextY < 0 || nextX >= grid.width || nextY >= grid.height) {
      break;
    }
    x = nextX;
    y = nextY;
  }
  return path;
}

function drawRoute(context, scale) {
  if (!state.position || !state.field) {
    return;
  }
  const path = routeFrom(state.position);
  if (path.length < 2) {
    return;
  }
  context.save();
  context.strokeStyle = "#f4f4f4";
  context.lineCap = "square";
  context.lineJoin = "miter";
  context.lineWidth = Math.max(1, scale * 0.22);
  context.beginPath();
  context.moveTo((path[0].x + 0.5) * scale, (path[0].y + 0.5) * scale);
  path.slice(1).forEach((cell) => {
    context.lineTo((cell.x + 0.5) * scale, (cell.y + 0.5) * scale);
  });
  context.stroke();
  context.restore();
}

function drawFieldMarkers(context, scale) {
  context.save();
  context.lineWidth = Math.max(1, scale * 0.16);
  context.strokeStyle = "#f4f4f4";
  if (state.goal) {
    const inset = Math.max(1, scale * 0.28);
    context.strokeRect(
      state.goal.x * scale + inset,
      state.goal.y * scale + inset,
      scale - inset * 2,
      scale - inset * 2,
    );
  }
  if (state.position) {
    const inset = Math.max(1, scale * 0.28);
    const left = state.position.x * scale + inset;
    const top = state.position.y * scale + inset;
    const right = state.position.x * scale + scale - inset;
    const bottom = state.position.y * scale + scale - inset;
    context.beginPath();
    context.moveTo(left, top);
    context.lineTo(right, bottom);
    context.moveTo(right, top);
    context.lineTo(left, bottom);
    context.stroke();
  }
  context.restore();
}

if (typeof ResizeObserver === "function") {
  const viewportObserver = new ResizeObserver(() => {
    scheduleViewportSync();
  });
  viewportObserver.observe(elements.viewport);
}
