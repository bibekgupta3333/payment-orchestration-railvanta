import { test, expect, type Page } from "@playwright/test";
import { ApiClient } from "../support/api";
import { signIn } from "../support/browser";
import { state } from "../support/state";

test.beforeEach(async ({ context }) => {
  await signIn(context);
});

async function runDemo(page: Page, id: string, timeout = 30_000) {
  const card = page.getByTestId(`default-demo-${id}`);
  const button = card.getByRole("button", { name: "Run the default demo" });
  await expect(button).toBeEnabled();
  await button.click();
  await expect(page.getByTestId(`default-demo-${id}-result`)).toContainText(
    /\d+ of \d+ invariants passed/,
    { timeout },
  );
}

test("ledger: the default demo posts a journal that appears in the list", async ({
  page,
}) => {
  await page.goto(`${state().webOrigin}/dashboard/ledger`);
  await runDemo(page, "ledger");
  // Journals are on the Journal tab (Accounts is the default tab).
  await page.getByRole("tab", { name: "Journal" }).click();
  await expect(
    page.getByRole("tabpanel", { name: "Journal" }).locator("tbody tr").first(),
  ).toBeVisible({ timeout: 15_000 });
});

test("reconciliation: the default demo lists an unknown_outcome discrepancy", async ({
  page,
}) => {
  await page.goto(`${state().webOrigin}/dashboard/reconciliation`);
  await runDemo(page, "reconciliation", 45_000);
  // The filter dropdown also has an "Unknown outcome" option; look in the table.
  await expect(
    page
      .getByRole("region", { name: "Discrepancies" })
      .locator("tbody tr", { hasText: "Unknown outcome" })
      .first(),
  ).toBeVisible({ timeout: 15_000 });
});

test("events: the default demo shows an event with a duplicate count", async ({
  page,
}) => {
  await page.goto(`${state().webOrigin}/dashboard/events`);
  await runDemo(page, "events");
  const table = page.getByRole("region", { name: "Events" });
  await expect(
    table
      .locator("tbody tr")
      .filter({
        has: page.locator("td:nth-child(6)", { hasText: /^[1-9]\d*$/ }),
      })
      .first(),
  ).toBeVisible({ timeout: 15_000 });
});

test("settings: the default demo key shortcut mints a working one-time key", async ({
  page,
}) => {
  await page.goto(`${state().webOrigin}/dashboard/settings`);
  await expect(page.getByTestId("new-api-key")).toHaveCount(0);
  await page.getByRole("button", { name: "Generate default demo key" }).click();
  const panel = page.getByTestId("new-api-key");
  await expect(panel).toBeVisible();
  const secret = (
    await panel.locator("pre").first().textContent()
  )?.trim() as string;
  expect(secret).toMatch(/^rvk_[0-9a-f]{8}\.[0-9a-f]{48}$/);

  const api = new ApiClient(page.request);
  const workspace = await api.call("GET", "/v1/workspace", { key: secret });
  expect(workspace.status()).toBe(200);

  const prefix = secret.split(".")[0];
  const list = (await (await api.call("GET", "/v1/api-keys")).json()) as {
    items: { id: string; prefix: string }[];
  };
  const minted = list.items.find((item) => item.prefix === prefix);
  expect(minted).toBeDefined();
  expect(
    (await api.call("DELETE", `/v1/api-keys/${minted?.id}`)).status(),
  ).toBe(200);
});

test("hub: Load sample data runs every scenario and Payments lists several payments", async ({
  page,
}) => {
  test.setTimeout(150_000);
  await page.goto(`${state().webOrigin}/dashboard`);
  const card = page.getByTestId("sample-data-card");
  await card.getByRole("button", { name: "Load sample data" }).click();
  await expect(page.getByTestId("sample-data-progress")).toHaveText(
    /8 of 8 done\./,
    { timeout: 90_000 },
  );
  await expect(page.getByTestId("sample-data-list")).not.toContainText("error");
  await page.goto(`${state().webOrigin}/dashboard/payments`);
  await expect
    .poll(async () => page.getByRole("main").locator("tbody tr").count(), {
      timeout: 15_000,
    })
    .toBeGreaterThan(3);
});
