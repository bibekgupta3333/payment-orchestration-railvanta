import AxeBuilder from "@axe-core/playwright";
import { test, expect, type Page } from "@playwright/test";
import { ApiClient } from "../support/api";
import { signIn } from "../support/browser";
import { state } from "../support/state";

interface Invariant {
  id: string;
  status: string;
  evidence: string;
}
interface Simulation {
  id: string;
  label: string;
  status: string;
  fingerprint: string | null;
  payment: { id: string; status: string } | null;
  attempts: { provider_id: string }[];
  journals: { entries: { side: string }[] }[];
  invariants: Invariant[];
  webhooks: {
    event_type: string;
    outcome: string | null;
    outcome_reason: string | null;
    duplicate_count: number;
  }[];
  events: { event_type: string }[];
  request: { amount_minor: string };
  routing: { failover_blocked: boolean };
}

test.beforeEach(async ({ context }) => {
  await signIn(context);
});

async function runScenario(page: Page, label: string, seed: string) {
  await page.goto(`${state().webOrigin}/dashboard/simulator`);
  const select = page.getByRole("combobox", { name: "Scenario" });
  await expect(select).toBeEnabled();
  await select.selectOption({ label });
  await page.getByLabel("Seed").fill(seed);
  await page.getByRole("button", { name: "Run scenario" }).click();
  await expect(page).toHaveURL(/\/dashboard\/simulator\?run=/);
  await expect(page.getByTestId("run-status")).toHaveText("Completed", {
    timeout: 30_000,
  });
}

test("the simulated badge is persistent and every catalogued scenario is enabled", async ({
  page,
}) => {
  await page.goto(`${state().webOrigin}/dashboard/simulator`);
  await expect(
    page.getByText("SIMULATED / SANDBOX ONLY").first(),
  ).toBeVisible();
  const select = page.getByRole("combobox", { name: "Scenario" });
  await expect(select).toBeEnabled();
  for (const name of [/Duplicate webhook/, /Reordered events/]) {
    const option = select.getByRole("option", { name });
    await expect(option).toBeEnabled();
    await expect(option).not.toHaveText(/waiting for/);
  }
  await expect(page.getByText(/Unavailable: waiting for/)).toHaveCount(0);
  await expect(
    select.getByRole("option", { name: "Success", exact: true }),
  ).toBeEnabled();
  const results = await new AxeBuilder({ page }).analyze();
  expect(
    results.violations.filter((v) =>
      ["serious", "critical"].includes(v.impact ?? ""),
    ),
  ).toEqual([]);
});

test("timeout after success runs the real pipeline and the backend stores passing invariants with evidence", async ({
  page,
  request,
}) => {
  await runScenario(page, "Timeout after success", "ui-gate");
  // The accepted-run announcement is made once and never rewritten by polling.
  await expect(page.locator("p[aria-live='polite'].sr-only")).toHaveText(
    /Simulation run accepted/,
  );
  await expect(
    page.getByText("SIMULATED / SANDBOX ONLY").first(),
  ).toBeVisible();
  const timeline = page.getByRole("list", { name: "Execution timeline" });
  await expect(timeline).toContainText("Payment processing to unknown");
  await expect(timeline).toContainText("Payment unknown to succeeded");
  await expect(timeline).toContainText("Journal posted");

  await page.getByRole("tab", { name: "Invariants" }).click();
  const panel = page.getByRole("tabpanel");
  const rows = panel.locator("tbody tr");
  await expect(rows).toHaveCount(6);
  for (let i = 0; i < 6; i++) {
    await expect(rows.nth(i)).toContainText("Pass");
    await expect(rows.nth(i)).not.toContainText("Pending");
  }
  await expect(panel).toContainText("1 succeeded provider charge(s)");

  await page.getByRole("tab", { name: "Journal" }).click();
  const journal = page.getByRole("tabpanel");
  await expect(journal).toContainText("provider_clearing");
  await expect(journal.locator("tbody tr")).toHaveCount(2);
  await page.getByRole("tab", { name: "Attempts" }).click();
  await expect(page.getByRole("tabpanel").locator("tbody tr")).toHaveCount(1);
  await page.getByRole("tab", { name: "Request" }).click();
  await expect(page.getByRole("tabpanel")).toContainText("sim_");

  // Reload keeps the run: it is addressed by the URL and read from the API.
  await page.reload();
  await expect(page.getByTestId("run-status")).toHaveText("Completed");

  const id = new URL(page.url()).searchParams.get("run") as string;
  const api = new ApiClient(request);
  const response = await api.call("GET", `/v1/sandbox/simulations/${id}`);
  const run = (await response.json()) as Simulation;
  expect(run.label).toBe("SIMULATED / SANDBOX ONLY");
  expect(run.payment?.status).toBe("succeeded");
  expect(run.journals).toHaveLength(1);
  expect(run.attempts).toHaveLength(1);
  expect(run.invariants.every((item) => item.status === "pass")).toBe(true);
});

test("duplicate webhook delivers one event repeatedly through the inbox and the backend reports one effect", async ({
  page,
  request,
}) => {
  await runScenario(page, "Duplicate webhook", "ui-dupes");
  await page.getByRole("tab", { name: "Events" }).click();
  const inbox = page.getByRole("region", { name: "Webhook inbox" });
  await expect(inbox.locator("tbody tr")).toHaveCount(1);
  await expect(inbox).toContainText("payment.succeeded");
  await page.getByRole("tab", { name: "Invariants" }).click();
  const panel = page.getByRole("tabpanel");
  await expect(panel).toContainText("duplicate_count");
  await expect(
    panel.locator("tbody tr").filter({ hasText: "Pending" }),
  ).toHaveCount(0);
  await expect(
    panel.locator("tbody tr").filter({ hasText: "Fail" }),
  ).toHaveCount(0);

  const id = new URL(page.url()).searchParams.get("run") as string;
  const api = new ApiClient(request);
  const run = (await (
    await api.call("GET", `/v1/sandbox/simulations/${id}`)
  ).json()) as Simulation;
  expect(run.payment?.status).toBe("succeeded");
  expect(run.journals).toHaveLength(1);
  expect(run.webhooks).toHaveLength(1);
  expect(run.webhooks[0].duplicate_count).toBeGreaterThanOrEqual(2);
  expect(run.invariants.length).toBeGreaterThan(6);
  expect(run.invariants.every((item) => item.status === "pass")).toBe(true);
});

