import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import ts from "typescript";

const source = fs.readFileSync(
  new URL("../src/submission-recovery.ts", import.meta.url),
  "utf8",
);
const compiled = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.ESNext,
    target: ts.ScriptTarget.ES2022,
  },
}).outputText;
const {
  clearPendingSubmission,
  persistPendingSubmission,
  readPendingSubmission,
  submissionHasValidTask,
  submissionRecoveryKey,
  submissionWasRejected,
  withSubmissionRecoveryLock,
} = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
);
const entry = {
  quote_id: "quote-original",
  request_key: "original-request-key",
};
const key = submissionRecoveryKey("user-a", "project-a");
function memoryStorage() {
  const values = new Map();
  return {
    values,
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: (key) => values.delete(key),
  };
}

test("legacy submissions survive refresh and sign-in under the same user/project only", () => {
  const storage = memoryStorage();
  storage.setItem(key, JSON.stringify(entry));
  const recovered = readPendingSubmission(storage, key);
  assert.deepEqual(recovered, entry);
  persistPendingSubmission(storage, key, recovered);
  assert.equal(
    readPendingSubmission(
      storage,
      submissionRecoveryKey("user-b", "project-a"),
    ),
    null,
  );
  assert.equal(
    readPendingSubmission(
      storage,
      submissionRecoveryKey("user-a", "project-b"),
    ),
    null,
  );
  for (const cacheKey of storage.values.keys())
    if (cacheKey.startsWith("frame:draft:user-a:"))
      storage.removeItem(cacheKey);
  assert.deepEqual(readPendingSubmission(storage, key), entry);
});

test("an outstanding request cannot be replaced or cleared by another dialog", () => {
  const storage = memoryStorage();
  persistPendingSubmission(storage, key, entry);
  for (const changed of [
    { ...entry, quote_id: "another-quote" },
    { ...entry, request_key: "a-different-request-key" },
  ]) {
    assert.throws(() => persistPendingSubmission(storage, key, changed));
    assert.throws(() => clearPendingSubmission(storage, key, changed));
    assert.deepEqual(readPendingSubmission(storage, key), entry);
  }
});

test("corrupt recovery is retained and prevents overwriting it with a new request", () => {
  for (const raw of [
    "broken-json",
    "null",
    "[]",
    JSON.stringify({ ...entry, request_key: "short" }),
  ]) {
    const storage = memoryStorage();
    storage.setItem(key, raw);
    assert.throws(() => readPendingSubmission(storage, key));
    assert.throws(() => persistPendingSubmission(storage, key, entry));
    assert.equal(storage.getItem(key), raw);
  }
});

test("failed or unverifiable storage blocks before sending any submission", () => {
  for (const storage of [
    {
      ...memoryStorage(),
      getItem() {
        throw new Error("blocked");
      },
    },
    {
      ...memoryStorage(),
      setItem() {
        throw new Error("quota");
      },
    },
    { ...memoryStorage(), setItem() {} },
  ]) {
    let sends = 0;
    assert.throws(() => {
      persistPendingSubmission(storage, key, entry);
      sends++;
    });
    assert.equal(sends, 0);
  }
});

test("an unknown submission retains its original key after rejected retries", () => {
  for (const [status, code] of [
    [401, "unauthorized"],
    [403, "csrf_invalid"],
    [404, "not_found"],
    [409, "conflict"],
    [422, "invalid_input"],
    [402, "insufficient_credits"],
    [429, "rate_limited"],
    [500, "database_error"],
    [200, "invalid_response"],
  ]) {
    const storage = memoryStorage();
    persistPendingSubmission(storage, key, entry);
    assert.equal(submissionWasRejected(0, "network_error", false), false);
    assert.equal(submissionWasRejected(status, code, true), false);
    if (submissionWasRejected(status, code, true))
      clearPendingSubmission(storage, key, entry);
    assert.deepEqual(readPendingSubmission(storage, key), entry);
  }
  assert.equal(submissionWasRejected(409, "conflict", false), false);
});

