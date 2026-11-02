import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PaymentExplorer } from "@/components/payments/PaymentExplorer";
import { ProvidersView } from "@/components/provider-views/ProvidersView";
import { RoutingView } from "@/components/routing/RoutingView";
import { SimulatorView } from "@/components/simulator/SimulatorView";

vi.mock("next/navigation", () => ({
  usePathname: () => "/dashboard/test",
  useRouter: () => ({ push: vi.fn() }),
  useSearchParams: () => new URLSearchParams(),
}));

const workspace = {
  id: "00000000-0000-7000-8000-000000000001",
  name: "Demo workspace",
  environment: "sandbox",
};
const successMethod = {
  id: "00000000-0000-7000-8000-000000000121",
  rail: "mock",
  brand: "Mock success",
  last_four: "1111",
};
const routingMethod = {
  id: "00000000-0000-7000-8000-000000000131",
  rail: "mock",
  brand: "Routing demo card",
  last_four: "0131",
};
const scenario = (id: string, label: string) => ({
  id,
  label,
  description: `${label} description.`,
  implemented: true,
  missing_dependency: null,
  injected_fault: "none",
});
const catalog = {
  label: "SIMULATED / SANDBOX ONLY",
  items: [
    scenario("success", "Success"),
    scenario("outage", "Outage"),
    scenario("timeout_after_success", "Timeout after success"),
  ],
};

function run(scenarioId: string, status: "running" | "completed") {
  return {
    id: "run1",
    label: "SIMULATED / SANDBOX ONLY",
    scenario: scenarioId,
    seed: "s",
    status,
    payment_id: "pay1",
    injected_fault: "none",
    created_at: "2026-10-31T10:00:00.000Z",
    completed_at: null,
    failure_reason: null,
    fingerprint: null,
    request: {
      payment_method_id: routingMethod.id,
      amount_minor: "10000",
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
    webhooks: [],
    journals: [],
    invariants:
      status === "completed"
        ? [{ id: "a", title: "One payment", status: "pass", evidence: "ok" }]
        : [],
    timeline: [],
  };
}

const decision = (blocked: boolean) => ({
  provider_id: "prov1",
  provider_code: "mock",
  rule_id: "rule1",
  priority: 0,
  reason: blocked
    ? "Unsafe failover blocked: attempt 1 is unknown"
    : "Rule rule1 matches",
  rule: null,
  checks: [
    { id: "a", label: "Provider enabled", passed: true, detail: "enabled" },
  ],
  candidates: [],
  failover_blocked: blocked,
});

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json", "x-request-id": "req_test" },
  });
}

interface Call {
  method: string;
  path: string;
  body: Record<string, unknown> | null;
}

function stubApi(scenarioOnStart = "success") {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input).split("?")[0];
      const method = init?.method ?? "GET";
      const body = init?.body ? JSON.parse(String(init.body)) : null;
      calls.push({ method, path, body });
      if (path.endsWith("/v1/workspace")) return json(workspace);
      if (path.endsWith("/v1/sandbox/scenarios")) return json(catalog);
      if (path.endsWith("/v1/payment-methods"))
        return json({
          items: [successMethod, routingMethod],
          next_cursor: null,
        });
      if (path.endsWith("/v1/routing/evaluate"))
        return json(decision(Boolean(body?.payment_id)));
      if (path.endsWith("/v1/sandbox/simulations") && method === "POST")
        return json(
          run(String(body?.scenario ?? scenarioOnStart), "running"),
          202,
        );
      if (path.includes("/v1/sandbox/simulations/"))
        return json(run(scenarioOnStart, "completed"));
      return json({ items: [], next_cursor: null });
    }),
  );
  return calls;
}

