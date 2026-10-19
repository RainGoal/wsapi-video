import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import ts from "typescript";

function sourceURL(path, dependencies = {}) {
  let compiled = ts.transpileModule(
    fs.readFileSync(new URL(path, import.meta.url), "utf8"),
    {
      compilerOptions: {
        module: ts.ModuleKind.ESNext,
        target: ts.ScriptTarget.ES2022,
      },
    },
  ).outputText;
  for (const [specifier, url] of Object.entries(dependencies))
    compiled = compiled.replaceAll(
      JSON.stringify(specifier),
      JSON.stringify(url),
    );
  return `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`;
}
const canvasURL = sourceURL("../src/types/canvas.ts");
const { emptyGraph } = await import(canvasURL);
const {
  canvasGraphFromDocument,
  canvasDocumentGraph,
  withCanvasAppearanceDefaults,
} = await import(
  sourceURL("../src/lib/canvas/canvas-document.ts", {
    "../../types/canvas": canvasURL,
  })
);
const {
  applyGroupSelection,
  applyUngroupSelection,
  snapNodesIntoGroup,
  GROUP_WRAP_TOP_PADDING,
} = await import(
  sourceURL("../src/lib/canvas/canvas-node-geometry.ts", {
    "@/types/canvas": canvasURL,
  })
);
const {
  createCanvasHistory,
  checkpointCanvasHistory,
  undoCanvasHistory,
  redoCanvasHistory,
} = await import(sourceURL("../src/lib/canvas/canvas-history.ts"));
const document = (graph) => ({ revision: 4, graph_schema_version: 1, graph });
const node = (id, type = "image", groupId) => ({
  id,
  type,
  title: id,
  position: { x: 12, y: 24 },
  width: 300,
  height: 240,
  metadata: groupId ? { groupId } : {},
});
const edge = (fromNodeId, toNodeId) => ({
  id: `${fromNodeId}-${toNodeId}`,
  fromNodeId,
  toNodeId,
});

test("canvas appearance and node relations survive project wire round trips", () => {
  for (const backgroundMode of ["lines", "dots", "blank"])
    for (const showImageInfo of [true, false]) {
      const graph = {
        ...emptyGraph(),
        nodes: [
          node("group", "group"),
          node("image", "image", "group"),
          node("video", "video"),
        ],
        connections: [edge("image", "video")],
        viewport: { x: -10, y: 240, k: 0.6 },
        backgroundMode,
        showImageInfo,
      };
      const wire = canvasDocumentGraph(graph);
      const loaded = canvasGraphFromDocument(
        document(JSON.parse(JSON.stringify(wire))),
      );
      assert.deepEqual(loaded, graph);
      assert.deepEqual(wire.viewport, { x: -10, y: 240, zoom: 0.6 });
    }
});

test("legacy cloud documents and local drafts use the same appearance defaults", () => {
  const legacy = { nodes: [], edges: [] };
  const loaded = canvasGraphFromDocument(document(legacy));
  assert.deepEqual(loaded, emptyGraph());
  const oldDraft = {
    nodes: [],
    connections: [],
    viewport: emptyGraph().viewport,
  };
  assert.deepEqual(withCanvasAppearanceDefaults(oldDraft), loaded);
  assert.equal(oldDraft.backgroundMode, undefined);
  assert.equal(oldDraft.showImageInfo, undefined);
});

test("draft recovery and undo redo preserve project appearance independently", () => {
  const before = { ...emptyGraph(), nodes: [node("image")] };
  const history = createCanvasHistory();
  checkpointCanvasHistory(history, before);
  const after = { ...before, backgroundMode: "blank", showImageInfo: true };
  const recovered = withCanvasAppearanceDefaults(
    JSON.parse(JSON.stringify(after)),
  );
  assert.deepEqual(recovered, after);
  const undone = undoCanvasHistory(history, recovered);
  assert.deepEqual(undone, before);
  assert.deepEqual(redoCanvasHistory(history, undone), after);
  assert.deepEqual(
    canvasGraphFromDocument(document(canvasDocumentGraph(recovered))),
    after,
  );
});

test("regrouping removes only emptied source groups and retains deliberate empty groups", () => {
  const nodes = [
    node("empty", "group"),
    node("old", "group"),
    node("partial", "group"),
    node("a", "image", "old"),
    node("b", "image", "partial"),
    node("keep", "image", "partial"),
    node("target", "video"),
  ];
  const connections = [
    edge("empty", "target"),
    edge("old", "target"),
    edge("a", "target"),
  ];
  const next = applyGroupSelection(
    new Set(["a", "b"]),
    nodes,
    connections,
    node("new", "group"),
  );
  assert.deepEqual(
    next.nodes.map((item) => item.id),
    ["empty", "partial", "new", "a", "b", "keep", "target"],
  );
  assert.equal(
    next.nodes.find((item) => item.id === "a").metadata.groupId,
    "new",
  );
  assert.equal(
    next.nodes.find((item) => item.id === "b").metadata.groupId,
    "new",
  );
  assert.equal(
    next.nodes.find((item) => item.id === "keep").metadata.groupId,
    "partial",
  );
  assert.deepEqual(next.connections, [connections[0], connections[2]]);
  assert.equal(nodes.find((item) => item.id === "a").metadata.groupId, "old");
});

test("ungrouping preserves unrelated empty groups and only releases selected members", () => {
  const nodes = [
    node("empty", "group"),
    node("old", "group"),
    node("partial", "group"),
    node("a", "image", "old"),
    node("b", "image", "partial"),
    node("keep", "image", "partial"),
    node("target", "video"),
  ];
  const connections = [
    edge("empty", "target"),
    edge("old", "target"),
    edge("b", "target"),
  ];
  const next = applyUngroupSelection(new Set(["a", "b"]), nodes, connections);
  assert.deepEqual(
    next.nodes.map((item) => item.id),
    ["empty", "partial", "a", "b", "keep", "target"],
  );
  assert.equal(
    next.nodes.find((item) => item.id === "a").metadata.groupId,
    undefined,
  );
  assert.equal(
    next.nodes.find((item) => item.id === "b").metadata.groupId,
    undefined,
  );
  assert.equal(
    next.nodes.find((item) => item.id === "keep").metadata.groupId,
    "partial",
  );
  assert.deepEqual(next.connections, [connections[0], connections[2]]);
  const explicitlyRemoved = applyUngroupSelection(
    new Set(["empty"]),
    nodes,
    connections,
  );
  assert.equal(
    explicitlyRemoved.nodes.some((item) => item.id === "empty"),
    false,
  );
  assert.equal(
    explicitlyRemoved.nodes.some((item) => item.id === "old"),
    true,
  );
});

test("dropping members near a group edge preserves labels and member spacing", () => {
  const group = { ...node("group", "group"), width: 800, height: 600 };
  const a = { ...node("a"), position: { x: 0, y: 0 } };
  const b = { ...node("b"), position: { x: 320, y: 0 } };
  const untouched = node("outside");
  const result = snapNodesIntoGroup(
    new Set(["a", "b"]),
    [group, a, b, untouched],
    group,
  );
  assert.equal(result[1].position.y, group.position.y + GROUP_WRAP_TOP_PADDING);
  assert.ok(result[1].position.y - 31 >= group.position.y + 40);
  assert.equal(result[2].position.x - result[1].position.x, 320);
  assert.equal(result[1].metadata.groupId, group.id);
  assert.equal(result[2].metadata.groupId, group.id);
  assert.equal(result[3], untouched);
  assert.deepEqual(a.position, { x: 0, y: 0 });
});
