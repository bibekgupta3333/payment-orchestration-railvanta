import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { PaymentVolumeChart } from "@/components/charts/PaymentVolumeChart";
import { CreatePaymentDialog } from "@/components/payments/CreatePaymentDialog";
import { PaymentDetail } from "@/components/payments/PaymentDetail";
import { PaymentTable } from "@/components/payments/PaymentTable";
import { shouldRetryRead } from "@/lib/api/client";
import {
  clearPendingSubmission,
  idempotencyKeyFor,
  pendingStorageKey,
  readPendingSubmission,
} from "@/lib/hooks/useCreatePayment";

vi.mock("next/navigation", () => ({
  usePathname: () => "/dashboard/payments",
  useRouter: () => ({ push: vi.fn() }),
  useSearchParams: () => new URLSearchParams(),
}));

const workspace = {
  id: "00000000-0000-7000-8000-000000000001",
  name: "Demo workspace",
  environment: "sandbox",
};
const method = {
  id: "00000000-0000-7000-8000-000000000101",
  rail: "mock",
  brand: "Mock",
  last_four: "4242",
};
const paymentId = "01a19403-c2ba-74b8-9581-8451e1283ae6";
function payment(status: string, amount = "25000") {
  return {
    id: paymentId,
    amount_minor: amount,
    currency: "USD",
    payment_method_id: method.id,
    merchant_reference: "order-1",
    status,
    created_at: "2026-10-31T10:00:00.000Z",
    updated_at: "2026-10-31T10:00:01.000Z",
  };
}

function json(
  body: unknown,
  status = 200,
  headers: Record<string, string> = {},
) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type": "application/json",
      "X-Request-ID": "req_test",
      ...headers,
    },
  });
}

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: shouldRetryRead, retryDelay: 0 },
      mutations: { retry: false },
    },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function routeFetch(
  routes: Record<string, () => Response | Promise<Response>>,
) {
  const fetcher = vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input).split("?")[0];
    const handler = routes[path];
    if (!handler)
      return json(
        {
          code: "not_implemented",
          message: "Unavailable",
          request_id: "req_501",
          field_errors: [],
        },
        501,
      );
    return handler();
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

describe("payment detail", () => {
  it("shows an unknown outcome with a reconciliation explanation and no retry action", async () => {
    routeFetch({
      "/v1/workspace": () => json(workspace),
      [`/v1/payments/${paymentId}`]: () =>
        json(payment("unknown"), 200, { "X-Correlation-ID": "corr_1" }),
      "/v1/payment-methods": () => json({ items: [method], next_cursor: null }),
      [`/v1/payments/${paymentId}/history`]: () =>
        json({
          payment_id: paymentId,
          idempotency_key_masked: "****abcd",
          transitions: [
            {
              from_status: null,
              to_status: "created",
              reason: "payment accepted",
              actor_reference: "api_key:operator",
              created_at: "2026-10-31T10:00:00.000Z",
            },
            {
              from_status: "created",
              to_status: "processing",
              reason: "dispatched",
              actor_reference: "system",
              created_at: "2026-10-31T10:00:00.500Z",
            },
            {
              from_status: "processing",
              to_status: "unknown",
              reason: "provider timeout",
              actor_reference: "system",
              created_at: "2026-10-31T10:00:01.000Z",
            },
          ],
          events: [],
        }),
    });
    render(<PaymentDetail id={paymentId} />, { wrapper });
    expect(
      await screen.findByRole("heading", {
        name: /Outcome unknown — checking status/,
      }),
    ).toBeVisible();
    expect(screen.getByText(/resolved by reconciliation/)).toBeVisible();
    expect(
      screen.getByText(/will not retry it or send it to another provider/),
    ).toBeVisible();
    expect(await screen.findByText("****abcd")).toBeVisible();
    expect(screen.getByText("corr_1")).toBeVisible();
    expect(screen.getByText(/Processing → Outcome unknown/)).toBeVisible();
    expect(
      screen.queryByRole("button", { name: /retry|resend|provider|failover/i }),
    ).toBeNull();
    expect(
      screen.getByRole("button", { name: "Refresh status" }),
    ).toBeVisible();
  });

  it("reports the journal as not yet available instead of inventing entries", async () => {
    routeFetch({
      "/v1/workspace": () => json(workspace),
      [`/v1/payments/${paymentId}`]: () => json(payment("created")),
      "/v1/payment-methods": () => json({ items: [method], next_cursor: null }),
      [`/v1/payments/${paymentId}/history`]: () =>
        json({
          payment_id: paymentId,
          idempotency_key_masked: null,
          transitions: [],
          events: [],
        }),
    });
    const user = userEvent.setup();
    render(<PaymentDetail id={paymentId} />, { wrapper });
    await user.click(await screen.findByRole("tab", { name: "Journal" }));
    expect(
      await screen.findByRole("heading", {
        name: "Journal appears once posting is available",
      }),
    ).toBeVisible();
    expect(screen.getByText(/Pending — no final outcome yet/)).toBeVisible();
    expect(screen.queryByText("Succeeded")).toBeNull();
  });

  it("shows a read error with the request ID and a Retry action", async () => {
    routeFetch({
      "/v1/workspace": () =>
        json(
          {
            code: "unavailable",
            message: "Workspace unavailable",
            request_id: "req_503",
            field_errors: [],
          },
          503,
        ),
    });
    render(<PaymentDetail id={paymentId} />, { wrapper });
    const alert = await screen.findByRole("alert", {}, { timeout: 3000 });
    expect(within(alert).getByText("req_503")).toBeVisible();
    expect(within(alert).getByRole("button", { name: "Retry" })).toBeVisible();
  });
});

