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
  reconcileGeneratedNodes,
  canvasGenerationDefaults,
  canvasReferenceSelection,
} = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
);
const draft = (patch = {}) => ({
  kind: "image",
  productId: "product",
  prompt: "Later unsent draft",
  ratio: "1:1",
  duration: 0,
  resolution: "",
  size: "1024",
  quality: "standard",
  audio: false,
  referenceIds: ["old-asset"],
  startFrameId: "",
  endFrameId: "",
  connectedAssetIds: ["external", "old-asset"],
  ...patch,
});
const graph = () => ({
  nodes: [
    {
      id: "media",
      type: "image",
      title: "Custom title",
      position: { x: 24, y: 60 },
      width: 350,
      height: 260,
      metadata: {
        groupId: "group",
        assetId: "old-asset",
        prompt: "Old prompt",
        generationDraft: draft(),
        errorMessage: "Old error",
        error_message: "Legacy error",
        generationSourceNodeId: "legacy-source",
      },
    },
    { id: "ref", type: "image", metadata: { assetId: "external" } },
  ],
  connections: [{ id: "input", fromNodeId: "ref", toNodeId: "media" }],
  viewport: { x: 80, y: 80, k: 1 },
});
const task = (
  id = "task-a",
  createdAt = "2026-10-18T10:00:00.000000001Z",
  patch = {},
) => ({
  id,
  created_at: createdAt,
  project_id: "project",
  kind: "image",
  product_id: "accepted-product",
  prompt: "Accepted prompt",
  execution_status: "queued",
  delivery_status: "pending",
  procurement_status: "accepted",
  asset_ids: [],
  input: { reference_asset_ids: ["old-asset"] },
  ...patch,
});

test("media generation replaces its node contents while preserving identity and graph relations", () => {
  const before = graph(),
    encoded = JSON.stringify(before),
    accepted = task();
  const after = addAcceptedTask(
    before,
    accepted,
    "Ignored generated title",
    "media",
    ["external"],
  );
  assert.equal(JSON.stringify(before), encoded);
  assert.equal(after.nodes.length, before.nodes.length);
  assert.equal(after.connections, before.connections);
  assert.equal(after.viewport, before.viewport);
  const current = after.nodes[0];
  for (const key of ["id", "title", "position", "width", "height"])
    assert.deepEqual(current[key], before.nodes[0][key]);
  assert.equal(current.metadata.groupId, "group");
  assert.equal(current.metadata.generationSourceNodeId, "legacy-source");
  assert.equal(current.metadata.generationTaskId, accepted.id);
  assert.equal(current.metadata.assetId, undefined);
  assert.equal(current.metadata.errorMessage, "");
  assert.equal(current.metadata.error_message, undefined);
  assert.equal(current.metadata.generationDraft.prompt, "Later unsent draft");
  assert.deepEqual(current.metadata.generationInputSourceAssetIds, [
    "external",
  ]);
});

test("replayed receipts and older nanosecond timestamps cannot replace a newer task", () => {
  const newer = task("task-b", "2026-10-18T10:00:00.000000002Z");
  const current = addAcceptedTask(graph(), newer, "Image", "media");
  assert.equal(addAcceptedTask(current, newer, "Image", "media"), current);
  assert.equal(addAcceptedTask(current, task(), "Image", "media"), current);
  const sameTime = task("task-c", newer.created_at);
  const next = addAcceptedTask(current, sameTime, "Image", "media");
  assert.equal(next.nodes[0].metadata.generationTaskId, "task-c");
  assert.deepEqual(next.nodes[0].metadata.generationPreviousTaskIds, [
    "task-b",
  ]);
  assert.equal(addAcceptedTask(next, newer, "Image", "media"), next);
  const final = addAcceptedTask(
    next,
    task("task-d", newer.created_at),
    "Image",
    "media",
  );
  assert.equal(addAcceptedTask(final, newer, "Image", "media"), final);
});

test("a deleted source is never restored by an accepted task or its recovery", () => {
  const removed = { ...graph(), nodes: [], connections: [] };
  assert.equal(addAcceptedTask(removed, task(), "Image", "media"), removed);
  assert.equal(addAcceptedTask(removed, task(), "Image", ""), removed);
  assert.equal(addAcceptedTask(removed, task(), "Image").nodes.length, 2);
});

