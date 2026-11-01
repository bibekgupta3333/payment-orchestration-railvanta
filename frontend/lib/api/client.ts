import { z } from "zod";

export const errorEnvelopeSchema = z.object({
  code: z.string(),
  message: z.string(),
  request_id: z.string(),
  field_errors: z.array(z.object({ field: z.string(), message: z.string() })),
});
export type ErrorEnvelope = z.infer<typeof errorEnvelopeSchema>;

/**
 * status 0 means no HTTP response arrived (offline, timeout, or connection
 * reset). For a write that does NOT establish whether it took effect.
 */
export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly details: ErrorEnvelope,
  ) {
    super(details.message);
    this.name = "ApiError";
  }
  get requestId(): string {
    return this.details.request_id;
  }
  /** True when the request may or may not have reached the API. */
  get transportUncertain(): boolean {
    return this.status === 0 || this.status === 502 || this.status === 504;
  }
}

export interface ApiResult<T> {
  data: T;
  status: number;
  requestId: string;
  correlationId: string | null;
  headers: Headers;
  receivedAt: number;
}

export interface ApiOptions extends RequestInit {
  /** Abort after this many milliseconds; the outcome is then uncertain. */
  timeoutMs?: number;
}

/** A client-generated ID that satisfies the API's ^[A-Za-z0-9_-]{1,64}$ rule. */
export function newRequestId(prefix = "web"): string {
  const random =
    typeof crypto !== "undefined" && "randomUUID" in crypto
      ? crypto.randomUUID().replace(/-/g, "")
      : Math.random().toString(36).slice(2);
  return `${prefix}_${random}`.slice(0, 64);
}

/** Readable CSRF token for cookie-authenticated writes (never the session). */
export function readCsrfToken(): string | null {
  if (typeof document === "undefined") return null;
  for (const part of document.cookie.split(";")) {
    const [name, ...rest] = part.trim().split("=");
    if (name === "railvanta_csrf") return decodeURIComponent(rest.join("="));
  }
  return null;
}

function assertApiPath(path: string): void {
  if (!path.startsWith("/v1/") || path.includes("\\"))
    throw new Error("Invalid API path");
  const url = new URL(path, "https://railvanta.invalid");
  if (
    url.origin !== "https://railvanta.invalid" ||
    !url.pathname.startsWith("/v1/")
  ) {
    throw new Error("Invalid API path");
  }
}

/** No implicit retries: a timed-out mutation can already have taken effect. */
export async function apiRequestWithMeta<T>(
  path: `/v1/${string}`,
  schema: z.ZodType<T>,
  options: ApiOptions = {},
): Promise<ApiResult<T>> {
  assertApiPath(path);
  const { timeoutMs = 15_000, ...init } = options;
  const headers = new Headers(init.headers);
  if (!headers.has("Accept")) headers.set("Accept", "application/json");
  if (!headers.has("X-Request-ID")) headers.set("X-Request-ID", newRequestId());
  const method = (init.method ?? "GET").toUpperCase();
  if (!["GET", "HEAD", "OPTIONS"].includes(method)) {
    const csrf = readCsrfToken();
    if (csrf && !headers.has("X-CSRF-Token")) headers.set("X-CSRF-Token", csrf);
  }
  const sentRequestId = headers.get("X-Request-ID") ?? "unavailable";
  const signals = [AbortSignal.timeout(timeoutMs)];
  if (init.signal) signals.push(init.signal);
  let response: Response;
  try {
    response = await fetch(path, {
      ...init,
      method,
      credentials: "same-origin",
      cache: "no-store",
      headers,
      signal: AbortSignal.any(signals),
    });
  } catch (error) {
    if (init.signal?.aborted) throw error;
    throw new ApiError(0, {
      code: "network_error",
      message:
        "The API could not be reached. The request outcome is not known from this response.",
      request_id: sentRequestId,
      field_errors: [],
    });
  }
  const requestId = response.headers.get("x-request-id") ?? sentRequestId;
  const payload: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    const parsed = errorEnvelopeSchema.safeParse(payload);
    throw new ApiError(
      response.status,
      parsed.success
        ? parsed.data
        : {
            code: "invalid_error_response",
            message: "The service returned an unexpected response.",
            request_id: requestId,
            field_errors: [],
          },
    );
  }
  const parsed = schema.safeParse(payload);
  if (!parsed.success) {
    throw new ApiError(response.status, {
      code: "invalid_response",
      message: "The API response did not match the expected contract.",
      request_id: requestId,
      field_errors: [],
    });
  }
  return {
    data: parsed.data,
    status: response.status,
    requestId,
    correlationId: response.headers.get("x-correlation-id"),
    headers: response.headers,
    receivedAt: Date.now(),
  };
}

export async function apiRequest<T>(
  path: `/v1/${string}`,
  schema: z.ZodType<T>,
  options: ApiOptions = {},
): Promise<T> {
  return (await apiRequestWithMeta(path, schema, options)).data;
}

/** Bounded retries for safe reads; never for auth, validation or missing data. */
export function shouldRetryRead(failureCount: number, error: unknown): boolean {
  if (
    error instanceof ApiError &&
    [400, 401, 403, 404, 409, 422, 501].includes(error.status)
  )
    return false;
  return failureCount < 2;
}
