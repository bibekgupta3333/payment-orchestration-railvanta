import AxeBuilder from "@axe-core/playwright";
import { test, expect } from "@playwright/test";
import { ApiClient } from "../support/api";
import { signIn } from "../support/browser";
import { eventId, sendWebhook, webhookBody } from "../support/events";
import { createPayment, journalsFor, waitForStatus } from "../support/payments";
import { fixtures, state } from "../support/state";

test.beforeEach(async ({ context }) => {
  await signIn(context);
});

/** A payment resolved by a webhook, a duplicate, a changed payload and a late failure. */
async function seedScenario(api: ApiClient) {
  const payment = await createPayment(api, fixtures.outageMethod, "800");
  await waitForStatus(api, payment.id, "unknown");
  const resolving = eventId("evt_ui_resolve");
  const body = webhookBody(resolving, "payment.succeeded", payment.id);
  expect((await sendWebhook(api, body)).status()).toBe(202);
  expect((await sendWebhook(api, body)).status()).toBe(200);
  expect(
    (
      await sendWebhook(api, {
        ...body,
        provider_reference: "ref_changed",
      })
    ).status(),
  ).toBe(409);
  const late = eventId("evt_ui_late");
  expect(
    (
      await sendWebhook(api, webhookBody(late, "payment.failed", payment.id))
    ).status(),
  ).toBe(202);
  return { payment, resolving, late };
}

test("events page shows duplicates, outcomes, investigation and signature status with URL filters", async ({
  page,
  request,
}) => {
  const api = new ApiClient(request);
  const { payment, resolving, late } = await seedScenario(api);

  await page.goto(`${state().webOrigin}/dashboard/events?q=${payment.id}`);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Events");
  await expect(
    page.getByText("Source: simulated mock events (sandbox)"),
  ).toBeVisible();
  await expect(page.getByText(/^Updated /)).toBeVisible();
  const table = page.getByRole("region", { name: "Events" });
  const resolvedRow = table.locator("tr", { hasText: resolving });
  await expect(resolvedRow).toContainText("Needs investigation");
  await expect(resolvedRow).toContainText("Not applicable");
  await expect(
    resolvedRow.getByRole("cell").nth(5), // Duplicates column
  ).toHaveText("1");
  const lateRow = table.locator("tr", { hasText: late });
  await expect(lateRow).toContainText("Ignored");
  await expect(lateRow).toContainText("terminal state");
  await expect(
    table.locator("tr", { hasText: "payment.dispatch" }),
  ).toBeVisible();
  await expect(page.getByText("Verified")).toHaveCount(0);

  // Filters live in the URL and survive reload.
  await page.getByLabel("State").selectOption("investigation");
  await page.getByRole("button", { name: "Apply filters" }).click();
  await expect(page).toHaveURL(/state=investigation/);
  await expect(page).toHaveURL(new RegExp(`q=${payment.id}`));
  await expect(table.locator("tbody tr")).toHaveCount(1);
  await page.reload();
  await expect(page.getByLabel("State")).toHaveValue("investigation");
  await expect(table.locator("tbody tr")).toHaveCount(1);
  await page.getByRole("button", { name: "Clear filters" }).click();
  await expect(page).toHaveURL(`${state().webOrigin}/dashboard/events`);

  // Refresh reads again and keeps the label.
  await page.goto(`${state().webOrigin}/dashboard/events?q=${payment.id}`);
  const refreshed = page.waitForResponse(
    (response) =>
      response.url().includes("/v1/events") && response.status() === 200,
  );
  await page.getByRole("button", { name: "Refresh" }).click();
  await refreshed;
  await expect(page.getByRole("button", { name: "Refresh" })).toBeEnabled();

  // Cursor pagination in the URL.
  await page.goto(
    `${state().webOrigin}/dashboard/events?q=${payment.id}&limit=2`,
  );
  await page.getByRole("button", { name: "Next page" }).click();
  await expect(page).toHaveURL(/cursor=/);
  await expect(page).toHaveURL(/limit=2/);
  await page.goBack();
  await expect(page).not.toHaveURL(/cursor=/);

  const results = await new AxeBuilder({ page }).analyze();
  expect(
    results.violations.filter((v) =>
      ["serious", "critical"].includes(v.impact ?? ""),
    ),
  ).toEqual([]);
});

