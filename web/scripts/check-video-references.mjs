import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import ts from "typescript";

const compiled = ts.transpileModule(
  fs.readFileSync(
    new URL("../src/video-references.ts", import.meta.url),
    "utf8",
  ),
  {
    compilerOptions: {
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2022,
    },
  },
).outputText;
const { changeVideoReferenceMode, videoReferenceMode } = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
);
const assets = [
  { id: "a", kind: "image" },
  { id: "b", kind: "image" },
  { id: "c", kind: "image" },
  { id: "v", kind: "video" },
];
const draft = (patch = {}) => ({
  kind: "video",
  prompt: "A scene",
  referenceIds: [],
  startFrameId: "",
  endFrameId: "",
  ...patch,
});

test("reference mode uses actual frame roles and preserves empty frame intent", () => {
  assert.equal(videoReferenceMode(draft(), true), "reference");
  assert.equal(
    videoReferenceMode(draft({ referenceMode: "frames" }), true),
    "frames",
  );
  assert.equal(
    videoReferenceMode(draft({ startFrameId: "a" }), true),
    "frames",
  );
  assert.equal(
    videoReferenceMode(draft({ startFrameId: "a", referenceIds: ["v"] }), true),
    "mixed",
  );
  assert.equal(
    videoReferenceMode(draft({ referenceMode: "frames" }), false),
    "reference",
  );
  assert.equal(
    videoReferenceMode(draft({ kind: "image", referenceMode: "frames" }), true),
    "reference",
  );
});

test("switching to frames assigns two images without dropping other references", () => {
  const source = draft({ referenceIds: ["a", "v", "b", "c", "a"] });
  const next = changeVideoReferenceMode(source, "frames", assets);
  assert.equal(next.startFrameId, "a");
  assert.equal(next.endFrameId, "b");
  assert.deepEqual(next.referenceIds, ["v", "c"]);
  assert.equal(videoReferenceMode(next, true), "mixed");
  assert.deepEqual(source.referenceIds, ["a", "v", "b", "c", "a"]);
});

test("mode switches retain existing frame roles and every selected asset", () => {
  const source = draft({ startFrameId: "b", referenceIds: ["a", "v"] });
  const frames = changeVideoReferenceMode(source, "frames", assets);
  assert.equal(frames.startFrameId, "b");
  assert.equal(frames.endFrameId, "a");
  const reference = changeVideoReferenceMode(frames, "reference", assets);
  assert.equal(reference.startFrameId, "");
  assert.equal(reference.endFrameId, "");
  assert.deepEqual(new Set(reference.referenceIds), new Set(["a", "b", "v"]));
  assert.equal(videoReferenceMode(reference, true), "reference");
});

test("missing and non-image references never become new frames", () => {
  const next = changeVideoReferenceMode(
    draft({ referenceIds: ["v", "missing"] }),
    "frames",
    assets,
  );
  assert.equal(next.startFrameId, "");
  assert.equal(next.endFrameId, "");
  assert.deepEqual(next.referenceIds, ["v", "missing"]);
  const empty = changeVideoReferenceMode(draft(), "frames", assets);
  assert.equal(videoReferenceMode(empty, true), "frames");
});
