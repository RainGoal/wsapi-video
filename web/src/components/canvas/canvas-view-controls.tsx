import { useEffect, useId, useMemo, useRef, useState } from "react";
import type { KeyboardEvent, PointerEvent } from "react";
import { t } from "../../i18n";
import type { CanvasNodeData, ViewportTransform } from "../../types/canvas";
import {
  CANVAS_STACKED_CONTROLS_WIDTH,
  MINI_MAP_SIZE,
  miniMapFrame,
  pointInMiniMap,
  viewportAtMiniMapPoint,
  viewportInMiniMap,
  zoomViewportAtCenter,
  type MiniMapFrame,
} from "../../lib/canvas/canvas-view-geometry";
import "./canvas-view-controls.css";

export type CanvasViewControlsProps = {
  nodes: CanvasNodeData[];
  viewport: ViewportTransform;
  size: { width: number; height: number };
  onViewportChange: (viewport: ViewportTransform) => void;
  onFit: () => void;
  onHelp: () => void;
};

export function CanvasViewControls(props: CanvasViewControlsProps) {
  const { nodes, viewport, size, onViewportChange, onFit, onHelp } = props;
  const [miniMapOpen, setMiniMapOpen] = useState(false);
  const [dragFrame, setDragFrame] = useState<MiniMapFrame | null>(null);
  const mapId = useId();
  const latest = useRef(props);
  latest.current = props;
  const navigation = useRef<{
    pointerId: number;
    frame: MiniMapFrame;
    offset: { x: number; y: number };
  } | null>(null);
  const animation = useRef<number | null>(null);
  const pending = useRef<ViewportTransform | null>(null);
  const mapSurface = useRef<SVGSVGElement | null>(null);
  const frame = useMemo(
    () => dragFrame ?? miniMapFrame(miniMapOpen ? nodes : [], viewport, size),
    [nodes, viewport, size.width, size.height, dragFrame, miniMapOpen],
  );
  const activeFrame = dragFrame ?? frame;
  const visibleArea = viewportInMiniMap(viewport, size, activeFrame);
  const scalePercent = Math.round(Math.max(5, Math.min(500, viewport.k * 100)));

  const requestViewport = (value: ViewportTransform) => {
    pending.current = value;
    if (animation.current !== null) return;
    animation.current = requestAnimationFrame(() => {
      animation.current = null;
      const next = pending.current;
      pending.current = null;
      if (next) latest.current.onViewportChange(next);
    });
  };
  const finishNavigation = () => {
    navigation.current = null;
    setDragFrame(null);
    if (animation.current !== null) cancelAnimationFrame(animation.current);
    animation.current = null;
    const next = pending.current;
    pending.current = null;
    if (next) latest.current.onViewportChange(next);
  };

  useEffect(() => {
    const onBlur = () => {
      const pointerId = navigation.current?.pointerId;
      navigation.current = null;
      if (
        pointerId !== undefined &&
        mapSurface.current?.hasPointerCapture(pointerId)
      )
        mapSurface.current.releasePointerCapture(pointerId);
      setDragFrame(null);
      if (animation.current !== null) cancelAnimationFrame(animation.current);
      animation.current = null;
      pending.current = null;
    };
    window.addEventListener("blur", onBlur);
    return () => {
      window.removeEventListener("blur", onBlur);
      if (animation.current !== null) cancelAnimationFrame(animation.current);
    };
  }, []);

  const pointerInMap = (event: PointerEvent<SVGSVGElement>) => {
    const rect = event.currentTarget.getBoundingClientRect();
    if (!rect.width || !rect.height) return null;
    return {
      x:
        (Math.max(0, Math.min(rect.width, event.clientX - rect.left)) /
          rect.width) *
        MINI_MAP_SIZE.width,
      y:
        (Math.max(0, Math.min(rect.height, event.clientY - rect.top)) /
          rect.height) *
        MINI_MAP_SIZE.height,
    };
  };
  const navigate = (
    event: PointerEvent<SVGSVGElement>,
    mapFrame: MiniMapFrame,
  ) => {
    const point = pointerInMap(event);
    if (!point) return;
    const offset = navigation.current?.offset ?? { x: 0, y: 0 };
    requestViewport(
      viewportAtMiniMapPoint(
        {
          x: point.x - offset.x,
          y: point.y - offset.y,
        },
        mapFrame,
        latest.current.viewport,
        latest.current.size,
      ),
    );
  };

  const mapKeyDown = (event: KeyboardEvent<SVGSVGElement>) => {
    if (event.key === "Escape") {
      event.stopPropagation();
      finishNavigation();
      setMiniMapOpen(false);
      document.getElementById(`${mapId}-toggle`)?.focus();
      return;
    }
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      event.stopPropagation();
      onFit();
      return;
    }
    const directions: Record<string, [number, number]> = {
      ArrowLeft: [1, 0],
      ArrowRight: [-1, 0],
      ArrowUp: [0, 1],
      ArrowDown: [0, -1],
    };
    const direction = directions[event.key];
    if (!direction) return;
    event.preventDefault();
    event.stopPropagation();
    const distance = event.shiftKey ? 0.5 : 0.1;
    onViewportChange({
      ...viewport,
      x: viewport.x + direction[0] * size.width * distance,
      y: viewport.y + direction[1] * size.height * distance,
    });
  };

  return (
    <div
      className={`canvas-view-controls${size.width < CANVAS_STACKED_CONTROLS_WIDTH ? " canvas-view-controls-stacked" : ""}`}
      data-canvas-no-zoom
      onPointerDown={(event) => event.stopPropagation()}
      onDoubleClick={(event) => event.stopPropagation()}
      onWheel={(event) => event.stopPropagation()}
    >
      {miniMapOpen && (
        <div className="canvas-mini-map" id={mapId}>
          <div className="canvas-mini-map-heading">
            <span>{t("小地图")}</span>
            <span>{t("{0} 个节点", nodes.length)}</span>
          </div>
          <svg
            ref={mapSurface}
            viewBox={`0 0 ${MINI_MAP_SIZE.width} ${MINI_MAP_SIZE.height}`}
            className="canvas-mini-map-surface"
            role="group"
            tabIndex={0}
            aria-label={t("画布小地图，拖动或按方向键定位，回车适合画布")}
            onKeyDown={mapKeyDown}
            onPointerDown={(event) => {
              if (event.button !== 0 || navigation.current) return;
              const point = pointerInMap(event);
              if (!point) return;
              const insideViewport =
                point.x >= visibleArea.x &&
                point.x <= visibleArea.x + visibleArea.width &&
                point.y >= visibleArea.y &&
                point.y <= visibleArea.y + visibleArea.height;
              event.preventDefault();
              event.currentTarget.focus();
              event.currentTarget.setPointerCapture(event.pointerId);
              navigation.current = {
                pointerId: event.pointerId,
                frame,
                offset: insideViewport
                  ? {
                      x: point.x - visibleArea.x - visibleArea.width / 2,
                      y: point.y - visibleArea.y - visibleArea.height / 2,
                    }
                  : { x: 0, y: 0 },
              };
              setDragFrame(frame);
              navigate(event, frame);
            }}
            onPointerMove={(event) => {
              if (navigation.current?.pointerId === event.pointerId)
                navigate(event, navigation.current.frame);
            }}
            onPointerUp={(event) => {
              if (navigation.current?.pointerId !== event.pointerId) return;
              navigate(event, navigation.current.frame);
              finishNavigation();
              if (event.currentTarget.hasPointerCapture(event.pointerId))
                event.currentTarget.releasePointerCapture(event.pointerId);
            }}
            onPointerCancel={finishNavigation}
            onLostPointerCapture={() => {
              if (navigation.current) finishNavigation();
            }}
          >
            <MiniMapNodes nodes={nodes} frame={activeFrame} />
            <rect
              className="canvas-mini-map-viewport"
              x={visibleArea.x - Math.max(0, 3 - visibleArea.width) / 2}
              y={visibleArea.y - Math.max(0, 3 - visibleArea.height) / 2}
              width={Math.max(3, visibleArea.width)}
              height={Math.max(3, visibleArea.height)}
              rx="1"
            />
          </svg>
        </div>
      )}
      <div className="canvas-view-dock" role="group" aria-label={t("视图控制")}>
        <button
          type="button"
          id={`${mapId}-toggle`}
          data-canvas-no-zoom
          title={miniMapOpen ? t("关闭小地图") : t("打开小地图")}
          aria-label={miniMapOpen ? t("关闭小地图") : t("打开小地图")}
          aria-pressed={miniMapOpen}
          aria-expanded={miniMapOpen}
          aria-controls={mapId}
          onClick={() => {
            if (miniMapOpen) finishNavigation();
            setMiniMapOpen(!miniMapOpen);
          }}
        >
          <ViewIcon name="map" />
        </button>
        <button
          type="button"
          title={t("适合画布")}
          aria-label={t("适合画布")}
          data-canvas-no-zoom
          onClick={onFit}
        >
          <ViewIcon name="fit" />
        </button>
        <span className="canvas-view-divider" aria-hidden="true" />
        <input
          type="range"
          min="5"
          max="500"
          step="1"
          value={scalePercent}
          aria-label={t("画布缩放")}
          aria-valuetext={`${scalePercent}%`}
          data-canvas-no-zoom
          onChange={(event) =>
            onViewportChange(
              zoomViewportAtCenter(
                viewport,
                size,
                Number(event.target.value) / 100,
              ),
            )
          }
        />
        <output className="canvas-view-scale" aria-live="off">
          {scalePercent}%
        </output>
        <button
          type="button"
          title={t("快捷键")}
          aria-label={t("快捷键")}
          data-canvas-no-zoom
          onClick={onHelp}
        >
          <ViewIcon name="help" />
        </button>
      </div>
    </div>
  );
}

