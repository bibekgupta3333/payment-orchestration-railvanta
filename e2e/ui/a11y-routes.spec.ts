import AxeBuilder from "@axe-core/playwright";
import { test, expect, type Page } from "@playwright/test";
import { ApiClient } from "../support/api";
import { signIn } from "../support/browser";
import {
  eventId,
  listEvents,
  sendWebhook,
  webhookBody,
} from "../support/events";
import { createPayment, journalsFor, waitForStatus } from "../support/payments";
import { fixtures, state } from "../support/state";

// WBS 5.1.9: every public and dashboard route, list and detail, is checked at
// 375px and 1440px for horizontal scroll, keyboard-only operation and axe.

const staticRoutes = [
  "/",
  "/docs",
  "/dashboard",
  "/dashboard/payments",
  "/dashboard/routing",
  "/dashboard/providers",
  "/dashboard/simulator",
  "/dashboard/ledger",
  "/dashboard/reconciliation",
  "/dashboard/events",
  "/dashboard/settings",
];

test.beforeEach(async ({ context }) => {
  await signIn(context);
});

async function detailRoutes(api: ApiClient): Promise<string[]> {
  const payment = await createPayment(api, fixtures.successMethod, "4200");
  await waitForStatus(api, payment.id, "succeeded");
  const [journal] = await journalsFor(api, payment.id);
  const hook = eventId("evt_a11y");
  await sendWebhook(api, webhookBody(hook, "payment.succeeded", payment.id));
  const events = await listEvents(api, `?q=${hook}`);
  const routes = [
    `/dashboard/payments/${payment.id}`,
    `/dashboard/ledger/${journal.id}`,
    `/dashboard/providers/${fixtures.provider}`,
    "/dashboard/providers/stripe",
  ];
  if (events.items[0]) routes.push(`/dashboard/events/${events.items[0].id}`);
  return routes;
}

async function settle(page: Page, route: string) {
  await page.goto(`${state().webOrigin}${route}`);
  await expect(page.getByRole("heading", { level: 1 }).first()).toBeVisible();
  await expect(page.locator(".skeleton")).toHaveCount(0, { timeout: 15_000 });
}

/** Tabs through the page and asserts focus always lands on a visible element. */
async function tabThrough(page: Page, route: string) {
  await page.evaluate(() => (document.activeElement as HTMLElement)?.blur());
  const seen = new Set<string>();
  for (let step = 0; step < 60; step++) {
    await page.keyboard.press("Tab");
    const focus = await page.evaluate(() => {
      const el = document.activeElement as HTMLElement | null;
      if (!el || el === document.body) return null;
      // The Next.js dev overlay is not part of the product UI.
      if (el.tagName === "NEXTJS-PORTAL") return { key: "dev", visible: true };
      const box = el.getBoundingClientRect();
      const style = getComputedStyle(el);
      return {
        key: `${el.tagName}|${el.getAttribute("href") ?? ""}|${(el.textContent ?? "").trim().slice(0, 30)}|${el.id}`,
        visible:
          style.visibility !== "hidden" &&
          style.display !== "none" &&
          (box.width > 0 || box.height > 0),
      };
    });
    if (focus === null) break; // wrapped around to the browser chrome
    expect(
      focus.visible,
      `focus on a hidden element at ${route}: ${focus.key}`,
    ).toBe(true);
    seen.add(focus.key);
  }
  expect(seen.size, `no tabbable controls on ${route}`).toBeGreaterThan(0);
}

for (const width of [375, 1440]) {
  test(`every route fits ${width}px, is keyboard operable and has no serious axe violations`, async ({
    page,
    request,
  }) => {
    test.setTimeout(240_000);
    const api = new ApiClient(request);
    const routes = [...staticRoutes, ...(await detailRoutes(api))];
    await page.setViewportSize({ width, height: 900 });
    for (const route of routes) {
      await settle(page, route);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
        `horizontal scroll at ${route}`,
      ).toBe(true);
      await tabThrough(page, route);
      const results = await new AxeBuilder({ page }).analyze();
      expect(
        results.violations
          .filter((v) => ["serious", "critical"].includes(v.impact ?? ""))
          .map((v) => `${route}: ${v.id}`),
      ).toEqual([]);
    }
  });
}

test("375px drawer navigation: Tab stays inside the dialog, Escape closes and restores focus", async ({
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 900 });
  await settle(page, "/dashboard");
  const trigger = page.getByRole("button", {
    name: "Open navigation",
    exact: true,
  });
  await trigger.focus();
  await page.keyboard.press("Enter");
  const dialog = page.getByRole("dialog", { name: "Navigation menu" });
  await expect(dialog).toBeVisible();
  for (const key of ["Tab", "Tab", "Shift+Tab", "Shift+Tab", "Shift+Tab"]) {
    await page.keyboard.press(key);
    expect(
      await dialog.evaluate((el) => el.contains(document.activeElement)),
    ).toBe(true);
  }
  await page.keyboard.press("Escape");
  await expect(dialog).not.toBeVisible();
  await expect(trigger).toBeFocused();
});

for (const width of [375, 1440]) {
  test(`create-payment dialog traps focus, closes on Escape and restores focus at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await settle(page, "/dashboard/payments");
    const trigger = page.getByRole("button", {
      name: "Create sandbox payment",
      exact: true,
    });
    await expect(trigger).toBeEnabled();
    await trigger.focus();
    await page.keyboard.press("Enter");
    const dialog = page.getByRole("dialog", { name: "Create sandbox payment" });
    await expect(dialog).toBeVisible();
    const keys = [...Array(12).fill("Tab"), "Shift+Tab", "Shift+Tab"];
    for (const key of keys) {
      await page.keyboard.press(key);
      expect(
        await dialog.evaluate((el) => el.contains(document.activeElement)),
      ).toBe(true);
    }
    await page.keyboard.press("Escape");
    await expect(dialog).not.toBeVisible();
    await expect(trigger).toBeFocused();
  });
}
