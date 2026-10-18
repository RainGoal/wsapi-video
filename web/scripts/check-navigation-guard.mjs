import assert from "node:assert/strict";
import fs from "node:fs";
import { test } from "node:test";
import ts from "typescript";

const source = fs.readFileSync(
  new URL("../src/navigation-guard.ts", import.meta.url),
  "utf8",
);
const compiled = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.ESNext,
    target: ts.ScriptTarget.ES2022,
  },
}).outputText;
const { createNavigationGuard } = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
);

// The browser changes its location before traversal events; go() is asynchronous.
function fixture(start = "/#/projects") {
  const entries = [
    { href: new URL(start, "https://studio.test").href, state: null },
  ];
  let position = 0;
  const jobs = [];
  const browser = new EventTarget();
  browser.location = {
    get href() {
      return entries[position].href;
    },
  };
  browser.history = {
    get state() {
      return structuredClone(entries[position].state);
    },
    replaceState(state, unused, url) {
      entries[position] = {
        href: new URL(url ?? browser.location.href, browser.location.href).href,
        state: structuredClone(state),
      };
    },
    pushState(state, unused, url) {
      entries.splice(position + 1);
      entries.push({
        href: new URL(url, browser.location.href).href,
        state: structuredClone(state),
      });
      position += 1;
    },
    go(delta) {
      assert.notEqual(delta, 0, "A cancellation must not reload the document");
      const target = entries[position + delta];
      if (!target) return;
      jobs.push(() => {
        const old = browser.location.href;
        position = entries.indexOf(target);
        browser.dispatchEvent(new Event("popstate"));
        if (new URL(old).hash !== new URL(browser.location.href).hash)
          browser.dispatchEvent(new Event("hashchange"));
      });
    },
  };
  return {
    browser,
    entries,
    nativeHash(hash, copyState = false) {
      const target = new URL(browser.location.href);
      target.hash = hash;
      browser.history.pushState(
        copyState ? browser.history.state : null,
        "",
        target,
      );
      jobs.push(() => {
        browser.dispatchEvent(new Event("popstate"));
        browser.dispatchEvent(new Event("hashchange"));
      });
    },
    step() {
      jobs.shift()?.();
    },
    flush() {
      let count = 0;
      while (jobs.length) {
        assert.ok(count++ < 20, "History restoration must terminate");
        jobs.shift()();
      }
    },
  };
}

function guarded(start) {
  const f = fixture(start);
  const changed = [];
  let allowed = true;
  let prompts = 0;
  const guard = createNavigationGuard(
    f.browser,
    () => {
      prompts += 1;
      return allowed;
    },
    () => changed.push(f.browser.location.href),
  );
  return {
    ...f,
    guard,
    changed,
    allow(value) {
      allowed = value;
    },
    get prompts() {
      return prompts;
    },
  };
}

test("programmatic navigation confirms once and rejected navigation changes nothing", () => {
  const f = guarded();
  f.guard.navigate("/admin/settings");
  assert.equal(f.prompts, 1);
  assert.equal(f.changed.length, 1);
  f.browser.dispatchEvent(new Event("hashchange"));
  f.browser.dispatchEvent(new Event("popstate"));
  assert.equal(f.prompts, 1);
  const before = structuredClone(f.entries);
  f.allow(false);
  f.guard.navigate("/projects");
  assert.deepEqual(f.entries, before);
  assert.equal(f.changed.length, 1);
  assert.equal(f.prompts, 2);
  f.guard.navigate("/admin/settings");
  assert.equal(f.prompts, 2, "The current URL is not a navigation");
});

test("cancelled Back restores the original URL without losing either history entry", () => {
  const f = guarded();
  f.guard.navigate("/admin/users");
  f.guard.navigate("/admin/settings");
  const before = structuredClone(f.entries);
  f.allow(false);
  f.browser.history.go(-1);
  f.flush();
  assert.deepEqual(f.entries, before);
  assert.ok(f.browser.location.href.endsWith("#/admin/settings"));
  assert.equal(f.changed.length, 2, "The rejected route never reaches React");
  assert.equal(
    f.prompts,
    3,
    "popstate/hashchange and rollback must not reprompt",
  );
  f.allow(true);
  f.browser.history.go(-1);
  f.flush();
  assert.ok(f.changed.at(-1).endsWith("#/admin/users"));
  f.browser.history.go(1);
  f.flush();
  assert.ok(f.changed.at(-1).endsWith("#/admin/settings"));
});

test("cancelled Forward preserves its destination and an existing forward tail", () => {
  const f = guarded();
  for (const route of ["/admin/users", "/admin/settings", "/tasks"])
    f.guard.navigate(route);
  f.browser.history.go(-2);
  f.flush();
  const before = structuredClone(f.entries);
  const prompts = f.prompts;
  f.allow(false);
  f.browser.history.go(1);
  f.flush();
  assert.ok(f.browser.location.href.endsWith("#/admin/users"));
  assert.deepEqual(f.entries, before);
  assert.equal(f.prompts, prompts + 1);
  f.allow(true);
  f.browser.history.go(2);
  f.flush();
  assert.ok(f.changed.at(-1).endsWith("#/tasks"));
});

