import { createHash } from "node:crypto";
import { readFile, readdir, realpath, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { dirname, isAbsolute, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const canvasCommit = "3a6b64b602ad062b29fac66902ed18a7ef5fd9dc";
const hash = (bytes) => createHash("sha256").update(bytes).digest("hex");
const json = async (path) => JSON.parse(await readFile(path, "utf8"));
const noticeName = /^(?:licen[sc]e|copying|notice)(?:[._-].*)?$/i;
const licenseName = /^(?:licen[sc]e|copying)(?:[._-].*)?$/i;

async function readNotices(directory) {
  const entries = (await readdir(directory, { withFileTypes: true }))
    .filter((entry) => noticeName.test(entry.name))
    .sort((a, b) => a.name.localeCompare(b.name, "en"));
  if (!entries.some((entry) => licenseName.test(entry.name)))
    throw new Error(`No license file in ${directory}`);
  const notices = [];
  for (const entry of entries) {
    if (!entry.isFile()) throw new Error(`Notice is not a file: ${entry.name}`);
    const bytes = await readFile(resolve(directory, entry.name));
    const text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
    if (!text.trim()) throw new Error(`Empty notice: ${entry.name}`);
    notices.push({ name: entry.name, bytes, text });
  }
  return notices;
}

// Include the installed production dependency graph, including React DOM's
// scheduler. Versions and locations must match npm ci's lockfile entries.
export async function collectNotices(root) {
  root = await realpath(root);
  const lock = await json(resolve(root, "package-lock.json"));
  const application = await json(resolve(root, "package.json"));
  const packages = new Map();
  async function visit(name, parent) {
    const require = createRequire(resolve(parent, "package.json"));
    const directory = await realpath(
      dirname(require.resolve(`${name}/package.json`)),
    );
    const key = relative(root, directory).split(sep).join("/");
    if (
      isAbsolute(key) ||
      !key.startsWith("node_modules/") ||
      key.includes("../")
    )
      throw new Error(`Dependency must be inside node_modules: ${name}`);
    if (packages.has(key)) return;
    const metadata = await json(resolve(directory, "package.json"));
    const locked = lock.packages?.[key];
    if (
      !locked ||
      locked.link ||
      metadata.name !== name ||
      metadata.version !== locked.version
    )
      throw new Error(
        `Installed dependency does not match package-lock.json: ${key}`,
      );
    packages.set(key, { metadata, notices: await readNotices(directory) });
    for (const dependency of Object.keys({
      ...metadata.dependencies,
      ...metadata.optionalDependencies,
    }).sort())
      await visit(dependency, directory);
  }
  for (const name of Object.keys(application.dependencies || {}).sort())
    await visit(name, root);
  const canvas = await readFile(
    resolve(root, "public/infinite-canvas-LICENSE.txt"),
  );
  const canvasText = new TextDecoder("utf-8", { fatal: true }).decode(canvas);
  if (!canvasText.trim())
    throw new Error("The fixed canvas license is missing");
  let output = "FRAME Studio — browser distribution third-party notices\n\n";
  output +=
    "Generated from installed, lockfile-verified production dependencies.\n";
  output +=
    "Full upstream license/notice contents follow; no license text is inferred.\n\n";
  for (const [key, { metadata, notices }] of [...packages].sort()) {
    output += `=== ${metadata.name} ${metadata.version} (${key}) ===\n`;
    for (const notice of notices)
      output += `\n--- ${notice.name} (SHA-256 ${hash(notice.bytes)}) ---\n${notice.text}\n`;
    output += "\n";
  }
  output += `=== infinite-canvas v0.20.0 / ${canvasCommit} ===\n`;
  output += `Source: https://github.com/basketikun/infinite-canvas/tree/${canvasCommit}\n`;
  output +=
    "The platform adapts the canvas interaction components; visible author credits are retained.\n";
  output += `\n--- LICENSE (SHA-256 ${hash(canvas)}) ---\n${canvasText}\n`;
  return output;
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  try {
    if (process.argv.slice(2).some((argument) => argument !== "--check"))
      throw new Error("Usage: node scripts/build-notices.mjs [--check]");
    const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
    const target = resolve(root, "public/third-party-notices.txt");
    const output = await collectNotices(root);
    if (process.argv.includes("--check")) {
      if ((await readFile(target, "utf8")) !== output)
        throw new Error(
          "Browser notices are stale; run node scripts/build-notices.mjs",
        );
    } else await writeFile(target, output, "utf8");
    console.log("Browser third-party notices verified.");
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
