import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import ts from "typescript";

async function loadActions(file, componentName, names, bindings) {
  const source = ts.createSourceFile(
    file,
    fs.readFileSync(new URL(`../src/${file}`, import.meta.url), "utf8"),
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.TSX,
  );
  const component = source.statements.find(
    (node) =>
      ts.isFunctionDeclaration(node) && node.name?.text === componentName,
  );
  const actions = [];
  for (const statement of component.body.statements) {
    if (
      ts.isFunctionDeclaration(statement) &&
      names.includes(statement.name?.text)
    )
      actions.push(statement.getText(source));
    if (ts.isVariableStatement(statement))
      for (const declaration of statement.declarationList.declarations)
        if (
          ts.isIdentifier(declaration.name) &&
          names.includes(declaration.name.text)
        )
          actions.push(`const ${declaration.getText(source)};`);
  }
  const compiled = ts.transpileModule(
    `export function bind(env) {
      const { ${bindings.join(",")} } = env;
      ${actions.join("\n")}
      return { ${names.join(",")} };
    }`,
    {
      compilerOptions: {
        module: ts.ModuleKind.ESNext,
        target: ts.ScriptTarget.ES2022,
      },
    },
  ).outputText;
  const loaded = await import(
    `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
  );
  return loaded.bind;
}
const sharedBindings = [
  "api",
  "busyRef",
  "live",
  "FormData",
  "sessionVersion",
  "isCurrentSession",
  "setBusy",
  "setError",
  "errorMessage",
  "window",
  "translate",
  "t",
];
const bindPassword = await loadActions(
  "pages.tsx",
  "AccountPage",
  ["password"],
  [...sharedBindings, "endSession"],
);
const bindWallet = await loadActions(
  "pages.tsx",
  "WalletPage",
  ["createOrder", "check", "closeCheckout", "refresh"],
  [
    ...sharedBindings,
    "selectedTier",
    "method",
    "orderBusy",
    "checkBusy",
    "orders",
    "ledger",
    "wallet",
    "returnedOrder",
    "refreshWallet",
    "notify",
    "setSelectedTier",
    "setTab",
    "setCheckingOrderId",
  ],
);
const bindAuth = await loadActions(
  "Auth.tsx",
  "Auth",
  ["submit", "change"],
  [
    ...sharedBindings,
    "params",
    "mode",
    "setMode",
    "setMessage",
    "setDevURL",
    "onLogin",
    "waitForSessionChanges",
  ],
);
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
function harness(bind, options = {}) {
  const effects = [],
    sends = [];
  let version = 40;
  const env = {
    live: { current: true },
    busyRef: { current: false },
    orderBusy: { current: false },
    checkBusy: { current: false },
    selectedTier: { id: "tier-a" },
    method: "alipay",
    mode: options.mode || "login",
    params: new URLSearchParams(),
    sessionVersion: () => version,
    waitForSessionChanges:
      options.waitForSessionChanges || (() => Promise.resolve()),
    isCurrentSession: (expected) => expected === version,
    api: async (...args) => {
      sends.push(args);
      return options.api?.(...args);
    },
    FormData: class {
      constructor(form) {
        this.form = form;
      }
      get(name) {
        return this.form[name];
      }
    },
    window: {
      location: { assign: (url) => effects.push(["assign", url]) },
      history: {
        replaceState: (...args) => effects.push(["replaceState", ...args]),
      },
    },
    t: (text) => text,
    translate: (text) => text,
    errorMessage: (error) => error.message,
    notify: (...args) => effects.push(["notify", ...args]),
    onLogin: (identity) => effects.push(["onLogin", identity]),
    endSession: (expected, message) => {
      effects.push(["endSession", expected, message]);
      if (expected === version) version++;
    },
    refreshWallet: () => effects.push(["refreshWallet"]),
  };
  for (const name of ["wallet", "ledger", "orders", "returnedOrder"])
    env[name] = { refresh: () => effects.push([`${name}.refresh`]) };
  for (const field of [
    "busy",
    "error",
    "selectedTier",
    "tab",
    "checkingOrderId",
    "mode",
    "message",
    "devURL",
  ])
    env[`set${field[0].toUpperCase()}${field.slice(1)}`] = (value) => {
      env[field] = value;
      effects.push([`set:${field}`, value]);
    };
  const event = () => ({
    preventDefault() {},
    currentTarget: {
      email: "creator@example.test",
      current_password: "old-password",
      password: "new-password",
      reset: () => effects.push(["reset"]),
    },
  });
  return {
    env,
    effects,
    sends,
    event,
    actions: () => bind(env),
    switchSession: () => {
      version++;
    },
  };
}
const identity = { user: { id: "user-a" }, csrf_token: "fixture-token" };
const checkout = {
  id: "order-a",
  checkout_url: "https://cashier.example.test/order-a",
};
const flush = async () => {
  for (let index = 0; index < 8; index++) await Promise.resolve();
};

test("password submission is synchronously protected against double clicks", async () => {
  const response = deferred();
  const h = harness(bindPassword, { api: () => response.promise });
  const actions = h.actions();
  const first = actions.password(h.event()),
    second = actions.password(h.event());
  assert.equal(h.sends.length, 1);
  response.resolve();
  await Promise.all([first, second]);
  assert.equal(h.effects.filter(([name]) => name === "endSession").length, 1);
  assert.deepEqual(h.sends[0], [
    "/auth/change-password",
    "POST",
    {
      current_password: "old-password",
      password: "new-password",
    },
  ]);
});

test("password failure unlocks the form for an explicit retry", async () => {
  const response = deferred();
  const h = harness(bindPassword, { api: () => response.promise });
  const pending = h.actions().password(h.event());
  response.reject(new Error("Password rejected"));
  await pending;
  assert.equal(h.env.error, "Password rejected");
  assert.equal(h.env.busy, false);
  assert.equal(h.env.busyRef.current, false);
  await h.actions().password(h.event());
  assert.equal(h.sends.length, 2);
});

test("successful password change ends its original session after page unmount", async () => {
  const response = deferred();
  const h = harness(bindPassword, { api: () => response.promise });
  const pending = h.actions().password(h.event());
  h.env.live.current = false;
  const before = [...h.effects];
  response.resolve();
  await pending;
  assert.deepEqual(h.effects.slice(before.length), [
    ["endSession", 40, "密码已修改，请重新登录。"],
  ]);
  assert.equal(h.env.busyRef.current, false);
});

test("an old password success cannot end or reset a newer authenticated session", async () => {
  const response = deferred();
  const h = harness(bindPassword, { api: () => response.promise });
  const pending = h.actions().password(h.event());
  h.switchSession();
  const before = [...h.effects];
  response.resolve();
  await pending;
  assert.deepEqual(h.effects, before);
});

test("checkout creation blocks duplicate sends and immediate closure while pending", async () => {
  const response = deferred();
  const h = harness(bindWallet, { api: () => response.promise });
  const actions = h.actions(),
    target = h.env.selectedTier;
  const first = actions.createOrder(),
    second = actions.createOrder();
  actions.closeCheckout();
  assert.equal(h.env.selectedTier, target);
  assert.equal(h.sends.length, 1);
  assert.deepEqual(h.sends[0], [
    "/payment/orders",
    "POST",
    { tier_id: "tier-a", method: "alipay" },
  ]);
  response.resolve(checkout);
  await Promise.all([first, second]);
  assert.equal(h.env.selectedTier, null);
  assert.equal(h.effects.filter(([name]) => name === "assign").length, 1);
  assert.equal(h.env.busy, false);
});

test("checkout failure preserves its tier and unlocks retry and cancellation", async () => {
  const response = deferred();
  const h = harness(bindWallet, { api: () => response.promise });
  const pending = h.actions().createOrder();
  response.reject(new Error("Payment unavailable"));
  await pending;
  assert.equal(h.env.error, "Payment unavailable");
  assert.equal(h.env.busy, false);
  assert.equal(h.env.orderBusy.current, false);
  assert.equal(h.env.selectedTier.id, "tier-a");
  await h.actions().createOrder();
  assert.equal(h.sends.length, 2);
  h.actions().closeCheckout();
  assert.equal(h.env.selectedTier, null);
});

for (const circumstance of ["unmounted", "new session"])
  test(`a checkout success after ${circumstance} cannot close a new dialog or redirect`, async () => {
    const response = deferred();
    const h = harness(bindWallet, { api: () => response.promise });
    const pending = h.actions().createOrder();
    if (circumstance === "unmounted") h.env.live.current = false;
    else h.switchSession();
    h.env.selectedTier = { id: "new-tier" };
    const before = [...h.effects];
    response.resolve(checkout);
    await pending;
    assert.deepEqual(h.effects, before);
    assert.equal(h.env.selectedTier.id, "new-tier");
  });

test("order checks block double clicks and refresh only on confirmed current success", async () => {
  const response = deferred();
  const h = harness(bindWallet, { api: () => response.promise });
  const actions = h.actions();
  const first = actions.check(checkout),
    second = actions.check(checkout);
  assert.equal(h.sends.length, 1);
  assert.equal(h.env.checkingOrderId, "order-a");
  assert.deepEqual(h.sends[0], ["/payment/orders/order-a/check", "POST", {}]);
  response.resolve();
  await Promise.all([first, second]);
  assert.equal(h.env.checkingOrderId, "");
  assert.equal(
    h.effects.filter(([name]) => name === "refreshWallet").length,
    1,
  );
});

test("a failed order check unlocks a later explicit check", async () => {
  const response = deferred();
  const h = harness(bindWallet, { api: () => response.promise });
  const pending = h.actions().check(checkout);
  response.reject(new Error("Check failed"));
  await pending;
  assert.equal(h.env.checkBusy.current, false);
  assert.equal(h.env.checkingOrderId, "");
  assert.deepEqual(
    h.effects.find(([name]) => name === "notify"),
    ["notify", "Check failed", true],
  );
  await h.actions().check(checkout);
  assert.equal(h.sends.length, 2);
});

for (const circumstance of ["unmounted", "new session"])
  test(`an order check after ${circumstance} cannot refresh or notify a new page`, async () => {
    const response = deferred();
    const h = harness(bindWallet, { api: () => response.promise });
    const pending = h.actions().check(checkout);
    if (circumstance === "unmounted") h.env.live.current = false;
    else h.switchSession();
    const before = [...h.effects];
    response.resolve();
    await pending;
    assert.deepEqual(h.effects, before);
  });

test("authentication synchronously blocks duplicate submits and pending mode changes", async () => {
  const response = deferred();
  const h = harness(bindAuth, { api: () => response.promise });
  const actions = h.actions();
  const first = actions.submit(h.event()),
    second = actions.submit(h.event());
  const before = [...h.effects];
  actions.change("register");
  assert.deepEqual(h.effects, before);
  assert.equal(h.env.mode, "login");
  await flush();
  assert.equal(h.sends.length, 1);
  response.resolve(identity);
  await Promise.all([first, second]);
  assert.deepEqual(
    h.effects.filter(([name]) => name === "onLogin"),
    [["onLogin", identity]],
  );
});

test("failed authentication unlocks both retry and mode changes", async () => {
  const response = deferred();
  const h = harness(bindAuth, { api: () => response.promise });
  const pending = h.actions().submit(h.event());
  await flush();
  response.reject(new Error("Login failed"));
  await pending;
  assert.equal(h.env.error, "Login failed");
  assert.equal(h.env.busy, false);
  assert.equal(h.env.busyRef.current, false);
  await h.actions().submit(h.event());
  assert.equal(h.sends.length, 2);
  h.actions().change("register");
  assert.equal(h.env.mode, "register");
});

for (const circumstance of ["unmounted", "new session"])
  test(`authentication success after ${circumstance} cannot call onLogin`, async () => {
    const response = deferred();
    const h = harness(bindAuth, { api: () => response.promise });
    const pending = h.actions().submit(h.event());
    await flush();
    assert.equal(h.sends.length, 1);
    if (circumstance === "unmounted") h.env.live.current = false;
    else h.switchSession();
    const before = [...h.effects];
    response.resolve(identity);
    await pending;
    assert.deepEqual(
      h.effects.slice(before.length),
      circumstance === "unmounted" ? [] : [["set:busy", false]],
    );
    assert.equal(h.env.busyRef.current, false);
    if (circumstance === "new session") assert.equal(h.env.busy, false);
  });

test("login waits for earlier HTTP session changes before sending credentials", async () => {
  const sessionChange = deferred(),
    response = deferred();
  const h = harness(bindAuth, {
    waitForSessionChanges: () => sessionChange.promise,
    api: () => response.promise,
  });
  const pending = h.actions().submit(h.event());
  await flush();
  assert.equal(h.sends.length, 0);
  sessionChange.resolve();
  await flush();
  assert.equal(h.sends.length, 1);
  assert.equal(h.sends[0][0], "/auth/login");
  response.resolve(identity);
  await pending;
  assert.deepEqual(
    h.effects.filter(([name]) => name === "onLogin"),
    [["onLogin", identity]],
  );
});

for (const circumstance of ["unmounted", "new session"])
  test(`login does not send after ${circumstance} while waiting for old session changes`, async () => {
    const sessionChange = deferred();
    const h = harness(bindAuth, {
      waitForSessionChanges: () => sessionChange.promise,
      api: () => identity,
    });
    const pending = h.actions().submit(h.event());
    if (circumstance === "unmounted") h.env.live.current = false;
    else h.switchSession();
    const before = [...h.effects];
    sessionChange.resolve();
    await pending;
    assert.equal(h.sends.length, 0);
    assert.deepEqual(
      h.effects.slice(before.length),
      circumstance === "unmounted" ? [] : [["set:busy", false]],
    );
    assert.equal(h.env.busyRef.current, false);
    if (circumstance === "new session") assert.equal(h.env.busy, false);
  });
