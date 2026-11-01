import { randomUUID } from "node:crypto";
import AxeBuilder from "@axe-core/playwright";
import { test, expect, type Page } from "@playwright/test";
import { ApiClient } from "../support/api";
import { signIn } from "../support/browser";
import {
  createPayment,
  getPayment,
  journalsFor,
  waitForStatus,
} from "../support/payments";
import { fixtures, state } from "../support/state";

test.beforeEach(async ({ context }) => {
  await signIn(context);
});

interface Run {
  id: string;
  status: string;
  counts: {
    compared: number;
    matched: number;
    unmatched: number;
    mismatch: number;
  };
  simulated: boolean;
}
interface Discrepancy {
  id: string;
  run_id: string;
  payment_id: string | null;
  kind: string;
  status: string;
  resolution_note: string | null;
  resolved_by: string | null;
  resolved_at: string | null;
  history: { to_status: string; note: string; actor: string }[];
  details: { simulated: boolean; provider_charge: unknown };
}

function window24h() {
  const now = Date.now();
  return {
    window_start: new Date(now - 24 * 3_600_000).toISOString(),
    window_end: new Date(now + 60_000).toISOString(),
  };
}

async function startRun(api: ApiClient, key: string, window = window24h()) {
  return api.call("POST", "/v1/reconciliation/runs", {
    headers: { "Idempotency-Key": key },
    data: window,
  });
}

async function unknownPayment(api: ApiClient) {
  // The seeded outage method really leaves the payment unknown: the provider
  // never recorded a charge and cannot answer a lookup.
  const created = await createPayment(
    api,
    fixtures.outageMethod,
    "4321",
    "USD",
  );
  await waitForStatus(api, created.id, "unknown");
  return created;
}

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

test("an operator runs a sandbox reconciliation, sees the unknown outcome, and resolving it only records evidence", async ({
  page,
  request,
}) => {
  const api = new ApiClient(request);
  const unknown = await unknownPayment(api);
  const journalsBefore = await journalsFor(api, unknown.id);
  expect(journalsBefore).toHaveLength(0);

  await page.goto(`${state().webOrigin}/dashboard/reconciliation`);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "Reconciliation",
  );
  const runButton = page.getByRole("button", {
    name: "Run sandbox reconciliation",
  });
  await expect(runButton).toBeEnabled();
  await runButton.click();
  const dialog = page.getByRole("dialog", {
    name: "Run sandbox reconciliation",
  });
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText("creates no journals");
  await dialog.getByRole("button", { name: "Run reconciliation" }).click();
  await expect(dialog).not.toBeVisible();

  // Count cards and the discrepancy for our payment.
  await expect(page.getByTestId("count-Compared")).not.toHaveText("0");
  // The run history cards agree with the API's stored counts.
  const latest = (await (
    await api.call("GET", "/v1/reconciliation/runs?limit=1")
  ).json()) as { items: Run[] };
  const apiCounts = latest.items[0].counts;
  expect(apiCounts.unmatched).toBeGreaterThan(0);
  for (const [label, value] of [
    ["Compared", apiCounts.compared],
    ["Matched", apiCounts.matched],
    ["Unmatched", apiCounts.unmatched],
    ["Mismatch", apiCounts.mismatch],
  ] as const) {
    await expect(page.getByTestId(`count-${label}`)).toHaveText(String(value));
  }
  await expect(page.getByText("Simulated").first()).toBeVisible();

  const row = page
    .getByRole("row")
    .filter({ hasText: unknown.id.slice(0, 8) })
    .first();
  await expect(row).toContainText("Unknown outcome");
  await row.getByRole("button", { name: /View evidence/ }).click();

  const detail = page.getByRole("complementary", {
    name: "Discrepancy detail",
  });
  await expect(detail).toBeVisible();
  await expect(detail.getByText("Simulated").first()).toBeVisible();
  await expect(detail).toContainText("No charge recorded by the mock provider");
  // Evidence, then resolution history, then related journal.
  const headings = await detail
    .getByRole("heading", { level: 3 })
    .allInnerTexts();
  expect(headings.indexOf("Evidence")).toBeGreaterThanOrEqual(0);
  expect(headings.indexOf("Evidence")).toBeLessThan(
    headings.indexOf("Resolution history"),
  );
  expect(headings.indexOf("Resolution history")).toBeLessThan(
    headings.indexOf("Related journal"),
  );
  await expect(detail).toContainText(
    "This payment has no posted success journal",
  );
  await expectNoSeriousViolations(page);

  // Resolve with a note.
  const note = `Provider outage confirmed ${randomUUID()}`;
  await detail.getByLabel("New status").selectOption("resolved");
  await detail.getByLabel("Note (required)").fill(note);
  await detail.getByRole("button", { name: "Record status change" }).click();
  await expect(detail.getByText(note)).toBeVisible();
  await expect(
    detail.getByText(/Resolution recorded a note only/),
  ).toBeVisible();
  await expect(
    detail.getByRole("button", { name: "Record status change" }),
  ).toHaveCount(0);

  // The payment and ledger are unchanged: resolution is evidence only.
  const after = await getPayment(api, unknown.id);
  expect(after.status).toBe("unknown");
  expect(await journalsFor(api, unknown.id)).toHaveLength(0);

  const listed = await api.call(
    "GET",
    `/v1/reconciliation/discrepancies?status=resolved&kind=unknown_outcome&limit=100`,
  );
  const items = ((await listed.json()) as { items: Discrepancy[] }).items;
  const stored = items.find((item) => item.payment_id === unknown.id);
  expect(stored).toBeTruthy();
  expect(stored?.resolved_by).toBe("session:operator");
  expect(stored?.resolved_at).toBeTruthy();
  expect(stored?.resolution_note).toBe(note);
  expect(stored?.history.at(-1)).toMatchObject({
    to_status: "resolved",
    note,
    actor: "session:operator",
  });
  expect(stored?.details.simulated).toBe(true);
  expect(stored?.details.provider_charge).toBeNull();
});

