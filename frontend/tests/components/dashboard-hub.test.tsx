import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ArchitectureStrip,
  CredentialsCard,
  DemoRunCard,
  HubHeading,
  QuickActions,
} from "@/components/dashboard/Hub";
import type { Simulation } from "@/lib/api/routing-schemas";

const workspace = {
  id: "00000000-0000-7000-8000-000000000001",
  name: "Demo workspace",
  environment: "sandbox",
};

const SECRET = "rvk_1a2b3c4d.0123456789abcdef0123456789abcdef0123456789abcdef";

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json", "x-request-id": "req_test" },
  });
}

const scenarioIds = ["timeout_after_success", "duplicate_request"];
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

const keyFixture = {
  id: "00000000-0000-7000-8000-0000000000a1",
  name: "Default demo key",
  prefix: "rvk_aaaaaaaa",
  role: "operator",
  created_at: "2026-11-01T10:00:00Z",
  expires_at: "2026-11-02T10:00:00Z",
  revoked_at: null,
  status: "active",
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

function settings(role: "read" | "operator") {
  return {
    workspace,
    role,
    providers: [],
    capabilities: {
      api_key_management: "available",
      role_management: "unavailable",
      live_activation: "unavailable",
    },
  };
}

interface Options {
  role?: "read" | "operator";
  keys?: unknown[];
  scenarios?: string[];
}

function setup(options: Options = {}) {
  const posts: { path: string; body: unknown }[] = [];
  const role = options.role ?? "operator";
  let keys = options.keys ?? [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input).split("?")[0];
      const method = (init?.method ?? "GET").toUpperCase();
      if (method === "POST")
        posts.push({ path, body: JSON.parse(String(init?.body)) });
      if (path === "/v1/workspace") return json(workspace);
      if (path === "/v1/settings") return json(settings(role));
      if (path === "/v1/payment-methods")
        return json({ items: [], next_cursor: null });
      if (path === "/v1/api-keys" && method === "GET")
        return json({ items: keys, limit: 20 });
      if (path === "/v1/api-keys" && method === "POST") {
        const created = { ...keyFixture, id: "new-key" };
        keys = [created, ...keys];
        return json({ key: created, secret: SECRET }, 201);
      }
      if (path === "/v1/sandbox/scenarios")
        return json(catalog(options.scenarios ?? scenarioIds));
      if (path === "/v1/sandbox/simulations" && method === "POST")
        return json(simulation(), 202);
      if (path.startsWith("/v1/sandbox/simulations/"))
        return json(simulation());
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
  render(
    <QueryClientProvider client={client}>
      <HubHeading />
      <QuickActions />
      <CredentialsCard />
      <DemoRunCard />
      <ArchitectureStrip />
    </QueryClientProvider>,
  );
  return posts;
}

afterEach(() => vi.unstubAllGlobals());

describe("dashboard hub", () => {
  it("renders the heading and the four quick actions with their targets", async () => {
    setup();
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent(
      "Railvanta sandbox",
    );
    expect(screen.getByRole("link", { name: /Run a demo/ })).toHaveAttribute(
      "href",
      "/dashboard/playground",
    );
    expect(
      screen.getByRole("link", { name: /API reference \(Swagger\)/ }),
    ).toHaveAttribute("href", "/dashboard/api-reference");
    expect(
      screen.getByRole("link", { name: /Generate API key/ }),
    ).toHaveAttribute("href", "#integration-credentials");
    expect(
      screen.getByRole("button", { name: /Create a \$100 payment/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Stripe and Plaid adapters: Planned"),
    ).toBeVisible();
  });

  it("shows base URL, the placeholder curl and the key count without minting", async () => {
    const posts = setup({ keys: [{ ...keyFixture, name: "Other" }] });
    const card = screen.getByRole("region", {
      name: "Integration credentials",
    });
    expect(
      await within(card).findByText("1 of 20 active keys used"),
    ).toBeVisible();
    expect(within(card).getByText(window.location.origin)).toBeVisible();
    expect(within(card).getByText(/Authorization: Bearer \$KEY/)).toBeVisible();
    expect(within(card).getByText("Authorization: Bearer <key>")).toBeVisible();
    expect(posts).toHaveLength(0);
  });

  it("generates one default key on click, shows it once and hides it", async () => {
    const posts = setup({ keys: [keyFixture] });
    const user = userEvent.setup();
    expect(
      await screen.findByText(/already have an active Default demo key/),
    ).toBeVisible();
    await user.click(
      await screen.findByRole("button", { name: "Generate default demo key" }),
    );
    const panel = await screen.findByTestId("new-api-key");
    expect(within(panel).getByText(SECRET)).toBeVisible();
    expect(
      within(panel).getByText(/Authorization: Bearer rvk_1a2b3c4d\./),
    ).toBeVisible();
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
    await user.click(screen.getByRole("button", { name: /hide the key/i }));
    expect(screen.queryByTestId("new-api-key")).toBeNull();
    expect(document.body.textContent).not.toContain(
      "0123456789abcdef0123456789abcdef",
    );
  });

  it("offers no generate button to read-only roles", async () => {
    setup({ role: "read" });
    expect(await screen.findByText(/requires the operator role/)).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Generate default demo key" }),
    ).toBeNull();
  });

  it("runs a demo and reports its invariants", async () => {
    const posts = setup();
    const user = userEvent.setup();
    const button = screen.getByRole("button", { name: "Lost response" });
    await waitFor(() => expect(button).toBeEnabled());
    await user.click(button);
    expect(await screen.findByText("1 of 1 invariants passed")).toBeVisible();
    expect(posts).toHaveLength(1);
    expect(posts[0].body).toMatchObject({
      scenario: "timeout_after_success",
      amount_minor: "10000",
      currency: "USD",
    });
    expect(
      screen.getByRole("link", { name: "Open in Playground" }),
    ).toBeVisible();
  });

  it("disables a demo whose scenario is missing, with the reason", async () => {
    setup({ scenarios: ["timeout_after_success"] });
    const button = screen.getByRole("button", { name: "Double-entry ledger" });
    await waitFor(() =>
      expect(
        screen.getAllByText(/backend scenario not available/)[0],
      ).toBeVisible(),
    );
    expect(button).toBeDisabled();
    expect(button).toHaveAccessibleDescription(/not available/);
  });
});
