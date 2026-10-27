import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import ts from "typescript";

const source = ts.createSourceFile(
  "ForcePasswordChange.tsx",
  fs.readFileSync(
    new URL("../src/ForcePasswordChange.tsx", import.meta.url),
    "utf8",
  ),
  ts.ScriptTarget.Latest,
  true,
  ts.ScriptKind.TSX,
);
const component = source.statements.find(
  (node) =>
    ts.isFunctionDeclaration(node) && node.name?.text === "ForcePasswordChange",
);
const actions = component.body.statements
  .filter((node) => ts.isFunctionDeclaration(node))
  .map((node) => node.getText(source))
  .join("\n");
const compiled = ts.transpileModule(
  `export function bind(env) {
    const { api, busyRef, live, FormData, sessionVersion, isCurrentSession,
      setBusy, setError, errorMessage, endSession, logout, a } = env;
    ${actions}
    return {submit, signOut};
  }`,
  {
    compilerOptions: {
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2022,
    },
  },
).outputText;
const { bind } = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
);
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
function fixture(options = {}) {
  let version = 8;
  const effects = [],
    sends = [];
  const env = {
    live: { current: true },
    busyRef: { current: false },
    sessionVersion: () => version,
    isCurrentSession: (expected) => expected === version,
    api: (...args) => {
      sends.push(args);
      return options.api?.(...args);
    },
    logout: async () => {
      effects.push(["logout"]);
      return options.logout?.();
    },
    FormData: class {
      constructor(form) {
        this.form = form;
      }
      get(name) {
        return this.form[name];
      }
    },
    a: (zh) => zh,
    errorMessage: (error) => error.message,
    setBusy: (value) => {
      env.busy = value;
      effects.push(["busy", value]);
    },
    setError: (value) => {
      env.error = value;
      effects.push(["error", value]);
    },
    endSession: (...args) => effects.push(["endSession", ...args]),
  };
  const form = {
    current_password: "initial-password-123",
    password: "personal-password-456",
    confirm_password: "personal-password-456",
    reset: () => effects.push(["reset"]),
  };
  return {
    env,
    effects,
    sends,
    form,
    actions: bind(env),
    event: () => ({ preventDefault() {}, currentTarget: form }),
    switchSession: () => {
      version++;
    },
  };
}

test("new password validation preserves the form and prevents invalid submissions", async () => {
  for (const [password, confirmation] of [
    ["personal-password-456", "different-password-789"],
    ["initial-password-123", "initial-password-123"],
    ["short", "short"],
    ["密".repeat(25), "密".repeat(25)],
  ]) {
    const h = fixture();
    h.form.password = password;
    h.form.confirm_password = confirmation;
    await h.actions.submit(h.event());
    assert.equal(h.sends.length, 0);
    assert.ok(h.env.error);
    assert.equal(
      h.effects.some(([name]) => name === "reset"),
      false,
    );
  }
});

test("changing the initial password sends once and requires sign-in again", async () => {
  const response = deferred();
  const h = fixture({ api: () => response.promise });
  const first = h.actions.submit(h.event()),
    second = h.actions.submit(h.event());
  await h.actions.signOut();
  assert.equal(h.sends.length, 1);
  assert.equal(
    h.effects.some(([name]) => name === "logout"),
    false,
  );
  assert.deepEqual(h.sends[0], [
    "/auth/change-password",
    "POST",
    {
      current_password: h.form.current_password,
      password: h.form.password,
    },
  ]);
  response.resolve();
  await Promise.all([first, second]);
  assert.deepEqual(
    h.effects.filter(([name]) => name === "endSession"),
    [["endSession", 8, "密码已修改，请重新登录。"]],
  );
  assert.equal(h.effects.filter(([name]) => name === "reset").length, 1);
});

test("a failed password update keeps entered values available for retry", async () => {
  let attempts = 0;
  const h = fixture({
    api: async () => {
      if (attempts++ === 0) throw new Error("Current password rejected");
    },
  });
  await h.actions.submit(h.event());
  assert.equal(h.env.error, "Current password rejected");
  assert.equal(h.env.busy, false);
  assert.equal(h.env.busyRef.current, false);
  assert.equal(
    h.effects.some(([name]) => name === "reset"),
    false,
  );
  await h.actions.submit(h.event());
  assert.equal(h.sends.length, 2);
});

test("a late response cannot reset or sign out a newer session", async () => {
  const response = deferred();
  const h = fixture({ api: () => response.promise });
  const pending = h.actions.submit(h.event());
  h.switchSession();
  const before = [...h.effects];
  response.resolve();
  await pending;
  assert.deepEqual(h.effects, before);
});

test("a completed update still ends its original session after unmount", async () => {
  const response = deferred();
  const h = fixture({ api: () => response.promise });
  const pending = h.actions.submit(h.event());
  h.env.live.current = false;
  const before = h.effects.length;
  response.resolve();
  await pending;
  assert.deepEqual(h.effects.slice(before), [
    ["endSession", 8, "密码已修改，请重新登录。"],
  ]);
});

test("sign-out locks concurrent actions and exposes a retry after failure", async () => {
  const response = deferred();
  const h = fixture({ logout: () => response.promise });
  const first = h.actions.signOut(),
    second = h.actions.signOut();
  await h.actions.submit(h.event());
  assert.equal(h.effects.filter(([name]) => name === "logout").length, 1);
  assert.equal(h.sends.length, 0);
  response.reject(new Error("Sign-out temporarily unavailable"));
  await Promise.all([first, second]);
  assert.equal(h.env.error, "Sign-out temporarily unavailable");
  assert.equal(h.env.busy, false);
  assert.equal(h.env.busyRef.current, false);
});
