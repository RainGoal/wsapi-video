import assert from "node:assert/strict";
import fs from "node:fs";
import { test } from "node:test";
import ts from "typescript";

async function loadSource(path) {
  const compiled = ts.transpileModule(
    fs.readFileSync(new URL(path, import.meta.url), "utf8"),
    {
      compilerOptions: {
        module: ts.ModuleKind.ESNext,
        target: ts.ScriptTarget.ES2022,
      },
    },
  ).outputText;
  return import(
    `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
  );
}

const {
  createCanvasHistory,
  checkpointCanvasHistory,
  undoCanvasHistory,
  redoCanvasHistory,
  resetCanvasHistory,
} = await loadSource("../src/lib/canvas/canvas-history.ts");
const { addAcceptedTask } = await loadSource("../src/creation.ts");
const graph = (...ids) => ({
  nodes: ids.map((id) => ({
    id,
    type: "text",
    title: id,
    position: { x: 0, y: 0 },
    width: 260,
    height: 190,
    metadata: { content: id },
  })),
  connections: [],
  viewport: { x: 80, y: 80, k: 1 },
});

test("an external asset insertion is undoable even without an earlier edit", () => {
  const history = createCanvasHistory();
  const before = graph();
  const inserted = graph("asset");
  inserted.nodes[0].type = "image";
  inserted.nodes[0].metadata = { assetId: "asset-1" };
  checkpointCanvasHistory(history, before);
  const undone = undoCanvasHistory(history, inserted);
  assert.deepEqual(undone, before);
  assert.deepEqual(redoCanvasHistory(history, undone), inserted);
});

test("undoing an external insertion preserves the preceding canvas edit", () => {
  const history = createCanvasHistory();
  checkpointCanvasHistory(history, graph());
  checkpointCanvasHistory(history, graph("note"));
  const inserted = graph("note", "asset");
  const first = undoCanvasHistory(history, inserted);
  assert.deepEqual(first, graph("note"));
  const second = undoCanvasHistory(history, first);
  assert.deepEqual(second, graph());
  const redone = redoCanvasHistory(history, second);
  assert.deepEqual(redoCanvasHistory(history, redone), inserted);
});

test("new editors can reuse both directions of the same project history", () => {
  const history = createCanvasHistory();
  checkpointCanvasHistory(history, graph());
  checkpointCanvasHistory(history, graph("note"));
  const current = graph("note", "asset");
  const beforeSwitch = undoCanvasHistory(history, current);
  const remountedRedo = redoCanvasHistory.bind(null, history);
  assert.deepEqual(remountedRedo(beforeSwitch), current);
  const remountedUndo = undoCanvasHistory.bind(null, history);
  assert.deepEqual(remountedUndo(current), graph("note"));
});

test("editing after undo discards the previous redo branch", () => {
  const history = createCanvasHistory();
  checkpointCanvasHistory(history, graph());
  const before = undoCanvasHistory(history, graph("discarded"));
  checkpointCanvasHistory(history, before);
  const current = graph("replacement");
  assert.equal(redoCanvasHistory(history, current), undefined);
  assert.deepEqual(undoCanvasHistory(history, current), graph());
});

test("replacing the document baseline clears undo and redo together", () => {
  const history = createCanvasHistory();
  checkpointCanvasHistory(history, graph());
  checkpointCanvasHistory(history, graph("old-cloud"));
  undoCanvasHistory(history, graph("old-draft"));
  resetCanvasHistory(history);
  assert.equal(undoCanvasHistory(history, graph("new-baseline")), undefined);
  assert.equal(redoCanvasHistory(history, graph("new-baseline")), undefined);
  checkpointCanvasHistory(history, graph("new-baseline"));
  assert.deepEqual(
    undoCanvasHistory(history, graph("edited")),
    graph("new-baseline"),
  );
});

test("one drag checkpoint restores the start across multiple movement frames", () => {
  const history = createCanvasHistory();
  const before = graph("dragged");
  checkpointCanvasHistory(history, before);
  let current;
  for (const x of [4, 16, 47, 105]) {
    current = structuredClone(before);
    current.nodes[0].position.x = x;
  }
  const undone = undoCanvasHistory(history, current);
  assert.deepEqual(undone, before);
  assert.equal(undoCanvasHistory(history, undone), undefined);
  assert.deepEqual(redoCanvasHistory(history, undone), current);
});

test("the latest fifty edits remain undoable and redoable", () => {
  const history = createCanvasHistory();
  let current = graph("0");
  for (let index = 1; index <= 55; index++) {
    checkpointCanvasHistory(history, current);
    current = graph(String(index));
  }
  assert.equal(history.past.length, 50);
  for (let index = 0; index < 50; index++)
    current = undoCanvasHistory(history, current);
  assert.deepEqual(current, graph("5"));
  assert.equal(undoCanvasHistory(history, current), undefined);
  for (let index = 0; index < 50; index++)
    current = redoCanvasHistory(history, current);
  assert.deepEqual(current, graph("55"));
  assert.equal(redoCanvasHistory(history, current), undefined);
  assert.equal(history.past.length, 50);
});

test("snapshots isolate nested metadata in both directions", () => {
  const history = createCanvasHistory();
  const current = graph("note");
  checkpointCanvasHistory(history, current);
  current.nodes[0].metadata.content = "edited";
  const undone = undoCanvasHistory(history, current);
  assert.equal(undone.nodes[0].metadata.content, "note");
  current.nodes[0].metadata.content = "changed after undo";
  undone.nodes[0].metadata.content = "restored edit";
  const redone = redoCanvasHistory(history, undone);
  assert.equal(redone.nodes[0].metadata.content, "edited");
  redone.nodes[0].metadata.content = "changed after redo";
  assert.equal(
    undoCanvasHistory(history, redone).nodes[0].metadata.content,
    "restored edit",
  );
});

test("undoing accepted task layout leaves the accepted execution fact intact", () => {
  const history = createCanvasHistory();
  const before = graph("note");
  const task = {
    id: "task-1",
    project_id: "p1",
    product_id: "image",
    kind: "image",
    input: {},
    prompt: "Accepted prompt",
    credits: 100,
    execution_status: "queued",
    delivery_status: "pending",
    asset_ids: [],
  };
  const accepted = structuredClone(task);
  checkpointCanvasHistory(history, before);
  const generated = addAcceptedTask(before, task, "Concept");
  assert.equal(generated.nodes.at(-1).metadata.generationTaskId, task.id);
  const undone = undoCanvasHistory(history, generated);
  assert.deepEqual(undone, before);
  assert.deepEqual(task, accepted);
  assert.deepEqual(redoCanvasHistory(history, undone), generated);
  assert.deepEqual(task, accepted);
});
