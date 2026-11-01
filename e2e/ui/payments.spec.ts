import { randomUUID } from "node:crypto";
import AxeBuilder from "@axe-core/playwright";
import {
  test,
  expect,
  type APIRequestContext,
  type Page,
  type Request,
} from "@playwright/test";
import { ApiClient } from "../support/api";
import { signIn } from "../support/browser";
import { waitForStatus } from "../support/payments";
import { fixtures, startApi, state, stopApi } from "../support/state";

interface Payment {
  id: string;
  amount_minor: string;
  currency: string;
  merchant_reference: string | null;
  status: string;
}

const terminal = ["succeeded", "failed", "canceled"];
const statusLabels: Record<string, string> = {
  created: "Created",
  processing: "Processing",
  succeeded: "Succeeded",
  failed: "Failed",
  canceled: "Canceled",
  unknown: "Outcome unknown",
};

async function paymentsByReference(
  request: APIRequestContext,
  reference: string,
): Promise<Payment[]> {
  const api = new ApiClient(request);
  const response = await api.call(
    "GET",
    `/v1/payments?limit=100&q=${encodeURIComponent(reference)}`,
  );
  expect(response.status()).toBe(200);
  return ((await response.json()) as { items: Payment[] }).items;
}

async function createViaApi(
  request: APIRequestContext,
  amount: string,
  currency: string,
  reference: string,
  method: string = fixtures.method,
): Promise<Payment> {
  const api = new ApiClient(request);
  const response = await api.call("POST", "/v1/payments", {
    headers: { "Idempotency-Key": `e2e_${randomUUID()}` },
    data: {
      amount_minor: amount,
      currency,
      payment_method_id: method,
      merchant_reference: reference,
    },
  });
  expect(response.status()).toBe(202);
  return (await response.json()) as Payment;
}

function capturePaymentPosts(page: Page): Request[] {
  const posts: Request[] = [];
  page.on("request", (request) => {
    if (
      request.method() === "POST" &&
      new URL(request.url()).pathname === "/v1/payments"
    )
      posts.push(request);
  });
  return posts;
}

