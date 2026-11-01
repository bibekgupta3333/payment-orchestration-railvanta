import AxeBuilder from "@axe-core/playwright";
import { test, expect, type Page } from "@playwright/test";
import { ApiClient } from "../support/api";
import { signIn } from "../support/browser";
import { createPayment, journalsFor, waitForStatus } from "../support/payments";
import { fixtures, state } from "../support/state";

const maxInt64 = "9223372036854775807";

test.beforeEach(async ({ context }) => {
  await signIn(context);
});

async function expectNoSeriousViolations(page: Page) {
  const results = await new AxeBuilder({ page }).analyze();
  expect(
    results.violations
      .filter((violation) =>
        ["serious", "critical"].includes(violation.impact ?? ""),
      )
      .map((violation) => violation.id),
  ).toEqual([]);
}

async function succeededPayment(
  api: ApiClient,
  amount: string,
  currency: string,
) {
  const created = await createPayment(
    api,
    fixtures.successMethod,
    amount,
    currency,
  );
  await waitForStatus(api, created.id, "succeeded");
  const [journal] = await journalsFor(api, created.id);
  expect(journal).toBeTruthy();
  return { payment: created, journal };
}

test("a succeeded payment's journal is balanced and labelled posted / immutable, and navigation works both ways", async ({
  page,
  request,
}) => {
  const api = new ApiClient(request);
  const { payment, journal } = await succeededPayment(api, "25000", "USD");

  // Payment detail -> journal.
  await page.goto(`${state().webOrigin}/dashboard/payments/${payment.id}`);
  await page.getByRole("tab", { name: "Journal" }).click();
  await page.getByRole("link", { name: journal.id }).click();
  await expect(page).toHaveURL(new RegExp(`/dashboard/ledger/${journal.id}`));

  await expect(page.getByRole("heading", { level: 1 })).toHaveText(journal.id);
  await expect(page.getByText("Posted / immutable")).toBeVisible();
  await expect(page.getByRole("status").first()).toContainText(
    "Debits equal credits",
  );
  const entries = page.getByRole("region", { name: "Journal entries" });
  await expect(entries.getByRole("row")).toHaveCount(3); // header + 2 lines
  await expect(entries).toContainText("debit");
  await expect(entries).toContainText("credit");
  await expect(entries).toContainText("merchant_payable");
  await expect(entries).toContainText(`provider_clearing:${fixtures.provider}`);
  await expect(entries.getByText("USD 250.00")).toHaveCount(2);
  await expectNoSeriousViolations(page);

  // No edit or delete control anywhere.
  await expect(
    page.getByRole("button", { name: /edit|delete|remove|repost|reverse/i }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("link", { name: /edit|delete|remove/i }),
  ).toHaveCount(0);

  // Journal -> payment.
  await page.getByRole("link", { name: /^Payment / }).click();
  await expect(page).toHaveURL(
    new RegExp(`/dashboard/payments/${payment.id}$`),
  );
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(payment.id);
});

test("the ledger shows accounts per currency with no cross-currency totals, and journals filter by payment", async ({
  page,
  request,
}) => {
  const api = new ApiClient(request);
  const usd = await succeededPayment(api, "1234", "USD");
  await succeededPayment(api, "777", "EUR");

  await page.goto(`${state().webOrigin}/dashboard/ledger`);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Ledger");
  await expect(page.getByRole("tab", { name: "Accounts" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  const usdGroup = page.getByRole("region", { name: "USD ledger accounts" });
  const eurGroup = page.getByRole("region", { name: "EUR ledger accounts" });
  await expect(usdGroup).toBeVisible();
  await expect(eurGroup).toBeVisible();
  await expect(usdGroup).toContainText("merchant_payable");
  await expect(usdGroup).toContainText("provider_clearing");
  await expect(usdGroup).not.toContainText("EUR ");
  await expect(eurGroup).not.toContainText("USD ");
  await expect(page.getByText(/total balance|grand total/i)).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: /edit|delete|remove/i }),
  ).toHaveCount(0);
  await expectNoSeriousViolations(page);

  // Currency filter lives in the URL.
  await page.getByLabel("Currency").selectOption("EUR");
  await page.getByRole("button", { name: "Apply filters" }).click();
  await expect(page).toHaveURL(/currency=EUR/);
  await expect(
    page.getByRole("region", { name: "USD ledger accounts" }),
  ).toHaveCount(0);
  await expect(eurGroup).toBeVisible();

  // Journal tab: payment filter held in the URL.
  await page.goto(
    `${state().webOrigin}/dashboard/ledger?tab=journal&payment_id=${usd.payment.id}`,
  );
  await expect(page.getByLabel("Payment ID")).toHaveValue(usd.payment.id);
  const journals = page.getByRole("region", { name: "Posted journals" });
  await expect(journals.getByRole("row")).toHaveCount(2); // header + 1
  await expect(journals).toContainText("USD 12.34");
  await expect(journals).toContainText("balanced");
  await expectNoSeriousViolations(page);
  await journals.getByRole("link").first().click();
  await expect(page).toHaveURL(
    new RegExp(`/dashboard/ledger/${usd.journal.id}`),
  );
});

test("large amounts display exactly and an overflowing account total is an explicit error, never a rounded number", async ({
  page,
  request,
}) => {
  const api = new ApiClient(request);
  const first = await succeededPayment(api, maxInt64, "KWD");
  await succeededPayment(api, maxInt64, "KWD");

  await page.goto(`${state().webOrigin}/dashboard/ledger/${first.journal.id}`);
  const exact = "KWD 9,223,372,036,854,775.807";
  const entries = page.getByRole("region", { name: "Journal entries" });
  await expect(entries.getByText(exact)).toHaveCount(2);
  await expect(page.getByRole("status").first()).toContainText(
    `Debits ${exact}`,
  );
  await expect(page.getByRole("status").first()).toContainText(
    "Debits equal credits",
  );

  // Two max-int64 KWD journals cannot be summed in int64: the account says so.
  const accounts = await api.call("GET", "/v1/ledger/accounts?currency=KWD");
  expect(accounts.status()).toBe(200);
  const items = (
    (await accounts.json()) as {
      items: { balance_minor: string | null; balance_error: string | null }[];
    }
  ).items;
  expect(items.length).toBeGreaterThan(0);
  for (const item of items) {
    expect(item.balance_error).toBe("amount_overflow");
    expect(item.balance_minor).toBeNull();
  }
  await page.goto(`${state().webOrigin}/dashboard/ledger?currency=KWD`);
  await expect(page.getByRole("alert").first()).toContainText(
    /amount overflow/i,
  );
});

test("unknown journals are 404 and the API offers no way to modify them", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const { journal } = await succeededPayment(api, "500", "USD");
  const missing = await api.call(
    "GET",
    "/v1/ledger/transactions/does-not-exist",
  );
  expect(missing.status()).toBe(404);
  const own = await api.call("GET", `/v1/ledger/transactions/${journal.id}`);
  expect(own.status()).toBe(200);
  const body = (await own.json()) as {
    debit_total_minor: string;
    credit_total_minor: string;
    payment: { id: string } | null;
  };
  expect(body.debit_total_minor).toBe("500");
  expect(body.credit_total_minor).toBe("500");
  expect(body.payment?.id).toBe(journal.payment_id);
  for (const method of ["PUT", "PATCH", "DELETE", "POST"]) {
    const response = await request.fetch(
      `${state().apiOrigin}/v1/ledger/transactions/${journal.id}`,
      {
        method,
        headers: { Authorization: `Bearer ${state().operatorKey}` },
        data: {},
      },
    );
    expect(response.status()).toBeGreaterThanOrEqual(400);
  }
  const after = await api.call("GET", `/v1/ledger/transactions/${journal.id}`);
  expect(await after.json()).toEqual(body);
});

test("ledger tables stay readable at 375px and 1440px", async ({ page }) => {
  for (const width of [375, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    for (const path of ["/dashboard/ledger", "/dashboard/ledger?tab=journal"]) {
      await page.goto(`${state().webOrigin}${path}`);
      await expect(page.getByRole("heading", { level: 1 })).toHaveText(
        "Ledger",
      );
      await expect(
        page
          .getByRole("region", { name: /ledger accounts|Posted journals/ })
          .first(),
      ).toBeVisible();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      ).toBe(true);
      await expectNoSeriousViolations(page);
    }
  }
});