test("multi-entry traversal cancellation restores the exact original position", () => {
  const f = guarded();
  for (const route of ["/tasks", "/wallet", "/admin/settings"])
    f.guard.navigate(route);
  const before = structuredClone(f.entries);
  f.allow(false);
  f.browser.history.go(-3);
  f.flush();
  assert.deepEqual(f.entries, before);
  assert.ok(f.browser.location.href.endsWith("#/admin/settings"));
  assert.equal(f.changed.length, 3);
  assert.equal(f.prompts, 4);
});

for (const copiedState of [false, true])
  test(`native hash navigation is guarded once, including copied state=${copiedState}`, () => {
    const f = guarded("/#/admin/settings");
    f.allow(false);
    f.nativeHash("/projects", copiedState);
    f.flush();
    assert.ok(f.browser.location.href.endsWith("#/admin/settings"));
    assert.equal(f.changed.length, 0);
    assert.equal(f.prompts, 1);
    assert.equal(f.entries.length, 2, "The rejected destination is retained");
    f.allow(true);
    f.browser.history.go(1);
    f.flush();
    assert.ok(f.changed.at(-1).endsWith("#/projects"));
    assert.equal(f.prompts, 2);
    f.nativeHash("/admin/users");
    f.flush();
    assert.equal(f.prompts, 3);
    assert.equal(f.changed.length, 2);
  });

test("payment return cleanup does not navigate or resurrect the consumed query", () => {
  const f = guarded("/wallet?out_trade_no=paid-once&keep=1#/wallet");
  f.browser.history.replaceState(
    { application: "retained" },
    "",
    "/wallet?keep=1#/wallet",
  );
  assert.equal(f.changed.length, 0);
  assert.equal(f.prompts, 0);
  assert.equal(f.browser.history.state.application, "retained");
  f.guard.navigate("/admin/settings");
  f.allow(false);
  f.nativeHash("/projects");
  f.flush();
  assert.ok(f.browser.location.href.endsWith("?keep=1#/admin/settings"));
  assert.ok(!f.browser.location.href.includes("out_trade_no"));
  f.allow(true);
  f.browser.history.go(-1);
  f.flush();
  assert.ok(f.changed.at(-1).endsWith("?keep=1#/wallet"));
  assert.ok(f.entries.every((entry) => !entry.href.includes("out_trade_no")));
});

test("authentication replacement and StrictMode-style guard reinstallation retain history", () => {
  const f = fixture("/?token=one-use");
  const originalReplace = f.browser.history.replaceState;
  let firstChanges = 0;
  const first = createNavigationGuard(
    f.browser,
    () => true,
    () => firstChanges++,
  );
  f.browser.history.replaceState({}, "", "/");
  assert.equal(firstChanges, 0);
  first.navigate("/tasks");
  first.dispose();
  assert.equal(f.browser.history.replaceState, originalReplace);
  const changed = [];
  const second = createNavigationGuard(
    f.browser,
    () => true,
    () => changed.push(f.browser.location.href),
  );
  f.browser.history.go(-1);
  f.flush();
  assert.deepEqual(changed, ["https://studio.test/"]);
  assert.equal(firstChanges, 1);
  second.dispose();
});

test("rollback ignores duplicate events and new navigation until the original entry returns", () => {
  const f = guarded();
  f.guard.navigate("/admin/users");
  f.guard.navigate("/admin/settings");
  f.allow(false);
  f.browser.history.go(-1);
  f.step();
  assert.ok(f.browser.location.href.endsWith("#/admin/users"));
  assert.equal(f.changed.length, 2);
  f.guard.navigate("/projects");
  f.browser.dispatchEvent(new Event("popstate"));
  assert.equal(f.prompts, 3);
  f.flush();
  assert.ok(f.browser.location.href.endsWith("#/admin/settings"));
  assert.equal(f.changed.length, 2);
  f.allow(true);
  f.guard.navigate("/projects");
  assert.ok(f.changed.at(-1).endsWith("#/projects"));
});

test("hashchange before popstate also confirms only once", () => {
  const f = guarded("/#/admin/settings");
  f.allow(false);
  f.browser.history.pushState(null, "", "https://studio.test/#/projects");
  f.browser.dispatchEvent(new Event("hashchange"));
  f.browser.dispatchEvent(new Event("popstate"));
  f.flush();
  assert.ok(f.browser.location.href.endsWith("#/admin/settings"));
  assert.equal(f.changed.length, 0);
  assert.equal(f.prompts, 1);
});
