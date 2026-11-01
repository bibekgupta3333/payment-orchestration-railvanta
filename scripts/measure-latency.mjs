#!/usr/bin/env node
// Measures acceptance latency, time-to-terminal and same-key concurrency on a
// freshly booted local sandbox API. Usage: node scripts/measure-latency.mjs [N]
// Results are local, single-machine, mock-provider numbers and are not a
// capacity claim. The API allows 600 requests per minute per client address,
// so the harness throttles itself (see measure-lib.mjs) and a run takes several
// minutes. Prints JSON.
import { boot, environment, methods, summarize } from "./measure-lib.mjs";

const N = Number(process.argv[2] ?? 200);
const api = await boot();
const out = {
  environment: environment(),
  config: {
    N,
    dispatcher_poll: "50ms",
    dispatcher_max_in_flight: "4 (default)",
    client_request_budget:
      "540 per 61s window (API limit is 600/min per address)",
  },
};

// Server-side durations (ms) from the audit transitions: created -> status.
function durations(timings, status) {
  return Object.values(timings)
    .filter((t) => t.created !== undefined && t[status] !== undefined)
    .map((t) => t[status] - t.created);
}

try {
  for (let i = 0; i < 10; i++) await api.accept(methods.success); // warm-up

  // (1) Sequential acceptance latency (client-observed HTTP round trip).
  const acceptance = [];
  await api.reserve(N); // budget waits must never land inside a timed region
  for (let i = 0; i < N; i++) {
    const t = performance.now();
    const r = await api.accept(methods.success, { reserved: true });
    acceptance.push(performance.now() - t);
    if (r.status !== 202) throw new Error(`accept returned ${r.status}`);
  }
  out.acceptance_sequential = summarize(acceptance);

  // (2) Time to terminal, one payment at a time on an otherwise idle API.
  const M = Math.min(N, 100);
  const timings = {};
  for (let i = 0; i < M; i++) {
    const r = await api.accept(methods.success);
    Object.assign(timings, api.waitTerminal([r.body.id], 30));
  }
  out.time_to_terminal_success_idle = {
    ...summarize(durations(timings, "succeeded")),
    note: "server-side: created transition to succeeded transition (ms timestamps, so +/-1ms); includes dispatcher poll wait of up to 50ms unless the accept notification wakes it",
  };

  // (3) N accepts at several client concurrency levels, then drain to terminal.
  out.acceptance_concurrent = {};
  for (const concurrency of [1, 8, 32]) {
    const lat = [];
    const ids = [];
    await api.reserve(N);
    const start = performance.now();
    let next = 0;
    await Promise.all(
      Array.from({ length: concurrency }, async () => {
        while (next < N) {
          next++;
          const t = performance.now();
          const r = await api.accept(methods.success, { reserved: true });
          lat.push(performance.now() - t);
          if (r.status === 202) ids.push(r.body.id);
        }
      }),
    );
    const acceptedMs = performance.now() - start;
    const all = api.waitTerminal(ids, 120);
    out.acceptance_concurrent[`c${concurrency}`] = {
      ...summarize(lat),
      accepted: ids.length,
      accept_throughput_per_s: Math.round((ids.length / acceptedMs) * 1000),
      all_terminal_after_ms: Math.round(performance.now() - start),
      terminal_succeeded: Object.values(all).filter(
        (t) => t.succeeded !== undefined,
      ).length,
      time_to_terminal_under_load: summarize(durations(all, "succeeded")),
    };
  }

  // (4) Concurrent same-key requests: one payment, one charge, one journal.
  out.same_key = [];
  for (const concurrency of [2, 8, 32]) {
    const results = [];
    for (let round = 0; round < 5; round++) {
      const key = `m_samekey_${round}_${concurrency}_${Date.now()}`;
      await api.reserve(concurrency);
      const rs = await Promise.all(
        Array.from({ length: concurrency }, () =>
          api.accept(methods.success, {
            amount: "777",
            idempotencyKey: key,
            reserved: true,
          }),
        ),
      );
      const ids = new Set(rs.filter((r) => r.body?.id).map((r) => r.body.id));
      const statuses = {};
      for (const r of rs) statuses[r.status] = (statuses[r.status] ?? 0) + 1;
      const id = [...ids][0];
      api.waitTerminal([id], 30);
      await new Promise((ok) => setTimeout(ok, 300));
      const inspection = api.inspect(id);
      results.push({
        distinct_payment_ids: ids.size,
        statuses,
        charges: inspection.charges,
        journals: inspection.journals,
      });
    }
    const totals = {};
    for (const r of results)
      for (const [s, c] of Object.entries(r.statuses))
        totals[s] = (totals[s] ?? 0) + c;
    out.same_key.push({
      concurrency,
      rounds: results.length,
      max_distinct_payment_ids: Math.max(
        ...results.map((r) => r.distinct_payment_ids),
      ),
      http_status_totals: totals,
      max_charges: Math.max(...results.map((r) => r.charges)),
      journals_per_round: [...new Set(results.map((r) => r.journals))],
    });
  }
} finally {
  await api.stop();
}
console.log(JSON.stringify(out, null, 2));
