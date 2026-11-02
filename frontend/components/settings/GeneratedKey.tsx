"use client";

import { Button } from "@/components/ui";
import { CopyCode } from "@/components/ui/copy-code";
import type { ApiKeyCreated } from "@/lib/api/apikey-schemas";

export function formatTime(value: string | null): string {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? value
    : date.toLocaleString(undefined, {
        dateStyle: "medium",
        timeStyle: "short",
      });
}

/** A ready curl call. Pass `$KEY` for the placeholder form (shell expands it). */
export function curlExample(secret: string): string {
  const origin =
    typeof window === "undefined"
      ? "http://localhost:3000"
      : window.location.origin;
  return [
    `curl -s ${origin}/v1/workspace \\`,
    `  -H "Authorization: Bearer ${secret}"`,
  ].join("\n");
}

/**
 * The one-time key panel shared by Settings and the dashboard home. The
 * secret is rendered only while `created` is set (component state of the
 * caller); `onHide` must clear it.
 */
export function GeneratedKey({
  created,
  onHide,
}: {
  created: ApiKeyCreated | undefined;
  onHide: () => void;
}) {
  return (
    <>
      <div aria-live="polite" className="sr-only">
        {created
          ? "API key generated. Copy it now; it is shown only once."
          : ""}
      </div>
      {created && (
        <section
          className="unknown-notice"
          aria-label="New API key"
          data-testid="new-api-key"
        >
          <h3>Copy your new key now</h3>
          <p>
            This is the only time the full key is shown. It is not stored and
            cannot be retrieved later; if you lose it, revoke it and generate
            another.
          </p>
          <CopyCode code={created.secret} label="New API key" noun="key" />
          <p className="small">
            {created.key.name} ·{" "}
            {created.key.role === "operator" ? "Operator" : "Read only"} ·
            expires {formatTime(created.key.expires_at)}
          </p>
          <p className="small">Try it:</p>
          <CopyCode
            code={curlExample(created.secret)}
            label="curl example using the new key"
          />
          <Button type="button" className="button-secondary" onClick={onHide}>
            I have copied it, hide the key
          </Button>
        </section>
      )}
    </>
  );
}
