import type { Graph } from "../../types/canvas";

/** Removing a group removes its frame and references; its contents remain usable. */
export function removeCanvasSelection(
  graph: Graph,
  selected: string[],
  edgeId?: string | null,
): Graph {
  const removed = new Set(selected);
  return {
    ...graph,
    nodes: graph.nodes
      .filter((node) => !removed.has(node.id))
      .map((node) =>
        node.metadata?.groupId && removed.has(node.metadata.groupId)
          ? { ...node, metadata: { ...node.metadata, groupId: undefined } }
          : node,
      ),
    connections: graph.connections.filter(
      (edge) =>
        !removed.has(edge.fromNodeId) &&
        !removed.has(edge.toNodeId) &&
        edge.id !== edgeId,
    ),
  };
}

/** Clone group members and internal references together; external references stay on originals. */
export function duplicateCanvasSelection(graph: Graph, selected: string[]) {
  const selectedIds = new Set(selected);
  let changed = true;
  while (changed) {
    changed = false;
    for (const node of graph.nodes) {
      if (
        node.metadata?.groupId &&
        selectedIds.has(node.metadata.groupId) &&
        !selectedIds.has(node.id)
      ) {
        selectedIds.add(node.id);
        changed = true;
      }
    }
  }
  const ids = new Map([...selectedIds].map((id) => [id, crypto.randomUUID()]));
  const nodes = graph.nodes
    .filter((node) => ids.has(node.id))
    .map((node) => ({
      ...structuredClone(node),
      id: ids.get(node.id)!,
      position: { x: node.position.x + 40, y: node.position.y + 40 },
      metadata: {
        ...structuredClone(node.metadata),
        groupId: node.metadata?.groupId
          ? ids.get(node.metadata.groupId)
          : undefined,
        generationSourceNodeId:
          typeof node.metadata?.generationSourceNodeId === "string"
            ? ids.get(node.metadata.generationSourceNodeId) ||
              node.metadata.generationSourceNodeId
            : undefined,
      },
    }));
  const connections = graph.connections
    .filter((edge) => ids.has(edge.fromNodeId) && ids.has(edge.toNodeId))
    .map((edge) => ({
      ...edge,
      id: crypto.randomUUID(),
      fromNodeId: ids.get(edge.fromNodeId)!,
      toNodeId: ids.get(edge.toNodeId)!,
    }));
  return {
    graph: {
      ...graph,
      nodes: [...graph.nodes, ...nodes],
      connections: [...graph.connections, ...connections],
    },
    selected: selected.map((id) => ids.get(id)!).filter(Boolean),
  };
}