test("precise server refusal permits a new quote while generic conflicts retain recovery", () => {
  for (const [status, code, retry] of [
    [422, "invalid_input", false],
    [422, "invalid_json", false],
    [402, "insufficient_credits", false],
    [409, "quote_expired", false],
    [409, "quote_expired", true],
  ]) {
    const storage = memoryStorage();
    persistPendingSubmission(storage, key, entry);
    assert.equal(submissionWasRejected(status, code, retry), true);
    clearPendingSubmission(storage, key, entry);
    assert.equal(readPendingSubmission(storage, key), null);
    persistPendingSubmission(storage, key, { ...entry, quote_id: "new-quote" });
  }
});

test("malformed or wrong-project task receipts cannot discard the original recovery", () => {
  const task = {
    id: `task_${"a".repeat(32)}`,
    project_id: "project-a",
    product_id: "product-a",
    kind: "image",
    model: "model-a",
    prompt: "Original prompt",
    credits: 10,
    execution_status: "queued",
    delivery_status: "pending",
    settlement_status: "frozen",
    procurement_status: "pending",
    asset_ids: [],
    demo: true,
    created_at: "2026-10-17T10:00:00Z",
    updated_at: "2026-10-17T10:00:00Z",
    input: {},
  };
  for (const invalid of [
    null,
    {},
    [],
    { ...task, project_id: "another-project" },
    { ...task, asset_ids: null },
    { ...task, id: "not-a-task" },
    { ...task, credits: -1 },
    { ...task, input: null },
    { ...task, updated_at: "invalid" },
  ]) {
    const storage = memoryStorage();
    persistPendingSubmission(storage, key, entry);
    assert.equal(submissionHasValidTask(invalid, "project-a"), false);
    if (submissionHasValidTask(invalid, "project-a"))
      clearPendingSubmission(storage, key, entry);
    assert.deepEqual(readPendingSubmission(storage, key), entry);
  }
  assert.equal(submissionHasValidTask(task, "project-a"), true);
});

test("cleanup failure retains the request until local storage can be safely cleared", () => {
  const storage = memoryStorage();
  persistPendingSubmission(storage, key, entry);
  assert.throws(() =>
    clearPendingSubmission({ ...storage, removeItem() {} }, key, entry),
  );
  assert.throws(() =>
    clearPendingSubmission(
      {
        ...storage,
        removeItem() {
          throw new Error("blocked");
        },
      },
      key,
      entry,
    ),
  );
  assert.deepEqual(readPendingSubmission(storage, key), entry);
  clearPendingSubmission(storage, key, entry);
  assert.equal(readPendingSubmission(storage, key), null);
});

function queuedLocks() {
  const queues = new Map();
  return {
    request(name, action) {
      const next = (queues.get(name) || Promise.resolve()).then(action);
      queues.set(
        name,
        next.catch(() => {}),
      );
      return next;
    },
  };
}

test("simultaneous tabs cannot overwrite or clear a different outstanding request", async () => {
  const storage = memoryStorage(),
    locks = queuedLocks();
  const another = { ...entry, request_key: "another-tab-request-key" };
  const results = await Promise.allSettled([
    withSubmissionRecoveryLock(locks, key, () =>
      persistPendingSubmission(storage, key, entry),
    ),
    withSubmissionRecoveryLock(locks, key, () =>
      persistPendingSubmission(storage, key, another),
    ),
  ]);
  assert.deepEqual(
    results.map((result) => result.status),
    ["fulfilled", "rejected"],
  );
  assert.deepEqual(readPendingSubmission(storage, key), entry);
  await withSubmissionRecoveryLock(locks, key, () =>
    clearPendingSubmission(storage, key, entry),
  );
  const cleanupRace = await Promise.allSettled([
    withSubmissionRecoveryLock(locks, key, () =>
      persistPendingSubmission(storage, key, another),
    ),
    withSubmissionRecoveryLock(locks, key, () =>
      clearPendingSubmission(storage, key, entry),
    ),
  ]);
  assert.deepEqual(
    cleanupRace.map((result) => result.status),
    ["fulfilled", "rejected"],
  );
  assert.deepEqual(readPendingSubmission(storage, key), another);
});

test("browsers without cross-tab locking stop before persisting or sending", async () => {
  let operations = 0;
  await assert.rejects(
    withSubmissionRecoveryLock(undefined, key, () => {
      operations++;
    }),
  );
  assert.equal(operations, 0);
});
