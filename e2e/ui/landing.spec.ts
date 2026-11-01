import AxeBuilder from "@axe-core/playwright";
import { test, expect, type Page } from "@playwright/test";
import { state } from "../support/state";

async function publicAccess(page: Page): Promise<boolean> {
  const response = await page.request.get(
    `${state().webOrigin}/v1/sandbox/status`,
  );
  if (!response.ok()) return false;
  const body = (await response.json()) as { public_access?: boolean };
  return body.public_access === true;
}

test("the signed-out landing page shows the lifecycle visual and its text list", async ({
  page,
}) => {
  await page.goto(state().webOrigin);
  await expect(page.getByRole("heading", { level: 1 })).toContainText(
    "Payments,",
  );
  const lifecycle = page.getByRole("region", {
    name: "From request to balanced ledger",
  });
  await expect(lifecycle).toBeVisible();
  // The canvas (or its fallback) is decorative; the ordered list is the text.
  await expect(lifecycle.locator('[aria-hidden="true"]').first()).toBeVisible();
  await expect(
    lifecycle
      .getByRole("list", { name: "Payment lifecycle steps" })
      .getByRole("listitem"),
  ).toHaveCount(7);

  const caption = page.getByTestId("lifecycle-caption");
  await expect(caption).toContainText("accepted once");
  await lifecycle.getByRole("button", { name: /Lost response/ }).click();
  await expect(caption).toContainText("never to a second one");
  await lifecycle.getByRole("button", { name: "Duplicate request" }).click();
  await expect(caption).toContainText("stored response replayed");
});

test("the landing page has no horizontal scroll at 375px and no serious a11y violations", async ({
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 800 });
  await page.goto(state().webOrigin);
  await expect(page.getByTestId("lifecycle-caption")).toBeVisible();
  const overflow = await page.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);

  const results = await new AxeBuilder({ page }).exclude("canvas").analyze();
  const serious = results.violations.filter(
    (violation) =>
      violation.impact === "serious" || violation.impact === "critical",
  );
  expect(serious).toEqual([]);
});

test("Start sandbox shows a one-time key and Run it creates a payment", async ({
  page,
}) => {
  test.skip(
    !(await publicAccess(page)),
    "public sandbox access is off in this stack",
  );
  await page.goto(state().webOrigin);
  const panel = page.getByRole("region", { name: "Call the API in one step" });
  await panel.getByRole("button", { name: "Start sandbox" }).click();
  const key = page.getByTestId("new-api-key");
  await expect(key).toBeVisible();
  const secret = (await key.locator("pre").first().textContent())?.trim();
  expect(secret).toMatch(/^rvk_[0-9a-f]{8}\.[0-9a-f]{48}$/);
  await expect(
    panel.getByLabel("API example: create a sandbox payment"),
  ).toContainText(`Bearer ${secret}`);

  await panel.getByRole("button", { name: "Run it", exact: true }).click();
  const result = page.getByTestId("run-result");
  await expect(result).toContainText("HTTP 202");
  await expect(result).toContainText(/"id":\s*"/);

  await key.getByRole("button", { name: /hide the key/i }).click();
  await expect(page.getByTestId("new-api-key")).toHaveCount(0);
});
