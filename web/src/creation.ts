import type { Asset, Task } from "./types";
import type { CanvasNodeData, CanvasNodeType, Graph } from "./types/canvas";

export type GenerationInputSnapshot = {
  reference_asset_ids: string[];
  start_frame_asset_id: string;
  end_frame_asset_id: string;
};
export function readGenerationInput(
  value: unknown,
): GenerationInputSnapshot | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return;
  const input = value as Record<string, unknown>;
  const references = input.reference_asset_ids ?? [];
  if (
    !Array.isArray(references) ||
    references.some((id) => typeof id !== "string" || !id)
  )
    return;
  if (
    [input.start_frame_asset_id, input.end_frame_asset_id].some(
      (id) => id !== undefined && typeof id !== "string",
    )
  )
    return;
  return {
    reference_asset_ids: [...references],
    start_frame_asset_id:
      typeof input.start_frame_asset_id === "string"
        ? input.start_frame_asset_id
        : "",
    end_frame_asset_id:
      typeof input.end_frame_asset_id === "string"
        ? input.end_frame_asset_id
        : "",
  };
}
export function canvasReferenceSelection(
  node: CanvasNodeData | undefined,
  connectedIds: string[],
) {
  const input = readGenerationInput(node?.metadata?.generationInput);
  const baseline = node?.metadata?.generationInputSourceAssetIds;
  const connected = [...new Set(connectedIds)];
  const restored =
    node?.type === "config" &&
    input &&
    Array.isArray(baseline) &&
    baseline.length === connected.length &&
    baseline.every((id) => connected.includes(id));
  return restored
    ? {
        referenceIds: [...input.reference_asset_ids],
        startFrameId: input.start_frame_asset_id,
        endFrameId: input.end_frame_asset_id,
        restored: true,
      }
    : {
        referenceIds: connected,
        startFrameId: "",
        endFrameId: "",
        restored: false,
      };
}

export function canvasGenerationDefaults(
  node?: CanvasNodeData,
  kind?: "image" | "video",
  referenceIds: string[] = [],
  notes: { content: string }[] = [],
): { kind: "image" | "video"; prompt: string; referenceIds: string[] } {
  const prompts = [
    node?.metadata?.prompt,
    node?.type === "text" ? node.metadata?.content : "",
    ...notes.map((note) => note.content),
  ];
  let prompt = "";
  for (const value of prompts) {
    const part = value?.trim();
    if (!part || `\n\n${prompt}\n\n`.includes(`\n\n${part}\n\n`)) continue;
    prompt = prompt ? `${prompt}\n\n${part}` : part;
  }
  return {
    kind:
      kind ||
      (node?.metadata?.generationKind === "video" || node?.type === "video"
        ? "video"
        : "image"),
    prompt,
    referenceIds: [
      ...new Set(
        [...referenceIds, node?.metadata?.assetId].filter((id): id is string =>
          Boolean(id),
        ),
      ),
    ],
  };
}

export function generationReferenceDefaults(
  kind: "image" | "video",
  supportsFrames: boolean,
  referenceIds: string[],
  assets: Asset[],
  preferredImageId?: string,
  previousEndFrameId = "",
  preserveFrameRoles = false,
) {
  const unique = [...new Set(referenceIds)];
  const images = unique.filter((id) =>
    assets.some((asset) => asset.id === id && asset.kind === "image"),
  );
  const endFrameId =
    kind === "video" && supportsFrames && images.includes(previousEndFrameId)
      ? previousEndFrameId
      : "";
  const candidates = images.filter((id) => id !== endFrameId);
  const firstImage = candidates.includes(preferredImageId || "")
    ? preferredImageId!
    : !preserveFrameRoles && candidates.length === 1
      ? candidates[0]
      : "";
  const startFrameId = kind === "video" && supportsFrames ? firstImage : "";
  return {
    startFrameId,
    endFrameId,
    referenceIds: unique.filter(
      (id) => id !== startFrameId && id !== endFrameId,
    ),
  };
}

