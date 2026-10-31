import { render, screen, within, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { AppShell, navigation } from "@/components/layout/app-shell";
import { ThemeProvider, ThemeSelect } from "@/components/theme";
import { StatusPill, UnavailableState } from "@/components/ui";

vi.mock("next/navigation", () => ({ usePathname: () => "/dashboard" }));

describe("sandbox shell", () => {
  it("shows sandbox context, all destinations, and current route", () => {
    render(
      <ThemeProvider>
        <AppShell>
          <h1>Overview</h1>
        </AppShell>
      </ThemeProvider>,
    );
    const nav = screen.getAllByRole("navigation", {
      name: "Main navigation",
    })[0];
    for (const item of navigation)
      expect(
        within(nav).getByRole("link", { name: item.label }),
      ).toHaveAttribute("href", item.href);
    expect(within(nav).getByRole("link", { name: "Overview" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByText("Sandbox", { exact: true })).toBeVisible();
    expect(screen.getByRole("main")).toHaveAttribute("id", "main-content");
  });

  it("traps drawer focus and restores the trigger when closed", async () => {
    const user = userEvent.setup();
    render(
      <ThemeProvider>
        <AppShell>
          <h1>Overview</h1>
        </AppShell>
      </ThemeProvider>,
    );
    const trigger = screen.getByRole("button", { name: "Open navigation" });
    await user.click(trigger);
    const dialog = screen.getByRole("dialog", { name: "Navigation menu" });
    const close = within(dialog).getByRole("button", {
      name: "Close navigation menu",
    });
    const theme = within(dialog).getByRole("combobox", { name: "Theme" });
    close.focus();
    fireEvent.keyDown(dialog, { key: "Tab", shiftKey: true });
    expect(theme).toHaveFocus();
    fireEvent.keyDown(dialog, { key: "Tab" });
    expect(close).toHaveFocus();
    fireEvent(dialog, new Event("cancel", { cancelable: true }));
    expect(dialog).not.toHaveAttribute("open");
    expect(trigger).toHaveFocus();
  });

  it("closes navigation after selecting a destination", async () => {
    const user = userEvent.setup();
    render(
      <ThemeProvider>
        <AppShell>
          <h1>Overview</h1>
        </AppShell>
      </ThemeProvider>,
    );
    await user.click(screen.getByRole("button", { name: "Open navigation" }));
    const dialog = screen.getByRole("dialog", { name: "Navigation menu" });
    await user.click(within(dialog).getByRole("link", { name: "Payments" }));
    expect(dialog).not.toHaveAttribute("open");
  });

  it("shows uncertainty without a retry action", () => {
    render(
      <>
        <StatusPill status="unknown" />
        <UnavailableState />
      </>,
    );
    expect(screen.getByText("Outcome unknown")).toBeVisible();
    expect(
      screen.getByRole("heading", { name: "Unavailable until backend exists" }),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: /retry/i }),
    ).not.toBeInTheDocument();
  });
});

describe("theme preference", () => {
  it("persists dark preference and restores it on mount", async () => {
    const user = userEvent.setup();
    const { unmount } = render(
      <ThemeProvider>
        <ThemeSelect />
      </ThemeProvider>,
    );
    await user.selectOptions(
      screen.getByRole("combobox", { name: "Theme" }),
      "dark",
    );
    expect(document.documentElement).toHaveClass("dark");
    expect(localStorage.getItem("railvanta-theme")).toBe("dark");
    unmount();
    render(
      <ThemeProvider>
        <ThemeSelect />
      </ThemeProvider>,
    );
    expect(screen.getByRole("combobox", { name: "Theme" })).toHaveValue("dark");
    expect(document.documentElement).toHaveClass("dark");
  });
});
