import Link from "next/link";
import { whyProblems } from "@/lib/why-story";

/** Landing section: the payment failures this sandbox exists to show. */
export function WhyRailvanta() {
  return (
    <section className="why-story" aria-labelledby="why-title">
      <span className="eyebrow">Why Railvanta exists</span>
      <h2 id="why-title">Payments fail in the gaps.</h2>
      <p className="why-intro">
        Most payment bugs live between &ldquo;we sent it&rdquo; and &ldquo;we
        know what happened&rdquo;: a retry, a lost response, an outage, a
        repeated webhook, a replayed ledger job, a provider that disagrees with
        our records. This sandbox reproduces each one against a deterministic
        mock provider and shows the guarantee that handles it, with the evidence
        on the dashboard.
      </p>
      <ul className="why-grid">
        {whyProblems.map((problem, index) => (
          <li key={problem.id} className="card why-card">
            <span className="text-label">Problem {index + 1}</span>
            <h3>{problem.title}</h3>
            <p className="muted">{problem.whatGoesWrong}</p>
            <p className="why-guarantee">
              <strong>Railvanta:</strong> {problem.guarantee}
            </p>
            <Link
              className="text-link"
              href={problem.proof.href}
              aria-label={`See it in the sandbox: ${problem.title} (${problem.proof.label})`}
            >
              See it in the sandbox <span aria-hidden="true">→</span>
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}
