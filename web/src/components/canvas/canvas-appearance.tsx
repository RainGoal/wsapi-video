// Appearance options follow infinite-canvas's toolbar (MIT), commit 3a6b64b.
import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { t } from "../../i18n";
import type { CanvasBackgroundMode } from "../../lib/canvas-theme";
import { useThemeStore } from "../../stores/use-theme-store";
import "./canvas-appearance.css";

export function CanvasAppearance({
  backgroundMode,
  showImageInfo,
  onChange,
}: {
  backgroundMode: CanvasBackgroundMode;
  showImageInfo: boolean;
  onChange: (patch: {
    backgroundMode?: CanvasBackgroundMode;
    showImageInfo?: boolean;
  }) => void;
}) {
  const { theme, setTheme } = useThemeStore((state) => state);
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const panelId = useId();
  useEffect(() => {
    if (!open) return;
    const outside = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      event.stopPropagation();
      setOpen(false);
      trigger.current?.focus({ preventScroll: true });
    };
    root.current
      ?.querySelector<HTMLButtonElement>(".canvas-appearance-popover button")
      ?.focus({ preventScroll: true });
    document.addEventListener("pointerdown", outside, true);
    document.addEventListener("keydown", escape, true);
    return () => {
      document.removeEventListener("pointerdown", outside, true);
      document.removeEventListener("keydown", escape, true);
    };
  }, [open]);
  return (
    <div
      className="canvas-appearance-control"
      ref={root}
      data-canvas-no-zoom
      onPointerDown={(event) => event.stopPropagation()}
      onDoubleClick={(event) => event.stopPropagation()}
      onWheel={(event) => event.stopPropagation()}
    >
      <button
        ref={trigger}
        type="button"
        className={`icon-button ${open ? "selected" : ""}`}
        title={t("画布外观")}
        aria-label={t("画布外观")}
        aria-expanded={open}
        aria-controls={open ? panelId : undefined}
        aria-haspopup="dialog"
        onClick={() => setOpen((value) => !value)}
      >
        <AppearanceIcon kind="palette" />
      </button>
      {open && (
        <section
          id={panelId}
          role="dialog"
          aria-label={t("画布外观")}
          className="canvas-appearance-popover"
          data-canvas-theme={theme}
        >
          <h3>{t("画布外观")}</h3>
          <fieldset>
            <legend>{t("主题")}</legend>
            <div className="canvas-appearance-segments">
              {(["light", "dark"] as const).map((value) => (
                <button
                  key={value}
                  type="button"
                  aria-pressed={theme === value}
                  onClick={() => setTheme(value)}
                >
                  <AppearanceIcon kind={value} />
                  {t(value === "light" ? "浅色" : "深色")}
                </button>
              ))}
            </div>
          </fieldset>
          <fieldset>
            <legend>{t("背景样式")}</legend>
            <div className="canvas-appearance-segments">
              {(["dots", "lines", "blank"] as const).map((value) => (
                <button
                  key={value}
                  type="button"
                  aria-pressed={backgroundMode === value}
                  onClick={() => onChange({ backgroundMode: value })}
                >
                  <AppearanceIcon kind={value} />
                  {t(
                    value === "dots"
                      ? "点阵"
                      : value === "lines"
                        ? "线格"
                        : "空白",
                  )}
                </button>
              ))}
            </div>
          </fieldset>
          <label className="canvas-appearance-switch">
            <span>{t("显示图片信息")}</span>
            <input
              type="checkbox"
              role="switch"
              checked={showImageInfo}
              onChange={(event) =>
                onChange({ showImageInfo: event.target.checked })
              }
            />
            <span
              className="canvas-appearance-switch-track"
              aria-hidden="true"
            />
          </label>
        </section>
      )}
    </div>
  );
}

function AppearanceIcon({
  kind,
}: {
  kind: "palette" | "light" | "dark" | CanvasBackgroundMode;
}) {
  const paths: Record<typeof kind, ReactNode> = {
    palette: (
      <>
        <path d="M12 3a9 9 0 1 0 0 18h1a2 2 0 0 0 1-3.7 1.8 1.8 0 0 1 1-3.3h2a4 4 0 0 0 4-4c0-4-4-7-9-7Z" />
        <circle cx="7" cy="10" r=".7" />
        <circle cx="11" cy="7" r=".7" />
        <circle cx="16" cy="8" r=".7" />
        <circle cx="7" cy="15" r=".7" />
      </>
    ),
    light: (
      <>
        <circle cx="12" cy="12" r="4" />
        <path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5" />
      </>
    ),
    dark: <path d="M20 14a8 8 0 0 1-10-10 8.5 8.5 0 1 0 10 10Z" />,
    dots: (
      <>
        {[6, 12, 18].flatMap((x) =>
          [6, 12, 18].map((y) => (
            <circle
              key={`${x}-${y}`}
              cx={x}
              cy={y}
              r=".9"
              fill="currentColor"
              stroke="none"
            />
          )),
        )}
      </>
    ),
    lines: (
      <>
        <rect x="4" y="4" width="16" height="16" rx="2" />
        <path d="M4 10h16M4 15h16M10 4v16m5-16v16" />
      </>
    ),
    blank: <rect x="4" y="4" width="16" height="16" rx="2" />,
  };
  return (
    <svg
      width="17"
      height="17"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {paths[kind]}
    </svg>
  );
}
