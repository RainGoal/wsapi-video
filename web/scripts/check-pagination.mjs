import assert from "node:assert/strict";
import fs from "node:fs";
import { test } from "node:test";
import ts from "typescript";

const source = fs.readFileSync(
  new URL("../src/paging.ts", import.meta.url),
  "utf8",
);
const compiled = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.ESNext,
    target: ts.ScriptTarget.ES2022,
  },
}).outputText;
const { pageRequest, returnedOrderReference, readAllItems } = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
);

test("paged searches preserve scoped filters and replace stale paging parameters", () => {
  const request = pageRequest(
    "/tasks?project_id=p1&status=failed&limit=9&offset=90",
    { q: "cloud & light", status: "" },
    2,
    25,
  );
  const url = new URL(request, "https://studio.invalid");
  assert.equal(url.pathname, "/tasks");
  assert.equal(url.searchParams.get("project_id"), "p1");
  assert.equal(url.searchParams.get("q"), "cloud & light");
  assert.equal(url.searchParams.has("status"), false);
  assert.equal(url.searchParams.get("limit"), "25");
  assert.equal(url.searchParams.get("offset"), "50");
  assert.equal(
    new URL(pageRequest("/assets", {}, -1, 25), url).searchParams.get("offset"),
    "0",
  );
});

test("payment return lookup only accepts a single exact reference", () => {
  assert.equal(
    returnedOrderReference(
      "?out_trade_no=own%26order&money=0.01&trade_status=TRADE_SUCCESS",
    ),
    "own&order",
  );
  for (const search of [
    "",
    "?out_trade_no=",
    "?out_trade_no=own&out_trade_no=own",
    "?out_trade_no=own&out_trade_no=other",
  ])
    assert.equal(returnedOrderReference(search), undefined);
});

test("whole project collections include records beyond two server pages", async () => {
  const records = Array.from({ length: 205 }, (_, index) => ({
    id: `asset-${index}`,
  }));
  const offsets = [];
  const all = await readAllItems("/assets?project_id=p1", async (request) => {
    const params = new URL(request, "https://studio.invalid").searchParams;
    const offset = Number(params.get("offset"));
    offsets.push(offset);
    assert.equal(params.get("project_id"), "p1");
    assert.equal(params.get("limit"), "100");
    return {
      items: records.slice(offset, offset + 100),
      total: records.length,
      limit: 100,
      offset,
    };
  });
  assert.deepEqual(offsets, [0, 100, 200]);
  assert.deepEqual(all, records);
});

test("short server pages advance by the received count without repeating records", async () => {
  const records = Array.from({ length: 95 }, (_, index) => ({
    id: `project-${index}`,
  }));
  const offsets = [];
  const all = await readAllItems("/projects?status=all", async (request) => {
    const offset = Number(
      new URL(request, "https://studio.invalid").searchParams.get("offset"),
    );
    offsets.push(offset);
    return {
      items: records.slice(offset, offset + 40),
      total: records.length,
      limit: 40,
      offset,
    };
  });
  assert.deepEqual(offsets, [0, 40, 80]);
  assert.deepEqual(all, records);
});

test("duplicates are merged and an incomplete concurrent snapshot is rejected", async () => {
  const first = {
    items: [{ id: "a" }, { id: "b", revision: 1 }],
    total: 3,
    limit: 100,
    offset: 0,
  };
  const changed = {
    items: [{ id: "b", revision: 2 }],
    total: 2,
    limit: 100,
    offset: 2,
  };
  let calls = 0;
  const merged = await readAllItems("/projects", async () =>
    calls++ === 0 ? first : changed,
  );
  assert.deepEqual(merged, [{ id: "a" }, { id: "b", revision: 2 }]);
  calls = 0;
  await assert.rejects(
    readAllItems("/projects", async () =>
      calls++ === 0 ? first : { ...changed, total: 3 },
    ),
    /列表在加载期间发生变化/,
  );
});

test("empty, malformed and failed later pages never return a truncated collection", async () => {
  await assert.rejects(
    readAllItems("/tasks", async () => ({
      items: [],
      total: 20,
      limit: 100,
      offset: 0,
    })),
    /列表未完整加载/,
  );
  await assert.rejects(
    readAllItems("/tasks", async () => ({
      items: [{ id: "x" }],
      total: 1,
      limit: 100,
      offset: 50,
    })),
    /无法识别/,
  );
  await assert.rejects(
    readAllItems("/tasks", async () => ({
      items: [{}],
      total: 1,
      limit: 100,
      offset: 0,
    })),
    /无法识别/,
  );
  let calls = 0;
  await assert.rejects(
    readAllItems("/tasks", async () => {
      if (calls++) throw new Error("network unavailable");
      return { items: [{ id: "x" }], total: 2, limit: 100, offset: 0 };
    }),
    /network unavailable/,
  );
});
