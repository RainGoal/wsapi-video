import assert from "node:assert/strict";
import fs from "node:fs";
import { test } from "node:test";
import ts from "typescript";

async function load(name) {
  const source = fs.readFileSync(
    new URL(`../src/lib/canvas/${name}.ts`, import.meta.url),
    "utf8",
  );
  const compiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2022,
    },
  }).outputText;
  return import(
    `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
  );
}
const {
  collectCanvasReferences,
  validateReferenceConnection,
  canOutputReference,
  canReceiveReference,
} = await load("canvas-references");
const { duplicateCanvasSelection, removeCanvasSelection } =
  await load("canvas-editing");
const node = (id, type, metadata = {}) => ({
  id,
  title: id,
  type,
  metadata,
  position: { x: 10, y: 20 },
  width: 300,
  height: 240,
});
const edge = (fromNodeId, toNodeId, id = `${fromNodeId}-${toNodeId}`) => ({
  id,
  fromNodeId,
  toNodeId,
});
const graph = (nodes, connections = []) => ({
  nodes,
  connections,
  viewport: { x: 80, y: 80, k: 0.5 },
});

test("references expand groups, deduplicate direct inputs, and never follow generation provenance", () => {
  const value = graph(
    [
      node("group", "group"),
      node("nested", "group", { groupId: "group" }),
      node("asset", "image", { groupId: "nested", assetId: "private-asset" }),
      node("note", "text", {
        groupId: "group",
        content: "  keep this prompt  ",
      }),
      node("config", "config", { groupId: "group" }),
      node("result", "image", {
        assetId: "generated-asset",
        generationTaskId: "task",
      }),
    ],
    [
      edge("group", "config"),
      edge("asset", "config"),
      edge("note", "config"),
      edge("config", "result"),
    ],
  );
  const found = collectCanvasReferences(value, "config");
  assert.deepEqual(found.assetIds, ["private-asset"]);
  assert.deepEqual(found.notes, [
    { nodeId: "note", title: "note", content: "keep this prompt" },
  ]);
  assert.deepEqual(found.issues, []);
  assert.deepEqual(collectCanvasReferences(value, "result").assetIds, []);
});

test("a group input excludes the receiving media node's own output", () => {
  const value = graph(
    [
      node("group", "group"),
      node("target", "image", { groupId: "group", assetId: "own-output" }),
      node("other", "image", { groupId: "group", assetId: "reference" }),
    ],
    [edge("group", "target")],
  );
  assert.deepEqual(collectCanvasReferences(value, "target").assetIds, [
    "reference",
  ]);
});

test("damaged and unfinished references report issues and group cycles terminate", () => {
  const value = graph(
    [
      node("g1", "group", { groupId: "g2" }),
      node("g2", "group", { groupId: "g1" }),
      node("empty", "text"),
      node("pending", "video", { generationTaskId: "live-task" }),
      node("empty-group", "group"),
      node("config", "config"),
    ],
    [
      edge("g1", "config"),
      edge("missing", "config"),
      edge("empty", "config"),
      edge("pending", "config"),
      edge("empty-group", "config"),
    ],
  );
  const found = collectCanvasReferences(value, "config");
  assert.deepEqual(found.assetIds, []);
  assert.deepEqual(found.issues.map((item) => item.code).sort(), [
    "empty-group",
    "empty-note",
    "missing-node",
    "pending-result",
  ]);
});

test("manual connections accept ready references to media and config inputs", () => {
  const value = graph([
    node("asset", "image", { assetId: "a" }),
    node("config", "config"),
    node("other", "config"),
    node("pending", "video", { generationTaskId: "task" }),
    node("image-creator", "image", { creationNode: true }),
    node("video-creator", "video", { creationNode: true }),
  ]);
  assert.equal(validateReferenceConnection(value, "asset", "config"), null);
  assert.equal(
    validateReferenceConnection(value, "asset", "image-creator"),
    null,
  );
  assert.equal(
    validateReferenceConnection(value, "asset", "video-creator"),
    null,
  );
  assert.equal(canReceiveReference(node("audio", "audio")), false);
  assert.equal(validateReferenceConnection(value, "config", "config"), "self");
  assert.equal(validateReferenceConnection(value, "config", "other"), "source");
  assert.equal(validateReferenceConnection(value, "asset", "pending"), null);
  assert.equal(
    validateReferenceConnection(value, "pending", "config"),
    "source",
  );
  value.connections.push(edge("asset", "config"));
  assert.equal(
    validateReferenceConnection(value, "asset", "config"),
    "duplicate",
  );
  assert.equal(canOutputReference(node("empty", "image")), false);
  assert.equal(
    canOutputReference(
      node("ready", "video", { generationTaskId: "task", assetId: "a" }),
    ),
    true,
  );
});

test("media results do not inherit provenance as a new reference input", () => {
  const value = graph(
    [
      node("original", "image", { assetId: "original-asset" }),
      node("new-input", "image", { assetId: "new-asset" }),
      node("result", "image", {
        assetId: "result-asset",
        generationSourceNodeId: "original",
      }),
    ],
    [edge("original", "result"), edge("new-input", "result")],
  );
  assert.deepEqual(collectCanvasReferences(value, "result"), {
    assetIds: ["new-asset"],
    notes: [],
    issues: [],
  });
});

test("copied media chains retain remapped provenance and independent drafts", () => {
  const value = graph(
    [
      node("source", "image", { assetId: "source-asset" }),
      node("result", "image", {
        assetId: "result-asset",
        generationSourceNodeId: "source",
        generationDraft: { referenceIds: ["draft-asset"] },
      }),
    ],
    [edge("source", "result")],
  );
  const copied = duplicateCanvasSelection(value, ["source", "result"]);
  const [sourceId, resultId] = copied.selected;
  const result = copied.graph.nodes.find((item) => item.id === resultId);
  assert.equal(result.metadata.generationSourceNodeId, sourceId);
  assert.deepEqual(
    collectCanvasReferences(copied.graph, resultId).assetIds,
    [],
  );
  result.metadata.generationDraft.referenceIds.push("changed");
  assert.deepEqual(value.nodes[1].metadata.generationDraft.referenceIds, [
    "draft-asset",
  ]);
});

test("duplicating a group retains its member references without altering originals", () => {
  const value = graph(
    [
      node("group", "group"),
      node("asset", "image", { groupId: "group", assetId: "asset" }),
      node("config", "config", { groupId: "group" }),
      node("outside", "config"),
    ],
    [edge("asset", "config"), edge("asset", "outside")],
  );
  const copy = duplicateCanvasSelection(value, ["group"]);
  assert.equal(value.nodes.length, 4);
  assert.equal(copy.graph.nodes.length, 7);
  assert.equal(copy.graph.connections.length, 3);
  const clonedGroup = copy.graph.nodes.find(
    (item) => item.id === copy.selected[0],
  );
  const members = copy.graph.nodes.filter(
    (item) => item.metadata.groupId === clonedGroup.id,
  );
  assert.equal(members.length, 2);
  assert.ok(
    copy.graph.connections.some(
      (item) =>
        members.some((member) => member.id === item.fromNodeId) &&
        members.some((member) => member.id === item.toNodeId),
    ),
  );
  assert.deepEqual(clonedGroup.position, { x: 50, y: 60 });
});

test("deleting a group releases contents and removing edges never touches assets or tasks", () => {
  const value = graph(
    [
      node("group", "group"),
      node("asset", "image", {
        groupId: "group",
        assetId: "asset",
        generationTaskId: "task",
      }),
      node("config", "config"),
    ],
    [edge("group", "config"), edge("asset", "config")],
  );
  const removed = removeCanvasSelection(value, ["group"]);
  assert.equal(removed.nodes.length, 2);
  assert.equal(removed.nodes[0].metadata.groupId, undefined);
  assert.equal(removed.nodes[0].metadata.assetId, "asset");
  assert.equal(removed.nodes[0].metadata.generationTaskId, "task");
  assert.deepEqual(removed.connections, [edge("asset", "config")]);
  assert.equal(
    removeCanvasSelection(removed, [], "asset-config").nodes.length,
    2,
  );
  assert.equal(
    removeCanvasSelection(removed, [], "asset-config").connections.length,
    0,
  );
});
