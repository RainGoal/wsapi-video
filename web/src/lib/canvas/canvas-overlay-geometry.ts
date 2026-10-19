import type { CanvasNodeData, ViewportTransform } from "../../types/canvas";

const finite = (value: number, fallback = 0) =>
  Number.isFinite(value) ? Math.max(-1e12, Math.min(1e12, value)) : fallback;
const clamp = (value: number, minimum: number, maximum: number) =>
  Math.max(minimum, Math.min(Math.max(minimum, maximum), value));

export function placeCanvasComposer(
  node: CanvasNodeData,
  viewport: ViewportTransform,
  size: { width: number; height: number },
  width: number,
  bottomInset = 80,
): { x: number; y: number; maxHeight: number } {
  const stageWidth = Math.max(0, finite(size.width));
  const stageHeight = Math.max(0, finite(size.height));
  const left = Math.min(12, stageWidth / 2);
  const right = Math.max(left, stageWidth - left);
  const top = Math.min(12, stageHeight);
  const bottom = Math.max(
    top,
    stageHeight - Math.max(0, finite(bottomInset)) - 12,
  );
  const panelWidth = Math.min(Math.max(0, finite(width)), right - left);
  const panelHeight = Math.min(420, bottom - top);
  const k = clamp(finite(viewport.k, 1), 0.05, 5);
  const nodeLeft = finite(viewport.x) + finite(node.position.x) * k;
  const nodeTop = finite(viewport.y) + finite(node.position.y) * k;
  const nodeRight = nodeLeft + Math.max(0, finite(node.width)) * k;
  const nodeBottom = nodeTop + Math.max(0, finite(node.height)) * k;
  const centerX = (nodeLeft + nodeRight - panelWidth) / 2;
  const centerY = (nodeTop + nodeBottom - panelHeight) / 2;
  const place = (x: number, y: number, height = panelHeight) => {
    const maxHeight = clamp(height, 0, bottom - top);
    return {
      x: clamp(x, left, right - panelWidth),
      y: clamp(y, top, bottom - maxHeight),
      maxHeight,
    };
  };
  const belowY = Math.max(top, nodeBottom + 16);
  const aboveBottom = Math.min(bottom, nodeTop - 16);
  const belowSpace = Math.max(0, bottom - belowY);
  const aboveSpace = Math.max(0, aboveBottom - top);

  if (belowSpace >= 420) return place(centerX, belowY, 420);
  if (aboveSpace >= 420) return place(centerX, aboveBottom - 420, 420);

  const rightX = Math.max(left, nodeRight + 16);
  if (rightX + panelWidth <= right) return place(rightX, centerY);
  const leftX = Math.min(right - panelWidth, nodeLeft - 16 - panelWidth);
  if (leftX >= left) return place(leftX, centerY);

  // A narrow canvas can use the larger free strip with internal scrolling.
  if (belowSpace > 0 && belowSpace >= aboveSpace)
    return place(centerX, belowY, Math.min(420, belowSpace));
  if (aboveSpace > 0)
    return place(
      centerX,
      aboveBottom - Math.min(420, aboveSpace),
      Math.min(420, aboveSpace),
    );

  // A node covering the whole usable viewport leaves no non-overlapping position.
  return place(centerX, nodeBottom + 16);
}
