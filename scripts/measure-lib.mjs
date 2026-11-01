// Shared harness for scripts/measure-*.mjs: builds the Go API, boots it on a
// temporary SQLite file in sandbox demo mode and offers small HTTP and
// read-only database helpers. Nothing here is production code.
import { spawn, execFileSync } from "node:child_process";
import { randomBytes, randomUUID } from "node:crypto";
import { mkdtempSync, rmSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";
import { fileURLToPath } from "node:url";

const root = resolve(fileURLToPath(import.meta.url), "../..");

export const methods = {
  success: "00000000-0000-7000-8000-000000000121",
  decline: "00000000-0000-7000-8000-000000000122",
  delayed_success: "00000000-0000-7000-8000-000000000123",
  timeout_before_execution: "00000000-0000-7000-8000-000000000124",
  timeout_after_success: "00000000-0000-7000-8000-000000000125",
  outage: "00000000-0000-7000-8000-000000000126",
};

async function freePort() {
  const server = createServer();
  await new Promise((ok) => server.listen(0, "127.0.0.1", ok));
  const { port } = server.address();
  await new Promise((ok) => server.close(ok));
  return port;
}

export function percentile(sorted, p) {
  if (sorted.length === 0) return null;
  const index = Math.min(
    sorted.length - 1,
    Math.ceil((p / 100) * sorted.length) - 1,
  );
  return sorted[index];
}

export function summarize(values) {
  const sorted = [...values].sort((a, b) => a - b);
  const round = (v) => (v === null ? null : Math.round(v * 10) / 10);
  return {
    n: sorted.length,
    p50_ms: round(percentile(sorted, 50)),
    p95_ms: round(percentile(sorted, 95)),
    p99_ms: round(percentile(sorted, 99)),
    max_ms: round(sorted.at(-1) ?? null),
  };
}

/** Boots a fresh API; env overrides are merged over the measurement defaults. */
export async function boot(overrides = {}) {
  const dir = mkdtempSync(join(tmpdir(), "railvanta-measure-"));
  const binary = join(dir, "railvanta-api");
  const database = join(dir, "sandbox.sqlite");
  execFileSync("go", ["-C", "backend", "build", "-o", binary, "./cmd/api"], {
    cwd: root,
    stdio: "pipe",
  });
  const secret = () => randomBytes(32).toString("hex");
  const key = `operator.${secret()}`;
  const apiPort = await freePort();
  const healthPort = await freePort();
  const env = {
    ...process.env,
    APP_ENV: "sandbox",
    HTTP_ADDR: `127.0.0.1:${apiPort}`,
    HEALTH_ADDR: `127.0.0.1:${healthPort}`,
    SQLITE_PATH: database,
    PUBLIC_APP_ORIGIN: "http://127.0.0.1:3000",
    DEMO_MODE: "true",
    DEMO_OPERATOR_KEY: key,
    DEMO_READER_KEY: `reader.${secret()}`,
    DEMO_OTHER_WORKSPACE_KEY: `other.${secret()}`,
    DEMO_SESSION_TOKEN: secret(),
    DEMO_CSRF_TOKEN: secret(),
    DISPATCHER_POLL_INTERVAL: "50ms",
    PROVIDER_TIMEOUT: "30s",
    MOCK_DELAYED_SUCCESS_DELAY: "2s",
    MOCK_TIMEOUT_DELAY: "200ms",
    RECOVERY_BASE_BACKOFF: "100ms",
    RECOVERY_MAX_BACKOFF: "500ms",
    ...overrides,
  };
  let logs = "";
  const child = spawn(binary, [], {
    cwd: root,
    env,
    stdio: ["ignore", "pipe", "pipe"],
  });
  child.stdout.on("data", (d) => (logs = (logs + d).slice(-20000)));
  child.stderr.on("data", (d) => (logs = (logs + d).slice(-20000)));
  const base = `http://127.0.0.1:${apiPort}`;
  const deadline = Date.now() + 60000;
  for (;;) {
    if (child.exitCode !== null) throw new Error(`API exited early: ${logs}`);
    if (Date.now() > deadline) throw new Error(`API not ready: ${logs}`);
    try {
      const r = await fetch(`http://127.0.0.1:${healthPort}/health/ready`);
      if (r.ok) break;
    } catch {
      /* still starting */
    }
    await sleep(100);
  }
  // The API rate-limits each client address to 600 requests per fixed
  // one-minute window (backend/cmd/api/main.go), so the harness budgets its own
  // requests below that and waits for the window to roll over.
  const budget = 540;
  let windowStart = 0;
  let used = 0;
  async function reserve(count = 1) {
    for (;;) {
      const now = Date.now();
      if (now - windowStart > 61000) {
        windowStart = now;
        used = 0;
      }
      if (used + count <= budget) {
        used += count;
        return;
      }
      await sleep(windowStart + 61000 - now + 50);
    }
  }
  async function call(method, path, { body, headers, reserved } = {}) {
    if (!reserved) await reserve(1);
    const response = await fetch(base + path, {
      method,
      headers: {
        Authorization: `Bearer ${key}`,
        "Content-Type": "application/json",
        ...headers,
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await response.text();
    return { status: response.status, body: text ? JSON.parse(text) : null };
  }
  async function accept(
    methodId,
    {
      amount = "25000",
      idempotencyKey = `m_${randomUUID()}`,
      reserved = false,
    } = {},
  ) {
    return call("POST", "/v1/payments", {
      body: {
        amount_minor: amount,
        currency: "USD",
        payment_method_id: methodId,
        merchant_reference: `m_${idempotencyKey}`,
      },
      headers: { "Idempotency-Key": idempotencyKey },
      reserved,
    });
  }
  // Waits (reading the database, not the API) until every payment is terminal
  // and returns, per payment, the earliest audit-transition time (epoch ms) of
  // each status.
  function waitTerminal(ids, timeoutSeconds = 120) {
    const script = `
import json, sqlite3, sys, time
d = sqlite3.connect("file:" + sys.argv[1] + "?mode=ro", uri=True, timeout=10)
ids = json.loads(sys.argv[2]); deadline = time.time() + float(sys.argv[3])
while True:
    rows = {}
    for pid in ids:
        rows[pid] = {r[0]: r[1] for r in d.execute("SELECT to_status, min(created_at) FROM payment_state_transitions WHERE payment_id=? GROUP BY to_status", (pid,))}
    done = all(any(k in t for k in ("succeeded", "failed", "canceled")) for t in rows.values())
    if done or time.time() > deadline:
        break
    time.sleep(0.05)
print(json.dumps(rows))`;
    return JSON.parse(
      execFileSync(
        "python3",
        ["-c", script, database, JSON.stringify(ids), String(timeoutSeconds)],
        { encoding: "utf8", maxBuffer: 64 << 20 },
      ),
    );
  }
  // Read-only counts the API does not expose (mock provider charges).
  function inspect(paymentId) {
    const script = `
import json, sqlite3, sys
d = sqlite3.connect("file:" + sys.argv[1] + "?mode=ro", uri=True, timeout=10)
pid = sys.argv[2]
keys = [r[0] for r in d.execute("SELECT provider_idempotency_key FROM payment_attempts WHERE payment_id=?", (pid,))]
keys += ["rv_" + pid + "_" + str(n) for n in range(1, 4)]
charges = d.execute("SELECT count(*) FROM mock_provider_charges WHERE provider_idempotency_key IN (%s)" % ",".join("?" * len(keys)), keys).fetchone()[0]
journals = d.execute("SELECT count(*) FROM ledger_journals WHERE payment_id=?", (pid,)).fetchone()[0]
transitions = [r[0] for r in d.execute("SELECT to_status FROM payment_state_transitions WHERE payment_id=? ORDER BY created_at, rowid", (pid,))]
print(json.dumps({"charges": charges, "journals": journals, "transitions": transitions}))`;
    return JSON.parse(
      execFileSync("python3", ["-c", script, database, paymentId], {
        encoding: "utf8",
      }),
    );
  }
  async function stop() {
    child.kill("SIGTERM");
    await Promise.race([
      new Promise((ok) => child.once("exit", ok)),
      sleep(8000),
    ]);
    if (child.exitCode === null) child.kill("SIGKILL");
    rmSync(dir, { recursive: true, force: true });
  }
  return { base, call, accept, inspect, waitTerminal, reserve, stop, env };
}

export function environment() {
  const vcs = (...args) =>
    execFileSync("git", args, { cwd: root, encoding: "utf8" }).trim();
  return {
    date: new Date().toISOString(),
    commit: vcs("rev-parse", "--short", "HEAD"),
    uncommitted_tracked_changes:
      vcs("status", "--porcelain", "--untracked-files=no") !== "",
    node: process.version,
    go: execFileSync("go", ["version"], { encoding: "utf8" }).trim(),
    os: `${process.platform} ${process.arch}`,
    mode: "native process, temporary file-backed SQLite, mock provider",
  };
}
