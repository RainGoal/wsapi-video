import assert from "node:assert/strict";
import fs from "node:fs";
import { test } from "node:test";
import ts from "typescript";

const source = fs.readFileSync(
  new URL("../src/routing.ts", import.meta.url),
  "utf8",
);
const compiled = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.ESNext,
    target: ts.ScriptTarget.ES2022,
  },
}).outputText;
const { routeFromLocation, hasPaymentReturn, matchingReturnedOrder } =
  await import(
    `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
  );

test("payment pathname reaches wallet while explicit hash routes retain priority", () => {
  assert.equal(
    routeFromLocation({ hash: "", pathname: "/wallet" }).page,
    "wallet",
  );
  assert.equal(
    routeFromLocation({ hash: "", pathname: "/wallet/" }).page,
    "wallet",
  );
  assert.equal(
    routeFromLocation({ hash: "#/projects", pathname: "/wallet" }).page,
    "projects",
  );
  assert.deepEqual(routeFromLocation({ hash: "#/project/p1", pathname: "/" }), {
    page: "project",
    id: "p1",
  });
  assert.equal(
    routeFromLocation({ hash: "", pathname: "/unknown" }).page,
    "projects",
  );
});

test("return reference only locates an exact order from the supplied account list", () => {
  const ownOrder = {
    id: "internal-1",
    out_trade_no: "own-1",
    amount_fen: 1000,
  };
  const ownOrders = [ownOrder];
  assert.equal(hasPaymentReturn("?out_trade_no=own-1"), true);
  assert.equal(hasPaymentReturn("?trade_status=TRADE_SUCCESS"), false);
  assert.equal(
    matchingReturnedOrder(
      "?out_trade_no=own-1&money=0.01&trade_status=TRADE_SUCCESS",
      ownOrders,
    ),
    ownOrder,
  );
  assert.equal(ownOrder.amount_fen, 1000);
  assert.equal(
    matchingReturnedOrder("?out_trade_no=someone-elses-order", ownOrders),
    undefined,
  );
  assert.equal(
    matchingReturnedOrder("?out_trade_no=internal-1", ownOrders),
    undefined,
  );
  assert.equal(matchingReturnedOrder("?out_trade_no=own-1", []), undefined);
});

test("missing, empty, and ambiguous return references cannot trigger a match", () => {
  const ownOrders = [{ id: "internal-1", out_trade_no: "own-1" }];
  for (const search of [
    "",
    "?money=100",
    "?out_trade_no=",
    "?out_trade_no=own-1&out_trade_no=own-1",
    "?out_trade_no=own-1&out_trade_no=other",
  ])
    assert.equal(matchingReturnedOrder(search, ownOrders), undefined);
});
