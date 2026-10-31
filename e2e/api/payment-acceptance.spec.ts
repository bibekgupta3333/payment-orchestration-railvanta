import { randomUUID } from "node:crypto";
import { test, expect, type APIResponse } from "@playwright/test";
import { ApiClient } from "../support/api";
import { fixtures, restartApi } from "../support/state";

interface Payment {
  id: string;
  amount_minor: string;
  currency: string;
  payment_method_id: string;
  merchant_reference: string | null;
  status: string;
}

function intent(
  amount: string,
  reference: string,
  currency = "USD",
): Record<string, unknown> {
  return {
    amount_minor: amount,
    currency,
    payment_method_id: fixtures.method,
    merchant_reference: reference,
  };
}

function create(
  api: ApiClient,
  key: string | undefined,
  data: Record<string, unknown>,
): Promise<APIResponse> {
  return api.call("POST", "/v1/payments", {
    data,
    headers: key === undefined ? {} : { "Idempotency-Key": key },
  });
}

// Pages through the list endpoint and returns every payment with the reference.
async function listByReference(
  api: ApiClient,
  reference: string,
): Promise<Payment[]> {
  const found: Payment[] = [];
  let cursor: string | null = null;
  do {
    const query: string = cursor
      ? `?limit=100&cursor=${encodeURIComponent(cursor)}`
      : "?limit=100";
    const response = await api.call("GET", `/v1/payments${query}`);
    expect(response.status()).toBe(200);
    const page = (await response.json()) as {
      items: Payment[];
      next_cursor: string | null;
    };
    found.push(
      ...page.items.filter((item) => item.merchant_reference === reference),
    );
    cursor = page.next_cursor;
  } while (cursor);
  return found;
}

test("POST returns 202 with a payment ID and Location that GET resolves", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const reference = `location_${randomUUID()}`;
  const response = await create(
    api,
    `location_${randomUUID()}`,
    intent("25000", reference),
  );
  expect(response.status()).toBe(202);
  const accepted = (await response.json()) as Payment;
  expect(accepted.id).toMatch(/^[0-9a-f-]{36}$/);
  expect(accepted.status).toBe("created");
  const location = response.headers()["location"];
  expect(location).toBe(`/v1/payments/${accepted.id}`);
  const read = await api.call("GET", location);
  expect(read.status()).toBe(200);
  expect(await read.json()).toMatchObject({
    id: accepted.id,
    amount_minor: "25000",
    currency: "USD",
    payment_method_id: fixtures.method,
    merchant_reference: reference,
  });
  const attempts = await api.call("GET", `${location}/attempts`);
  expect(attempts.status()).toBe(200);
  expect((await attempts.json()).items).toEqual([]);
});

test("same key and body replays byte-identically; a changed body returns 409", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const key = `replay_${randomUUID()}`;
  const reference = `replay_${randomUUID()}`;
  const first = await create(api, key, intent("1999", reference));
  expect(first.status()).toBe(202);
  const replay = await create(api, key, intent("1999", reference));
  expect(replay.status()).toBe(202);
  expect((await replay.body()).equals(await first.body())).toBe(true);
  expect(replay.headers()["idempotent-replayed"]).toBe("true");
  expect(replay.headers()["location"]).toBe(first.headers()["location"]);
  for (const changed of [
    intent("2000", reference),
    intent("1999", reference, "EUR"),
    { ...intent("1999", reference), payment_method_id: fixtures.otherMethod },
  ]) {
    const conflict = await create(api, key, changed);
    expect(conflict.status()).toBe(409);
    expect((await conflict.json()).code).toBe("idempotency_conflict");
  }
  expect(await listByReference(api, reference)).toHaveLength(1);
});

test("20 concurrent same-key requests produce exactly one payment", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const key = `concurrent_${randomUUID()}`;
  const reference = `concurrent_${randomUUID()}`;
  const responses = await Promise.all(
    Array.from({ length: 20 }, () =>
      create(api, key, intent("4242", reference)),
    ),
  );
  const bodies = await Promise.all(responses.map((r) => r.body()));
  for (const [index, response] of responses.entries()) {
    expect(response.status()).toBe(202);
    expect(bodies[index].equals(bodies[0])).toBe(true);
  }
  const listed = await listByReference(api, reference);
  expect(listed).toHaveLength(1);
  expect(listed[0].id).toBe((JSON.parse(bodies[0].toString()) as Payment).id);
});

test("a request without an Idempotency-Key is rejected and creates nothing", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const reference = `nokey_${randomUUID()}`;
  const response = await create(api, undefined, intent("100", reference));
  expect(response.status()).toBe(400);
  const envelope = await response.json();
  expect(envelope.code).toBe("validation_error");
  expect(envelope.field_errors[0].field).toBe("Idempotency-Key");
  expect(await listByReference(api, reference)).toHaveLength(0);
});

test("after restartApi() the original key still replays the stored response", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const key = `restart_${randomUUID()}`;
  const reference = `restart_${randomUUID()}`;
  const first = await create(api, key, intent("31337", reference, "JPY"));
  expect(first.status()).toBe(202);
  await restartApi();
  const replay = await create(api, key, intent("31337", reference, "JPY"));
  expect(replay.status()).toBe(202);
  expect((await replay.body()).equals(await first.body())).toBe(true);
  expect(replay.headers()["idempotent-replayed"]).toBe("true");
  expect(await listByReference(api, reference)).toHaveLength(1);
});

test("amounts above Number.MAX_SAFE_INTEGER round-trip exactly as decimal strings", async ({
  request,
}) => {
  const api = new ApiClient(request);
  for (const amount of ["9007199254740993", "9223372036854775807"]) {
    expect(BigInt(amount) > BigInt(Number.MAX_SAFE_INTEGER)).toBe(true);
    const response = await create(
      api,
      `big_${randomUUID()}`,
      intent(amount, `big_${randomUUID()}`, "KWD"),
    );
    expect(response.status()).toBe(202);
    expect(await response.text()).toContain(`"amount_minor":"${amount}"`);
    const read = await api.call("GET", response.headers()["location"]);
    expect(((await read.json()) as Payment).amount_minor).toBe(amount);
  }
  const overflow = await create(
    api,
    `overflow_${randomUUID()}`,
    intent("9223372036854775808", "overflow"),
  );
  expect(overflow.status()).toBe(400);
});
