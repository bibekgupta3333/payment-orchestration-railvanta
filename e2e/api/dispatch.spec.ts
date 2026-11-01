import { test, expect } from "@playwright/test";
import { ApiClient } from "../support/api";
import {
  createPayment,
  getPayment,
  listAttempts,
  waitForStatus,
  type Payment,
} from "../support/payments";
import {
  fixtures,
  inspectPayment,
  killApi,
  restartApi,
  state,
  waitForProviderCharge,
} from "../support/state";

interface Decision {
  provider_id: string;
  provider_code: string;
  rule_id: string;
  priority: number;
  reason: string;
}

async function countPayments(api: ApiClient): Promise<number> {
  let total = 0;
  let cursor: string | null = null;
  do {
    const query: string = cursor
      ? `?limit=100&cursor=${encodeURIComponent(cursor)}`
      : "?limit=100";
    const response = await api.call("GET", `/v1/payments${query}`);
    const page = (await response.json()) as {
      items: Payment[];
      next_cursor: string | null;
    };
    total += page.items.length;
    cursor = page.next_cursor;
  } while (cursor);
  return total;
}

test("the success mock method reaches succeeded with a routed, timed attempt", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const accepted = await createPayment(api, fixtures.successMethod);
  await waitForStatus(api, accepted.id, "succeeded");
  const attempts = await listAttempts(api, accepted.id);
  expect(attempts).toHaveLength(1);
  const [attempt] = attempts;
  expect(attempt).toMatchObject({
    provider_id: fixtures.provider,
    attempt_number: 1,
    status: "succeeded",
    error_code: null,
  });
  expect(attempt.routing_rule_id).toBeTruthy();
  expect(attempt.routing_reason).toContain("Rule");
  expect(attempt.provider_reference).toMatch(/^mock_ch_/);
  expect(attempt.submitted_at).toBeTruthy();
  expect(attempt.completed_at).toBeTruthy();
  expect(Date.parse(attempt.completed_at as string)).toBeGreaterThanOrEqual(
    Date.parse(attempt.submitted_at as string),
  );
});

test("the decline mock method ends failed with no further attempts", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const accepted = await createPayment(api, fixtures.declineMethod);
  await waitForStatus(api, accepted.id, "failed");
  const attempts = await listAttempts(api, accepted.id);
  expect(attempts).toHaveLength(1);
  expect(attempts[0]).toMatchObject({
    status: "failed",
    error_code: "declined",
  });
  expect(attempts[0].completed_at).toBeTruthy();
  // Give the dispatcher several poll rounds to prove nothing retries.
  await new Promise((resolve) => setTimeout(resolve, 500));
  expect(await listAttempts(api, accepted.id)).toHaveLength(1);
  expect((await inspectPayment(accepted.id)).charges).toHaveLength(1);
});

test("POST /v1/routing/evaluate returns provider and reason without creating anything", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const before = await countPayments(api);
  const body = {
    payment_method_id: fixtures.successMethod,
    amount_minor: "4321",
    currency: "EUR",
  };
  const response = await api.call("POST", "/v1/routing/evaluate", {
    data: body,
  });
  expect(response.status()).toBe(200);
  const decision = (await response.json()) as Decision;
  expect(decision.provider_id).toBe(fixtures.provider);
  expect(decision.provider_code).toBe("mock");
  expect(decision.reason).toContain("EUR");
  expect(await countPayments(api)).toBe(before);
  const unknownMethod = await api.call("POST", "/v1/routing/evaluate", {
    data: { ...body, payment_method_id: fixtures.otherMethod },
  });
  expect(unknownMethod.status()).toBe(404);
  const reader = await api.call("POST", "/v1/routing/evaluate", {
    data: body,
    key: state().readerKey,
  });
  expect(reader.status()).toBe(403);
  expect(await countPayments(api)).toBe(before);

  // A real dispatch of the same input uses the same provider, rule and reason.
  const accepted = await createPayment(
    api,
    fixtures.successMethod,
    "4321",
    "EUR",
  );
  await waitForStatus(api, accepted.id, "succeeded");
  const [attempt] = await listAttempts(api, accepted.id);
  expect(attempt.provider_id).toBe(decision.provider_id);
  expect(attempt.routing_rule_id).toBe(decision.rule_id);
  expect(attempt.routing_reason).toBe(decision.reason);
});

test("killApi() while a delayed-success attempt is submitted never submits a second charge", async ({
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
  const before = await waitForProviderCharge(accepted.id);
  expect(before.attempts).toHaveLength(1);
  expect(before.charges).toHaveLength(1);
  expect(before.charges[0].status).toBe("processing");
  const key = before.attempts[0].provider_idempotency_key;

  await killApi();
  // After the dead owner's lease expires the restarted dispatcher reclaims the
  // command, finds the attempt still submitted and moves it to the lookup
  // path; the lookup at the original provider (WBS 2.4) then settles it.
  await waitForStatus(api, accepted.id, "succeeded", 30_000);
  const after = await inspectPayment(accepted.id);
  expect(after.attempts).toHaveLength(1);
  expect(after.attempts[0].id).toBe(before.attempts[0].id);
  expect(after.attempts[0].provider_idempotency_key).toBe(key);
  expect(after.attempts[0].provider_id).toBe(fixtures.provider);
  expect(after.charges).toHaveLength(1);
  expect(after.charges[0].provider_idempotency_key).toBe(key);
  expect(
    after.outbox.filter((row) => row.event_type === "payment.lookup"),
  ).toHaveLength(1);
  expect(after.transitions).toContain("unknown");
  const [attempt] = await listAttempts(api, accepted.id);
  expect(attempt.status).toBe("succeeded");
});

test("an unknown attempt is never retried with a different provider", async ({
  request,
}) => {
  const api = new ApiClient(request);
  // The outage method never records a charge, so its lookup keeps answering
  // "no record" and the payment stays unknown.
  const accepted = await createPayment(api, fixtures.outageMethod);
  await waitForStatus(api, accepted.id, "unknown");
  const first = await inspectPayment(accepted.id);
  expect(first.attempts).toHaveLength(1);
  expect(first.attempts[0]).toMatchObject({
    provider_id: fixtures.provider,
    status: "unknown",
  });

  await restartApi();
  await new Promise((resolve) => setTimeout(resolve, 1_000));
  const later = await inspectPayment(accepted.id);
  expect(later.attempts).toEqual(first.attempts);
  expect(later.attempts.every((a) => a.provider_id === fixtures.provider)).toBe(
    true,
  );
  expect(
    later.outbox.find((row) => row.event_type === "payment.dispatch")?.status,
  ).toBe("delivered");
  expect((await getPayment(api, accepted.id)).status).toBe("unknown");
  const attempts = await listAttempts(api, accepted.id);
  expect(attempts).toHaveLength(1);
  expect(attempts[0].completed_at).toBeNull();
});
