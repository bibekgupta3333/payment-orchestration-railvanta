/**
 * Why Railvanta exists: the real-world payment failures this sandbox
 * reproduces and the guarantee the backend applies to each. Shared by the
 * landing page and the dashboard hub. Wording follows the simulation catalog
 * (backend/internal/simulation/catalog.go); everything runs against the
 * deterministic sandbox mock provider, never a real one.
 */
export interface WhyProblem {
  id: string;
  /** Backend simulation scenarios that reproduce this problem. */
  scenarios: readonly string[];
  /** The real-world failure, in plain words. */
  title: string;
  whatGoesWrong: string;
  /** What Railvanta does about it in this sandbox build. */
  guarantee: string;
  proof: { label: string; href: string };
}

export const whyProblems: readonly WhyProblem[] = [
  {
    id: "double-charge",
    scenarios: ["duplicate_request"],
    title: "A retry charges the customer twice",
    whatGoesWrong:
      "A client times out and sends the same payment again, or a double click fires two requests at once. Without a shared key, each request can become its own charge.",
    guarantee:
      "Every create carries an Idempotency-Key. Of ten concurrent requests with one key, exactly one is accepted and the rest replay the stored response; the same key with a changed amount is rejected with 409. One payment, one provider attempt.",
    proof: { label: "Payments", href: "/dashboard/payments" },
  },
  {
    id: "lost-response",
    scenarios: ["timeout_after_success"],
    title: "The provider charged, but the response never arrived",
    whatGoesWrong:
      "The charge succeeds at the provider and the network drops the reply. Treating the timeout as a failure invites a second charge; treating it as success is a guess.",
    guarantee:
      "The payment is marked unknown, not failed. It is resolved only by a lookup at the same provider with the stored key, which finds the original charge and settles it as succeeded.",
    proof: { label: "Simulator", href: "/dashboard/simulator" },
  },
  {
    id: "outage",
    scenarios: ["outage"],
    title: "The provider goes down mid-payment",
    whatGoesWrong:
      "When a provider stops answering, the tempting fix is to send the charge to a backup provider. If the first one did execute it, the customer pays twice.",
    guarantee:
      "Submission and lookups fail, the payment stays unknown, and no other provider is tried. An ambiguous charge is never failed over blindly.",
    proof: { label: "Routing", href: "/dashboard/routing" },
  },
  {
    id: "duplicate-webhook",
    scenarios: ["duplicate_webhook"],
    title: "The same webhook arrives again and again",
    whatGoesWrong:
      "Providers deliver events at least once, so one success notice can arrive several times. Handling each copy as new can apply the same effect repeatedly.",
    guarantee:
      "Events pass through a webhook inbox keyed by the provider event id. One inbox row counts the repeats, and no second financial effect occurs.",
    proof: { label: "Events", href: "/dashboard/events" },
  },
  {
    id: "double-posting",
    scenarios: ["duplicate_posting"],
    title: "The books record one payment twice",
    whatGoesWrong:
      "A replayed job or a retried worker posts the same success to the ledger again, and balances drift from what actually happened.",
    guarantee:
      "Each ledger posting has a posting key. Replaying the success posting twice more still leaves exactly one balanced double-entry journal.",
    proof: { label: "Ledger", href: "/dashboard/ledger" },
  },
  {
    id: "settlement-mismatch",
    scenarios: ["settlement_mismatch"],
    title: "The provider and our records disagree",
    whatGoesWrong:
      "The provider reports a settled charge while the internal payment is still unknown. Quietly editing balances to match hides the problem instead of explaining it.",
    guarantee:
      "Reconciliation records the discrepancy with the provider evidence and changes no payment or ledger row. The discrepancy moves to investigating with an evidence-only note.",
    proof: { label: "Reconciliation", href: "/dashboard/reconciliation" },
  },
];

/** The baseline the failures are compared with: a clean success and decline. */
export const whyBaseline = {
  scenarios: ["success", "decline"] as const,
  title: "Baseline: a charge that succeeds and one that is declined",
  guarantee:
    "Both reach a definite state, and a success posts one balanced journal.",
} as const;
