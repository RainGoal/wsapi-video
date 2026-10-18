import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import ts from "typescript";

const compiled = ts.transpileModule(
  fs.readFileSync(new URL("../src/creation.ts", import.meta.url), "utf8"),
  {
    compilerOptions: {
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2022,
    },
  },
).outputText;
const {
  addAcceptedTask,
  canvasReferenceSelection,
  generationReferenceDefaults,
  readGenerationInput,
  reconcileGeneratedNodes,
} = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
);
const node = (id, type, metadata = {}) => ({
  id,
  type,
  title: id,
  metadata,
  position: { x: 0, y: 0 },
  width: 280,
  height: 220,
});
const graph = () => ({
  nodes: [
    node("group", "group"),
    node("a", "image", { groupId: "group", assetId: "asset-a" }),
    node("note", "text", { groupId: "group", content: "Lighting note" }),
    node("config", "config"),
  ],
  connections: [{ id: "group-input", fromNodeId: "group", toNodeId: "config" }],
  viewport: { x: 80, y: 80, k: 1 },
});
const task = (id = "task-b", input = { reference_asset_ids: ["asset-b"] }) => ({
  id,
  project_id: "project",
  product_id: "image-product",
  kind: "image",
  prompt: "Accepted prompt",
  execution_status: "queued",
  delivery_status: "pending",
  asset_ids: [],
  input,
});
const config = (value) => value.nodes.find((item) => item.id === "config");
const compiledReferences = ts.transpileModule(
  fs.readFileSync(
    new URL("../src/lib/canvas/canvas-references.ts", import.meta.url),
    "utf8",
  ),
  {
    compilerOptions: {
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2022,
    },
  },
).outputText;
const { collectCanvasReferences } = await import(
  `data:text/javascript;base64,${Buffer.from(compiledReferences).toString("base64")}`
);

test("a newly delivered reference uses the live asset baseline when saving a different selection", () => {
  const before = graph();
  before.nodes[1] = node("a", "image", {
    groupId: "group",
    generationTaskId: "task-a",
  });
  const delivered = {
    ...task("task-a"),
    execution_status: "completed",
    delivery_status: "ready",
    asset_ids: ["asset-a"],
  };
  const live = reconcileGeneratedNodes(
    before,
    [delivered],
    [{ id: "asset-a", kind: "image" }],
  );
  assert.deepEqual(collectCanvasReferences(before, "config").assetIds, []);
  const next = addAcceptedTask(
    before,
    task(),
    "Image",
    "config",
    collectCanvasReferences(live, "config").assetIds,
  );
  const reopened = reconcileGeneratedNodes(
    next,
    [delivered],
    [{ id: "asset-a", kind: "image" }],
  );
  assert.deepEqual(
    canvasReferenceSelection(
      config(reopened),
      collectCanvasReferences(reopened, "config").assetIds,
    ),
    {
      referenceIds: ["asset-b"],
      startFrameId: "",
      endFrameId: "",
      restored: true,
    },
  );
});

test("accepted reference changes retain user group/note relations and restore the actual selection", () => {
  const before = graph(),
    encoded = JSON.stringify(before),
    accepted = task();
  const next = addAcceptedTask(before, accepted, "Image", "config", [
    "asset-a",
  ]);
  assert.equal(JSON.stringify(before), encoded);
  assert.deepEqual(next.connections[0], before.connections[0]);
  assert.deepEqual(next.nodes.slice(0, 3), before.nodes.slice(0, 3));
  assert.deepEqual(next.nodes.at(-1).metadata.generationInput, {
    reference_asset_ids: ["asset-b"],
    start_frame_asset_id: "",
    end_frame_asset_id: "",
  });
  assert.deepEqual(canvasReferenceSelection(config(next), ["asset-a"]), {
    referenceIds: ["asset-b"],
    startFrameId: "",
    endFrameId: "",
    restored: true,
  });
  assert.deepEqual(canvasReferenceSelection(config(next), ["asset-c"]), {
    referenceIds: ["asset-c"],
    startFrameId: "",
    endFrameId: "",
    restored: false,
  });
});

test("later generations and mutable config defaults cannot rewrite earlier result inputs", () => {
  const accepted = task();
  const first = addAcceptedTask(graph(), accepted, "Image", "config", [
    "asset-a",
  ]);
  accepted.input.reference_asset_ids.push("task-mutated");
  config(first).metadata.generationInput.reference_asset_ids.push(
    "config-mutated",
  );
  assert.deepEqual(
    first.nodes.at(-1).metadata.generationInput.reference_asset_ids,
    ["asset-b"],
  );
  const second = addAcceptedTask(
    first,
    task("task-c", { reference_asset_ids: ["asset-c"] }),
    "Image",
    "config",
    ["asset-a"],
  );
  assert.deepEqual(
    second.nodes.at(-2).metadata.generationInput.reference_asset_ids,
    ["asset-b"],
  );
  assert.deepEqual(
    second.nodes.at(-1).metadata.generationInput.reference_asset_ids,
    ["asset-c"],
  );
  assert.deepEqual(
    canvasReferenceSelection(config(second), ["asset-a"]).referenceIds,
    ["asset-c"],
  );
});

