import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SampleDataCard } from "@/components/dashboard/Hub";
import { EventExplorer } from "@/components/events/EventExplorer";
import { LedgerExplorer } from "@/components/ledger/LedgerExplorer";
import { ReconciliationView } from "@/components/reconciliation/ReconciliationView";
import { ApiKeys } from "@/components/settings/ApiKeys";
import { sampleScenarios } from "@/lib/hooks/sample-data";

vi.mock("next/navigation", () => ({
  usePathname: () => "/dashboard/x",
  useRouter: () => ({ push: vi.fn() }),
  useSearchParams: () => new URLSearchParams(""),
}));

const workspace = {
  id: "00000000-0000-7000-8000-000000000001",
  name: "Demo workspace",
  environment: "sandbox",
};
const SECRET = "rvk_1a2b3c4d.0123456789abcdef0123456789abcdef0123456789abcdef";

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json", "x-request-id": "req_t" },
  });
}

function catalog(ids: string[]) {
  return {
    label: "SIMULATED / SANDBOX ONLY",
    items: ids.map((id) => ({
      id,
      label: id,
      description: id,
      implemented: true,
      missing_dependency: null,
      injected_fault: "none",
    })),
  };
}

function simulation(
  id: string,
  scenario: string,
  status: "running" | "completed",
  fail = false,
) {
  const now = "2026-11-01T10:00:00Z";
  return {
    id,
    label: "SIMULATED / SANDBOX ONLY",
    scenario,
    seed: "s",
    status,
    payment_id: null,
    injected_fault: "none",
    created_at: now,
    completed_at: status === "running" ? null : now,
    failure_reason: null,
    fingerprint: null,
    request: {
      payment_method_id: "pm",
      amount_minor: "10000",
      currency: "USD",
      idempotency_key: "k",
      merchant_reference: "r",
    },
    response: { status_code: 202, body: {} },
    payment: null,
    routing: { failover_blocked: false, failover_reason: null, decisions: [] },
    attempts: [],
    transitions: [],
    events: [],
    webhooks: [],
    journals: [],
    invariants:
      status === "running"
        ? []
        : [
            { id: "a", title: "A", status: "pass", evidence: "ok" },
            {
              id: "b",
              title: "B",
              status: fail ? "fail" : "pass",
              evidence: "x",
            },
          ],
    timeline: [],
  };
}

const empty = { items: [], next_cursor: null };

interface Options {
  scenarios?: string[];
  /** Polls that return "running" before a run completes. */
  runningPolls?: number;
  failScenario?: string;
  networkErrorOn?: string;
}

function mockFetch(options: Options = {}) {
  const posts: { path: string; body: Record<string, unknown> }[] = [];
  const log: string[] = [];
  const runs = new Map<string, { scenario: string; polls: number }>();
  let n = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input).split("?")[0];
      const method = (init?.method ?? "GET").toUpperCase();
      if (method === "POST") {
        const body = JSON.parse(String(init?.body));
        posts.push({ path, body });
        if (path === "/v1/api-keys")
          return json(
            {
              key: {
                id: "k1",
                name: body.name,
                prefix: "rvk_1a2b3c4d",
                role: body.role,
                created_at: "2026-11-01T10:00:00Z",
                expires_at: "2026-11-02T10:00:00Z",
                revoked_at: null,
                status: "active",
              },
              secret: SECRET,
            },
            201,
          );
        if (path === "/v1/sandbox/simulations") {
          if (body.scenario === options.networkErrorOn)
            throw new TypeError("network down");
          const id = `sim_${++n}`;
          runs.set(id, { scenario: body.scenario, polls: 0 });
          log.push(`start:${body.scenario}`);
          return json(simulation(id, body.scenario, "running"), 202);
        }
      }
      if (path === "/v1/workspace") return json(workspace);
      if (path === "/v1/session") return json({ role: "operator" });
      if (path === "/v1/api-keys") return json({ items: [], limit: 20 });
      if (path === "/v1/sandbox/scenarios")
        return json(catalog(options.scenarios ?? [...sampleScenarios]));
      if (path.startsWith("/v1/sandbox/simulations/")) {
        const id = path.split("/").pop() ?? "";
        const run = runs.get(id);
        if (!run)
          return json(
            {
              code: "not_found",
              message: "x",
              request_id: "r",
              field_errors: [],
            },
            404,
          );
        run.polls += 1;
        const done = run.polls > (options.runningPolls ?? 0);
        if (done) log.push(`settled:${run.scenario}`);
        return json(
          simulation(
            id,
            run.scenario,
            done ? "completed" : "running",
            run.scenario === options.failScenario,
          ),
        );
      }
      if (
        path === "/v1/ledger/transactions" ||
        path === "/v1/ledger/accounts" ||
        path === "/v1/reconciliation/runs" ||
        path === "/v1/events"
      )
        return json(empty);
      return json(
        {
          code: "not_found",
          message: "Missing",
          request_id: "r",
          field_errors: [],
        },
        404,
      );
    }),
  );
  return { posts, log };
}

