import { test, expect } from "@playwright/test";
import { ApiClient } from "../support/api";
import { fixtures, rejectProduction, state } from "../support/state";

test("API boots and exposes private readiness without routing it publicly", async ({
  request,
}) => {
  // The container health listener is private to the API container; the same
  // facts are asserted with exec/inspect in ops/container-runtime.spec.ts.
  test.skip(
    Boolean(process.env.E2E_BASE_URL),
    "native-only: container health is private; see ops/container-runtime.spec.ts",
  );
  const current = state();
  const response = await request.get(`${current.healthOrigin}/health/ready`);
  expect(response.status()).toBe(200);
  expect(await response.json()).toEqual({ status: "ok" });
  const publicResponse = await request.get(`${current.apiOrigin}/health/ready`);
  expect(publicResponse.status()).not.toBe(200);
});

test("Next.js forwards the complete /v1 path and request identifier to Go", async ({
  request,
}) => {
  const response = await new ApiClient(request).call("GET", "/v1/workspace", {
    origin: state().webOrigin,
    headers: { "X-Request-ID": "scaffold_proxy_probe" },
  });
  expect(response.status()).toBe(200);
  expect(response.headers()["x-request-id"]).toBe("scaffold_proxy_probe");
  expect((await response.json()).id).toBe(fixtures.workspace);
});

test("APP_ENV=production fails startup and sandbox remains recoverable", async ({
  request,
}) => {
  const result = await rejectProduction();
  expect(result.exitCode).not.toBe(0);
  expect((await new ApiClient(request).workspace()).id).toBe(
    fixtures.workspace,
  );
});
