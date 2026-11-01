import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { PlaygroundView } from "@/components/playground/PlaygroundView";
import { LedgerEvidence } from "@/components/playground/RunResult";
import { RoutingDemo } from "@/components/playground/RoutingDemo";
import type { Simulation } from "@/lib/api/routing-schemas";

vi.mock("next/navigation", () => ({
  usePathname: () => "/dashboard/playground",
  useRouter: () => ({ push: vi.fn() }),
  useSearchParams: () => new URLSearchParams(),
}));

const workspace = {
  id: "00000000-0000-7000-8000-000000000001",
  name: "Demo workspace",
  environment: "sandbox",
};

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json", "x-request-id": "req_test" },
  });
}

const catalog = {
  label: "SIMULATED / SANDBOX ONLY",
  items: [
    "success",
    "duplicate_request",
    "timeout_after_success",
    "outage",
    "duplicate_posting",
  ].map((id) => ({
    id,
    label: id,
    description: id,
    implemented: true,
    missing_dependency: null,
    injected_fault: "none",
  })),
};

const BIG = "9007199254740993"; // 2^53 + 1

function simulation(overrides: Partial<Simulation> = {}): Simulation {
  const now = "2026-11-01T10:00:00Z";
  return {
    id: "sim_1",
    label: "SIMULATED / SANDBOX ONLY",
    scenario: "duplicate_posting",
    seed: "pg",
    status: "completed",
    payment_id: "pay_1",
    injected_fault: "none",
    created_at: now,
    completed_at: now,
    failure_reason: null,
    fingerprint: null,
    request: {
      payment_method_id: "pm_1",
      amount_minor: BIG,
      currency: "USD",
      idempotency_key: "k",
      merchant_reference: "r",
    },
    response: { status_code: 202, body: {} },
    payment: {
      id: "pay_1",
      amount_minor: BIG,
      currency: "USD",
      payment_method_id: "pm_1",
      merchant_reference: null,
      status: "succeeded",
      created_at: now,
      updated_at: now,
    },
    routing: { failover_blocked: false, failover_reason: null, decisions: [] },
    attempts: [],
    transitions: [
      {
        from_status: "processing",
        to_status: "succeeded",
        reason: "provider confirmed",
        actor_reference: "system",
        created_at: now,
      },
    ],
    events: [],
    webhooks: [],
    journals: [
      {
        id: "jrn_1",
        payment_id: "pay_1",
        currency: "USD",
        status: "posted",
        posting_key: "pk",
        reversal_of: null,
        created_at: now,
        posted_at: now,
        entries: [
          {
            id: "e1",
            account_id: "a1",
            account_code: "provider_clearing",
            provider_id: null,
            side: "debit",
            amount_minor: BIG,
          },
          {
            id: "e2",
            account_id: "a2",
            account_code: "merchant_payable",
            provider_id: null,
            side: "credit",
            amount_minor: BIG,
          },
        ],
      },
    ],
    invariants: [
      {
        id: "one_journal",
        title: "Exactly one journal",
        status: "pass",
        evidence: "journals=1 after 2 extra postings",
      },
    ],
    timeline: [
      {
        at: now,
        source: "ledger",
        title: "Journal posted",
        detail: "balanced",
      },
    ],
    ...overrides,
  };
}

function setup(run: Simulation, scenarioBody: unknown = catalog) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input).split("?")[0];
      if (path === "/v1/workspace") return json(workspace);
      if (path === "/v1/sandbox/scenarios") return json(scenarioBody);
      if (path === "/v1/payment-methods")
        return json({ items: [], next_cursor: null });
      if (path === "/v1/sandbox/simulations") return json(run, 202);
      if (path.startsWith("/v1/sandbox/simulations/")) return json(run);
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
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <PlaygroundView />
    </QueryClientProvider>,
  );
}