async function openCreateDialog(page: Page) {
  await page.goto(`${state().webOrigin}/dashboard/payments`);
  const trigger = page.getByRole("button", {
    name: "Create sandbox payment",
    exact: true,
  });
  await expect(trigger).toBeEnabled();
  await trigger.click();
  const dialog = page.getByRole("dialog", { name: "Create sandbox payment" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByLabel("Payment method")).not.toHaveValue("");
  return { dialog, trigger };
}

test.beforeEach(async ({ context }) => {
  await signIn(context);
});

test("a created payment shows as pending, never optimistic success, and its detail shows the API state", async ({
  page,
  request,
}) => {
  const reference = `ui_create_${randomUUID()}`;
  const posts = capturePaymentPosts(page);
  const { dialog } = await openCreateDialog(page);
  // 2^53 + 1 minor units: must round-trip exactly.
  await dialog.getByLabel("Amount").fill("90071992547409.93");
  await dialog.getByLabel("Merchant reference (optional)").fill(reference);
  await dialog
    .getByRole("button", { name: "Create sandbox payment", exact: true })
    .click();

  await expect(
    dialog.getByRole("heading", { name: "Payment accepted — outcome pending" }),
  ).toBeVisible();
  await expect(dialog.getByText("USD 90,071,992,547,409.93")).toBeVisible();
  await expect(dialog.getByText("Succeeded")).toHaveCount(0);
  expect(posts).toHaveLength(1);
  const body = posts[0].postDataJSON() as Record<string, unknown>;
  expect(body.amount_minor).toBe("9007199254740993");

  const [created] = await paymentsByReference(request, reference);
  expect(created.amount_minor).toBe("9007199254740993");
  await dialog.getByRole("link", { name: "View payment status" }).click();
  await expect(page).toHaveURL(new RegExp(`/dashboard/payments/${created.id}`));
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(created.id);
  await expect(
    page.getByText("USD 90,071,992,547,409.93").first(),
  ).toBeVisible();

  // The page shows whatever the API reports now; success only if confirmed.
  const api = new ApiClient(request);
  const current = (await (
    await api.call("GET", `/v1/payments/${created.id}`)
  ).json()) as Payment;
  const heading = page.locator(".page-heading");
  if (terminal.includes(current.status)) {
    await expect(
      page.getByText("Final state confirmed by the API"),
    ).toBeVisible();
  } else {
    await expect(
      page.getByText(/Checking status every 3 seconds/),
    ).toBeVisible();
    await expect(heading.getByText("Succeeded")).toHaveCount(0);
  }
  await expect(heading.locator(".status-pill")).toContainText(
    statusLabels[
      (
        (await (
          await api.call("GET", `/v1/payments/${created.id}`)
        ).json()) as Payment
      ).status
    ],
  );
  await expect(page.getByText("Idempotency key")).toBeVisible();
  await expect(
    page.locator("dd.mono").filter({ hasText: /^\*{4}/ }),
  ).toBeVisible();
  for (const tab of ["Attempts", "Events", "Journal"]) {
    await page.getByRole("tab", { name: tab }).click();
    await expect(page.getByRole("tabpanel")).toBeVisible();
  }
  await expect(page.getByRole("tabpanel")).toContainText(
    /Journal appears once posting is available|No journal posted|Journal /,
  );
});

test("double-clicking submit and reloading while pending still create exactly one payment", async ({
  page,
  request,
}) => {
  // Double-click: the request is slowed so the second click lands while the
  // first is in flight.
  const doubleReference = `ui_double_${randomUUID()}`;
  const posts = capturePaymentPosts(page);
  await page.route("**/v1/payments", async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    await new Promise((resolve) => setTimeout(resolve, 1_000));
    await route.continue();
  });
  let { dialog } = await openCreateDialog(page);
  await dialog.getByLabel("Amount").fill("12.34");
  await dialog
    .getByLabel("Merchant reference (optional)")
    .fill(doubleReference);
  await dialog
    .getByRole("button", { name: "Create sandbox payment", exact: true })
    .dblclick();
  await expect(
    dialog.getByRole("heading", { name: "Payment accepted — outcome pending" }),
  ).toBeVisible();
  expect(posts).toHaveLength(1);
  expect(await paymentsByReference(request, doubleReference)).toHaveLength(1);
  await page.unroute("**/v1/payments");

  // Reload while pending: the request reaches the API, but its response is
  // held until after the page has reloaded, so the browser never sees it.
  const reloadReference = `ui_reload_${randomUUID()}`;
  posts.length = 0;
  await page.route("**/v1/payments", async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    try {
      const response = await route.fetch();
      await new Promise((resolve) => setTimeout(resolve, 3_000));
      await route.fulfill({ response });
    } catch {
      /* The page reloaded first; the response is intentionally lost. */
    }
  });
  ({ dialog } = await openCreateDialog(page));
  await dialog.getByLabel("Amount").fill("56.78");
  await dialog
    .getByLabel("Merchant reference (optional)")
    .fill(reloadReference);
  const sent = page.waitForRequest(
    (candidate) =>
      candidate.method() === "POST" &&
      new URL(candidate.url()).pathname === "/v1/payments",
  );
  await dialog
    .getByRole("button", { name: "Create sandbox payment", exact: true })
    .click();
  await sent;
  await expect(dialog.getByText("Submitting…").first()).toBeVisible();
  await expect
    .poll(
      async () => (await paymentsByReference(request, reloadReference)).length,
    )
    .toBe(1);
  await page.reload();
  await page.unroute("**/v1/payments");

  const notice = page.getByText(
    "A payment submission from this tab was not confirmed.",
  );
  await expect(notice).toBeVisible();
  await page
    .getByRole("button", { name: "Review unconfirmed submission" })
    .click();
  dialog = page.getByRole("dialog", { name: "Create sandbox payment" });
  await expect(dialog.getByLabel("Amount")).toHaveValue("56.78");
  await expect(dialog.getByLabel("Merchant reference (optional)")).toHaveValue(
    reloadReference,
  );
  await dialog
    .getByRole("button", { name: "Create sandbox payment", exact: true })
    .click();
  await expect(
    dialog.getByText("replayed the original response", { exact: false }),
  ).toBeVisible();

  const reloadPosts = posts.filter(
    (post) =>
      (post.postDataJSON() as { merchant_reference?: string })
        .merchant_reference === reloadReference,
  );
  expect(reloadPosts).toHaveLength(2);
  const keys = await Promise.all(
    reloadPosts.map((post) => post.headerValue("idempotency-key")),
  );
  expect(keys[0]).toBeTruthy();
  expect(keys[1]).toBe(keys[0]);
  expect(await paymentsByReference(request, reloadReference)).toHaveLength(1);
});

