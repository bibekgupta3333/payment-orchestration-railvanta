import type { Metadata } from "next";
import Link from "next/link";
import { Badge } from "@/components/ui";
import { SwaggerReference } from "@/components/api-reference/SwaggerReference";

export const metadata: Metadata = { title: "API reference" };

export default function Page() {
  return (
    <>
      <div className="page-heading">
        <p className="eyebrow">Developers</p>
        <h1>API reference</h1>
        <p>
          The interactive OpenAPI reference for the Railvanta sandbox API,
          served from the same contract the backend and these docs are checked
          against.
        </p>
      </div>
      <section
        aria-labelledby="reference-help-heading"
        className="reference-help"
      >
        <h2 id="reference-help-heading" className="sr-only">
          How to try requests
        </h2>
        <p>
          <Badge>Sandbox</Badge>{" "}
          <strong>
            Generate a sandbox API key in{" "}
            <Link href="/dashboard/settings#api-keys">Settings</Link>, click
            Authorize, paste it, then Try it out. Sandbox only; no real funds.
          </strong>
        </p>
        <p>
          Requests go to this origin through the same-origin <code>/v1</code>{" "}
          proxy. Your signed-in session also works for reads, but writes with
          the session cookie need a CSRF header, so bearer keys are the
          supported way to send writes from here.
        </p>
      </section>
      <SwaggerReference />
    </>
  );
}
