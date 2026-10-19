import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import ts from "typescript";

const compiled = ts.transpileModule(
  fs.readFileSync(
    new URL("../src/video-durations.ts", import.meta.url),
    "utf8",
  ),
  {
    compilerOptions: {
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2022,
    },
  },
).outputText;
const { parseVideoDurations, videoDurationRange } = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
);

test("duration ranges expand every supported integer second and preserve discrete choices", () => {
  for (const separator of ["-", "–", "~"])
    assert.deepEqual(
      parseVideoDurations(`4 ${separator} 15`),
      Array.from({ length: 12 }, (_, i) => i + 4),
    );
  assert.deepEqual(parseVideoDurations("5, 10"), [5, 10]);
  assert.deepEqual(parseVideoDurations(" 15，4-6\n5, 10 "), [15, 4, 5, 6, 10]);
  assert.deepEqual(parseVideoDurations("1, 600, 1-1"), [1, 600]);
  assert.deepEqual(parseVideoDurations(""), []);
  assert.deepEqual(parseVideoDurations(" \n "), []);
});

test("invalid and incomplete duration entries fail as a whole", () => {
  for (const text of [
    "0",
    "601",
    "-1",
    "1.5",
    "4-3",
    "4-601",
    "0-4",
    "4-5.5",
    "4-",
    "4--15",
    "4-5-6",
    "4 5",
    "4s",
    "NaN",
    "Infinity",
    "1e2",
    "0x10",
    "4,invalid,15",
    "4,,15",
    "4,",
    ",4",
    "4\n\n15",
    "99999999999999999999",
  ])
    assert.equal(parseVideoDurations(text), null, text);
});

test("duration choices match the backend limit after deduplication", () => {
  const choices = Array.from({ length: 64 }, (_, i) => i + 1);
  assert.deepEqual(parseVideoDurations("1-64"), choices);
  assert.deepEqual(parseVideoDurations("1-64,1-64"), choices);
  assert.deepEqual(parseVideoDurations(choices.join(",")), choices);
  assert.equal(parseVideoDurations("1-65"), null);
  assert.equal(parseVideoDurations("1-64,600"), null);
  assert.equal(parseVideoDurations([...choices, 65].join(",")), null);
});

test("only continuous valid options expose a slider range without mutating the list", () => {
  const options = [6, 4, 5, 5];
  assert.deepEqual(videoDurationRange(options), { min: 4, max: 6 });
  assert.deepEqual(options, [6, 4, 5, 5]);
  assert.deepEqual(videoDurationRange(parseVideoDurations("4-15")), {
    min: 4,
    max: 15,
  });
  for (const options of [
    [],
    [5],
    [5, 5],
    [5, 10],
    [4, 6, 7],
    [0, 1],
    [600, 601],
    [4, 4.5, 5],
    [NaN, 5],
  ])
    assert.equal(videoDurationRange(options), null, JSON.stringify(options));
});