test("reordered events never regress the terminal state and keep the late failure for reconciliation", async ({
  page,
  request,
}) => {
  await runScenario(page, "Reordered events", "ui-reorder");
  await page.getByRole("tab", { name: "Events" }).click();
  const inbox = page.getByRole("region", { name: "Webhook inbox" });
  await expect(inbox.locator("tbody tr")).toHaveCount(3);
  await expect(inbox).toContainText("payment.failed");
  await expect(inbox).toContainText("terminal_state");
  await expect(
    page.getByRole("region", { name: "Durable events" }),
  ).toContainText("payment.late_outcome");
  await page.getByRole("tab", { name: "Invariants" }).click();
  await expect(
    page
      .getByRole("tabpanel")
      .locator("tbody tr")
      .filter({ hasText: "Pending" }),
  ).toHaveCount(0);

  const id = new URL(page.url()).searchParams.get("run") as string;
  const api = new ApiClient(request);
  const run = (await (
    await api.call("GET", `/v1/sandbox/simulations/${id}`)
  ).json()) as Simulation;
  expect(run.payment?.status).toBe("succeeded");
  expect(run.journals).toHaveLength(1);
  expect(run.webhooks.at(-1)?.outcome).toBe("ignored");
  expect(run.webhooks.at(-1)?.outcome_reason).toBe("terminal_state");
  expect(run.events.map((e) => e.event_type)).toContain("payment.late_outcome");
  expect(run.invariants.every((item) => item.status === "pass")).toBe(true);
});

test("an outage ends unknown, explains the blocked failover and never moves to another provider", async ({
  page,
  request,
}) => {
  await runScenario(page, "Provider outage", "ui-outage");
  await expect(page.getByText("Outcome unknown").first()).toBeVisible();
  await expect(
    page.getByRole("heading", { name: /Unsafe failover blocked/ }).first(),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: /retry/i })).toHaveCount(0);
  await page.getByRole("tab", { name: "Routing" }).click();
  await expect(page.getByRole("tabpanel")).toContainText(
    "never routed to another provider",
  );
  await page.getByRole("tab", { name: "Attempts" }).click();
  await expect(page.getByRole("tabpanel").locator("tbody tr")).toHaveCount(1);
  await page.getByRole("tab", { name: "Journal" }).click();
  await expect(page.getByRole("tabpanel")).toContainText("No journal posted");

  const id = new URL(page.url()).searchParams.get("run") as string;
  const api = new ApiClient(request);
  const run = (await (
    await api.call("GET", `/v1/sandbox/simulations/${id}`)
  ).json()) as Simulation;
  expect(run.payment?.status).toBe("unknown");
  expect(run.routing.failover_blocked).toBe(true);
  expect(new Set(run.attempts.map((a) => a.provider_id)).size).toBe(1);
  expect(run.invariants.every((item) => item.status === "pass")).toBe(true);
});

test("the same seed gives identical states and invariant results; unknown scenarios return 422", async ({
  request,
}) => {
  const api = new ApiClient(request);
  const start = async (scenario: string, seed: string) => {
    const response = await api.call("POST", "/v1/sandbox/simulations", {
      data: { scenario, seed },
    });
    expect(response.status()).toBe(202);
    const accepted = (await response.json()) as Simulation;
    expect(accepted.label).toBe("SIMULATED / SANDBOX ONLY");
    // Until the backend has evidence, invariants are pending, never pass.
    if (accepted.status === "running") {
      expect(accepted.invariants.every((i) => i.status === "pending")).toBe(
        true,
      );
    }
    let latest = accepted;
    await expect
      .poll(
        async () => {
          latest = (await (
            await api.call("GET", `/v1/sandbox/simulations/${accepted.id}`)
          ).json()) as Simulation;
          return latest.status;
        },
        { timeout: 30_000, intervals: [100, 200, 400] },
      )
      .toBe("completed");
    return latest;
  };
  for (const scenario of [
    "success",
    "timeout_after_success",
    "duplicate_request",
    "duplicate_posting",
    "timeout_before_execution",
    "duplicate_webhook",
    "reordered_events",
  ]) {
    const first = await start(scenario, "repeat-seed");
    const second = await start(scenario, "repeat-seed");
    expect(second.payment?.id).not.toBe(first.payment?.id);
    expect(second.fingerprint).toBe(first.fingerprint);
    expect(second.fingerprint).toBeTruthy();
    expect(second.request.amount_minor).toBe(first.request.amount_minor);
    expect(second.payment?.status).toBe(first.payment?.status);
    expect(second.invariants.map((i) => [i.id, i.status])).toEqual(
      first.invariants.map((i) => [i.id, i.status]),
    );
    expect(first.invariants.every((i) => i.status === "pass")).toBe(true);
  }

  const unknown = await api.call("POST", "/v1/sandbox/simulations", {
    data: { scenario: "made_up" },
  });
  expect(unknown.status()).toBe(422);
  const reader = await api.call("POST", "/v1/sandbox/simulations", {
    key: state().readerKey,
    data: { scenario: "success" },
  });
  expect(reader.status()).toBe(403);
});
