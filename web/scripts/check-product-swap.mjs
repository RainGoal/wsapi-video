import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import ts from "typescript";

const compiled = ts.transpileModule(
  fs.readFileSync(new URL("../src/product-swap.ts", import.meta.url), "utf8"),
  {
    compilerOptions: {
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2022,
    },
  },
).outputText;
const {
  emptySwapDraft,
  readSwapDraft,
  swapProducts,
  swapQuoteInput,
  swapDraftKey,
} = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
);
const product = {
  id: "seedance",
  kind: "video",
  model: "seedance-configured",
  demo: false,
  capabilities: {
    supports_product_swap: true,
    max_images: 3,
    max_videos: 1,
    durations: [5],
    resolutions: ["720p"],
    aspect_ratios: ["9:16"],
  },
};

test("product replacement only exposes explicitly enabled Seedance capabilities", () => {
  assert.equal(swapProducts([product]).length, 1);
  for (const patch of [
    { model: "other-video" },
    { demo: true },
    { capabilities: { ...product.capabilities, supports_product_swap: false } },
    { capabilities: { ...product.capabilities, max_videos: 0 } },
    { capabilities: { ...product.capabilities, durations: [60] } },
  ])
    assert.equal(swapProducts([{ ...product, ...patch }]).length, 0);
});
test("draft restores inputs without trusting saved analysis and separates accounts", () => {
  const draft = emptySwapDraft();
  draft.swap.source_video_id = "source";
  draft.swap.images = [{ asset_id: "front", angle: "front" }];
  draft.swap.analysis_id = "stale-analysis";
  const restored = readSwapDraft(JSON.stringify(draft));
  assert.equal(restored.swap.analysis_id, undefined);
  assert.equal(restored.swap.source_video_id, "source");
  assert.notEqual(swapDraftKey("one"), swapDraftKey("two"));
  assert.throws(() => readSwapDraft("{broken"));
  assert.throws(() =>
    readSwapDraft(
      JSON.stringify({
        ...draft,
        swap: { ...draft.swap, audio_mode: "invented" },
      }),
    ),
  );
});
test("quote binds angles, audio and instructions, without sending ordinary prompt or reference overrides", () => {
  const draft = emptySwapDraft();
  draft.swap.images = [{ asset_id: "front", angle: "front" }];
  const input = swapQuoteInput("workspace", draft);
  assert.equal(input.project_id, "workspace");
  assert.equal(input.product_swap, draft.swap);
  assert.equal(input.prompt, undefined);
  assert.equal(input.reference_asset_ids, undefined);
  const previous = JSON.stringify(input);
  draft.swap.images[0].angle = "back";
  assert.notEqual(JSON.stringify(swapQuoteInput("workspace", draft)), previous);
});