function renderWith(node: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const spy = vi.spyOn(client, "invalidateQueries");
  render(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
  return { client, spy };
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("default demos on ledger, reconciliation and events", () => {
  it("ledger shows its default demo and points the empty state at it", async () => {
    const { posts } = mockFetch();
    renderWith(<LedgerExplorer />);
    const card = await screen.findByTestId("default-demo-ledger");
    expect(
      within(card).getByRole("button", { name: "Run the default demo" }),
    ).toBeInTheDocument();
    expect(card).toHaveTextContent(
      "a balanced journal and proof that a duplicate post adds nothing",
    );
    expect(
      await screen.findByText(
        /Run the default demo to post your first journal/,
      ),
    ).toBeVisible();
    expect(posts).toEqual([]);
  });

  it("reconciliation shows the demo with the simulated-evidence note", async () => {
    const { posts } = mockFetch();
    renderWith(<ReconciliationView />);
    const card = await screen.findByTestId("default-demo-reconciliation");
    expect(card).toHaveTextContent("takes about 6 seconds");
    expect(card).toHaveTextContent(/simulated/i);
    expect(card).toHaveTextContent(/nothing here adjusts the ledger/i);
    expect(
      await screen.findByText(/run the default demo above/i),
    ).toBeVisible();
    expect(posts).toEqual([]);
  });

  it("events shows the demo and an empty state that mentions it", async () => {
    const { posts } = mockFetch();
    renderWith(<EventExplorer />);
    const card = await screen.findByTestId("default-demo-events");
    expect(card).toHaveTextContent(
      "webhook inbox events with a duplicate count",
    );
    expect(await screen.findByText(/Run the default demo above/)).toBeVisible();
    expect(posts).toEqual([]);
  });

  it("each demo starts its own scenario", async () => {
    const user = userEvent.setup();
    const { posts } = mockFetch();
    renderWith(<EventExplorer />);
    const button = await screen.findByRole("button", {
      name: "Run the default demo",
    });
    await waitFor(() => expect(button).toBeEnabled());
    await user.click(button);
    await waitFor(() => expect(posts).toHaveLength(1));
    expect(posts[0].body).toMatchObject({
      scenario: "duplicate_webhook",
      amount_minor: "10000",
      currency: "USD",
    });
  });
});

describe("settings default key shortcut", () => {
  it("mints exactly the default key on click and not on load", async () => {
    const user = userEvent.setup();
    const { posts } = mockFetch();
    renderWith(<ApiKeys role="operator" />);
    const button = await screen.findByRole("button", {
      name: "Generate default demo key",
    });
    expect(posts).toEqual([]);
    expect(screen.getByLabelText("Key name")).toHaveValue("Default demo key");
    await user.click(button);
    expect(await screen.findByTestId("new-api-key")).toBeVisible();
    expect(screen.getByText(SECRET)).toBeVisible();
    expect(posts).toEqual([
      {
        path: "/v1/api-keys",
        body: {
          name: "Default demo key",
          role: "operator",
          expires_in_hours: 24,
        },
      },
    ]);
  });
});

describe("sample data", () => {
  async function runCard(options: Options) {
    const user = userEvent.setup();
    const mocks = mockFetch(options);
    const { spy } = renderWith(<SampleDataCard />);
    expect(mocks.posts).toEqual([]);
    await user.click(screen.getByRole("button", { name: "Load sample data" }));
    return { ...mocks, spy };
  }

  it("runs the scenarios strictly one at a time, in order, then refreshes", async () => {
    const { posts, log, spy } = await runCard({ runningPolls: 1 });
    await waitFor(
      () =>
        expect(screen.getByTestId("sample-data-progress")).toHaveTextContent(
          "8 of 8 done.",
        ),
      { timeout: 30_000 },
    );
    expect(posts.map((p) => p.body.scenario)).toEqual([...sampleScenarios]);
    // start(n+1) only after settled(n).
    const expected = sampleScenarios.flatMap((s) => [
      `start:${s}`,
      `settled:${s}`,
    ]);
    expect(log).toEqual(expected);
    expect(spy).toHaveBeenCalledWith({ queryKey: ["railvanta"] });
    expect(screen.getByTestId("sample-data-list")).toHaveTextContent(
      "2 of 2 invariants passed",
    );
  }, 40_000);

  it("skips unavailable scenarios and reports them", async () => {
    const available = sampleScenarios.filter((s) => s !== "outage");
    const { posts } = await runCard({ scenarios: [...available] });
    await waitFor(
      () =>
        expect(screen.getByTestId("sample-data-progress")).toHaveTextContent(
          "8 of 8 done.",
        ),
      { timeout: 15_000 },
    );
    expect(posts.map((p) => p.body.scenario)).toEqual(available);
    expect(screen.getByRole("note")).toHaveTextContent(/Skipped.*outage/);
  }, 20_000);

  it("continues after a failed invariant and reports it", async () => {
    const { posts } = await runCard({ failScenario: "decline" });
    await waitFor(
      () =>
        expect(screen.getByTestId("sample-data-progress")).toHaveTextContent(
          "8 of 8 done.",
        ),
      { timeout: 15_000 },
    );
    expect(posts).toHaveLength(8);
    expect(screen.getByTestId("sample-data-list")).toHaveTextContent(
      "1 of 2 invariants passed; failed: B",
    );
  }, 20_000);

  it("stops on a network error", async () => {
    const { posts } = await runCard({ networkErrorOn: "duplicate_request" });
    await waitFor(
      () =>
        expect(screen.getByTestId("sample-data-progress")).toHaveTextContent(
          "(stopped)",
        ),
      { timeout: 15_000 },
    );
    expect(posts.map((p) => p.body.scenario)).toEqual([
      "success",
      "decline",
      "duplicate_request",
    ]);
  }, 20_000);
});
