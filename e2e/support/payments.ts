import { randomUUID } from "node:crypto";
import { expect } from "@playwright/test";
import type { ApiClient } from "./api";

export interface Payment {
  id: string;
  amount_minor: string;
  currency: string;
  payment_method_id: string;
  merchant_reference: string | null;
  status: string;
}

export interface Attempt {
  id: string;
  payment_id: string;
  provider_id: string;
  attempt_number: number;
  status: string;
  routing_rule_id: string | null;
  routing_reason: string;
  provider_reference: string | null;
  error_code: string | null;
  submitted_at: string | null;
  completed_at: string | null;
}

export interface Journal {
  id: string;
  payment_id: string | null;
  currency: string;
  status: string;
  posting_key: string;
  entries: {
    account_code: string;
    provider_id: string | null;
    side: "debit" | "credit";
    amount_minor: string;
  }[];
}

/** Accepts a payment and returns its 202 body. */
export async function createPayment(
  api: ApiClient,
  method: string,
  amount = "25000",
  currency = "USD",
): Promise<Payment> {
  const response = await api.call("POST", "/v1/payments", {
    data: {
      amount_minor: amount,
      currency,
      payment_method_id: method,
      merchant_reference: `e2e_${randomUUID()}`,
    },
    headers: { "Idempotency-Key": `e2e_${randomUUID()}` },
  });
  expect(response.status()).toBe(202);
  return (await response.json()) as Payment;
}

export async function getPayment(api: ApiClient, id: string): Promise<Payment> {
  const response = await api.call("GET", `/v1/payments/${id}`);
  expect(response.status()).toBe(200);
  return (await response.json()) as Payment;
}

export async function listAttempts(
  api: ApiClient,
  id: string,
): Promise<Attempt[]> {
  const response = await api.call("GET", `/v1/payments/${id}/attempts`);
  expect(response.status()).toBe(200);
  return ((await response.json()) as { items: Attempt[] }).items;
}

/** Polls until the payment reaches the status, failing after the timeout. */
export async function waitForStatus(
  api: ApiClient,
  id: string,
  status: string,
  timeout = 15_000,
): Promise<Payment> {
  let latest: Payment | undefined;
  await expect
    .poll(
      async () => {
        latest = await getPayment(api, id);
        return latest.status;
      },
      { timeout, intervals: [50, 100, 200] },
    )
    .toBe(status);
  return latest as Payment;
}

export async function journalsFor(
  api: ApiClient,
  paymentId: string,
  key?: string,
): Promise<Journal[]> {
  const response = await api.call(
    "GET",
    `/v1/ledger/transactions?payment_id=${paymentId}`,
    key === undefined ? {} : { key },
  );
  expect(response.status()).toBe(200);
  return ((await response.json()) as { items: Journal[] }).items;
}
