import { act, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { EventDetailView, traceUrl } from "@/components/events/EventDetail";
import {
  SettingsDetails,
  unavailableCapabilities,
} from "@/components/settings/SettingsView";
import { ThemeProvider, ThemeSelect } from "@/components/theme";
import { CopyCode } from "@/components/ui/copy-code";
import DocsPage from "@/app/docs/page";
import type { EventDetail, Settings } from "@/lib/api/event-schemas";
import {
  curlSnippet,
  docsSource,
  goSnippet,
  typescriptSnippet,
} from "@/lib/docs/snippets";

function renderTheme() {
  return render(
    <ThemeProvider>
      <ThemeSelect />
    </ThemeProvider>,
  );
}

describe("theme toggle", () => {
  it("offers System, Light and Dark and persists the choice locally", async () => {
    const user = userEvent.setup();
    const first = renderTheme();
    const select = screen.getByRole("combobox", { name: "Theme" });
    expect(
      within(select)
        .getAllByRole("option")
        .map((option) => option.textContent),
    ).toEqual(["System", "Light", "Dark"]);
    expect(select).toHaveValue("system");

    await user.selectOptions(select, "dark");
    expect(document.documentElement).toHaveClass("dark");
    expect(localStorage.getItem("railvanta-theme")).toBe("dark");

    await user.selectOptions(select, "light");
    expect(document.documentElement).not.toHaveClass("dark");
    expect(localStorage.getItem("railvanta-theme")).toBe("light");

    await user.selectOptions(select, "dark");
    first.unmount();
    document.documentElement.classList.remove("dark");
    renderTheme();
    expect(screen.getByRole("combobox", { name: "Theme" })).toHaveValue("dark");
    expect(document.documentElement).toHaveClass("dark");
  });

  it("keeps working when browser storage is unavailable", async () => {
    const user = userEvent.setup();
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    renderTheme();
    await user.selectOptions(
      screen.getByRole("combobox", { name: "Theme" }),
      "dark",
    );
    expect(document.documentElement).toHaveClass("dark");
  });
});

const settings: Settings = {
  workspace: { id: "w1", name: "Demo workspace", environment: "sandbox" },
  role: "operator",
  providers: [
    { code: "mock", label: "Mock provider", status: "enabled" },
    { code: "stripe", label: "Stripe", status: "not_configured" },
    { code: "plaid", label: "Plaid", status: "not_configured" },
  ],
  capabilities: {
    api_key_management: "unavailable",
    role_management: "unavailable",
    live_activation: "unavailable",
  },
};

describe("settings", () => {
  it("shows a fixed sandbox environment and read-only provider status", () => {
    render(<SettingsDetails settings={settings} />);
    expect(screen.getByText("Demo workspace")).toBeVisible();
    expect(screen.getByText("Sandbox")).toBeVisible();
    const table = screen.getByRole("region", {
      name: "Provider configuration",
    });
    expect(within(table).getByText("Enabled")).toBeVisible();
    expect(within(table).getAllByText("Not configured")).toHaveLength(2);
  });

  it("shows key, role and live-activation as unavailable with no controls", () => {
    render(<SettingsDetails settings={settings} />);
    for (const item of unavailableCapabilities)
      expect(screen.getByText(item.label)).toBeVisible();
    expect(screen.getAllByText("Unavailable")).toHaveLength(3);
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.queryByRole("switch")).toBeNull();
    expect(screen.queryByRole("checkbox")).toBeNull();
  });
});

describe("copy buttons", () => {
  it("announces success through a polite live region", async () => {
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
    render(<CopyCode label="cURL: example" code="curl https://example.test" />);
    const region = document.querySelector("[aria-live='polite']");
    expect(region).toHaveTextContent("");
    await user.click(screen.getByRole("button", { name: "Copy example" }));
    expect(writeText).toHaveBeenCalledWith("curl https://example.test");
    expect(region).toHaveTextContent("Example copied to clipboard");
  });

  it("announces failure without claiming success", async () => {
    const user = userEvent.setup();
    vi.stubGlobal("navigator", {
      ...navigator,
      clipboard: { writeText: vi.fn().mockRejectedValue(new Error("denied")) },
    });
    render(<CopyCode label="cURL: example" code="curl x" />);
    await user.click(screen.getByRole("button", { name: "Copy example" }));
    const region = document.querySelector("[aria-live='polite']");
    expect(region).toHaveTextContent("Copy failed; select the text instead");
    expect(screen.queryByText("Copied")).toBeNull();
  });
});

