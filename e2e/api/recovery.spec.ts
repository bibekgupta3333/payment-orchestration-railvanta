import { test, expect } from "@playwright/test";
import { ApiClient } from "../support/api";
import {
  createPayment,
  journalsFor,
  listAttempts,
  waitForStatus,
} from "../support/payments";
import {
  fixtures,
  inspectPayment,
  killApi,
  seedCreatedPayment,
} from "../support/state";

interface RecoveryView {
  lookup_status: string;
  lookup_attempts: number;
  retry_allowed: boolean;
  late_outcomes: unknown[];
}

async function recovery(api: ApiClient, id: string): Promise<RecoveryView> {
  const response = await api.call("GET", `/v1/payments/${id}/recovery`);
  expect(response.status()).toBe(200);
  return (await response.json()) as RecoveryView;
}

test("central demo: timeout after success resolves through a lookup at the original provider", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const accepted = await createPayment(
    api,
    fixtures.timeoutAfterSuccessMethod,
    "31000",
  );
  await waitForStatus(api, accepted.id, "succeeded");
  const inspection = await inspectPayment(accepted.id);
  // The response was lost (unknown), then a lookup resolved it.
  expect(inspection.transitions).toEqual([
    "created",
    "processing",
    "unknown",
    "succeeded",
  ]);
  expect(inspection.attempts).toHaveLength(1);
  expect(inspection.attempts[0]).toMatchObject({
    provider_id: fixtures.provider,
    status: "succeeded",
  });
  expect(inspection.charges).toHaveLength(1);
  expect(inspection.charges[0].provider_idempotency_key).toBe(
    inspection.attempts[0].provider_idempotency_key,
  );
  expect(
    inspection.outbox.filter((row) => row.event_type === "payment.lookup"),
  ).toEqual([expect.objectContaining({ status: "delivered" })]);
  const journals = await journalsFor(api, accepted.id);
  expect(journals).toHaveLength(1);
  const debit = journals[0].entries.filter((e) => e.side === "debit");
  const credit = journals[0].entries.filter((e) => e.side === "credit");
  expect(debit.reduce((sum, e) => sum + BigInt(e.amount_minor), 0n)).toBe(
    31000n,
  );
  expect(credit.reduce((sum, e) => sum + BigInt(e.amount_minor), 0n)).toBe(
    31000n,
  );
  expect((await recovery(api, accepted.id)).lookup_status).toBe("resolved");
});

test("killApi() after provider acceptance and before the result is recorded resolves by lookup with no second charge", async ({
  request,
}) => {
  test.setTimeout(60_000);
  const api = new ApiClient(request);
  const accepted = await createPayment(api, fixtures.delayedSuccessMethod);
  await expect
    .poll(async () => (await listAttempts(api, accepted.id))[0]?.status, {
      timeout: 10_000,
      intervals: [50, 100],
    })
    .toBe("submitted");
  const before = await inspectPayment(accepted.id);
  // The provider has accepted the charge (it is processing); no result is recorded.
  expect(before.charges).toHaveLength(1);
  await killApi();
  await waitForStatus(api, accepted.id, "succeeded", 30_000);
  const after = await inspectPayment(accepted.id);
  expect(after.attempts).toHaveLength(1);
  expect(after.attempts[0].provider_idempotency_key).toBe(
    before.attempts[0].provider_idempotency_key,
  );
  expect(after.charges).toHaveLength(1);
  expect(after.transitions).toEqual([
    "created",
    "processing",
    "unknown",
    "succeeded",
  ]);
  expect(await journalsFor(api, accepted.id)).toHaveLength(1);
});

