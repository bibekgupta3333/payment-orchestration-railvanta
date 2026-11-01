import { test, expect } from "@playwright/test";
import { signIn } from "../support/browser";
import { state } from "../support/state";

test.beforeEach(async ({ context }) => {
  await signIn(context);
});

test("payments: the default demo creates a succeeded $100.00 payment in the list", async ({
  page,
}) => {
  await page.goto(`${state().webOrigin}/dashboard/payments`);
  const card = page.getByTestId("default-demo-payments");
  await expect(card).toBeVisible();
  await card.getByRole("button", { name: "Run the default demo" }).click();
  await expect(page.getByTestId("default-demo-payments-result")).toContainText(
    "invariants passed",
    { timeout: 30_000 },
  );
  await expect(
    page
      .locator("tbody tr")
      .filter({ hasText: "USD 100.00" })
      .filter({ hasText: "Succeeded" })
      .first(),
  ).toBeVisible({ timeout: 15_000 });
});

test("routing: opens with a decision, then the outage demo shows failover blocked", async ({
  page,
}) => {
  await page.goto(`${state().webOrigin}/dashboard/routing`);
  // No click: the evaluator is preconfigured and ran once on load.
  const path = page.getByRole("list", { name: "Evaluation path" });
  await expect(path).toBeVisible();
  await expect(path).toContainText("Route to");
  await expect(page.getByLabel("Amount (minor units)")).toHaveValue("10000");

  const card = page.getByTestId("default-demo-routing");
  await card.getByRole("button", { name: "Run the default demo" }).click();
  await expect(page.getByTestId("default-demo-routing-result")).toBeVisible({
    timeout: 30_000,
  });
  await expect(
    page.getByRole("heading", { name: /Unsafe failover blocked/ }),
  ).toBeVisible({ timeout: 15_000 });
  await expect(page.getByLabel(/Existing payment ID/)).not.toHaveValue("");
});

test("providers: the default demo runs and the Mock provider gains observations", async ({
  page,
}) => {
  await page.goto(`${state().webOrigin}/dashboard/providers`);
  const card = page.getByTestId("default-demo-providers");
  await expect(card).toBeVisible();
  await card.getByRole("button", { name: "Run the default demo" }).click();
  await expect(page.getByTestId("default-demo-providers-result")).toContainText(
    "invariants passed",
    { timeout: 30_000 },
  );
  // Provider cards only (the default-demo card above also says "Mock provider").
  const cards = page.locator(".capability-grid section.card");
  const mock = cards.filter({ hasText: "Mock provider" });
  await expect(mock).not.toContainText("No observations", { timeout: 15_000 });
  await expect(cards.filter({ hasText: "Stripe" })).toContainText(
    "Not configured",
  );
});

test("simulator: Run default demo produces a timeline with invariants", async ({
  page,
}) => {
  await page.goto(`${state().webOrigin}/dashboard/simulator`);
  await expect(page.getByRole("combobox", { name: "Scenario" })).toHaveValue(
    "timeout_after_success",
  );
  await page.getByRole("button", { name: "Run default demo" }).click();
  await expect(page).toHaveURL(/\/dashboard\/simulator\?run=/);
  await expect(page.getByTestId("run-status")).toHaveText("Completed", {
    timeout: 30_000,
  });
  await expect(
    page.getByRole("list", { name: "Execution timeline" }),
  ).toBeVisible();
  await page.getByRole("tab", { name: "Invariants" }).click();
  await expect(
    page.getByRole("region", { name: "Invariant results" }),
  ).toBeVisible();
  await expect(page.getByText("Pass").first()).toBeVisible();
});
