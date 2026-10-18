import assert from "node:assert/strict";
import fs from "node:fs";
import { test } from "node:test";
import ts from "typescript";

async function load(path) {
  const source = fs.readFileSync(new URL(path, import.meta.url), "utf8");
  const compiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2022,
    },
  }).outputText;
  return import(
    `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
  );
}
const {
  signalDefinitions,
  signalDefinition,
  signalDestination,
  recordsSignalTarget,
} = await load("../src/admin/operation-signals.ts");
const { routeFromLocation } = await load("../src/routing.ts");

test("signal categories navigate to the corresponding complete filtered lists", () => {
  const expected = {
    task_submission_unknown: ["tasks", "submission_unknown"],
    task_frozen_overdue: ["tasks", "frozen_overdue"],
    task_archive_failed: ["tasks", "archive_failed"],
    task_lease_expired: ["tasks", "lease_expired"],
    payment_review: ["orders", "review_required"],
    procurement_review_disputed: ["tasks", "procurement_review_disputed"],
  };
  assert.equal(Object.keys(signalDefinitions).length, 8);
  for (const [code, [section, status]] of Object.entries(expected)) {
    const route = routeFromLocation({
      hash: `#/admin/${signalDestination(code)}`,
      pathname: "/",
    });
    assert.equal(route.page, "admin");
    assert.equal(route.id, section);
    assert.deepEqual(recordsSignalTarget(route.id, route.query), {
      recordId: "",
      initialStatus: status,
    });
  }
});

test("task and order samples retain the exact local ID while preserving admin navigation", () => {
  for (const code of [
    "task_submission_unknown",
    "payment_review",
    "procurement_review_disputed",
  ]) {
    const taskSignal = code !== "payment_review";
    const localId = taskSignal ? "tsk_local_123" : "ord_local_456";
    const route = routeFromLocation({
      hash: `#/admin/${signalDestination(code, localId)}`,
      pathname: "/",
    });
    assert.equal(route.id, taskSignal ? "tasks" : "orders");
    assert.deepEqual(recordsSignalTarget(route.id, route.query), {
      recordId: localId,
      initialStatus: "",
    });
  }
});

test("mail never routes local outbox IDs to task or payment details and unknown categories have no action", () => {
  assert.equal(
    signalDestination("mail_pending", "private-outbox-id"),
    "settings",
  );
  assert.equal(
    signalDestination("mail_retry", "private-outbox-id"),
    "settings",
  );
  assert.equal(signalDestination("unknown"), null);
  assert.equal(signalDestination("__proto__"), null);
  assert.equal(signalDefinition("constructor"), undefined);
});

test("ambiguous record targets and unrelated status filters are ignored", () => {
  assert.deepEqual(
    recordsSignalTarget("tasks", "record=first&record=second&status=failed"),
    { recordId: "", initialStatus: "" },
  );
  assert.deepEqual(recordsSignalTarget("orders", "status=lease_expired"), {
    recordId: "",
    initialStatus: "",
  });
  assert.deepEqual(recordsSignalTarget("users", "record=some-user"), {
    recordId: "",
    initialStatus: "",
  });
  assert.deepEqual(recordsSignalTarget("tasks", `record=${"x".repeat(201)}`), {
    recordId: "",
    initialStatus: "",
  });
});