test("restored inputs preserve ordinary reference, first-frame and last-frame roles", () => {
  const accepted = {
    ...task("video", {
      reference_asset_ids: ["reference"],
      start_frame_asset_id: "first",
      end_frame_asset_id: "last",
    }),
    kind: "video",
  };
  const next = addAcceptedTask(graph(), accepted, "Video", "config", [
    "asset-a",
  ]);
  const restored = canvasReferenceSelection(config(next), [
    "asset-a",
    "asset-a",
  ]);
  const assets = ["reference", "first", "last"].map((id) => ({
    id,
    kind: "image",
  }));
  assert.deepEqual(
    generationReferenceDefaults(
      "video",
      true,
      [...restored.referenceIds, restored.startFrameId, restored.endFrameId],
      assets,
      restored.startFrameId,
      restored.endFrameId,
      true,
    ),
    {
      referenceIds: ["reference"],
      startFrameId: "first",
      endFrameId: "last",
    },
  );
  assert.deepEqual(
    generationReferenceDefaults(
      "video",
      false,
      [...restored.referenceIds, restored.startFrameId, restored.endFrameId],
      assets,
      restored.startFrameId,
      restored.endFrameId,
      true,
    ),
    {
      referenceIds: ["reference", "first", "last"],
      startFrameId: "",
      endFrameId: "",
    },
  );
  assert.deepEqual(
    generationReferenceDefaults(
      "image",
      true,
      [...restored.referenceIds, restored.startFrameId, restored.endFrameId],
      assets,
      restored.startFrameId,
      restored.endFrameId,
      true,
    ),
    {
      referenceIds: ["reference", "first", "last"],
      startFrameId: "",
      endFrameId: "",
    },
  );
});

test("an explicitly ordinary image stays ordinary when reopening a frame-capable model", () => {
  const accepted = { ...task(), kind: "video" };
  const next = addAcceptedTask(graph(), accepted, "Video", "config", [
    "asset-a",
  ]);
  const restored = canvasReferenceSelection(config(next), ["asset-a"]);
  assert.deepEqual(
    generationReferenceDefaults(
      "video",
      true,
      restored.referenceIds,
      [{ id: "asset-b", kind: "image" }],
      undefined,
      "",
      true,
    ),
    {
      referenceIds: ["asset-b"],
      startFrameId: "",
      endFrameId: "",
    },
  );
});

test("recovering an accepted task retains its inputs without inventing user connections", () => {
  const next = addAcceptedTask(graph(), task(), "Recovered");
  const recoveredConfig = next.nodes.find(
    (item) => item.id !== "config" && item.type === "config",
  );
  assert.deepEqual(canvasReferenceSelection(recoveredConfig, []), {
    referenceIds: ["asset-b"],
    startFrameId: "",
    endFrameId: "",
    restored: true,
  });
  assert.equal(
    next.connections.filter((edge) => edge.toNodeId === recoveredConfig.id)
      .length,
    0,
  );
  assert.deepEqual(
    next.nodes.at(-1).metadata.generationInput.reference_asset_ids,
    ["asset-b"],
  );
});

test("live task snapshots fill older results while invalid saved metadata cannot override current connections", () => {
  const original = graph();
  original.nodes.push(
    node("old-result", "image", { generationTaskId: "task-b" }),
  );
  const next = reconcileGeneratedNodes(original, [task()], []);
  assert.equal(original.nodes.at(-1).metadata.generationInput, undefined);
  assert.deepEqual(
    next.nodes.at(-1).metadata.generationInput.reference_asset_ids,
    ["asset-b"],
  );
  assert.equal(
    readGenerationInput({ reference_asset_ids: "bad legacy value" }),
    undefined,
  );
  assert.deepEqual(
    canvasReferenceSelection(
      node("bad", "config", {
        generationInput: { reference_asset_ids: "invalid" },
        generationInputSourceAssetIds: ["asset-a"],
      }),
      ["asset-a"],
    ),
    {
      referenceIds: ["asset-a"],
      startFrameId: "",
      endFrameId: "",
      restored: false,
    },
  );
});

test("an older task without input does not inherit the previous generation selection", () => {
  const first = addAcceptedTask(graph(), task(), "Image", "config", [
    "asset-a",
  ]);
  const legacy = { ...task("old-task"), input: undefined };
  const next = addAcceptedTask(first, legacy, "Legacy", "config", ["asset-a"]);
  assert.equal(next.nodes.at(-1).metadata.generationInput, undefined);
  assert.equal(config(next).metadata.generationInput, undefined);
  assert.deepEqual(canvasReferenceSelection(config(next), ["asset-a"]), {
    referenceIds: ["asset-a"],
    startFrameId: "",
    endFrameId: "",
    restored: false,
  });
  assert.deepEqual(
    next.nodes.at(-2).metadata.generationInput.reference_asset_ids,
    ["asset-b"],
  );
});