test("runs are idempotent: the same run key returns the stored run and creates no duplicate discrepancies", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const unknown = await unknownPayment(api);
  const key = `e2e_recon_${randomUUID()}`;
  const win = window24h();
  const first = await startRun(api, key, win);
  expect(first.status()).toBe(201);
  const run = (await first.json()) as Run;
  expect(run.status).toBe("completed");
  expect(run.simulated).toBe(true);
  expect(run.counts.unmatched).toBeGreaterThan(0);

  const list = async () =>
    (
      (await (
        await api.call(
          "GET",
          `/v1/reconciliation/discrepancies?run_id=${run.id}&limit=100`,
        )
      ).json()) as { items: Discrepancy[] }
    ).items;
  const before = await list();
  expect(before.filter((item) => item.payment_id === unknown.id)).toHaveLength(
    1,
  );

  const second = await startRun(api, key, win);
  expect(second.status()).toBe(200);
  expect(((await second.json()) as Run).id).toBe(run.id);
  const after = await list();
  expect(after.map((item) => item.id).sort()).toEqual(
    before.map((item) => item.id).sort(),
  );

  // The same key over a different window is a conflict.
  const conflict = await api.call("POST", "/v1/reconciliation/runs", {
    headers: { "Idempotency-Key": key },
    data: {
      window_start: new Date(Date.now() - 72 * 3_600_000).toISOString(),
      window_end: new Date(Date.now() - 48 * 3_600_000).toISOString(),
    },
  });
  expect(conflict.status()).toBe(409);

  // Runs are listed and no run changed the payment.
  const runs = await api.call("GET", "/v1/reconciliation/runs?limit=100");
  expect(
    ((await runs.json()) as { items: Run[] }).items.some(
      (item) => item.id === run.id,
    ),
  ).toBe(true);
  expect((await getPayment(api, unknown.id)).status).toBe("unknown");
});

test("read-only credentials can read but cannot start runs or resolve", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const unknown = await unknownPayment(api);
  const key = `e2e_recon_${randomUUID()}`;
  const run = (await (await startRun(api, key)).json()) as Run;
  const discrepancy = (
    (await (
      await api.call(
        "GET",
        `/v1/reconciliation/discrepancies?run_id=${run.id}&kind=unknown_outcome&limit=100`,
      )
    ).json()) as { items: Discrepancy[] }
  ).items.find((item) => item.payment_id === unknown.id) as Discrepancy;
  expect(discrepancy).toBeTruthy();

  const denied = await api.call("POST", "/v1/reconciliation/runs", {
    key: state().readerKey,
    headers: { "Idempotency-Key": `e2e_recon_${randomUUID()}` },
    data: window24h(),
  });
  expect(denied.status()).toBe(403);
  const noResolve = await api.call(
    "POST",
    `/v1/reconciliation/discrepancies/${discrepancy.id}/resolve`,
    {
      key: state().readerKey,
      data: { status: "resolved", note: "not allowed" },
    },
  );
  expect(noResolve.status()).toBe(403);
  const blank = await api.call(
    "POST",
    `/v1/reconciliation/discrepancies/${discrepancy.id}/resolve`,
    { data: { status: "resolved", note: "   " } },
  );
  expect(blank.status()).toBe(400);
  const readerCanRead = await api.call(
    "GET",
    `/v1/reconciliation/discrepancies/${discrepancy.id}`,
    { key: state().readerKey },
  );
  expect(readerCanRead.status()).toBe(200);
  expect(((await readerCanRead.json()) as Discrepancy).status).toBe("open");
});

test("the reconciliation page is accessible and readable at 375px and 1440px", async ({
  page,
}) => {
  for (const width of [375, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto(`${state().webOrigin}/dashboard/reconciliation`);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(
      "Reconciliation",
    );
    await expect(
      page.getByRole("button", { name: "Run sandbox reconciliation" }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await expectNoSeriousViolations(page);
  }
});
