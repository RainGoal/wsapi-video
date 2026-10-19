import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type AriaRole,
  type KeyboardEventHandler,
  type ReactNode,
  type RefObject,
} from "react";
import { createPortal } from "react-dom";
import { useThemeStore } from "../../stores/use-theme-store";

// Popovers live outside the scrollable node composer and use screen coordinates.
export function CanvasPopover({
  anchor,
  width,
  className,
  label,
  role = "group",
  id,
  initialFocusSelector,
  onKeyDown,
  onClose,
  children,
}: {
  anchor: RefObject<HTMLElement | null>;
  width: number;
  className: string;
  label?: string;
  role?: AriaRole;
  id?: string;
  initialFocusSelector?: string;
  onKeyDown?: KeyboardEventHandler<HTMLDivElement>;
  onClose: () => void;
  children: ReactNode;
}) {
  const theme = useThemeStore((state) => state.theme);
  const popup = useRef<HTMLDivElement>(null);
  const previousFocus = useRef(document.activeElement as HTMLElement | null);
  const close = useRef(onClose);
  close.current = onClose;
  const [position, setPosition] = useState({
    left: 12,
    top: 12,
    width,
    maxHeight: 400,
  });
  function place() {
    const target = anchor.current?.getBoundingClientRect();
    if (!target || !popup.current) return;
    const availableWidth = Math.max(0, window.innerWidth - 24);
    const popupWidth = Math.min(width, availableWidth);
    const maxHeight = Math.max(80, window.innerHeight - 24);
    const popupHeight = Math.min(
      popup.current.getBoundingClientRect().height,
      maxHeight,
    );
    const below = window.innerHeight - target.bottom - 20;
    const above = target.top - 20;
    const desiredTop =
      below >= popupHeight || below >= above
        ? target.bottom + 8
        : target.top - popupHeight - 8;
    const next = {
      left: Math.max(
        12,
        Math.min(target.left, window.innerWidth - popupWidth - 12),
      ),
      top: Math.max(
        12,
        Math.min(desiredTop, window.innerHeight - popupHeight - 12),
      ),
      width: popupWidth,
      maxHeight,
    };
    setPosition((current) =>
      Object.keys(next).every(
        (key) =>
          current[key as keyof typeof next] === next[key as keyof typeof next],
      )
        ? current
        : next,
    );
  }
  useLayoutEffect(place);
  const placeRef = useRef(place);
  placeRef.current = place;
  useEffect(() => {
    const reposition = () => placeRef.current();
    const outside = (event: PointerEvent) => {
      const target = event.target as Node;
      if (!anchor.current?.contains(target) && !popup.current?.contains(target))
        close.current();
    };
    const observer = new ResizeObserver(reposition);
    if (popup.current) observer.observe(popup.current);
    window.addEventListener("resize", reposition);
    window.addEventListener("scroll", reposition, true);
    document.addEventListener("pointerdown", outside, true);
    popup.current
      ?.querySelector<HTMLElement>(
        initialFocusSelector ??
          "button:not(:disabled),input:not(:disabled),select:not(:disabled)",
      )
      ?.focus({ preventScroll: true });
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", reposition);
      window.removeEventListener("scroll", reposition, true);
      document.removeEventListener("pointerdown", outside, true);
    };
  }, [anchor, initialFocusSelector]);
  return createPortal(
    <div
      ref={popup}
      id={id}
      className={className}
      role={role}
      aria-label={label}
      data-canvas-no-zoom
      data-canvas-theme={theme}
      style={{
        ...position,
        position: "fixed",
        bottom: "auto",
        zIndex: 100,
        overflowY: "auto",
      }}
      onPointerDown={(event) => event.stopPropagation()}
      onMouseDown={(event) => event.stopPropagation()}
      onDoubleClick={(event) => event.stopPropagation()}
      onWheel={(event) => event.stopPropagation()}
      onKeyDown={(event) => {
        if (event.key !== "Tab") event.stopPropagation();
        onKeyDown?.(event);
        if (event.defaultPrevented) return;
        if (event.key === "Escape") {
          event.preventDefault();
          close.current();
          previousFocus.current?.focus({ preventScroll: true });
        }
      }}
    >
      {children}
    </div>,
    document.body,
  );
}