test("event detail shows safe fields only, never raw JSON or secrets, and no trace link by default", async ({
  page,
  request,
}) => {
  const api = new ApiClient(request);
  const { payment, resolving } = await seedScenario(api);
  await page.goto(`${state().webOrigin}/dashboard/events?q=${resolving}`);
  await page.getByRole("link", { name: "payment.succeeded" }).click();
  await expect(page).toHaveURL(/\/dashboard\/events\/whk_/);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "Event details",
  );
  await expect(page.getByText("Signature: Not applicable")).toBeVisible();
  await expect(page.getByText("Duplicate deliveries")).toBeVisible();
  await expect(page.getByRole("main").getByRole("alert")).toContainText(
    "Needs investigation",
  );
  await expect(page.getByText("Stored payload hash")).toBeVisible();
  await expect(page.getByText("Conflicting hash")).toBeVisible();
  await expect(
    page.getByRole("link", { name: payment.id }).first(),
  ).toBeVisible();
  await expect(page.getByText("The raw payload is never shown")).toBeVisible();
  await expect(page.getByRole("link", { name: "Open trace" })).toHaveCount(0);

  const html = await page.content();
  for (const forbidden of [
    state().operatorKey,
    state().sessionToken,
    state().csrfToken,
    "ref_changed",
    '"event_id"',
    "Idempotency-Key",
  ])
    expect(html).not.toContain(forbidden);

  await page.getByRole("link", { name: "← Back to events" }).click();
  await expect(page).toHaveURL(/\/dashboard\/events\?q=/);

  const results = await new AxeBuilder({ page }).analyze();
  expect(
    results.violations.filter((v) =>
      ["serious", "critical"].includes(v.impact ?? ""),
    ),
  ).toEqual([]);

  const missing = await page.goto(
    `${state().webOrigin}/dashboard/events/whk_missing`,
  );
  expect(missing?.status()).toBeLessThan(500);
  await expect(page.getByRole("main").getByRole("alert")).toContainText(
    /not found|Event not found/i,
  );
});

test("events fit a 375px viewport with cards instead of a wide table", async ({
  page,
  request,
}) => {
  const api = new ApiClient(request);
  const { payment } = await seedScenario(api);
  await page.setViewportSize({ width: 375, height: 900 });
  await page.goto(`${state().webOrigin}/dashboard/events?q=${payment.id}`);
  const cards = page.getByRole("list", { name: "Events" });
  await expect(cards).toBeVisible();
  await expect(
    cards.getByText(/Signature: Not applicable/).first(),
  ).toBeVisible();
  await expect(page.getByRole("region", { name: "Events" })).toBeHidden();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});

test("three deliveries show a duplicate count of 2 with one ledger posting, and the audit timeline matches the API", async ({
  page,
  request,
}) => {
  const api = new ApiClient(request);
  const payment = await createPayment(api, fixtures.outageMethod, "800");
  await waitForStatus(api, payment.id, "unknown");
  const event = eventId("evt_ui_dup");
  const body = webhookBody(event, "payment.succeeded", payment.id);
  for (const status of [202, 200, 200])
    expect((await sendWebhook(api, body)).status()).toBe(status);
  await waitForStatus(api, payment.id, "succeeded");
  expect(await journalsFor(api, payment.id)).toHaveLength(1);

  await page.goto(`${state().webOrigin}/dashboard/events?q=${event}`);
  const row = page
    .getByRole("region", { name: "Events" })
    .locator("tr", { hasText: event });
  await expect(row.getByRole("cell").nth(5)).toHaveText("2");
  await expect(row).not.toContainText("Needs investigation");

  // The audit timeline on payment detail matches the API's transition history.
  const history = (await (
    await api.call("GET", `/v1/payments/${payment.id}/history`)
  ).json()) as {
    transitions: { to_status: string; actor_reference: string }[];
  };
  await page.goto(`${state().webOrigin}/dashboard/payments/${payment.id}`);
  const items = page.locator("ol.timeline li");
  await expect(items).toHaveCount(history.transitions.length);
  const last = history.transitions[history.transitions.length - 1];
  expect(last.to_status).toBe("succeeded");
  await expect(items.last()).toContainText("Succeeded");
  await expect(items.last()).toContainText(last.actor_reference);
  expect(
    history.transitions.filter((t) => t.to_status === "succeeded"),
  ).toHaveLength(1);
});
