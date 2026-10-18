import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import ts from "typescript";

const source = fs.readFileSync(
  new URL("../src/admin/validation.ts", import.meta.url),
  "utf8",
);
const compiled = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.ESNext,
    target: ts.ScriptTarget.ES2022,
  },
}).outputText;
const { decimalFen, settingsPayload } = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
);
test("payment prices convert exact decimal strings to integer fen", () => {
  for (const [value, expected] of [
    ["0.01", 1],
    ["0.29", 29],
    ["19.9", 1990],
    ["19.99", 1999],
    ["1000000.00", 100000000],
  ])
    assert.equal(decimalFen(value), expected);
  for (const value of [
    "0",
    "-1",
    "1.001",
    "1e2",
    "0.0001",
    "1000000.01",
    "Infinity",
    "NaN",
    "",
  ])
    assert.equal(decimalFen(value), null);
});
test("settings save retains blank secrets and strips server-only state", () => {
  const draft = {
    media: {
      origin: "https://gateway.example",
      output_origins: [" https://files.example ", ""],
      image_key_configured: true,
    },
    storage: {
      driver: "s3",
      location_locked: true,
      secret_key_configured: true,
    },
  };
  const original = structuredClone(draft);
  const payload = settingsPayload(
    draft,
    { "media.image_key": "", "storage.secret_key": "" },
    [],
  );
  assert.deepEqual(payload, {
    settings: {
      media: {
        origin: "https://gateway.example",
        output_origins: ["https://files.example"],
      },
      storage: { driver: "s3" },
    },
    clear_secrets: [],
  });
  assert.deepEqual(draft, original);
});
test("settings secrets replace only explicit values and explicit clear takes precedence", () => {
  const payload = settingsPayload(
    {
      media: { origin: "https://gateway.example" },
      payment: { enabled: false },
    },
    { "media.image_key": "replacement", "payment.key": "ignored" },
    ["payment.key", "payment.key"],
  );
  assert.equal(payload.settings.media.image_key, "replacement");
  assert.equal(payload.settings.payment.key, undefined);
  assert.deepEqual(payload.clear_secrets, ["payment.key"]);
});
