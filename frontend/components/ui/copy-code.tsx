"use client";

import { useState } from "react";
import { Button } from ".";

export function CopyCode({ code, label }: { code: string; label: string }) {
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
          {status === "copied" ? "Copied" : "Copy example"}
        </Button>
      </div>
      <pre tabIndex={0} aria-label={label}>
        <code>{code}</code>
      </pre>
      <span className="sr-only" aria-live="polite">
        {status === "copied"
          ? "Example copied to clipboard"
          : status === "failed"
            ? "Copy failed; select the text instead"
            : ""}
      </span>
    </div>
  );
}
