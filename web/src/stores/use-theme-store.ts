import { useSyncExternalStore } from "react";

export type CanvasTheme = "dark" | "light";
type ThemeState = { theme: CanvasTheme; setTheme: typeof setTheme };
const preferenceKey = "frame:canvas-theme";
const listeners = new Set<() => void>();

function readTheme(): CanvasTheme | undefined {
  try {
    return localStorage.getItem(preferenceKey) === "light" ? "light" : "dark";
  } catch {
    return undefined;
  }
}

let state: ThemeState = { theme: readTheme() ?? "dark", setTheme };
function updateTheme(theme: CanvasTheme) {
  if (state.theme === theme) return;
  state = { ...state, theme };
  listeners.forEach((listener) => listener());
}
function setTheme(theme: CanvasTheme) {
  try {
    localStorage.setItem(preferenceKey, theme);
  } catch {
    /* Keep theme switching available when browser storage is unavailable. */
  }
  updateTheme(theme);
}
function syncPreference(event: StorageEvent) {
  if (event.key !== preferenceKey && event.key !== null) return;
  const next = readTheme();
  if (next) updateTheme(next);
}
function subscribe(listener: () => void) {
  if (!listeners.size) {
    window.addEventListener("storage", syncPreference);
    const next = readTheme();
    if (next) updateTheme(next);
  }
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
    if (!listeners.size) window.removeEventListener("storage", syncPreference);
  };
}
const getSnapshot = () => state;
const serverSnapshot: ThemeState = { theme: "dark", setTheme };

// Preserve the selector interface consumed by the adapted canvas components.
export function useThemeStore<T>(selector: (state: ThemeState) => T): T {
  return selector(
    useSyncExternalStore(subscribe, getSnapshot, () => serverSnapshot),
  );
}
