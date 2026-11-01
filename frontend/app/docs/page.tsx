import type { Metadata } from "next";
import Link from "next/link";
import { ExampleBlock } from "@/components/docs/ExampleBlock";
import { Badge, Table } from "@/components/ui";
import { baseUrlVariable, docsSource, examplesIn } from "@/lib/docs/snippets";

export const metadata: Metadata = { title: "API documentation" };

const sections = [
  { id: "getting-started", title: "Getting started" },
  { id: "authentication", title: "Authentication" },
  { id: "payments", title: "Payments" },
  { id: "idempotency", title: "Idempotency and retry safety" },
  { id: "webhooks", title: "Events and webhooks" },
  { id: "routing", title: "Routing" },
  { id: "errors", title: "Errors" },
  { id: "limitations", title: "Supported and planned" },
] as const;

function Examples({ section }: { section: string }) {
  return (
    <>
      {examplesIn(section).map((example) => (
        <ExampleBlock key={example.id} example={example} />
      ))}
    </>
  );
}

export default function DocsPage() {
  return (
    <div className="public-page">
      <header className="public-header">
        <Link href="/" className="brand">
          ◇ Railvanta
        </Link>
        <nav aria-label="Public navigation">
          <Link href="/dashboard">Dashboard</Link>
          <Badge>Sandbox</Badge>
        </nav>
      </header>
      <div className="docs-layout">
        <details className="docs-nav" open>
          <summary>On this page</summary>
          <nav aria-label="Documentation sections">
            <ol>
              {sections.map((section) => (
                <li key={section.id}>
                  <a href={`#${section.id}`}>{section.title}</a>
                </li>
              ))}
            </ol>
          </nav>
        </details>
        <main id="main-content" className="docs-content" tabIndex={-1}>
          <p className="eyebrow">Developer documentation</p>
          <h1>Railvanta sandbox API</h1>
          <p>
            Every example below is executed against the live sandbox API in CI
            and validated against the OpenAPI contract in{" "}
            <code className="mono">backend/api/openapi.yaml</code>. All values
            are sandbox values: nothing moves real funds, and credentials in
            snippets are placeholders.
          </p>

          <section id="getting-started" aria-labelledby="h-getting-started">
            <h2 id="h-getting-started">Getting started</h2>
            <p>
              Set <code className="mono">{baseUrlVariable}</code> to your
              sandbox API origin (the local default is{" "}
              <code className="mono">http://localhost:8080</code>) and{" "}
              <code className="mono">RAILVANTA_SANDBOX_KEY</code> to a sandbox
              API key. Keep the key on your server; this site never asks for raw
              payment credentials and the browser never holds an API key.
            </p>
            <Examples section="getting-started" />
          </section>

          <section id="authentication" aria-labelledby="h-authentication">
            <h2 id="h-authentication">Authentication</h2>
            <ul className="prose-list">
              <li>
                Send{" "}
                <code className="mono">
                  Authorization: Bearer prefix.secret
                </code>
                . Only a one-way hash of each key is stored, and revoked or
                expired keys are rejected immediately.
              </li>
              <li>
                Keys are bound to one workspace and a role. A read role can only
                read; an operator role can create payments and use the sandbox
                webhook route.
              </li>
              <li>
                Another workspace&apos;s resources answer 404, not 403, so IDs
                are never confirmed across workspaces.
              </li>
              <li>
                API keys are provisioned out of band in this milestone. There
                are no key or role management endpoints.
              </li>
            </ul>
          </section>

          <section id="payments" aria-labelledby="h-payments">
            <h2 id="h-payments">Payments</h2>
            <p>
              Money is always an integer count of minor units written as a
              decimal string, with an ISO currency code. Floating-point amounts
              are rejected. Accepted payments run asynchronously against the
              deterministic mock provider.
            </p>
            <Examples section="payments" />
          </section>

          <section id="idempotency" aria-labelledby="h-idempotency">
            <h2 id="h-idempotency">Idempotency and retry safety</h2>
            <ul className="prose-list">
              <li>
                <strong>Timeout or dropped connection:</strong> resend the
                identical request with the <em>same</em> Idempotency-Key. You
                get the original response back and never a second payment.
              </li>
              <li>
                <strong>New intent:</strong> use a <em>new</em> key. Reusing a
                key with a different amount, currency, method or reference
                returns 409 <code className="mono">idempotency_conflict</code>.
              </li>
              <li>
                <strong>
                  Outcome <code className="mono">unknown</code>:
                </strong>{" "}
                the provider result is ambiguous. Poll the payment. Do not
                create another payment to &quot;try again&quot;, and never
                assume another provider is safe: Railvanta resolves an unknown
                outcome only by looking it up at the original provider.
              </li>
              <li>
                Reads (GET) are always safe to retry. Do not retry a failed
                validation without fixing the request.
              </li>
            </ul>
            <Examples section="idempotency" />
          </section>

          <section id="webhooks" aria-labelledby="h-webhooks">
            <h2 id="h-webhooks">Events and webhooks</h2>
            <p>
              Events combine the webhook inbox and the outbox. Delivery is
              at-least-once, so handlers must tolerate duplicates and
              reordering. Railvanta stores each provider event ID once:
              identical repeats only increment a duplicate counter, a repeat
              with different content is flagged for investigation and never
              overwrites the first, and an event that conflicts with a finished
              payment is recorded as ignored instead of moving it backwards.
            </p>
            <p>
              The mock provider is unsigned, so events show{" "}
              <strong>Signature: Not applicable</strong>. Only a verification
              recorded by the backend may ever show Verified or Rejected, and
              real provider webhooks are planned. The sandbox route below exists
              to drive these scenarios; it needs an operator key and is not
              available outside the sandbox.
            </p>
            <Examples section="webhooks" />
          </section>

          <section id="routing" aria-labelledby="h-routing">
            <h2 id="h-routing">Routing</h2>
            <p>
              Each payment is routed by priority-ordered rules to one provider.
              Rules are seeded and read-only; the evaluation preview does not
              create a payment or contact a provider. A routing decision is
              recorded on every attempt as a reason. Railvanta never retries an
              ambiguous money-moving call against a different provider.
            </p>
          </section>

          <section id="errors" aria-labelledby="h-errors">
            <h2 id="h-errors">Errors</h2>
            <ul className="prose-list">
              <li>
                400 validation, 401 missing or invalid credentials, 403
                insufficient role, 404 not found in your workspace, 409 conflict
                (idempotency or state), 429 rate limited (honor Retry-After),
                503 temporarily unavailable.
              </li>
              <li>
                Quote the <code className="mono">request_id</code> when asking
                for help. Error messages never include secrets or payloads.
              </li>
              <li>
                A 5xx or a transport error on a write does not say whether it
                took effect: retry with the same Idempotency-Key.
              </li>
            </ul>
            <Examples section="errors" />
          </section>

          <section id="limitations" aria-labelledby="h-limitations">
            <h2 id="h-limitations">Supported and planned</h2>
            <h3>Supported endpoints</h3>
            <Table label="Supported endpoints">
              <thead>
                <tr>
                  <th scope="col">Method</th>
                  <th scope="col">Path</th>
                  <th scope="col">Purpose</th>
                </tr>
              </thead>
              <tbody>
                {docsSource.supported.map((endpoint) => (
                  <tr key={`${endpoint.method} ${endpoint.path}`}>
                    <td>{endpoint.method}</td>
                    <td>
                      <code className="mono">{endpoint.path}</code>
                    </td>
                    <td>{endpoint.note}</td>
                  </tr>
                ))}
              </tbody>
            </Table>
            <h3>Planned (not available)</h3>
            <ul className="prose-list">
              {docsSource.planned.map((item) => (
                <li key={item.name}>
                  <Badge>Planned</Badge> <strong>{item.name}.</strong>{" "}
                  {item.note}
                </li>
              ))}
            </ul>
          </section>
        </main>
      </div>
      <footer className="public-footer">Sandbox only / No real funds</footer>
    </div>
  );
}
