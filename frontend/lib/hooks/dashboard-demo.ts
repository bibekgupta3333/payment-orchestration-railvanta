import type { ApiKey } from "@/lib/api/apikey-schemas";
import type { Simulation } from "@/lib/api/routing-schemas";

/** The one-click key: operator, 24 hours. Nothing is minted on page load. */
export const defaultKeyInput = {
  name: "Default demo key",
  role: "operator",
  expires_in_hours: 24,
} as const;

/** The three hero demos on the dashboard home ($100.00 USD sandbox payment). */
export const heroDemos = [
  { scenario: "timeout_after_success", label: "Lost response" },
  { scenario: "duplicate_request", label: "Duplicate requests" },
  { scenario: "duplicate_posting", label: "Double-entry ledger" },
] as const;
export type HeroScenario = (typeof heroDemos)[number]["scenario"];

export function freshSeed(): string {
  return `hub-${Date.now().toString(36)}${Math.floor(Math.random() * 1296).toString(36)}`;
}

/** True when an active key already has the default name (ignoring `exceptId`). */
export function hasActiveDefaultKey(
  items: ApiKey[],
  exceptId?: string,
): boolean {
  return items.some(
    (item) =>
      item.name === defaultKeyInput.name &&
      item.status === "active" &&
      item.id !== exceptId,
  );
}

export function activeKeyCount(items: ApiKey[]): number {
  return items.filter((item) => item.status === "active").length;
}

export function invariantSummary(run: Simulation): {
  total: number;
  passed: number;
  failed: string[];
  text: string;
} {
  const total = run.invariants.length;
  const passed = run.invariants.filter((item) => item.status === "pass").length;
  const failed = run.invariants
    .filter((item) => item.status === "fail")
    .map((item) => item.title);
  const text =
    failed.length > 0
      ? `${passed} of ${total} invariants passed; failed: ${failed.join(", ")}`
      : `${passed} of ${total} invariants passed`;
  return { total, passed, failed, text };
}

/** Live label while a run is in flight, taken from the simulation view. */
export function liveLabel(run: Simulation | null): string {
  const status = run?.payment?.status;
  const names: Record<string, string> = {
    created: "Created",
    processing: "Processing",
    succeeded: "Succeeded",
    failed: "Failed",
    canceled: "Canceled",
    unknown: "Unknown",
  };
  return status
    ? `Running… payment is ${names[status] ?? status}`
    : "Running… starting the simulation";
}
