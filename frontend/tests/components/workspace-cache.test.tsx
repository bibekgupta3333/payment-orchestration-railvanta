import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { PaymentExplorer } from "@/components/payments/PaymentExplorer";
import { shouldRetryRead } from "@/lib/api/client";
import { scopedKey, usePayments } from "@/lib/hooks/payments";

vi.mock("next/navigation", () => ({
  usePathname: () => "/dashboard/payments",
  useRouter: () => ({ push: vi.fn() }),
  useSearchParams: () => new URLSearchParams(),
}));

const workspaceA = {
  id: "00000000-0000-7000-8000-00000000000a",
  name: "Workspace A",
  environment: "sandbox",
};
const workspaceB = {
  id: "00000000-0000-7000-8000-00000000000b",
  name: "Workspace B",
  environment: "sandbox",
};

function paymentFor(id: string, amount: string) {
  return {
    id,
    amount_minor: amount,
    currency: "USD",
    payment_method_id: "00000000-0000-7000-8000-000000000101",
    merchant_reference: null,
    status: "succeeded",
    created_at: "2026-10-31T10:00:00.000Z",
    updated_at: "2026-10-31T10:00:01.000Z",
  };
}

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", "X-Request-ID": "req_ws" },
  });
}

function newClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: shouldRetryRead, retryDelay: 0 },
      mutations: { retry: false },
    },
  });
}

function withClient(client: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
  };
}

describe("workspace-keyed query cache", () => {
  it("prefixes every key with workspace and environment", () => {
    expect(scopedKey(workspaceA, "payments", "list", {})).not.toEqual(
      scopedKey(workspaceB, "payments", "list", {}),
    );
    expect(scopedKey({ ...workspaceA, environment: "live" }, "x")).not.toEqual(
      scopedKey(workspaceA, "x"),
    );
    expect(scopedKey(undefined, "x").slice(0, 3)).toEqual([
      "railvanta",
      "none",
      "none",
    ]);
  });

  it("never renders workspace A rows after the session switches to workspace B", async () => {
    let current = workspaceA;
    let holdB: () => void = () => undefined;
    const bGate = new Promise<void>((resolve) => {
      holdB = resolve;
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input).split("?")[0];
        if (path === "/v1/workspace") return json(current);
        if (path === "/v1/payments") {
          if (current.id === workspaceA.id)
            return json({
              items: [paymentFor("pay-from-workspace-a", "111")],
              next_cursor: null,
            });
          await bGate; // B's list is slow: A's data must not fill the gap
          return json({
            items: [paymentFor("pay-from-workspace-b", "222")],
            next_cursor: null,
          });
        }
        return json({ items: [], next_cursor: null });
      }),
    );
    function Probe() {
      const { query, scope } = usePayments({});
      return (
        <div>
          <p data-testid="scope">{scope?.name ?? "no scope"}</p>
          <ul>
            {query.data?.data.items.map((item) => (
              <li key={item.id}>{item.id}</li>
            ))}
          </ul>
        </div>
      );
    }
    const client = newClient();
    render(<Probe />, { wrapper: withClient(client) });
    expect(await screen.findByText("pay-from-workspace-a")).toBeVisible();
    expect(screen.getByTestId("scope")).toHaveTextContent("Workspace A");

    // The session now belongs to workspace B (for example after sign-in as a
    // different member): the workspace read is refreshed and keys change.
    current = workspaceB;
    await act(async () => {
      await client.invalidateQueries({ queryKey: ["railvanta", "workspace"] });
    });
    await waitFor(() =>
      expect(screen.getByTestId("scope")).toHaveTextContent("Workspace B"),
    );
    expect(screen.queryByText("pay-from-workspace-a")).toBeNull();
    holdB();
    expect(await screen.findByText("pay-from-workspace-b")).toBeVisible();
    expect(screen.queryByText("pay-from-workspace-a")).toBeNull();

    // Cache entries are kept apart: A's list is still stored under A's key only.
    const keysWithA = client
      .getQueryCache()
      .findAll({ queryKey: scopedKey(workspaceA, "payments") });
    const keysWithB = client
      .getQueryCache()
      .findAll({ queryKey: scopedKey(workspaceB, "payments") });
    expect(keysWithA).toHaveLength(1);
    expect(keysWithB).toHaveLength(1);
    expect(JSON.stringify(keysWithB[0].state.data)).not.toContain(
      "workspace-a",
    );
  });
});

describe("payments explorer loading, empty and error states", () => {
  it("shows a loading state first, then the empty state with no invented rows", async () => {
    let release: () => void = () => undefined;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input).split("?")[0];
        if (path === "/v1/workspace") return json(workspaceA);
        await gate;
        return json({ items: [], next_cursor: null });
      }),
    );
    render(<PaymentExplorer />, { wrapper: withClient(newClient()) });
    expect(await screen.findByText("Loading payments")).toBeInTheDocument();
    expect(screen.queryByText("No payments yet")).toBeNull();
    release();
    expect(await screen.findByText("No payments yet")).toBeVisible();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("shows an honest read error with the request ID and does not render an empty state", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input).split("?")[0];
        if (path === "/v1/workspace") return json(workspaceA);
        return json(
          {
            code: "unavailable",
            message: "Payments unavailable",
            request_id: "req_outage_1",
            field_errors: [],
          },
          503,
        );
      }),
    );
    render(<PaymentExplorer />, { wrapper: withClient(newClient()) });
    expect(await screen.findByText(/req_outage_1/)).toBeVisible();
    expect(screen.getByRole("button", { name: /retry/i })).toBeVisible();
    expect(screen.queryByText("No payments yet")).toBeNull();
  });
});
