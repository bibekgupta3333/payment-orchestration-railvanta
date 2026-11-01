import { test, expect } from "@playwright/test";
import { ApiClient } from "../support/api";
import {
  createPayment,
  journalsFor,
  listAttempts,
  waitForStatus,
  type Journal,
} from "../support/payments";
import {
  fixtures,
  inspectPayment,
  redeliverDispatch,
  restartApi,
  seedCreatedPayment,
  state,
} from "../support/state";

function totals(journal: Journal): { debits: bigint; credits: bigint } {
  let debits = 0n;
  let credits = 0n;
  for (const entry of journal.entries) {
    if (entry.side === "debit") debits += BigInt(entry.amount_minor);
    else credits += BigInt(entry.amount_minor);
  }
  return { debits, credits };
}

test("a succeeded payment has exactly one balanced posted journal in its currency", async ({
  request,
}) => {
  const api = new ApiClient(request);
  // Above Number.MAX_SAFE_INTEGER so the amount must stay a decimal string.
  const amount = "9007199254740993";
  const accepted = await createPayment(
    api,
    fixtures.successMethod,
    amount,
    "GBP",
  );
  await waitForStatus(api, accepted.id, "succeeded");
  const journals = await journalsFor(api, accepted.id);
  expect(journals).toHaveLength(1);
  const [journal] = journals;
  expect(journal).toMatchObject({
    payment_id: accepted.id,
    currency: "GBP",
    status: "posted",
    posting_key: `payment:${accepted.id}:success`,
  });
  expect(journal.entries).toHaveLength(2);
  const { debits, credits } = totals(journal);
  expect(debits).toBe(credits);
  expect(debits).toBe(BigInt(amount));
  // Read-only credentials see the same journal (cross-workspace isolation is
  // covered by the Go integration suite; the other-workspace key is revoked
  // earlier in this E2E run).
  expect(await journalsFor(api, accepted.id, state().readerKey)).toEqual(
    journals,
  );
});

test("the debit goes to the executing provider's clearing account and the credit to merchant payable", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const accepted = await createPayment(api, fixtures.method, "1250", "USD");
  await waitForStatus(api, accepted.id, "succeeded");
  const [attempt] = await listAttempts(api, accepted.id);
  const [journal] = await journalsFor(api, accepted.id);
  const debit = journal.entries.filter((entry) => entry.side === "debit");
  const credit = journal.entries.filter((entry) => entry.side === "credit");
  expect(debit).toHaveLength(1);
  expect(credit).toHaveLength(1);
  expect(debit[0]).toMatchObject({
    account_code: `provider_clearing:${attempt.provider_id}`,
    provider_id: attempt.provider_id,
    amount_minor: "1250",
  });
  expect(credit[0]).toMatchObject({
    account_code: "merchant_payable",
    provider_id: null,
    amount_minor: "1250",
  });
});

test("a re-delivered dispatcher completion leaves exactly one journal", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const accepted = await createPayment(api, fixtures.successMethod, "777");
  await waitForStatus(api, accepted.id, "succeeded");
  const before = await journalsFor(api, accepted.id);
  expect(before).toHaveLength(1);
  for (const restart of [false, true]) {
    await redeliverDispatch(accepted.id);
    if (restart) await restartApi();
    await expect
      .poll(
        async () =>
          (await inspectPayment(accepted.id)).outbox.find(
            (row) => row.event_type === "payment.dispatch",
          )?.status,
        { timeout: 10_000, intervals: [50, 100] },
      )
      .toBe("delivered");
  }
  const inspection = await inspectPayment(accepted.id);
  expect(
    inspection.outbox.find((row) => row.event_type === "payment.dispatch")
      ?.attempts,
  ).toBe(3);
  expect(inspection.journals).toBe(1);
  expect(inspection.attempts).toHaveLength(1);
  expect(inspection.charges).toHaveLength(1);
  const after = await journalsFor(api, accepted.id);
  expect(after).toEqual(before);
});

test("failed payments have no journal", async ({ request }) => {
  const api = new ApiClient(request);
  const accepted = await createPayment(api, fixtures.declineMethod, "640");
  await waitForStatus(api, accepted.id, "failed");
  expect(await journalsFor(api, accepted.id)).toHaveLength(0);
  expect((await inspectPayment(accepted.id)).journals).toBe(0);
});

test("canceled payments have no journal", async ({ request }) => {
  const api = new ApiClient(request);
  const id = await seedCreatedPayment(fixtures.successMethod);
  const response = await api.call("POST", `/v1/payments/${id}/cancel`);
  expect(response.status()).toBe(200);
  expect(((await response.json()) as { status: string }).status).toBe(
    "canceled",
  );
  expect(await journalsFor(api, id)).toHaveLength(0);
  expect((await inspectPayment(id)).journals).toBe(0);
});
