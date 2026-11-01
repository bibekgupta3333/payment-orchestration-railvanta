import { randomUUID } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import AxeBuilder from "@axe-core/playwright";
import { test, expect } from "@playwright/test";
import { ApiClient } from "../support/api";
import { signIn } from "../support/browser";
import { state } from "../support/state";

interface RenderedExample {
  id: string;
  method: string;
  path: string;
  status: number;
  headers: Record<string, string>;
  body: string;
  binds: Record<string, string>;
  bodyBinds: Record<string, string>;
}

const unique = (
  JSON.parse(
    readFileSync(
      resolve(__dirname, "../../frontend/lib/docs/examples.json"),
      "utf8",
    ),
  ) as { unique: string[] }
).unique;

const serious = (violations: { impact?: string | null }[]) =>
  violations.filter((v) => ["serious", "critical"].includes(v.impact ?? ""));

test("the docs page is server-rendered with every example and no credentials", async ({
  request,
}) => {
  const response = await request.get(`${state().webOrigin}/docs`);
  expect(response.status()).toBe(200);
  const html = await response.text();
  expect(html.match(/data-docs-example/g)?.length).toBeGreaterThanOrEqual(10);
  for (const heading of [
    "Getting started",
    "Authentication",
    "Idempotency and retry safety",
    "Events and webhooks",
    "Supported and planned",
  ])
    expect(html).toContain(heading);
  for (const secret of [
    state().operatorKey,
    state().readerKey,
    state().sessionToken,
    state().csrfToken,
  ])
    expect(html).not.toContain(secret);
  expect(html).toContain("RAILVANTA_SANDBOX_KEY");
});

test("every example on /docs runs against the live sandbox API and returns its documented status", async ({
  page,
  request,
}) => {
  test.setTimeout(120_000);
  await page.goto(`${state().webOrigin}/docs`);
  const examples: RenderedExample[] = await page.$$eval(
    "[data-docs-example]",
    (nodes) =>
      nodes.map((node) => {
        const element = node as HTMLElement;
        return {
          id: element.dataset.exampleId ?? "",
          method: element.dataset.method ?? "",
          path: element.dataset.path ?? "",
          status: Number(element.dataset.expectedStatus),
          headers: JSON.parse(element.dataset.requestHeaders ?? "{}"),
          body: element.dataset.requestBody ?? "",
          binds: JSON.parse(element.dataset.binds ?? "{}"),
          bodyBinds: JSON.parse(element.dataset.bodyBinds ?? "{}"),
        };
      }),
  );
  expect(examples.length).toBeGreaterThanOrEqual(10);

  const run = randomUUID().replace(/-/g, "").slice(0, 10);
  const uniquify = (text: string) =>
    unique.reduce(
      (value, token) => value.split(token).join(`${token}-${run}`),
      text,
    );
  const api = new ApiClient(request);
  const bound: Record<string, string> = {};
  const responses: Record<
    string,
    {
      status: number;
      body: Record<string, unknown>;
      headers: Record<string, string>;
    }
  > = {};

  for (const example of examples) {
    let path = example.path;
    for (const [name, value] of Object.entries(bound))
      path = path.split(`{${name}}`).join(value);
    expect(path, `${example.id} has an unbound placeholder`).not.toMatch(
      /\{[a-z_]+\}/,
    );
    let data: Record<string, unknown> | undefined;
    if (example.body) {
      data = JSON.parse(uniquify(example.body)) as Record<string, unknown>;
      for (const [field, name] of Object.entries(example.bodyBinds))
        data[field] = bound[name];
    }
    const headers = Object.fromEntries(
      Object.entries(example.headers).map(([name, value]) => [
        name,
        uniquify(value),
      ]),
    );
    const response = await api.call(example.method, path, { data, headers });
    expect(response.status(), `${example.id} ${example.method} ${path}`).toBe(
      example.status,
    );
    const body = (await response.json()) as Record<string, unknown>;
    responses[example.id] = {
      status: response.status(),
      body,
      headers: response.headers(),
    };
    for (const [name, property] of Object.entries(example.binds)) {
      bound[name] = String(body[property]);
      expect(bound[name]).not.toBe("undefined");
    }
  }

  // The documented behaviors hold, not just the status codes.
  expect(responses["get-workspace"].body.environment).toBe("sandbox");
  expect(responses["retry-create-payment"].body.id).toBe(
    responses["create-payment"].body.id,
  );
  expect(responses["retry-create-payment"].headers["idempotent-replayed"]).toBe(
    "true",
  );
  expect(responses["send-mock-webhook"].body.signature_status).toBe(
    "not_applicable",
  );
  expect(responses["resend-mock-webhook"].body).toMatchObject({
    result: "duplicate",
    duplicate_count: 1,
  });
  expect(responses["invalid-amount"].body.code).toBe("validation_error");
  expect(
    (responses["list-events"].body.items as unknown[]).length,
  ).toBeGreaterThan(0);
  expect(
    (responses["get-settings"].body.providers as { code: string }[]).map(
      (p) => p.code,
    ),
  ).toEqual(["mock", "stripe", "plaid"]);
});

