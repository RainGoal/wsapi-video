import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
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
  userFilters,
  userListParams,
  userRecord,
  validInitialPassword,
  userAuditValues,
} = await load("../src/admin/users.ts");
const { recordsUserTarget } = await load("../src/admin/operation-signals.ts");

test("user filters preserve independent account and verification states across navigation", () => {
  const filters = userFilters(
    "q=studio&status=disabled&email_verified=false&role=user&created_from=2026-10-01&created_to=2026-10-27&sort=created_asc&page=3",
  );
  assert.equal(filters.status, "disabled");
  assert.equal(filters.email_verified, "false");
  assert.equal(filters.page, 3);
  assert.deepEqual(userFilters(String(userListParams(filters))), filters);
  const params = userListParams(filters, true);
  assert.equal(params.get("offset"), "75");
  assert.equal(params.get("limit"), "25");
  assert.equal(params.has("record"), false);
  assert.equal(userFilters("status=unverified").email_verified, "false");
});

test("malformed user dates and offsets do not enter list requests", () => {
  for (const value of ["-1", "0.5", "NaN", "400001", "9007199254740991"])
    assert.equal(userFilters(`page=${value}`).page, 0);
  for (const value of ["2026-02-30", "2026-13-01", "27/10/2026"])
    assert.equal(userFilters(`created_from=${value}`).created_from, "");
  assert.equal(
    userFilters("created_from=2024-02-29").created_from,
    "2024-02-29",
  );
  assert.equal(userFilters("role=owner&sort=anything").sort, "created_desc");
});

test("registration dates match the local calendar across timezone and daylight saving boundaries", () => {
  const previous = process.env.TZ;
  try {
    process.env.TZ = "Asia/Shanghai";
    let params = userListParams(
      userFilters("created_from=2026-10-21&created_to=2026-10-21"),
      true,
    );
    assert.equal(params.get("created_from"), "2026-10-20T16:00:00.000Z");
    assert.equal(params.get("created_to"), "2026-10-21T16:00:00.000Z");
    process.env.TZ = "Europe/Berlin";
    params = userListParams(
      userFilters("created_from=2026-10-25&created_to=2026-10-25"),
      true,
    );
    assert.equal(params.get("created_from"), "2026-10-24T22:00:00.000Z");
    assert.equal(params.get("created_to"), "2026-10-25T23:00:00.000Z");
  } finally {
    if (previous === undefined) delete process.env.TZ;
    else process.env.TZ = previous;
  }
});

test("user business and credit links carry one precise user ID", () => {
  for (const section of ["tasks", "orders", "adjust"])
    assert.equal(recordsUserTarget(section, "user_id=usr_exact"), "usr_exact");
  assert.equal(
    recordsUserTarget("audit", "target_user_id=usr_exact"),
    "usr_exact",
  );
  assert.equal(recordsUserTarget("tasks", "user_id=first&user_id=second"), "");
  assert.equal(recordsUserTarget("settings", "user_id=usr_exact"), "");
  assert.equal(userRecord("record=usr_exact"), "usr_exact");
  assert.equal(userRecord("record=first&record=second"), "");
});

test("initial password validation observes the server character and UTF-8 byte limits", () => {
  assert.equal(validInitialPassword("initial-pass-2026"), true);
  assert.equal(validInitialPassword("short"), false);
  assert.equal(validInitialPassword("密".repeat(24)), true);
  assert.equal(validInitialPassword("密".repeat(25)), false);
  assert.equal(validInitialPassword("x".repeat(72)), true);
  assert.equal(validInitialPassword("x".repeat(73)), false);
});

test("user audit preserves legacy status transitions and creation values", () => {
  assert.deepEqual(
    userAuditValues({
      details: {
        previous_status: "active",
        next_status: "disabled",
        previous_values: {},
        next_values: {},
      },
    }).changes,
    [{ key: "status", previous: "active", next: "disabled" }],
  );
  assert.deepEqual(
    userAuditValues({
      details: {
        previous_values: {},
        next_values: { email: "new@example.test", email_verified: false },
      },
    }).changes,
    [
      { key: "email", previous: undefined, next: "new@example.test" },
      { key: "email_verified", previous: undefined, next: false },
    ],
  );
});

test("user audit keeps readable credit facts and task snapshots", () => {
  const result = userAuditValues({
    details: { amount: -25, request_key: "adjustment-reference", demo: true },
  });
  assert.deepEqual(result.changes, []);
  assert.deepEqual(result.facts, [
    { key: "amount", value: -25 },
    { key: "request_key", value: "adjustment-reference" },
    { key: "demo", value: true },
  ]);
  assert.deepEqual(
    userAuditValues({
      details: {
        previous: { settlement_status: "frozen", credits: 25 },
        upstream_id: "provider-task",
      },
    }).changes,
    [
      { key: "settlement_status", previous: "frozen", next: undefined },
      { key: "credits", previous: 25, next: undefined },
    ],
  );
});
