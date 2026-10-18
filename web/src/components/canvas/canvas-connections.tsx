import type { MouseEvent as ReactMouseEvent } from "react";

import { canvasThemes } from "@/lib/canvas-theme";
import { useThemeStore } from "@/stores/use-theme-store";
import type {
  CanvasConnection,
  CanvasNodeData,
  ConnectionHandle,
  Position,
} from "@/types/canvas";

export function ConnectionPath({
  connection,
  from,
  to,
  active,
  label,
  onSelect,
  onContextMenu,
}: {
  connection: CanvasConnection;
  from: CanvasNodeData;
  to: CanvasNodeData;
  active: boolean;
  label?: string;
  onSelect: () => void;
  onContextMenu?: (event: ReactMouseEvent<SVGPathElement>) => void;
}) {
  const theme = canvasThemes[useThemeStore((state) => state.theme)];
  const startX = from.position.x + from.width;
  const startY = from.position.y + from.height / 2;
  const endX = to.position.x;
  const endY = to.position.y + to.height / 2;
  const dx = Math.abs(endX - startX);
  const curvature = Math.max(dx * 0.5, 50);
  const pathD = `M ${startX} ${startY} C ${startX + curvature} ${startY}, ${endX - curvature} ${endY}, ${endX} ${endY}`;

  return (
    <g>
      <title>{label || `${from.title} → ${to.title}`}</title>
      <path
        data-connection-id={connection.id}
        d={pathD}
        stroke="transparent"
        strokeWidth="16"
        vectorEffect="non-scaling-stroke"
        fill="none"
        role="button"
        tabIndex={0}
        aria-label={label || `${from.title} → ${to.title}`}
        aria-pressed={active}
        style={{ cursor: "pointer", pointerEvents: "stroke" }}
        onPointerDown={(event) => event.stopPropagation()}
        onClick={(event) => {
          event.stopPropagation();
          onSelect();
        }}
        onKeyDown={(event) => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            onSelect();
          }
        }}
        onContextMenu={(event) => {
          event.preventDefault();
          event.stopPropagation();
          onContextMenu?.(event);
        }}
      />
      <path
        d={`M ${endX - 9} ${endY - 5} L ${endX - 2} ${endY} L ${endX - 9} ${endY + 5}`}
        stroke={active ? theme.node.activeStroke : theme.node.muted}
        strokeWidth="2"
        fill="none"
        style={{ pointerEvents: "none" }}
      />
      <path
        d={pathD}
        stroke={active ? theme.node.activeStroke : theme.node.muted}
        strokeWidth={active ? 3 : 2}
        vectorEffect="non-scaling-stroke"
        strokeOpacity={active ? 1 : 0.82}
        fill="none"
        style={{
          filter: active
            ? `drop-shadow(0 0 8px ${theme.node.activeStroke}66)`
            : undefined,
          pointerEvents: "none",
        }}
      />
    </g>
  );
}

export function ActiveConnectionPath({
  node,
  handle,
  mouseWorld,
  target,
}: {
  node?: CanvasNodeData;
  handle: ConnectionHandle;
  mouseWorld: Position;
  target?: CanvasNodeData;
}) {
  const theme = canvasThemes[useThemeStore((state) => state.theme)];
  if (!node) return null;

  const startX =
    handle.handleType === "source"
      ? node.position.x + node.width
      : mouseWorld.x;
  const startY =
    handle.handleType === "source"
      ? node.position.y + node.height / 2
      : mouseWorld.y;
  const endX = handle.handleType === "source" ? mouseWorld.x : node.position.x;
  const endY =
    handle.handleType === "source"
      ? mouseWorld.y
      : node.position.y + node.height / 2;
  const snappedStartX =
    handle.handleType === "target" && target
      ? target.position.x + target.width
      : startX;
  const snappedStartY =
    handle.handleType === "target" && target
      ? target.position.y + target.height / 2
      : startY;
  const snappedEndX =
    handle.handleType === "source" && target ? target.position.x : endX;
  const snappedEndY =
    handle.handleType === "source" && target
      ? target.position.y + target.height / 2
      : endY;
  const curvature = Math.max(Math.abs(snappedEndX - snappedStartX) * 0.5, 50);
  const pathD = `M ${snappedStartX} ${snappedStartY} C ${snappedStartX + curvature} ${snappedStartY}, ${snappedEndX - curvature} ${snappedEndY}, ${snappedEndX} ${snappedEndY}`;

  return (
    <path
      data-connection-preview="true"
      d={pathD}
      stroke={theme.node.activeStroke}
      strokeWidth="2"
      vectorEffect="non-scaling-stroke"
      fill="none"
      strokeDasharray="5,5"
      style={{ pointerEvents: "none" }}
    />
  );
}