describe("docs", () => {
  it("renders every example with machine-readable data and placeholder credentials", () => {
    const { container } = render(<DocsPage />);
    const blocks = container.querySelectorAll("[data-docs-example]");
    expect(blocks).toHaveLength(docsSource.examples.length);
    for (const example of docsSource.examples) {
      const block = container.querySelector(
        `[data-example-id="${example.id}"]`,
      );
      expect(block).toHaveAttribute("data-method", example.method);
      expect(block).toHaveAttribute("data-path", example.path);
      expect(block).toHaveAttribute(
        "data-expected-status",
        String(example.expect.status),
      );
    }
    expect(container.textContent).toContain("RAILVANTA_SANDBOX_KEY");
    expect(container.textContent).not.toMatch(
      /Bearer [A-Za-z0-9]{4,}\.[A-Za-z0-9]{8,}/,
    );
    for (const heading of [
      "Getting started",
      "Authentication",
      "Payments",
      "Idempotency and retry safety",
      "Events and webhooks",
      "Routing",
      "Errors",
      "Supported and planned",
    ])
      expect(screen.getByRole("heading", { name: heading })).toBeVisible();
    expect(
      screen.getByRole("navigation", { name: "Documentation sections" }),
    ).toBeVisible();
  });

  it("states retry safety and separates planned endpoints", () => {
    render(<DocsPage />);
    expect(screen.getByText(/same/, { selector: "em" })).toBeVisible();
    expect(
      screen.getByText(/never assume another provider is safe/),
    ).toBeVisible();
    for (const name of ["Capture", "Refunds", "Routing rule editing"])
      expect(screen.getByText(`${name}.`)).toBeVisible();
  });

  it("generates cURL, TypeScript and Go from the same example", () => {
    const example = docsSource.examples.find((e) => e.id === "get-payment")!;
    expect(curlSnippet(example)).toContain(
      '"$RAILVANTA_BASE_URL/v1/payments/$PAYMENT_ID"',
    );
    expect(typescriptSnippet(example)).toContain("/v1/payments/${paymentId}");
    expect(goSnippet(example)).toContain('"/v1/payments/" + paymentId');
    for (const text of [
      curlSnippet(example),
      typescriptSnippet(example),
      goSnippet(example),
    ])
      expect(text).toContain("RAILVANTA_SANDBOX_KEY");
  });
});

const detail: EventDetail = {
  id: "whk_1",
  source: "inbox",
  event_type: "payment.succeeded",
  status: "processed",
  outcome: "ignored",
  outcome_reason: "terminal_state",
  provider_event_id: "evt_1",
  payment_id: "pay_1",
  duplicate_count: 2,
  needs_investigation: true,
  signature_status: "not_applicable",
  data_source: "simulated",
  created_at: "2026-01-01T00:00:00.000Z",
  last_received_at: "2026-01-01T00:00:05.000Z",
  provider_id: "prov",
  payload_hash: "a".repeat(64),
  conflicting_payload_hash: "b".repeat(64),
  conflict_count: 1,
  delivery_attempts: 0,
  processed_at: "2026-01-01T00:00:00.000Z",
  payment_status: "succeeded",
  attempts: [],
  payload_summary: {
    fields: [{ name: "type", value: "<img src=x onerror=alert(1)>" }],
    redacted_count: 2,
  },
};

describe("event detail", () => {
  it("labels mock events as signature not applicable and flags investigations", () => {
    render(<EventDetailView event={detail} />);
    expect(screen.getByText("Signature: Not applicable")).toBeVisible();
    expect(screen.queryByText(/Verified|Rejected/)).toBeNull();
    expect(screen.getByRole("alert")).toHaveTextContent("Needs investigation");
    expect(screen.getByText("b".repeat(64))).toBeVisible();
    expect(screen.getByText(/2 other fields redacted/)).toBeVisible();
  });

  it("renders payload values as text, never as HTML", () => {
    const { container } = render(<EventDetailView event={detail} />);
    expect(container.querySelector("img")).toBeNull();
    expect(container).toHaveTextContent("<img src=x onerror=alert(1)>");
  });

  it("offers a trace link only when a trace destination is configured", () => {
    const { rerender } = render(<EventDetailView event={detail} />);
    expect(screen.queryByRole("link", { name: "Open trace" })).toBeNull();
    expect(traceUrl("whk_1", undefined)).toBeNull();
    expect(traceUrl("whk_1", "http://insecure/{id}")).toBeNull();
    expect(traceUrl("whk_1", "https://traces.example/{id}")).toBe(
      "https://traces.example/whk_1",
    );
    act(() => rerender(<EventDetailView event={detail} />));
  });
});
