"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { DefaultDemo } from "@/components/demo/DefaultDemo";
import { Button, Card, EmptyState, Table } from "@/components/ui";
import { QueryState } from "@/components/ui/query-state";
import type { ApiResult } from "@/lib/api/client";
import type {
  RoutingDecision,
  RoutingRule,
  Simulation,
} from "@/lib/api/routing-schemas";
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

/**
 * The latest settled default-demo simulation started on this page. The shared
 * DefaultDemo card owns its run; the start mutation seeds the query cache with
 * the run, so this reads it from there. Runs that predate this mount (for
 * example from the Playground) are ignored.
 */
function useSettledDemoRun(): Simulation | null {
  const client = useQueryClient();
  const [run, setRun] = useState<Simulation | null>(null);
  useEffect(() => {
    const cache = client.getQueryCache();
    const mountedAt = Date.now();
    const scan = () => {
      let best: { run: Simulation; at: number } | null = null;
      for (const query of cache.findAll({
        predicate: (candidate) =>
          candidate.queryKey[3] === "sandbox" &&
          candidate.queryKey[4] === "simulation",
      })) {
        const data = (query.state.data as ApiResult<Simulation> | undefined)
          ?.data;
        if (
          !data ||
          data.status !== "completed" ||
          !data.payment_id ||
          query.state.dataUpdatedAt < mountedAt
        )
          continue;
        if (!best || query.state.dataUpdatedAt > best.at)
          best = { run: data, at: query.state.dataUpdatedAt };
      }
      if (best) {
        const found = best.run;
        setRun((previous) => (previous?.id === found.id ? previous : found));
      }
    };
    scan();
    return cache.subscribe(scan);
  }, [client]);
  return run;
}

function Evaluator({ demoRun }: { demoRun: Simulation | null }) {
  const id = useId();
  const methods = usePaymentMethods();
  const evaluate = useEvaluateRouting();
  const [method, setMethod] = useState("");
  const [amount, setAmount] = useState("10000");
  const [currency, setCurrency] = useState("USD");
  const [paymentId, setPaymentId] = useState("");
  const [appliedRun, setAppliedRun] = useState<string | null>(null);
  const [localError, setLocalError] = useState<string | null>(null);
  const items = methods.query.data?.data.items ?? [];
  // Preconfigured: the seeded Routing demo card, else the first method.
  const preferred =
    items.find((candidate) => candidate.id.endsWith("0131"))?.id ??
    items[0]?.id ??
    "";
  const effectiveMethod = method || preferred;
  const submitted = evaluate.variables
    ? {
        method: evaluate.variables.payment_method_id,
        amount: evaluate.variables.amount_minor,
        currency: evaluate.variables.currency,
      }
    : null;

  // Once the default demo settles, show its inputs and evaluate against its
  // payment so the unsafe-failover explanation appears.
  if (demoRun && demoRun.id !== appliedRun) {
    setAppliedRun(demoRun.id);
    setMethod(demoRun.request.payment_method_id);
    setAmount(demoRun.request.amount_minor);
    setCurrency(demoRun.request.currency);
    setPaymentId(demoRun.payment_id ?? "");
    setLocalError(null);
  }

  // One automatic read-only evaluation on first load; manual afterwards.
  const autoRan = useRef(false);
  useEffect(() => {
    if (autoRan.current || !effectiveMethod) return;
    autoRan.current = true;
    evaluate.mutate({
      payment_method_id: effectiveMethod,
      amount_minor: "10000",
      currency: "USD",
    });
  }, [effectiveMethod, evaluate]);

  const evaluatedRun = useRef<string | null>(null);
  useEffect(() => {
    if (!demoRun || evaluatedRun.current === demoRun.id) return;
    evaluatedRun.current = demoRun.id;
    evaluate.mutate({
      payment_method_id: demoRun.request.payment_method_id,
      amount_minor: demoRun.request.amount_minor,
      currency: demoRun.request.currency,
      ...(demoRun.payment_id ? { payment_id: demoRun.payment_id } : {}),
    });
  }, [demoRun, evaluate]);

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const trimmed = amount.trim();
    const trimmedPayment = paymentId.trim();
    if (!/^[1-9][0-9]{0,18}$/.test(trimmed)) {
      setLocalError("Enter a positive whole number of minor units.");
      return;
    }
    if (!effectiveMethod) {
      setLocalError("Choose a payment method.");
      return;
    }
    setLocalError(null);
    evaluate.mutate({
      payment_method_id: effectiveMethod,
      amount_minor: trimmed,
      currency,
      ...(trimmedPayment ? { payment_id: trimmedPayment } : {}),
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
          <select
            id={`${id}-method`}
            name="method"
            value={effectiveMethod}
            onChange={(event) => setMethod(event.target.value)}
          >
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
            value={amount}
            onChange={(event) => setAmount(event.target.value)}
            aria-invalid={localError?.startsWith("Enter") ? true : undefined}
            aria-describedby={localError ? `${id}-error` : undefined}
          />
        </div>
        <div className="field">
          <label htmlFor={`${id}-currency`}>Currency</label>
          <select
            id={`${id}-currency`}
            name="currency"
            value={currency}
            onChange={(event) => setCurrency(event.target.value)}
          >
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
            value={paymentId}
            onChange={(event) => setPaymentId(event.target.value)}
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
  const demoRun = useSettledDemoRun();
  return (
    <>
      <DefaultDemo
        id="routing"
        scenario="outage"
        title="Default demo: unknown outcome, failover blocked"
        description="Starts a simulated $100.00 USD payment whose provider outage leaves the outcome unknown. When it settles, the evaluator below is filled with that payment and re-run, so you can see why the router refuses to fail over to another provider."
        producesLabel="a payment stuck at its preferred provider"
      />
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
      <Evaluator demoRun={demoRun} />
    </>
  );
}
