import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import ts from "typescript";

const source = fs.readFileSync(
  new URL("../src/admin/adjustment-recovery.ts", import.meta.url),
  "utf8",
);
const compiled = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.ESNext,
    target: ts.ScriptTarget.ES2022,
  },
}).outputText;
const {
  adjustmentHasValidWallet,
  adjustmentWasRejected,
  clearPendingAdjustment,
  persistPendingAdjustment,
  readPendingAdjustment,
  withAdjustmentRecoveryLock,
} = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
);
function memoryStorage() {
  const values = new Map();
  return {
    values,
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: (key) => values.delete(key),
  };
}
const request = () => ({
  admin_id: "admin-a",
  target: {
    id: "user-a",
    email: "creator@example.test",
    display_name: "创作者",
  },
  payload: {
    user_id: "user-a",
    amount: -29,
    reason: " 核对原调账依据，保留空白与中文。 ",
    request_key: "original-request-key-123",
  },
});

const recordsSource = ts.createSourceFile(
  "RecordsPage.tsx",
  fs.readFileSync(
    new URL("../src/admin/RecordsPage.tsx", import.meta.url),
    "utf8",
  ),
  ts.ScriptTarget.Latest,
  true,
  ts.ScriptKind.TSX,
);
const adjustmentForm = recordsSource.statements.find(
  (node) =>
    ts.isFunctionDeclaration(node) && node.name?.text === "AdjustmentForm",
);
const actions = adjustmentForm.body.statements
  .filter(
    (node) =>
      ts.isFunctionDeclaration(node) &&
      ["submit", "clearConfirmedRequest"].includes(node.name?.text),
  )
  .map((node) => node.getText(recordsSource))
  .join("\n");
