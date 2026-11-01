import { randomUUID } from "node:crypto";
import { expect, type APIResponse } from "@playwright/test";
import type { ApiClient } from "./api";

export interface Receipt {
  event_id: string;
  provider_event_id: string;
  result: "processed" | "duplicate";
  outcome: "applied" | "ignored" | null;
  outcome_reason: string | null;
  duplicate_count: number;
  signature_status: string;
}

export interface EventItem {
  id: string;
  source: "inbox" | "outbox";
  event_type: string;
  status: string;
  outcome: string | null;
  outcome_reason: string | null;
  provider_event_id: string | null;
  payment_id: string | null;
  duplicate_count: number;
  needs_investigation: boolean;
  signature_status: string;
}

export function eventId(prefix = "evt_e2e"): string {
  return `${prefix}_${randomUUID().replace(/-/g, "").slice(0, 16)}`;
}

export function webhookBody(
  event: string,
  type: "payment.succeeded" | "payment.failed" | "payment.processing",
  paymentId: string,
): Record<string, string> {
  return { event_id: event, type, payment_id: paymentId };
}

/** Posts to the sandbox-only mock inbound route as an operator. */
export function sendWebhook(
  api: ApiClient,
  body: Record<string, string>,
): Promise<APIResponse> {
  return api.call("POST", "/v1/sandbox/webhooks/mock", { data: body });
}

export async function receipt(response: APIResponse): Promise<Receipt> {
  return (await response.json()) as Receipt;
}

export async function listEvents(
  api: ApiClient,
  query: string,
): Promise<{ items: EventItem[]; next_cursor: string | null }> {
  const response = await api.call("GET", `/v1/events${query}`);
  expect(response.status()).toBe(200);
  return response.json();
}
