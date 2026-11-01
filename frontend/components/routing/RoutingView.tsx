"use client";

import { useId, useState, type FormEvent } from "react";
import { Button, Card, EmptyState, Table } from "@/components/ui";
import { QueryState } from "@/components/ui/query-state";
import type { RoutingDecision, RoutingRule } from "@/lib/api/routing-schemas";
import { usePaymentMethods } from "@/lib/hooks/payments";
import {
  useEvaluateRouting,
  useRoutingRules,
  type EvaluationFailure,
} from "@/lib/hooks/routing";
import { currencies } from "@/lib/money";

export const mutationDisabledReason = "requires rule mutation contract";

function RuleTable({ rules }: { rules: RoutingRule[] }) {
  return (
    <Table label="Routing rules">
      <thead>
        <tr>
          <th scope="col">Priority</th>
          <th scope="col">Rail</th>
          <th scope="col">Currency</th>
          <th scope="col">Amount range (minor units)</th>
          <th scope="col">Provider</th>
          <th scope="col">Enabled</th>
          <th scope="col">Actions</th>
        </tr>
      </thead>
      <tbody>
        {rules.map((rule) => (
          <tr key={rule.id}>
            <td className="numeric">{rule.priority}</td>
            <td>{rule.rail}</td>
            <td>{rule.currency}</td>
            <td className="numeric">
              {rule.min_amount_minor} – {rule.max_amount_minor ?? "unbounded"}
            </td>
            <td>{rule.provider_code}</td>
            <td>{rule.enabled ? "Enabled" : "Disabled"}</td>
            <td>
              <Button
                className="button-quiet button-small"
                disabled
                aria-describedby="rule-mutation-reason"
              >
                Edit
              </Button>
            </td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}

function CheckList({ decision }: { decision: RoutingDecision }) {
  return (
    <ul className="check-list">
      {decision.checks.map((check) => (
        <li
          key={check.id}
          className={check.passed ? "check-pass" : "check-fail"}
        >
          <span aria-hidden="true">{check.passed ? "✓" : "✕"}</span>{" "}
          <strong>{check.label}</strong>
          <span className="sr-only">
            {check.passed ? " passed" : " failed"}
          </span>
          <span className="muted small"> — {check.detail}</span>
        </li>
      ))}
    </ul>
  );
}

export function EvaluationPath({
  request,
  decision,
}: {
  request: { method: string; amount: string; currency: string };
  decision: RoutingDecision;
}) {
  return (
    <ol className="evaluation-path" aria-label="Evaluation path">
      <li>
        <h3>1. Request</h3>
        <p>
          Method {request.method}, {request.amount} {request.currency} minor
          units.
        </p>
      </li>
      <li>
        <h3>2. Matching rule</h3>
        {decision.rule ? (
          <p>
            Rule <span className="mono">{decision.rule.id}</span>, priority{" "}
            {decision.rule.priority}, {decision.rule.rail}/
            {decision.rule.currency}, {decision.rule.min_amount_minor} –{" "}
            {decision.rule.max_amount_minor ?? "unbounded"}.
          </p>
        ) : (
          <p>
            No rule is on record for this decision: the original attempt
            predates rule tracking.
          </p>
        )}
      </li>
      <li>
        <h3>3. Capability checks</h3>
        <CheckList decision={decision} />
      </li>
      <li>
        <h3>4. Result</h3>
        {decision.failover_blocked ? (
          <section className="unknown-notice" role="status">
            <h4>
              <span aria-hidden="true">! </span>Unsafe failover blocked
            </h4>
            <p>{decision.reason}</p>
          </section>
        ) : (
          <>
            <p>
              Route to <strong>{decision.provider_code}</strong> (
              <span className="mono">{decision.provider_id}</span>).
            </p>
            <p className="muted small">{decision.reason}</p>
          </>
        )}
      </li>
    </ol>
  );
}

function FailureView({ failure }: { failure: EvaluationFailure }) {
  return (
    <section role="alert" className="state-panel error-panel">
      <h3>
        {failure.status === 422
          ? "No provider can take this payment"
          : "Evaluation failed"}
      </h3>
      <p>{failure.message}</p>
      {failure.reasons.length > 0 && (
        <ul>
          {failure.reasons.map((reason) => (
            <li key={`${reason.field}-${reason.message}`}>
              <strong>{reason.field.replace("provider:", "")}</strong>:{" "}
              {reason.message}
            </li>
          ))}
        </ul>
      )}
      <p className="muted small">
        Request ID: <code className="mono">{failure.requestId}</code>
      </p>
    </section>
  );
}

function Evaluator() {
  const id = useId();
  const methods = usePaymentMethods();
  const evaluate = useEvaluateRouting();
  const [submitted, setSubmitted] = useState<{
    method: string;
    amount: string;
    currency: string;
  } | null>(null);
  const [localError, setLocalError] = useState<string | null>(null);
  const items = methods.query.data?.data.items ?? [];

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const amount = String(form.get("amount") ?? "").trim();
    const method = String(form.get("method") ?? "");
    const currency = String(form.get("currency") ?? "USD");
    const paymentId = String(form.get("payment_id") ?? "").trim();
    if (!/^[1-9][0-9]{0,18}$/.test(amount)) {
      setLocalError("Enter a positive whole number of minor units.");
      return;
    }
    if (!method) {
      setLocalError("Choose a payment method.");
      return;
    }
    setLocalError(null);
    setSubmitted({ method, amount, currency });
    evaluate.mutate({
      payment_method_id: method,
      amount_minor: amount,
      currency,
      ...(paymentId ? { payment_id: paymentId } : {}),
    });
  };

  return (
    <Card aria-labelledby={`${id}-title`}>
      <h2 id={`${id}-title`}>Evaluate a routing decision</h2>
      <p className="muted">
        Asks the same router the dispatcher uses which provider a payment would
        reach. It is read-only: nothing is created and no money moves.
      </p>
      <form onSubmit={onSubmit} noValidate className="filter-bar">
        <div className="field">
          <label htmlFor={`${id}-method`}>Payment method</label>
          <select id={`${id}-method`} name="method" defaultValue="">
            <option value="" disabled>
              Select a method
            </option>
            {items.map((method) => (
              <option key={method.id} value={method.id}>
                {method.brand} •••• {method.last_four} ({method.rail})
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor={`${id}-amount`}>Amount (minor units)</label>
          <input
            id={`${id}-amount`}
            name="amount"
            inputMode="numeric"
            autoComplete="off"
            defaultValue="2500"
            aria-invalid={localError?.startsWith("Enter") ? true : undefined}
            aria-describedby={localError ? `${id}-error` : undefined}
          />
        </div>
        <div className="field">
          <label htmlFor={`${id}-currency`}>Currency</label>
          <select id={`${id}-currency`} name="currency" defaultValue="USD">
            {currencies.map((currency) => (
              <option key={currency} value={currency}>
                {currency}
              </option>
            ))}
          </select>
        </div>
        <div className="field field-wide">
          <label htmlFor={`${id}-payment`}>
            Existing payment ID (optional)
          </label>
          <input
            id={`${id}-payment`}
            name="payment_id"
            autoComplete="off"
            aria-describedby={`${id}-payment-hint`}
          />
          <p id={`${id}-payment-hint`} className="field-hint">
            Name a payment with an unresolved attempt to see why it is never
            routed to another provider.
          </p>
        </div>
        <div className="filter-actions">
          <Button type="submit" disabled={evaluate.isPending}>
            {evaluate.isPending ? "Evaluating…" : "Evaluate"}
          </Button>
        </div>
      </form>
      {localError && (
        <p id={`${id}-error`} className="field-error" role="alert">
          {localError}
        </p>
      )}
      {evaluate.isError && <FailureView failure={evaluate.error} />}
      {evaluate.isSuccess && submitted && (
        <EvaluationPath request={submitted} decision={evaluate.data} />
      )}
    </Card>
  );
}

export function RoutingView() {
  const { query, workspace } = useRoutingRules();
  return (
    <>
      <p className="sandbox-notice" role="note">
        Sandbox routing over the Mock provider only. Rules are read-only here.
      </p>
      <section aria-labelledby="rules-heading">
        <div className="card-heading">
          <h2 id="rules-heading">Routing rules</h2>
          <div className="heading-actions">
            <Button disabled aria-describedby="rule-mutation-reason">
              Create rule
            </Button>
            <Button disabled aria-describedby="rule-mutation-reason">
              Reorder
            </Button>
          </div>
        </div>
        <p id="rule-mutation-reason" className="muted small">
          Rule create, edit and reorder are disabled: {mutationDisabledReason}.
        </p>
        <QueryState
          query={query}
          workspace={workspace}
          loadingLabel="Loading routing rules"
        >
          {(result) =>
            result.data.items.length === 0 ? (
              <EmptyState title="No routing rules">
                Without a rule no payment can be routed to a provider.
              </EmptyState>
            ) : (
              <RuleTable rules={result.data.items} />
            )
          }
        </QueryState>
      </section>
      <Evaluator />
    </>
  );
}
