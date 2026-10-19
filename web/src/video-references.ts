import type { Asset } from "./types";
import type { GenerationDraft } from "./types/canvas";

export function videoReferenceMode(
  draft: GenerationDraft,
  supportsFrames: boolean,
): "reference" | "frames" | "mixed" {
  if (draft.kind !== "video" || !supportsFrames) return "reference";
  const frames =
    draft.startFrameId || draft.endFrameId || draft.referenceMode === "frames";
  return frames
    ? draft.referenceIds.length
      ? "mixed"
      : "frames"
    : "reference";
}

// Modes arrange real asset roles; there is no separate upstream mode parameter.
// Keep every selected asset when changing modes, including existing mixed inputs.
export function changeVideoReferenceMode(
  draft: GenerationDraft,
  mode: "reference" | "frames",
  assets: Asset[],
): GenerationDraft {
  const selected = [
    ...new Set(
      [...draft.referenceIds, draft.startFrameId, draft.endFrameId].filter(
        Boolean,
      ),
    ),
  ];
  if (mode === "reference")
    return {
      ...draft,
      referenceMode: mode,
      referenceIds: selected,
      startFrameId: "",
      endFrameId: "",
    };
  const images = selected.filter((id) =>
    assets.some((asset) => asset.id === id && asset.kind === "image"),
  );
  const startFrameId =
    draft.startFrameId || images.find((id) => id !== draft.endFrameId) || "";
  const endFrameId =
    draft.endFrameId || images.find((id) => id !== startFrameId) || "";
  return {
    ...draft,
    referenceMode: mode,
    startFrameId,
    endFrameId,
    referenceIds: selected.filter(
      (id) => id !== startFrameId && id !== endFrameId,
    ),
  };
}
