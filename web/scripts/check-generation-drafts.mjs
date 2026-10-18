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
  readGenerationDraft,
  normalizeGenerationDraft,
  canvasReferenceSelection,
  canvasGenerationDefaults,
  addAcceptedTask,
} = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
);
const draft = (patch = {}) => ({
  kind: "video",
  productId: "video-model",
  prompt: "Draft prompt",
  ratio: "16:9",
  duration: 10,
  resolution: "1080p",
  size: "",
  quality: "",
  audio: true,
  referenceIds: ["reference", "manual"],
  startFrameId: "first",
  endFrameId: "last",
  connectedAssetIds: ["reference", "first", "last", "removed"],
  ...patch,
});
const node = (patch = {}) => ({
  id: "source",
  type: "video",
  title: "Source",
  position: { x: 0, y: 0 },
  width: 300,
  height: 240,
  metadata: { generationDraft: draft() },
  ...patch,
});
const product = (patch = {}) => ({
  id: "next-model",
  kind: "video",
  capabilities: {
    aspect_ratios: ["1:1", "16:9"],
    durations: [5, 10],
    resolutions: ["720p", "1080p"],
    sizes: ["1024"],
    qualities: ["standard"],
    supports_frames: true,
    supports_audio: true,
    ...patch,
  },
});

test("draft reads reject malformed metadata and isolate reference arrays", () => {
  for (const value of [
    null,
    [],
    "bad",
    {},
    draft({ kind: "audio" }),
    draft({ audio: "false" }),
    draft({ duration: Infinity }),
    draft({ duration: -1 }),
    draft({ prompt: {} }),
    draft({ referenceIds: [1] }),
    draft({ connectedAssetIds: [""] }),
  ])
    assert.equal(readGenerationDraft(value), undefined);
  const source = draft({ referenceIds: ["a", "a"] });
  const read = readGenerationDraft(source);
  assert.deepEqual(read.referenceIds, ["a"]);
  read.referenceIds.push("new");
  read.connectedAssetIds.push("new");
  assert.deepEqual(source.referenceIds, ["a", "a"]);
  assert.equal(source.connectedAssetIds.includes("new"), false);
  assert.equal(readGenerationDraft({ kind: "image" }).prompt, "");
});

test("reopening a draft merges new connections and preserves explicit roles and removals", () => {
  const source = node();
  const selected = canvasReferenceSelection(source, [
    "reference",
    "first",
    "last",
    "removed",
    "new",
  ]);
  assert.deepEqual(selected, {
    referenceIds: ["reference", "manual", "new"],
    startFrameId: "first",
    endFrameId: "last",
    restored: true,
  });
  assert.equal(selected.referenceIds.includes("removed"), false);
  assert.deepEqual(canvasReferenceSelection(source, ["last", "new"]), {
    referenceIds: ["manual", "new"],
    startFrameId: "",
    endFrameId: "last",
    restored: true,
  });
  assert.deepEqual(source.metadata.generationDraft.referenceIds, [
    "reference",
    "manual",
  ]);
  assert.deepEqual(canvasReferenceSelection(node({ type: "image" }), []), {
    referenceIds: ["manual"],
    startFrameId: "",
    endFrameId: "",
    restored: true,
  });
});

test("draft prompt and mode restore without repeating connected notes or immutable prompts", () => {
  const source = node({
    metadata: {
      prompt: "Original result prompt",
      generationKind: "image",
      generationDraft: draft({ prompt: "Draft prompt\n\nLighting note" }),
    },
  });
  const reopened = canvasGenerationDefaults(
    source,
    undefined,
    [],
    [{ content: "Lighting note" }],
  );
  assert.equal(reopened.kind, "video");
  assert.equal(reopened.prompt, "Draft prompt\n\nLighting note");
  assert.equal(canvasGenerationDefaults(source, "image").kind, "image");
  for (const edited of [
    "",
    "Draft prompt",
    "Changed lighting",
    "  Preserve spaces  ",
  ]) {
    source.metadata.generationDraft.prompt = edited;
    assert.equal(
      canvasGenerationDefaults(
        source,
        undefined,
        [],
        [{ content: "Lighting note" }, { content: "New note" }],
      ).prompt,
      edited,
    );
  }
});

test("an explicit reference to a result's provenance source remains a manual selection", () => {
  const result = node({
    metadata: {
      generationTaskId: "task-b",
      generationSourceNodeId: "node-a",
      assetId: "asset-b",
      generationDraft: draft({
        referenceIds: ["asset-a"],
        startFrameId: "",
        endFrameId: "",
        connectedAssetIds: ["asset-b"],
      }),
    },
  });
  assert.deepEqual(canvasReferenceSelection(result, ["asset-b"]), {
    referenceIds: ["asset-a"],
    startFrameId: "",
    endFrameId: "",
    restored: true,
  });
});

