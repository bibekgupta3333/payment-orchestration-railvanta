"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { ApiError, type ApiResult } from "@/lib/api/client";
import { ErrorState, Skeleton, StaleNotice } from ".";

function describe(error: unknown): { message: string; requestId: string } {
  if (error instanceof ApiError) {
    if (error.status === 401)
      return {
        message:
          "A valid sandbox session or API key is required to read workspace data.",
        requestId: error.requestId,
      };
    return { message: error.message, requestId: error.requestId };
  }
  return {
    message: "An unexpected error occurred while reading this view.",
    requestId: "unavailable",
  };
}

/**
 * Renders loading, read-error (with request ID and Retry) and stale states
 * for one API read, then the children with the current data.
 */
export function QueryState<T>({
  query,
  workspace,
  loadingLabel,
  lines = 3,
  children,
}: {
  query: UseQueryResult<ApiResult<T>, unknown>;
  workspace?: UseQueryResult<unknown, unknown>;
  loadingLabel: string;
  lines?: number;
  children: (result: ApiResult<T>) => ReactNode;
}) {
  if (workspace?.isError && !query.data) {
    const { message, requestId } = describe(workspace.error);
    return (
      <ErrorState
        message={message}
        requestId={requestId}
        retry={() => void workspace.refetch()}
      />
    );
  }
  if (query.isError && !query.data) {
    const { message, requestId } = describe(query.error);
    return (
      <ErrorState
        message={message}
        requestId={requestId}
        retry={() => void query.refetch()}
      />
    );
  }
  if (!query.data) return <Skeleton label={loadingLabel} lines={lines} />;
  const stale = query.isError ? describe(query.error) : null;
  return (
    <>
      {stale && (
        <StaleNotice
          updatedAt={query.dataUpdatedAt}
          requestId={stale.requestId}
          retry={() => void query.refetch()}
        />
      )}
      {children(query.data)}
    </>
  );
}
