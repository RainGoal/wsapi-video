import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";

const compiled = ts.transpileModule(
  fs.readFileSync(
    new URL("../src/stores/use-theme-store.ts", import.meta.url),
    "utf8",
  ),
  {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
    },
  },
).outputText;
function harness(initial = "dark", storageUnavailable = false) {
  const values = new Map([["frame:canvas-theme", initial]]);
  const events = new Map();
  let subscription;
  const exports = {};
  vm.runInNewContext(compiled, {
    exports,
    require: () => ({
      useSyncExternalStore: (subscribe, get) => {
        subscription = subscribe;
        return get();
      },
    }),
    localStorage: {
      getItem: (key) => {
        if (storageUnavailable) throw Error("blocked");
        return values.get(key) ?? null;
      },
      setItem: (key, value) => {
        if (storageUnavailable) throw Error("blocked");
        values.set(key, value);
      },
    },
    window: {
      addEventListener: (name, fn) => events.set(name, fn),
      removeEventListener: (name) => events.delete(name),
    },
  });
  const read = () => exports.useThemeStore((state) => state);
  return {
    read,
    values,
    events,
    subscribe: (fn) => {
      read();
      return subscription(fn);
    },
  };
}

test("canvas theme restores and persists the personal preference", () => {
  const store = harness("light");
  assert.equal(store.read().theme, "light");
  const setTheme = store.read().setTheme;
  setTheme("dark");
  assert.equal(store.read().theme, "dark");
  assert.equal(store.values.get("frame:canvas-theme"), "dark");
  assert.equal(store.read().setTheme, setTheme);
});

test("canvas theme works when browser storage is unavailable", () => {
  const store = harness("light", true);
  assert.equal(store.read().theme, "dark");
  const unsubscribe = store.subscribe(() => {});
  store.read().setTheme("light");
  assert.equal(store.read().theme, "light");
  unsubscribe();
  store.subscribe(() => {})();
  assert.equal(store.read().theme, "light");
});

test("cross-tab theme updates notify once and stop after the final subscriber", () => {
  const store = harness();
  let notifications = 0;
  const unsubscribe = store.subscribe(() => notifications++);
  store.values.set("frame:canvas-theme", "light");
  store.events.get("storage")({ key: "unrelated" });
  assert.equal(store.read().theme, "dark");
  store.events.get("storage")({ key: "frame:canvas-theme" });
  assert.equal(store.read().theme, "light");
  assert.equal(notifications, 1);
  store.events.get("storage")({ key: "frame:canvas-theme" });
  assert.equal(notifications, 1);
  unsubscribe();
  assert.equal(store.events.size, 0);
  store.values.set("frame:canvas-theme", "dark");
  store.subscribe(() => {})();
  assert.equal(store.read().theme, "dark");
});