test("model normalization retains supported settings and translates unsupported frame roles", () => {
  const original = draft();
  const same = normalizeGenerationDraft(original, product());
  assert.equal(same.productId, "next-model");
  assert.equal(same.ratio, "16:9");
  assert.equal(same.duration, 10);
  assert.equal(same.resolution, "1080p");
  assert.equal(same.audio, true);
  assert.equal(same.startFrameId, "first");
  const changed = normalizeGenerationDraft(
    original,
    product({
      aspect_ratios: ["1:1"],
      durations: [5],
      resolutions: ["720p"],
      supports_frames: false,
      supports_audio: false,
    }),
  );
  assert.equal(changed.ratio, "1:1");
  assert.equal(changed.duration, 5);
  assert.equal(changed.resolution, "720p");
  assert.equal(changed.audio, false);
  assert.deepEqual(changed.referenceIds, [
    "reference",
    "manual",
    "first",
    "last",
  ]);
  assert.equal(changed.startFrameId, "");
  assert.equal(changed.endFrameId, "");
  assert.equal(original.startFrameId, "first");
});

test("accepted tasks reuse media sources without replacing their content or later draft", () => {
  const source = node({
    metadata: {
      prompt: "Original media prompt",
      assetId: "original-asset",
      generationDraft: draft({ prompt: "Newer unsent prompt" }),
    },
  });
  const before = {
    nodes: [source],
    connections: [],
    viewport: { x: 80, y: 80, k: 1 },
  };
  const task = {
    id: "task",
    project_id: "project",
    kind: "video",
    product_id: "accepted-model",
    prompt: "Accepted prompt",
    execution_status: "queued",
    delivery_status: "pending",
    input: {
      reference_asset_ids: ["accepted-reference"],
      start_frame_asset_id: "first",
    },
    asset_ids: [],
  };
  const after = addAcceptedTask(before, task, "Result", "source", [
    "connected-at-submit",
  ]);
  assert.equal(after.nodes.length, 2);
  assert.equal(after.connections.length, 1);
  assert.equal(after.nodes[0].metadata.prompt, "Original media prompt");
  assert.equal(after.nodes[0].metadata.assetId, "original-asset");
  assert.deepEqual(
    after.nodes[0].metadata.generationDraft,
    source.metadata.generationDraft,
  );
  assert.equal(after.nodes[1].metadata.generationSourceNodeId, "source");
  assert.equal(after.nodes[1].metadata.prompt, "Accepted prompt");
  assert.deepEqual(after.nodes[0].metadata.generationInputSourceAssetIds, [
    "connected-at-submit",
  ]);
  assert.deepEqual(
    after.nodes[1].metadata.generationInput.reference_asset_ids,
    ["accepted-reference"],
  );
  after.nodes[0].metadata.generationInput.reference_asset_ids.push(
    "source-edited",
  );
  assert.deepEqual(
    after.nodes[1].metadata.generationInput.reference_asset_ids,
    ["accepted-reference"],
  );
  assert.equal(addAcceptedTask(after, task, "Result", "source"), after);
  assert.equal(source.metadata.generationInput, undefined);
});

test("legacy media sources restore accepted roles from the submission connection baseline", () => {
  const source = node({ metadata: { assetId: "original-asset" } });
  const before = {
    nodes: [source],
    connections: [],
    viewport: { x: 0, y: 0, k: 1 },
  };
  const after = addAcceptedTask(
    before,
    {
      id: "accepted",
      kind: "video",
      prompt: "Accepted",
      product_id: "product",
      input: {
        reference_asset_ids: ["manual"],
        end_frame_asset_id: "original-asset",
      },
    },
    "Video",
    "source",
    ["original-asset"],
  );
  assert.deepEqual(
    canvasReferenceSelection(after.nodes[0], ["original-asset"]),
    {
      referenceIds: ["manual"],
      startFrameId: "",
      endFrameId: "original-asset",
      restored: true,
    },
  );
});

test("continuing from a generated result preserves the original task provenance", () => {
  const source = node({
    metadata: {
      assetId: "original-asset",
      prompt: "Original prompt",
      productId: "original-model",
      generationTaskId: "original-task",
      generationKind: "video",
      generationInput: { reference_asset_ids: ["original-reference"] },
      generationDraft: draft({ prompt: "New draft" }),
    },
  });
  const before = {
    nodes: [source],
    connections: [],
    viewport: { x: 0, y: 0, k: 1 },
  };
  const after = addAcceptedTask(
    before,
    {
      id: "new-task",
      kind: "image",
      prompt: "New prompt",
      product_id: "new-model",
      input: { reference_asset_ids: ["new-reference"] },
    },
    "Next result",
    "source",
    ["original-asset"],
  );
  assert.deepEqual(after.nodes[0], source);
  assert.equal(after.nodes[1].metadata.prompt, "New prompt");
  assert.deepEqual(
    after.nodes[1].metadata.generationInput.reference_asset_ids,
    ["new-reference"],
  );
});