test("polling follows only the current task and exposes its asset only when archived and available", () => {
  const old = task("task-old", "2026-10-17T10:00:00Z", {
    execution_status: "completed",
    delivery_status: "ready",
    asset_ids: ["old-delivered"],
  });
  const accepted = task();
  const pending = addAcceptedTask(graph(), accepted, "Image", "media");
  const oldAssets = [{ id: "old-delivered", kind: "image" }];
  const waiting = reconcileGeneratedNodes(pending, [old, accepted], oldAssets);
  assert.equal(waiting.nodes[0].metadata.assetId, undefined);
  assert.equal(waiting.nodes[0].metadata.executionStatus, "queued");
  const done = {
    ...accepted,
    execution_status: "completed",
    delivery_status: "ready",
    asset_ids: ["new-delivered"],
  };
  const missing = reconcileGeneratedNodes(waiting, [old, done], oldAssets);
  assert.equal(missing.nodes[0].metadata.assetId, undefined);
  const ready = reconcileGeneratedNodes(
    missing,
    [old, done],
    [...oldAssets, { id: "new-delivered", kind: "image" }],
  );
  assert.equal(ready.nodes[0].metadata.assetId, "new-delivered");
  assert.equal(
    reconcileGeneratedNodes(
      ready,
      [old, done],
      [...oldAssets, { id: "new-delivered", kind: "image" }],
    ),
    ready,
  );
  assert.equal(oldAssets.length, 1);
  assert.equal(old.asset_ids[0], "old-delivered");
  const stale = { ...accepted, asset_ids: ["old-delivered"] };
  assert.equal(
    reconcileGeneratedNodes(ready, [stale], oldAssets).nodes[0].metadata
      .assetId,
    undefined,
  );
});

test("an imported self image stays explicitly selected through pending and ready without injecting the new output", () => {
  const before = graph();
  assert.deepEqual(
    canvasGenerationDefaults(before.nodes[0], undefined, ["external"])
      .referenceIds,
    ["external", "old-asset"],
  );
  const accepted = task();
  const pending = addAcceptedTask(before, accepted, "Image", "media", [
    "external",
  ]);
  assert.deepEqual(
    pending.nodes[0].metadata.generationDraft.connectedAssetIds,
    ["external"],
  );
  assert.deepEqual(
    canvasReferenceSelection(pending.nodes[0], ["external"]).referenceIds,
    ["old-asset"],
  );
  const done = {
    ...accepted,
    execution_status: "completed",
    delivery_status: "ready",
    asset_ids: ["new-asset"],
  };
  const ready = reconcileGeneratedNodes(
    pending,
    [done],
    [{ id: "new-asset", kind: "image" }],
  );
  assert.deepEqual(
    canvasGenerationDefaults(ready.nodes[0], undefined, ["external"])
      .referenceIds,
    ["external"],
  );
  assert.deepEqual(
    canvasReferenceSelection(ready.nodes[0], ["external"]).referenceIds,
    ["old-asset"],
  );
  const framed = graph();
  framed.nodes[0].metadata.generationDraft = draft({
    kind: "video",
    referenceIds: [],
    startFrameId: "old-asset",
  });
  const videoPending = addAcceptedTask(
    framed,
    task("video", undefined, { kind: "video" }),
    "Video",
    "media",
    ["external"],
  );
  assert.equal(
    canvasReferenceSelection(videoPending.nodes[0], ["external"]).startFrameId,
    "old-asset",
  );
});

test("an own asset also supplied by an external connection keeps its explicit removal baseline", () => {
  const before = graph();
  before.nodes[0].metadata.generationDraft = draft({
    referenceIds: [],
    connectedAssetIds: ["old-asset"],
  });
  const pending = addAcceptedTask(before, task(), "Image", "media", [
    "old-asset",
  ]);
  assert.deepEqual(
    pending.nodes[0].metadata.generationDraft.connectedAssetIds,
    ["old-asset"],
  );
  assert.deepEqual(
    canvasReferenceSelection(pending.nodes[0], ["old-asset"]).referenceIds,
    [],
  );
});

test("cross-kind replacement updates the card and matching draft mode while same-kind receipts preserve later edits", () => {
  const before = graph();
  const video = addAcceptedTask(
    before,
    task("video", undefined, { kind: "video" }),
    "Video",
    "media",
  );
  assert.equal(video.nodes[0].type, "video");
  assert.equal(video.nodes[0].metadata.generationDraft.kind, "video");
  assert.equal(
    video.nodes[0].metadata.generationDraft.productId,
    "accepted-product",
  );
  assert.equal(
    video.nodes[0].metadata.generationDraft.prompt,
    "Later unsent draft",
  );
  const edited = graph();
  edited.nodes[0].metadata.generationDraft = draft({
    kind: "video",
    productId: "later-model",
  });
  const image = addAcceptedTask(edited, task(), "Image", "media");
  assert.equal(image.nodes[0].metadata.generationDraft.kind, "video");
  assert.equal(
    image.nodes[0].metadata.generationDraft.productId,
    "later-model",
  );
});
