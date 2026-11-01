import { test, expect } from "@playwright/test";
import { ApiClient } from "../support/api";
import {
  eventId,
  listEvents,
  receipt,
  sendWebhook,
  webhookBody,
} from "../support/events";
import { createPayment, journalsFor, waitForStatus } from "../support/payments";
import { fixtures, inspectPayment, restartApi, state } from "../support/state";

// WBS 4.3 over real HTTP: duplicates, changed payloads, reordered events,
// safe event reads and workspace isolation.

test("duplicate deliveries count once and have one effect, even across a restart", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const payment = await createPayment(api, fixtures.outageMethod, "800");
  await waitForStatus(api, payment.id, "unknown");
  const event = eventId();
  const body = webhookBody(event, "payment.succeeded", payment.id);

  const first = await sendWebhook(api, body);
  expect(first.status()).toBe(202);
  const firstReceipt = await receipt(first);
  expect(firstReceipt).toMatchObject({
    result: "processed",
    outcome: "applied",
    duplicate_count: 0,
    signature_status: "not_applicable",
  });
  for (const count of [1, 2]) {
    const again = await sendWebhook(api, body);
    expect(again.status()).toBe(200);
    expect(await receipt(again)).toMatchObject({
      result: "duplicate",
      duplicate_count: count,
      event_id: firstReceipt.event_id,
    });
  }
  await restartApi();
  const third = await sendWebhook(api, body);
  expect((await receipt(third)).duplicate_count).toBe(3);

  await waitForStatus(api, payment.id, "succeeded");
  expect(await journalsFor(api, payment.id)).toHaveLength(1);
  const inspection = await inspectPayment(payment.id);
  expect(inspection.transitions.filter((s) => s === "succeeded")).toHaveLength(
    1,
  );
  const events = await listEvents(api, `?q=${event}`);
  expect(events.items).toHaveLength(1);
  expect(events.items[0]).toMatchObject({
    source: "inbox",
    duplicate_count: 3,
    needs_investigation: false,
    signature_status: "not_applicable",
  });
});

test("the same event ID with a different payload is flagged and never applied or overwritten", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const payment = await createPayment(api, fixtures.outageMethod, "800");
  await waitForStatus(api, payment.id, "unknown");
  const event = eventId();
  expect(
    (
      await sendWebhook(
        api,
        webhookBody(event, "payment.processing", payment.id),
      )
    ).status(),
  ).toBe(202);
  const changed = await sendWebhook(
    api,
    webhookBody(event, "payment.succeeded", payment.id),
  );
  expect(changed.status()).toBe(409);
  expect((await changed.json()).code).toBe("event_payload_conflict");
  await expect
    .poll(async () =>
      (await api.call("GET", `/v1/payments/${payment.id}`))
        .json()
        .then((p) => p.status),
    )
    .toBe("unknown");

  const [item] = (await listEvents(api, `?q=${event}`)).items;
  expect(item.needs_investigation).toBe(true);
  expect(item.event_type).toBe("payment.processing");
  const detail = await (await api.call("GET", `/v1/events/${item.id}`)).json();
  expect(detail.payload_hash).toMatch(/^[0-9a-f]{64}$/);
  expect(detail.conflicting_payload_hash).toMatch(/^[0-9a-f]{64}$/);
  expect(detail.conflicting_payload_hash).not.toBe(detail.payload_hash);
  expect(detail.conflict_count).toBe(1);
  expect(
    (await listEvents(api, "?state=investigation&limit=100")).items.map(
      (i) => i.id,
    ),
  ).toContain(item.id);
});

test("reordered events are recorded as ignored and never regress a terminal payment", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const payment = await createPayment(api, fixtures.successMethod, "500");
  await waitForStatus(api, payment.id, "succeeded");
  const failed = await sendWebhook(
    api,
    webhookBody(eventId(), "payment.failed", payment.id),
  );
  expect(failed.status()).toBe(202);
  expect(await receipt(failed)).toMatchObject({
    outcome: "ignored",
    outcome_reason: "terminal_state",
  });
  const processing = await sendWebhook(
    api,
    webhookBody(eventId(), "payment.processing", payment.id),
  );
  expect((await receipt(processing)).outcome).toBe("ignored");
  expect(
    (await (await api.call("GET", `/v1/payments/${payment.id}`)).json()).status,
  ).toBe("succeeded");
  expect(await journalsFor(api, payment.id)).toHaveLength(1);
  expect(
    (await inspectPayment(payment.id)).transitions.filter(
      (s) => s === "failed",
    ),
  ).toHaveLength(0);
});

test("event reads expose safe fields only and are isolated per workspace", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const payment = await createPayment(api, fixtures.successMethod, "500");
  await waitForStatus(api, payment.id, "succeeded");
  const event = eventId();
  await sendWebhook(api, {
    ...webhookBody(event, "payment.processing", payment.id),
    provider_reference: "sk_live_must_never_appear",
  });
  const [item] = (await listEvents(api, `?q=${event}`)).items;
  const detailResponse = await api.call("GET", `/v1/events/${item.id}`);
  const text = await detailResponse.text();
  for (const forbidden of [
    "sk_live_must_never_appear",
    state().operatorKey,
    "lease_owner",
    "last_error",
    "Idempotency-Key",
  ])
    expect(text).not.toContain(forbidden);
  const detail = JSON.parse(text);
  expect(detail.payload_summary.redacted_count).toBe(1);
  expect(detail.attempts.length).toBeGreaterThan(0);

  const outbox = await listEvents(api, `?source=outbox&q=${payment.id}`);
  expect(outbox.items.length).toBeGreaterThan(0);
  expect(
    outbox.items.every(
      (i) => i.source === "outbox" && i.payment_id === payment.id,
    ),
  ).toBe(true);

  // Cross-workspace isolation (404 / empty list) is covered by the Go suite:
  // auth-contract.spec.ts revokes the other workspace's key earlier in this run.
  const readerRead = await api.call("GET", `/v1/events/${item.id}`, {
    key: state().readerKey,
  });
  expect(readerRead.status()).toBe(200);
  const reader = await api.call("POST", "/v1/sandbox/webhooks/mock", {
    key: state().readerKey,
    data: webhookBody(eventId(), "payment.processing", payment.id),
  });
  expect(reader.status()).toBe(403);
  const anonymous = await api.call("GET", "/v1/events", { key: null });
  expect(anonymous.status()).toBe(401);
});

test("event lists paginate by cursor without gaps or repeats", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const payment = await createPayment(api, fixtures.successMethod, "500");
  await waitForStatus(api, payment.id, "succeeded");
  for (let i = 0; i < 3; i++)
    await sendWebhook(
      api,
      webhookBody(eventId(), "payment.processing", payment.id),
    );
  const seen: string[] = [];
  let cursor = "";
  for (let pages = 0; pages < 20; pages++) {
    const page = await listEvents(api, `?limit=2&q=${payment.id}${cursor}`);
    seen.push(...page.items.map((i) => i.id));
    if (!page.next_cursor) break;
    cursor = `&cursor=${page.next_cursor}`;
  }
  expect(new Set(seen).size).toBe(seen.length);
  expect(seen.length).toBeGreaterThanOrEqual(4);
  const bad = await api.call("GET", "/v1/events?state=bogus");
  expect(bad.status()).toBe(400);
});
