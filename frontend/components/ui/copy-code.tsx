"use client";

import { useState } from "react";
import { Button } from ".";

export function CopyCode({
  code,
  label,
  noun = "example",
}: {
  code: string;
  label: string;
  /** What the button copies, e.g. "key"; shown as "Copy key". */
  noun?: string;
}) {
  const [status, setStatus] = useState<"idle" | "copied" | "failed">("idle");
  return (
    <div className="code-block">
      <div className="code-toolbar">
        <span className="text-label">{label}</span>
        <Button
          className="button-quiet button-small"
          onClick={async () => {
            try {
              await navigator.clipboard.writeText(code);
              setStatus("copied");
            } catch {
              setStatus("failed");
            }
          }}
        >
          {status === "copied" ? "Copied" : `Copy ${noun}`}
        </Button>
      </div>
      <pre tabIndex={0} aria-label={label}>
        <code>{code}</code>
      </pre>
      <span className="sr-only" aria-live="polite">
        {status === "copied"
          ? `${noun.charAt(0).toUpperCase()}${noun.slice(1)} copied to clipboard`
          : status === "failed"
            ? "Copy failed; select the text instead"
            : ""}
      </span>
    </div>
  );
}