export function addAcceptedTask(
  graph: Graph,
  task: Task,
  title: string,
  sourceId?: string,
  sourceReferenceIds?: string[],
): Graph {
  if (graph.nodes.some((node) => node.metadata?.generationTaskId === task.id))
    return graph;
  const acceptedInput = task.input as { shot_id?: string } | undefined;
  const acceptedShotId = acceptedInput?.shot_id;
  const generationInput = readGenerationInput(task.input);
  const nodes = [...graph.nodes];
  const connections = [...graph.connections];
  let source = nodes.find((node) => node.id === sourceId);
  const inputMetadata = {
    generationInput,
    generationInputSourceAssetIds: generationInput
      ? [
          ...new Set(
            sourceReferenceIds ??
              (source?.metadata?.assetId ? [source.metadata.assetId] : []),
          ),
        ]
      : undefined,
  };
  if (!source || source.type !== "config") {
    const reference = source;
    const position = reference
      ? {
          x: reference.position.x + reference.width + 90,
          y: reference.position.y,
        }
      : {
          x: (100 - graph.viewport.x) / graph.viewport.k,
          y: (110 - graph.viewport.y) / graph.viewport.k,
        };
    while (
      nodes.some(
        (node) =>
          node.type !== "group" &&
          position.x < node.position.x + node.width + 20 &&
          position.x + 300 > node.position.x &&
          position.y < node.position.y + node.height + 20 &&
          position.y + 240 > node.position.y,
      )
    )
      position.y += 270;
    source = {
      id: crypto.randomUUID(),
      type: "config" as CanvasNodeType,
      title,
      position,
      width: 280,
      height: 220,
      metadata: {
        prompt: task.prompt,
        productId: task.product_id,
        generationKind: task.kind,
        shotId: acceptedShotId,
        ...inputMetadata,
      },
    };
    nodes.push(source);
    if (reference)
      connections.push({
        id: crypto.randomUUID(),
        fromNodeId: reference.id,
        toNodeId: source.id,
      });
  } else {
    source = {
      ...source,
      metadata: {
        ...source.metadata,
        prompt: task.prompt,
        productId: task.product_id,
        generationKind: task.kind,
        ...inputMetadata,
      },
    };
    nodes[nodes.findIndex((node) => node.id === sourceId)] = source;
  }
  const position = {
    x: source.position.x + source.width + 90,
    y: source.position.y,
  };
  while (
    nodes.some(
      (node) =>
        node.type !== "group" &&
        position.x < node.position.x + node.width + 20 &&
        position.x + 300 + 20 > node.position.x &&
        position.y < node.position.y + node.height + 20 &&
        position.y + 240 + 20 > node.position.y,
    )
  )
    position.y += 270;
  const result: CanvasNodeData = {
    id: crypto.randomUUID(),
    type: task.kind as CanvasNodeType,
    title: `${title} · ${task.id.slice(-6)}`,
    position,
    width: 300,
    height: 240,
    metadata: {
      generationTaskId: task.id,
      executionStatus: task.execution_status,
      deliveryStatus: task.delivery_status,
      procurementStatus: task.procurement_status,
      shotId: acceptedShotId,
      prompt: task.prompt,
      ...(generationInput
        ? { generationInput: readGenerationInput(task.input) }
        : {}),
    },
  };
  return {
    ...graph,
    nodes: [...nodes, result],
    connections: [
      ...connections,
      { id: crypto.randomUUID(), fromNodeId: source.id, toNodeId: result.id },
    ],
  };
}

export function reconcileGeneratedNodes(
  graph: Graph,
  tasks: Task[],
  assets: Asset[],
): Graph {
  let changed = false;
  const nodes = graph.nodes.map((node) => {
    const task = tasks.find(
      (item) => item.id === node.metadata?.generationTaskId,
    );
    if (!task) return node;
    const asset = assets.find((item) => task.asset_ids.includes(item.id));
    const generationInput = readGenerationInput(task.input);
    const metadata = {
      ...node.metadata,
      executionStatus: task.execution_status,
      deliveryStatus: task.delivery_status,
      procurementStatus: task.procurement_status,
      errorMessage: task.error_message || "",
      ...(generationInput ? { generationInput } : {}),
      ...(asset ? { assetId: asset.id } : {}),
    };
    if (JSON.stringify(metadata) === JSON.stringify(node.metadata)) return node;
    changed = true;
    return { ...node, metadata };
  });
  return changed ? { ...graph, nodes } : graph;
}
