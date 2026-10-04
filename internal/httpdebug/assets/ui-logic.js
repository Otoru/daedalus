// Small DOM-free rules used by the map UI. Keeping these separate makes the
// interaction contract executable without a browser.
const DebugUI = {
  initialGraphSurface(graph, abilities, preferredAbility = "") {
    const nodes = new Map((graph.nodes || []).map(node => [String(node.id), node]));
    const candidates = (graph.edges || []).filter(edge => {
      const from = nodes.get(String(edge.from)), to = nodes.get(String(edge.to));
      return from && to && String(from.surface) !== String(to.surface)
        && ["jump", "double-jump", "dash", "wall-jump", "wall-cling", "climb"].includes(edge.kind)
        && this.graphRequirements(edge).every(ability => abilities.has(ability));
    });
    const edge = candidates.find(edge => preferredAbility === "wall-jump" && edge.kind === "wall-cling"
      && this.graphRequirements(edge).includes(preferredAbility))
      || candidates.find(edge => preferredAbility && this.graphRequirements(edge).includes(preferredAbility))
      || candidates.find(edge => this.graphRequirements(edge).length > 0) || candidates[0];
    return edge ? [nodes.get(String(edge.from)).surface] : [];
  },

  surfaceIDsAt(nodes, room, x, feet, kind = "") {
    const inRoom = nodes.filter(node => String(node.room) === String(room));
    if (kind === "climbable") {
      return [...new Set(inRoom.filter(node => (Number(node.mode) === 6 || String(node.mode).toLowerCase() === "climbing")
        && Math.abs(Number(node.height) - feet) <= 1
        && Math.abs(Number(node.footing?.lo ?? node.footing_lo ?? 0) - (x + .5)) <= 1.15
      ).map(node => node.surface))];
    }
    // Wall-cling nodes sit in AIR beside a solid face and their heights are
    // fractional. A solid wall tile therefore needs a face hit test rather
    // than the integer-height footing rule used for horizontal platforms.
    if (kind === "solid") {
      const wall = inRoom.filter(node => (Number(node.mode) === 4 || String(node.mode).toLowerCase().includes("wall-cling"))
        && Math.abs(Number(node.height) - feet) <= 1
        && Math.abs(Number(node.footing?.lo ?? node.footing_lo ?? 0) - (x + .5)) <= 1.15);
      if (wall.length) return [...new Set(wall.map(node => node.surface))];
    }
    return [...new Set(inRoom.filter(node =>
      Number(node.mode) === 1 || String(node.mode).toLowerCase() === "grounded"
    ).filter(node =>
      Math.abs(Number(node.height) - feet) < .01
      && Number(node.footing?.hi ?? node.footing_hi ?? 0) >= x
      && Number(node.footing?.lo ?? node.footing_lo ?? 0) < x + 1
    ).map(node => node.surface))];
  },
  scrollForAnchoredZoom({oldScale, newScale, scroll, mouse}) {
    const mapX = (scroll.x + mouse.x) / oldScale;
    const mapY = (scroll.y + mouse.y) / oldScale;
    return {x: mapX * newScale - mouse.x, y: mapY * newScale - mouse.y};
  },

  isEditingTarget(target) {
    if (!target) return false;
    if (target.isContentEditable) return true;
    return ["TEXTAREA", "INPUT", "SELECT"].includes(String(target.tagName || "").toUpperCase());
  },

  shouldPanWithKey(key, target) {
    return ["w", "a", "s", "d"].includes(String(key).toLowerCase()) && !this.isEditingTarget(target);
  },

  toolGroupsForLayout(layout) {
    if (!layout) return ["empty"];
    if (layout.platform) return ["camera", "platform-summary", "platform-navigation"];
    return ["camera", "dungeon-field", "dungeon-vision", "dungeon-gating"];
  },

  graphRequirements(edge) {
    if (Array.isArray(edge.requires)) return edge.requires;
    if (typeof edge.requires === "string") return edge.requires === "base" ? [] : edge.requires.split(",");
    return [];
  },

  graphEdgeLength(from, to) {
    if (!from || !to) return 0;
    const fromSpan = from.footing || {};
    const toSpan = to.footing || {};
    const fx = (Number(fromSpan.lo) + Number(fromSpan.hi)) / 2;
    const tx = (Number(toSpan.lo) + Number(toSpan.hi)) / 2;
    const fy = Number(from.height);
    const ty = Number(to.height);
    return Math.hypot(tx - fx, ty - fy);
  },

  selectGraphEdges(graph, selectedSurfaceIDs, abilities) {
    const selected = new Set(selectedSurfaceIDs.map(String));
    if (selected.size === 0) return [];
    const nodes = new Map((graph.nodes || []).map((node) => [String(node.id), node]));
    const jumpKinds = new Set(["jump", "double-jump", "dash", "wall-jump", "wall-cling", "climb"]);
    return (graph.edges || []).filter((edge) => {
      if (!jumpKinds.has(edge.kind)) return false;
      const requirements = this.graphRequirements(edge);
      if (!requirements.every((ability) => abilities.has(ability))) return false;
      const from = nodes.get(String(edge.from));
      const to = nodes.get(String(edge.to));
      if (!from || !to) return false;
      if (!selected.has(String(from.surface)) || String(from.surface) === String(to.surface)) return false;
      return true;
    });
  },

  aggregateGraphEdges(edges, nodes) {
    const byID = new Map(nodes.map((node) => [String(node.id), node]));
    const groups = new Map();
    edges.forEach((edge) => {
      const from = byID.get(String(edge.from));
      const to = byID.get(String(edge.to));
      if (!from || !to) return;
      const requirements = this.graphRequirements(edge).slice().sort();
      const key = [from.surface, to.surface, edge.kind, requirements.join(",")].map(String).join("|");
      const group = groups.get(key);
      if (group) group.refinedCount += 1;
      else groups.set(key, {...edge, requires: requirements, refinedCount: 1});
    });
    return [...groups.values()];
  },

  previewGraphEdges(edges, nodes, limit = 12) {
    const byID = new Map(nodes.map(node => [String(node.id), node]));
    const ranked = edges.map((edge, index) => ({edge, index,
      length: this.graphEdgeLength(byID.get(String(edge.from)), byID.get(String(edge.to)))}))
      .sort((a, b) => a.length - b.length || a.index - b.index);
    const chosen = [];
    const used = new Set();
    const kinds = new Set();
    for (const item of ranked) {
      if (kinds.has(item.edge.kind)) continue;
      kinds.add(item.edge.kind);
      used.add(item.index);
      chosen.push(item.edge);
      if (chosen.length >= limit) return chosen;
    }
    for (const item of ranked) {
      if (used.has(item.index)) continue;
      chosen.push(item.edge);
      if (chosen.length >= limit) break;
    }
    return chosen;
  },
};

if (typeof module !== "undefined") module.exports = DebugUI;
