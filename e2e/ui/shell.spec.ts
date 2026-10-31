import AxeBuilder from "@axe-core/playwright";
import { test, expect } from "@playwright/test";
import { state } from "../support/state";

const routes = [
  "/dashboard", "/dashboard/payments", "/dashboard/routing", "/dashboard/providers",
  "/dashboard/simulator", "/dashboard/ledger", "/dashboard/reconciliation",
  "/dashboard/events", "/dashboard/settings", "/docs",
];

test("public landing page renders through the browser harness", async ({ page }) => {
  await page.goto(state().webOrigin);
  await expect(page.getByRole("heading", { level: 1 })).toContainText("Payments,");
  await expect(page.getByRole("link", { name: /Explore the dashboard/ })).toBeVisible();
  await expect(page.getByText("Payment execution is not available", { exact: false })).toBeVisible();
});

for (const width of [375, 1440]) {
  test(`every navigation destination is keyboard reachable at ${width}px`, async ({ page }) => {
    test.setTimeout(120_000);
    await page.setViewportSize({ width, height: 900 });
    await page.goto(`${state().webOrigin}/dashboard`);
    for (const route of routes) {
      if (width === 375) {
        const trigger = page.getByRole("button", { name: "Open navigation", exact: true });
        await trigger.focus();
        await trigger.press("Enter");
      }
      const navigation = page.getByRole("navigation", { name: "Main navigation", exact: true });
      const destination = navigation.locator(`a[href="${route}"]`);
      await destination.focus();
      await expect(destination).toBeFocused();
      await destination.press("Enter");
      await expect(page).toHaveURL(`${state().webOrigin}${route}`);
    }
  });
  test(`all routes preserve an honest accessible sandbox shell at ${width}px`, async ({ page }) => {
    test.setTimeout(120_000);
    await page.setViewportSize({ width, height: 900 });
    for (const route of routes) {
      await page.goto(`${state().webOrigin}${route}`);
      await expect(page.getByRole("main")).toBeVisible();
      await expect(page.getByRole("main").getByRole("heading", { level: 1 })).toBeVisible();
      await expect(page.getByText("Sandbox", { exact: true }).first()).toBeVisible();
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
      const results = await new AxeBuilder({ page }).analyze();
      expect(results.violations.filter((violation) => ["serious", "critical"].includes(violation.impact ?? ""))).toEqual([]);
      if (!["/dashboard/settings", "/docs"].includes(route)) {
        await expect(page.getByText("Unavailable until backend exists", { exact: true })).toBeVisible();
      }
    }
  });
}

test("mobile drawer traps keyboard focus, closes on Escape, and restores its trigger", async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 900 });
  await page.goto(`${state().webOrigin}/dashboard`);
  const trigger = page.getByRole("button", { name: "Open navigation", exact: true });
  await trigger.focus();
  await page.keyboard.press("Enter");
  const dialog = page.getByRole("dialog", { name: "Navigation menu" });
  await expect(dialog).toBeVisible();
  for (let index = 0; index < 18; index++) {
    await page.keyboard.press("Tab");
    expect(await dialog.evaluate((element) => element.contains(document.activeElement))).toBe(true);
  }
  await page.keyboard.press("Escape");
  await expect(dialog).not.toBeVisible();
  await expect(trigger).toBeFocused();
  await trigger.press("Enter");
  await dialog.getByRole("link", { name: "Payments", exact: true }).focus();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(`${state().webOrigin}/dashboard/payments`);
});

test("theme selection persists without removing the Sandbox label", async ({ page }) => {
  await page.goto(`${state().webOrigin}/dashboard/settings`);
  const theme = page.getByRole("combobox", { name: "Theme", exact: true }).first();
  await theme.selectOption("dark");
  await page.reload();
  await expect(page.getByRole("combobox", { name: "Theme", exact: true }).first()).toHaveValue("dark");
  await expect(page.getByText("Sandbox", { exact: true }).first()).toBeVisible();
  await page.getByRole("combobox", { name: "Theme", exact: true }).first().selectOption("light");
});
