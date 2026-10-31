import Link from "next/link";
import { Badge } from "@/components/ui";
import { CopyCode } from "@/components/ui/copy-code";

const apiExample = `curl -X POST http://localhost:3000/v1/payments \\
  -H "Authorization: Bearer $RAILVANTA_API_KEY" \\
  -H "Content-Type: application/json" \\
  -H "Idempotency-Key: order-1042-attempt" \\
  -d '{
    "amount_minor": "25000",
    "currency": "USD",
    "payment_method_id": "<sandbox payment method ID>",
    "merchant_reference": "order-1042"
  }'

# 202 Accepted + Location: /v1/payments/{id}
# Resending the same body with the same key replays the
# original response instead of creating a second payment.`;

const flow: { step: string; detail: string; available: boolean }[] = [
  { step: "Browser", detail: "Next.js dashboard", available: true },
  { step: "API", detail: "Go · same-origin /v1", available: true },
  { step: "Accept once", detail: "Idempotency-Key", available: true },
  { step: "Route", detail: "Rule engine dispatch", available: false },
  { step: "Mock provider", detail: "Deterministic sandbox", available: false },
  { step: "Journal", detail: "Balanced ledger posting", available: false },
];

const capabilities: {
  title: string;
  description: string;
  available: boolean;
}[] = [
  {
    title: "Idempotent acceptance",
    description:
      "One Idempotency-Key per payment intent; retries replay the stored response.",
    available: true,
  },
  {
    title: "Payment explorer",
    description:
      "Search, filter and inspect payments, attempts and state history from the API.",
    available: true,
  },
  {
    title: "Observed metrics",
    description:
      "Volume per currency, success rate and p95 computed from recorded payments.",
    available: true,
  },
  {
    title: "Explainable routing",
    description: "Provider selection with a recorded reason for every attempt.",
    available: false,
  },
  {
    title: "Ledger journals",
    description: "Balanced, immutable double-entry postings per payment.",
    available: false,
  },
  {
    title: "Reconciliation",
    description:
      "Unknown outcomes resolved from provider evidence, never blind retries.",
    available: false,
  },
];

export default function Home() {
  return (
    <div className="public-page">
      <header className="public-header">
        <Link href="/" className="brand">
          <span className="brand-mark" aria-hidden="true">
            ◇
          </span>
          Railvanta
        </Link>
        <nav aria-label="Public navigation">
          <Link href="/docs">API docs</Link>
          <Link href="/dashboard" className="button button-small">
            Explore sandbox <span aria-hidden="true">↗</span>
          </Link>
        </nav>
      </header>
      <main id="main-content" tabIndex={-1}>
        <section className="hero">
          <Badge>Sandbox infrastructure</Badge>
          <h1>
            Payments,
            <br />
            intelligently routed.
          </h1>
          <p className="hero-description">
            One API. Every rail. Complete control.
            <br />A sandbox for safe payment acceptance, explainable routing,
            and balanced financial records.
          </p>
          <div className="hero-actions">
            <Link className="button" href="/dashboard">
              Explore the dashboard <span aria-hidden="true">→</span>
            </Link>
            <Link className="text-link" href="/docs">
              Read the API documentation
            </Link>
          </div>
          <p className="sandbox-notice" role="note">
            <strong>Sandbox only / no real funds.</strong> Payments use
            simulated sandbox methods; no card or bank credentials are
            collected.
          </p>
        </section>
        <section
          className="architecture-card"
          aria-labelledby="architecture-title"
        >
          <div className="section-title">
            <span className="eyebrow">Architecture</span>
            <span className="text-label">
              Available now · Planned steps are marked
            </span>
          </div>
          <h2 id="architecture-title">Every outcome has an explanation.</h2>
          <ol className="flow-list flow-strip">
            {flow.map(({ step, detail, available }, index) => (
              <li key={step} className={available ? "" : "planned"}>
                <span>{index + 1}</span>
                <div>
                  <strong>{step}</strong>
                  <p className="text-label">
                    {detail} · {available ? "Available" : "Planned"}
                  </p>
                </div>
              </li>
            ))}
          </ol>
          <p className="muted">
            Stripe and Plaid sandbox adapters are planned and not configured.
          </p>
        </section>
        <section className="capabilities" aria-labelledby="capabilities-title">
          <h2 id="capabilities-title">Capabilities</h2>
          <div className="capability-grid">
            {capabilities.map(({ title, description, available }) => (
              <article key={title} className="card">
                <h3>{title}</h3>
                <p className="muted">{description}</p>
                <span className={available ? "badge" : "badge badge-planned"}>
                  {available ? "Available" : "Planned"}
                </span>
              </article>
            ))}
          </div>
        </section>
        <section className="api-example" aria-labelledby="api-example-title">
          <h2 id="api-example-title">Create a sandbox payment</h2>
          <CopyCode code={apiExample} label="API example: create a payment" />
        </section>
      </main>
      <footer className="public-footer">
        Railvanta · Sandbox only / No real funds
      </footer>
    </div>
  );
}
