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
  provider: "00000000-0000-7000-8000-000000000201",
  // Seeded demo-workspace methods whose mock token selects a scenario.
  successMethod: "00000000-0000-7000-8000-000000000121",
  declineMethod: "00000000-0000-7000-8000-000000000122",
  delayedSuccessMethod: "00000000-0000-7000-8000-000000000123",
  timeoutBeforeExecutionMethod: "00000000-0000-7000-8000-000000000124",
  timeoutAfterSuccessMethod: "00000000-0000-7000-8000-000000000125",
  outageMethod: "00000000-0000-7000-8000-000000000126",
} as const;

export function state(): HarnessState {
  const file = process.env.RAILVANTA_E2E_STATE;
  if (!file)
    throw new Error("Playwright global setup did not initialize the harness");
  return JSON.parse(readFileSync(file, "utf8")) as HarnessState;
}

async function control(
  operation: string,
  body?: Record<string, unknown>,
): Promise<Record<string, unknown>> {
  const current = state();
  const response = await fetch(`${current.controlOrigin}/${operation}`, {
    method: "POST",
    headers: { Authorization: `Bearer ${current.controlToken}` },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
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

export interface Inspection {
  attempts: {
    id: string;
    provider_id: string;
    provider_idempotency_key: string;
    status: string;
  }[];
  charges: {
    provider_id: string;
    provider_idempotency_key: string;
    status: string;
  }[];
  outbox: { event_type: string; status: string; attempts: number }[];
  journals: number;
  /** to_status of every audit transition, oldest first. */
  transitions: string[];
}

/** Read-only view of provider keys, mock charges and outbox rows for a payment. */
export async function inspectPayment(paymentId: string): Promise<Inspection> {
  return (await control("inspect", { paymentId })) as unknown as Inspection;
}

/** Re-queues a payment's dispatch command as if its completion was lost. */
export async function redeliverDispatch(paymentId: string): Promise<void> {
  await control("redeliver", { paymentId });
}

/**
 * Inserts a payment that stays `created` (its dispatch command is due in an
 * hour) so cancellation can be tested deterministically. Restarts the API.
 */
export async function seedCreatedPayment(method: string): Promise<string> {
  return (await control("seed-created", { method })).paymentId as string;
}