test("docs copy buttons announce success through aria-live, and sections collapse on mobile", async ({
  page,
  context,
}) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.setViewportSize({ width: 375, height: 900 });
  await page.goto(`${state().webOrigin}/docs`);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  const block = page.locator(".code-block").first();
  await block.getByRole("button", { name: "Copy example" }).click();
  await expect(block.locator("[aria-live='polite']")).toHaveText(
    "Example copied to clipboard",
  );
  expect(await page.evaluate(() => navigator.clipboard.readText())).toContain(
    "curl",
  );

  const nav = page.locator("details.docs-nav");
  await expect(nav).toHaveAttribute("open", "");
  await nav.locator("summary").click();
  await expect(nav).not.toHaveAttribute("open", "");
  await nav.locator("summary").click();
  await page.getByRole("link", { name: "Errors" }).click();
  await expect(page).toHaveURL(/#errors$/);

  const results = await new AxeBuilder({ page }).analyze();
  expect(serious(results.violations)).toEqual([]);
});

test("docs separate supported endpoints from planned ones", async ({
  page,
}) => {
  await page.goto(`${state().webOrigin}/docs`);
  const supported = page.getByRole("region", { name: "Supported endpoints" });
  await expect(supported).toContainText("/v1/payments");
  await expect(supported).toContainText("/v1/sandbox/webhooks/mock");
  await expect(supported).not.toContainText("refund");
  for (const item of ["Capture", "Refunds", "Routing rule editing"])
    await expect(page.getByText(`${item}.`)).toBeVisible();
  await expect(
    page.getByText(/never assume another provider is safe/),
  ).toBeVisible();
});

