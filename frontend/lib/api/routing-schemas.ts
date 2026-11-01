import { z } from "zod";
import type { components } from "./schema";
import { attemptSchema, paymentSchema } from "./schemas";

/**
 * Validators for the routing, provider and simulator reads (WBS 3.2). Each is
 * annotated with the generated OpenAPI type so contract drift fails
 * typechecking.
 */
type Schemas = components["schemas"];

export const providerCodeSchema = z.enum(["mock", "stripe", "plaid"]);

export const routingRuleSchema: z.ZodType<Schemas["RoutingRule"]> = z.object({
  id: z.string(),
  provider_id: z.string(),
  provider_code: z.string(),
  rail: z.string(),
  currency: z.string(),
  priority: z.number().int(),
  min_amount_minor: z.string(),
  max_amount_minor: z.string().nullable(),
  enabled: z.boolean(),
});
export type RoutingRule = Schemas["RoutingRule"];

export const routingRulePageSchema = z.object({
  items: z.array(routingRuleSchema),
  next_cursor: z.string().nullable(),
});

export const routingCheckSchema: z.ZodType<Schemas["RoutingCheck"]> = z.object({
  id: z.string(),
  label: z.string(),
  passed: z.boolean(),
  detail: z.string(),
});

export const routingDecisionSchema: z.ZodType<Schemas["RoutingDecision"]> =
  z.object({
    provider_id: z.string(),
    provider_code: providerCodeSchema,
    rule_id: z.string().nullable(),
    priority: z.number().int().nullable(),
    reason: z.string(),
    rule: routingRuleSchema.nullable(),
    checks: z.array(routingCheckSchema),
    candidates: z.array(
      z.object({
        provider_id: z.string(),
        provider_code: z.string(),
        rule_id: z.string(),
        priority: z.number().int(),
      }),
    ),
    failover_blocked: z.boolean(),
  });
export type RoutingDecision = Schemas["RoutingDecision"];

export const providerSchema: z.ZodType<Schemas["Provider"]> = z.object({
  id: z.string(),
  code: providerCodeSchema,
  display_name: z.string(),
  mode: z.enum(["simulated", "configured", "not_configured"]),
  enabled: z.boolean(),
  configured: z.boolean(),
  capabilities: z
    .object({
      idempotent_replay: z.boolean(),
      lookup_by_idempotency_key: z.boolean(),
      rails: z.array(z.string()),
    })
    .nullable(),
  rule_count: z.number().int(),
});
export type Provider = Schemas["Provider"];

export const providerPageSchema = z.object({
  items: z.array(providerSchema),
  next_cursor: z.string().nullable(),
});

export const healthStatusSchema = z.enum([
  "no_observations",
  "insufficient_data",
  "healthy",
  "degraded",
  "unhealthy",
]);

export const providerHealthSchema: z.ZodType<Schemas["ProviderHealth"]> =
  z.object({
    id: z.string(),
    code: providerCodeSchema,
    configured: z.boolean(),
    data_source: z.enum(["observed", "simulated", "seeded"]),
    observation_count: z.number().int(),
    status: healthStatusSchema,
    windows: z.array(
      z.object({
        window: z.enum(["1h", "24h", "7d"]),
        observations: z.number().int(),
        succeeded: z.number().int(),
        failed: z.number().int(),
        unknown: z.number().int(),
        success_rate_percent: z.string().nullable(),
        p95_ms: z.number().int().nullable(),
        latency_samples: z.number().int(),
        error_counts: z.record(z.string(), z.number().int()),
      }),
    ),
  });
export type ProviderHealth = Schemas["ProviderHealth"];

export const providerHealthPageSchema = z.object({
  items: z.array(providerHealthSchema),
  next_cursor: z.string().nullable(),
});

export const providerDetailSchema = z.object({
  provider: providerSchema,
  health: providerHealthSchema,
  rules: z.array(routingRuleSchema),
});

export const scenarioCatalogSchema = z.object({
  label: z.string(),
  items: z.array(
    z.object({
      id: z.string(),
      label: z.string(),
      description: z.string(),
      implemented: z.boolean(),
      missing_dependency: z.string().nullable(),
      injected_fault: z.string(),
    }),
  ),
});
export type ScenarioCatalog = z.infer<typeof scenarioCatalogSchema>;

export const invariantSchema = z.object({
  id: z.string(),
  title: z.string(),
  status: z.enum(["pending", "pass", "fail"]),
  evidence: z.string(),
});
export type Invariant = z.infer<typeof invariantSchema>;

const journalSchema = z.object({
  id: z.string(),
  payment_id: z.string().nullable(),
  currency: z.string(),
  status: z.string(),
  posting_key: z.string(),
  reversal_of: z.string().nullable(),
  created_at: z.string(),
  posted_at: z.string(),
  entries: z.array(
    z.object({
      id: z.string(),
      account_id: z.string(),
      account_code: z.string(),
      provider_id: z.string().nullable(),
      side: z.enum(["debit", "credit"]),
      amount_minor: z.string(),
    }),
  ),
});

export const simulationSchema = z.object({
  id: z.string(),
  label: z.string(),
  scenario: z.string(),
  seed: z.string(),
  status: z.enum(["running", "completed", "failed"]),
  payment_id: z.string().nullable(),
  injected_fault: z.string(),
  created_at: z.string(),
  completed_at: z.string().nullable(),
  failure_reason: z.string().nullable(),
  fingerprint: z.string().nullable(),
  request: z.object({
    payment_method_id: z.string(),
    amount_minor: z.string(),
    currency: z.string(),
    idempotency_key: z.string(),
    merchant_reference: z.string(),
  }),
  response: z
    .object({
      status_code: z.number().int(),
      body: z.record(z.string(), z.unknown()),
    })
    .nullable(),
  payment: paymentSchema.nullable(),
  routing: z.object({
    failover_blocked: z.boolean(),
    failover_reason: z.string().nullable(),
    decisions: z.array(
      z.object({
        attempt_number: z.number().int(),
        provider_id: z.string(),
        rule_id: z.string().nullable(),
        reason: z.string(),
      }),
    ),
  }),
  attempts: z.array(attemptSchema),
  transitions: z.array(
    z.object({
      from_status: z.string().nullable(),
      to_status: z.string(),
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
  journals: z.array(journalSchema),
  invariants: z.array(invariantSchema),
  timeline: z.array(
    z.object({
      at: z.string(),
      source: z.string(),
      title: z.string(),
      detail: z.string(),
    }),
  ),
});
export type Simulation = z.infer<typeof simulationSchema>;
