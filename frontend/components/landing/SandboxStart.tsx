"use client";

import Link from "next/link";
import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { z } from "zod";
import { Button } from "@/components/ui";
import { CopyCode } from "@/components/ui/copy-code";
import { ApiError, apiRequest, newRequestId } from "@/lib/api/client";
import {
  apiKeyCreatedSchema,
  type ApiKeyCreated,
} from "@/lib/api/apikey-schemas";
import { paymentMethodPageSchema } from "@/lib/api/schemas";
import { START_SANDBOX_EVENT } from "./StartSandboxButton";

const statusSchema = z.object({
  public_access: z.boolean(),
  workspace_name: z.string().optional(),
  session_ttl_hours: z.number().optional(),
});
const sessionSchema = z.object({
  workspace: z.object({
    id: z.string(),
    name: z.string(),
    environment: z.string(),
  }),
  role: z.string(),
  expires_at: z.string(),
});

export const OFF_MESSAGE =
  "Public sandbox access is off. Start the stack with pnpm demo:up (it turns it on) and open the dashboard with pnpm demo:open";

type Access = "checking" | "on" | "off";
type Phase = "idle" | "starting" | "started" | "error";

interface Started {
  created: ApiKeyCreated;
  methodId: string;
  idempotencyKey: string;
}
interface RunResult {
  status: number;
  body: string;
  replay: boolean;
}

function requestBody(methodId: string, reference: string) {
  return {
    amount_minor: "25000",
    currency: "USD",
    payment_method_id: methodId,
    merchant_reference: reference,
  };
}

/** The curl for the same request "Run it" sends (bearer key, not the cookie). */
export function buildCurl(
  origin: string,
  key: string,
  methodId: string,
  idempotencyKey: string,
) {
  return [
    `curl -X POST ${origin}/v1/payments \\`,
    `  -H "Authorization: Bearer ${key}" \\`,
    `  -H "Content-Type: application/json" \\`,
    `  -H "Idempotency-Key: ${idempotencyKey}" \\`,
    `  -d '${JSON.stringify(requestBody(methodId, idempotencyKey), null, 2).replace(/\n/g, "\n  ")}'`,
  ].join("\n");
}

const PLACEHOLDER_KEY = "<your sandbox API key>";
const PLACEHOLDER_METHOD = "<sandbox payment method ID>";

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.details.code === "sandbox_access_disabled") return OFF_MESSAGE;
    return `${error.message} (request ${error.requestId})`;
  }
  return "Something went wrong starting the sandbox. Nothing was charged; no real funds are involved.";
}

