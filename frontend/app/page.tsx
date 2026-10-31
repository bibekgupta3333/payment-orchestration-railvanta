import Link from "next/link";
import { Badge } from "@/components/ui";

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
          <Badge>Sandbox infrastructure · Foundation build</Badge>
          <h1>
            Payments,
            <br />
            intelligently routed.
          </h1>
          <p className="hero-description">
            One API. Every rail. Complete control.
            <br />A foundation for safe payment execution, explainable routing,
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
          <p className="small muted">
            No real funds. Payment execution is not available in this build.
          </p>
        </section>
        <section
          className="architecture-card"
          aria-labelledby="architecture-title"
        >
          <div className="section-title">
            <span className="eyebrow">Designed for correctness</span>
            <span className="text-label">Planned payment flow</span>
          </div>
          <h2 id="architecture-title">Every outcome has an explanation.</h2>
          <ol className="flow-list">
            {[
              "Accept once",
              "Route safely",
              "Record exactly",
              "Reconcile uncertainty",
            ].map((step, index) => (
              <li key={step}>
                <span>{index + 1}</span>
                {step}
              </li>
            ))}
          </ol>
          <p className="muted">
            Provider execution, journals, and reconciliation are planned
            milestones. The current foundation includes the API, database, auth
            contract, and this responsive shell.
          </p>
        </section>
        <section className="principles" aria-label="Engineering principles">
          {[
            [
              "Idempotency",
              "A repeated request must not become a second payment.",
            ],
            [
              "Exact accounting",
              "Integer money and balanced, immutable journal entries.",
            ],
            [
              "Honest recovery",
              "An unknown outcome requires evidence, not a blind retry.",
            ],
          ].map(([title, description]) => (
            <article key={title}>
              <h2>{title}</h2>
              <p>{description}</p>
              <span className="text-label">Design principle</span>
            </article>
          ))}
        </section>
      </main>
      <footer className="public-footer">
        Railvanta · Sandbox only / No real funds
      </footer>
    </div>
  );
}
