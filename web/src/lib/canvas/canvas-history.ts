import type { Graph } from "../../types/canvas";

export type CanvasHistory = { past: Graph[]; future: Graph[] };

// The project owns this object so editor remounts retain the same timeline.
export function createCanvasHistory(): CanvasHistory {
  return { past: [], future: [] };
}

export function checkpointCanvasHistory(
  history: CanvasHistory,
  current: Graph,
) {
  history.past = [...history.past.slice(-49), structuredClone(current)];
  history.future = [];
}

export function undoCanvasHistory(history: CanvasHistory, current: Graph) {
  const previous = history.past.pop();
  if (!previous) return;
  history.future.push(structuredClone(current));
  return previous;
}

export function redoCanvasHistory(history: CanvasHistory, current: Graph) {
  const next = history.future.pop();
  if (!next) return;
  history.past.push(structuredClone(current));
  return next;
}

export function resetCanvasHistory(history: CanvasHistory) {
  history.past = [];
  history.future = [];
}
