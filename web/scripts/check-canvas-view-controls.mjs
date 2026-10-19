import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import ts from "typescript";

const compiled = ts.transpileModule(
  fs.readFileSync(
    new URL("../src/lib/canvas/canvas-view-geometry.ts", import.meta.url),
    "utf8",
  ),
  {
    compilerOptions: {
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2022,
    },
  },
).outputText;
const {
  MINI_MAP_SIZE,
  miniMapFrame,
  pointInMiniMap,
  viewportAtMiniMapPoint,
  viewportInMiniMap,
  zoomViewportAtCenter,
} = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
);
const node = (x, y, width = 300, height = 200) => ({
  id: "node",
  type: "image",
  title: "Image",
  position: { x, y },
  width,
  height,
});
const close = (actual, expected) =>
  assert.ok(
    Math.abs(actual - expected) < 1e-8,
    `Expected ${actual} to be close to ${expected}`,
  );

test("slider zoom preserves the world point at the canvas center and clamps limits", () => {
  const size = { width: 1200, height: 800 };
  const viewport = { x: -240, y: 120, k: 0.7 };
  const next = zoomViewportAtCenter(viewport, size, 2.1);
  close((600 - next.x) / next.k, (600 - viewport.x) / viewport.k);
  close((400 - next.y) / next.k, (400 - viewport.y) / viewport.k);
  assert.equal(zoomViewportAtCenter(viewport, size, 0).k, 0.05);
  assert.equal(zoomViewportAtCenter(viewport, size, 90).k, 5);
});

test("mini map includes negative-position nodes and the current viewport", () => {
  const viewport = { x: -800, y: 300, k: 0.5 };
  const size = { width: 1000, height: 700 };
  const nodes = [node(-600, -900), node(5000, 3000)];
  const frame = miniMapFrame(nodes, viewport, size);
  for (const point of [
    { x: -600, y: -900 },
    { x: 5300, y: 3200 },
    { x: 1600, y: -600 },
    { x: 3600, y: 800 },
  ]) {
    const projected = pointInMiniMap(point, frame);
    assert.ok(
      projected.x >= 12 - 1e-8 &&
        projected.x <= MINI_MAP_SIZE.width - 12 + 1e-8,
    );
    assert.ok(
      projected.y >= 12 - 1e-8 &&
        projected.y <= MINI_MAP_SIZE.height - 12 + 1e-8,
    );
  }
});

test("mini map navigation centers a clicked world point without changing zoom", () => {
  const viewport = { x: 300, y: -100, k: 1.4 };
  const size = { width: 1100, height: 750 };
  const frame = miniMapFrame([node(-500, -700)], viewport, size);
  const point = { x: -340, y: -620 };
  const next = viewportAtMiniMapPoint(
    pointInMiniMap(point, frame),
    frame,
    viewport,
    size,
  );
  close((size.width / 2 - next.x) / next.k, point.x);
  close((size.height / 2 - next.y) / next.k, point.y);
  assert.equal(next.k, viewport.k);
});

test("dragging against a fixed mini map frame stays stable as viewport moves", () => {
  const size = { width: 1000, height: 700 };
  const viewport = { x: 0, y: 0, k: 1 };
  const frame = miniMapFrame([node(3000, -500)], viewport, size);
  const target = pointInMiniMap({ x: 2800, y: -200 }, frame);
  const first = viewportAtMiniMapPoint(target, frame, viewport, size);
  const second = viewportAtMiniMapPoint(target, frame, first, size);
  assert.deepEqual(second, first);
  const visible = viewportInMiniMap(second, size, frame);
  close(visible.x + visible.width / 2, target.x);
  close(visible.y + visible.height / 2, target.y);
});

test("empty canvases and malformed coordinates retain finite usable mini maps", () => {
  for (const nodes of [[], [node(NaN, Infinity)], [node(-1e300, 1e300)]]) {
    const viewport = { x: 0, y: 0, k: 0.05 };
    const size = { width: 0, height: 0 };
    const frame = miniMapFrame(nodes, viewport, size);
    assert.ok(frame.scale > 0);
    assert.ok(Object.values(frame).every(Number.isFinite));
    assert.ok(
      Object.values(
        viewportAtMiniMapPoint({ x: 120, y: 72 }, frame, viewport, size),
      ).every(Number.isFinite),
    );
  }
});

test("a distant viewport and large graph retain all node bounds without mutation", () => {
  const nodes = Array.from({ length: 10000 }, (_, index) =>
    node(index * 500, -index * 300),
  );
  const original = structuredClone(nodes);
  const viewport = { x: -1e8, y: 1e8, k: 5 };
  const size = { width: 1600, height: 900 };
  const frame = miniMapFrame(nodes, viewport, size);
  assert.ok(frame.scale > 0 && Number.isFinite(frame.scale));
  assert.deepEqual(nodes, original);
  const visible = viewportInMiniMap(viewport, size, frame);
  assert.ok(Object.values(visible).every(Number.isFinite));
  const target = pointInMiniMap(nodes[9999].position, frame);
  assert.ok(target.x >= 0 && target.x <= MINI_MAP_SIZE.width);
  assert.ok(target.y >= 0 && target.y <= MINI_MAP_SIZE.height);
});
