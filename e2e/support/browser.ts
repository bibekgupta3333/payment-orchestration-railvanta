import type { BrowserContext } from "@playwright/test";
import { state } from "./state";

/**
 * Gives the browser the seeded demo session: the HttpOnly session cookie the
 * API authenticates, plus the readable CSRF cookie the dashboard echoes in
 * X-CSRF-Token on writes. Loopback HTTP counts as a secure context in
 * Chromium, so the Secure __Host- cookie is accepted and sent.
 */
export async function signIn(context: BrowserContext): Promise<void> {
  const current = state();
  const host = new URL(current.webOrigin).hostname;
  await context.addCookies([
    {
      name: "__Host-railvanta_session",
      value: current.sessionToken,
      domain: host,
      path: "/",
      secure: true,
      httpOnly: true,
      sameSite: "Strict",
    },
    {
      name: "railvanta_csrf",
      value: current.csrfToken,
      domain: host,
      path: "/",
      secure: true,
      httpOnly: false,
      sameSite: "Strict",
    },
  ]);
}