test("canceling while created gives canceled, and canceling once processing started is rejected", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const id = await seedCreatedPayment(fixtures.successMethod);
  const cancel = await api.call("POST", `/v1/payments/${id}/cancel`);
  expect(cancel.status()).toBe(200);
  expect(((await cancel.json()) as { status: string }).status).toBe("canceled");
  // Repeating is harmless.
  expect((await api.call("POST", `/v1/payments/${id}/cancel`)).status()).toBe(
    200,
  );
  const inspection = await inspectPayment(id);
  expect(inspection.transitions).toEqual(["created", "canceled"]);
  expect(inspection.attempts).toHaveLength(0);
  expect(inspection.charges).toHaveLength(0);
  expect(
    inspection.outbox.find((row) => row.event_type === "payment.dispatch")
      ?.status,
  ).toBe("delivered");

  const succeeded = await createPayment(api, fixtures.successMethod);
  await waitForStatus(api, succeeded.id, "succeeded");
  const rejected = await api.call(
    "POST",
    `/v1/payments/${succeeded.id}/cancel`,
  );
  expect(rejected.status()).toBe(409);
  expect(((await rejected.json()) as { code: string }).code).toBe(
    "not_cancelable",
  );
  const missing = await api.call(
    "POST",
    "/v1/payments/00000000-0000-7000-8000-00000000dead/cancel",
  );
  expect(missing.status()).toBe(404);
});

test("racing cancel against dispatch leaves exactly one winner", async ({
  request,
}) => {
  const api = new ApiClient(request);
  let canceledWins = 0;
  let dispatchWins = 0;
  for (let i = 0; i < 12; i++) {
    const accepted = await createPayment(api, fixtures.successMethod, "777");
    const response = await api.call(
      "POST",
      `/v1/payments/${accepted.id}/cancel`,
    );
    if (response.status() === 200) {
      canceledWins++;
      await new Promise((resolve) => setTimeout(resolve, 150));
      const inspection = await inspectPayment(accepted.id);
      expect(inspection.transitions).toEqual(["created", "canceled"]);
      expect(inspection.attempts).toHaveLength(0);
      expect(inspection.charges).toHaveLength(0);
      expect(inspection.journals).toBe(0);
    } else {
      expect(response.status()).toBe(409);
      dispatchWins++;
      await waitForStatus(api, accepted.id, "succeeded");
      const inspection = await inspectPayment(accepted.id);
      expect(inspection.transitions).toEqual([
        "created",
        "processing",
        "succeeded",
      ]);
      expect(inspection.attempts).toHaveLength(1);
      expect(inspection.charges).toHaveLength(1);
      expect(inspection.journals).toBe(1);
    }
  }
  expect(canceledWins + dispatchWins).toBe(12);
});

test("an unknown payment exposes no retry action and creates no new attempt", async ({
  request,
}) => {
  const api = new ApiClient(request);
  // The outage method never records a charge, so it cannot resolve.
  const accepted = await createPayment(api, fixtures.outageMethod);
  await waitForStatus(api, accepted.id, "unknown");
  const view = await recovery(api, accepted.id);
  expect(view.retry_allowed).toBe(false);
  expect(view.late_outcomes).toEqual([]);
  expect(["pending", "in_progress"]).toContain(view.lookup_status);
  for (const path of ["retry", "resubmit", "dispatch"]) {
    const response = await api.call(
      "POST",
      `/v1/payments/${accepted.id}/${path}`,
    );
    expect(response.status()).toBe(404);
  }
  const cancel = await api.call("POST", `/v1/payments/${accepted.id}/cancel`);
  expect(cancel.status()).toBe(409);
  await new Promise((resolve) => setTimeout(resolve, 1_500));
  expect((await getStatus(api, accepted.id)).status).toBe("unknown");
  const attempts = await listAttempts(api, accepted.id);
  expect(attempts).toHaveLength(1);
  expect(attempts[0].status).toBe("unknown");
  expect((await recovery(api, accepted.id)).lookup_attempts).toBeGreaterThan(1);
  expect((await inspectPayment(accepted.id)).journals).toBe(0);
});

async function getStatus(
  api: ApiClient,
  id: string,
): Promise<{ status: string }> {
  const response = await api.call("GET", `/v1/payments/${id}`);
  return (await response.json()) as { status: string };
}
