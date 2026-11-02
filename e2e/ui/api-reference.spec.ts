import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";
import { signIn } from "../support/browser";
import { state } from "../support/state";

test.beforeEach(async ({ context }) => {
  await signIn(context);
});

test("the Swagger UI API reference lists the contract and fits narrow screens", async ({
  page,
}) => {
  await page.goto(`${state().webOrigin}/dashboard/api-reference`);
  await expect(
    page.getByRole("heading", { level: 1, name: "API reference" }),
  ).toBeVisible();
  await expect(page.getByText("Sandbox only; no real funds.")).toBeVisible();
  // The help text links to the key generator (the sidebar also has a Settings link).
  await expect(
    page.getByRole("main").locator('a[href="/dashboard/settings#api-keys"]'),
  ).toBeVisible();

  const operations = page.locator(".swagger-ui .opblock");
  await expect(operations.first()).toBeVisible({ timeout: 30_000 });
  const payments = page
    .locator(".swagger-ui .opblock-post")
    .filter({ has: page.locator('[data-path="/v1/payments"]') });
  const keys = page
    .locator(".swagger-ui .opblock-post")
    .filter({ has: page.locator('[data-path="/v1/api-keys"]') });
  await expect(payments).toHaveCount(1);
  await expect(keys).toHaveCount(1);

  await payments.locator(".opblock-summary-control").click();
  await expect(payments.locator(".opblock-body")).toBeVisible();
  await expect(
    page.locator(".swagger-ui").getByRole("button", { name: /Authorize/ }),
  ).toBeVisible();

  // Swagger UI internals are third-party markup, so axe covers only our wrapper.
  const results = await new AxeBuilder({ page })
    .exclude(".swagger-ui")
    .analyze();
  expect(results.violations).toEqual([]);

  await page.setViewportSize({ width: 375, height: 800 });
  const overflow = await page.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
});