test.describe("settings", () => {
  test.beforeEach(async ({ context }) => {
    await signIn(context);
  });

  test("shows the workspace, a fixed Sandbox environment and read-only providers with no secret or fake controls", async ({
    page,
  }) => {
    const api = new ApiClient(page.request);
    const workspace = await api.workspace();
    await page.goto(`${state().webOrigin}/dashboard/settings`);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(
      "Settings",
    );
    await expect(page.getByText(workspace.name, { exact: true })).toBeVisible();
    const environment = page.locator("dd", { hasText: "Fixed." });
    await expect(environment).toContainText("Sandbox");
    const providers = page.getByRole("region", {
      name: "Provider configuration",
    });
    await expect(
      providers.locator("tr", { hasText: "Mock provider" }),
    ).toContainText("Enabled");
    await expect(providers.locator("tr", { hasText: "Stripe" })).toContainText(
      "Not configured",
    );
    await expect(providers.locator("tr", { hasText: "Plaid" })).toContainText(
      "Not configured",
    );
    const unavailable = page.locator(".unavailable-row");
    await expect(unavailable).toHaveCount(2);
    for (const label of ["Member roles", "Live activation"])
      await expect(unavailable.filter({ hasText: label })).toContainText(
        "Unavailable",
      );

    // The API key generator is real; roles and live mode remain unavailable
    // and have no controls anywhere on the page.
    await expect(
      page.getByRole("button", { name: "Generate API key" }),
    ).toBeVisible();
    await expect(
      page
        .getByRole("main")
        .getByRole("button", { name: /role|member|live|activate/i }),
    ).toHaveCount(0);
    const html = await page.content();
    for (const secret of [
      state().operatorKey,
      state().readerKey,
      state().otherKey,
      state().sessionToken,
      state().csrfToken,
    ])
      expect(html).not.toContain(secret);
    const results = await new AxeBuilder({ page }).analyze();
    expect(serious(results.violations)).toEqual([]);
  });

  test("generates a sandbox API key once, uses it against the API and revokes it", async ({
    page,
  }) => {
    await page.goto(`${state().webOrigin}/dashboard/settings`);
    await page.getByLabel("Key name").fill("E2E generated key");
    await page.getByLabel("Role").selectOption("read");
    await page.getByRole("button", { name: "Generate API key" }).click();

    const panel = page.getByTestId("new-api-key");
    await expect(panel).toBeVisible();
    await expect(panel).toContainText("only time the full key is shown");
    const secret = (
      await panel.locator("code, pre").first().innerText()
    ).trim();
    expect(secret).toMatch(/^rvk_[0-9a-f]{8}\.[0-9a-f]{48}$/);
    const prefix = secret.split(".")[0];

    // The generated key really authenticates, with the role it was given.
    const origin = state().webOrigin;
    const read = await page.request.get(`${origin}/v1/workspace`, {
      headers: { Authorization: `Bearer ${secret}` },
    });
    expect(read.status()).toBe(200);
    const denied = await page.request.post(`${origin}/v1/payments`, {
      headers: {
        Authorization: `Bearer ${secret}`,
        "Idempotency-Key": `e2e-${randomUUID()}`,
      },
      data: {},
    });
    expect(denied.status()).toBe(403);

    // The list shows the prefix, never the secret.
    const list = page.getByRole("region", { name: "API key list" });
    await expect(list).toContainText(prefix);
    await expect(list).not.toContainText(secret.split(".")[1]);

    // Dismissing removes the secret from the page, and a reload cannot bring it back.
    await page.getByRole("button", { name: /hide the key/i }).click();
    await expect(panel).toHaveCount(0);
    await page.reload();
    expect(await page.content()).not.toContain(secret.split(".")[1]);

    // Revoke through the UI with the explicit confirmation step.
    await page
      .getByRole("button", {
        name: new RegExp(`Revoke key E2E generated key \\(${prefix}\\)`),
      })
      .click();
    await page.getByRole("button", { name: "Confirm revoke" }).click();
    await expect(
      page
        .getByRole("region", { name: "API key list" })
        .locator("tr", { hasText: prefix }),
    ).toContainText("Revoked");
    const after = await page.request.get(`${origin}/v1/workspace`, {
      headers: { Authorization: `Bearer ${secret}` },
    });
    expect(after.status()).toBe(401);
  });

  test("theme choice (System, Light, Dark) persists across reloads without removing the Sandbox label", async ({
    page,
  }) => {
    await page.goto(`${state().webOrigin}/dashboard/settings`);
    const theme = () =>
      page.getByRole("main").getByRole("combobox", { name: "Theme" });
    await expect(theme().locator("option")).toHaveText([
      "System",
      "Light",
      "Dark",
    ]);
    await theme().selectOption("dark");
    await expect(page.locator("html")).toHaveClass(/dark/);
    await page.reload();
    await expect(theme()).toHaveValue("dark");
    await expect(page.locator("html")).toHaveClass(/dark/);
    await theme().selectOption("light");
    await page.reload();
    await expect(theme()).toHaveValue("light");
    await expect(page.locator("html")).not.toHaveClass(/dark/);
    await expect(
      page.getByText("Sandbox", { exact: true }).first(),
    ).toBeVisible();
  });
});

test("settings reads require a workspace membership", async ({
  page,
  request,
}) => {
  const api = new ApiClient(request);
  const anonymous = await api.call("GET", "/v1/settings", { key: null });
  expect(anonymous.status()).toBe(401);
  // Cross-workspace isolation is covered by the Go suite: auth-contract.spec.ts
  // revokes the other workspace's key earlier in this run.
  const wrong = await api.call("GET", "/v1/settings", {
    key: `reader.${"z".repeat(32)}`,
  });
  expect(wrong.status()).toBe(401);
  const reader = await api.call("GET", "/v1/settings", {
    key: state().readerKey,
  });
  expect(reader.status()).toBe(200);
  const body = await reader.text();
  expect((JSON.parse(body) as { role: string }).role).toBe("read");
  expect(body).not.toContain(state().readerKey);
  expect(body).not.toContain(state().operatorKey);
  // A browser without a session sees an error with a request ID, never data.
  await page.goto(`${state().webOrigin}/dashboard/settings`);
  await expect(page.getByRole("main").getByRole("alert")).toContainText(
    /Request ID/,
  );
  await expect(page.getByText("Demo workspace")).toHaveCount(0);
});