const boundActions = ts.transpileModule(
  `export function bindActions(env) {
    const { busy, busyRef, live, settled, pending, storageError, invalid,
      adminID, selected, numericAmount, reason, localStorage, navigator, crypto,
      api, ApiError, a, errorMessage, storageFailure, setBusy, setPending,
      setError, setSettled, setStorageError, setConfirmed, setAmount, setReason,
      detail, refreshWallet, notify, adjustmentWasRejected,
      adjustmentHasValidWallet, persistPendingAdjustment, clearPendingAdjustment,
      withAdjustmentRecoveryLock } = env;
    ${actions}
    return { submit };
  }`,
  {
    compilerOptions: {
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2022,
    },
  },
).outputText;
const { bindActions } = await import(
  `data:text/javascript;base64,${Buffer.from(boundActions).toString("base64")}`
);

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
function deferred() {
  let resolve;
  const promise = new Promise((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
const flushActions = async () => {
  for (let index = 0; index < 12; index++) await Promise.resolve();
};

function actionHarness(options = {}) {
  const storage = options.storage || memoryStorage();
  const entry = options.pending === null ? null : request();
  if (entry) persistPendingAdjustment(storage, entry);
  const effects = [],
    sends = [],
    live = { current: true };
  const state = {};
  const environment = {
    busy: false,
    busyRef: { current: false },
    live,
    pending: entry,
    settled: options.settled || null,
    storageError: "",
    invalid: false,
    adminID: "admin-a",
    selected: request().target,
    numericAmount: request().payload.amount,
    reason: request().payload.reason,
    localStorage: storage,
    navigator: {
      locks: options.locks === undefined ? queuedLocks() : options.locks,
    },
    crypto: { randomUUID: () => options.requestKey || "new-request-key-123" },
    api: async (...args) => {
      sends.push(args);
      return options.response
        ? options.response.promise
        : { available: 100, frozen: 0, demo: false };
    },
    ApiError: class extends Error {
      constructor(status, code, message) {
        super(message);
        this.status = status;
        this.code = code;
      }
    },
    a: (chinese) => chinese,
    errorMessage: (error) => error.message,
    storageFailure: () => "storage unavailable",
    detail: { refresh: () => effects.push("detail.refresh") },
    refreshWallet: () => effects.push("refreshWallet"),
    notify: () => effects.push("notify"),
    adjustmentHasValidWallet,
    adjustmentWasRejected,
    persistPendingAdjustment,
    clearPendingAdjustment,
    withAdjustmentRecoveryLock,
  };
  for (const name of [
    "Busy",
    "Pending",
    "Error",
    "Settled",
    "StorageError",
    "Confirmed",
    "Amount",
    "Reason",
  ])
    environment[`set${name}`] = (value) => {
      state[name] = value;
      effects.push(`set${name}`);
    };
  const { submit } = bindActions(environment);
  return {
    storage,
    live,
    effects,
    sends,
    state,
    environment,
    submit: () => submit({ preventDefault() {} }),
  };
}

test("the same rendered form cannot submit its pending adjustment twice", async () => {
  const response = deferred();
  const harness = actionHarness({ response });
  const first = harness.submit(),
    second = harness.submit();
  await flushActions();
  assert.equal(harness.sends.length, 1);
  response.resolve({ available: 100, frozen: 0, demo: false });
  await Promise.all([first, second]);
});

test("an unmounted form retains recovery when its in-flight response succeeds", async () => {
  const response = deferred();
  const harness = actionHarness({ response });
  const submission = harness.submit();
  await flushActions();
  assert.equal(harness.sends.length, 1);
  harness.live.current = false;
  const before = [...harness.effects];
  response.resolve({ available: 100, frozen: 0, demo: false });
  await submission;
  assert.deepEqual(
    readPendingAdjustment(harness.storage, "admin-a"),
    request(),
  );
  assert.deepEqual(harness.effects, before);
});

test("a form unmounted while waiting for its recovery lock neither writes nor sends", async () => {
  const release = deferred(),
    locks = queuedLocks();
  const held = locks.request(
    "frame:admin-adjustment:admin-a",
    () => release.promise,
  );
  const harness = actionHarness({ pending: null, locks });
  const submission = harness.submit();
  harness.live.current = false;
  const before = [...harness.effects];
  release.resolve();
  await Promise.all([held, submission]);
  assert.equal(harness.sends.length, 0);
  assert.equal(readPendingAdjustment(harness.storage, "admin-a"), null);
  assert.deepEqual(harness.effects, before);
});

test("the form refuses to persist or send without cross-tab locks", async () => {
  const harness = actionHarness({ pending: null, locks: null });
  await harness.submit();
  assert.equal(harness.sends.length, 0);
  assert.equal(readPendingAdjustment(harness.storage, "admin-a"), null);
  assert.ok(harness.state.StorageError);
});

test("two tabs serialize new requests while the accepted tab waits outside the lock", async () => {
  const storage = memoryStorage(),
    locks = queuedLocks(),
    response = deferred();
  const first = actionHarness({
    storage,
    locks,
    response,
    pending: null,
    requestKey: "first-tab-request-key",
  });
  const second = actionHarness({
    storage,
    locks,
    pending: null,
    requestKey: "second-tab-request-key",
  });
  const firstSubmission = first.submit(),
    secondSubmission = second.submit();
  await flushActions();
  assert.equal(first.sends.length, 1);
  assert.equal(second.sends.length, 0);
  assert.ok(second.state.StorageError);
  let observed;
  await withAdjustmentRecoveryLock(locks, "admin-a", () => {
    observed = readPendingAdjustment(storage, "admin-a");
  });
  assert.equal(observed.payload.request_key, "first-tab-request-key");
  response.resolve({ available: 100, frozen: 0, demo: false });
  await Promise.all([firstSubmission, secondSubmission]);
});

test("queued old cleanup cannot delete another tab's newer recovery record", async () => {
  const storage = memoryStorage(),
    locks = queuedLocks();
  const original = request(),
    next = request();
  next.payload.request_key = "newer-tab-request-key";
  persistPendingAdjustment(storage, original);
  await withAdjustmentRecoveryLock(locks, "admin-a", () =>
    clearPendingAdjustment(storage, original),
  );
  const results = await Promise.allSettled([
    withAdjustmentRecoveryLock(locks, "admin-a", () =>
      persistPendingAdjustment(storage, next),
    ),
    withAdjustmentRecoveryLock(locks, "admin-a", () =>
      clearPendingAdjustment(storage, original),
    ),
  ]);
  assert.deepEqual(
    results.map((result) => result.status),
    ["fulfilled", "rejected"],
  );
  assert.deepEqual(readPendingAdjustment(storage, "admin-a"), next);
});

test("settled cleanup is synchronous-busy protected and never sends another adjustment", async () => {
  const storage = memoryStorage();
  let removals = 0;
  const remove = storage.removeItem;
  storage.removeItem = (key) => {
    removals++;
    remove(key);
  };
  const harness = actionHarness({ storage, settled: "success" });
  await Promise.all([harness.submit(), harness.submit()]);
  assert.equal(harness.sends.length, 0);
  assert.equal(removals, 1);
  assert.equal(readPendingAdjustment(storage, "admin-a"), null);
});

test("settled cleanup preserves a different request installed by another tab", async () => {
  const harness = actionHarness({ settled: "success" });
  const next = request();
  next.payload.request_key = "a-newer-tab-request-key";
  clearPendingAdjustment(harness.storage, request());
  persistPendingAdjustment(harness.storage, next);
  await harness.submit();
  assert.equal(harness.sends.length, 0);
  assert.equal(harness.state.Settled, "success");
  assert.ok(harness.state.StorageError);
  assert.deepEqual(readPendingAdjustment(harness.storage, "admin-a"), next);
});

test("lost acknowledgement survives a recreated page with the exact original debit and key", () => {
  const storage = memoryStorage();
  const original = request();
  persistPendingAdjustment(storage, original);
  const restored = readPendingAdjustment(storage, "admin-a");
  assert.notEqual(restored, original);
  assert.deepEqual(restored, original);
  persistPendingAdjustment(storage, restored);
  assert.deepEqual(
    readPendingAdjustment(storage, "admin-a").payload,
    original.payload,
  );
});

test("different administrators have isolated pending requests and draft cleanup retains them", () => {
  const storage = memoryStorage();
  persistPendingAdjustment(storage, request());
  assert.equal(readPendingAdjustment(storage, "admin-b"), null);
  const other = request();
  other.admin_id = "admin-b";
  other.payload.request_key = "other-admin-request-key";
  persistPendingAdjustment(storage, other);
  storage.setItem("frame:draft:admin-a:project", "draft");
  for (const key of storage.values.keys())
    if (key.startsWith("frame:draft:admin-a:")) storage.removeItem(key);
  assert.deepEqual(readPendingAdjustment(storage, "admin-a"), request());
  clearPendingAdjustment(storage, request());
  assert.deepEqual(readPendingAdjustment(storage, "admin-b"), other);
});

test("an existing request cannot be replaced or cleared with a changed payload", () => {
  const storage = memoryStorage();
  persistPendingAdjustment(storage, request());
  for (const change of [
    { amount: 29 },
    { reason: "another reason" },
    { request_key: "a-brand-new-request-key" },
    { user_id: "user-b" },
  ]) {
    const changed = request();
    Object.assign(changed.payload, change);
    if (change.user_id) changed.target.id = change.user_id;
    assert.throws(() => persistPendingAdjustment(storage, changed));
    assert.throws(() => clearPendingAdjustment(storage, changed));
    assert.deepEqual(readPendingAdjustment(storage, "admin-a"), request());
  }
});

test("corrupt, owner-mismatched and target-mismatched records block a new request without deletion", () => {
  for (const raw of [
    "not-json",
    JSON.stringify({ ...request(), admin_id: "admin-b" }),
    JSON.stringify({
      ...request(),
      target: { ...request().target, id: "other" },
    }),
    JSON.stringify({
      ...request(),
      payload: { ...request().payload, amount: 0 },
    }),
  ]) {
    const storage = memoryStorage();
    persistPendingAdjustment(storage, request());
    const key = [...storage.values.keys()][0];
    storage.setItem(key, raw);
    assert.throws(() => readPendingAdjustment(storage, "admin-a"));
    assert.throws(() => persistPendingAdjustment(storage, request()));
    assert.equal(storage.getItem(key), raw);
  }
});

test("unavailable writes, reads and unverifiable persistence stop before a send", () => {
  for (const storage of [
    {
      ...memoryStorage(),
      setItem() {
        throw new Error("quota");
      },
    },
    {
      ...memoryStorage(),
      getItem() {
        throw new Error("blocked");
      },
    },
    { ...memoryStorage(), setItem() {} },
  ]) {
    let sends = 0;
    assert.throws(() => {
      persistPendingAdjustment(storage, request());
      sends++;
    });
    assert.equal(sends, 0);
  }
});

test("only a first-attempt pre-transaction validation refusal can clear the request", () => {
  for (const [status, code] of [
    [0, "network_error"],
    [200, "invalid_response"],
    [400, "request_failed"],
    [401, "unauthorized"],
    [403, "forbidden"],
    [404, "not_found"],
    [409, "conflict"],
    [422, "request_failed"],
    [429, "rate_limited"],
    [500, "database_error"],
    [502, "request_failed"],
    [503, "request_failed"],
  ]) {
    const storage = memoryStorage();
    persistPendingAdjustment(storage, request());
    assert.equal(adjustmentWasRejected(status, code, false), false);
    if (adjustmentWasRejected(status, code, false))
      clearPendingAdjustment(storage, request());
    assert.deepEqual(readPendingAdjustment(storage, "admin-a"), request());
  }
  for (const code of ["invalid_input", "invalid_json"]) {
    const storage = memoryStorage();
    persistPendingAdjustment(storage, request());
    assert.equal(adjustmentWasRejected(422, code, false), true);
    clearPendingAdjustment(storage, request());
    assert.equal(readPendingAdjustment(storage, "admin-a"), null);
  }
});

test("500 followed by a rejected retry and sign-in retains the exact original request", () => {
  for (const [status, code] of [
    [400, "request_failed"],
    [401, "unauthorized"],
    [403, "forbidden"],
    [404, "not_found"],
    [409, "conflict"],
    [422, "invalid_input"],
    [422, "invalid_json"],
    [429, "rate_limited"],
  ]) {
    const storage = memoryStorage();
    const original = request();
    persistPendingAdjustment(storage, original);
    assert.equal(adjustmentWasRejected(500, "database_error", false), false);
    const recovered = readPendingAdjustment(storage, "admin-a");
    persistPendingAdjustment(storage, recovered);
    assert.equal(adjustmentWasRejected(status, code, true), false);
    if (adjustmentWasRejected(status, code, true))
      clearPendingAdjustment(storage, recovered);
    const afterSignIn = readPendingAdjustment(storage, "admin-a");
    assert.deepEqual(afterSignIn, original);
    assert.equal(afterSignIn.payload.request_key, original.payload.request_key);
  }
});

test("a code-ok response clears recovery only when its data is a valid Wallet", () => {
  const valid = { available: 100, frozen: 29, demo: false };
  for (const data of [
    undefined,
    null,
    {},
    [],
    "wallet",
    { available: 100, frozen: 29 },
    { available: 100, demo: false },
    { frozen: 29, demo: false },
    ...["100", -1, 0.5, NaN, Infinity, Number.MAX_SAFE_INTEGER + 1].flatMap(
      (value) => [
        { ...valid, available: value },
        { ...valid, frozen: value },
      ],
    ),
    { ...valid, demo: "false" },
    { ...valid, demo: null },
  ]) {
    const storage = memoryStorage();
    persistPendingAdjustment(storage, request());
    assert.equal(adjustmentHasValidWallet(data), false);
    if (adjustmentHasValidWallet(data))
      clearPendingAdjustment(storage, request());
    assert.deepEqual(readPendingAdjustment(storage, "admin-a"), request());
  }
  for (const data of [valid, { available: 0, frozen: 0, demo: true }]) {
    const storage = memoryStorage();
    persistPendingAdjustment(storage, request());
    assert.equal(adjustmentHasValidWallet(data), true);
    if (adjustmentHasValidWallet(data))
      clearPendingAdjustment(storage, request());
    assert.equal(readPendingAdjustment(storage, "admin-a"), null);
  }
});

test("failed cleanup keeps the original record, and successful cleanup permits a new request", () => {
  for (const removeItem of [
    () => {
      throw new Error("unavailable");
    },
    () => {},
  ]) {
    const storage = memoryStorage();
    persistPendingAdjustment(storage, request());
    assert.throws(() =>
      clearPendingAdjustment({ ...storage, removeItem }, request()),
    );
    assert.deepEqual(readPendingAdjustment(storage, "admin-a"), request());
  }
  const storage = memoryStorage();
  persistPendingAdjustment(storage, request());
  clearPendingAdjustment(storage, request());
  assert.equal(readPendingAdjustment(storage, "admin-a"), null);
  const next = request();
  next.payload.request_key = "new-request-after-confirmed-cleanup";
  persistPendingAdjustment(storage, next);
  assert.deepEqual(readPendingAdjustment(storage, "admin-a"), next);
});
