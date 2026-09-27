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
  cellDetails: document.querySelector("#cell-details"),
  errorBox: document.querySelector("#error-box"),
  responseViewer: document.querySelector("#response-viewer"),
  copyResponse: document.querySelector("#copy-response"),
};

const state = {
  layout: null,
  responseText: "",
  selectedCell: null,
  scale: Number(elements.zoom.value),
};

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
  state.scale = Number(elements.zoom.value);
  updateZoomLabel();
  renderLayout();
});

elements.fitMap.addEventListener("click", fitMap);
elements.canvas.addEventListener("click", selectCellFromPointer);
elements.canvas.addEventListener("keydown", moveSelectionWithKeyboard);

async function generateLayout() {
  const requestText = elements.requestEditor.value;
  try {
    JSON.parse(requestText);
  } catch {
    showLocalError("The editor contains invalid JSON. Fix the syntax before generating.");
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
    fitMap();
    renderLayout();
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
}

function showLocalError(message) {
  elements.errorBox.textContent = message;
  elements.errorBox.hidden = false;
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

function fitMap() {
  if (!state.layout?.grid) {
    return;
  }
  const grid = state.layout.grid;
  const horizontalScale = Math.floor((elements.viewport.clientWidth - 2) / grid.width);
  const verticalScale = Math.floor((elements.viewport.clientHeight - 2) / grid.height);
  state.scale = clamp(Math.min(horizontalScale, verticalScale), 2, 24);
  elements.zoom.value = String(state.scale);
  updateZoomLabel();
  renderLayout();
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
  canvas.width = grid.width * scale;
  canvas.height = grid.height * scale;
  canvas.classList.add("visible");
  elements.placeholder.hidden = true;

  const context = canvas.getContext("2d");
  context.imageSmoothingEnabled = false;
  context.fillStyle = "#182431";
  context.fillRect(0, 0, canvas.width, canvas.height);

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
}

function colorForCell(cell) {
  if (cell.kind === "CELL_KIND_ROOM") {
    return roomColor(cell.room_id || 0);
  }
  if (cell.kind === "CELL_KIND_CORRIDOR") {
    return "#f3c95e";
  }
  return "#182431";
}

function roomColor(roomID) {
  /*
   * Section 2.3 requires a deterministic colour per RoomID, but does not fix the palette.
   * The prime step spreads neighbouring IDs; alternating lightness keeps
   * a second visual difference beyond hue.
   */
  const hue = (Number(roomID) * 137 + 211) % 360;
  const lightness = Number(roomID) % 2 === 0 ? 58 : 68;
  return `hsl(${hue} 55% ${lightness}%)`;
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
  context.strokeStyle = "#68470b";
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
    context.fillStyle = "#ff6f61";
    context.strokeStyle = "#2c0704";
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
  const x = Math.floor((event.clientX - bounds.left) * elements.canvas.width / bounds.width / state.scale);
  const y = Math.floor((event.clientY - bounds.top) * elements.canvas.height / bounds.height / state.scale);
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
}

function readableEnum(value, prefix) {
  return typeof value === "string" ? value.replace(prefix, "") : value;
}

function clamp(value, minimum, maximum) {
  return Math.max(minimum, Math.min(maximum, value));
}

updateZoomLabel();
