import { test, expect } from "@playwright/test";
import { signIn } from "../support/browser";
import { state } from "../support/state";

test("the landing page explains why Railvanta exists with six problem cards", async ({
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 800 });
  await page.goto(state().webOrigin);
  const why = page.getByRole("region", { name: "Payments fail in the gaps." });
  await expect(why).toBeVisible();
  await expect(why).toContainText("Why Railvanta exists");
  await expect(why.getByRole("listitem")).toHaveCount(6);
  await expect(
    why.getByRole("link", { name: /See it in the sandbox/ }),
  ).toHaveCount(6);
  await expect(
    why.getByRole("heading", { name: "A retry charges the customer twice" }),
  ).toBeVisible();
  // No horizontal page scroll at phone width.
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - window.innerWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
});

test("the dashboard hub opens with the story panel", async ({
  page,
  context,
}) => {
  await signIn(context);
  await page.goto(`${state().webOrigin}/dashboard`);
  const panel = page.getByTestId("hub-story");
  await expect(
    panel.getByRole("heading", {
      name: "The story: what this sandbox proves",
    }),
  ).toBeVisible();
  await expect(panel.getByRole("listitem")).toHaveCount(6);
  // The native harness does not seed samples (SANDBOX_SAMPLE_DATA unset);
  // a Compose stack does. Accept either the empty-state hint or loaded rows.
  const hint = panel.getByTestId("hub-story-empty");
  const loaded = panel.getByText(/invariants pass|Running/).first();
  const unavailable = panel.getByTestId("hub-story-error");
  await expect(hint.or(loaded).or(unavailable)).toBeVisible();
  if (await hint.isVisible()) {
    await expect(
      hint.getByRole("link", { name: "Load sample data" }),
    ).toHaveAttribute("href", "#hub-sample-data");
  }
});
