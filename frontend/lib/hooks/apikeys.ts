"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  apiRequestWithMeta,
  type ApiError,
  type ApiResult,
} from "@/lib/api/client";
import {
  apiKeyCreatedSchema,
  apiKeyListSchema,
  apiKeySchema,
  type ApiKey,
  type ApiKeyCreated,
  type CreateApiKeyInput,
} from "@/lib/api/apikey-schemas";
import { scopedKey, useScopedRead, useWorkspace } from "@/lib/hooks/payments";

/** Key metadata for the workspace. Only operators may list; others get a 403. */
export function useApiKeys(enabled: boolean) {
  return useScopedRead(["api-keys"], "/v1/api-keys", apiKeyListSchema, {
    enabled,
  });
}

/**
 * Mints a key. The secret lives only in this mutation's result (component
 * state): it is never written to the query cache, storage or the URL, and the
 * panel clears it with `reset()`. No retry: a retried POST could mint a second
 * key whose secret the user would never see.
 */
export function useCreateApiKey() {
  const client = useQueryClient();
  const workspace = useWorkspace();
  return useMutation<ApiKeyCreated, ApiError, CreateApiKeyInput>({
    retry: false,
    mutationFn: async (input) => {
      const result = await apiRequestWithMeta(
        "/v1/api-keys",
        apiKeyCreatedSchema,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(input),
        },
      );
      return result.data;
    },
    onSuccess: () => {
      const scope = workspace.data?.data;
      void client.invalidateQueries({
        queryKey: scopedKey(scope, "api-keys"),
      });
    },
  });
}

export function useRevokeApiKey() {
  const client = useQueryClient();
  const workspace = useWorkspace();
  return useMutation<ApiResult<ApiKey>, ApiError, string>({
    retry: false,
    mutationFn: (id) =>
      apiRequestWithMeta(
        `/v1/api-keys/${encodeURIComponent(id)}`,
        apiKeySchema,
        { method: "DELETE" },
      ),
    onSuccess: () => {
      const scope = workspace.data?.data;
      void client.invalidateQueries({
        queryKey: scopedKey(scope, "api-keys"),
      });
    },
  });
}