describe("idempotency keys", () => {
  const intent = {
    amount_minor: "25000",
    currency: "USD",
    payment_method_id: method.id,
    merchant_reference: "order-1",
  };
  it("keeps one key per unconfirmed intent and replaces it when the intent is edited", () => {
    sessionStorage.clear();
    const first = idempotencyKeyFor(intent);
    expect(idempotencyKeyFor({ ...intent }).idempotencyKey).toBe(
      first.idempotencyKey,
    );
    expect(readPendingSubmission()?.idempotencyKey).toBe(first.idempotencyKey);
    const edited = idempotencyKeyFor({ ...intent, amount_minor: "25001" });
    expect(edited.idempotencyKey).not.toBe(first.idempotencyKey);
    // The original key is replaced, so returning to the old values is a new intent.
    expect(idempotencyKeyFor(intent).idempotencyKey).not.toBe(
      first.idempotencyKey,
    );
    clearPendingSubmission();
    expect(sessionStorage.getItem(pendingStorageKey)).toBeNull();
  });
});

describe("create payment dialog", () => {
  function renderDialog() {
    return render(
      <CreatePaymentDialog open onClose={() => undefined} methods={[method]} />,
      { wrapper },
    );
  }

  it("shows inline field errors and does not submit invalid amounts", async () => {
    sessionStorage.clear();
    const fetcher = routeFetch({});
    const user = userEvent.setup();
    renderDialog();
    await user.type(screen.getByLabelText("Amount"), "1.001");
    await user.click(
      screen.getByRole("button", { name: "Create sandbox payment" }),
    );
    expect(
      await screen.findByText("USD allows at most 2 decimal places"),
    ).toBeVisible();
    expect(screen.getByLabelText("Amount")).toHaveAttribute(
      "aria-invalid",
      "true",
    );
    expect(fetcher).not.toHaveBeenCalled();
    expect(screen.queryByLabelText(/card number/i)).toBeNull();
  });

  it("resends a lost submission with the same key and only reports API-confirmed acceptance", async () => {
    sessionStorage.clear();
    const keys: string[] = [];
    const bodies: string[] = [];
    let calls = 0;
    let loseFirstResponse: () => void = () => undefined;
    const fetcher = vi.fn(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        keys.push(new Headers(init?.headers).get("Idempotency-Key") ?? "");
        bodies.push(String(init?.body));
        calls += 1;
        if (calls === 1) {
          // Hold the first request open so the second click lands while it
          // is in flight, then lose its response.
          await new Promise<void>((resolve) => {
            loseFirstResponse = resolve;
          });
          throw new TypeError("Failed to fetch");
        }
        return json(payment("created", "9007199254740993"), 202, {
          "Idempotent-Replayed": "true",
        });
      },
    );
    vi.stubGlobal("fetch", fetcher);
    const user = userEvent.setup();
    renderDialog();
    await user.type(screen.getByLabelText("Amount"), "90071992547409.93");
    await user.dblClick(
      screen.getByRole("button", { name: "Create sandbox payment" }),
    );
    expect(
      await screen.findByText(/Submitting…/, { selector: "p" }),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "Submitting…" })).toBeDisabled();
    expect(fetcher).toHaveBeenCalledTimes(1);
    loseFirstResponse();
    expect(
      await screen.findByRole("heading", {
        name: "Outcome unknown — checking status",
      }),
    ).toBeVisible();
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(screen.queryByText(/accepted/i)).toBeNull();
    await user.click(
      screen.getByRole("button", { name: "Resend unchanged request" }),
    );
    expect(
      await screen.findByRole("heading", {
        name: "Payment accepted — outcome pending",
      }),
    ).toBeVisible();
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(keys[1]).toBe(keys[0]);
    expect(JSON.parse(bodies[0]).amount_minor).toBe("9007199254740993");
    expect(screen.getByText("USD 90,071,992,547,409.93")).toBeVisible();
    expect(screen.getByText(/replayed the original response/)).toBeVisible();
    expect(screen.queryByText("Succeeded")).toBeNull();
    expect(readPendingSubmission()).toBeNull();
  });
});

