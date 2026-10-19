import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import ts from "typescript";

const pricingModule = ts.transpileModule(
  fs.readFileSync(new URL("../src/pricing.ts", import.meta.url), "utf8"),
  {
    compilerOptions: {
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2022,
    },
  },
).outputText;
const pricingURL = `data:text/javascript;base64,${Buffer.from(pricingModule).toString("base64")}`;
const { estimateProductPrice } = await import(pricingURL);
const durationsModule = ts.transpileModule(
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
const durationsURL = `data:text/javascript;base64,${Buffer.from(durationsModule).toString("base64")}`;
const compiled = ts
  .transpileModule(
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
  )
  .outputText.replace('"../pricing"', JSON.stringify(pricingURL))
  .replace('"../video-durations"', JSON.stringify(durationsURL));
const {
  productCapabilities,
  productDefinitionChanged,
  productPricing,
  pricingResolutions,
  productPriceLimitsValid,
} = await import(
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
    {
      pricing: {
        billing_unit: "per_second",
        resolution_credits: { "720p": 20 },
      },
    },
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

test("catalog range input saves existing duration arrays and rejects malformed entries", () => {
  const original = product();
  const lists = {
    aspect_ratios: "16:9",
    durations: "4-15",
    resolutions: "720p,1080p",
    sizes: "",
    qualities: "",
  };
  const capabilities = productCapabilities(
    "video",
    original.capabilities,
    lists,
  );
  assert.deepEqual(
    capabilities.durations,
    Array.from({ length: 12 }, (_, i) => i + 4),
  );
  assert.deepEqual(original.capabilities.durations, []);
  for (const durations of ["5,10", "5，10"])
    assert.deepEqual(
      productCapabilities("video", original.capabilities, {
        ...lists,
        durations,
      }).durations,
      [5, 10],
    );
  for (const durations of [
    "15-4",
    "4-15,invalid",
    "5,10.5",
    "5,,10",
    "0,5",
    "4-601",
  ])
    assert.equal(
      productCapabilities("video", original.capabilities, {
        ...lists,
        durations,
      }),
      null,
      durations,
    );
  assert.deepEqual(
    productCapabilities("image", original.capabilities, {
      ...lists,
      durations: "invalid",
    }).durations,
    [],
  );
});

test("video pricing saves only selected resolutions and blank overrides use the default", () => {
  const overrides = { "720p": " 25 ", "1080p": "", "4k": "200" };
  const before = structuredClone(overrides);
  assert.deepEqual(pricingResolutions(["720p", " 720P ", "1080P", ""]), [
    "720p",
    "1080p",
  ]);
  assert.deepEqual(
    productPricing("video", "per_second", [" 720P ", "1080p"], overrides),
    {
      billing_unit: "per_second",
      resolution_credits: { "720p": 25 },
    },
  );
  assert.deepEqual(productPricing("video", "per_video", ["720p"], overrides), {
    billing_unit: "per_video",
    resolution_credits: { "720p": 25 },
  });
  assert.deepEqual(overrides, before);
  const unusual = productPricing(
    "video",
    "per_video",
    ["__proto__"],
    Object.fromEntries([["__proto__", "37"]]),
  );
  assert.deepEqual(Object.entries(unusual.resolution_credits), [
    ["__proto__", 37],
  ]);
  assert.equal(
    Object.getPrototypeOf(unusual.resolution_credits),
    Object.prototype,
  );
  assert.deepEqual(
    productPricing("video", "per_video", ["720p"], { "720p": "" }),
    {
      billing_unit: "per_video",
      resolution_credits: {},
    },
  );
  assert.equal(
    productPricing("image", "per_second", ["720p"], overrides),
    undefined,
  );
});

test("resolution overrides require positive integer credits within the billing limit", () => {
  for (const value of ["0", "-1", "1.5", "NaN", "Infinity", "1000000001"])
    assert.equal(
      productPricing("video", "per_video", ["720p"], { "720p": value }),
      null,
      value,
    );
  for (const value of ["1", "1000000000"])
    assert.equal(
      productPricing("video", "per_second", ["720p"], { "720p": value })
        .resolution_credits["720p"],
      Number(value),
    );
  assert.deepEqual(productPricing("video", "per_video", ["constructor"], {}), {
    billing_unit: "per_video",
    resolution_credits: {},
  });
});

test("per-request resolution prices stay fixed while per-second prices use requested duration", () => {
  const overrides = { "720p": "30", "1080p": "50", "480p": "" };
  const resolutions = ["480p", "720p", "1080p"];
  const video = { ...product(), kind: "video", credits: 20 };
  const fixed = productPricing("video", "per_video", resolutions, overrides);
  const perSecond = productPricing(
    "video",
    "per_second",
    resolutions,
    overrides,
  );
  assert.deepEqual(fixed.resolution_credits, perSecond.resolution_credits);
  assert.deepEqual(overrides, { "720p": "30", "1080p": "50", "480p": "" });
  for (const duration of [4, 5, 10, 15]) {
    assert.equal(
      estimateProductPrice({ ...video, pricing: fixed }, duration, "720p")
        .total_credits,
      30,
    );
    assert.equal(
      estimateProductPrice({ ...video, pricing: fixed }, duration, "1080p")
        .total_credits,
      50,
    );
    assert.equal(
      estimateProductPrice({ ...video, pricing: fixed }, duration, "480p")
        .total_credits,
      20,
    );
    assert.equal(
      estimateProductPrice({ ...video, pricing: perSecond }, duration, "720p")
        .total_credits,
      30 * duration,
    );
    assert.equal(
      estimateProductPrice({ ...video, pricing: perSecond }, duration, "1080p")
        .total_credits,
      50 * duration,
    );
    assert.equal(
      estimateProductPrice({ ...video, pricing: perSecond }, duration, "480p")
        .total_credits,
      20 * duration,
    );
  }
});

test("saving per-second pricing validates every duration against default and resolution prices", () => {
  const price = {
    billing_unit: "per_second",
    resolution_credits: { "720p": 200000000 },
  };
  assert.equal(productPriceLimitsValid(10, price, []), false);
  assert.equal(productPriceLimitsValid(10, price, [0, 5]), false);
  assert.equal(productPriceLimitsValid(10, price, [5, 5.5]), false);
  assert.equal(productPriceLimitsValid(10, price, [5, 601]), false);
  assert.equal(productPriceLimitsValid(10, price, [1, 5]), true);
  assert.equal(productPriceLimitsValid(10, price, [1, 5, 10]), false);
  assert.equal(
    productPriceLimitsValid(
      250000000,
      { ...price, resolution_credits: { "720p": 10 } },
      [5],
    ),
    false,
  );
  assert.equal(
    productPriceLimitsValid(
      200000000,
      { ...price, resolution_credits: null },
      [5],
    ),
    true,
  );
  assert.equal(
    productPriceLimitsValid(
      1000000000,
      { ...price, billing_unit: "per_video" },
      [],
    ),
    true,
  );
  assert.equal(productPriceLimitsValid(1000000000, undefined, []), true);
  assert.equal(
    productPriceLimitsValid(1, { billing_unit: "per_frame" }, [5]),
    false,
  );
});
