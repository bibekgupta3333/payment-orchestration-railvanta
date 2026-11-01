import { execFileSync, spawnSync } from "node:child_process";
import { randomBytes, timingSafeEqual } from "node:crypto";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { createServer, type IncomingMessage } from "node:http";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { setTimeout as pause } from "node:timers/promises";
import type { HarnessState } from "./state";

/**
 * Container mode (E2E_BASE_URL set): the suite runs against an already started
 * `docker compose` stack at one origin instead of booting native processes.
 * The harness-only controls (restart, kill, stop, start, logs, inspect,
 * redeliver, seed-created, durability-fixture, production, checksum-drift) are
 * implemented with `docker compose` and one-off python containers that mount
 * the data volume only while the API container is stopped (or read-only for
 * inspection). Compose project, files and overrides come from the standard
 * COMPOSE_* environment variables, exactly as for `pnpm compose:up`.
 */
const root = resolve(__dirname, "../..");
// Harness-only helper image (python's sqlite3 module); pinned by digest.
const pythonImage =
  "python:3.13-alpine@sha256:7984cf56ceaddebe4fefeb75d64000b9f45d8b77b11a09b24524da6cdc3af0c8";
const dataDir = "/var/lib/railvanta";

export function compose(args: string[], timeout = 120_000): string {
  return execFileSync("docker", ["compose", ...args], {
    cwd: root,
    env: process.env,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
    timeout,
  }).trim();
}

/** The data volume the API container currently mounts (changes after a restore). */
export function dataVolume(): string {
  const id = compose(["ps", "-a", "-q", "api"]);
  return execFileSync(
    "docker",
    ["inspect", id, "--format", "{{range .Mounts}}{{.Name}}{{end}}"],
    { encoding: "utf8" },
  ).trim();
}

export function sqlite(script: string, ...args: string[]): string {
  return execFileSync(
    "docker",
    [
      "run",
      "--rm",
      "--user",
      "10001:10001",
      "--cap-drop",
      "ALL",
      "-v",
      `${dataVolume()}:${dataDir}`,
      pythonImage,
      "python3",
      "-c",
      script,
      `${dataDir}/railvanta.db`,
      ...args,
    ],
    { encoding: "utf8", timeout: 60_000 },
  ).trim();
}

export async function waitApiReady(timeoutMs = 60_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let last = "";
  while (Date.now() < deadline) {
    const probe = spawnSync(
      "docker",
      [
        "compose",
        "exec",
        "-T",
        "api",
        "wget",
        "-q",
        "-O",
        "/dev/null",
        "http://127.0.0.1:8081/health/ready",
      ],
      { cwd: root, env: process.env, encoding: "utf8", timeout: 10_000 },
    );
    if (probe.status === 0) return;
    last = `${probe.stderr}${probe.stdout}`;
    await pause(250);
  }
  throw new Error(`API container did not become ready: ${last}`);
}

