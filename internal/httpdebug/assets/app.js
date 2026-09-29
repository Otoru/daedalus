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
    }
  }
}`;

const elements = {
  requestEditor: document.querySelector("#request-editor"),
  loadExample: document.querySelector("#load-example"),
  copyRequest: document.querySelector("#copy-request"),
  generate: document.querySelector("#generate"),
  status: document.querySelector("#status"),
  zoom: document.querySelector("#zoom"),
  zoomValue: document.querySelector("#zoom-value"),
  fitMap: document.querySelector("#fit-map"),
  showCenterline: document.querySelector("#show-centerline"),
  clickMode: document.querySelector("#click-mode"),
  flee: document.querySelector("#flee"),
  clearField: document.querySelector("#clear-field"),
  visibilityMode: document.querySelector("#visibility-mode"),
  visibilityRadius: document.querySelector("#visibility-radius"),
  visibilityRadiusValue: document.querySelector("#visibility-radius-value"),
  clearVisibility: document.querySelector("#clear-visibility"),
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
};

const panels = {
  request: {trigger: elements.requestTrigger, pane: elements.requestPane},
  response: {trigger: elements.responseTrigger, pane: elements.responsePane},
};

let viewportSyncTimer = 0;
let lastViewport = {width: 0, height: 0, dpr: 0};

elements.requestEditor.value = exampleRequest;

elements.loadExample.addEventListener("click", () => {
  elements.requestEditor.value = exampleRequest;
  setStatus("Example loaded", "success");
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
elements.canvas.addEventListener("click", selectCellFromPointer);
elements.canvas.addEventListener("keydown", moveSelectionWithKeyboard);
elements.requestTrigger.addEventListener("click", () => togglePanel("request"));
elements.responseTrigger.addEventListener("click", () => togglePanel("response"));

document.addEventListener("keydown", (event) => {
  if (event.key !== "Escape" || !anyPanelOpen()) {
    return;
  }
  event.preventDefault();
  closeForegroundPanel();
});

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
    const response = await fetch("/api/v1/generate", {
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

    const layout = JSON.parse(responseText);
    state.layout = layout;
    state.selectedCell = null;
    clearFieldState();
    clearVisibilityState();
    clearInspector();
    fitMap();
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
  const cssWidth = Math.max(elements.viewport.clientWidth, contentWidth);
  const cssHeight = Math.max(elements.viewport.clientHeight, contentHeight);
  state.origin = {
    x: Math.floor((cssWidth - contentWidth) / 2),
    y: Math.floor((cssHeight - contentHeight) / 2),
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

  drawRoomBoundaries(context, scale);
  drawGrid(context, grid.width, grid.height, scale);
  drawCorridorBands(context, scale);
  drawField(context, scale);
  drawVisibility(context, scale);
  drawCenterlines(context, scale);
  if (state.field) {
    drawRoomBoundaries(context, scale);
  }
  drawDoors(context, scale);
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
};

function colorForCell(cell) {
  if (cell.kind === "CELL_KIND_ROOM") {
    return roomColor(cell.room_id || 0);
  }
  if (cell.kind === "CELL_KIND_CORRIDOR") {
    return cellColors.corridor;
  }
  return cellColors.empty;
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

function updateInspector(x, y) {
  const grid = state.layout.grid;
  const index = y * grid.width + x;
  const cell = (grid.cells || [])[index] || {at: {x, y}, kind: "CELL_KIND_EMPTY"};
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
  };
  const headline = state.observer
    ? `Vision: ${visible ? "Visible" : "Hidden"}${step ? ` · ${statusLabel(step.status)} · distance ${step.distance}` : ""}`
    : step
      ? `${statusLabel(step.status)} · distance ${step.distance}`
      : "No field on this cell yet.";
  elements.cellDetails.textContent = `${headline}\n${JSON.stringify(details, null, 2)}`;
  elements.inspector.hidden = false;
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

function clearVisibilityState() {
  state.observer = null;
  state.visibility = null;
}

function isPassableCell(x, y) {
  const kind = kindAt(x, y);
  return kind === "CELL_KIND_ROOM" || kind === "CELL_KIND_CORRIDOR";
}

function isTransparentCell(x, y) {
  return isPassableCell(x, y);
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
      costs[index] = 1;
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
    if (cell && (cell.kind === "CELL_KIND_ROOM" || cell.kind === "CELL_KIND_CORRIDOR")) {
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
