import type { CanvasDocument } from "../../types";
import { emptyGraph, type Graph } from "../../types/canvas";

export function withCanvasAppearanceDefaults(graph: Graph): Graph {
  return {
    ...graph,
    backgroundMode: graph.backgroundMode ?? "lines",
    showImageInfo: graph.showImageInfo ?? false,
  };
}

export function canvasGraphFromDocument(document: CanvasDocument): Graph {
  const { graph } = document;
  return withCanvasAppearanceDefaults({
    nodes: graph.nodes || [],
    connections: graph.edges || [],
    viewport: graph.viewport
      ? {
          x: graph.viewport.x,
          y: graph.viewport.y,
          k: graph.viewport.zoom || 1,
        }
      : emptyGraph().viewport,
    backgroundMode: graph.backgroundMode,
    showImageInfo: graph.showImageInfo,
  });
}

export function canvasDocumentGraph(graph: Graph): CanvasDocument["graph"] {
  return {
    nodes: graph.nodes,
    edges: graph.connections,
    viewport: {
      x: graph.viewport.x,
      y: graph.viewport.y,
      zoom: graph.viewport.k,
    },
    backgroundMode: graph.backgroundMode ?? "lines",
    showImageInfo: graph.showImageInfo ?? false,
  };
}