describe("payment table and volume chart", () => {
  it("pairs statuses with text and formats exact amounts", () => {
    render(
      <PaymentTable
        payments={[payment("unknown", "9223372036854775807") as never]}
        methods={[method]}
      />,
    );
    const region = screen.getByRole("region", { name: "Payments" });
    expect(region).toHaveAttribute("tabindex", "0");
    expect(within(region).getByText("Outcome unknown")).toBeVisible();
    expect(
      within(region).getByText("USD 92,233,720,368,547,758.07"),
    ).toBeVisible();
  });

  it("summarises volumes per currency with a data source and an exact table", () => {
    render(
      <PaymentVolumeChart
        volume={{
          data_source: "observed",
          window: "24h",
          bucket: "hour",
          window_start: "2026-10-30T10:00:00.000Z",
          window_end: "2026-10-31T10:00:00.000Z",
          generated_at: "2026-10-31T10:00:00.000Z",
          next_cursor: null,
          items: [
            {
              timestamp: 1793440800000,
              currency: "USD",
              amount_minor: "18446744073709551614",
              payment_count: 2,
            },
            {
              timestamp: 1793440800000,
              currency: "JPY",
              amount_minor: "500",
              payment_count: 1,
            },
          ],
        }}
      />,
    );
    expect(
      screen.getAllByText(/Observed sandbox API data/).length,
    ).toBeGreaterThan(0);
    expect(
      screen.getByText(
        /USD 184,467,440,737,095,516.14 accepted; JPY 500 accepted/,
      ),
    ).toBeVisible();
    const table = screen.getByRole("region", {
      name: "Payment volume data",
      hidden: true,
    });
    expect(
      within(table).getByText("USD 184,467,440,737,095,516.14"),
    ).toBeInTheDocument();
  });
});

describe("retry policy", () => {
  it("never retries reads on client errors and bounds other read retries", async () => {
    const { ApiError } = await import("@/lib/api/client");
    const error = (status: number) =>
      new ApiError(status, {
        code: "x",
        message: "x",
        request_id: "r",
        field_errors: [],
      });
    for (const status of [401, 403, 404, 422])
      expect(shouldRetryRead(0, error(status))).toBe(false);
    expect(shouldRetryRead(0, error(503))).toBe(true);
    expect(shouldRetryRead(2, error(503))).toBe(false);
  });
});