describe("playground", () => {
  it("renders cards from the scenario list and disables missing scenarios", async () => {
    setup(simulation());
    const ledgerCard = await screen.findByTestId("demo-ledger");
    expect(
      within(ledgerCard).getByRole("button", { name: "Run demo 5" }),
    ).toBeEnabled();
    const recon = screen.getByTestId("demo-reconciliation");
    expect(
      within(recon).getByRole("button", { name: "Run demo 6" }),
    ).toBeDisabled();
    expect(
      within(recon).getByText(/backend scenario not available yet/),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Run all in order" }),
    ).toBeDisabled();
    expect(
      screen.getAllByText("SIMULATED / SANDBOX ONLY").length,
    ).toBeGreaterThan(0);
  });

  it("shows steps, invariants and exact money after running a card", async () => {
    setup(simulation());
    const user = userEvent.setup();
    const card = await screen.findByTestId("demo-ledger");
    await user.click(within(card).getByRole("button", { name: "Run demo 5" }));
    expect(await within(card).findByText("Exactly one journal")).toBeVisible();
    expect(
      within(card).getByText(/journals=1 after 2 extra postings/),
    ).toBeVisible();
    expect(within(card).getByText(/processing → succeeded/)).toBeVisible();
    expect(within(card).getByTestId("balance-check")).toHaveTextContent(
      "Debits = credits",
    );
    expect(within(card).getByTestId("journal-count")).toHaveTextContent(
      "still one journal",
    );
    await waitFor(() =>
      expect(screen.getByTestId("announcer")).toHaveTextContent(
        /1 of 1 invariants passed/,
      ),
    );
  });

  it("offers no retry action while the payment outcome is unknown", async () => {
    setup(
      simulation({
        scenario: "outage",
        journals: [],
        payment: { ...simulation().payment!, status: "unknown" },
      }),
    );
    const user = userEvent.setup();
    const card = await screen.findByTestId("demo-lost");
    await user.click(
      within(card).getByRole("button", { name: /provider outage/i }),
    );
    expect(
      await within(card).findByText("Outcome unknown", { selector: "h4" }),
    ).toBeVisible();
    expect(
      within(card).queryByRole("button", { name: /retry/i }),
    ).not.toBeInTheDocument();
  });

  it("shows why a provider was chosen and blocks failover for an unknown payment", async () => {
    const bodies: unknown[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input).split("?")[0];
        if (path === "/v1/workspace") return json(workspace);
        if (path === "/v1/payment-methods")
          return json({
            items: [
              {
                id: "00000000-0000-7000-8000-000000000133",
                rail: "card",
                brand: "visa",
                last_four: "4242",
                created_at: "2026-11-01T00:00:00Z",
              },
            ],
            next_cursor: null,
          });
        if (path === "/v1/routing/evaluate") {
          bodies.push(JSON.parse(String(init?.body)));
          return json({
            provider_id: "00000000-0000-7000-8000-000000000211",
            provider_code: "mock_fast",
            rule_id: "00000000-0000-7000-8000-000000000731",
            priority: 0,
            reason:
              "Unsafe failover blocked: the payment is resolved at mock_fast",
            rule: null,
            checks: [],
            candidates: [],
            failover_blocked: true,
            alternatives: [
              {
                provider_id: "00000000-0000-7000-8000-000000000211",
                provider_code: "mock_fast",
                status: "pinned",
                eligible: true,
                rule_id: null,
                priority: 0,
                reasons: [],
              },
              {
                provider_id: "00000000-0000-7000-8000-000000000212",
                provider_code: "mock_economy",
                status: "blocked",
                eligible: false,
                rule_id: null,
                priority: 1,
                reasons: ["unsafe failover blocked: prior attempt unresolved"],
              },
            ],
          });
        }
        return json(
          {
            code: "not_found",
            message: "x",
            request_id: "r",
            field_errors: [],
          },
          404,
        );
      }),
    );
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    render(
      <QueryClientProvider client={client}>
        <RoutingDemo />
      </QueryClientProvider>,
    );
    const user = userEvent.setup();
    await user.type(
      await screen.findByLabelText(/existing payment id/i),
      "01a1977d-545d-7254-be10-8062e76ddc8b",
    );
    await user.click(screen.getByRole("button", { name: /evaluate routing/i }));
    const list = await screen.findByTestId("routing-alternatives");
    expect(within(list).getByText(/Pinned \(lookup only/)).toBeVisible();
    expect(within(list).getByText(/Blocked: unsafe failover/)).toBeVisible();
    expect(
      await screen.findByText(/Unknown outcome — failover blocked/),
    ).toBeVisible();
    expect(bodies[0]).toMatchObject({
      payment_id: "01a1977d-545d-7254-be10-8062e76ddc8b",
    });
  });

  it("renders amounts above 2^53 exactly", () => {
    render(<LedgerEvidence run={simulation()} />);
    expect(
      screen.getAllByText("USD 90,071,992,547,409.93").length,
    ).toBeGreaterThan(0);
  });
});