test("an unknown payment shows Outcome unknown with an explanation and no retry action", async ({
  page,
  request,
}) => {
  const created = await createViaApi(
    request,
    "4200",
    "USD",
    `ui_unknown_${randomUUID()}`,
    fixtures.outageMethod,
  );
  // The outage mock method produces a real unknown outcome that its lookup
  // cannot resolve, so the payment stays unknown.
  await waitForStatus(new ApiClient(request), created.id, "unknown");
  await page.goto(`${state().webOrigin}/dashboard/payments/${created.id}`);
  await expect(
    page.getByRole("heading", { name: /Outcome unknown — checking status/ }),
  ).toBeVisible();
  await expect(page.getByText(/resolved by reconciliation/)).toBeVisible();
  await expect(
    page.getByText("Awaiting reconciliation evidence."),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: /retry|resend|provider|failover/i }),
  ).toHaveCount(0);
  await expect(page.getByText(/Processing → Outcome unknown/)).toBeVisible();

  await page.goto(`${state().webOrigin}/dashboard`);
  const banner = page.locator(".attention-banner");
  await expect(banner).toContainText("unknown outcome");
  await expect(
    banner.getByRole("link", { name: `Inspect ${created.id}` }),
  ).toBeVisible();
  await expect(banner.getByRole("button")).toHaveCount(0);
  await page.goto(`${state().webOrigin}/dashboard/payments?status=unknown`);
  await expect(page.getByRole("link", { name: created.id })).toBeVisible();
  await expect(
    page.getByRole("button", { name: /retry|resend|failover/i }),
  ).toHaveCount(0);
});

test("list search, filters, sort and cursor live in the URL and survive reload and back navigation", async ({
  page,
  request,
}) => {
  const base = 7_000_000 + Math.floor(Math.random() * 1_000_000) * 10;
  const references: string[] = [];
  for (let index = 1; index <= 3; index++) {
    const reference = `ui_list_${index}_${randomUUID()}`;
    references.push(reference);
    await createViaApi(request, String(base + index), "KWD", reference);
  }
  await page.goto(`${state().webOrigin}/dashboard/payments?limit=2`);
  const form = page.getByRole("form", { name: "Payment filters" });
  await form.getByLabel("Currency").selectOption("KWD");
  await form.getByLabel("Min amount (minor units)").fill(String(base + 1));
  await form.getByLabel("Max amount (minor units)").fill(String(base + 3));
  await form.getByLabel("Sort").selectOption("created_asc");
  await form.getByRole("button", { name: "Apply filters" }).click();
  await expect(page).toHaveURL(/currency=KWD/);
  await expect(page).toHaveURL(/sort=created_asc/);
  const table = page.getByRole("region", { name: "Payments" });
  await expect(table.getByRole("row")).toHaveCount(3); // header + 2
  await expect(table).toContainText(references[0]);
  await expect(table).toContainText(references[1]);
  const firstPage = page.url();

  await page.getByRole("button", { name: "Next page" }).click();
  await expect(page).toHaveURL(/cursor=/);
  await expect(table.getByRole("row")).toHaveCount(2);
  await expect(table).toContainText(references[2]);
  const secondPage = page.url();

  await page.reload();
  await expect(page).toHaveURL(secondPage);
  await expect(table.getByRole("row")).toHaveCount(2);
  await expect(table).toContainText(references[2]);
  await expect(form.getByLabel("Currency")).toHaveValue("KWD");
  await expect(form.getByLabel("Sort")).toHaveValue("created_asc");

  await page.goBack();
  await expect(page).toHaveURL(firstPage);
  await expect(table.getByRole("row")).toHaveCount(3);
  await expect(table).toContainText(references[0]);

  // Detail links keep the list query so "Back to payments" restores it.
  await table.getByRole("link").first().click();
  await page.getByRole("link", { name: "Back to payments" }).click();
  await expect(page).toHaveURL(firstPage);

  // Search by merchant_reference.
  await form.getByLabel("Search payment ID or reference").fill(references[1]);
  await form.getByRole("button", { name: "Apply filters" }).click();
  await expect(page).toHaveURL(/q=ui_list_2_/);
  await expect(table.getByRole("row")).toHaveCount(2);
  await expect(table).toContainText(references[1]);
  await page.reload();
  await expect(table.getByRole("row")).toHaveCount(2);
  await expect(form.getByLabel("Search payment ID or reference")).toHaveValue(
    references[1],
  );

  await form
    .getByLabel("Search payment ID or reference")
    .fill(`none_${randomUUID()}`);
  await form.getByRole("button", { name: "Apply filters" }).click();
  await expect(
    page.getByRole("heading", { name: "No payments match these filters" }),
  ).toBeVisible();
});