function MiniMapNodes({
  nodes,
  frame,
}: {
  nodes: CanvasNodeData[];
  frame: MiniMapFrame;
}) {
  // One SVG path per node type keeps large graphs from creating thousands of DOM elements.
  const paths = useMemo(() => {
    const result: Record<string, string[]> = {};
    for (const node of nodes) {
      if (
        ![node.position.x, node.position.y, node.width, node.height].every(
          Number.isFinite,
        )
      )
        continue;
      const origin = pointInMiniMap(node.position, frame);
      const corner = pointInMiniMap(
        {
          x: node.position.x + Math.max(0, node.width),
          y: node.position.y + Math.max(0, node.height),
        },
        frame,
      );
      const width = Math.max(1, corner.x - origin.x);
      const height = Math.max(1, corner.y - origin.y);
      (result[node.type] ??= []).push(
        `M${origin.x},${origin.y}h${width}v${height}h${-width}Z`,
      );
    }
    return Object.entries(result).sort(([first], [second]) =>
      first === "group"
        ? -1
        : second === "group"
          ? 1
          : first.localeCompare(second),
    );
  }, [nodes, frame]);
  return (
    <g aria-hidden="true">
      {paths.map(([type, shapes]) => (
        <path
          key={type}
          className={`canvas-mini-map-node canvas-mini-map-node-${type}`}
          d={shapes.join("")}
        />
      ))}
    </g>
  );
}

function ViewIcon({ name }: { name: "map" | "fit" | "help" }) {
  return (
    <svg
      width="17"
      height="17"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {name === "map" && (
        <>
          <path d="m3 6 6-3 6 3 6-3v15l-6 3-6-3-6 3Z" />
          <path d="M9 3v15m6-12v15" />
        </>
      )}
      {name === "fit" && (
        <>
          <path d="M8 3H3v5m13-5h5v5M3 16v5h5m13-5v5h-5" />
          <rect x="8" y="8" width="8" height="8" rx="1" />
        </>
      )}
      {name === "help" && (
        <>
          <circle cx="12" cy="12" r="9" />
          <path d="M9.5 9a2.5 2.5 0 1 1 4 2c-1 .7-1.5 1-1.5 2.5m0 3h.01" />
        </>
      )}
    </svg>
  );
}
