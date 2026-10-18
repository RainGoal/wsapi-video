import assert from "node:assert/strict";
import fs from "node:fs";
import { test } from "node:test";
import ts from "typescript";

async function load(sourcePath) {
  const source = fs.readFileSync(new URL(sourcePath, import.meta.url), "utf8");
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
const { policyFields, policyText, policyCodePointCount, policyLengthErrors } =
  await load("../src/policies.ts");
const { settingsPayload } = await load("../src/admin/validation.ts");

test("old and blank public policy fields are unpublished while content stays literal", () => {
  for (const value of [undefined, null, "", " \n\t ", 123, {}])
    assert.equal(policyText(value), "");
  const literal = '<img src=x onerror="alert(1)">\n第二行';
  assert.equal(policyText(literal), literal);
  assert.equal(policyFields.length, 5);
});

test("policy limits count Unicode code points after trimming instead of UTF-8 bytes or UTF-16 units", () => {
  const content = "🎬".repeat(2000);
  assert.equal(policyCodePointCount(` \n${content}\n `), 2000);
  assert.deepEqual(policyLengthErrors({ retention_policy: content }), []);
  assert.deepEqual(policyLengthErrors({ refund_policy: `${content}字` }), [
    "refund_policy",
  ]);
  assert.deepEqual(policyLengthErrors({}), []);
  for (const { key } of policyFields)
    assert.deepEqual(policyLengthErrors({ [key]: "字".repeat(2001) }), [key]);
});

test("settings save preserves all five plain-text policies, including explicit clearing", () => {
  const site = Object.fromEntries(
    policyFields.map(({ key }, i) => [
      key,
      i === 0 ? "" : `政策 ${i}\n<script>literal</script>`,
    ]),
  );
  const original = structuredClone(site);
  const payload = settingsPayload({ site }, {}, []);
  assert.deepEqual(payload.settings.site, site);
  assert.deepEqual(site, original);
});
