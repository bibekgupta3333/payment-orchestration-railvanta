// OPTIONAL: records a video of the guided flow with Playwright's built-in recorder against the
// running demo stack. Not part of any gate. Usage: pnpm demo:up && node scripts/demo-record.mjs
// Output: .demo/recordings/*.webm. It only opens the Playground and clicks buttons whose
// accessible names match DEMO_BUTTONS (comma separated, default below); adjust to the final UI.
import { mkdirSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { signedInContext } from "./demo-open.mjs";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const { chromium } = createRequire(join(root, "e2e", "package.json"))("@playwright/test");
const dir = join(root, ".demo", "recordings");
mkdirSync(dir, { recursive: true });

const buttons = (process.env.DEMO_BUTTONS ?? "Create payment,Lose provider response").split(",");
const browser = await chromium.launch({ headless: false });
const { context, origin } = await signedInContext(browser, {
  viewport: { width: 1440, height: 900 },
  recordVideo: { dir, size: { width: 1440, height: 900 } },
});
const page = await context.newPage();
await page.goto(`${origin}/dashboard/playground`);
await page.waitForTimeout(3000);
for (const name of buttons) {
  await page.getByRole("button", { name: new RegExp(name.trim(), "i") }).first().click();
  await page.waitForTimeout(12000);
}
await context.close();
await browser.close();
console.log(`Video saved under ${dir}`);
