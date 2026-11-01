import { test, expect } from "@playwright/test";
import { ApiClient } from "../support/api";
import { signIn } from "../support/browser";
import { state } from "../support/state";

test.beforeEach(async ({ context }) => {
  await signIn(context);
});

test("the dashboard home is a hub with quick actions and the live metrics below", async ({
  page,
}) => {
  await page.goto(`${state().webOrigin}/dashboard`);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "Railvanta sandbox",
  );
  const main = page.getByRole("main");
  await expect(main.getByRole("link", { name: /Run a demo/ })).toHaveAttribute(
    "href",
    "/dashboard/playground",
  );
  await expect(
    main.getByRole("button", { name: /Create a \$100 payment/ }),
  ).toBeVisible();
  await expect(
    main.getByRole("link", { name: /API reference \(Swagger\)/ }),
  ).toHaveAttribute("href", "/dashboard/api-reference");
  await expect(
    main.getByRole("link", { name: /Generate API key/ }),
  ).toBeVisible();
  await expect(
    main.getByRole("heading", { name: "Live sandbox metrics" }),
  ).toBeVisible();
  await expect(page.locator(".source-line")).toContainText(/^Source:/);
});

test("one click mints the default demo key, which authenticates, then revoke it", async ({
  page,
}) => {
  await page.goto(`${state().webOrigin}/dashboard`);
  const card = page.locator("#integration-credentials");
  await expect(card.getByText(/\d+ of \d+ active keys used/)).toBeVisible();
  await expect(page.getByTestId("new-api-key")).toHaveCount(0);

  await card.getByRole("button", { name: "Generate default demo key" }).click();
  const panel = page.getByTestId("new-api-key");
  await expect(panel).toBeVisible();
  const secret = (
    await panel.locator("pre").first().textContent()
  )?.trim() as string;
  expect(secret).toMatch(/^rvk_[0-9a-f]{8}\.[0-9a-f]{48}$/);

  const api = new ApiClient(page.request);
  const workspace = await api.call("GET", "/v1/workspace", { key: secret });
  expect(workspace.status()).toBe(200);

  await panel.getByRole("button", { name: /hide the key/i }).click();
  await expect(page.getByTestId("new-api-key")).toHaveCount(0);

  // Leave state clean: revoke the key through the API.
  const prefix = secret.split(".")[0];
  const list = (await (await api.call("GET", "/v1/api-keys")).json()) as {
    items: { id: string; prefix: string; status: string }[];
  };
  const minted = list.items.find((item) => item.prefix === prefix);
  expect(minted).toBeDefined();
  const revoked = await api.call("DELETE", `/v1/api-keys/${minted?.id}`);
  expect(revoked.status()).toBe(200);
});

test("the Lost response demo button runs a real simulation and reports invariants", async ({
  page,
}) => {
  await page.goto(`${state().webOrigin}/dashboard`);
  const button = page.getByRole("button", { name: "Lost response" });
  await expect(button).toBeEnabled();
  await button.click();
  await expect(page.getByTestId("hub-demo-result")).toContainText(
    /\d+ of \d+ invariants passed/,
    { timeout: 30_000 },
  );
  await expect(
    page.getByRole("link", { name: "Open in Playground" }),
  ).toBeVisible();
});

test("the hub does not scroll horizontally at 375px", async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 900 });
  await page.goto(`${state().webOrigin}/dashboard`);
  await expect(
    page.getByRole("heading", { name: "Live sandbox metrics" }),
  ).toBeVisible();
  const overflow = await page.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
});
