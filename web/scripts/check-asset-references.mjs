import assert from "node:assert/strict";
import fs from "node:fs";
import { test } from "node:test";
import ts from "typescript";

const source = fs.readFileSync(
  new URL("../src/asset-references.ts", import.meta.url),
  "utf8",
);
const compiled = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.ESNext,
    target: ts.ScriptTarget.ES2022,
  },
}).outputText;
const { assetReferenceTarget, validReferencePage, workspaceLocation } =
  await import(
    `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
  );
const reference = (owner_type, owner_id = owner_type, extra = {}) => ({
  project_id: "p1",
  project_name: "Film",
  project_status: "active",
  project_deleted: false,
  owner_type,
  owner_id,
  owner_label: "",
  ...extra,
});

test("references open canvas, project assets and task history without retired views", () => {
  assert.deepEqual(assetReferenceTarget(reference("canvas")), {
    page: "project",
    id: "p1?view=canvas",
  });
  assert.deepEqual(assetReferenceTarget(reference("cover")), {
    page: "project",
    id: "p1?view=assets",
  });
  assert.deepEqual(assetReferenceTarget(reference("shot", "shot & 片段")), {
    page: "project",
    id: "p1?view=canvas",
  });
  assert.deepEqual(assetReferenceTarget(reference("task", "task-id")), {
    page: "tasks",
    id: "task-id",
  });
});

test("deleted projects have no project link while their task history remains reachable", () => {
  for (const type of ["canvas", "cover", "shot"])
    assert.equal(
      assetReferenceTarget(reference(type, "owner", { project_deleted: true })),
      null,
    );
  assert.deepEqual(
    assetReferenceTarget(
      reference("task", "task-id", { project_deleted: true }),
    ),
    { page: "tasks", id: "task-id" },
  );
  assert.deepEqual(
    assetReferenceTarget(
      reference("canvas", "p1", { project_status: "archived" }),
    ),
    { page: "project", id: "p1?view=canvas" },
  );
});

test("current workspace routes retain their view and retired deep links fall back to canvas", () => {
  for (const view of ["canvas", "assets"])
    assert.deepEqual(workspaceLocation(`view=${view}`), { view });
  for (const query of [
    "view=shots",
    "view=shots&shot=target",
    "shot=target",
    "shot_id=target",
    "view=assets&shot_id=target",
    "shot=",
    "shot=a&shot=b",
  ])
    assert.deepEqual(workspaceLocation(query), { view: "canvas" });
});

test("unknown and ambiguous view selectors keep the default workspace route", () => {
  assert.deepEqual(workspaceLocation(), { view: undefined });
  assert.equal(workspaceLocation("view=unknown").view, undefined);
  assert.equal(workspaceLocation("view=shots&view=canvas").view, undefined);
  assert.equal(
    workspaceLocation("view=assets&view=canvas&shot_id=target").view,
    undefined,
  );
});

test("a reference snapshot exposes exactly the requested page, including an exhausted page", () => {
  const items = Array.from({ length: 25 }, (_, index) =>
    reference("shot", `s${index}`),
  );
  assert.equal(
    validReferencePage({ items, total: 26, offset: 0, limit: 25 }, 0, 25),
    true,
  );
  assert.equal(
    validReferencePage(
      { items: [reference("task")], total: 26, offset: 25, limit: 25 },
      25,
      25,
    ),
    true,
  );
  assert.equal(
    validReferencePage({ items: [], total: 0, offset: 0, limit: 25 }, 0, 25),
    true,
  );
  assert.equal(
    validReferencePage({ items: [], total: 20, offset: 25, limit: 25 }, 25, 25),
    true,
  );
});

test("retired references remain visible facts in reference snapshots", () => {
  const historical = reference("shot", "legacy-shot", {
    owner_label: "已保存的历史位置",
  });
  const page = { items: [historical], total: 1, offset: 0, limit: 25 };
  assert.equal(validReferencePage(page, 0, 25), true);
  assert.deepEqual(assetReferenceTarget(historical), {
    page: "project",
    id: "p1?view=canvas",
  });
});

test("incomplete, duplicate and malformed reference snapshots cannot appear as an empty or complete list", () => {
  const one = reference("shot");
  const page = { items: [one], total: 1, offset: 0, limit: 25 };
  for (const invalid of [
    null,
    { ...page, total: 2 },
    { ...page, items: [], total: 1 },
    { ...page, items: [one, one], total: 2 },
    { ...page, offset: 25 },
    { ...page, limit: 100 },
    { ...page, total: -1 },
    { ...page, total: 1.5 },
    { ...page, total: 0 },
    { ...page, items: [{ ...one, project_id: "" }] },
    { ...page, items: [{ ...one, owner_type: "unknown" }] },
    { ...page, items: [{ ...one, project_deleted: "false" }] },
    { ...page, items: [{ ...one, owner_label: null }] },
  ])
    assert.equal(validReferencePage(invalid, 0, 25), false);
});
