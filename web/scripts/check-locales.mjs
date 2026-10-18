import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import ts from "typescript";

const root = path.resolve(import.meta.dirname, "../src");
const parse = (file) =>
  ts.createSourceFile(
    file,
    fs.readFileSync(file, "utf8"),
    ts.ScriptTarget.Latest,
    true,
    file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
  );
const dictionary = new Map();
function readDictionary(node) {
  if (
    ts.isVariableDeclaration(node) &&
    node.name.getText() === "english" &&
    node.initializer &&
    ts.isObjectLiteralExpression(node.initializer)
  ) {
    for (const property of node.initializer.properties) {
      if (
        ts.isPropertyAssignment(property) &&
        (ts.isStringLiteral(property.name) || ts.isIdentifier(property.name)) &&
        ts.isStringLiteral(property.initializer)
      )
        dictionary.set(property.name.text, property.initializer.text);
    }
  }
  ts.forEachChild(node, readDictionary);
}
readDictionary(parse(path.join(root, "i18n.ts")));
assert.ok(
  dictionary.size > 400,
  "The complete English UI dictionary must be present.",
);
const used = new Set(),
  missing = new Set();
for (const name of fs
  .readdirSync(root)
  .filter((name) => /\.(tsx?|ts)$/.test(name) && name !== "i18n.ts")) {
  const source = parse(path.join(root, name));
  function visit(node) {
    if (
      ts.isCallExpression(node) &&
      ts.isIdentifier(node.expression) &&
      ["t", "translate"].includes(node.expression.text) &&
      node.arguments[0] &&
      ts.isStringLiteral(node.arguments[0])
    ) {
      const phrase = node.arguments[0].text;
      used.add(phrase);
      if (!dictionary.has(phrase)) missing.add(phrase);
    }
    ts.forEachChild(node, visit);
  }
  visit(source);
}
assert.deepEqual(
  [...missing],
  [],
  `Missing English UI translations: ${[...missing].join(", ")}`,
);
const placeholders = (text) =>
  [
    ...new Set([...text.matchAll(/\{(\d+)\}/g)].map((match) => match[1])),
  ].sort();
for (const [source, translated] of dictionary)
  assert.deepEqual(
    placeholders(translated),
    placeholders(source),
    `Translation interpolation mismatch: ${source}`,
  );
console.log(
  `PASS: ${used.size} static UI phrases covered; ${dictionary.size} translations retain all interpolation placeholders.`,
);
