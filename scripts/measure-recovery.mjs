#!/usr/bin/env node
// Runs each deterministic mock scenario through the live sandbox API and prints
// the final payment/attempt state, provider charge count, journal count and
// time to resolution. Usage: node scripts/measure-recovery.mjs [outageWindowMs]
import { boot, environment, methods } from "./measure-lib.mjs";
import { setTimeout as sleep } from "node:timers/promises";

const outageWindow = Number(process.argv[2] ?? 15000);
const terminal = new Set(["succeeded", "failed", "canceled"]);
const api = await boot();
const rows = [];
try {
  for (const [scenario, method] of Object.entries(methods)) {
    const t0 = performance.now();
    const accepted = await api.accept(method, { amount: "31000" });
    const id = accepted.body.id;
    const limit = scenario === "outage" ? outageWindow : 30000;
    // Wait via the database (the API rate-limits polling); time to resolution
    // is the server-side created -> terminal transition gap.
    const timing = api.waitTerminal([id], limit / 1000)[id];
    const terminalAt = [...terminal]
      .map((s) => timing[s])
      .find((v) => v !== undefined);
    const resolved = terminalAt !== undefined;
    const ms = resolved ? terminalAt - timing.created : null;
    await sleep(300);
    const payment = (await api.call("GET", `/v1/payments/${id}`)).body;
    const attemptsResponse = await api.call(
      "GET",
      `/v1/payments/${id}/attempts`,
    );
    if (attemptsResponse.status !== 200)
      throw new Error(
        `${scenario}: attempts ${attemptsResponse.status} ${JSON.stringify(attemptsResponse.body)}`,
      );
    const attempts = attemptsResponse.body.items;
    const recovery = (await api.call("GET", `/v1/payments/${id}/recovery`))
      .body;
    const inspection = api.inspect(id);
    rows.push({
      scenario,
      http_accept: accepted.status,
      final_payment_status: payment.status,
      attempt_statuses: attempts.map((a) => a.status),
      attempts: attempts.length,
      provider_charges: inspection.charges,
      journals: inspection.journals,
      transitions: inspection.transitions,
      lookup_status: recovery.lookup_status,
      lookup_attempts: recovery.lookup_attempts,
      retry_allowed: recovery.retry_allowed,
      reached_terminal: resolved,
      time_ms: ms,
      observation_window_ms: resolved ? null : limit,
    });
  }
} finally {
  await api.stop();
}
console.log(
  JSON.stringify(
    {
      environment: environment(),
      config: {
        dispatcher_poll: "50ms",
        mock_delayed_success_delay: "2s",
        mock_timeout_delay: "200ms",
        provider_timeout: "30s",
        recovery_backoff: "100ms..500ms",
      },
      scenarios: rows,
    },
    null,
    2,
  ),
);
