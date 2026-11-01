// Launches a HEADED Chromium already signed in to the demo stack (session + CSRF cookies from
// .demo/credentials.env, same recipe as e2e/support/browser.ts) at the Playground.
// Run via `pnpm demo:open`; it stays open until the window is closed.
import { createRequire } from "node:module";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const { chromium } = createRequire(join(root, "e2e", "package.json"))("@playwright/test");

export function readCredentials() {
  let text;
  try {
    text = readFileSync(join(root, ".demo", "credentials.env"), "utf8");
  } catch {
    console.error("Missing .demo/credentials.env: run `pnpm demo:up` first.");
    process.exit(1);
  }
  return Object.fromEntries(
    text.split("\n").filter((l) => l.includes("=")).map((l) => [l.slice(0, l.indexOf("=")), l.slice(l.indexOf("=") + 1)]),
  );
}

export async function signedInContext(browser, contextOptions = {}) {
  const creds = readCredentials();
  const origin = process.env.DEMO_ORIGIN ?? `http://localhost:${process.env.RAILVANTA_PORT ?? 3000}`;
  const host = new URL(origin).hostname;
  const context = await browser.newContext({ viewport: null, ...contextOptions });
  await context.addCookies([
    { name: "__Host-railvanta_session", value: creds.DEMO_SESSION_TOKEN, domain: host, path: "/", secure: true, httpOnly: true, sameSite: "Strict" },
    { name: "railvanta_csrf", value: creds.DEMO_CSRF_TOKEN, domain: host, path: "/", secure: true, httpOnly: false, sameSite: "Strict" },
  ]);
  return { context, origin, creds };
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const browser = await chromium.launch({ headless: false, args: ["--start-maximized"] });
  const { context, origin } = await signedInContext(browser);
  const check = await context.request.get(`${origin}/v1/session`);
  console.log(`/v1/session -> ${check.status()}${check.ok() ? " (signed in)" : " (NOT signed in: is the stack up and seeded?)"}`);
  const page = await context.newPage();
  await page.goto(`${origin}/dashboard/playground`);
  console.log("Browser open at /dashboard/playground. Close the window (or Ctrl+C) to exit.");
  await new Promise((resolve) => browser.on("disconnected", resolve));
}
