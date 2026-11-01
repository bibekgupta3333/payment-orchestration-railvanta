#!/usr/bin/env node
// Scripted central demo: accept a payment, lose the provider response, observe
// `unknown`, watch the original provider resolve it by lookup, and assert one
// attempt, one provider charge and one balanced journal. Boots its own sandbox
// API on a temporary database. Exits non-zero when any assertion fails.
// Usage: node scripts/measure-central-demo.mjs
import assert from "node:assert/strict";
import { boot, methods } from "./measure-lib.mjs";

const api = await boot();
try {
  const accepted = await api.accept(methods.timeout_after_success, {
    amount: "31000",
  });
  assert.equal(accepted.status, 202);
  const id = accepted.body.id;
  console.log(
    `1. accepted payment ${id} (202, status ${accepted.body.status})`,
  );

  const timing = api.waitTerminal([id], 30)[id];
  assert.ok(timing.succeeded !== undefined, "payment did not reach succeeded");
  const inspection = api.inspect(id);
  console.log(`2. audit transitions: ${inspection.transitions.join(" -> ")}`);
  assert.deepEqual(inspection.transitions, [
    "created",
    "processing",
    "unknown",
    "succeeded",
  ]);
  console.log(
    `   unknown at +${timing.unknown - timing.created}ms, resolved at +${timing.succeeded - timing.created}ms`,
  );

  const attempts = (await api.call("GET", `/v1/payments/${id}/attempts`)).body
    .items;
  assert.equal(attempts.length, 1, "exactly one attempt, no failover");
  console.log(
    `3. attempts: ${attempts.length} (provider ${attempts[0].provider_id}, status ${attempts[0].status})`,
  );

  assert.equal(inspection.charges, 1, "exactly one provider charge");
  console.log(`4. provider charges for this payment: ${inspection.charges}`);

  const journals = (
    await api.call("GET", `/v1/ledger/transactions?payment_id=${id}`)
  ).body.items;
  assert.equal(journals.length, 1, "exactly one journal");
  const sum = (side) =>
    journals[0].entries
      .filter((e) => e.side === side)
      .reduce((total, e) => total + BigInt(e.amount_minor), 0n);
  assert.equal(sum("debit"), 31000n);
  assert.equal(sum("credit"), 31000n);
  console.log(
    `5. journals: 1, debits ${sum("debit")} = credits ${sum("credit")} (minor units)`,
  );

  const recovery = (await api.call("GET", `/v1/payments/${id}/recovery`)).body;
  assert.equal(recovery.lookup_status, "resolved");
  console.log(
    `6. recovery: lookup_status=${recovery.lookup_status}, lookups=${recovery.lookup_attempts}, retry_allowed=${recovery.retry_allowed}`,
  );
  console.log("central demo: PASS");
} finally {
  await api.stop();
}
