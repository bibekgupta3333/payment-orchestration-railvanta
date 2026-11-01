import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { HealthBadge } from "@/components/provider-views/HealthBadge";
import {
  EvaluationPath,
  RoutingView,
  mutationDisabledReason,
} from "@/components/routing/RoutingView";
import {
  FailoverNotice,
  InvariantStatus,
  SimulatorView,
} from "@/components/simulator/SimulatorView";
import type {
  ProviderHealth,
  RoutingDecision,
  Simulation,
} from "@/lib/api/routing-schemas";

let runParam: string | null = null;
vi.mock("next/navigation", () => ({
  usePathname: () => "/dashboard/simulator",
  useRouter: () => ({ push: vi.fn() }),
  useSearchParams: () => new URLSearchParams(runParam ? `run=${runParam}` : ""),
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

function wrap(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

const health = (overrides: Partial<ProviderHealth>): ProviderHealth => ({
  id: "p",
  code: "mock",
  configured: true,
  data_source: "simulated",
  observation_count: 0,
  status: "no_observations",
  windows: [],
  ...overrides,
});

describe("HealthBadge", () => {
  it("never shows a rate or green status without observations", () => {
    render(<HealthBadge health={health({})} />);
    expect(screen.getByText(/No observations/)).toBeInTheDocument();
    expect(screen.queryByText(/Healthy/)).not.toBeInTheDocument();
    expect(screen.queryByText(/%/)).not.toBeInTheDocument();
  });

  it("does not call a small sample healthy", () => {
    render(
      <HealthBadge
        health={health({ status: "insufficient_data", observation_count: 3 })}
      />,
    );
    expect(screen.getByText(/Not enough evidence/)).toBeInTheDocument();
    expect(screen.queryByText(/Healthy/)).not.toBeInTheDocument();
  });

  it("labels evidence-backed health with its data source", () => {
    render(
      <HealthBadge
        health={health({ status: "healthy", observation_count: 40 })}
      />,
    );
    expect(screen.getByText(/Healthy \(24h\)/)).toHaveTextContent(
      /Simulated data/,
    );
  });
});

const baseRun: Simulation = {
  id: "run1",
  label: "SIMULATED / SANDBOX ONLY",
  scenario: "outage",
  seed: "demo",
  status: "running",
  payment_id: "pay1",
  injected_fault: "provider unavailable",
  created_at: "2026-10-31T10:00:00.000Z",
  completed_at: null,
  failure_reason: null,
  fingerprint: null,
  request: {
    payment_method_id: "m",
    amount_minor: "2500",
    currency: "USD",
    idempotency_key: "sim_run1",
    merchant_reference: "sim:run1",
  },
  response: null,
  payment: null,
  routing: { failover_blocked: false, failover_reason: null, decisions: [] },
  attempts: [],
  transitions: [],
  events: [],
  journals: [],
  invariants: [
    {
      id: "a",
      title: "One payment per key",
      status: "pending",
      evidence: "Waiting",
    },
  ],
  timeline: [],
};

describe("simulator pieces", () => {
  it("explains a blocked failover only when the backend reports one", () => {
    const { rerender } = render(<FailoverNotice run={baseRun} />);
    expect(
      screen.queryByText("Unsafe failover blocked"),
    ).not.toBeInTheDocument();
    rerender(
      <FailoverNotice
        run={{
          ...baseRun,
          routing: {
            ...baseRun.routing,
            failover_blocked: true,
            failover_reason: "attempt 1 is unknown",
          },
        }}
      />,
    );
    expect(screen.getByText("Unsafe failover blocked")).toBeInTheDocument();
    expect(screen.getByText("attempt 1 is unknown")).toBeInTheDocument();
  });

  it("renders pending invariants as text, not as a pass", () => {
    render(<InvariantStatus status="pending" />);
    expect(screen.getByText("Pending")).toBeInTheDocument();
    expect(screen.queryByText("Pass")).not.toBeInTheDocument();
  });
});

describe("EvaluationPath", () => {
  const decision: RoutingDecision = {
    provider_id: "prov1",
    provider_code: "mock",
    rule_id: "rule1",
    priority: 0,
    reason: "Rule rule1 matches",
    rule: {
      id: "rule1",
      provider_id: "prov1",
      provider_code: "mock",
      rail: "mock",
      currency: "USD",
      priority: 0,
      min_amount_minor: "1",
      max_amount_minor: null,
      enabled: true,
    },
    checks: [
      { id: "a", label: "Provider enabled", passed: true, detail: "enabled" },
      {
        id: "b",
        label: "Currency matches",
        passed: false,
        detail: "EUR vs USD",
      },
    ],
    candidates: [],
    failover_blocked: false,
  };
  const request = { method: "m", amount: "2500", currency: "USD" };

  it("shows request, rule, checks and result in order with pass and fail text", () => {
    render(<EvaluationPath request={request} decision={decision} />);
    const steps = within(screen.getByRole("list", { name: "Evaluation path" }));
    expect(steps.getByText("1. Request")).toBeInTheDocument();
    expect(steps.getByText("2. Matching rule")).toBeInTheDocument();
    expect(steps.getByText("3. Capability checks")).toBeInTheDocument();
    expect(steps.getByText("4. Result")).toBeInTheDocument();
    expect(steps.getByText("passed", { exact: false })).toBeInTheDocument();
    expect(steps.getByText("failed", { exact: false })).toBeInTheDocument();
  });

  it("states unsafe failover blocked instead of a route", () => {
    render(
      <EvaluationPath
        request={request}
        decision={{
          ...decision,
          failover_blocked: true,
          reason: "Unsafe failover blocked: attempt 1 unknown",
        }}
      />,
    );
    expect(
      screen.getByRole("heading", { name: /Unsafe failover blocked/ }),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Route to/)).not.toBeInTheDocument();
  });
});

describe("RoutingView", () => {
  it("disables rule mutation and says why", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) =>
        url.endsWith("/v1/workspace")
          ? json(workspace)
          : url.endsWith("/v1/payment-methods")
            ? json({ items: [], next_cursor: null })
            : json({ items: [], next_cursor: null }),
      ),
    );
    render(wrap(<RoutingView />));
    expect(await screen.findByText("No routing rules")).toBeInTheDocument();
    for (const name of ["Create rule", "Reorder"]) {
      expect(screen.getByRole("button", { name })).toBeDisabled();
    }
    expect(
      screen.getByText(new RegExp(mutationDisabledReason)),
    ).toBeInTheDocument();
  });
});

