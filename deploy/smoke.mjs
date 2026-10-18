// Builds/pulls happen before this script. All running smoke services are isolated.
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdtemp, writeFile, unlink, rmdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

const image = process.argv[2] || "frame-studio:ci";
const databaseImage = "postgres:16.4-bookworm";
const owner = randomBytes(8).toString("hex");
const prefix = `frame-smoke-${owner}`;
const names = {
  database: `${prefix}-db`,
  rejected: `${prefix}-no-admin`,
  app: `${prefix}-app`,
};
const origin = "https://setup-smoke.invalid";
const password = randomBytes(24).toString("hex");
const mainKey = randomBytes(32).toString("hex");
const adminPassword = randomBytes(24).toString("hex");
const adminEmail = "admin@setup-smoke.invalid";
let directory;
let networkCreated = false;

function docker(args, { input, allowFailure = false, timeout = 15000 } = {}) {
  return new Promise((resolve, reject) => {
    const child = execFile(
      "docker",
      args,
      { timeout, maxBuffer: 2 << 20 },
      (error, stdout, stderr) => {
        if (error && !allowFailure) {
          const message =
            error.code === "ENOENT"
              ? "Docker CLI is unavailable; container smoke was not executed."
              : `Docker ${args[0]} failed (${error.code || "timeout"}); verify the daemon and preloaded images.`;
          reject(new Error(message));
        } else
          resolve({
            code: error ? error.code || 1 : 0,
            stdout: stdout.trim(),
            stderr: stderr.trim(),
          });
      },
    );
    child.stdin?.end(input);
  });
}

async function waitFor(label, check, timeout = 90000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if (await check()) return;
    await delay(1000);
  }
  throw new Error(`Timed out waiting for ${label}.`);
}

async function appRequest(path, args = [], input) {
  return (
    await docker(
      [
        "exec",
        ...(input === undefined ? [] : ["--interactive"]),
        names.app,
        "curl",
        "--silent",
        "--show-error",
        "--max-time",
        "4",
        ...args,
        `http://127.0.0.1:8090${path}`,
      ],
      { input },
    )
  ).stdout;
}

async function startApp(envFile) {
  await docker([
    "run",
    "--detach",
    "--pull=never",
    "--init",
    "--name",
    names.app,
    "--label",
    `frame.smoke=${owner}`,
    "--network",
    prefix,
    "--cap-drop=ALL",
    "--security-opt=no-new-privileges:true",
    "--env-file",
    envFile,
    image,
  ]);
  await waitFor("production setup healthz", async () => {
    try {
      return (
        (await appRequest("/healthz", [
          "--output",
          "/dev/null",
          "--write-out",
          "%{http_code}",
        ])) === "200"
      );
    } catch {
      return false;
    }
  });
}

async function login() {
  const raw = await appRequest(
    "/api/v1/auth/login",
    [
      "--include",
      "--request",
      "POST",
      "--header",
      `Origin: ${origin}`,
      "--header",
      "Content-Type: application/json",
      "--data-binary",
      "@-",
    ],
    JSON.stringify({ email: adminEmail, password: adminPassword }),
  );
  const boundary = raw.indexOf("\r\n\r\n");
  assert.ok(boundary > 0, "Login must include HTTP response headers.");
  const headers = raw.slice(0, boundary);
  const body = JSON.parse(raw.slice(boundary + 4));
  assert.match(headers, /^HTTP\/1\.1 200/m);
  assert.match(headers, /^Set-Cookie: [^\r\n]*; HttpOnly/im);
  assert.match(headers, /^Set-Cookie: [^\r\n]*; Secure/im);
  assert.match(headers, /^Set-Cookie: [^\r\n]*; SameSite=Lax/im);
  assert.equal(body.code, "ok");
  assert.equal(body.data.user.role, "admin");
  const cookie = headers.match(/^Set-Cookie: ([^;\r\n]+)/im)?.[1];
  assert.ok(cookie, "Bootstrap login must set a session cookie.");
  return cookie;
}

