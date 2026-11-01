"use client";

import { Button } from "@/components/ui";

export const START_SANDBOX_EVENT = "railvanta:start-sandbox";

/**
 * Hero call to action: scrolls to the API panel and asks it to start. When
 * public access is off the panel shows why instead of doing anything.
 */
export function StartSandboxButton() {
  return (
    <Button
      type="button"
      onClick={() => {
        document
          .getElementById("api-example-title")
          ?.scrollIntoView?.({ behavior: "smooth", block: "start" });
        window.dispatchEvent(new Event(START_SANDBOX_EVENT));
      }}
    >
      Start sandbox <span aria-hidden="true">→</span>
    </Button>
  );
}
