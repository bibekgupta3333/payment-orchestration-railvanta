import { z } from "zod";
import type { components } from "./schema";

/**
 * Runtime validators for API responses. Each is annotated with the generated
 * OpenAPI type so contract drift fails typechecking, and payloads are checked
 * at runtime because TypeScript types cannot validate the network.
 */
type Schemas = components["schemas"];

const minorUnits = z.string().regex(/^[1-9]\d{0,18}$/);
const aggregateMinorUnits = z.string().regex(/^(0|[1-9]\d*)$/);
export const currencySchema = z.enum(["USD", "EUR", "GBP", "JPY", "KWD"]);
export const paymentStatusSchema = z.enum([
  "created",
  "processing",
  "succeeded",
  "failed",
  "canceled",
  "unknown",
]);
export type PaymentStatus = z.infer<typeof paymentStatusSchema>;
export const terminalStatuses: readonly PaymentStatus[] = [
  "succeeded",
  "failed",
  "canceled",
];

export const paymentSchema: z.ZodType<Schemas["Payment"]> = z.object({
  id: z.string(),
  amount_minor: minorUnits,
  currency: currencySchema,
  payment_method_id: z.string(),
  merchant_reference: z.string().nullable(),
  status: paymentStatusSchema,
  created_at: z.string(),
  updated_at: z.string(),
});
export type Payment = Schemas["Payment"];

export const paymentPageSchema = z.object({
  items: z.array(paymentSchema),
  next_cursor: z.string().nullable(),
});
export type PaymentPage = z.infer<typeof paymentPageSchema>;

export const attemptSchema: z.ZodType<Schemas["Attempt"]> = z.object({
  id: z.string(),
  payment_id: z.string(),
  provider_id: z.string(),
  attempt_number: z.number().int(),
  status: z.enum(["created", "submitted", "succeeded", "failed", "unknown"]),
  routing_rule_id: z.string().nullable(),
  routing_reason: z.string(),
  provider_reference: z.string().nullable(),
  error_code: z.string().nullable(),
  submitted_at: z.string().nullable(),
  completed_at: z.string().nullable(),
  created_at: z.string(),
});
export type Attempt = Schemas["Attempt"];
export const attemptPageSchema = z.object({
  items: z.array(attemptSchema),
  next_cursor: z.string().nullable(),
});

export const paymentHistorySchema: z.ZodType<Schemas["PaymentHistory"]> =
  z.object({
    payment_id: z.string(),
    idempotency_key_masked: z.string().nullable(),
    transitions: z.array(
      z.object({
        from_status: paymentStatusSchema.nullable(),
        to_status: paymentStatusSchema,
        reason: z.string(),
        actor_reference: z.string(),
        created_at: z.string(),
      }),
    ),
    events: z.array(
      z.object({
        id: z.string(),
        event_type: z.string(),
        status: z.string(),
        attempts: z.number().int(),
        created_at: z.string(),
        delivered_at: z.string().nullable(),
      }),
    ),
  });
export type PaymentHistory = Schemas["PaymentHistory"];

const dataSourceSchema = z.enum(["observed", "simulated", "seeded"]);
const windowSchema = z.enum(["24h", "7d", "30d"]);
export type MetricWindow = z.infer<typeof windowSchema>;
const currencyVolumeSchema = z.object({
  currency: currencySchema,
  amount_minor: aggregateMinorUnits,
  payment_count: z.number().int(),
});

export const metricSummarySchema: z.ZodType<Schemas["MetricSummary"]> =
  z.object({
    data_source: dataSourceSchema,
    window: windowSchema,
    window_start: z.string(),
    window_end: z.string(),
    generated_at: z.string(),
    payment_count: z.number().int(),
    status_counts: z.record(z.string(), z.number().int()),
    volumes: z.array(currencyVolumeSchema),
    succeeded_volumes: z.array(currencyVolumeSchema),
    terminal_count: z.number().int(),
    success_rate_percent: z
      .string()
      .regex(/^\d{1,3}\.\d$/)
      .nullable(),
    completion_latency_p95_ms: z.number().int().nullable(),
    completion_latency_samples: z.number().int(),
  });
export type MetricSummary = Schemas["MetricSummary"];

export const paymentVolumeSchema = z.object({
  data_source: dataSourceSchema,
  window: windowSchema,
  bucket: z.enum(["hour", "day"]),
  window_start: z.string(),
  window_end: z.string(),
  generated_at: z.string(),
  items: z.array(
    z.object({
      timestamp: z.number().int(),
      currency: currencySchema,
      amount_minor: aggregateMinorUnits,
      payment_count: z.number().int(),
    }),
  ),
  next_cursor: z.string().nullable(),
});
export type PaymentVolume = z.infer<typeof paymentVolumeSchema>;

export const paymentMethodPageSchema = z.object({
  items: z.array(
    z.object({
      id: z.string(),
      rail: z.string(),
      brand: z.string().nullable(),
      last_four: z.string().nullable(),
    }),
  ),
  next_cursor: z.string().nullable(),
});
export type PaymentMethod = z.infer<
  typeof paymentMethodPageSchema
>["items"][number];

export const workspaceSchema = z.object({
  id: z.string(),
  name: z.string(),
  environment: z.string(),
});
export type Workspace = z.infer<typeof workspaceSchema>;

export const ledgerJournalPageSchema = z.object({
  items: z.array(
    z.object({
      id: z.string(),
      payment_id: z.string(),
      currency: currencySchema,
      status: z.literal("posted"),
      entries: z.array(
        z.object({
          account_id: z.string(),
          account_code: z.string().optional(),
          side: z.enum(["debit", "credit"]),
          amount_minor: minorUnits,
        }),
      ),
    }),
  ),
  next_cursor: z.string().nullable(),
});
export type LedgerJournalPage = z.infer<typeof ledgerJournalPageSchema>;
