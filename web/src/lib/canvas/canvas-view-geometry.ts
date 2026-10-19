import type { CanvasNodeData, ViewportTransform } from "../../types/canvas";

type Size = { width: number; height: number };
type Point = { x: number; y: number };
export type MiniMapFrame = Size & {
  left: number;
  top: number;
  scale: number;
  insetX: number;
  insetY: number;
};

export const MINI_MAP_SIZE = { width: 240, height: 144 };
export const CANVAS_STACKED_CONTROLS_WIDTH = 1000;
const finite = (value: number, fallback = 0) =>
  Number.isFinite(value) ? value : fallback;
// Keep malformed or extreme persisted coordinates from overflowing SVG geometry.
const world = (value: number) => Math.max(-1e12, Math.min(1e12, finite(value)));
const zoom = (value: number) => Math.max(0.05, Math.min(5, finite(value, 1)));

export function zoomViewportAtCenter(
  viewport: ViewportTransform,
  size: Size,
  nextScale: number,
): ViewportTransform {
  const k = zoom(nextScale);
  const previousScale = zoom(viewport.k);
  const centerX = Math.max(0, finite(size.width)) / 2;
  const centerY = Math.max(0, finite(size.height)) / 2;
  return {
    x: centerX - ((centerX - finite(viewport.x)) / previousScale) * k,
    y: centerY - ((centerY - finite(viewport.y)) / previousScale) * k,
    k,
  };
}

export function miniMapFrame(
  nodes: CanvasNodeData[],
  viewport: ViewportTransform,
  size: Size,
): MiniMapFrame {
  const k = zoom(viewport.k);
  const viewLeft = world(-finite(viewport.x) / k);
  const viewTop = world(-finite(viewport.y) / k);
  let left = viewLeft;
  let top = viewTop;
  let right = world(viewLeft + Math.max(0, finite(size.width)) / k);
  let bottom = world(viewTop + Math.max(0, finite(size.height)) / k);
  for (const node of nodes) {
    if (
      !Number.isFinite(node.position.x) ||
      !Number.isFinite(node.position.y) ||
      !Number.isFinite(node.width) ||
      !Number.isFinite(node.height)
    )
      continue;
    left = Math.min(left, world(node.position.x));
    top = Math.min(top, world(node.position.y));
    right = Math.max(right, world(node.position.x + Math.max(0, node.width)));
    bottom = Math.max(
      bottom,
      world(node.position.y + Math.max(0, node.height)),
    );
  }
  const width = Math.max(160, right - left);
  const height = Math.max(100, bottom - top);
  left -= (width - (right - left)) / 2;
  top -= (height - (bottom - top)) / 2;
  const scale = Math.min(
    (MINI_MAP_SIZE.width - 24) / width,
    (MINI_MAP_SIZE.height - 24) / height,
  );
  return {
    left,
    top,
    width,
    height,
    scale,
    insetX: (MINI_MAP_SIZE.width - width * scale) / 2,
    insetY: (MINI_MAP_SIZE.height - height * scale) / 2,
  };
}

export function pointInMiniMap(point: Point, frame: MiniMapFrame): Point {
  return {
    x: (world(point.x) - frame.left) * frame.scale + frame.insetX,
    y: (world(point.y) - frame.top) * frame.scale + frame.insetY,
  };
}

export function viewportAtMiniMapPoint(
  point: Point,
  frame: MiniMapFrame,
  viewport: ViewportTransform,
  size: Size,
): ViewportTransform {
  const k = zoom(viewport.k);
  const worldX = (point.x - frame.insetX) / frame.scale + frame.left;
  const worldY = (point.y - frame.insetY) / frame.scale + frame.top;
  return {
    x: Math.max(0, finite(size.width)) / 2 - worldX * k,
    y: Math.max(0, finite(size.height)) / 2 - worldY * k,
    k,
  };
}

export function viewportInMiniMap(
  viewport: ViewportTransform,
  size: Size,
  frame: MiniMapFrame,
): Point & Size {
  const k = zoom(viewport.k);
  const origin = pointInMiniMap(
    { x: -viewport.x / k, y: -viewport.y / k },
    frame,
  );
  return {
    ...origin,
    width: (Math.max(0, finite(size.width)) / k) * frame.scale,
    height: (Math.max(0, finite(size.height)) / k) * frame.scale,
  };
}
