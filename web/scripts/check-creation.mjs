import assert from "node:assert/strict";
import fs from "node:fs";
import { test } from "node:test";
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
  generationReferenceDefaults,
} = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
);
const graph = () => ({
  nodes: [
    {
      id: "source",
      type: "config",
      title: "Source",
      position: { x: 0, y: 0 },
      width: 280,
      height: 220,
      metadata: { prompt: "Original prompt" },
    },
  ],
  connections: [],
  viewport: { x: 80, y: 80, k: 1 },
});
const task = {
  id: "task-1",
  project_id: "p1",
  product_id: "product",
  kind: "image",
  prompt: "Accepted immutable prompt",
  execution_status: "queued",
  delivery_status: "pending",
  asset_ids: [],
};

test("accepted tasks retain source relationships and do not duplicate results", () => {
  const original = graph();
  const next = addAcceptedTask(original, task, "Result", "source");
  assert.equal(original.nodes.length, 1);
  assert.equal(next.nodes.length, 2);
  assert.equal(next.nodes[1].metadata.generationTaskId, task.id);
  assert.equal(next.nodes[0].metadata.generationKind, "image");
  assert.equal(next.nodes[0].metadata.prompt, task.prompt);
  assert.equal(original.nodes[0].metadata.prompt, "Original prompt");
  assert.equal(next.connections[0].fromNodeId, "source");
  assert.equal(addAcceptedTask(next, task, "Result", "source"), next);
  const second = addAcceptedTask(
    next,
    { ...task, id: "task-2" },
    "Result",
    "source",
  );
  assert.notDeepEqual(second.nodes[1].position, second.nodes[2].position);
});

test("results only become referenceable after the archived asset is present", () => {
  const initial = addAcceptedTask(graph(), task, "Result", "source");
  const completed = {
    ...task,
    execution_status: "completed",
    delivery_status: "ready",
    asset_ids: ["asset-1"],
  };
  const waiting = reconcileGeneratedNodes(initial, [completed], []);
  assert.equal(waiting.nodes[1].metadata.assetId, undefined);
  const ready = reconcileGeneratedNodes(
    waiting,
    [completed],
    [{ id: "asset-1", task_id: task.id, kind: "image" }],
  );
  assert.equal(ready.nodes[1].metadata.assetId, "asset-1");
  assert.equal(
    reconcileGeneratedNodes(
      ready,
      [completed],
      [{ id: "asset-1", task_id: task.id, kind: "image" }],
    ),
    ready,
  );
  assert.equal(ready.nodes[0].metadata.prompt, task.prompt);
});

test("recovering an old task retains historical provenance without a storyboard flow", () => {
  const recovered = addAcceptedTask(
    graph(),
    { ...task, input: { shot_id: "original-shot" } },
    "Result",
    undefined,
  );
  assert.equal(recovered.nodes.at(-1).metadata.shotId, "original-shot");
  const unassociated = addAcceptedTask(
    graph(),
    { ...task, input: {} },
    "Result",
    undefined,
  );
  assert.equal(unassociated.nodes.at(-1).metadata.shotId, undefined);
});

test("starting from an asset creates an editable concept between reference and output", () => {
  const initial = graph();
  initial.nodes[0] = {
    ...initial.nodes[0],
    type: "image",
    metadata: { assetId: "input-asset" },
  };
  const next = addAcceptedTask(initial, task, "Concept", "source");
  assert.deepEqual(
    next.nodes.map((node) => node.type),
    ["image", "config", "image"],
  );
  assert.equal(next.connections[0].fromNodeId, "source");
  assert.equal(next.connections[0].toNodeId, next.nodes[1].id);
  assert.equal(next.connections[1].fromNodeId, next.nodes[1].id);
  assert.equal(next.nodes[1].metadata.prompt, task.prompt);
  assert.equal(next.nodes[1].metadata.generationKind, "image");
});