async function verifyReleaseLicenses() {
  const browserNotices = await appRequest("/third-party-notices.txt");
  for (const dependency of ["react", "react-dom", "scheduler"])
    assert.match(
      browserNotices,
      new RegExp(`=== ${dependency} [^\\r\\n]+ ===`),
    );
  assert.match(
    browserNotices,
    /Copyright \(c\) Meta Platforms, Inc\. and affiliates\./,
  );
  assert.match(
    browserNotices,
    /=== infinite-canvas v0\.20\.0 \/ 3a6b64b602ad062b29fac66902ed18a7ef5fd9dc ===/,
  );
  assert.match(browserNotices, /Copyright \(c\) 2026 basketikun/);
  assert.equal(
    (browserNotices.match(/THE SOFTWARE IS PROVIDED "AS IS"/g) || []).length,
    4,
  );
  const manifest = JSON.parse(
    (await docker(["exec", names.app, "cat", "/app/licenses/manifest.json"]))
      .stdout,
  );
  assert.deepEqual(manifest.binaries.map((binary) => binary.name).sort(), [
    "monitor",
    "studio",
  ]);
  assert.ok(manifest.modules.length > 0);
  const keys = new Set(
    manifest.modules.map((module) => `${module.path}@${module.version}`),
  );
  const expected = new Map();
  for (const binary of manifest.binaries) {
    assert.equal(binary.go_version, manifest.go_runtime.version);
    for (const module of binary.modules) assert.ok(keys.has(module));
    expected.set(`/app/${binary.name}`, binary.sha256);
  }
  for (const notice of [
    ...manifest.go_runtime.notices,
    ...manifest.modules.flatMap((module) => module.notices),
  ]) {
    assert.match(notice.path, /^(?:go|modules)\//);
    assert.ok(!notice.path.split("/").includes(".."));
    expected.set(`/app/licenses/${notice.path}`, notice.sha256);
  }
  assert.ok(expected.has("/app/licenses/go/LICENSE"));
  const checksums = (
    await docker(["exec", names.app, "sha256sum", "--", ...expected.keys()])
  ).stdout.split("\n");
  assert.equal(checksums.length, expected.size);
  for (const line of checksums) {
    const match = /^([a-f0-9]{64})  (.+)$/.exec(line);
    assert.ok(match, "License checksum output must be well formed.");
    assert.equal(match[1], expected.get(match[2]));
  }
}

async function cleanup() {
  for (const name of Object.values(names)) {
    const label = await docker(
      ["inspect", "--format", '{{index .Config.Labels "frame.smoke"}}', name],
      { allowFailure: true },
    );
    if (label.stdout === owner)
      await docker(["rm", "--force", name], { allowFailure: true });
  }
  if (networkCreated) {
    const label = await docker(
      [
        "network",
        "inspect",
        "--format",
        '{{index .Labels "frame.smoke"}}',
        prefix,
      ],
      { allowFailure: true },
    );
    if (label.stdout === owner)
      await docker(["network", "rm", prefix], { allowFailure: true });
  }
  if (directory) {
    for (const name of ["database.env", "setup.env", "bootstrap.env"])
      await unlink(join(directory, name)).catch(() => {});
    await rmdir(directory).catch(() => {});
  }
}

try {
  await docker(["version", "--format", "{{.Server.Version}}"]);
  await docker(["image", "inspect", "--format", "{{.Id}}", image]);
  await docker(["image", "inspect", "--format", "{{.Id}}", databaseImage]);
  directory = await mkdtemp(join(tmpdir(), `${prefix}-`));
  await writeFile(
    join(directory, "database.env"),
    `POSTGRES_USER=studio\nPOSTGRES_DB=studio\nPOSTGRES_PASSWORD=${password}\n`,
    { mode: 0o600 },
  );
  const setup = `APP_ENV=production\nAPP_SETUP_MODE=true\nDEMO_MODE=false\nPUBLIC_URL=${origin}\nLISTEN_ADDR=0.0.0.0:8090\nDATABASE_URL=postgres://studio:${password}@database:5432/studio?sslmode=disable\nAPP_SECRET=${mainKey}\nSTORAGE_DRIVER=s3\nS3_REGION=auto\nAWS_EC2_METADATA_DISABLED=true\n`;
  await writeFile(join(directory, "setup.env"), setup, { mode: 0o600 });
  await writeFile(
    join(directory, "bootstrap.env"),
    `${setup}BOOTSTRAP_ADMIN_EMAIL=${adminEmail}\nBOOTSTRAP_ADMIN_PASSWORD=${adminPassword}\n`,
    { mode: 0o600 },
  );
  await docker([
    "network",
    "create",
    "--internal",
    "--label",
    `frame.smoke=${owner}`,
    prefix,
  ]);
  networkCreated = true;
  await docker([
    "run",
    "--detach",
    "--pull=never",
    "--name",
    names.database,
    "--label",
    `frame.smoke=${owner}`,
    "--network",
    prefix,
    "--network-alias",
    "database",
    "--tmpfs",
    "/var/lib/postgresql/data:rw,nosuid,size=256m",
    "--env-file",
    join(directory, "database.env"),
    databaseImage,
  ]);
  await waitFor(
    "isolated PostgreSQL",
    async () =>
      (
        await docker(
          [
            "exec",
            names.database,
            "pg_isready",
            "-h",
            "127.0.0.1",
            "-U",
            "studio",
            "-d",
            "studio",
          ],
          { allowFailure: true },
        )
      ).code === 0,
  );

  await docker([
    "run",
    "--detach",
    "--pull=never",
    "--name",
    names.rejected,
    "--label",
    `frame.smoke=${owner}`,
    "--network",
    prefix,
    "--env-file",
    join(directory, "setup.env"),
    image,
  ]);
  await waitFor(
    "missing-admin rejection",
    async () =>
      (
        await docker([
          "inspect",
          "--format",
          "{{.State.Running}}",
          names.rejected,
        ])
      ).stdout === "false",
  );
  assert.notEqual(
    (
      await docker([
        "inspect",
        "--format",
        "{{.State.ExitCode}}",
        names.rejected,
      ])
    ).stdout,
    "0",
  );
  const rejectedEvents = (await docker(["logs", names.rejected])).stderr
    .split(/\r?\n/)
    .filter((line) => line.trim())
    .map((line) => JSON.parse(line));
  assert.ok(
    rejectedEvents.some(
      (entry) =>
        entry.msg === "platform_event" &&
        entry.event === "server_lifecycle" &&
        entry.code === "run_failed",
    ),
    "Rejected startup must record the fixed run_failed lifecycle event.",
  );

  await startApp(join(directory, "bootstrap.env"));
  assert.equal(
    await appRequest("/readyz", [
      "--output",
      "/dev/null",
      "--write-out",
      "%{http_code}",
    ]),
    "503",
  );
  const system = JSON.parse(await appRequest("/api/v1/system"));
  assert.equal(system.code, "ok");
  assert.equal(system.data.demo, false);
  assert.equal(system.data.storage_driver, "s3");
  assert.equal(system.data.registration_enabled, false);
  assert.equal(system.data.payment_enabled, false);
  assert.equal(system.data.media_configured, false);
  assert.match(await appRequest("/"), /<script[^>]+src="\/assets\//);
  await verifyReleaseLicenses();
  const cookie = await login();
  const settings = JSON.parse(
    await appRequest("/api/v1/admin/settings", [
      "--header",
      `Cookie: ${cookie}`,
    ]),
  );
  assert.equal(settings.code, "ok");
  assert.equal(settings.data.settings.storage.driver, "s3");
  assert.equal(settings.data.readiness.smtp, false);
  assert.equal(settings.data.readiness.storage, false);

  await docker(["stop", "--time", "30", names.app], { timeout: 40000 });
  await docker(["rm", names.app]);
  await startApp(join(directory, "setup.env"));
  await login();
  assert.equal(
    await appRequest("/readyz", [
      "--output",
      "/dev/null",
      "--write-out",
      "%{http_code}",
    ]),
    "503",
  );
  console.log(
    "PASS: isolated production setup, release licenses, missing-admin rejection, healthz/readyz separation, secure admin login, and restart without bootstrap.",
  );
} catch (failure) {
  console.error(
    `FAIL: ${failure instanceof Error ? failure.message : "container smoke failed"}`,
  );
  process.exitCode = 1;
} finally {
  await cleanup();
}
