import type { Capabilities, Product } from "../types";

export type CapabilityLists = Record<
  "aspect_ratios" | "durations" | "resolutions" | "sizes" | "qualities",
  string
>;
const split = (text: string) => [
  ...new Set(
    text
      .split(/[,，\n]/)
      .map((value) => value.trim())
      .filter(Boolean),
  ),
];

export function productCapabilities(
  kind: Product["kind"],
  capabilities: Capabilities,
  lists: CapabilityLists,
): Capabilities {
  return {
    ...capabilities,
    aspect_ratios: split(lists.aspect_ratios),
    durations: kind === "video" ? split(lists.durations).map(Number) : [],
    resolutions: kind === "video" ? split(lists.resolutions) : [],
    sizes: kind === "image" ? split(lists.sizes) : [],
    qualities: kind === "image" ? split(lists.qualities) : [],
    ...(kind === "image"
      ? {
          max_videos: 0,
          max_audios: 0,
          supports_audio: false,
          supports_frames: false,
          supports_cancel: false,
        }
      : {}),
  };
}

function definition(product: Product) {
  const c = product.capabilities;
  return JSON.stringify({
    kind: product.kind,
    model: product.model,
    aspect_ratios: c.aspect_ratios || [],
    durations: c.durations || [],
    resolutions: c.resolutions || [],
    sizes: c.sizes || [],
    qualities: c.qualities || [],
    max_images: c.max_images || 0,
    max_videos: c.max_videos || 0,
    max_audios: c.max_audios || 0,
    supports_audio: !!c.supports_audio,
    supports_frames: !!c.supports_frames,
    supports_cancel: !!c.supports_cancel,
    max_reference_duration_seconds: c.max_reference_duration_seconds || 0,
    max_reference_bytes: c.max_reference_bytes || 0,
    max_reference_width: c.max_reference_width || 0,
    max_reference_height: c.max_reference_height || 0,
  });
}

export function productDefinitionChanged(initial: Product, next: Product) {
  return (
    initial.version > 0 &&
    definition(initial) !== definition({ ...next, model: next.model.trim() })
  );
}