export function SandboxStart() {
  const [access, setAccess] = useState<Access>("checking");
  const [phase, setPhase] = useState<Phase>("idle");
  const [error, setError] = useState("");
  const [started, setStarted] = useState<Started | undefined>(undefined);
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<RunResult | undefined>(undefined);
  const origin = useSyncExternalStore(
    () => () => undefined,
    () => window.location.origin,
    () => "http://localhost:3000",
  );

  useEffect(() => {
    const controller = new AbortController();
    apiRequest("/v1/sandbox/status", statusSchema, {
      signal: controller.signal,
    })
      .then((status) => setAccess(status.public_access ? "on" : "off"))
      .catch(() => {
        if (!controller.signal.aborted) setAccess("off");
      });
    return () => controller.abort();
  }, []);

  const requested = useRef<() => void>(() => undefined);
  useEffect(() => {
    const onRequest = () => requested.current();
    window.addEventListener(START_SANDBOX_EVENT, onRequest);
    return () => window.removeEventListener(START_SANDBOX_EVENT, onRequest);
  }, []);
  useEffect(() => {
    requested.current = () => {
      if (access === "on" && (phase === "idle" || phase === "error"))
        void start();
    };
  });

  async function start() {
    setPhase("starting");
    setError("");
    setResult(undefined);
    try {
      await apiRequest("/v1/sandbox/session", sessionSchema, {
        method: "POST",
      });
      const created = await apiRequest("/v1/api-keys", apiKeyCreatedSchema, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: "Landing demo key",
          role: "operator",
          expires_in_hours: 24,
        }),
      });
      const methods = await apiRequest(
        "/v1/payment-methods",
        paymentMethodPageSchema,
      );
      const method =
        methods.items.find((item) => item.last_four === "0121") ??
        methods.items[0];
      if (!method) throw new Error("no sandbox payment method");
      setStarted({
        created,
        methodId: method.id,
        idempotencyKey: newRequestId("landing").replace(/_/g, "-"),
      });
      setPhase("started");
    } catch (failure) {
      setError(errorMessage(failure));
      setPhase("error");
    }
  }

  async function run() {
    if (!started) return;
    const { created, methodId, idempotencyKey } = started;
    setRunning(true);
    const replay = result !== undefined;
    try {
      // The bearer key, never the session cookie, authenticates this call.
      const response = await fetch("/v1/payments", {
        method: "POST",
        credentials: "omit",
        cache: "no-store",
        headers: {
          Authorization: `Bearer ${created.secret}`,
          "Content-Type": "application/json",
          "Idempotency-Key": idempotencyKey,
        },
        body: JSON.stringify(requestBody(methodId, idempotencyKey)),
      });
      const payload: unknown = await response.json().catch(() => null);
      setResult({
        status: response.status,
        body: JSON.stringify(payload, null, 2),
        replay,
      });
    } catch {
      setResult({
        status: 0,
        body: "No response arrived. The outcome is unknown; resend with the same Idempotency-Key.",
        replay,
      });
    } finally {
      setRunning(false);
    }
  }

  const secret = started?.created.secret;
  const curl = buildCurl(
    origin,
    secret ?? PLACEHOLDER_KEY,
    started?.methodId ?? PLACEHOLDER_METHOD,
    started?.idempotencyKey ?? "order-1042-attempt",
  );

  return (
    <section className="api-example" aria-labelledby="api-example-title">
      <div className="section-title">
        <span className="eyebrow">API</span>
        <span className="text-label">Sandbox · no real funds</span>
      </div>
      <h2 id="api-example-title">Call the API in one step</h2>
      <p className="muted">
        One click starts a sandbox session, generates an API key and a ready
        curl request using a simulated payment method. No card or bank
        credentials are involved.
      </p>
      {access === "off" && (
        <p className="unknown-notice" role="status" data-testid="sandbox-off">
          {OFF_MESSAGE}
        </p>
      )}
      {access === "checking" && (
        <p className="muted" role="status">
          Checking sandbox access...
        </p>
      )}
      <div className="hero-actions">
        <Button
          type="button"
          disabled={access !== "on" || phase === "starting" || Boolean(secret)}
          onClick={() => void start()}
        >
          {phase === "starting"
            ? "Starting sandbox..."
            : phase === "error"
              ? "Try again"
              : "Start sandbox"}
        </Button>
        {phase === "started" && (
          <>
            <Link className="button button-quiet" href="/dashboard">
              Open the dashboard
            </Link>
            <Link
              className="button button-quiet"
              href="/dashboard/api-reference"
            >
              API reference
            </Link>
          </>
        )}
      </div>
      {phase === "error" && (
        <p className="unknown-notice" role="alert">
          {error}
        </p>
      )}
      {secret && started && (
        <section
          className="unknown-notice"
          aria-label="New API key"
          data-testid="new-api-key"
        >
          <h3>Copy your new key now</h3>
          <p>
            This is the only time the full key is shown. It lives in this page
            only, is never stored, and expires in 24 hours.
          </p>
          <CopyCode code={secret} label="New API key" noun="key" />
          <Button
            type="button"
            className="button-quiet"
            onClick={() =>
              setStarted(
                (value) =>
                  value && {
                    ...value,
                    created: { ...value.created, secret: "" },
                  },
              )
            }
          >
            I have copied it, hide the key
          </Button>
        </section>
      )}
      <CopyCode
        code={curl}
        label="API example: create a sandbox payment"
        noun="curl"
      />
      {phase === "started" && (
        <div className="landing-run">
          <div className="hero-actions">
            <Button
              type="button"
              disabled={running || !secret}
              onClick={() => void run()}
            >
              {running ? "Running..." : "Run it"}
            </Button>
            <Button
              type="button"
              className="button-quiet"
              disabled={running || !secret || !result}
              onClick={() => void run()}
            >
              Run it again (same Idempotency-Key)
            </Button>
          </div>
          {!secret && (
            <p className="muted">
              The key is hidden; generate a new one to run the example again.
            </p>
          )}
          <div role="status" aria-live="polite">
            {result && (
              <div data-testid="run-result">
                <p>
                  <strong>HTTP {result.status || "no response"}</strong>
                  {result.replay &&
                    " · same Idempotency-Key: the stored response is replayed, no second payment"}
                </p>
                <pre tabIndex={0} aria-label="Response body">
                  <code>{result.body}</code>
                </pre>
              </div>
            )}
          </div>
        </div>
      )}
    </section>
  );
}