test("with the API stopped, pages show the request ID and Retry, and never present stale data as current", async ({
  page,
  request,
}) => {
  const created = await createViaApi(
    request,
    "999",
    "USD",
    `ui_offline_${randomUUID()}`,
  );
  // Stale: a loaded detail page whose refresh fails is labelled stale.
  await page.goto(`${state().webOrigin}/dashboard/payments/${created.id}`);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(created.id);
  await stopApi();
  try {
    await page.getByRole("button", { name: "Refresh status" }).click();
    const stale = page.locator(".stale-notice");
    await expect(stale).toContainText("Showing stale data.", {
      timeout: 20_000,
    });
    await expect(stale).toContainText("Request ID:");
    await expect(stale.getByRole("button", { name: "Retry" })).toBeVisible();

    for (const route of [
      "/dashboard",
      "/dashboard/payments",
      `/dashboard/payments/${created.id}`,
    ]) {
      await page.goto(`${state().webOrigin}${route}`);
      const alert = page
        .getByRole("alert")
        .filter({ hasText: "Unable to load this view" })
        .first();
      await expect(alert).toBeVisible({ timeout: 20_000 });
      await expect(alert).toContainText(/Request ID: \S+/);
      await expect(alert.getByRole("button", { name: "Retry" })).toBeVisible();
      await expect(page.locator(".payment-table")).toHaveCount(0);
      await expect(page.locator(".metric-value")).toHaveCount(0);
    }
  } finally {
    await startApi();
  }
  await page
    .getByRole("alert")
    .filter({ hasText: "Unable to load this view" })
    .first()
    .getByRole("button", { name: "Retry" })
    .click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(created.id, {
    timeout: 20_000,
  });
});

