import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import ts from "typescript";

const compiled = ts.transpileModule(
  fs.readFileSync(
    new URL("../src/lib/canvas/canvas-overlay-geometry.ts", import.meta.url),
    "utf8",
  ),
  {
    compilerOptions: {
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2022,
    },
  },
).outputText;
const { placeCanvasComposer } = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
);
const node = (x, y, width = 300, height = 200) => ({
  id: "image",
  type: "image",
  title: "Image",
  position: { x, y },
  width,
  height,
});
const viewport = { x: 0, y: 0, k: 1 };

test("composer prefers a full-height panel below the current node", () => {
  const result = placeCanvasComposer(
    node(400, 100, 300, 180),
    viewport,
    { width: 1500, height: 1000 },
    520,
  );
  assert.deepEqual(result, { x: 290, y: 296, maxHeight: 420 });
});

test("composer uses the full space above a low node before considering sides", () => {
  const result = placeCanvasComposer(
    node(600, 650),
    viewport,
    { width: 1500, height: 900 },
    520,
  );
  assert.deepEqual(result, { x: 490, y: 214, maxHeight: 420 });
});

test("composer uses the right or left side when vertical space cannot fit 420px", () => {
  const size = { width: 1600, height: 900 };
  const right = placeCanvasComposer(
    node(300, 200, 260, 360),
    viewport,
    size,
    520,
  );
  assert.equal(right.x, 576);
  assert.equal(right.maxHeight, 420);
  assert.ok(right.y >= 12 && right.y + right.maxHeight <= size.height - 92);
  const left = placeCanvasComposer(
    node(900, 200, 300, 360),
    viewport,
    size,
    520,
  );
  assert.equal(left.x + 520, 884);
  assert.equal(left.maxHeight, 420);
});

test("narrow canvases use a scrollable free strip without covering the image", () => {
  const size = { width: 700, height: 900 };
  const below = placeCanvasComposer(
    node(150, 180, 300, 300),
    viewport,
    size,
    520,
  );
  assert.deepEqual(below, { x: 40, y: 496, maxHeight: 312 });
  const above = placeCanvasComposer(
    node(150, 400, 300, 300),
    viewport,
    size,
    520,
  );
  assert.deepEqual(above, { x: 40, y: 12, maxHeight: 372 });
});

test("overlay fallback stays in the usable area when a node fills the canvas", () => {
  const result = placeCanvasComposer(
    node(0, 0, 700, 500),
    viewport,
    { width: 700, height: 500 },
    520,
  );
  assert.equal(result.y, 12);
  assert.equal(result.maxHeight, 396);
  assert.ok(result.x >= 12 && result.x + 520 <= 688);
});

test("narrow layouts reserve both stacked toolbars below the composer", () => {
  const result = placeCanvasComposer(
    node(230, 260, 80, 60),
    viewport,
    { width: 390, height: 681 },
    366,
    144,
  );
  assert.ok(result.y >= 12);
  assert.ok(result.y + result.maxHeight <= 681 - 144 - 12);
  assert.ok(result.y >= 336 || result.y + result.maxHeight <= 244);
});

test("off-screen nodes and zoomed coordinates keep the composer reachable", () => {
  const size = { width: 1200, height: 900 };
  for (const x of [-3000, 0, 3000]) {
    for (const y of [-3000, 0, 3000]) {
      const result = placeCanvasComposer(
        node(x, y),
        { x: -100, y: 150, k: 2 },
        size,
        520,
      );
      assert.ok(result.x >= 12 && result.x + 520 <= size.width - 12);
      assert.ok(
        result.y >= 12 && result.y + result.maxHeight <= size.height - 92,
      );
      assert.ok(result.maxHeight >= 0 && result.maxHeight <= 420);
    }
  }
});

test("extremely narrow, short, empty or invalid stages produce finite coordinates", () => {
  for (const size of [
    { width: 10, height: 20 },
    { width: 0, height: 0 },
    { width: 1, height: 1 },
    { width: NaN, height: Infinity },
  ]) {
    const result = placeCanvasComposer(
      node(Infinity, NaN, -30, 1e300),
      { x: Infinity, y: NaN, k: 0 },
      size,
      520,
    );
    assert.ok(Object.values(result).every(Number.isFinite));
    assert.ok(result.x >= 0 && result.y >= 0 && result.maxHeight >= 0);
  }
});
