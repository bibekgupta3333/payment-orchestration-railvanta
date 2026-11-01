"use client";

import { useQuery } from "@tanstack/react-query";
import type { z } from "zod";
import {
  apiRequestWithMeta,
  shouldRetryRead,
  type ApiResult,
} from "@/lib/api/client";
import {
  eventDetailSchema,
  eventPageSchema,
  settingsSchema,
} from "@/lib/api/event-schemas";
import { useWorkspace } from "@/lib/hooks/payments";

export interface EventFilters {
  q?: string;
  state?: string;
  source?: string;
  created_from?: string;
  created_to?: string;
  cursor?: string;
  limit?: string;
}

export const eventFilterNames: (keyof EventFilters)[] = [
  "q",
  "state",
  "source",
  "created_from",
  "created_to",
  "cursor",
  "limit",
];

export function eventQueryString(filters: EventFilters): string {
  const params = new URLSearchParams();
  for (const name of eventFilterNames) {
    const value = filters[name];
    if (value) params.set(name, value);
  }
  const text = params.toString();
  return text ? `?${text}` : "";
}

/** A workspace-scoped read: waits for the workspace so caches never mix scopes. */
function useScoped<T>(
  parts: unknown[],
  path: `/v1/${string}`,
  schema: z.ZodType<T>,
) {
  const workspace = useWorkspace();
  const scope = workspace.data?.data;
  const query = useQuery<ApiResult<T>>({
    queryKey: [
      "railvanta",
      scope?.id ?? "none",
      scope?.environment ?? "none",
      ...parts,
    ],
    queryFn: ({ signal }) => apiRequestWithMeta(path, schema, { signal }),
    enabled: Boolean(scope),
    retry: shouldRetryRead,
    refetchIntervalInBackground: false,
  });
  return { query, workspace, scope };
}

export function useEvents(filters: EventFilters) {
  return useScoped(
    ["events", "list", filters],
    `/v1/events${eventQueryString(filters)}`,
    eventPageSchema,
  );
}

export function useEvent(id: string) {
  return useScoped(
    ["events", "detail", id],
    `/v1/events/${encodeURIComponent(id)}`,
    eventDetailSchema,
  );
}

export function useSettings() {
  return useScoped(["settings"], "/v1/settings", settingsSchema);
}
