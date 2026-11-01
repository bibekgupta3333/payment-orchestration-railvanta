import { z } from "zod";
import type { components } from "./schema";

type Schemas = components["schemas"];

const eventFields = {
  id: z.string(),
  source: z.enum(["inbox", "outbox"]),
  event_type: z.string(),
  status: z.enum(["pending", "processing", "processed", "delivered", "failed"]),
  outcome: z.enum(["applied", "ignored"]).nullable(),
  outcome_reason: z.string().nullable(),
  provider_event_id: z.string().nullable(),
  payment_id: z.string().nullable(),
  duplicate_count: z.number().int().nonnegative(),
  needs_investigation: z.boolean(),
  signature_status: z.enum(["not_applicable", "verified", "rejected"]),
  data_source: z.literal("simulated"),
  created_at: z.string(),
  last_received_at: z.string(),
};

export const eventSchema: z.ZodType<Schemas["Event"]> = z.object(eventFields);
export type EventItem = Schemas["Event"];

export const eventPageSchema: z.ZodType<Schemas["EventPage"]> = z.object({
  items: z.array(eventSchema),
  next_cursor: z.string().nullable(),
});
export type EventPage = Schemas["EventPage"];

export const eventDetailSchema: z.ZodType<Schemas["EventDetail"]> = z.object({
  ...eventFields,
  provider_id: z.string().nullable(),
  payload_hash: z.string().nullable(),
  conflicting_payload_hash: z.string().nullable(),
  conflict_count: z.number().int().nonnegative(),
  delivery_attempts: z.number().int().nonnegative(),
  processed_at: z.string().nullable(),
  payment_status: z.string().nullable(),
  attempts: z.array(
    z.object({
      id: z.string(),
      attempt_number: z.number().int(),
      provider_id: z.string(),
      status: z.string(),
    }),
  ),
  payload_summary: z.object({
    fields: z.array(z.object({ name: z.string(), value: z.string() })),
    redacted_count: z.number().int().nonnegative(),
  }),
});
export type EventDetail = Schemas["EventDetail"];

export const settingsSchema: z.ZodType<Schemas["Settings"]> = z.object({
  workspace: z.object({
    id: z.string(),
    name: z.string(),
    environment: z.literal("sandbox"),
  }),
  role: z.enum(["read", "operator"]),
  providers: z.array(
    z.object({
      code: z.enum(["mock", "stripe", "plaid"]),
      label: z.string(),
      status: z.enum(["enabled", "disabled", "not_configured"]),
    }),
  ),
  capabilities: z.object({
    api_key_management: z.literal("unavailable"),
    role_management: z.literal("unavailable"),
    live_activation: z.literal("unavailable"),
  }),
});
export type Settings = Schemas["Settings"];

export const signatureLabels = {
  not_applicable: "Not applicable",
  verified: "Verified",
  rejected: "Rejected",
} as const;
