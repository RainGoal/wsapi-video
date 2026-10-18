import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import ts from "typescript";

const compiled = ts.transpileModule(
  fs.readFileSync(
    new URL("../src/admin/catalog-product.ts", import.meta.url),
    "utf8",
  ),
  {
    compilerOptions: {
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2022,
    },
  },
).outputText;
const { productCapabilities, productDefinitionChanged } = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
);
const product = () => ({
  id: "offer",
  name: "Verified offer",
  kind: "image",
  model: "model-a",
  credits: 10,
  version: 3,
  active: true,
  verified: true,
  demo: false,
  capabilities: {
    aspect_ratios: ["1:1", "16:9"],
    durations: [],
    resolutions: [],
    sizes: [],
    qualities: [],
    max_images: 0,
    max_videos: 0,
    max_audios: 0,
    supports_audio: false,
    supports_frames: false,
    supports_cancel: false,
  },
});

test("model, media and every capability invalidate old verification while commercial edits retain it", () => {
  const original = product(),
    saved = JSON.stringify(original);
  for (const patch of [
    { name: "Renamed", credits: 50 },
    { model: " model-a " },
    { version: 4, active: false, verified: false },
  ])
    assert.equal(
      productDefinitionChanged(original, { ...original, ...patch }),
      false,
    );
  for (const patch of [{ model: "model-b" }, { kind: "video" }])
    assert.equal(
      productDefinitionChanged(original, { ...original, ...patch }),
      true,
    );
  for (const [key, value] of Object.entries({
    aspect_ratios: ["16:9", "1:1"],
    durations: [5],
    resolutions: ["720p"],
    sizes: ["1024x1024"],
    qualities: ["high"],
    max_images: 1,
    max_videos: 1,
    max_audios: 1,
    supports_audio: true,
    supports_frames: true,
    supports_cancel: true,
    max_reference_bytes: 100,
    max_reference_duration_seconds: 10,
    max_reference_width: 1024,
    max_reference_height: 1024,
  }))
    assert.equal(
      productDefinitionChanged(original, {
        ...original,
        capabilities: { ...original.capabilities, [key]: value },
      }),
      true,
      key,
    );
  assert.equal(JSON.stringify(original), saved);
  assert.equal(
    productDefinitionChanged(
      { ...original, version: 0 },
      { ...original, model: "new" },
    ),
    false,
  );
});

test("empty collection/default representations are equivalent and reverting restores the old definition", () => {
  const original = product(),
    next = structuredClone(original);
  for (const key of ["durations", "resolutions", "sizes", "qualities"])
    next.capabilities[key] = null;
  Object.assign(next.capabilities, {
    max_reference_bytes: 0,
    max_reference_duration_seconds: 0,
    max_reference_width: 0,
    max_reference_height: 0,
  });
  assert.equal(productDefinitionChanged(original, next), false);
  next.model = "changed";
  assert.equal(productDefinitionChanged(original, next), true);
  next.model = original.model;
  assert.equal(productDefinitionChanged(original, next), false);
  assert.equal(
    productDefinitionChanged({ ...original, model: " model-a " }, original),
    true,
  );
});

test("switching media cleans unsupported fields without mutating the original verified product", () => {
  const original = product(),
    before = JSON.stringify(original);
  const lists = {
    aspect_ratios: "16:9， 9:16,16:9",
    durations: "5, 10",
    resolutions: "720p\n1080p",
    sizes: "1024x1024",
    qualities: "high",
  };
  const video = productCapabilities("video", original.capabilities, lists);
  assert.deepEqual(video.sizes, []);
  assert.deepEqual(video.qualities, []);
  assert.deepEqual(video.durations, [5, 10]);
  assert.deepEqual(video.resolutions, ["720p", "1080p"]);
  assert.deepEqual(video.aspect_ratios, ["16:9", "9:16"]);
  const image = productCapabilities(
    "image",
    {
      ...video,
      supports_audio: true,
      supports_frames: true,
      supports_cancel: true,
      max_videos: 2,
      max_audios: 1,
    },
    lists,
  );
  assert.deepEqual(image.sizes, ["1024x1024"]);
  assert.deepEqual(image.qualities, ["high"]);
  assert.deepEqual(image.durations, []);
  assert.deepEqual(image.resolutions, []);
  for (const key of ["max_videos", "max_audios"]) assert.equal(image[key], 0);
  for (const key of ["supports_audio", "supports_frames", "supports_cancel"])
    assert.equal(image[key], false);
  assert.equal(JSON.stringify(original), before);
});

test("cleaning legacy video-only unsupported options requires re-verification", () => {
  const original = { ...product(), kind: "video" };
  original.capabilities.qualities = ["high"];
  original.capabilities.sizes = ["1024x1024"];
  const next = productCapabilities("video", original.capabilities, {
    aspect_ratios: "1:1,16:9",
    durations: "",
    resolutions: "",
    sizes: "1024x1024",
    qualities: "high",
  });
  assert.equal(
    productDefinitionChanged(original, { ...original, capabilities: next }),
    true,
  );
  assert.deepEqual(original.capabilities.qualities, ["high"]);
  assert.deepEqual(original.capabilities.sizes, ["1024x1024"]);
});
