// Small DOM-free rules used by the map UI. Keeping these separate makes the
// interaction contract executable without a browser.
var DebugUI = {
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

  selectGraphEdges(graph, selectedSurfaceIDs, abilities) {
    const selected = new Set(selectedSurfaceIDs.map(String));
    const nodes = new Map((graph.nodes || []).map((node) => [String(node.id), node]));
    const jumpKinds = new Set(["jump", "double-jump", "dash", "wall-jump", "wall-cling"]);
    return (graph.edges || []).filter((edge) => {
      if (!jumpKinds.has(edge.kind)) return false;
      const requirements = this.graphRequirements(edge);
      if (!requirements.every((ability) => abilities.has(ability))) return false;
      const from = nodes.get(String(edge.from));
      const to = nodes.get(String(edge.to));
	  // Before a surface is selected, show the whole usable graph. Requiring a
	  // click here made the ability switches look inert on a freshly rendered
	  // map because there were no arrows for them to filter.
	  return Boolean(from && to && (selected.size === 0 || selected.has(String(from.surface)) || selected.has(String(to.surface))));
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
};
