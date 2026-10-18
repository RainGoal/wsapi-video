import assert from "node:assert/strict";
import fs from "node:fs";
import { test } from "node:test";
import ts from "typescript";

const source = fs
  .readFileSync(new URL("../src/api.ts", import.meta.url), "utf8")
  .replace('import { t } from "./i18n";', "const t = (text) => text;");
const compiled = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.ESNext,
    target: ts.ScriptTarget.ES2022,
  },
}).outputText;
const {
  api,
  ApiError,
  csrfToken,
  setCSRF,
  sessionVersion,
  isCurrentSession,
  waitForSessionChanges,
} = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
);
function deferred() {
  let resolve;
  const promise = new Promise((done) => (resolve = done));
  return { promise, resolve };
}
function response(status, data = null) {
  return {
    status,
    ok: status === 200,
    json: async () =>
      status === 200
        ? { code: "ok", data }
        : { code: "unauthorized", message: "expired" },
  };
}
function fixture(context, handler) {
  const oldFetch = globalThis.fetch;
  const oldWindow = globalThis.window;
  const browser = new EventTarget();
  const events = [];
  browser.addEventListener("frame:unauthorized", () => events.push("expired"));
  globalThis.fetch = handler;
  globalThis.window = browser;
  context.after(() => {
    globalThis.fetch = oldFetch;
    if (oldWindow === undefined) delete globalThis.window;
    else globalThis.window = oldWindow;
  });
  return { browser, events };
}
const unauthorized = (error) =>
  error instanceof ApiError && error.status === 401;

test("a delayed old 401 cannot invalidate a newly signed-in session", async (context) => {
  const pending = deferred();
  const { events } = fixture(context, () => pending.promise);
  setCSRF("account-a");
  const original = sessionVersion();
  const request = api("/wallet");
  setCSRF("account-b");
  pending.resolve(response(401));
  await assert.rejects(request, unauthorized);
  assert.equal(isCurrentSession(original), false);
  assert.equal(csrfToken(), "account-b");
  assert.deepEqual(events, []);
});

test("the boundary includes delayed response parsing and re-login with the same token", async (context) => {
  const parsed = deferred();
  const started = deferred();
  const { events } = fixture(context, async () => ({
    ...response(401),
    json: () => {
      started.resolve();
      return parsed.promise;
    },
  }));
  setCSRF("same-token");
  const request = api("/tasks");
  await started.promise;
  setCSRF("");
  setCSRF("same-token");
  parsed.resolve({ code: "unauthorized" });
  await assert.rejects(request, unauthorized);
  assert.equal(csrfToken(), "same-token");
  assert.deepEqual(events, []);
});

test("current protected auth requests also invalidate an expired session", async (context) => {
  const { events } = fixture(context, async () => response(401));
  setCSRF("current");
  for (const path of ["/wallet", "/auth/sessions", "/auth/change-password"])
    await assert.rejects(api(path), unauthorized);
  assert.equal(events.length, 3);
});

test("ending a session makes simultaneous old 401s harmless", async (context) => {
  const first = deferred();
  const second = deferred();
  const pending = [first, second];
  const { browser, events } = fixture(context, () => pending.shift().promise);
  browser.addEventListener("frame:unauthorized", () => setCSRF(""));
  setCSRF("current");
  const a = api("/tasks");
  const b = api("/assets");
  first.resolve(response(401));
  await assert.rejects(a, unauthorized);
  second.resolve(response(401));
  await assert.rejects(b, unauthorized);
  assert.equal(events.length, 1);
  assert.equal(csrfToken(), "");
});

test("public auth refusals, bootstrap and explicit logout leave lifecycle to their caller", async (context) => {
  const { events } = fixture(context, async () => response(401));
  for (const path of [
    "/me",
    "/auth/login",
    "/auth/register",
    "/auth/verify",
    "/auth/resend-verification",
    "/auth/forgot-password",
    "/auth/reset-password",
    "/auth/logout",
  ])
    await assert.rejects(api(path, "POST", {}), unauthorized);
  assert.deepEqual(events, []);
});

test("each request sends the CSRF token from its own session", async (context) => {
  const calls = [];
  fixture(context, async (url, options) => {
    calls.push({ url, options });
    return response(200, { accepted: true });
  });
  setCSRF("first");
  await api("/quotes", "POST", { prompt: "one" });
  setCSRF("second");
  await api("/quotes", "POST", { prompt: "two" });
  setCSRF("");
  await api("/auth/login", "POST", {});
  assert.equal(calls[0].options.headers["X-CSRF-Token"], "first");
  assert.equal(calls[1].options.headers["X-CSRF-Token"], "second");
  assert.equal(calls[2].options.headers["X-CSRF-Token"], undefined);
  assert.equal(calls[0].options.credentials, "same-origin");
});

test("network failures and malformed success responses do not invalidate identity", async (context) => {
  const { events } = fixture(context, async () => {
    throw new Error("offline");
  });
  setCSRF("current");
  await assert.rejects(
    api("/wallet"),
    (error) => error.code === "network_error",
  );
  globalThis.fetch = async () => ({
    ...response(200),
    json: async () => {
      throw new Error("invalid JSON");
    },
  });
  await assert.rejects(
    api("/wallet"),
    (error) => error.code === "invalid_response",
  );
  assert.equal(csrfToken(), "current");
  assert.deepEqual(events, []);
});

test("a new session waits for every earlier cookie-changing response, including failures", async (context) => {
  const logout = deferred();
  const password = deferred();
  const pending = [logout, password];
  fixture(context, () => pending.shift().promise);
  const out = api("/auth/logout", "POST", {});
  const changed = api("/auth/change-password", "POST", {});
  const refused = assert.rejects(out, unauthorized);
  let released = false;
  const waiting = waitForSessionChanges().then(() => {
    released = true;
  });
  await new Promise(setImmediate);
  assert.equal(released, false);
  logout.resolve(response(401));
  await refused;
  await new Promise(setImmediate);
  assert.equal(released, false);
  password.resolve(response(200));
  await changed;
  await waiting;
  assert.equal(released, true);
});

test("the cookie barrier rechecks requests added while waiting and releases network errors", async (context) => {
  const first = deferred();
  const added = deferred();
  const pending = [first, added];
  fixture(context, () => pending.shift().promise);
  const out = api("/auth/logout", "POST", {});
  let released = false;
  const waiting = waitForSessionChanges().then(() => {
    released = true;
  });
  const revoke = api("/auth/sessions/current", "DELETE");
  first.resolve(response(200));
  await out;
  await new Promise(setImmediate);
  assert.equal(released, false);
  added.resolve(response(200));
  await revoke;
  await waiting;
  globalThis.fetch = async () => {
    throw new Error("offline");
  };
  await assert.rejects(
    api("/auth/login", "POST", {}),
    (error) => error.code === "network_error",
  );
  await waitForSessionChanges();
});
