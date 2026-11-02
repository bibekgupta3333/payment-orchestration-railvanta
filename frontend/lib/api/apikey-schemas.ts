import { z } from "zod";
import type { components } from "./schema";

type Schemas = components["schemas"];

export const apiKeySchema: z.ZodType<Schemas["APIKey"]> = z.object({
  id: z.string(),
  name: z.string(),
  prefix: z.string(),
  role: z.enum(["read", "operator"]),
  created_at: z.string(),
  expires_at: z.string().nullable(),
  revoked_at: z.string().nullable(),
  status: z.enum(["active", "expired", "revoked"]),
});
export type ApiKey = Schemas["APIKey"];

export const apiKeyCreatedSchema: z.ZodType<Schemas["APIKeyCreated"]> =
  z.object({
    key: apiKeySchema,
    secret: z.string(),
  });
export type ApiKeyCreated = Schemas["APIKeyCreated"];

export const apiKeyListSchema: z.ZodType<Schemas["APIKeyList"]> = z.object({
  items: z.array(apiKeySchema),
  limit: z.number().int(),
});
export type ApiKeyList = Schemas["APIKeyList"];

export interface CreateApiKeyInput {
  name: string;
  role: "read" | "operator";
  expires_in_hours: number;
}
