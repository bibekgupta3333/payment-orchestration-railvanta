import { readFileSync } from "node:fs";

export interface HarnessState {
  apiOrigin: string;
  healthOrigin: string;
  webOrigin: string;
  controlOrigin: string;
  controlToken: string;
  operatorKey: string;
  readerKey: string;
  otherKey: string;
  sessionToken: string;
  csrfToken: string;
}

export const fixtures = {
  workspace: "00000000-0000-7000-8000-000000000001",
  otherWorkspace: "00000000-0000-7000-8000-000000000002",
  method: "00000000-0000-7000-8000-000000000101",
  otherMethod: "00000000-0000-7000-8000-000000000102",
  durableMethod: "00000000-0000-7000-8000-000000000199",
} as const;

export function state(): HarnessState {
  const file = process.env.RAILVANTA_E2E_STATE;
  if (!file)
    throw new Error("Playwright global setup did not initialize the harness");
  return JSON.parse(readFileSync(file, "utf8")) as HarnessState;
}

async function control(operation: string): Promise<Record<string, unknown>> {
  const current = state();
  const response = await fetch(`${current.controlOrigin}/${operation}`, {
    method: "POST",
    headers: { Authorization: `Bearer ${current.controlToken}` },
    signal: AbortSignal.timeout(40_000),
  });
  if (!response.ok)
    throw new Error(`Harness ${operation} failed: ${await response.text()}`);
  return (await response.json()) as Record<string, unknown>;
}

export async function restartApi(): Promise<void> {
  await control("restart");
}

export async function killApi(): Promise<void> {
  await control("kill");
}

/** Stops the API without restarting it; always pair with startApi(). */
export async function stopApi(): Promise<void> {
  await control("stop");
}

export async function startApi(): Promise<void> {
  await control("start");
}

/** Offline fixture: moves a created payment to unknown (restarts the API). */
export async function markPaymentUnknown(paymentId: string): Promise<void> {
  await control(`mark-unknown?payment=${encodeURIComponent(paymentId)}`);
}

export function rejectProduction(): Promise<Record<string, unknown>> {
  return control("production");
}

export function rejectChecksumDrift(): Promise<Record<string, unknown>> {
  return control("checksum-drift");
}

export function readLogs(): Promise<Record<string, unknown>> {
  return control("logs");
}

export async function prepareDurabilityFixture(): Promise<void> {
  await control("durability-fixture");
}
