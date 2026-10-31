import { test, expect } from "@playwright/test";
import { ApiClient } from "../support/api";
import { fixtures, killApi, prepareDurabilityFixture, rejectChecksumDrift, restartApi } from "../support/state";

test("startup migrates a fresh file and graceful restart preserves seeded rows", async ({ request }) => {
  const api = new ApiClient(request);
  const before = await api.workspace();
  const methodsBefore = await api.call("GET", "/v1/payment-methods");
  expect((await methodsBefore.json()).items).toEqual([
    { id: fixtures.method, rail: "mock", brand: "Mock", last_four: "4242" },
  ]);
  await restartApi();
  expect(await api.workspace()).toEqual(before);
  const methodsAfter = await api.call("GET", "/v1/payment-methods");
  expect(await methodsAfter.json()).toEqual(await methodsBefore.json());
});

test("committed seeded rows survive SIGKILL and reopening the same WAL database", async ({ request }) => {
  const api = new ApiClient(request);
  await prepareDurabilityFixture();
  const before = await api.call("GET", `/v1/payment-methods/${fixtures.durableMethod}`);
  expect(before.status()).toBe(200);
  expect((await before.json()).brand).toBe("Durability fixture");
  await killApi();
  const after = await api.call("GET", `/v1/payment-methods/${fixtures.durableMethod}`);
  expect(after.status()).toBe(200);
  expect(await after.json()).toEqual(await before.json());
});

test("tampered migration history fails startup before readiness", async ({ request }) => {
  const result = await rejectChecksumDrift();
  expect(result.exitCode).not.toBe(0);
  // The harness restores its controlled corruption before subsequent tests run.
  expect((await new ApiClient(request).workspace()).id).toBe(fixtures.workspace);
});
