import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import ts from "typescript";

const compiled = ts.transpileModule(
  fs.readFileSync(
    new URL("../src/admin/settings-operation.ts", import.meta.url),
    "utf8",
  ),
  {
    compilerOptions: {
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2022,
    },
  },
).outputText;
const { createSettingsOperationScope } = await import(
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

function operation(name, request, events) {
  return {
    onStart: () => events.push(`${name}:busy`),
    request: () => {
      events.push(`${name}:request`);
      return request.promise;
    },
    onSuccess: (value) => events.push([`${name}:success`, value]),
    onError: (failure) => events.push([`${name}:error`, failure.message]),
    onFinish: () => events.push(`${name}:idle`),
  };
}

test("same-turn saves and connection tests share one synchronous request lock", async () => {
  for (const [first, second] of [
    ["save", "save"],
    ["test", "test"],
    ["save", "test"],
    ["test", "save"],
  ]) {
    const scope = createSettingsOperationScope();
    scope.mount();
    const request = deferred();
    const events = [];
    const pending = scope.run(operation(first, request, events));
    await scope.run(operation(second, deferred(), events));
    assert.deepEqual(events, [`${first}:busy`, `${first}:request`]);
    request.resolve("confirmed");
    await pending;
    assert.deepEqual(events.slice(2), [
      [`${first}:success`, "confirmed"],
      `${first}:idle`,
    ]);
    assert.equal(scope.available(), true);
  }
});

test("unmount suppresses every late save or test success, failure and finish effect", async () => {
  for (const name of ["save", "test"])
    for (const outcome of ["resolve", "reject"]) {
      const scope = createSettingsOperationScope();
      const unmount = scope.mount();
      const request = deferred();
      const events = [];
      const pending = scope.run(operation(name, request, events));
      unmount();
      request[outcome](outcome === "resolve" ? "saved" : new Error("failed"));
      await pending;
      assert.deepEqual(events, [`${name}:busy`, `${name}:request`]);
      await scope.run(operation(name, deferred(), events));
      assert.equal(events.length, 2);
    }
});

test("an old lifetime cannot publish or release a new request after setup repeats", async () => {
  for (const outcome of ["resolve", "reject"]) {
    const scope = createSettingsOperationScope();
    const unmount = scope.mount();
    const oldRequest = deferred();
    const events = [];
    const oldPending = scope.run(operation("old", oldRequest, events));
    unmount();
    scope.mount();
    const currentRequest = deferred();
    const currentPending = scope.run(
      operation("current", currentRequest, events),
    );
    oldRequest[outcome](
      outcome === "resolve" ? "obsolete" : new Error("obsolete"),
    );
    await oldPending;
    assert.equal(scope.available(), false);
    await scope.run(operation("duplicate", deferred(), events));
    assert.deepEqual(events, [
      "old:busy",
      "old:request",
      "current:busy",
      "current:request",
    ]);
    currentRequest.resolve("saved");
    await currentPending;
    assert.deepEqual(events.slice(4), [
      ["current:success", "saved"],
      "current:idle",
    ]);
    assert.equal(scope.available(), true);
  }
});

test("a current failure releases the lock and a later operation can succeed", async () => {
  const scope = createSettingsOperationScope();
  scope.mount();
  const request = deferred();
  const events = [];
  const failed = scope.run(operation("save", request, events));
  request.reject(new Error("revision conflict"));
  await failed;
  assert.deepEqual(events, [
    "save:busy",
    "save:request",
    ["save:error", "revision conflict"],
    "save:idle",
  ]);
  const next = deferred();
  const pending = scope.run(operation("retry", next, events));
  next.resolve("updated");
  await pending;
  assert.deepEqual(events.slice(4), [
    "retry:busy",
    "retry:request",
    ["retry:success", "updated"],
    "retry:idle",
  ]);
});

test("obsolete cleanup cannot invalidate a newer mounted settings page", async () => {
  const scope = createSettingsOperationScope();
  const obsoleteCleanup = scope.mount();
  scope.mount();
  obsoleteCleanup();
  const request = deferred();
  const events = [];
  const pending = scope.run(operation("save", request, events));
  request.resolve("saved");
  await pending;
  assert.deepEqual(events, [
    "save:busy",
    "save:request",
    ["save:success", "saved"],
    "save:idle",
  ]);
});
