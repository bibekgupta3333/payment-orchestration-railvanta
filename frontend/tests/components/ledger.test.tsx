import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { JournalBody } from "@/components/ledger/JournalDetail";
import { LedgerExplorer } from "@/components/ledger/LedgerExplorer";
import { ReconciliationView } from "@/components/reconciliation/ReconciliationView";
import type { LedgerJournalDetail } from "@/lib/api/ledger-schemas";
import { shouldRetryRead } from "@/lib/api/client";
import { formatSignedMoney, totalsFromEntries } from "@/lib/ledger-format";

let search = "";
vi.mock("next/navigation", () => ({
  usePathname: () => "/dashboard/ledger",
  useRouter: () => ({ push: vi.fn() }),
  useSearchParams: () => new URLSearchParams(search),
}));

const workspace = {
  id: "00000000-0000-7000-8000-000000000001",
  name: "Demo workspace",
  environment: "sandbox",
};

function journal(
  overrides: Partial<LedgerJournalDetail> = {},
  amount = "25000",
): LedgerJournalDetail {
  return {
    id: "01a19403-0000-7000-8000-00000000aaaa",
    payment_id: "01a19403-0000-7000-8000-00000000bbbb",
    currency: "USD",
    status: "posted",
    posting_key: "payment:x:success",
    reversal_of: null,
    reversed_by: null,
    created_at: "2026-10-31T10:00:00.000Z",
    posted_at: "2026-10-31T10:00:00.100Z",
    entries: [
      {
        id: "e1",
        account_id: "a1",
        account_code: "provider_clearing:prov",
        provider_id: "prov",
        side: "debit",
        amount_minor: amount,
      },
      {
        id: "e2",
        account_id: "a2",
        account_code: "merchant_payable",
        provider_id: null,
        side: "credit",
        amount_minor: amount,
      },
    ],
    debit_total_minor: amount,
    credit_total_minor: amount,
    payment: {
      id: "01a19403-0000-7000-8000-00000000bbbb",
      status: "succeeded",
      amount_minor: amount,
      currency: "USD",
    },
    ...overrides,
  };
}

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", "X-Request-ID": "req_t" },
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

function routeFetch(routes: Record<string, () => Response>) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const handler = routes[String(input).split("?")[0]];
      return handler
        ? handler()
        : json(
            {
              code: "not_found",
              message: "Missing",
              request_id: "req_404",
              field_errors: [],
            },
            404,
          );
    }),
  );
}

describe("journal detail", () => {
  it("shows the immutable badge, entry lines and a computed balanced check", () => {
    render(<JournalBody journal={journal()} />);
    expect(screen.getByText("Posted / immutable")).toBeVisible();
    const table = screen.getByRole("region", { name: "Journal entries" });
    expect(within(table).getByText("merchant_payable")).toBeVisible();
    expect(within(table).getByText("debit")).toBeVisible();
    expect(within(table).getAllByText("USD 250.00")).toHaveLength(2);
    expect(screen.getByRole("status")).toHaveTextContent(
      "Debits equal credits",
    );
    expect(screen.getByRole("link", { name: /Payment/ })).toHaveAttribute(
      "href",
      "/dashboard/payments/01a19403-0000-7000-8000-00000000bbbb",
    );
    // There is no edit or delete control anywhere on a posted journal.
    expect(
      screen.queryByRole("button", { name: /edit|delete|remove|repost/i }),
    ).toBeNull();
  });

  it("computes the check from the lines, not the server totals", () => {
    const broken = journal();
    broken.entries[1] = { ...broken.entries[1], amount_minor: "24999" };
    render(<JournalBody journal={broken} />);
    expect(screen.getByRole("status")).toHaveTextContent(
      "Debits do not equal credits",
    );
  });

  it("links reversals in both directions", () => {
    const { unmount } = render(
      <JournalBody
        journal={journal({
          reversed_by: "01a19403-0000-7000-8000-00000000cccc",
        })}
      />,
    );
    expect(
      screen.getByRole("link", { name: /Reversed by journal/ }),
    ).toHaveAttribute(
      "href",
      "/dashboard/ledger/01a19403-0000-7000-8000-00000000cccc",
    );
    unmount();
    render(
      <JournalBody
        journal={journal({
          reversal_of: "01a19403-0000-7000-8000-00000000dddd",
        })}
      />,
    );
    expect(
      screen.getByRole("link", { name: /Reverses journal/ }),
    ).toHaveAttribute(
      "href",
      "/dashboard/ledger/01a19403-0000-7000-8000-00000000dddd",
    );
  });

  it("renders amounts beyond Number.MAX_SAFE_INTEGER exactly", () => {
    const max = "9223372036854775807";
    render(
      <JournalBody
        journal={journal(
          {
            currency: "KWD",
            payment: null,
          },
          max,
        )}
      />,
    );
    // KWD has three decimals: 9,223,372,036,854,775.807
    const exact = "KWD 9,223,372,036,854,775.807";
    expect(screen.getAllByText(exact)).toHaveLength(2);
    expect(screen.getByRole("status")).toHaveTextContent(`Debits ${exact}`);
    expect(screen.getByRole("status")).toHaveTextContent(`Credits ${exact}`);
  });
});

