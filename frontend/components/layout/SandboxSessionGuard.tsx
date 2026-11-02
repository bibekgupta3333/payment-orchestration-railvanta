"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState, type ReactNode } from "react";
import { z } from "zod";
import { Skeleton } from "@/components/ui";
import { ApiError, apiRequest } from "@/lib/api/client";
import { workspaceSchema } from "@/lib/api/schemas";

const statusSchema = z.object({ public_access: z.boolean() });
const sessionSchema = z.object({ role: z.string() });

type Phase = "checking" | "starting" | "ready";

/**
 * Opens the dashboard without a dead end: when the browser has no valid
 * session (401) and the API reports public sandbox access (ADR 0013), it starts
 * a visitor session in the demo workspace and then renders the page. When
 * public access is off, or anything else fails, the page renders as before and
 * shows its own error.
 */
export function SandboxSessionGuard({ children }: { children: ReactNode }) {
  const client = useQueryClient();
  const [phase, setPhase] = useState<Phase>("checking");

  useEffect(() => {
    const controller = new AbortController();
    const { signal } = controller;
    (async () => {
      try {
        await apiRequest("/v1/workspace", workspaceSchema, { signal });
      } catch (error) {
        if (signal.aborted) return;
        if (error instanceof ApiError && error.status === 401) {
          try {
            const status = await apiRequest(
              "/v1/sandbox/status",
              statusSchema,
              { signal },
            );
            if (status.public_access) {
              setPhase("starting");
              await apiRequest("/v1/sandbox/session", sessionSchema, {
                method: "POST",
                signal,
              });
              await client.invalidateQueries({ queryKey: ["railvanta"] });
            }
          } catch {
            // Fall through: the page shows its own error.
          }
        }
      }
      if (!signal.aborted) setPhase("ready");
    })();
    return () => controller.abort();
  }, [client]);

  if (phase === "ready") return <>{children}</>;
  return (
    <Skeleton
      lines={3}
      label={
        phase === "starting"
          ? "Starting your sandbox session…"
          : "Checking your sandbox session…"
      }
    />
  );
}
