"use client";

import { useId, useState, type FormEvent } from "react";
import { Button } from "@/components/ui";
import { currencies, parseMajorAmount } from "@/lib/money";
import { usePaymentMethods } from "@/lib/hooks/payments";
import { useEvaluateRouting } from "@/lib/hooks/routing";

const verdictLabel = {
  chosen: "Chosen",
  pinned: "Pinned (lookup only, unresolved attempt)",
  eligible_lower_priority: "Eligible, lower priority",
  ineligible: "Ineligible",
  blocked: "Blocked: unsafe failover",
} as const;

export function RoutingDemo() {
  const id = useId();
  const methods = usePaymentMethods();
  const evaluate = useEvaluateRouting();
  const [methodId, setMethodId] = useState("");
  const [amount, setAmount] = useState("100.00");
  const [currency, setCurrency] = useState("USD");
  const [paymentId, setPaymentId] = useState("");
  const [formError, setFormError] = useState<string | null>(null);
  const items = methods.query.data?.data.items ?? [];
  const chosen = methodId || items[0]?.id || "";
  const decision = evaluate.data;
  const failure = evaluate.error;

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const parsed = parseMajorAmount(amount, currency as never);
    if (!parsed.ok) {
      setFormError(parsed.message);
      return;
    }
    setFormError(null);
    evaluate.mutate({
      payment_method_id: chosen,
      amount_minor: parsed.amountMinor,
      currency,
      ...(paymentId.trim() ? { payment_id: paymentId.trim() } : {}),
    });
  };

  return (
    <div className="mt-3">
      <form
        onSubmit={submit}
        className="filter-bar"
        aria-label="Routing evaluation"
      >
        <div className="field field-wide">
          <label htmlFor={`${id}-method`}>Payment method</label>
          <select
            id={`${id}-method`}
            value={chosen}
            onChange={(event) => setMethodId(event.target.value)}
          >
            {items.map((method) => (
              <option key={method.id} value={method.id}>
                {method.brand ?? method.rail} ···· {method.last_four ?? "—"} (
                {method.rail}, {method.id.slice(-4)})
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor={`${id}-amount`}>Amount</label>
          <input
            id={`${id}-amount`}
            value={amount}
            inputMode="decimal"
            autoComplete="off"
            onChange={(event) => setAmount(event.target.value)}
          />
        </div>
        <div className="field">
          <label htmlFor={`${id}-currency`}>Currency</label>
          <select
            id={`${id}-currency`}
            value={currency}
            onChange={(event) => setCurrency(event.target.value)}
          >
            {currencies.map((code) => (
              <option key={code}>{code}</option>
            ))}
          </select>
        </div>
        <div className="filter-actions">
          <div className="field field-wide">
            <label htmlFor={`${id}-payment`}>
              Existing payment ID (optional, shows the failover guard)
            </label>
            <input
              id={`${id}-payment`}
              value={paymentId}
              onChange={(event) => setPaymentId(event.target.value)}
              placeholder="Paste an unknown payment's ID"
              spellCheck={false}
            />
          </div>
          <Button type="submit" disabled={!chosen || evaluate.isPending}>
            {evaluate.isPending ? "Evaluating…" : "Evaluate routing"}
          </Button>
        </div>
      </form>
      {formError && (
        <p role="alert" className="field-error">
          {formError}
        </p>
      )}
      {failure && (
        <section role="alert" className="state-panel error-panel">
          <p>
            {failure.message}{" "}
            <span className="muted">
              Request ID: <code className="mono">{failure.requestId}</code>
            </span>
          </p>
          {failure.reasons.length > 0 && (
            <ul>
              {failure.reasons.map((reason, index) => (
                <li key={index}>
                  <span className="mono">{reason.field}</span>: {reason.message}
                </li>
              ))}
            </ul>
          )}
        </section>
      )}
      {decision && (
        <section aria-label="Routing decision" data-testid="routing-decision">
          <dl className="detail-list">
            <div>
              <dt>Chosen provider</dt>
              <dd className="mono">
                {decision.provider_code} ({decision.provider_id})
              </dd>
            </div>
            <div>
              <dt>Rule</dt>
              <dd className="mono">{decision.rule_id ?? "—"}</dd>
            </div>
            <div>
              <dt>Reason</dt>
              <dd>{decision.reason}</dd>
            </div>
          </dl>
          {decision.failover_blocked && (
            <section className="unknown-notice" role="status">
              <h4>
                <span aria-hidden="true">! </span>Unknown outcome — failover
                blocked
              </h4>
              <p>
                A provider reported an ambiguous outcome, so the payment is not
                sent to another provider: it might already have been charged.
              </p>
            </section>
          )}
          {decision.alternatives && decision.alternatives.length > 0 && (
            <>
              <h4>Why this provider, and why not the others</h4>
              <ul data-testid="routing-alternatives">
                {decision.alternatives.map((alt) => (
                  <li key={alt.provider_id}>
                    <strong className="mono">{alt.provider_code}</strong>:{" "}
                    {verdictLabel[alt.status]}
                    {alt.reasons.length > 0 && (
                      <span className="muted small">
                        {" "}
                        — {alt.reasons.join("; ")}
                      </span>
                    )}
                  </li>
                ))}
              </ul>
            </>
          )}
          <h4>Per-provider eligibility</h4>
          <ul>
            {decision.checks.map((check) => (
              <li key={check.id}>
                <span aria-hidden="true">{check.passed ? "✓" : "✕"}</span>{" "}
                <strong>{check.label}</strong>: {check.passed ? "pass" : "fail"}
                <span className="muted small"> — {check.detail}</span>
              </li>
            ))}
          </ul>
          {decision.candidates.length > 0 && (
            <p className="small">
              Eligible candidates by priority:{" "}
              {decision.candidates
                .map((c) => `${c.provider_code} (#${c.priority})`)
                .join(", ")}
            </p>
          )}
          <p className="muted small">
            Simulated / sandbox: read-only evaluation, no money moves.
          </p>
        </section>
      )}
    </div>
  );
}
