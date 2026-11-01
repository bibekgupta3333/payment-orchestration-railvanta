import AxeBuilder from "@axe-core/playwright";
import { test, expect, type Page } from "@playwright/test";
import { ApiClient } from "../support/api";
import { signIn } from "../support/browser";
import { createPayment, waitForStatus } from "../support/payments";
import { fixtures, state } from "../support/state";

test.beforeEach(async ({ context }) => {
  await signIn(context);
});

async function axeClean(page: Page) {
  const results = await new AxeBuilder({ page }).analyze();
  expect(
    results.violations.filter((v) =>
      ["serious", "critical"].includes(v.impact ?? ""),
    ),
  ).toEqual([]);
}

async function evaluate(page: Page, methodLabel: string, paymentId = "") {
  await page.getByLabel("Payment method").selectOption({ label: methodLabel });
  await page.getByLabel("Amount (minor units)").fill("2500");
  if (paymentId) await page.getByLabel(/Existing payment ID/).fill(paymentId);
  await page.getByRole("button", { name: "Evaluate" }).click();
}

test("rules are read-only with disabled mutation controls, and evaluation shows its path without side effects", async ({
  page,
  request,
}) => {
  const api = new ApiClient(request);
  const count = async () =>
    (
      (await (await api.call("GET", "/v1/payments?limit=100")).json()) as {
        items: unknown[];
      }
    ).items.length;
  const before = await count();

  await page.goto(`${state().webOrigin}/dashboard/routing`);
  const rules = page.getByRole("region", { name: "Routing rules" });
  await expect(rules.locator("tbody tr").first()).toBeVisible();
  await expect(rules.locator("thead")).toContainText("Priority");
  await expect(rules.locator("thead")).toContainText("Amount range");
  await expect(rules.locator("tbody")).toContainText("mock");
  for (const name of ["Create rule", "Reorder"]) {
    await expect(page.getByRole("button", { name })).toBeDisabled();
  }
  await expect(
    rules.getByRole("button", { name: "Edit" }).first(),
  ).toBeDisabled();
  await expect(
    page.getByText("requires rule mutation contract", { exact: false }),
  ).toBeVisible();

  await evaluate(page, "Mock success •••• 1111 (mock)");
  const path = page.getByRole("list", { name: "Evaluation path" });
  await expect(path).toBeVisible();
  for (const step of [
    "1. Request",
    "2. Matching rule",
    "3. Capability checks",
    "4. Result",
  ])
    await expect(path.getByText(step)).toBeVisible();
  await expect(path).toContainText("Provider enabled");
  await expect(path).toContainText("Route to mock");
  await expect(path.getByText("failed")).toHaveCount(0);
  expect(await count()).toBe(before);
  await axeClean(page);
});

test("an unresolved prior attempt shows unsafe failover blocked", async ({
  page,
  request,
}) => {
  const api = new ApiClient(request);
  const created = await createPayment(api, fixtures.outageMethod, "4100");
  await waitForStatus(api, created.id, "unknown");
  await page.goto(`${state().webOrigin}/dashboard/routing`);
  await evaluate(page, "Mock outage •••• 0006 (mock)", created.id);
  await expect(
    page.getByRole("heading", { name: /Unsafe failover blocked/ }),
  ).toBeVisible();
  await expect(
    page.getByRole("list", { name: "Evaluation path" }),
  ).toContainText("never routed to another provider");
  await expect(page.getByText("Route to mock")).toHaveCount(0);
});

test("invalid evaluation input is rejected with an explanation before any request", async ({
  page,
}) => {
  await page.goto(`${state().webOrigin}/dashboard/routing`);
  await page
    .getByLabel("Payment method")
    .selectOption({ label: "Mock success •••• 1111 (mock)" });
  await page.getByLabel("Amount (minor units)").fill("0");
  await page.getByRole("button", { name: "Evaluate" }).click();
  await expect(
    page.getByText("Enter a positive whole number of minor units."),
  ).toBeVisible();
});

test("providers show Simulated and Not configured labels and health only with evidence", async ({
  page,
  request,
}) => {
  const api = new ApiClient(request);
  await page.goto(`${state().webOrigin}/dashboard/providers`);
  // The grid holds mock, Stripe, Plaid and the three simulated routing-demo
  // providers; the default-demo card above it is not a provider card.
  const cards = page.locator(".capability-grid section.card");
  await expect(cards).toHaveCount(6);
  for (const name of ["Simulated Fast", "Simulated Economy", "Simulated ACH"])
    await expect(cards.filter({ hasText: name })).toContainText("Simulated");
  const mock = cards.filter({ hasText: "Mock provider" });
  await expect(mock).toContainText("Simulated");
  await expect(mock).toContainText("Idempotent replay");
  for (const name of ["Stripe", "Plaid"]) {
    const card = cards.filter({ hasText: name });
    await expect(card).toContainText("Not configured");
    await expect(card).toContainText("No observations");
    await expect(card).not.toContainText("Healthy");
  }

  const health = (await (
    await api.call("GET", "/v1/providers/health")
  ).json()) as {
    items: { code: string; status: string; observation_count: number }[];
  };
  const mockHealth = health.items.find((item) => item.code === "mock");
  expect(mockHealth).toBeTruthy();
  if (mockHealth?.status === "no_observations") {
    await expect(mock).toContainText("No observations");
  } else {
    // Whatever the badge says, it names its simulated data source.
    await expect(mock).toContainText("Simulated data");
  }
  await expect(mock).not.toContainText("Healthy (24h) · Observed");
  await axeClean(page);

  await mock.getByRole("link", { name: "Mock provider" }).click();
  await expect(page).toHaveURL(/\/dashboard\/providers\/00000000-/);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "Provider details",
  );
  await expect(
    page.getByRole("heading", { name: "Capabilities" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Routing rules" }),
  ).toBeVisible();
  await expect(page.getByText("Simulated").first()).toBeVisible();
  await axeClean(page);

  await page.goto(`${state().webOrigin}/dashboard/providers/stripe`);
  await expect(page.getByText("Not configured").first()).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "No observations" }),
  ).toBeVisible();
  await expect(page.getByText("No adapter is configured")).toBeVisible();
});

test("rules, providers and evaluation responses satisfy the OpenAPI contract", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const rules = (await (await api.call("GET", "/v1/routing/rules")).json()) as {
    items: { id: string; provider_id: string }[];
  };
  expect(rules.items.length).toBeGreaterThan(0);
  const providers = (await (await api.call("GET", "/v1/providers")).json()) as {
    items: { id: string; code: string; mode: string }[];
  };
  expect(providers.items.map((p) => [p.code, p.mode])).toEqual([
    ["mock", "simulated"],
    ["stripe", "not_configured"],
    ["plaid", "not_configured"],
    ["mock_fast", "simulated"],
    ["mock_economy", "simulated"],
    ["mock_ach", "simulated"],
  ]);
  for (const key of [providers.items[0].id, "stripe"]) {
    expect((await api.call("GET", `/v1/providers/${key}`)).status()).toBe(200);
  }
  expect((await api.call("GET", "/v1/providers/missing")).status()).toBe(404);
  const evaluated = await api.call("POST", "/v1/routing/evaluate", {
    data: {
      payment_method_id: fixtures.successMethod,
      amount_minor: "2500",
      currency: "USD",
    },
  });
  expect(evaluated.status()).toBe(200);
  const reader = await api.call("GET", "/v1/routing/rules", {
    key: state().readerKey,
  });
  expect(reader.status()).toBe(200);
});
