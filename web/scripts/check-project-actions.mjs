import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import ts from "typescript";

const source = ts.createSourceFile(
  "Projects.tsx",
  fs.readFileSync(new URL("../src/Projects.tsx", import.meta.url), "utf8"),
  ts.ScriptTarget.Latest,
  true,
  ts.ScriptKind.TSX,
);
const component = source.statements.find(
  (node) => ts.isFunctionDeclaration(node) && node.name?.text === "Projects",
);
const actions = component.body.statements
  .filter(
    (node) =>
      ts.isFunctionDeclaration(node) &&
      ["save", "remove", "closeEditor", "closeDelete"].includes(
        node.name?.text,
      ),
  )
  .map((node) => node.getText(source))
  .join("\n");
const compiled = ts.transpileModule(
  `export function bindActions(env) {
    const { api, editing, deleteProject, busyRef, live, FormData, setBusy,
      setError, setCreate, setEditing, setDeleteProject, projects, navigate,
      notify, t, errorMessage } = env;
    ${actions}
    return { save, remove, closeEditor, closeDelete };
  }`,
  {
    compilerOptions: {
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2022,
    },
  },
).outputText;
const { bindActions } = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
);
const project = (status = "active", revision = 7) => ({
  id: "project-a",
  name: "My canvas",
  status,
  revision,
});
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
function harness(options = {}) {
  const effects = [],
    sends = [];
  const env = {
    create: true,
    editing: options.editing || null,
    deleteProject: options.deleteProject || null,
    busyRef: { current: false },
    live: { current: true },
    api: async (...args) => {
      sends.push(args);
      return options.api ? options.api(...args) : project();
    },
    FormData: class {
      constructor(values) {
        this.values = values;
      }
      get(name) {
        return this.values[name];
      }
    },
    projects: { refresh: () => effects.push(["refresh"]) },
    navigate: (...args) => effects.push(["navigate", ...args]),
    notify: (...args) => effects.push(["notify", ...args]),
    t: (value) => value,
    errorMessage: (error) => error.message,
  };
  for (const [setter, field] of [
    ["setBusy", "busy"],
    ["setError", "error"],
    ["setCreate", "create"],
    ["setEditing", "editing"],
    ["setDeleteProject", "deleteProject"],
  ])
    env[setter] = (value) => {
      env[field] = value;
      effects.push([setter, value]);
    };
  return { env, effects, sends, actions: () => bindActions(env) };
}
const formEvent = () => ({
  preventDefault() {},
  currentTarget: { name: "Updated canvas", description: "Notes" },
});

test("saving blocks duplicate submissions and immediate dialog closure", async () => {
  const response = deferred();
  const h = harness({ api: () => response.promise });
  const actions = h.actions();
  const first = actions.save(formEvent());
  const second = actions.save(formEvent());
  const beforeClose = [...h.effects];
  actions.closeEditor();
  assert.deepEqual(h.effects, beforeClose);
  assert.equal(h.sends.length, 1);
  response.resolve(project());
  await Promise.all([first, second]);
  assert.equal(h.env.create, false);
  assert.equal(h.env.busy, false);
  assert.equal(h.effects.filter(([name]) => name === "navigate").length, 1);
});

test("a failed save unlocks its existing dialog and can then be closed", async () => {
  const h = harness({
    api: () => {
      throw new Error("Save failed");
    },
  });
  await h.actions().save(formEvent());
  assert.equal(h.env.error, "Save failed");
  assert.equal(h.env.create, true);
  assert.equal(h.env.busy, false);
  assert.equal(h.env.busyRef.current, false);
  h.actions().closeEditor();
  assert.equal(h.env.create, false);
});

test("a save response cannot navigate or update a page that has unmounted", async () => {
  const response = deferred();
  const h = harness({ api: () => response.promise });
  const pending = h.actions().save(formEvent());
  h.env.live.current = false;
  const before = [...h.effects];
  response.resolve(project());
  await pending;
  assert.deepEqual(h.effects, before);
});

test("deletion retries with the archived response revision after DELETE fails", async () => {
  const archived = project("archived", 8);
  let attempts = 0;
  const h = harness({
    deleteProject: project(),
    api: (_url, method) => {
      if (method === "PATCH") return archived;
      if (++attempts === 1) throw new Error("Delete failed");
    },
  });
  await h.actions().remove();
  assert.equal(h.env.deleteProject, archived);
  assert.equal(h.env.busyRef.current, false);
  await h.actions().remove();
  assert.deepEqual(
    h.sends.map(([, method, body]) => [method, body.revision]),
    [
      ["PATCH", 7],
      ["DELETE", 8],
      ["DELETE", 8],
    ],
  );
  assert.equal(h.env.deleteProject, null);
  assert.equal(attempts, 2);
});

test("pending deletion blocks duplicate requests and immediate dialog closure", async () => {
  const response = deferred();
  const target = project("archived");
  const h = harness({ deleteProject: target, api: () => response.promise });
  const actions = h.actions();
  const first = actions.remove(),
    second = actions.remove();
  actions.closeDelete();
  assert.equal(h.env.deleteProject, target);
  assert.equal(h.sends.length, 1);
  response.resolve();
  await Promise.all([first, second]);
  assert.equal(h.env.deleteProject, null);
  assert.equal(h.env.busy, false);
});

test("unmounting during archival prevents the follow-up DELETE and stale effects", async () => {
  const response = deferred();
  const h = harness({ deleteProject: project(), api: () => response.promise });
  const pending = h.actions().remove();
  h.env.live.current = false;
  const before = [...h.effects];
  response.resolve(project("archived", 8));
  await pending;
  assert.equal(h.sends.length, 1);
  assert.deepEqual(h.effects, before);
});

test("an unmounted delete response cannot close a newer page or report success", async () => {
  const response = deferred();
  const h = harness({
    deleteProject: project("archived"),
    api: () => response.promise,
  });
  const pending = h.actions().remove();
  h.env.live.current = false;
  const before = [...h.effects];
  response.resolve();
  await pending;
  assert.deepEqual(h.effects, before);
});
