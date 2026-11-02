import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { StoryPanel } from "@/components/dashboard/Hub";
import { WhyRailvanta } from "@/components/landing/WhyRailvanta";
import type { SandboxSample } from "@/lib/api/sample-schemas";
import { sampleScenarios } from "@/lib/hooks/sample-data";
import {
  samplesPollMs,
  samplesRefetchInterval,
} from "@/lib/hooks/sandbox-samples";
import { whyProblems } from "@/lib/why-story";

const workspace = {
  id: "00000000-0000-7000-8000-000000000001",
  name: "Demo workspace",
  environment: "sandbox",
};

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json", "x-request-id": "req_t" },
  });
}

function sample(
  scenario: string,
  status: SandboxSample["status"],
  overrides: Partial<SandboxSample> = {},
): SandboxSample {
  const present = status !== "missing";
  return {
    scenario,
    run_id: present ? `sim_${scenario}` : null,
    status,
    payment_id: present ? `pay_${scenario}` : null,
    invariants_passed: status === "completed" ? 4 : 0,
    invariants_total: status === "completed" ? 4 : 0,
    ...overrides,
  };
}

function mockSamples(responses: SandboxSample[][]) {
  let call = 0;
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input).split("?")[0];
    if (path === "/v1/workspace") return json(workspace);
    if (path === "/v1/sandbox/samples") {
      const items = responses[Math.min(call, responses.length - 1)];
      call += 1;
      return json({ items });
    }
    return json(
      { code: "not_found", message: "x", request_id: "r", field_errors: [] },
      404,
    );
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function renderWithClient(node: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
  return client;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("why Railvanta exists (landing)", () => {
  it("renders every problem with its guarantee and a sandbox link", () => {
    render(<WhyRailvanta />);
    const section = screen.getByRole("region", {
      name: "Payments fail in the gaps.",
    });
    expect(section).toHaveTextContent("Why Railvanta exists");
    const cards = within(section).getAllByRole("listitem");
    expect(cards).toHaveLength(6);
    expect(whyProblems).toHaveLength(6);
    for (const problem of whyProblems) {
      expect(
        within(section).getByRole("heading", { level: 3, name: problem.title }),
      ).toBeInTheDocument();
      const link = within(section).getByRole("link", {
        name: new RegExp(`See it in the sandbox: ${problem.title}`),
      });
      expect(link).toHaveAttribute("href", problem.proof.href);
    }
  });

  it("maps every problem to a real sample scenario and never claims failover", () => {
    for (const problem of whyProblems) {
      for (const scenario of problem.scenarios)
        expect(sampleScenarios).toContain(scenario);
    }
    const outage = whyProblems.find((problem) => problem.id === "outage");
    expect(outage?.guarantee).toContain("no other provider is tried");
  });
});

describe("hub story panel", () => {
  it("shows statuses, invariant counts and links from the sample runs", async () => {
    mockSamples([
      [
        sample("success", "completed"),
        sample("decline", "completed"),
        sample("duplicate_request", "completed"),
        sample("timeout_after_success", "completed", {
          invariants_passed: 3,
          invariants_total: 5,
        }),
        sample("outage", "failed", { payment_id: null }),
        sample("duplicate_posting", "completed"),
        sample("duplicate_webhook", "missing"),
        sample("settlement_mismatch", "completed"),
      ],
    ]);
    renderWithClient(<StoryPanel />);
    const panel = screen.getByRole("region", {
      name: "The story: what this sandbox proves",
    });
    const dup = within(panel).getByTestId("hub-story-double-charge");
    await waitFor(() => expect(dup).toHaveTextContent("Completed"));
    expect(dup).toHaveTextContent("4/4 invariants pass");
    expect(
      within(dup).getByRole("link", { name: /Inspect run/ }),
    ).toHaveAttribute("href", "/dashboard/simulator?run=sim_duplicate_request");
    expect(within(dup).getByRole("link", { name: /Payment/ })).toHaveAttribute(
      "href",
      "/dashboard/payments/pay_duplicate_request",
    );

    expect(screen.getByTestId("hub-story-lost-response")).toHaveTextContent(
      "3/5 invariants pass",
    );

    const outage = screen.getByTestId("hub-story-outage");
    expect(outage).toHaveTextContent("Failed");
    expect(within(outage).getByRole("link", { name: /Inspect run/ }));
    expect(within(outage).queryByRole("link", { name: /^Payment/ })).toBeNull();

    const webhook = screen.getByTestId("hub-story-duplicate-webhook");
    expect(webhook).toHaveTextContent("Not loaded");
    expect(within(webhook).queryByRole("link", { name: /Inspect run/ })).toBe(
      null,
    );
    expect(within(webhook).getByRole("link")).toHaveAttribute(
      "href",
      "/dashboard/events",
    );
    expect(screen.queryByTestId("hub-story-empty")).toBeNull();
  });

  it("points at Load sample data when no samples exist on the server", async () => {
    mockSamples([sampleScenarios.map((id) => sample(id, "missing"))]);
    renderWithClient(<StoryPanel />);
    const hint = await screen.findByTestId("hub-story-empty");
    expect(
      within(hint).getByRole("link", { name: "Load sample data" }),
    ).toHaveAttribute("href", "#hub-sample-data");
    expect(screen.getAllByText("Not loaded")).toHaveLength(6);
  });

  it("polls while a run is running and stops once none are", async () => {
    const running = sampleScenarios.map((id) =>
      sample(id, id === "outage" ? "running" : "completed"),
    );
    const done = sampleScenarios.map((id) => sample(id, "completed"));
    mockSamples([running, done]);
    renderWithClient(<StoryPanel />);
    expect(await screen.findByTestId("hub-story-running")).toBeInTheDocument();
    expect(screen.getByTestId("hub-story-outage")).toHaveTextContent("Running");

    expect(samplesRefetchInterval(undefined)).toBe(false);
    expect(
      samplesRefetchInterval({
        data: { items: running },
        requestId: "r",
      } as never),
    ).toBe(samplesPollMs);
    expect(
      samplesRefetchInterval({
        data: { items: done },
        requestId: "r",
      } as never),
    ).toBe(false);

    await waitFor(
      () =>
        expect(screen.getByTestId("hub-story-outage")).toHaveTextContent(
          "Completed",
        ),
      { timeout: samplesPollMs + 2_000 },
    );
    expect(screen.queryByTestId("hub-story-running")).toBeNull();
  }, 10_000);

  it("is scoped under the railvanta key so Load sample data refreshes it", async () => {
    mockSamples([sampleScenarios.map((id) => sample(id, "completed"))]);
    const client = renderWithClient(<StoryPanel />);
    await screen.findAllByText("Completed");
    const keys = client
      .getQueryCache()
      .getAll()
      .map((query) => query.queryKey);
    expect(keys).toContainEqual([
      "railvanta",
      workspace.id,
      workspace.environment,
      "sandbox",
      "samples",
    ]);
  });
});
