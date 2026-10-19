import type { Asset, Product, Task } from "./types";
import type {
  CanvasNodeData,
  CanvasNodeType,
  GenerationDraft,
  Graph,
} from "./types/canvas";

export function readGenerationDraft(
  value: unknown,
): GenerationDraft | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return;
  const draft = value as Record<string, unknown>;
  if (draft.kind !== "image" && draft.kind !== "video") return;
  const strings = [
    "productId",
    "prompt",
    "ratio",
    "resolution",
    "size",
    "quality",
    "startFrameId",
    "endFrameId",
  ] as const;
  if (
    strings.some(
      (key) => draft[key] !== undefined && typeof draft[key] !== "string",
    )
  )
    return;
  if (
    (draft.duration !== undefined &&
      (typeof draft.duration !== "number" ||
        !Number.isFinite(draft.duration) ||
        draft.duration < 0)) ||
    (draft.audio !== undefined && typeof draft.audio !== "boolean")
  )
    return;
  for (const key of ["referenceIds", "connectedAssetIds"])
    if (
      draft[key] !== undefined &&
      (!Array.isArray(draft[key]) ||
        draft[key].some((id) => typeof id !== "string" || !id))
    )
      return;
  return {
    kind: draft.kind,
    productId: (draft.productId as string | undefined) ?? "",
    prompt: (draft.prompt as string | undefined) ?? "",
    ratio: (draft.ratio as string | undefined) ?? "",
    duration: (draft.duration as number | undefined) ?? 0,
    resolution: (draft.resolution as string | undefined) ?? "",
    size: (draft.size as string | undefined) ?? "",
    quality: (draft.quality as string | undefined) ?? "",
    audio: (draft.audio as boolean | undefined) ?? false,
    referenceIds: [
      ...new Set((draft.referenceIds as string[] | undefined) ?? []),
    ],
    startFrameId: (draft.startFrameId as string | undefined) ?? "",
    endFrameId: (draft.endFrameId as string | undefined) ?? "",
    ...(draft.referenceMode === "reference" || draft.referenceMode === "frames"
      ? { referenceMode: draft.referenceMode }
      : {}),
    connectedAssetIds: [
      ...new Set((draft.connectedAssetIds as string[] | undefined) ?? []),
    ],
  };
}

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
  const connected = [...new Set(connectedIds)];
  const canRestore = ["config", "image", "video"].includes(node?.type ?? "");
  const draft =
    canRestore && readGenerationDraft(node?.metadata?.generationDraft);
  if (draft) {
    const retained = (id: string) =>
      !draft.connectedAssetIds.includes(id) || connected.includes(id);
    const startFrameId = retained(draft.startFrameId) ? draft.startFrameId : "";
    const endFrameId = retained(draft.endFrameId) ? draft.endFrameId : "";
    return {
      referenceIds: [
        ...draft.referenceIds.filter(retained),
        ...connected.filter(
          (id) =>
            !draft.connectedAssetIds.includes(id) &&
            id !== startFrameId &&
            id !== endFrameId,
        ),
      ].filter((id, index, ids) => ids.indexOf(id) === index),
      startFrameId,
      endFrameId,
      restored: true,
    };
  }
  const input = readGenerationInput(node?.metadata?.generationInput);
  const baseline = node?.metadata?.generationInputSourceAssetIds;
  const restored =
    canRestore &&
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
  const draft = readGenerationDraft(node?.metadata?.generationDraft);
  const prompts = draft
    ? []
    : [
        node?.metadata?.prompt,
        node?.type === "text" ? node.metadata?.content : "",
        ...notes.map((note) => note.content),
      ];
  let prompt = draft?.prompt ?? "";
  for (const value of prompts) {
    const part = value?.trim();
    if (!part || `\n\n${prompt}\n\n`.includes(`\n\n${part}\n\n`)) continue;
    prompt = prompt ? `${prompt}\n\n${part}` : part;
  }
  return {
    kind:
      kind ||
      draft?.kind ||
      (node?.metadata?.generationKind === "video" || node?.type === "video"
        ? "video"
        : "image"),
    prompt,
    referenceIds: [
      ...new Set(
        [
          ...referenceIds,
          ...(!node?.metadata?.generationTaskId
            ? [node?.metadata?.assetId]
            : []),
        ].filter((id): id is string => Boolean(id)),
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

export function normalizeGenerationDraft(
  draft: GenerationDraft,
  product: Product,
): GenerationDraft {
  const caps = product.capabilities;
  const supported = <T extends string | number>(
    value: T,
    values: T[],
    fallback: T,
  ): T => (values.includes(value) ? value : (values[0] ?? fallback));
  const frames = product.kind === "video" && caps.supports_frames;
  return {
    ...draft,
    kind: product.kind,
    productId: product.id,
    ratio: supported(draft.ratio, caps.aspect_ratios ?? [], ""),
    duration:
      product.kind === "video"
        ? supported(draft.duration, caps.durations ?? [], 0)
        : 0,
    resolution: supported(draft.resolution, caps.resolutions ?? [], ""),
    size:
      product.kind === "image"
        ? supported(draft.size, caps.sizes ?? [], "")
        : "",
    quality:
      product.kind === "image"
        ? supported(draft.quality, caps.qualities ?? [], "")
        : "",
    audio: product.kind === "video" && caps.supports_audio && draft.audio,
    referenceIds: [
      ...new Set([
        ...draft.referenceIds,
        ...(!frames
          ? [draft.startFrameId, draft.endFrameId].filter(Boolean)
          : []),
      ]),
    ],
    startFrameId: frames ? draft.startFrameId : "",
    endFrameId: frames ? draft.endFrameId : "",
    ...(draft.referenceMode
      ? { referenceMode: frames ? draft.referenceMode : ("reference" as const) }
      : {}),
    connectedAssetIds: [...draft.connectedAssetIds],
  };
}

function generationTaskTime(value: unknown) {
  if (typeof value !== "string") return;
  const milliseconds = Date.parse(value);
  if (!Number.isFinite(milliseconds)) return;
  // Date.parse truncates server nanoseconds; retain the remainder for ordering.
  const fraction = (value.match(/\.(\d+)(?:Z|[+-]\d{2}:\d{2})$/i)?.[1] ?? "")
    .slice(3)
    .padEnd(6, "0");
  return { value, milliseconds, fraction };
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
  if (sourceId !== undefined && !source) return graph;
  const inputMetadata = {
    generationInput,
    generationInputSourceAssetIds: generationInput
      ? [
          ...new Set(
            sourceReferenceIds ??
              (source?.metadata?.assetId &&
              !["image", "video"].includes(source.type)
                ? [source.metadata.assetId]
                : []),
          ),
        ]
      : undefined,
  };
  if (source && ["image", "video"].includes(source.type)) {
    const previousIds = Array.isArray(
      source.metadata?.generationPreviousTaskIds,
    )
      ? source.metadata.generationPreviousTaskIds.filter(
          (id): id is string => typeof id === "string",
        )
      : [];
    if (previousIds.includes(task.id)) return graph;
    const currentTime = generationTaskTime(
      source.metadata?.generationTaskCreatedAt,
    );
    const incomingTime = generationTaskTime(task.created_at);
    if (
      currentTime &&
      (!incomingTime ||
        incomingTime.milliseconds < currentTime.milliseconds ||
        (incomingTime.milliseconds === currentTime.milliseconds &&
          incomingTime.fraction < currentTime.fraction))
    )
      return graph;
    const draft = readGenerationDraft(source.metadata?.generationDraft);
    const previousAssetId = source.metadata?.assetId;
    const metadata = {
      ...source.metadata,
      ...inputMetadata,
      ...(draft
        ? {
            generationDraft: {
              ...draft,
              ...(source.type !== task.kind && draft.kind !== task.kind
                ? {
                    kind: task.kind as GenerationDraft["kind"],
                    productId: task.product_id,
                  }
                : {}),
              connectedAssetIds: draft.connectedAssetIds.filter(
                (id) =>
                  id !== previousAssetId || sourceReferenceIds?.includes(id),
              ),
            },
          }
        : {}),
      generationTaskId: task.id,
      generationTaskCreatedAt: incomingTime?.value,
      generationPreviousTaskIds: [
        ...new Set([
          ...previousIds,
          ...(typeof source.metadata?.generationTaskId === "string"
            ? [source.metadata.generationTaskId]
            : []),
        ]),
      ],
      generationKind: task.kind,
      prompt: task.prompt,
      productId: task.product_id,
      shotId: acceptedShotId,
      assetId: undefined,
      executionStatus: task.execution_status,
      deliveryStatus: task.delivery_status,
      procurementStatus: task.procurement_status,
      errorMessage: task.error_message || "",
      error_message: undefined,
    };
    return {
      ...graph,
      nodes: nodes.map((node) =>
        node.id === sourceId
          ? { ...node, type: task.kind as CanvasNodeType, metadata }
          : node,
      ),
    };
  }
  if (!source || !["config", "image", "video"].includes(source.type)) {
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
  } else if (source.type === "config" || !source.metadata?.generationTaskId) {
    source = {
      ...source,
      metadata: {
        ...source.metadata,
        ...(source.type === "config" ? { prompt: task.prompt } : {}),
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
      generationSourceNodeId: source.id,
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
    const asset =
      task.execution_status === "completed" && task.delivery_status === "ready"
        ? assets.find(
            (item) =>
              task.asset_ids.includes(item.id) && item.kind === task.kind,
          )
        : undefined;
    const generationInput = readGenerationInput(task.input);
    const metadata = {
      ...node.metadata,
      executionStatus: task.execution_status,
      deliveryStatus: task.delivery_status,
      procurementStatus: task.procurement_status,
      errorMessage: task.error_message || "",
      error_message: undefined,
      ...(generationInput ? { generationInput } : {}),
      assetId: asset?.id,
    };
    if (JSON.stringify(metadata) === JSON.stringify(node.metadata)) return node;
    changed = true;
    return { ...node, metadata };
  });
  return changed ? { ...graph, nodes } : graph;
}
