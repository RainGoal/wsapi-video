import type { CanvasNodeData, Graph } from "../../types/canvas";

export type CanvasReferenceIssue = {
  nodeId: string;
  title: string;
  code:
    | "missing-node"
    | "pending-result"
    | "empty-note"
    | "empty-group"
    | "unsupported-node";
};

export function canOutputReference(node: CanvasNodeData): boolean {
  return (
    node.type === "text" ||
    node.type === "group" ||
    (node.type !== "config" && Boolean(node.metadata?.assetId))
  );
}

export function canReceiveReference(node: CanvasNodeData): boolean {
  return (
    ["config", "image", "video"].includes(node.type) &&
    (!node.metadata?.generationTaskId || Boolean(node.metadata.assetId))
  );
}

/** Media and config nodes receive references; accepted outputs retain separate provenance. */
export function validateReferenceConnection(
  graph: Graph,
  fromNodeId: string,
  toNodeId: string,
): "self" | "source" | "target" | "duplicate" | null {
  if (fromNodeId === toNodeId) return "self";
  const from = graph.nodes.find((node) => node.id === fromNodeId);
  const to = graph.nodes.find((node) => node.id === toNodeId);
  if (!from || !canOutputReference(from)) return "source";
  if (!to || !canReceiveReference(to)) return "target";
  if (
    graph.connections.some(
      (edge) => edge.fromNodeId === fromNodeId && edge.toNodeId === toNodeId,
    )
  )
    return "duplicate";
  return null;
}

/** Expand direct inputs, without following the accepted result's provenance edge. */
export function collectCanvasReferences(graph: Graph, nodeId: string) {
  const assetIds = new Set<string>();
  const notes: { nodeId: string; title: string; content: string }[] = [];
  const issues: CanvasReferenceIssue[] = [];
  const visited = new Set<string>();
  const nodes = new Map(graph.nodes.map((node) => [node.id, node]));
  const sourceNodeId = nodes.get(nodeId)?.metadata?.generationSourceNodeId;
  function visit(id: string) {
    if (visited.has(id)) return;
    visited.add(id);
    const node = nodes.get(id);
    const issue = (code: CanvasReferenceIssue["code"]) =>
      issues.push({ nodeId: id, title: node?.title || id, code });
    if (!node) return issue("missing-node");
    if (node.type === "group") {
      const members = graph.nodes.filter(
        (item) => item.metadata?.groupId === id && item.type !== "config",
      );
      if (!members.length) return issue("empty-group");
      members.forEach((member) => visit(member.id));
    } else if (
      typeof node.metadata?.assetId === "string" &&
      node.metadata.assetId
    ) {
      assetIds.add(node.metadata.assetId);
    } else if (node.type === "text") {
      const content = node.metadata?.content?.trim() || "";
      if (content) notes.push({ nodeId: id, title: node.title, content });
      else issue("empty-note");
    } else if (node.metadata?.generationTaskId) issue("pending-result");
    else issue("unsupported-node");
  }
  graph.connections
    .filter(
      (edge) =>
        edge.toNodeId === nodeId &&
        edge.fromNodeId !== sourceNodeId &&
        nodes.get(edge.fromNodeId)?.type !== "config",
    )
    .forEach((edge) => visit(edge.fromNodeId));
  return { assetIds: [...assetIds], notes, issues };
}
