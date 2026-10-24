import type { Product } from "./types";

export type SwapImage = {
  asset_id: string;
  angle: "unknown" | "front" | "side" | "back" | "detail";
};
export type SwapSettings = {
  source_video_id: string;
  images: SwapImage[];
  target: string;
  instructions: string;
  preserve_scene: boolean;
  audio_mode: "original" | "muted" | "generated";
  analysis_id?: string;
};
export type SwapDraft = {
  swap: SwapSettings;
  product_id: string;
  duration: number;
  resolution: string;
  aspect_ratio: string;
  last_task_id?: string;
};
export type SwapAnalysis = {
  id: string;
  model: string;
  target: string;
  action: string;
  occlusion: string;
  product_description: string;
  risks: string[];
  missing_angles: string[];
  sampled_at: number[];
};
export const emptySwapDraft = (): SwapDraft => ({
  swap: {
    source_video_id: "",
    images: [],
    target: "",
    instructions: "",
    preserve_scene: true,
    audio_mode: "original",
  },
  product_id: "",
  duration: 0,
  resolution: "",
  aspect_ratio: "",
});
export const swapDraftKey = (userID: string) =>
  `frame:draft:${userID}:product-swap`;

export function readSwapDraft(raw: string | null): SwapDraft {
  if (!raw) return emptySwapDraft();
  const draft: SwapDraft = JSON.parse(raw);
  const swap = draft?.swap;
  if (
    !swap ||
    typeof swap.source_video_id !== "string" ||
    !Array.isArray(swap.images) ||
    swap.images.length > 6 ||
    swap.images.some(
      (image) =>
        !image ||
        typeof image.asset_id !== "string" ||
        !["unknown", "front", "side", "back", "detail"].includes(image.angle),
    ) ||
    typeof swap.target !== "string" ||
    typeof swap.instructions !== "string" ||
    typeof swap.preserve_scene !== "boolean" ||
    !["original", "muted", "generated"].includes(swap.audio_mode) ||
    typeof draft.product_id !== "string" ||
    !Number.isSafeInteger(draft.duration) ||
    typeof draft.resolution !== "string" ||
    typeof draft.aspect_ratio !== "string" ||
    (draft.last_task_id !== undefined &&
      !/^task_[0-9a-f]{32}$/.test(draft.last_task_id))
  )
    throw new Error("Invalid product replacement draft");
  // Analyses are bound to exact server inputs and are never restored from local text.
  return { ...draft, swap: { ...swap, analysis_id: undefined } };
}

export function swapProducts(products: Product[]) {
  return products.filter(
    (p) =>
      p.kind === "video" &&
      !p.demo &&
      /seedance/i.test(p.model) &&
      p.capabilities.supports_product_swap &&
      p.capabilities.max_images > 0 &&
      p.capabilities.max_videos > 0 &&
      p.capabilities.durations.some((value) => value <= 30) &&
      p.capabilities.resolutions.length > 0 &&
      p.capabilities.aspect_ratios.length > 0,
  );
}

export const swapQuoteInput = (projectID: string, draft: SwapDraft) => ({
  project_id: projectID,
  product_id: draft.product_id,
  duration: draft.duration,
  resolution: draft.resolution,
  aspect_ratio: draft.aspect_ratio,
  product_swap: draft.swap,
});
