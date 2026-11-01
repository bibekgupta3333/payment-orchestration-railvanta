import AxeBuilder from "@axe-core/playwright";
import { test, expect, type APIRequestContext } from "@playwright/test";
import { ApiClient } from "../support/api";
import { signIn } from "../support/browser";
import { state } from "../support/state";

test.beforeEach(async ({ context }) => {
  await signIn(context);
});

/** Scenario IDs the running backend implements; demos skip when missing. */
async function implemented(request: APIRequestContext): Promise<string[]> {
  const api = new ApiClient(request);
  const response = await api.call("GET", "/v1/sandbox/scenarios");
  const body = (await response.json()) as {
    items: { id: string; implemented: boolean }[];
  };
  return body.items.filter((item) => item.implemented).map((item) => item.id);
}

async function run(
  page: import("@playwright/test").Page,
  demo: string,
  button: string | RegExp,
) {
  await page.goto(`${state().webOrigin}/dashboard/playground`);
  const card = page.getByTestId(`demo-${demo}`);
  await expect(card).toBeVisible();
  await card.getByRole("button", { name: button }).click();
  await expect(card.getByTestId("run-state")).toHaveText("Completed", {
    timeout: 45_000,
  });
  return card;
}

test("page is labelled simulated, lists six demos and is axe-clean", async ({
  page,
}) => {
  await page.goto(`${state().webOrigin}/dashboard/playground`);
  await expect(
    page.getByText("SIMULATED / SANDBOX ONLY").first(),
  ).toBeVisible();
  await expect(page.locator("[data-testid^='demo-']")).toHaveCount(6);
  await expect(
    page.getByRole("button", { name: "Run all in order" }),
  ).toBeVisible();
  const results = await new AxeBuilder({ page }).analyze();
  expect(
    results.violations.filter((v) =>
      ["serious", "critical"].includes(v.impact ?? ""),
    ),
  ).toEqual([]);
});

test("demo 1 live payment: success with a balanced journal", async ({
  page,
  request,
}) => {
  test.skip(!(await implemented(request)).includes("success"));
  const card = await run(page, "live", "Run demo 1");
  await expect(card.getByText("Succeeded").first()).toBeVisible();
  await expect(card.getByTestId("balance-check")).toContainText(
    "Debits = credits",
  );
  await expect(card.getByRole("link", { name: "Payment" })).toBeVisible();
});

test("demo 2 duplicate protection: one payment, one attempt", async ({
  page,
  request,
}) => {
  test.skip(!(await implemented(request)).includes("duplicate_request"));
  const card = await run(page, "duplicate", "Run demo 2");
  await expect(card.getByText("Provider attempts")).toBeVisible();
  await expect(card.locator("dl[aria-label='Evidence counts']")).toContainText(
    /Provider attempts\s*1/,
  );
  await expect(card.getByText("Fail", { exact: true })).toHaveCount(0);
});

test("demo 3 response lost: unknown is resolved by lookup with no second charge", async ({
  page,
  request,
}) => {
  test.skip(!(await implemented(request)).includes("timeout_after_success"));
  const card = await run(page, "lost", "Run demo 3");
  const history = card.getByRole("region", { name: "State history" });
  await expect(history).toContainText("processing → unknown");
  await expect(history).toContainText("unknown → succeeded");
  await expect(card.getByTestId("no-second-charge")).toContainText(
    "No second charge",
  );
  // The card's summary list shows the journal count (the ledger panel with
  // `journal-count` belongs to demo 5).
  await expect(
    card
      .locator("dt", { hasText: /^Journals$/ })
      .locator("xpath=following-sibling::dd[1]"),
  ).toHaveText("1");
});

test("demo 5 ledger: debits equal credits and one journal after re-posting", async ({
  page,
  request,
}) => {
  test.skip(!(await implemented(request)).includes("duplicate_posting"));
  const card = await run(page, "ledger", "Run demo 5");
  await expect(card.getByTestId("balance-check")).toContainText(
    "Debits = credits",
  );
  await expect(card.getByTestId("journal-count")).toContainText(
    "still one journal",
  );
  await expect(card.getByText("provider_clearing").first()).toBeVisible();
});