/** Waits until the public proxy forwards /v1 to the (possibly new) API container. */
export async function waitProxyReady(
  origin: string,
  timeoutMs = 60_000,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${origin}/v1/workspace`, {
        signal: AbortSignal.timeout(2_000),
      });
      if (response.status === 401 || response.status === 200) return;
    } catch {
      /* the proxy may still be reconnecting */
    }
    await pause(250);
  }
  throw new Error("The proxy did not forward /v1 to the API");
}

export function seedDemo(keys: {
  operator: string;
  reader: string;
  other: string;
  session: string;
  csrf: string;
}): void {
  try {
    compose([
      "exec",
      "-T",
      "-e",
      `DEMO_OPERATOR_KEY=${keys.operator}`,
      "-e",
      `DEMO_READER_KEY=${keys.reader}`,
      "-e",
      `DEMO_OTHER_WORKSPACE_KEY=${keys.other}`,
      "-e",
      `DEMO_SESSION_TOKEN=${keys.session}`,
      "-e",
      `DEMO_CSRF_TOKEN=${keys.csrf}`,
      "api",
      "railvanta-api",
      "seed-demo",
    ]);
  } catch {
    throw new Error(
      "seed-demo failed: container E2E needs a fresh sandbox volume (docker compose down -v, then compose up) or reuse the cached credentials",
    );
  }
}

const inspectScript = `
import json, sqlite3, sys
d = sqlite3.connect("file:" + sys.argv[1] + "?mode=ro", uri=True, timeout=10)
d.row_factory = sqlite3.Row
pid = sys.argv[2]
attempts = [dict(r) for r in d.execute("SELECT id, provider_id, provider_idempotency_key, status FROM payment_attempts WHERE payment_id=? ORDER BY attempt_number", (pid,))]
keys = [a["provider_idempotency_key"] for a in attempts] + ["rv_" + pid + "_" + str(n) for n in range(1, 4)]
charges = [dict(r) for r in d.execute("SELECT provider_id, provider_idempotency_key, status FROM mock_provider_charges WHERE provider_idempotency_key IN (%s)" % ",".join("?" * len(keys)), keys)]
outbox = [dict(r) for r in d.execute("SELECT event_type, status, attempts FROM outbox_events WHERE payment_id=? ORDER BY created_at, id", (pid,))]
journals = d.execute("SELECT count(*) FROM ledger_journals WHERE payment_id=?", (pid,)).fetchone()[0]
transitions = [r[0] for r in d.execute("SELECT to_status FROM payment_state_transitions WHERE payment_id=? ORDER BY created_at, rowid", (pid,))]
print(json.dumps({"attempts": attempts, "charges": charges, "outbox": outbox, "journals": journals, "transitions": transitions}))
d.close()`;

async function readBody(request: IncomingMessage): Promise<string> {
  const chunks: Buffer[] = [];
  for await (const chunk of request) chunks.push(chunk as Buffer);
  return Buffer.concat(chunks).toString("utf8");
}

export default async function containerGlobalSetup(): Promise<
  () => Promise<void>
> {
  const origin = (process.env.E2E_BASE_URL as string).replace(/\/$/, "");
  const directory = mkdtempSync(join(tmpdir(), "railvanta-e2e-containers-"));
  const secret = () => randomBytes(32).toString("hex");
  // seed-demo refuses different credentials in an existing database, so the
  // generated credentials are cached (0600, outside the repo) per Compose project
  // and reused on every run, including against a freshly created or restored volume.
  const credentialFile = join(
    tmpdir(),
    `railvanta-e2e-credentials-${process.env.COMPOSE_PROJECT_NAME ?? "railvanta"}.json`,
  );
  const fresh = {
    operatorKey: `operator.${secret()}`,
    readerKey: `reader.${secret()}`,
    otherKey: `other.${secret()}`,
    sessionToken: secret(),
    csrfToken: secret(),
  };
  let credentials = fresh;
  try {
    credentials = JSON.parse(
      readFileSync(credentialFile, "utf8"),
    ) as typeof fresh;
  } catch {
    writeFileSync(credentialFile, JSON.stringify(fresh), { mode: 0o600 });
  }
  const current: HarnessState = {
    apiOrigin: origin,
    // API health is private to the container; the ops specs probe it with exec.
    healthOrigin: "http://127.0.0.1:9",
    webOrigin: origin,
    controlOrigin: "",
    controlToken: secret(),
    ...credentials,
  };
  await waitApiReady();
  await waitProxyReady(origin);
  seedDemo({
    operator: current.operatorKey,
    reader: current.readerKey,
    other: current.otherKey,
    session: current.sessionToken,
    csrf: current.csrfToken,
  });

  const startApi = async () => {
    compose(["start", "api"]);
    await waitApiReady();
    await waitProxyReady(origin);
  };
  const stopApi = (hard = false) =>
    compose(hard ? ["kill", "api"] : ["stop", "api"], 60_000);
  // One-off API container that must refuse to start; returns its exit code.
  const refusedStartup = (extra: string[]): number => {
    const run = spawnSync(
      "docker",
      ["compose", "run", "--rm", "--no-deps", "-T", ...extra, "api"],
      { cwd: root, env: process.env, encoding: "utf8", timeout: 40_000 },
    );
    if (run.status === null)
      throw new Error("Invalid startup did not terminate");
    if (run.status === 0) throw new Error("Invalid startup returned success");
    return run.status;
  };

  const control = createServer(async (request, response) => {
    const incoming = Buffer.from(request.headers.authorization ?? "");
    const expected = Buffer.from(`Bearer ${current.controlToken}`);
    if (
      request.method !== "POST" ||
      incoming.length !== expected.length ||
      !timingSafeEqual(incoming, expected)
    ) {
      response.writeHead(403).end();
      return;
    }
    try {
      let body: Record<string, unknown> = { ok: true };
      if (request.url === "/restart") {
        stopApi();
        await startApi();
      } else if (request.url === "/kill") {
        stopApi(true);
        await startApi();
      } else if (request.url === "/stop") {
        stopApi();
      } else if (request.url === "/start") {
        await startApi();
      } else if (request.url === "/durability-fixture") {
        stopApi();
        try {
          sqlite(
            `import sqlite3,sys; d=sqlite3.connect(sys.argv[1]); d.execute("PRAGMA foreign_keys=ON"); d.execute("INSERT INTO payment_methods(id,workspace_id,rail,brand,last_four,created_at) VALUES('00000000-0000-7000-8000-000000000199','00000000-0000-7000-8000-000000000001','mock','Durability fixture','9999',17)"); d.commit(); d.close()`,
          );
        } finally {
          await startApi();
        }
      } else if (request.url === "/production") {
        body = { exitCode: refusedStartup(["-e", "APP_ENV=production"]) };
      } else if (request.url === "/checksum-drift") {
        stopApi();
        const original = sqlite(
          "import sqlite3,sys; d=sqlite3.connect(sys.argv[1]); print(d.execute('SELECT checksum FROM schema_migrations WHERE version=1').fetchone()[0]); d.execute(\"UPDATE schema_migrations SET checksum=? WHERE version=1\",('0'*64,)); d.commit(); d.close()",
        );
        try {
          body = { exitCode: refusedStartup([]) };
        } finally {
          sqlite(
            "import sqlite3,sys; d=sqlite3.connect(sys.argv[1]); d.execute('UPDATE schema_migrations SET checksum=? WHERE version=1',(sys.argv[2],)); d.commit(); d.close()",
            original,
          );
          await startApi();
        }
      } else if (request.url === "/seed-created") {
        const { method } = JSON.parse(await readBody(request)) as {
          method?: unknown;
        };
        if (typeof method !== "string" || !/^[0-9a-f-]{36}$/.test(method))
          throw new Error("method must be a UUID");
        stopApi();
        try {
          const script = [
            "import sqlite3,sys,uuid,time",
            "d=sqlite3.connect(sys.argv[1]); d.execute('PRAGMA foreign_keys=ON')",
            "pid=str(uuid.uuid4()); w='00000000-0000-7000-8000-000000000001'; now=int(time.time()*1000)",
            `d.execute("INSERT INTO payments(id,workspace_id,payment_method_id,amount_minor,currency,status,version,created_at,updated_at) VALUES(?,?,?,12345,'USD','created',1,?,?)",(pid,w,'${method}',now,now))`,
            `d.execute("INSERT INTO payment_state_transitions(id,workspace_id,payment_id,from_status,to_status,reason,actor_reference,created_at) VALUES(?,?,?,NULL,'created','seeded by e2e','e2e',?)",(str(uuid.uuid4()),w,pid,now))`,
            `d.execute("INSERT INTO outbox_events(id,workspace_id,payment_id,event_type,deduplication_key,payload,status,attempts,available_at,created_at) VALUES(?,?,?,'payment.dispatch',?,?,'pending',0,?,?)",(str(uuid.uuid4()),w,pid,'payment.dispatch:'+pid,'{"payment_id":"'+pid+'","workspace_id":"'+w+'"}',now+3600000,now))`,
            "d.commit(); d.close(); print(pid)",
          ].join("\n");
          body = { paymentId: sqlite(script) };
        } finally {
          await startApi();
        }
      } else if (request.url === "/logs") {
        body = { logs: compose(["logs", "--no-color", "api"]).slice(-100_000) };
      } else if (request.url === "/inspect" || request.url === "/redeliver") {
        const { paymentId } = JSON.parse(await readBody(request)) as {
          paymentId?: unknown;
        };
        if (typeof paymentId !== "string" || !/^[0-9a-f-]{36}$/.test(paymentId))
          throw new Error("paymentId must be a UUID");
        if (request.url === "/redeliver")
          sqlite(
            `import sqlite3,sys; d=sqlite3.connect(sys.argv[1], timeout=10); d.execute("UPDATE outbox_events SET status='pending', delivered_at=NULL, available_at=0 WHERE payment_id=? AND event_type='payment.dispatch'", (sys.argv[2],)); d.commit(); d.close()`,
            paymentId,
          );
        else body = JSON.parse(sqlite(inspectScript, paymentId));
      } else {
        response.writeHead(404).end();
        return;
      }
      response
        .writeHead(200, { "Content-Type": "application/json" })
        .end(JSON.stringify(body));
    } catch (error) {
      response
        .writeHead(500)
        .end(
          error instanceof Error ? error.message : "Harness operation failed",
        );
    }
  });
  await new Promise<void>((accept, reject) => {
    control.once("error", reject);
    control.listen(0, "127.0.0.1", accept);
  });
  const address = control.address();
  if (!address || typeof address === "string")
    throw new Error("No control address");
  current.controlOrigin = `http://127.0.0.1:${address.port}`;
  const stateFile = join(directory, "state.json");
  writeFileSync(stateFile, JSON.stringify(current), { mode: 0o600 });
  process.env.RAILVANTA_E2E_STATE = stateFile;
  return async () => {
    await new Promise<void>((accept) => {
      control.close(() => accept());
      control.closeAllConnections();
    });
    rmSync(directory, { recursive: true, force: true });
  };
}