test("canvas creation initializes mode, connected notes and unique asset references", () => {
  const node = {
    type: "config",
    metadata: { generationKind: "video", prompt: "Camera movement" },
  };
  const initial = canvasGenerationDefaults(
    node,
    undefined,
    ["a", "a"],
    [{ content: " Warm lighting " }, { content: "Warm lighting" }],
  );
  assert.deepEqual(initial, {
    kind: "video",
    prompt: "Camera movement\n\nWarm lighting",
    referenceIds: ["a"],
  });
  assert.equal(canvasGenerationDefaults(node, "image").kind, "image");
  const reopened = canvasGenerationDefaults(
    { ...node, metadata: { ...node.metadata, prompt: initial.prompt } },
    undefined,
    [],
    [{ content: "Warm lighting" }],
  );
  assert.equal(reopened.prompt, initial.prompt);
  assert.equal(
    canvasGenerationDefaults(
      { type: "config", metadata: { prompt: "An idea\n\nLine one\nLine two" } },
      undefined,
      [],
      [{ content: "Line one\nLine two" }],
    ).prompt,
    "An idea\n\nLine one\nLine two",
  );
  assert.equal(canvasGenerationDefaults({ type: "video" }).kind, "video");
  assert.equal(canvasGenerationDefaults().kind, "image");
  assert.equal(
    canvasGenerationDefaults({ type: "text", metadata: { content: "An idea" } })
      .prompt,
    "An idea",
  );
});

test("image to video uses one first frame or a reference according to model capability", () => {
  const assets = [
    { id: "image", kind: "image" },
    { id: "video", kind: "video" },
  ];
  const initial = canvasGenerationDefaults(
    { type: "image", metadata: { assetId: "image" } },
    "video",
    ["image", "video"],
  );
  assert.deepEqual(initial.referenceIds, ["image", "video"]);
  assert.deepEqual(
    generationReferenceDefaults(
      "video",
      true,
      initial.referenceIds,
      assets,
      "image",
    ),
    {
      startFrameId: "image",
      endFrameId: "",
      referenceIds: ["video"],
    },
  );
  for (const [kind, frames] of [
    ["image", true],
    ["video", false],
  ]) {
    assert.deepEqual(
      generationReferenceDefaults(
        kind,
        frames,
        initial.referenceIds,
        assets,
        "image",
      ),
      {
        startFrameId: "",
        endFrameId: "",
        referenceIds: ["image", "video"],
      },
    );
  }
});

test("connected images select a sole first frame without guessing between multiple images", () => {
  const assets = [
    { id: "a", kind: "image" },
    { id: "b", kind: "image" },
  ];
  assert.deepEqual(
    generationReferenceDefaults("video", true, ["a", "a"], assets),
    {
      startFrameId: "a",
      endFrameId: "",
      referenceIds: [],
    },
  );
  assert.deepEqual(
    generationReferenceDefaults("video", true, ["a", "b"], assets),
    {
      startFrameId: "",
      endFrameId: "",
      referenceIds: ["a", "b"],
    },
  );
  assert.deepEqual(
    generationReferenceDefaults("video", true, ["missing"], assets),
    {
      startFrameId: "",
      endFrameId: "",
      referenceIds: ["missing"],
    },
  );
});

test("model changes preserve both frame roles or restore both as ordinary references", () => {
  const assets = [
    { id: "a", kind: "image" },
    { id: "b", kind: "image" },
  ];
  assert.deepEqual(
    generationReferenceDefaults("video", true, ["a", "b"], assets, "a", "b"),
    {
      startFrameId: "a",
      endFrameId: "b",
      referenceIds: [],
    },
  );
  assert.deepEqual(
    generationReferenceDefaults("video", false, ["a", "b"], assets, "a", "b"),
    {
      startFrameId: "",
      endFrameId: "",
      referenceIds: ["a", "b"],
    },
  );
});

test("continuing creation retains immutable previous outputs and the new media mode", () => {
  const first = addAcceptedTask(graph(), task, "Image", "source");
  const next = addAcceptedTask(
    first,
    { ...task, id: "video-task", kind: "video", prompt: "Animate" },
    "Video",
    "source",
  );
  assert.equal(first.nodes[0].metadata.generationKind, "image");
  assert.equal(next.nodes[0].metadata.generationKind, "video");
  assert.equal(next.nodes[1].metadata.prompt, task.prompt);
  assert.equal(next.nodes[2].metadata.prompt, "Animate");
  assert.equal(next.nodes[2].type, "video");
  assert.equal(next.connections.length, 2);
});
