import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import ts from "typescript";

const compiled = ts.transpileModule(
  fs.readFileSync(new URL("../src/pricing.ts", import.meta.url), "utf8"),
  {
    compilerOptions: {
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2022,
    },
  },
).outputText;
const { estimateProductPrice, productBillingUnit, validCredits } = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
);
const product = (pricing) => ({
  kind: "video",
  credits: 10,
  ...(pricing ? { pricing } : {}),
});

test("legacy products retain fixed per-generation pricing and images ignore video pricing", () => {
  assert.equal(productBillingUnit(product()), "per_video");
  assert.deepEqual(estimateProductPrice(product(), 15, "720p"), {
    billing_unit: "per_video",
    unit_credits: 10,
    quantity: 1,
    resolution: "720p",
    total_credits: 10,
  });
  const image = {
    ...product({
      billing_unit: "per_second",
      resolution_credits: { "720p": 30 },
    }),
    kind: "image",
  };
  assert.deepEqual(estimateProductPrice(image, 15, "720p"), {
    billing_unit: "per_video",
    unit_credits: 10,
    quantity: 1,
    resolution: "",
    total_credits: 10,
  });
});

test("resolution prices override the default case-insensitively for both billing units", () => {
  for (const billing_unit of ["per_video", "per_second"]) {
    const offer = product({
      billing_unit,
      resolution_credits: { " 720P ": 25, "1080p": 50 },
    });
    const before = JSON.stringify(offer);
    const priced = estimateProductPrice(offer, 8, " 720p ");
    assert.equal(priced.unit_credits, 25);
    assert.equal(priced.quantity, billing_unit === "per_second" ? 8 : 1);
    assert.equal(
      priced.total_credits,
      billing_unit === "per_second" ? 200 : 25,
    );
    assert.equal(estimateProductPrice(offer, 8, "1080P").unit_credits, 50);
    assert.equal(estimateProductPrice(offer, 8, "4k").unit_credits, 10);
    assert.equal(estimateProductPrice(offer, 8, "").unit_credits, 10);
    assert.equal(JSON.stringify(offer), before);
  }
});

test("per-second estimates follow requested duration and switching units never converts prices", () => {
  const pricing = {
    billing_unit: "per_second",
    resolution_credits: { "720p": 25 },
  };
  assert.equal(
    estimateProductPrice(product(pricing), 5, "720p").total_credits,
    125,
  );
  assert.equal(
    estimateProductPrice(product(pricing), 10, "720p").total_credits,
    250,
  );
  assert.equal(
    estimateProductPrice(
      product({ ...pricing, billing_unit: "per_video" }),
      10,
      "720p",
    ).total_credits,
    25,
  );
});

test("integer bounds prevent invalid unit prices, durations and overflowing totals", () => {
  assert.equal(
    estimateProductPrice(product({ billing_unit: "per_frame" }), 5, "720p"),
    null,
  );
  assert.equal(
    estimateProductPrice(product({ billing_unit: "" }), 5, "720p"),
    null,
  );
  assert.equal(
    estimateProductPrice(product({ billing_unit: "per_second" }), 5, "720p")
      .total_credits,
    50,
  );
  for (const value of [0, -1, 1.5, NaN, Infinity, 1000000001]) {
    assert.equal(validCredits(value), false);
    assert.equal(
      estimateProductPrice({ ...product(), credits: value }, 5, "720p"),
      null,
    );
    assert.equal(
      estimateProductPrice(
        product({ billing_unit: "per_second", resolution_credits: {} }),
        value,
        "720p",
      ),
      null,
    );
    assert.equal(
      estimateProductPrice(
        product({
          billing_unit: "per_video",
          resolution_credits: { "720p": value },
        }),
        5,
        "720p",
      ),
      null,
    );
  }
  assert.equal(validCredits(1), true);
  assert.equal(validCredits(1000000000), true);
  const maximum = {
    ...product({ billing_unit: "per_second", resolution_credits: {} }),
    credits: 200000000,
  };
  assert.equal(
    estimateProductPrice(maximum, 5, "720p").total_credits,
    1000000000,
  );
  assert.equal(estimateProductPrice(maximum, 6, "720p"), null);
});
