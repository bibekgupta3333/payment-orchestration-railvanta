import { spawn, execFileSync, type ChildProcess } from "node:child_process";
import { randomBytes, timingSafeEqual } from "node:crypto";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { createServer, type IncomingMessage } from "node:http";
import { createServer as createNetServer } from "node:net";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";
import { setTimeout as pause } from "node:timers/promises";
import type { HarnessState } from "./state";

const root = resolve(__dirname, "../..");
const allocatedPorts = new Set<number>();

async function freePort(): Promise<number> {
  const server = createNetServer();
  await new Promise<void>((accept, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", accept);
  });
  const address = server.address();
  if (!address || typeof address === "string")
    throw new Error("No ephemeral port");
  await new Promise<void>((accept, reject) =>
    server.close((error) => (error ? reject(error) : accept())),
  );
  if (allocatedPorts.has(address.port)) return freePort();
  allocatedPorts.add(address.port);
  return address.port;
}

function signal(child: ChildProcess, value: NodeJS.Signals): void {
  if (!child.pid || child.exitCode !== null || child.signalCode !== null)
    return;
  try {
    if (process.platform !== "win32") process.kill(-child.pid, value);
    else child.kill(value);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ESRCH") throw error;
  }
}

async function stop(
  child: ChildProcess | undefined,
  hard = false,
): Promise<void> {
  if (!child?.pid || child.exitCode !== null || child.signalCode !== null)
    return;
  const exited = new Promise<void>((accept) =>
    child.once("exit", () => accept()),
  );
  signal(child, hard ? "SIGKILL" : "SIGTERM");
  await Promise.race([
    exited,
    pause(8_000, undefined, { ref: false }).then(() =>
      signal(child, "SIGKILL"),
    ),
  ]);
  if (child.exitCode === null && child.signalCode === null) await exited;
}

async function ready(
  url: string,
  child: ChildProcess,
  logs: () => string,
): Promise<void> {
  const deadline = Date.now() + 60_000;
  while (Date.now() < deadline) {
    if (child.exitCode !== null || child.signalCode !== null) {
      throw new Error(
        `Service exited before readiness (${child.exitCode ?? child.signalCode}): ${logs()}`,
      );
    }
    try {
      const response = await fetch(url, { signal: AbortSignal.timeout(1_000) });
      if (response.ok) return;
    } catch {
      /* A refused connection is expected during startup. */
    }
    await pause(100);
  }
  throw new Error(`Service readiness timed out: ${logs()}`);
}

