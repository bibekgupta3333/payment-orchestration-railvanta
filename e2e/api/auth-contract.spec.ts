import { test, expect } from "@playwright/test";
import { ApiClient } from "../support/api";
import { fixtures, readLogs, state } from "../support/state";

function intent(amount: unknown = "1"): Record<string, unknown> {
  return {
    amount_minor: amount,
    currency: "USD",
    payment_method_id: fixtures.method,
  };
}

test("missing credentials produce a safe documented 401 envelope", async ({
  request,
}) => {
  const response = await new ApiClient(request).call("GET", "/v1/workspace", {
    key: null,
  });
  expect(response.status()).toBe(401);
  expect((await response.json()).code).toBe("unauthorized");
});

test("workspace and payment-method reads cannot cross the authenticated scope", async ({
  request,
}) => {
  const api = new ApiClient(request);
  for (const path of [
    `/v1/workspaces/${fixtures.otherWorkspace}`,
    `/v1/payment-methods/${fixtures.otherMethod}`,
  ]) {
    const response = await api.call("GET", path);
    expect(response.status()).toBe(404);
    expect(await response.text()).not.toContain(fixtures.otherWorkspace);
  }
});

test("money input rejects non-string, fractional, signed, blank and overflowing values", async ({
  request,
}) => {
  const api = new ApiClient(request);
  for (const amount of [
    1,
    "1.5",
    "-1",
    "0",
    " 1",
    "+1",
    "",
    "9223372036854775808",
  ]) {
    const response = await api.call("POST", "/v1/payments", {
      data: intent(amount),
      headers: { "Idempotency-Key": "invalid_money_contract" },
    });
    expect(response.status()).toBe(400);
    expect((await response.json()).field_errors.length).toBeGreaterThan(0);
  }
  // The full int64 range is accepted (WBS 2.1); dispatch is a later package.
  const acceptedInput = await api.call("POST", "/v1/payments", {
    data: intent("9223372036854775807"),
    headers: { "Idempotency-Key": "valid_money_contract" },
  });
  expect(acceptedInput.status()).toBe(202);
});

test("read credentials cannot invoke operator mutations", async ({
  request,
}) => {
  const response = await new ApiClient(request).call("POST", "/v1/payments", {
    key: state().readerKey,
    data: intent(),
    headers: { "Idempotency-Key": "role_contract" },
  });
  expect(response.status()).toBe(403);
});

test("session writes require both their CSRF token and configured origin", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const current = state();
  const cookie = `__Host-railvanta_session=${current.sessionToken}`;
  const session = await api.call("GET", "/v1/session", {
    key: null,
    headers: { Cookie: cookie },
  });
  expect(session.status()).toBe(200);
  const missing = await api.call("POST", "/v1/payments", {
    key: null,
    data: intent(),
    headers: { Cookie: cookie, "Idempotency-Key": "csrf_contract" },
  });
  expect(missing.status()).toBe(403);
  const valid = await api.call("POST", "/v1/payments", {
    key: null,
    data: intent(),
    headers: {
      Cookie: cookie,
      Origin: current.webOrigin,
      "X-CSRF-Token": current.csrfToken,
      "Idempotency-Key": "csrf_contract",
    },
  });
  expect(valid.status()).toBe(202);
});

test("request logs exclude authorization, cookie, body, query and idempotency secrets", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const marker = "sensitive_marker_do_not_log";
  await api.call("POST", `/v1/payments?private=${marker}`, {
    data: { ...intent(), merchant_reference: marker },
    headers: { "Idempotency-Key": marker },
  });
  const { logs } = await readLogs();
  expect(typeof logs).toBe("string");
  for (const sensitive of [
    marker,
    state().operatorKey,
    state().readerKey,
    state().sessionToken,
    state().csrfToken,
  ]) {
    expect(logs).not.toContain(sensitive);
  }
});

test("API key self-revocation rejects subsequent server-to-server requests", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const key = state().otherKey;
  expect((await api.call("GET", "/v1/workspace", { key })).status()).toBe(200);
  expect(
    (await api.call("DELETE", "/v1/api-keys/current", { key })).status(),
  ).toBe(204);
  expect((await api.call("GET", "/v1/workspace", { key })).status()).toBe(401);
});
