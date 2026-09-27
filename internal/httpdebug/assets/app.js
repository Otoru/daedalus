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
    "extra_edge_count": 0,
    "room_role_requests": [],
    "density_regions": [],
    "room_geometry": {
      "min_width": 3,
      "max_width": 9,
      "min_height": 3,
      "max_height": 9,
      "max_footprint_cells": 81,
      "min_room_gap": 1,
      "shapes": [
        {"shape": "ROOM_SHAPE_RECTANGLE", "weight": 4},
        {"shape": "ROOM_SHAPE_L", "weight": 2},
        {"shape": "ROOM_SHAPE_T", "weight": 2},
        {"shape": "ROOM_SHAPE_CROSS", "weight": 1},
        {"shape": "ROOM_SHAPE_CIRCLE", "weight": 2}
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
  drawer: document.querySelector("#json-drawer"),
};

const state = {
  layout: null,
  responseText: "",
  selectedCell: null,
  origin: {x: 0, y: 0},
  scale: Number(elements.zoom.value),
  fitted: true,
  openDrawer: null,
};

const drawerPanes = {
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
elements.canvas.addEventListener("click", selectCellFromPointer);
elements.canvas.addEventListener("keydown", moveSelectionWithKeyboard);
elements.requestTrigger.addEventListener("click", () => toggleDrawer("request"));
elements.responseTrigger.addEventListener("click", () => toggleDrawer("response"));

document.addEventListener("keydown", (event) => {
  if (event.key !== "Escape" || !state.openDrawer) {
    return;
  }
  event.preventDefault();
  closeDrawer();
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
  openDrawer("response");
}

function showLocalError(message) {
  elements.errorBox.textContent = message;
  elements.errorBox.hidden = false;
  openDrawer("response");
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

function toggleDrawer(name) {
  if (state.openDrawer === name) {
    closeDrawer();
    return;
  }
  openDrawer(name);
}

function openDrawer(name) {
  state.openDrawer = name;
  elements.drawer.hidden = false;
  Object.entries(drawerPanes).forEach(([key, entry]) => {
    const open = key === name;
    entry.pane.hidden = !open;
    entry.trigger.setAttribute("aria-expanded", open ? "true" : "false");
  });
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

function closeDrawer() {
  const trigger = state.openDrawer ? drawerPanes[state.openDrawer].trigger : null;
  state.openDrawer = null;
  elements.drawer.hidden = true;
  Object.values(drawerPanes).forEach((entry) => {
    entry.pane.hidden = true;
    entry.trigger.setAttribute("aria-expanded", "false");
  });
  if (trigger) {
    trigger.focus();
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
  // while the map is fitted, so a drawer or window resize does not leave a
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
  // stretches when the drawer or the window changes the map.
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
  drawCorridorRoutes(context, scale);
  drawDoors(context, scale);
  drawGrid(context, grid.width, grid.height, scale);
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

function drawCorridorRoutes(context, scale) {
  const corridors = Array.isArray(state.layout.corridors) ? state.layout.corridors : [];
  context.save();
  context.strokeStyle = cellColors.corridorRoute;
  context.lineCap = "square";
  context.lineJoin = "miter";
  context.lineWidth = Math.max(1, scale * 0.22);
  corridors.forEach((corridor) => {
    const cells = corridor.cells || [];
    if (cells.length === 0) {
      return;
    }
    context.beginPath();
    context.moveTo((cells[0].x + 0.5) * scale, (cells[0].y + 0.5) * scale);
    cells.slice(1).forEach((cell) => {
      context.lineTo((cell.x + 0.5) * scale, (cell.y + 0.5) * scale);
    });
    context.stroke();
  });
  context.restore();
}

function drawDoors(context, scale) {
  const doors = Array.isArray(state.layout.doors) ? state.layout.doors : [];
  context.save();
  doors.forEach((door) => {
    if (!door.at) {
      return;
    }
    const centerX = (door.at.x + 0.5) * scale;
    const centerY = (door.at.y + 0.5) * scale;
    const direction = directionVector(door.direction);
    const radius = Math.max(1.5, scale * 0.28);
    context.fillStyle = cellColors.door;
    context.strokeStyle = cellColors.doorMark;
    context.lineWidth = Math.max(1, scale * 0.1);
    context.beginPath();
    context.arc(centerX, centerY, radius, 0, Math.PI * 2);
    context.fill();
    context.stroke();
    context.beginPath();
    context.moveTo(centerX, centerY);
    context.lineTo(
      centerX + direction.x * scale * 0.45,
      centerY + direction.y * scale * 0.45,
    );
    context.stroke();
  });
  context.restore();
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
  updateInspector(x, y);
  renderLayout();
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
  const corridorIDs = cell.corridor_ids || [];
  const corridors = (state.layout.corridors || []).filter((corridor) => corridorIDs.includes(corridor.id));
  const doors = (state.layout.doors || []).filter((door) => door.at && door.at.x === x && door.at.y === y);
  const details = {
    coordinates: {x, y},
    kind: readableEnum(cell.kind, "CELL_KIND_"),
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
      corridor_ids: door.corridor_ids || [],
    })),
  };
  elements.cellDetails.textContent = JSON.stringify(details, null, 2);
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

if (typeof ResizeObserver === "function") {
  const viewportObserver = new ResizeObserver(() => {
    scheduleViewportSync();
  });
  viewportObserver.observe(elements.viewport);
}