describe("ledger helpers", () => {
  it("sums lines exactly and requires two lines to be balanced", () => {
    const totals = totalsFromEntries([
      { side: "debit", amount_minor: "9007199254740993" },
      { side: "credit", amount_minor: "9007199254740993" },
    ]);
    expect(totals.debit).toBe(9007199254740993n);
    expect(totals.balanced).toBe(true);
    expect(
      totalsFromEntries([{ side: "debit", amount_minor: "5" }]).balanced,
    ).toBe(false);
  });
  it("formats negative balances without losing precision", () => {
    expect(formatSignedMoney("-9007199254740993", "USD")).toBe(
      "-USD 90,071,992,547,409.93",
    );
    expect(formatSignedMoney("0", "JPY")).toBe("JPY 0");
  });
});

describe("ledger explorer", () => {
  it("groups accounts per currency and reports overflow instead of a number", async () => {
    search = "";
    const account = (over: object) => ({
      id: "a",
      code: "merchant_payable",
      kind: "liability",
      currency: "USD",
      provider_id: null,
      provider_code: null,
      debit_total_minor: "0",
      credit_total_minor: "500",
      balance_minor: "500",
      balance_error: null,
      normal_balance: "credit",
      entry_count: 1,
      ...over,
    });
    routeFetch({
      "/v1/workspace": () => json(workspace),
      "/v1/ledger/accounts": () =>
        json({
          items: [
            account({ id: "1" }),
            account({
              id: "2",
              currency: "KWD",
              debit_total_minor: null,
              credit_total_minor: null,
              balance_minor: null,
              balance_error: "amount_overflow",
            }),
          ],
        }),
    });
    render(<LedgerExplorer />, { wrapper });
    expect(
      await screen.findByRole("region", { name: "USD ledger accounts" }),
    ).toBeVisible();
    expect(
      screen.getByRole("region", { name: "KWD ledger accounts" }),
    ).toBeVisible();
    expect(screen.getAllByText("USD 5.00").length).toBeGreaterThan(0);
    expect(screen.getByRole("alert")).toHaveTextContent(/amount overflow/);
    // No cross-currency total is ever rendered.
    expect(screen.queryByText(/total balance/i)).toBeNull();
  });

  it("shows an empty state and keeps the filters in the form", async () => {
    search = "tab=journal&currency=EUR&payment_id=abc";
    routeFetch({
      "/v1/workspace": () => json(workspace),
      "/v1/ledger/transactions": () => json({ items: [], next_cursor: null }),
    });
    render(<LedgerExplorer />, { wrapper });
    expect(
      await screen.findByText("No journals match these filters"),
    ).toBeVisible();
    expect(screen.getByLabelText("Payment ID")).toHaveValue("abc");
    expect(screen.getByLabelText("Currency")).toHaveValue("EUR");
  });

  it("shows a read error with a request ID and retry", async () => {
    search = "";
    routeFetch({
      "/v1/workspace": () => json(workspace),
      "/v1/ledger/accounts": () =>
        json(
          {
            code: "unavailable",
            message: "Ledger unavailable",
            request_id: "req_503",
            field_errors: [],
          },
          503,
        ),
    });
    render(<LedgerExplorer />, { wrapper });
    expect(await screen.findByRole("alert")).toHaveTextContent("req_503");
    expect(screen.getByRole("button", { name: "Retry" })).toBeVisible();
  });
});

describe("reconciliation run control", () => {
  const run = {
    id: "run-1",
    run_key: "k",
    status: "completed",
    window_start: "2026-10-30T10:00:00.000Z",
    window_end: "2026-10-31T10:00:00.000Z",
    created_at: "2026-10-31T10:00:00.000Z",
    completed_at: "2026-10-31T10:00:00.100Z",
    requested_by: "api_key:operator",
    counts: { compared: 3, matched: 2, unmatched: 1, mismatch: 0 },
    evidence_consumed_count: 0,
    simulated: true,
  };
  const routes = (role: string) => ({
    "/v1/workspace": () => json(workspace),
    "/v1/session": () => json({ workspace_id: workspace.id, role }),
    "/v1/reconciliation/runs": () => json({ items: [run], next_cursor: null }),
    "/v1/reconciliation/discrepancies": () =>
      json({ items: [], next_cursor: null }),
  });

  it("is disabled with a reason for read-only credentials", async () => {
    search = "";
    routeFetch(routes("read"));
    render(<ReconciliationView />, { wrapper });
    const button = screen.getByRole("button", {
      name: "Run sandbox reconciliation",
    });
    expect(
      await screen.findByText(/Operator permission is required/),
    ).toBeVisible();
    expect(button).toBeDisabled();
    expect(await screen.findByTestId("count-Compared")).toHaveTextContent("3");
    expect(screen.getByTestId("count-Unmatched")).toHaveTextContent("1");
  });

  it("asks for confirmation before running for an operator", async () => {
    search = "";
    routeFetch(routes("operator"));
    render(<ReconciliationView />, { wrapper });
    const button = screen.getByRole("button", {
      name: "Run sandbox reconciliation",
    });
    await vi.waitFor(() => expect(button).toBeEnabled());
    await userEvent.click(button);
    expect(await screen.findByText(/creates no journals/)).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Run reconciliation" }),
    ).toBeVisible();
  });
});
