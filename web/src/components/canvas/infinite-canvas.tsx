import React, { useEffect, useRef, useState } from "react";

import { canvasThemes, type CanvasBackgroundMode } from "@/lib/canvas-theme";
import { useThemeStore } from "@/stores/use-theme-store";
import type { ViewportTransform } from "@/types/canvas";

type InfiniteCanvasProps = {
  containerRef: React.RefObject<HTMLDivElement | null>;
  viewport: ViewportTransform;
  tool: "select" | "pan";
  backgroundMode?: CanvasBackgroundMode;
  onViewportChange: (viewport: ViewportTransform) => void;
  onGestureStart?: () => void;
  onCanvasMouseDown?: (event: React.PointerEvent<HTMLDivElement>) => void;
  onCanvasDeselect?: () => void;
  onCanvasDoubleClick?: (event: React.MouseEvent<HTMLDivElement>) => void;
  onContextMenu?: (event: React.MouseEvent) => void;
  onDrop?: (event: React.DragEvent<HTMLDivElement>) => void;
  children: React.ReactNode;
};

export function InfiniteCanvas({
  containerRef,
  viewport,
  tool,
  backgroundMode = "lines",
  onViewportChange,
  onGestureStart,
  onCanvasMouseDown,
  onCanvasDeselect,
  onCanvasDoubleClick,
  onContextMenu,
  onDrop,
  children,
}: InfiniteCanvasProps) {
  const theme = canvasThemes[useThemeStore((state) => state.theme)];
  const panState = useRef({
    isPanning: false,
    startX: 0,
    startY: 0,
    initialX: 0,
    initialY: 0,
    hasMoved: false,
    startedOnBackground: false,
  });
  const scaleRef = useRef(viewport.k);
  const frameRef = useRef<number | null>(null);
  const nextViewportRef = useRef<ViewportTransform | null>(null);
  const gestureCallbacks = useRef({ onViewportChange, onCanvasDeselect });
  gestureCallbacks.current = { onViewportChange, onCanvasDeselect };
  const touches = useRef(new Map<number, { x: number; y: number }>());
  const pinch = useRef<{
    distance: number;
    viewport: ViewportTransform;
    world: { x: number; y: number };
  } | null>(null);
  const [isSpacePressed, setIsSpacePressed] = useState(false);
  const [isControlPressed, setIsControlPressed] = useState(false);
  const [isPanning, setIsPanning] = useState(false);

  useEffect(() => {
    scaleRef.current = viewport.k;
  }, [viewport.k]);

  useEffect(
    () => () => {
      if (frameRef.current) cancelAnimationFrame(frameRef.current);
    },
    [],
  );

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      const target = event.target instanceof Element ? event.target : null;
      if (
        !target ||
        !(
          containerRef.current?.contains(target) ||
          containerRef.current
            ?.closest("[data-canvas-keyboard-scope]")
            ?.contains(target)
        )
      )
        return;
      if (event.key === "Control") setIsControlPressed(true);
      if (event.code !== "Space") return;
      if (
        event.target instanceof HTMLInputElement ||
        event.target instanceof HTMLTextAreaElement ||
        event.target instanceof HTMLSelectElement ||
        target?.closest(
          "[contenteditable='true'],button,a,[role=button],[role=dialog]",
        )
      )
        return;
      event.preventDefault();
      setIsSpacePressed(true);
    };

    const handleKeyUp = (event: KeyboardEvent) => {
      if (event.code === "Space") {
        const target = event.target instanceof Element ? event.target : null;
        if (
          !(
            event.target instanceof HTMLInputElement ||
            event.target instanceof HTMLTextAreaElement ||
            event.target instanceof HTMLSelectElement ||
            target?.closest(
              "[contenteditable='true'],button,a,[role=button],[role=dialog]",
            )
          )
        )
          event.preventDefault();
        setIsSpacePressed(false);
      }
      if (event.key === "Control") setIsControlPressed(false);
    };

    const handleBlur = () => {
      setIsSpacePressed(false);
      setIsControlPressed(false);
      panState.current.isPanning = false;
      touches.current.clear();
      pinch.current = null;
      setIsPanning(false);
      document.body.style.cursor = "";
    };

    window.addEventListener("keydown", handleKeyDown);
    window.addEventListener("keyup", handleKeyUp);
    window.addEventListener("blur", handleBlur);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
      window.removeEventListener("keyup", handleKeyUp);
      window.removeEventListener("blur", handleBlur);
    };
  }, []);

  const handleWheel = (event: React.WheelEvent<HTMLDivElement>) => {
    const target = event.target instanceof Element ? event.target : null;
    if (
      target?.closest(
        "[data-canvas-no-zoom],.ant-modal,.ant-popover,.ant-dropdown,.ant-select-dropdown,.ant-picker-dropdown",
      )
    )
      return;

    const delta = -event.deltaY;
    const factor = Math.pow(1.1, delta / 100);
    const newScale = Math.min(Math.max(viewport.k * factor, 0.05), 5);
    const rect = containerRef.current?.getBoundingClientRect();
    if (!rect) return;

    const mouseX = event.clientX - rect.left;
    const mouseY = event.clientY - rect.top;
    const worldX = (mouseX - viewport.x) / viewport.k;
    const worldY = (mouseY - viewport.y) / viewport.k;

    onViewportChange({
      x: mouseX - worldX * newScale,
      y: mouseY - worldY * newScale,
      k: newScale,
    });
  };

  const handlePointerDown = (event: React.PointerEvent<HTMLDivElement>) => {
    if (pinch.current) return;
    const target = event.target instanceof Element ? event.target : null;
    if (target?.closest("[data-canvas-no-zoom]")) return;
    if (target?.closest("[data-connection-create-menu]")) return;
    const isBackgroundClick = !target?.closest(
      "[data-node-id],[data-connection-id]",
    );
    const temporaryTool = event.ctrlKey || isSpacePressed;
    const activeTool = temporaryTool
      ? tool === "select"
        ? "pan"
        : "select"
      : tool;
    const shouldPan =
      event.button === 1 || (event.button === 0 && activeTool === "pan");

    if (shouldPan) {
      event.preventDefault();
      event.currentTarget.setPointerCapture(event.pointerId);
      panState.current = {
        isPanning: true,
        startX: event.clientX,
        startY: event.clientY,
        initialX: viewport.x,
        initialY: viewport.y,
        hasMoved: false,
        startedOnBackground: isBackgroundClick,
      };
      setIsPanning(true);
      document.body.style.cursor = "grabbing";
      return;
    }

    if (event.button === 0 && isBackgroundClick) {
      event.preventDefault();
      event.currentTarget.setPointerCapture(event.pointerId);
      onCanvasMouseDown?.(event);
    }
  };

  const handleTouchStart = (event: React.PointerEvent<HTMLDivElement>) => {
    if (event.pointerType !== "touch") return;
    touches.current.set(event.pointerId, {
      x: event.clientX,
      y: event.clientY,
    });
    if (touches.current.size !== 2) return;
    const [first, second] = [...touches.current.values()];
    const rect = containerRef.current?.getBoundingClientRect();
    if (!rect) return;
    event.preventDefault();
    event.stopPropagation();
    for (const id of touches.current.keys())
      event.currentTarget.setPointerCapture(id);
    const center = {
      x: (first.x + second.x) / 2 - rect.left,
      y: (first.y + second.y) / 2 - rect.top,
    };
    pinch.current = {
      distance: Math.max(1, Math.hypot(second.x - first.x, second.y - first.y)),
      viewport,
      world: {
        x: (center.x - viewport.x) / viewport.k,
        y: (center.y - viewport.y) / viewport.k,
      },
    };
    panState.current.isPanning = false;
    setIsPanning(false);
    document.body.style.cursor = "";
    onGestureStart?.();
  };

  const handleDoubleClick = (event: React.MouseEvent<HTMLDivElement>) => {
    const target = event.target instanceof Element ? event.target : null;
    if (
      target?.closest(
        "[data-canvas-no-zoom],[data-node-id],[data-connection-id]",
      )
    )
      return;
    onCanvasDoubleClick?.(event);
  };

  useEffect(() => {
    const handlePointerMove = (event: PointerEvent) => {
      if (touches.current.has(event.pointerId))
        touches.current.set(event.pointerId, {
          x: event.clientX,
          y: event.clientY,
        });
      if (pinch.current && touches.current.size >= 2) {
        const [first, second] = [...touches.current.values()];
        const rect = containerRef.current?.getBoundingClientRect();
        if (!rect) return;
        const k = Math.min(
          5,
          Math.max(
            0.05,
            (pinch.current.viewport.k *
              Math.hypot(second.x - first.x, second.y - first.y)) /
              pinch.current.distance,
          ),
        );
        gestureCallbacks.current.onViewportChange({
          x: (first.x + second.x) / 2 - rect.left - pinch.current.world.x * k,
          y: (first.y + second.y) / 2 - rect.top - pinch.current.world.y * k,
          k,
        });
        return;
      }
      if (!panState.current.isPanning) return;

      const dx = event.clientX - panState.current.startX;
      const dy = event.clientY - panState.current.startY;
      if (Math.abs(dx) > 3 || Math.abs(dy) > 3) {
        panState.current.hasMoved = true;
      }

      nextViewportRef.current = {
        x: panState.current.initialX + dx,
        y: panState.current.initialY + dy,
        k: scaleRef.current,
      };
      if (frameRef.current) return;
      frameRef.current = requestAnimationFrame(() => {
        frameRef.current = null;
        if (nextViewportRef.current)
          gestureCallbacks.current.onViewportChange(nextViewportRef.current);
      });
    };

    const handlePointerUp = (event: PointerEvent) => {
      touches.current.delete(event.pointerId);
      if (pinch.current) {
        if (touches.current.size < 2) pinch.current = null;
        return;
      }
      if (!panState.current.isPanning) return;

      if (!panState.current.hasMoved && panState.current.startedOnBackground) {
        gestureCallbacks.current.onCanvasDeselect?.();
      }
      panState.current.isPanning = false;
      setIsPanning(false);
      document.body.style.cursor = "";
    };

    window.addEventListener("pointermove", handlePointerMove);
    // A node's pointer-up can render its inspector before window bubbling.
    // Clean up active pointers first, without resubscribing on each viewport edit.
    window.addEventListener("pointerup", handlePointerUp, true);
    window.addEventListener("pointercancel", handlePointerUp, true);
    return () => {
      window.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", handlePointerUp, true);
      window.removeEventListener("pointercancel", handlePointerUp, true);
      document.body.style.cursor = "";
    };
  }, [containerRef]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    // Prevent canvas scrolling from moving the page while preserving native scrolling inside overlays and dialogs.
    const preventWheelScroll = (event: WheelEvent) => {
      const target = event.target instanceof Element ? event.target : null;
      if (
        target?.closest(
          "[data-canvas-no-zoom],.ant-modal,.ant-popover,.ant-dropdown,.ant-select-dropdown,.ant-picker-dropdown",
        )
      )
        return;
      event.preventDefault();
    };
    container.addEventListener("wheel", preventWheelScroll, { passive: false });
    return () => container.removeEventListener("wheel", preventWheelScroll);
  }, [containerRef]);

  const temporaryTool = isControlPressed || isSpacePressed;
  const activeTool = temporaryTool
    ? tool === "select"
      ? "pan"
      : "select"
    : tool;
  const cursor = isPanning
    ? "grabbing"
    : activeTool === "pan"
      ? "grab"
      : undefined;

  return (
    <div
      ref={containerRef}
      className="relative h-full w-full select-none overflow-hidden"
      style={{
        background: theme.canvas.background,
        cursor,
        touchAction: "none",
      }}
      onPointerDownCapture={handleTouchStart}
      onPointerDown={handlePointerDown}
      onDoubleClick={handleDoubleClick}
      onWheel={handleWheel}
      onContextMenu={onContextMenu}
      onDragOver={(event) => event.preventDefault()}
      onDrop={onDrop}
    >
      <CanvasGrid viewport={viewport} mode={backgroundMode} />
      <div
        className="absolute origin-top-left"
        style={{
          transform: `translate(${viewport.x}px, ${viewport.y}px) scale(${viewport.k})`,
        }}
      >
        {children}
      </div>
    </div>
  );
}

function CanvasGrid({
  viewport,
  mode,
}: {
  viewport: ViewportTransform;
  mode: CanvasBackgroundMode;
}) {
  const theme = canvasThemes[useThemeStore((state) => state.theme)];
  if (mode === "blank") return null;

  const gridSize = 48 * viewport.k;
  const x = viewport.x % gridSize;
  const y = viewport.y % gridSize;
  const dotSize = viewport.k < 0.12 ? 0.8 : 1.15;
  const backgroundImage =
    mode === "dots"
      ? `radial-gradient(circle, ${theme.canvas.dot} ${dotSize}px, transparent ${dotSize + 0.2}px)`
      : `linear-gradient(${theme.canvas.line} 1px, transparent 1px), linear-gradient(90deg, ${theme.canvas.line} 1px, transparent 1px)`;

  return (
    <div
      className="pointer-events-none absolute inset-0 opacity-40"
      style={{
        backgroundImage,
        backgroundSize: `${gridSize}px ${gridSize}px`,
        backgroundPosition: `${x}px ${y}px`,
      }}
    />
  );
}
