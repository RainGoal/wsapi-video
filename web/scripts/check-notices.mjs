import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { basename, dirname, resolve } from "node:path";
import test from "node:test";
import { collectNotices } from "./build-notices.mjs";

async function fixture(t) {
  const root = await mkdtemp(resolve(tmpdir(), "frame-notices-"));
  t.after(async () => {
    assert.equal(dirname(root), resolve(tmpdir()));
    assert.ok(basename(root).startsWith("frame-notices-"));
    await rm(root, { recursive: true });
  });
  const put = async (path, value) => {
    const target = resolve(root, path);
    await mkdir(dirname(target), { recursive: true });
    await writeFile(
      target,
      typeof value === "string" ? value : JSON.stringify(value),
    );
  };
  await put("package.json", {
    dependencies: { react: "1.0.0", "react-dom": "1.0.0" },
  });
  await put("package-lock.json", {
    packages: {
      "node_modules/react": { version: "1.0.0" },
      "node_modules/react-dom": { version: "1.0.0" },
      "node_modules/scheduler": { version: "0.1.0" },
    },
  });
  for (const [name, version] of [
    ["react", "1.0.0"],
    ["react-dom", "1.0.0"],
    ["scheduler", "0.1.0"],
  ]) {
    await put(`node_modules/${name}/package.json`, {
      name,
      version,
      ...(name === "react-dom"
        ? { dependencies: { scheduler: "^0.1.0" } }
        : {}),
    });
    await put(
      `node_modules/${name}/LICENSE`,
      `copyright ${name}\r\ncomplete ${name} permission\r\n`,
    );
  }
  await put("node_modules/scheduler/NOTICE", "scheduler attribution\n");
  await put(
    "public/infinite-canvas-LICENSE.txt",
    "copyright basketikun\ncanvas permission\n",
  );
  return { root, put };
}

test("browser notices include scheduler and unchanged upstream license/notice bytes", async (t) => {
  const { root } = await fixture(t);
  const result = await collectNotices(root);
  for (const name of ["react", "react-dom", "scheduler"]) {
    const license = await readFile(
      resolve(root, `node_modules/${name}/LICENSE`),
      "utf8",
    );
    assert.ok(result.includes(license));
  }
  assert.match(result, /scheduler 0\.1\.0/);
  assert.match(result, /scheduler attribution/);
  assert.match(result, /3a6b64b602ad062b29fac66902ed18a7ef5fd9dc/);
  assert.equal(await collectNotices(root), result);
});

test("missing transitive license cannot silently use stale browser notices", async (t) => {
  const { root } = await fixture(t);
  await rm(resolve(root, "node_modules/scheduler/LICENSE"));
  await assert.rejects(collectNotices(root), /No license file/);
});

test("unlocked or substituted installed dependency rejects notice generation", async (t) => {
  const { root, put } = await fixture(t);
  await put("node_modules/scheduler/package.json", {
    name: "scheduler",
    version: "0.2.0",
  });
  await assert.rejects(collectNotices(root), /does not match package-lock/);
});
