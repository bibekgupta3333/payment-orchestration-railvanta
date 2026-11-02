import { z } from "zod";

/**
 * GET /v1/sandbox/samples: the latest run of each startup sample scenario
 * (seed `sample-<scenario>`). Hand-written rather than derived from the
 * generated OpenAPI types so the panel does not depend on regeneration.
 */
export const sandboxSampleSchema = z.object({
  scenario: z.string(),
  run_id: z.string().nullable(),
  status: z.enum(["completed", "running", "failed", "missing"]),
  payment_id: z.string().nullable(),
  invariants_passed: z.number().int().nonnegative(),
  invariants_total: z.number().int().nonnegative(),
});
export type SandboxSample = z.infer<typeof sandboxSampleSchema>;

export const sandboxSamplesSchema = z.object({
  items: z.array(sandboxSampleSchema),
});
export type SandboxSamples = z.infer<typeof sandboxSamplesSchema>;
