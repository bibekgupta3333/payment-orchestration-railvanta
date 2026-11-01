"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  useCallback,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { ApiError, apiRequestWithMeta, type ApiResult } from "@/lib/api/client";
import { paymentSchema, type Payment } from "@/lib/api/schemas";

/** The exact request body of one payment intent. */
export interface PaymentIntent {
  amount_minor: string;
  currency: string;
  payment_method_id: string;
  merchant_reference?: string;
}

export interface PendingSubmission {
  fingerprint: string;
  idempotencyKey: string;
  intent: PaymentIntent;
  startedAt: string;
}

/**
 * sessionStorage survives a reload in the same tab, so a submission whose
 * response was lost (timeout, offline, reload while pending) is resent with
 * the same Idempotency-Key and replays instead of creating a second payment.
 */
export const pendingStorageKey = "railvanta.payment-intent.pending";

export function intentFingerprint(intent: PaymentIntent): string {
  return JSON.stringify([
    intent.amount_minor,
    intent.currency,
    intent.payment_method_id,
    intent.merchant_reference ?? "",
  ]);
}

function newIdempotencyKey(): string {
  return `pay_${crypto.randomUUID()}`;
}

function parsePendingSubmission(raw: string | null): PendingSubmission | null {
  if (!raw) return null;
  try {
    const value = JSON.parse(raw) as PendingSubmission;
    if (
      typeof value.idempotencyKey !== "string" ||
      typeof value.fingerprint !== "string" ||
      intentFingerprint(value.intent) !== value.fingerprint
    )
      return null;
    return value;
  } catch {
    return null;
  }
}

function readRawPending(): string | null {
  try {
    return sessionStorage.getItem(pendingStorageKey);
  } catch {
    return null;
  }
}

export function readPendingSubmission(): PendingSubmission | null {
  return parsePendingSubmission(readRawPending());
}

const pendingChangeEvent = "railvanta:pending-submission";

function writePendingSubmission(value: PendingSubmission | null): void {
  try {
    if (value) sessionStorage.setItem(pendingStorageKey, JSON.stringify(value));
    else sessionStorage.removeItem(pendingStorageKey);
  } catch {
    /* Storage can be unavailable; the in-memory key still covers this page. */
  }
  if (typeof window !== "undefined")
    window.dispatchEvent(new Event(pendingChangeEvent));
}

function subscribePending(onChange: () => void): () => void {
  window.addEventListener(pendingChangeEvent, onChange);
  window.addEventListener("storage", onChange);
  return () => {
    window.removeEventListener(pendingChangeEvent, onChange);
    window.removeEventListener("storage", onChange);
  };
}

/** The unconfirmed submission stored for this tab, if any (null on the server). */
export function usePendingSubmission(): PendingSubmission | null {
  const raw = useSyncExternalStore(
    subscribePending,
    readRawPending,
    () => null,
  );
  return useMemo(() => parsePendingSubmission(raw), [raw]);
}

/**
 * Returns the key for this intent: the stored key while the same intent is
 * unconfirmed, otherwise a fresh key that replaces the stored one (an edited
 * intent is a different payment and must never reuse a key).
 */
export function idempotencyKeyFor(intent: PaymentIntent): PendingSubmission {
  const fingerprint = intentFingerprint(intent);
  const existing = readPendingSubmission();
  if (existing && existing.fingerprint === fingerprint) return existing;
  const created: PendingSubmission = {
    fingerprint,
    idempotencyKey: newIdempotencyKey(),
    intent,
    startedAt: new Date().toISOString(),
  };
  writePendingSubmission(created);
  return created;
}

export function clearPendingSubmission(): void {
  writePendingSubmission(null);
}

export type SubmissionOutcome =
  | { kind: "idle" }
  | { kind: "pending" }
  | { kind: "accepted"; result: ApiResult<Payment>; replayed: boolean }
  /** No definitive response: the payment may or may not exist. */
  | { kind: "unconfirmed"; error: ApiError }
  /** A definitive rejection; nothing was created by this request. */
  | { kind: "rejected"; error: ApiError };

const definitiveRejections = [400, 401, 403, 404, 409, 413, 415, 422, 429];

export function useCreatePayment() {
  const queryClient = useQueryClient();
  const inFlight = useRef(false);
  const [outcome, setOutcome] = useState<SubmissionOutcome>({ kind: "idle" });
  const mutation = useMutation({
    retry: false,
    mutationFn: async (intent: PaymentIntent) => {
      const submission = idempotencyKeyFor(intent);
      return apiRequestWithMeta("/v1/payments", paymentSchema, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Idempotency-Key": submission.idempotencyKey,
        },
        body: JSON.stringify(intent),
        timeoutMs: 20_000,
      });
    },
  });

  const submit = useCallback(
    async (intent: PaymentIntent) => {
      // A synchronous lock: a double-click can fire twice before React
      // re-renders the disabled button.
      if (inFlight.current) return;
      inFlight.current = true;
      setOutcome({ kind: "pending" });
      try {
        const result = await mutation.mutateAsync(intent);
        // Only an API-confirmed 202 ends the intent; the payment is accepted,
        // not succeeded.
        clearPendingSubmission();
        setOutcome({
          kind: "accepted",
          result,
          replayed: result.headers.get("idempotent-replayed") === "true",
        });
        await queryClient.invalidateQueries({ queryKey: ["railvanta"] });
      } catch (caught) {
        const error =
          caught instanceof ApiError
            ? caught
            : new ApiError(0, {
                code: "client_error",
                message: "The submission could not be confirmed.",
                request_id: "unavailable",
                field_errors: [],
              });
        const inProgress = error.details.code === "idempotency_in_progress";
        if (definitiveRejections.includes(error.status) && !inProgress) {
          setOutcome({ kind: "rejected", error });
        } else {
          // Keep the stored key so a resend replays instead of duplicating.
          setOutcome({ kind: "unconfirmed", error });
        }
      } finally {
        inFlight.current = false;
      }
    },
    [mutation, queryClient],
  );

  const reset = useCallback(() => {
    setOutcome({ kind: "idle" });
    mutation.reset();
  }, [mutation]);

  return {
    submit,
    reset,
    outcome,
    isPending: outcome.kind === "pending",
  };
}