describe("SimulatorView", () => {
  const catalog = {
    label: "SIMULATED / SANDBOX ONLY",
    items: [
      {
        id: "success",
        label: "Success",
        description: "Charge succeeds.",
        implemented: true,
        missing_dependency: null,
        injected_fault: "none",
      },
      {
        id: "duplicate_webhook",
        label: "Duplicate webhook",
        description: "Delivered twice.",
        implemented: false,
        missing_dependency:
          "Webhook inbox and signature verification (WBS 4.3)",
        injected_fault: "duplicate webhook delivery",
      },
    ],
  };

  it("keeps the badge visible, disables unimplemented scenarios and names the dependency", async () => {
    runParam = null;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) =>
        url.endsWith("/v1/workspace") ? json(workspace) : json(catalog),
      ),
    );
    const user = userEvent.setup();
    render(wrap(<SimulatorView />));
    expect(screen.getByText("SIMULATED / SANDBOX ONLY")).toBeInTheDocument();
    const select = await screen.findByRole("combobox", { name: "Scenario" });
    const disabled = within(select).getByRole("option", {
      name: /Duplicate webhook/,
    });
    expect(disabled).toBeDisabled();
    expect(disabled).toHaveTextContent("waiting for Webhook inbox");
    expect(
      screen.getByText(/Unavailable: waiting for Webhook inbox/),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Run scenario" }));
    expect(screen.getByText("No run selected")).toBeInTheDocument();
  });

  it("shows pending invariants and no progress until the backend has evidence", async () => {
    runParam = "run1";
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) =>
        url.endsWith("/v1/workspace")
          ? json(workspace)
          : url.includes("/v1/sandbox/simulations/")
            ? json(baseRun)
            : json(catalog),
      ),
    );
    const user = userEvent.setup();
    render(wrap(<SimulatorView />));
    expect(await screen.findByTestId("run-status")).toHaveTextContent(
      "Running: waiting for the backend pipeline",
    );
    expect(
      screen.getByRole("button", { name: "Refresh now" }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "Invariants" }));
    expect(screen.getAllByText("Pending").length).toBeGreaterThan(0);
    expect(screen.queryByText("Pass")).not.toBeInTheDocument();
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
  });
});
