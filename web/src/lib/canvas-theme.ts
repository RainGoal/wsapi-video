export type CanvasBackgroundMode = "lines" | "dots" | "blank";
export const canvasThemes = {
  dark: {
    canvas: { background: "#151718", dot: "#65625b", line: "#444541" },
    node: { activeStroke: "#D7AB6A", muted: "#74716a" },
  },
  light: {
    canvas: { background: "#f4f3ef", dot: "#a7a49b", line: "#d3d0c7" },
    node: { activeStroke: "#a57231", muted: "#969084" },
  },
};
