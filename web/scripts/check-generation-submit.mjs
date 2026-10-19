import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import ts from "typescript";

async function loadModule(path) {
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
const recovery = await loadModule("../src/submission-recovery.ts");
const { validGenerationQuote } = await loadModule("../src/generation-quote.ts");
const { estimateProductPrice } = await loadModule("../src/pricing.ts");
const source = ts.createSourceFile(
  "Generate.tsx",
  fs.readFileSync(new URL("../src/Generate.tsx", import.meta.url), "utf8"),
  ts.ScriptTarget.Latest,
  true,
  ts.ScriptKind.TSX,
);
const component = source.statements.find(
  (node) => ts.isFunctionDeclaration(node) && node.name?.text === "Generate",
);
const actions = component.body.statements
  .filter(
    (node) =>
      ts.isFunctionDeclaration(node) &&
      ["submit", "finishConfirmed"].includes(node.name?.text),
  )
  .map((node) => node.getText(source))
  .join("\n");
const compiledActions = ts.transpileModule(
  `export function bindActions(env) {
    const { busyRef, settled, recoveryReady, storageError, recovery, actionDisabled,
      product, priceEstimate, context, latestQuoteInput, displayedCredits, live,
      projectId, prompt, ratio, duration, kind, resolution, size, quality, audio,
      references, startFrame, endFrame, quoteInput, node, localStorage, recoveryKey,
      navigator, window, crypto, api, ApiError, sessionVersion, onCreated,
      refreshWallet, t, errorMessage, storageFailure, reloadRecovery,
      setBusy, setError, setInsufficient, setQuote, setRecovery, setSettled,
      setStorageError, validGenerationQuote, completePendingSubmission,
      persistPendingSubmission, submissionHasValidTask, submissionWasRejected,
      withSubmissionRecoveryLock } = env;
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
  `data:text/javascript;base64,${Buffer.from(compiledActions).toString("base64")}`
);

class ApiError extends Error {
  constructor(status, code, message = code) {
    super(message);
    this.status = status;
    this.code = code;
  }
}
function storage() {
  const values = new Map();
  return {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: (key) => values.delete(key),
  };
}
function queuedLocks() {
  let pending = Promise.resolve();
  return {
    request(_key, action) {
      const next = pending.then(action);
      pending = next.catch(() => {});
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
async function flush() {
  for (let index = 0; index < 12; index++) await Promise.resolve();
}
const task = () => ({
  id: `task_${"a".repeat(32)}`,
  project_id: "project-a",
  product_id: "product-a",
  kind: "image",
  model: "model-a",
  prompt: "A quiet landscape",
  credits: 10,
  execution_status: "queued",
  delivery_status: "pending",
  settlement_status: "frozen",
  procurement_status: "pending",
  demo: false,
  asset_ids: [],
  input: {},
  created_at: "2026-10-19T00:00:00Z",
  updated_at: "2026-10-19T00:00:00Z",
});
function harness(options = {}) {
  let counter = 0;
  const sends = [],
    delivered = [],
    state = {};
  const localStorage = options.storage || storage();
  const product = options.product || {
    id: "product-a",
    kind: "image",
    version: 1,
    credits: 10,
    capabilities: {},
  };
  const duration = options.duration ?? 0,
    resolution = options.resolution ?? "";
  const env = {
    ...recovery,
    busyRef: { current: false },
    settled: null,
    recoveryReady: true,
    storageError: "",
    recovery: options.recovery || null,
    actionDisabled: false,
    product,
    priceEstimate: estimateProductPrice(product, duration, resolution),
    context: {
      current: { id: "node-a", userId: "user-a", projectId: "project-a" },
    },
    latestQuoteInput: { current: "original-input" },
    live: { current: true },
    quoteInput: { current: "" },
    node: { id: "node-a" },
    projectId: "project-a",
    prompt: "A quiet landscape",
    ratio: "16:9",
    duration,
    kind: product.kind,
    resolution,
    size: "1024x1024",
    quality: "high",
    audio: false,
    references: ["asset-a"],
    startFrame: "",
    endFrame: "",
    localStorage,
    recoveryKey: "frame:submission:user-a:project-a",
    navigator: {
      locks: options.locks === undefined ? queuedLocks() : options.locks,
    },
    window: { dispatchEvent() {} },
    crypto: { randomUUID: () => "fresh-request-key-123" },
    session: 1,
    now: Date.now(),
    sessionVersion: () => env.session,
    validGenerationQuote: (value, id) =>
      validGenerationQuote(value, id, env.now),
    ApiError,
    t: (text, ...args) =>
      text.replace(/\{(\d+)\}/g, (_, index) => String(args[index])),
    errorMessage: (error) => error.message,
    storageFailure: () => "storage unavailable",
    reloadRecovery() {},
    onCreated: (...args) => {
      delivered.push(args);
      return options.deliver !== false;
    },
    refreshWallet() {},
    api: async (path, method, body) => {
      sends.push({ path, method, body });
      if (path === "/quotes") {
        counter++;
        const quote = {
          id: `quote_${counter.toString(16).padStart(32, "0")}`,
          product_id: product.id,
          credits: options.credits ?? env.priceEstimate.total_credits,
          expires_at: new Date(env.now + 60_000).toISOString(),
          input_hash: "server-input-hash",
          demo: false,
        };
        return options.quote ? options.quote(quote, counter) : quote;
      }
      assert.deepEqual(
        recovery.readPendingSubmission(localStorage, env.recoveryKey),
        {
          quote_id: body.quote_id,
          request_key: body.request_key,
          source_node_id: options.recovery?.source_node_id || "node-a",
        },
        "every task send must have its exact durable recovery first",
      );
      return options.task ? options.task(body) : task();
    },
  };
  env.displayedCredits = env.priceEstimate.total_credits;
  for (const key of [
    "Busy",
    "Error",
    "Insufficient",
    "Recovery",
    "Settled",
    "StorageError",
  ])
    env[`set${key}`] = (value) => {
      state[key] = value;
      env[key[0].toLowerCase() + key.slice(1)] = value;
    };
  env.setQuote = (value) => {
    state.Quote = value;
    env.displayedCredits = value?.credits ?? env.priceEstimate.total_credits;
  };
  if (options.recovery)
    recovery.persistPendingSubmission(
      localStorage,
      env.recoveryKey,
      options.recovery,
    );
  return {
    env,
    state,
    sends,
    delivered,
    localStorage,
    actions: () => bindActions(env),
    submit: (retry) => bindActions(env).submit(retry),
    pending: () =>
      recovery.readPendingSubmission(localStorage, env.recoveryKey),
  };
}

test("one click obtains a fresh quote and submits once after durable recovery", async () => {
  const h = harness();
  await h.submit();
  assert.deepEqual(
    h.sends.map((send) => send.path),
    ["/quotes", "/tasks"],
  );
  assert.equal(h.pending(), null);
  assert.equal(h.delivered.length, 1);
  assert.equal(h.delivered[0][2], "node-a");
  assert.equal(h.env.busyRef.current, false);
});

test("the same rendered action holds its synchronous busy lock through quote and submission", async () => {
  const response = deferred();
  const h = harness({ quote: () => response.promise });
  const { submit } = h.actions();
  const first = submit(),
    second = submit();
  assert.equal(h.sends.length, 1);
  assert.equal(h.env.busyRef.current, true);
  response.resolve({
    id: `quote_${"a".repeat(32)}`,
    product_id: "product-a",
    credits: 10,
    expires_at: new Date(h.env.now + 60_000).toISOString(),
    input_hash: "hash",
    demo: false,
  });
  await Promise.all([first, second]);
  assert.deepEqual(
    h.sends.map((send) => send.path),
    ["/quotes", "/tasks"],
  );
});

test("price increases and decreases update the button and require one new click", async () => {
  for (const credits of [8, 15]) {
    const h = harness({ credits });
    await h.submit();
    assert.equal(h.sends.length, 1);
    assert.equal(h.env.displayedCredits, credits);
    assert.match(h.state.Error, /价格已更新/);
    assert.equal(h.pending(), null);
    h.env.now += 61_000;
    await h.submit();
    assert.deepEqual(
      h.sends.map((send) => send.path),
      ["/quotes", "/quotes", "/tasks"],
    );
    assert.equal(h.sends[2].body.quote_id, `quote_${"2".padStart(32, "0")}`);
    assert.equal(h.delivered.length, 1);
  }
});

test("invalid, wrong-product, unsafe and expired quotes never create tasks or recovery", async () => {
  for (const mutate of [
    () => null,
    (q) => ({ ...q, id: "bad" }),
    (q) => ({ ...q, product_id: "other" }),
    (q) => ({ ...q, credits: 0 }),
    (q) => ({ ...q, credits: 1.5 }),
    (q) => ({ ...q, credits: 1_000_000_001 }),
    (q) => ({ ...q, input_hash: "" }),
    (q) => ({ ...q, pricing: {} }),
    (q) => ({
      ...q,
      pricing: {
        billing_unit: "per_second",
        unit_credits: 2,
        quantity: 5,
        resolution: "1080p",
        total_credits: 11,
      },
    }),
    (q) => ({ ...q, expires_at: "invalid" }),
    (q) => ({ ...q, expires_at: new Date(0).toISOString() }),
  ]) {
    const h = harness({ quote: mutate });
    await h.submit();
    assert.equal(h.sends.length, 1);
    assert.equal(h.pending(), null);
    assert.match(h.state.Error, /有效生成价格/);
  }
});

test("closing, switching nodes, switching sessions or changing inputs during quote cancels before submission", async () => {
  for (const change of [
    (h) => {
      h.env.live.current = false;
    },
    (h) => {
      h.env.context.current = { ...h.env.context.current, id: "node-b" };
    },
    (h) => {
      h.env.session++;
    },
    (h) => {
      h.env.latestQuoteInput.current = "edited-input";
    },
  ]) {
    const response = deferred();
    const h = harness({ quote: (q) => response.promise.then(() => q) });
    const pending = h.submit();
    change(h);
    response.resolve();
    await pending;
    assert.equal(h.sends.length, 1);
    assert.equal(h.pending(), null);
    assert.equal(h.env.busyRef.current, false);
  }
});

test("input changes and quote expiry while waiting for the cross-tab lock prevent persistence", async () => {
  for (const change of [
    (h) => {
      h.env.latestQuoteInput.current = "edited-input";
    },
    (h) => {
      h.env.now += 61_000;
    },
  ]) {
    const gate = deferred();
    const h = harness({
      locks: { request: (_key, action) => gate.promise.then(action) },
    });
    const pending = h.submit();
    await flush();
    change(h);
    gate.resolve();
    await pending;
    assert.equal(h.sends.length, 1);
    assert.equal(h.pending(), null);
    assert.match(h.state.Error, /重新点击生成/);
  }
});

test("an input change immediately after persistence clears the known-unsent record", async () => {
  let h,
    calls = 0;
  const locks = {
    request: async (_key, action) => {
      action();
      if (++calls === 1) h.env.latestQuoteInput.current = "edited-input";
    },
  };
  h = harness({ locks });
  await h.submit();
  assert.equal(h.sends.length, 1);
  assert.equal(h.pending(), null);
  assert.match(h.state.Error, /重新点击生成/);
});

test("unknown task results retain the exact key and retry without obtaining another quote", async () => {
  for (const reason of [
    new ApiError(0, "network_error"),
    new ApiError(500, "database_error"),
    new ApiError(409, "conflict"),
    null,
  ]) {
    let attempts = 0;
    const h = harness({
      task: () => {
        if (++attempts === 1) {
          if (reason) throw reason;
          return {};
        }
        return task();
      },
    });
    await h.submit();
    const pending = h.pending();
    assert.ok(pending);
    await h.submit(pending);
    assert.deepEqual(
      h.sends.map((send) => send.path),
      ["/quotes", "/tasks", "/tasks"],
    );
    assert.deepEqual(h.sends[1].body, h.sends[2].body);
    assert.equal(h.pending(), null);
    assert.equal(h.delivered[0][1], true);
  }
});

test("definitive task refusals clear recovery without automatically creating another quote", async () => {
  for (const reason of [
    new ApiError(402, "insufficient_credits"),
    new ApiError(409, "quote_expired"),
    new ApiError(409, "product_unavailable"),
  ]) {
    const h = harness({
      task: () => {
        throw reason;
      },
    });
    await h.submit();
    assert.equal(h.sends.length, 2);
    assert.equal(h.pending(), null);
    assert.equal(h.delivered.length, 0);
  }
});

test("unavailable storage or Web Locks stops before any task send", async () => {
  for (const option of [
    { locks: null },
    {
      storage: {
        ...storage(),
        setItem() {
          throw new Error("quota");
        },
      },
    },
  ]) {
    const h = harness(option);
    await h.submit();
    assert.equal(h.sends.length, 1);
    assert.ok(h.state.StorageError);
    assert.equal(h.env.busyRef.current, false);
  }
});

test("concurrent tabs cannot submit two different fresh quotes", async () => {
  const response = deferred(),
    shared = storage(),
    locks = queuedLocks();
  const first = harness({
    storage: shared,
    locks,
    task: () => response.promise,
  });
  const second = harness({
    storage: shared,
    locks,
    quote: (q) => ({ ...q, id: `quote_${"b".repeat(32)}` }),
  });
  const sending = first.submit();
  await flush();
  await second.submit();
  assert.equal(first.sends.filter((send) => send.path === "/tasks").length, 1);
  assert.equal(second.sends.filter((send) => send.path === "/tasks").length, 0);
  response.resolve(task());
  await sending;
  assert.equal(first.pending(), null);
});

test("late accepted tasks remain recoverable when delivery or the original session is gone", async () => {
  for (const changeSession of [false, true]) {
    const response = deferred();
    const h = harness({ deliver: changeSession, task: () => response.promise });
    const sending = h.submit();
    await flush();
    if (changeSession) h.env.session++;
    response.resolve(task());
    await sending;
    assert.ok(h.pending());
    assert.equal(h.delivered.length, changeSession ? 0 : 1);
  }
});

test("closing the composer or switching nodes after task submission still delivers to the original node", async () => {
  for (const change of [
    (h) => {
      h.env.live.current = false;
    },
    (h) => {
      h.env.context.current = { ...h.env.context.current, id: "node-b" };
    },
  ]) {
    const response = deferred();
    const h = harness({ task: () => response.promise });
    const sending = h.submit();
    await flush();
    assert.ok(h.pending());
    change(h);
    response.resolve(task());
    await sending;
    assert.equal(h.pending(), null);
    assert.equal(h.delivered.length, 1);
    assert.equal(h.delivered[0][2], "node-a");
  }
});

test("per-second video pricing displays the selected duration and resolution total and submits the same input", async () => {
  const h = harness({
    product: {
      id: "product-a",
      kind: "video",
      version: 1,
      credits: 10,
      pricing: {
        billing_unit: "per_second",
        resolution_credits: { "1080p": 50 },
      },
      capabilities: {},
    },
    duration: 8,
    resolution: "1080p",
    quote: (q) => ({
      ...q,
      pricing: {
        billing_unit: "per_second",
        unit_credits: 50,
        quantity: 8,
        resolution: "1080p",
        total_credits: 400,
      },
    }),
  });
  assert.equal(h.env.displayedCredits, 400);
  await h.submit();
  assert.equal(h.sends.length, 2);
  assert.equal(h.sends[0].body.duration, 8);
  assert.equal(h.sends[0].body.resolution, "1080p");
});