function wrap(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

const writes = (calls: Call[]) => calls.filter((call) => call.method !== "GET");

afterEach(() => vi.unstubAllGlobals());

describe("Payments default demo", () => {
  it("shows the demo card, starts the success scenario on click only, and points the empty state at it", async () => {
    const calls = stubApi("success");
    const user = userEvent.setup();
    render(wrap(<PaymentExplorer />));
    const card = await screen.findByTestId("default-demo-payments");
    expect(within(card).getByText("Simulated / sandbox")).toBeInTheDocument();
    expect(
      await screen.findByText(/Run the default demo above/),
    ).toBeInTheDocument();
    expect(writes(calls)).toEqual([]);
    const button = within(card).getByRole("button", {
      name: "Run the default demo",
    });
    await waitFor(() => expect(button).toBeEnabled());
    await user.click(button);
    await waitFor(() =>
      expect(writes(calls)).toEqual([
        expect.objectContaining({
          path: expect.stringContaining("/v1/sandbox/simulations"),
          body: expect.objectContaining({
            scenario: "success",
            amount_minor: "10000",
            currency: "USD",
          }),
        }),
      ]),
    );
    expect(
      await screen.findByTestId("default-demo-payments-result"),
    ).toHaveTextContent("1 of 1 invariants passed");
  });
});

describe("Routing preconfiguration", () => {
  it("preselects the routing demo card and evaluates exactly once on load", async () => {
    const calls = stubApi("outage");
    const { rerender } = render(wrap(<RoutingView />));
    const select = await screen.findByRole("combobox", {
      name: "Payment method",
    });
    await waitFor(() => expect(select).toHaveValue(routingMethod.id));
    expect(screen.getByLabelText("Amount (minor units)")).toHaveValue("10000");
    expect(screen.getByLabelText("Currency")).toHaveValue("USD");
    expect(
      await screen.findByRole("list", { name: "Evaluation path" }),
    ).toBeInTheDocument();
    expect(screen.getByText(/Route to/)).toBeInTheDocument();
    rerender(wrap(<RoutingView />));
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(writes(calls)).toEqual([
      expect.objectContaining({
        path: expect.stringContaining("/v1/routing/evaluate"),
        body: {
          payment_method_id: routingMethod.id,
          amount_minor: "10000",
          currency: "USD",
        },
      }),
    ]);
  });

  it("re-evaluates with the demo payment once the outage demo settles", async () => {
    const calls = stubApi("outage");
    const user = userEvent.setup();
    render(wrap(<RoutingView />));
    await screen.findByRole("list", { name: "Evaluation path" });
    const card = screen.getByTestId("default-demo-routing");
    expect(card).toHaveTextContent("Default demo: unknown outcome");
    const button = within(card).getByRole("button", {
      name: "Run the default demo",
    });
    await waitFor(() => expect(button).toBeEnabled());
    await user.click(button);
    expect(
      await screen.findByRole("heading", { name: /Unsafe failover blocked/ }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText(/Existing payment ID/)).toHaveValue("pay1");
    const evaluations = calls.filter((call) =>
      call.path.endsWith("/v1/routing/evaluate"),
    );
    expect(evaluations).toHaveLength(2);
    expect(evaluations[1].body).toMatchObject({
      payment_id: "pay1",
      amount_minor: "10000",
    });
    expect(
      calls.filter((call) => call.path.endsWith("/v1/sandbox/simulations")),
    ).toHaveLength(1);
  });
});

describe("Providers default demo", () => {
  it("shows the demo card and writes nothing on load", async () => {
    const calls = stubApi("success");
    render(wrap(<ProvidersView />));
    const card = await screen.findByTestId("default-demo-providers");
    expect(card).toHaveTextContent("No observations");
    expect(card).toHaveTextContent("provider health observations");
    expect(writes(calls)).toEqual([]);
  });
});

describe("Simulator default demo", () => {
  it("opens with the headline scenario selected and the button runs exactly it", async () => {
    const calls = stubApi("timeout_after_success");
    const user = userEvent.setup();
    render(wrap(<SimulatorView />));
    const select = await screen.findByRole("combobox", { name: "Scenario" });
    await waitFor(() => expect(select).toHaveValue("timeout_after_success"));
    expect(writes(calls)).toEqual([]);
    await user.click(screen.getByRole("button", { name: "Run default demo" }));
    await waitFor(() =>
      expect(writes(calls)).toEqual([
        expect.objectContaining({
          path: expect.stringContaining("/v1/sandbox/simulations"),
          body: expect.objectContaining({
            scenario: "timeout_after_success",
            amount_minor: "10000",
            currency: "USD",
          }),
        }),
      ]),
    );
  });
});