export default async function globalSetup(): Promise<() => Promise<void>> {
  const directory = mkdtempSync(join(tmpdir(), "railvanta-e2e-"));
  const database = join(directory, "sandbox.sqlite");
  const binary = join(directory, "railvanta-api");
  let api: ChildProcess | undefined;
  let web: ChildProcess | undefined;
  let logs = "";
  let webLogs = "";
  const secret = () => randomBytes(32).toString("hex");
  const apiPort = await freePort();
  const healthPort = await freePort();
  const webPort = await freePort();
  const current: HarnessState = {
    apiOrigin: `http://127.0.0.1:${apiPort}`,
    healthOrigin: `http://127.0.0.1:${healthPort}`,
    webOrigin: `http://127.0.0.1:${webPort}`,
    controlOrigin: "",
    controlToken: secret(),
    operatorKey: `operator.${secret()}`,
    readerKey: `reader.${secret()}`,
    otherKey: `other.${secret()}`,
    sessionToken: secret(),
    csrfToken: secret(),
  };
  const environment = {
    ...process.env,
    APP_ENV: "sandbox",
    HTTP_ADDR: `127.0.0.1:${apiPort}`,
    HEALTH_ADDR: `127.0.0.1:${healthPort}`,
    SQLITE_PATH: database,
    PUBLIC_APP_ORIGIN: current.webOrigin,
    DEMO_MODE: "true",
    DEMO_OPERATOR_KEY: current.operatorKey,
    DEMO_READER_KEY: current.readerKey,
    DEMO_OTHER_WORKSPACE_KEY: current.otherKey,
    DEMO_SESSION_TOKEN: current.sessionToken,
    DEMO_CSRF_TOKEN: current.csrfToken,
    // Fast dispatch for black-box specs; a long delayed-success window lets
    // dispatch.spec kill the API while an attempt is submitted.
    DISPATCHER_POLL_INTERVAL: "50ms",
    DISPATCHER_LEASE: "2s",
    DISPATCHER_DRAIN_TIMEOUT: "1s",
    PROVIDER_TIMEOUT: "30s",
    MOCK_DELAYED_SUCCESS_DELAY: "8s",
    MOCK_TIMEOUT_DELAY: "200ms",
    // Quick lookups while an outcome stays unknown (WBS 2.4).
    RECOVERY_BASE_BACKOFF: "100ms",
    RECOVERY_MAX_BACKOFF: "500ms",
  };
  function launchApi(env = environment): ChildProcess {
    const child = spawn(binary, [], {
      cwd: root,
      env,
      detached: process.platform !== "win32",
      stdio: ["ignore", "pipe", "pipe"],
    });
    child.stdout?.on("data", (data: Buffer) => {
      logs = (logs + data.toString()).slice(-100_000);
    });
    child.stderr?.on("data", (data: Buffer) => {
      logs = (logs + data.toString()).slice(-100_000);
    });
    child.on("error", (error) => {
      logs += error.message;
    });
    return child;
  }
  async function startApi(): Promise<void> {
    api = launchApi();
    await ready(`${current.healthOrigin}/health/ready`, api, () => logs);
  }
  function sqlite(script: string): string {
    return execFileSync("python3", ["-c", script, database], {
      encoding: "utf8",
    }).trim();
  }
  async function isolatedStartupFailure(
    kind: "production" | "checksum-drift",
  ): Promise<number> {
    await stop(api);
    let checksum = "";
    if (kind === "checksum-drift") {
      checksum = sqlite(
        "import sqlite3,sys; d=sqlite3.connect(sys.argv[1]); print(d.execute('SELECT checksum FROM schema_migrations WHERE version=1').fetchone()[0]); d.execute(\"UPDATE schema_migrations SET checksum=? WHERE version=1\",('0'*64,)); d.commit(); d.close()",
      );
    }
    let failing: ChildProcess | undefined;
    try {
      failing = launchApi(
        kind === "production"
          ? { ...environment, APP_ENV: "production" }
          : environment,
      );
      const code = await Promise.race([
        new Promise<number>((accept, reject) => {
          failing?.once("error", reject);
          failing?.once("exit", (value) => accept(value ?? 1));
        }),
        pause(10_000).then(() => {
          throw new Error("Invalid startup did not terminate");
        }),
      ]);
      if (code === 0) throw new Error("Invalid startup returned success");
      return code;
    } finally {
      await stop(failing, true);
      if (kind === "checksum-drift") {
        execFileSync("python3", [
          "-c",
          "import sqlite3,sys; d=sqlite3.connect(sys.argv[1]); d.execute('UPDATE schema_migrations SET checksum=? WHERE version=1',(sys.argv[2],)); d.commit(); d.close()",
          database,
          checksum,
        ]);
      }
      await startApi();
    }
  }
  // Read-only inspection of rows the API never exposes (provider idempotency
  // keys, mock charges, outbox rows) for one payment.
  function inspect(paymentId: string): Record<string, unknown> {
    const script = `
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
    return JSON.parse(
      execFileSync("python3", ["-c", script, database, paymentId], {
        encoding: "utf8",
      }),
    ) as Record<string, unknown>;
  }
  // Re-delivers a payment's dispatch command, as if its completion had been
  // lost, so the running dispatcher processes it again.
  function redeliver(paymentId: string): void {
    execFileSync("python3", [
      "-c",
      `import sqlite3,sys; d=sqlite3.connect(sys.argv[1], timeout=10); d.execute("UPDATE outbox_events SET status='pending', delivered_at=NULL, available_at=0 WHERE payment_id=? AND event_type='payment.dispatch'", (sys.argv[2],)); d.commit(); d.close()`,
      database,
      paymentId,
    ]);
  }
  // Inserts a payment that stays \`created\`: the API is stopped, a payment and
  // a dispatch command due in an hour are written, and the API restarts. This
  // makes "cancel while created" deterministic for black-box specs.
  async function seedCreated(method: string): Promise<string> {
    if (!/^[0-9a-f-]{36}$/.test(method))
      throw new Error("method must be a UUID");
    await stop(api);
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
      return sqlite(script);
    } finally {
      await startApi();
    }
  }
  async function readBody(request: IncomingMessage): Promise<string> {
    const chunks: Buffer[] = [];
    for await (const chunk of request) chunks.push(chunk as Buffer);
    return Buffer.concat(chunks).toString("utf8");
  }
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
      const url = new URL(request.url ?? "/", "http://control.invalid");
      if (request.url === "/restart" || request.url === "/kill") {
        await stop(api, request.url === "/kill");
        await startApi();
      } else if (request.url === "/stop") {
        await stop(api);
      } else if (request.url === "/start") {
        if (!api || api.exitCode !== null || api.signalCode !== null)
          await startApi();
      } else if (request.url === "/durability-fixture") {
        await stop(api);
        try {
          sqlite(
            `import sqlite3,sys; d=sqlite3.connect(sys.argv[1]); d.execute("PRAGMA foreign_keys=ON"); d.execute("INSERT INTO payment_methods(id,workspace_id,rail,brand,last_four,created_at) VALUES('00000000-0000-7000-8000-000000000199','00000000-0000-7000-8000-000000000001','mock','Durability fixture','9999',17)"); d.commit(); d.close()`,
          );
        } finally {
          await startApi();
        }
      } else if (
        request.url === "/production" ||
        request.url === "/checksum-drift"
      ) {
        body = {
          exitCode: await isolatedStartupFailure(
            request.url.slice(1) as "production" | "checksum-drift",
          ),
        };
      } else if (request.url === "/seed-created") {
        const { method } = JSON.parse(await readBody(request)) as {
          method?: unknown;
        };
        if (typeof method !== "string") throw new Error("method is required");
        body = { paymentId: await seedCreated(method) };
      } else if (request.url === "/logs") {
        body = { logs };
      } else if (request.url === "/inspect" || request.url === "/redeliver") {
        const { paymentId } = JSON.parse(await readBody(request)) as {
          paymentId?: unknown;
        };
        if (typeof paymentId !== "string" || !/^[0-9a-f-]{36}$/.test(paymentId))
          throw new Error("paymentId must be a UUID");
        if (request.url === "/redeliver") redeliver(paymentId);
        else body = inspect(paymentId);
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
  async function cleanup(): Promise<void> {
    await stop(web);
    await stop(api);
    await new Promise<void>((accept) => {
      if (!control.listening) {
        accept();
        return;
      }
      control.close(() => accept());
      control.closeAllConnections();
    });
    rmSync(directory, { recursive: true, force: true });
  }
  try {
    execFileSync("go", ["-C", "backend", "build", "-o", binary, "./cmd/api"], {
      cwd: root,
      env: process.env,
      stdio: "pipe",
      timeout: 180_000,
    });
    await startApi();
    // Scaffold API specs exercise the /v1 proxy, so they also need Next.js.
    web = spawn(
      "pnpm",
      [
        "--filter",
        "@railvanta/frontend",
        "exec",
        "next",
        "dev",
        "--webpack",
        "--hostname",
        "127.0.0.1",
        "--port",
        String(webPort),
      ],
      {
        cwd: root,
        env: {
          ...process.env,
          API_INTERNAL_ORIGIN: current.apiOrigin,
          NEXT_TELEMETRY_DISABLED: "1",
        },
        detached: process.platform !== "win32",
        stdio: ["ignore", "pipe", "pipe"],
      },
    );
    web.stdout?.on("data", (data: Buffer) => {
      webLogs = (webLogs + data.toString()).slice(-30_000);
    });
    web.stderr?.on("data", (data: Buffer) => {
      webLogs = (webLogs + data.toString()).slice(-30_000);
    });
    web.on("error", (error) => {
      webLogs += error.message;
    });
    await ready(current.webOrigin, web, () => webLogs);
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
    return cleanup;
  } catch (error) {
    await cleanup();
    throw error;
  }
}
