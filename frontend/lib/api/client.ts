import { z } from "zod";

export const errorEnvelopeSchema = z.object({
  code: z.string(),
  message: z.string(),
  request_id: z.string(),
  field_errors: z.array(z.object({ field: z.string(), message: z.string() })),
});

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly details: z.infer<typeof errorEnvelopeSchema>,
  ) {
    super(details.message);
    this.name = "ApiError";
  }
}

/** No implicit retries: a timed-out mutation can already have taken effect. */
export async function apiRequest<T>(
  path: `/v1/${string}`,
  schema: z.ZodType<T>,
  options: RequestInit = {},
): Promise<T> {
  if (!path.startsWith("/v1/") || path.includes("\\"))
    throw new Error("Invalid API path");
  const url = new URL(path, "https://railvanta.invalid");
  if (
    url.origin !== "https://railvanta.invalid" ||
    !url.pathname.startsWith("/v1/")
  ) {
    throw new Error("Invalid API path");
  }
  const headers = new Headers(options.headers);
  if (!headers.has("Accept")) headers.set("Accept", "application/json");
  const response = await fetch(path, {
    ...options,
    credentials: "same-origin",
    cache: "no-store",
    headers,
  });
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
            request_id: response.headers.get("x-request-id") ?? "unavailable",
            field_errors: [],
          },
    );
  }
  return schema.parse(payload);
}

export function shouldRetryRead(failureCount: number, error: unknown): boolean {
  if (
    error instanceof ApiError &&
    [400, 401, 403, 404, 409, 422].includes(error.status)
  )
    return false;
  return failureCount < 2;
}