test("overview metrics show their source and never sum different currencies", async ({
  page,
  request,
}) => {
  await createViaApi(request, "100000", "USD", `ui_overview_${randomUUID()}`);
  await createViaApi(request, "500", "JPY", `ui_overview_${randomUUID()}`);
  const api = new ApiClient(request);
  const summary = (await (
    await api.call("GET", "/v1/dashboard/summary?window=24h")
  ).json()) as {
    volumes: { currency: string; amount_minor: string }[];
    completion_latency_p95_ms: number | null;
    success_rate_percent: string | null;
  };
  expect(new Set(summary.volumes.map((v) => v.currency)).size).toBe(
    summary.volumes.length,
  );
  await page.goto(`${state().webOrigin}/dashboard`);
  await expect(page.locator(".source-line")).toContainText(
    /^Source: Observed sandbox API data/,
  );
  const volumeCard = page.locator(".metric-card").filter({
    has: page.getByRole("heading", { name: "Accepted volume by currency" }),
  });
  const entries = volumeCard.locator(".volume-list li");
  await expect(entries).toHaveCount(summary.volumes.length);
  const currencies = await entries.locator(".metric-value").allTextContents();
  expect(currencies.map((text) => text.slice(0, 3)).sort()).toEqual(
    summary.volumes.map((v) => v.currency).sort(),
  );
  await expect(volumeCard).toContainText("never summed across currencies");
  if (summary.completion_latency_p95_ms === null) {
    await expect(
      page
        .locator(".metric-card")
        .filter({ hasText: "Observed p95 completion" })
        .getByText("No observations"),
    ).toBeVisible();
  }
  if (summary.success_rate_percent === null) {
    await expect(
      page
        .locator(".metric-card")
        .filter({ hasText: "Success rate" })
        .getByText("No observations"),
    ).toBeVisible();
  }
  const chart = page.locator(".chart-card");
  await expect(chart).toContainText("Source: Observed sandbox API data");
  await expect(chart).toContainText("Range:");
  await expect(chart).toContainText("Last updated");
  await chart.getByText("Show volume data table").click();
  await expect(
    chart.getByRole("region", { name: "Payment volume data" }),
  ).toContainText("USD");
});

for (const width of [375, 1440]) {
  test(`payment screens fit ${width}px, are keyboard reachable and pass axe`, async ({
    page,
    request,
  }) => {
    test.setTimeout(90_000);
    const created = await createViaApi(
      request,
      "250000",
      "EUR",
      `ui_layout_${width}_${randomUUID()}`,
    );
    await page.setViewportSize({ width, height: 900 });
    for (const route of [
      "/",
      "/dashboard",
      "/dashboard/payments",
      `/dashboard/payments/${created.id}`,
    ]) {
      await page.goto(`${state().webOrigin}${route}`);
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
      if (route !== "/")
        await expect(page.locator(".skeleton")).toHaveCount(0, {
          timeout: 15_000,
        });
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      ).toBe(true);
      const results = await new AxeBuilder({ page }).analyze();
      expect(
        results.violations
          .filter((violation) =>
            ["serious", "critical"].includes(violation.impact ?? ""),
          )
          .map((violation) => violation.id),
      ).toEqual([]);
    }

    // The create dialog is keyboard operable, traps focus, closes on Escape
    // and returns focus to its trigger.
    await page.goto(`${state().webOrigin}/dashboard/payments`);
    const trigger = page.getByRole("button", {
      name: "Create sandbox payment",
      exact: true,
    });
    await expect(trigger).toBeEnabled();
    await trigger.focus();
    await page.keyboard.press("Enter");
    const dialog = page.getByRole("dialog", { name: "Create sandbox payment" });
    await expect(dialog).toBeVisible();
    for (let index = 0; index < 14; index++) {
      await page.keyboard.press("Tab");
      expect(
        await dialog.evaluate((element) =>
          element.contains(document.activeElement),
        ),
      ).toBe(true);
    }
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    const results = await new AxeBuilder({ page }).include("dialog").analyze();
    expect(
      results.violations
        .filter((violation) =>
          ["serious", "critical"].includes(violation.impact ?? ""),
        )
        .map((violation) => violation.id),
    ).toEqual([]);
    await page.keyboard.press("Escape");
    await expect(dialog).not.toBeVisible();
    await expect(trigger).toBeFocused();

    // Detail tabs are reachable with the keyboard.
    await page.goto(`${state().webOrigin}/dashboard/payments/${created.id}`);
    const summaryTab = page.getByRole("tab", { name: "Summary" });
    await summaryTab.focus();
    await page.keyboard.press("ArrowRight");
    await expect(page.getByRole("tab", { name: "Attempts" })).toBeFocused();
    await expect(page.getByRole("tab", { name: "Attempts" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });
}
