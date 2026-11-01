import Link from "next/link";
import { Badge } from "@/components/ui";
import { Lifecycle } from "@/components/landing/Lifecycle";
import { SandboxStart } from "@/components/landing/SandboxStart";
import { StartSandboxButton } from "@/components/landing/StartSandboxButton";

const flow: { step: string; detail: string; available: boolean }[] = [
  { step: "Browser", detail: "Next.js dashboard", available: true },
  { step: "API", detail: "Go · same-origin /v1", available: true },
  { step: "Accept once", detail: "Idempotency-Key", available: true },
  { step: "Route", detail: "Rule engine dispatch", available: true },
  { step: "Mock provider", detail: "Deterministic sandbox", available: true },
  { step: "Journal", detail: "Balanced ledger posting", available: true },
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
    available: true,
  },
  {
    title: "Ledger journals",
    description: "Balanced, immutable double-entry postings per payment.",
    available: true,
  },
  {
    title: "Reconciliation",
    description:
      "Compares provider records, payments and journals; discrepancies are recorded with evidence, never silently adjusted.",
    available: true,
  },
  {
    title: "Recovery of unknown outcomes",
    description:
      "A lost provider response ends in a lookup at the original provider, never a retry elsewhere.",
    available: true,
  },
  {
    title: "Failure playground",
    description:
      "Run duplicate-request, lost-response and settlement-mismatch scenarios and see the backend's evidence.",
    available: true,
  },
  {
    title: "Stripe and Plaid adapters",
    description:
      "Real provider integrations are not built; the sandbox uses a deterministic mock provider.",
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
            <StartSandboxButton />
            <Link className="button button-quiet" href="/dashboard">
              Explore the dashboard <span aria-hidden="true">→</span>
            </Link>
            <Link className="text-link" href="/dashboard/api-reference">
              Open API reference
            </Link>
          </div>
          <p className="sandbox-notice" role="note">
            <strong>Sandbox only / no real funds.</strong> Payments use
            simulated sandbox methods; no card or bank credentials are
            collected.
          </p>
        </section>
        <Lifecycle />
        <SandboxStart />
        <section
          className="architecture-card"
          aria-labelledby="architecture-title"
        >
          <div className="section-title">
            <span className="eyebrow">Architecture</span>
            <span className="text-label">
              Sandbox build · Planned items are marked
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
      </main>
      <footer className="public-footer">
        Railvanta · Sandbox only / No real funds
      </footer>
    </div>
  );
}
