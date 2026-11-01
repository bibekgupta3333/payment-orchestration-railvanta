"use client";

import Link from "next/link";
import { StatusPill } from "@/components/ui";
import {
  FailoverNotice,
  InvariantStatus,
  RunTimeline,
} from "@/components/simulator/SimulatorView";
import { formatTimestamp } from "@/components/payments/PaymentTable";
import type { Simulation } from "@/lib/api/routing-schemas";
import {
  journalTotals,
  succeededAttempts,
  usePaymentDiscrepancy,
} from "@/lib/hooks/playground";
import { formatMoney } from "@/lib/money";

function Count({ label, value }: { label: string; value: number }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd className="numeric">{value}</dd>
    </div>
  );
}

function safeMoney(amount: string, currency: string): string {
  try {
    return formatMoney(amount, currency);
  } catch {
    return `${amount} (${currency} minor units)`;
  }
}

export function LedgerEvidence({ run }: { run: Simulation }) {
  if (run.journals.length === 0)
    return <p className="muted">No journal posted for this run.</p>;
  return (
    <section aria-label="Ledger evidence">
      <h4>Journal lines</h4>
      <p className="small" data-testid="journal-count">
        Journals for this payment: <strong>{run.journals.length}</strong>
        {run.scenario === "duplicate_posting" &&
          run.journals.length === 1 &&
          " — posted twice more, still one journal"}
      </p>
      {run.journals.map((journal) => {
        const totals = journalTotals(journal);
        return (
          <div key={journal.id}>
            <table className="data-table">
              <caption className="sr-only">
                Entries of journal {journal.id}
              </caption>
              <thead>
                <tr>
                  <th scope="col">Account</th>
                  <th scope="col">Side</th>
                  <th scope="col">Amount</th>
                </tr>
              </thead>
              <tbody>
                {journal.entries.map((entry) => (
                  <tr key={entry.id}>
                    <td className="mono">{entry.account_code}</td>
                    <td>{entry.side}</td>
                    <td className="numeric">
                      {safeMoney(entry.amount_minor, journal.currency)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            <p data-testid="balance-check">
              <strong>
                {totals.balanced ? "✓ Debits = credits" : "✕ UNBALANCED"}
              </strong>{" "}
              (debits {safeMoney(totals.debit, journal.currency)}, credits{" "}
              {safeMoney(totals.credit, journal.currency)}, computed from the
              returned lines)
            </p>
          </div>
        );
      })}
    </section>
  );
}

function ReconciliationEvidence({ run }: { run: Simulation }) {
  const { match, query } = usePaymentDiscrepancy(
    run.payment_id,
    run.status === "completed",
  );
  const ledgerInvariant = run.invariants.find((item) =>
    /ledger|adjust/i.test(`${item.id} ${item.title}`),
  );
  if (!query.data)
    return <p className="muted">Loading reconciliation evidence…</p>;
  if (!match)
    return (
      <p className="muted">No discrepancy row exists for this payment yet.</p>
    );
  const d = match.details;
  return (
    <section aria-label="Reconciliation evidence">
      <h4>Discrepancy</h4>
      <dl className="detail-list">
        <div>
          <dt>Kind</dt>
          <dd className="mono">{match.kind}</dd>
        </div>
        <div>
          <dt>Status</dt>
          <dd>{match.status}</dd>
        </div>
        <div>
          <dt>Summary</dt>
          <dd>{d.summary}</dd>
        </div>
        {d.expected && (
          <div>
            <dt>Expected</dt>
            <dd>{d.expected}</dd>
          </div>
        )}
        <div>
          <dt>Our record</dt>
          <dd>
            {d.payment.status},{" "}
            {safeMoney(d.payment.amount_minor, d.payment.currency)}
          </dd>
        </div>
        <div>
          <dt>Provider evidence</dt>
          <dd>
            {d.provider_charge
              ? `${d.provider_charge.provider_reference}: ${d.provider_charge.status}, ${safeMoney(d.provider_charge.amount_minor, d.provider_charge.currency)}`
              : "No provider charge found"}
          </dd>
        </div>
        <div>
          <dt>Resolution note</dt>
          <dd>{match.resolution_note ?? "Not resolved yet"}</dd>
        </div>
      </dl>
      <p data-testid="ledger-rows-changed">
        {ledgerInvariant?.status === "pass" ? (
          <>
            <strong>Ledger rows changed: 0</strong> — nothing adjusted to force
            the numbers to match. ({ledgerInvariant.evidence})
          </>
        ) : (
          "Ledger rows changed: not confirmed by the backend yet."
        )}
      </p>
    </section>
  );
}

export function RunResult({ run }: { run: Simulation }) {
  const first = run.journals[0];
  const unknown = run.payment?.status === "unknown";
  return (
    <div className="mt-4 grid gap-4" data-testid={`result-${run.scenario}`}>
      <p>
        <strong data-testid="run-state">
          {run.status === "running"
            ? "Running: waiting for the backend"
            : run.status === "completed"
              ? "Completed"
              : "Run failed"}
        </strong>
        {run.payment && (
          <>
            {" "}
            · payment <StatusPill status={run.payment.status} />
          </>
        )}
        <span className="badge ml-2">Simulated / sandbox</span>
      </p>
      {run.failure_reason && (
        <p role="alert" className="field-error">
          {run.failure_reason}
        </p>
      )}
      {unknown && (
        <section className="unknown-notice" role="status">
          <h4>
            <span aria-hidden="true">! </span>Outcome unknown
          </h4>
          <p>
            The provider never confirmed this payment. It is not shown as failed
            and is not retried elsewhere; only a lookup at the same provider can
            resolve it.
          </p>
        </section>
      )}
      <FailoverNotice run={run} />
      <dl className="detail-list" aria-label="Evidence counts">
        <Count label="Provider attempts" value={run.attempts.length} />
        <Count
          label="Succeeded attempts (charges)"
          value={succeededAttempts(run)}
        />
        <Count label="Journals" value={run.journals.length} />
        <Count label="Durable events" value={run.events.length} />
        {run.response && (
          <div>
            <dt>Acceptance response</dt>
            <dd>HTTP {run.response.status_code}</dd>
          </div>
        )}
      </dl>
      {run.scenario === "timeout_after_success" &&
        run.status === "completed" && (
          <p data-testid="no-second-charge">
            {succeededAttempts(run) <= 1 && run.attempts.length <= 1
              ? "No second charge: one attempt, at most one provider charge."
              : `Check evidence: ${run.attempts.length} attempts recorded.`}
          </p>
        )}
      <section aria-label="Invariants">
        <h4>Invariants checked by the backend</h4>
        <ul className="grid gap-2">
          {run.invariants.map((item) => (
            <li key={item.id}>
              <InvariantStatus status={item.status} />{" "}
              <strong>{item.title}</strong>
              <span className="muted small"> — {item.evidence}</span>
            </li>
          ))}
        </ul>
      </section>
      <section aria-label="State history">
        <h4>State history</h4>
        {run.transitions.length === 0 ? (
          <p className="muted">No transitions recorded yet.</p>
        ) : (
          <ol className="sim-timeline">
            {run.transitions.map((row, index) => (
              <li key={`${row.created_at}-${index}`}>
                <span className="nowrap mono">
                  {formatTimestamp(row.created_at)}
                </span>
                <span className="badge">state</span>
                <span>
                  <strong>
                    {row.from_status ?? "new"} → {row.to_status}
                  </strong>
                  <span className="muted small"> — {row.reason}</span>
                </span>
              </li>
            ))}
          </ol>
        )}
      </section>
      <section aria-label="Execution timeline">
        <h4>Execution timeline</h4>
        <RunTimeline run={run} />
      </section>
      {(run.scenario === "duplicate_posting" || run.scenario === "success") && (
        <LedgerEvidence run={run} />
      )}
      {run.scenario === "settlement_mismatch" && run.status === "completed" && (
        <ReconciliationEvidence run={run} />
      )}
      <p className="small">
        {run.payment_id && (
          <Link href={`/dashboard/payments/${run.payment_id}`}>Payment</Link>
        )}
        {first && (
          <>
            {" · "}
            <Link href={`/dashboard/ledger/${first.id}`}>Ledger journal</Link>
          </>
        )}
        {" · "}
        <Link href="/dashboard/events">Events</Link>
        {" · "}
        <Link href="/dashboard/reconciliation">Reconciliation</Link>
        {" · "}
        <Link href={`/dashboard/simulator?run=${encodeURIComponent(run.id)}`}>
          Full inspector
        </Link>
      </p>
    </div>
  );
}
