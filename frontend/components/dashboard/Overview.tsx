"use client";

import { formatDurationMs } from "@/lib/format-duration";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useId } from "react";
import {
  PaymentVolumeChart,
  dataSourceLabels,
} from "@/components/charts/PaymentVolumeChart";
import { PaymentTable } from "@/components/payments/PaymentTable";
import {
  ArchitectureStrip,
  CredentialsCard,
  DemoRunCard,
  HubHeading,
  QuickActions,
} from "@/components/dashboard/Hub";
import { EmptyState } from "@/components/ui";
import { QueryState } from "@/components/ui/query-state";
import type { MetricSummary, MetricWindow } from "@/lib/api/schemas";
import {
  useDashboardSummary,
  usePaymentMethods,
  usePaymentVolume,
  usePayments,
} from "@/lib/hooks/payments";
import { formatAggregateMoney } from "@/lib/money";

const windows: { value: MetricWindow; label: string }[] = [
  { value: "24h", label: "Last 24 hours" },
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" },
];

function MetricCards({ summary }: { summary: MetricSummary }) {
  const source = dataSourceLabels[summary.data_source];
  return (
    <div className="metric-grid">
      <section className="metric-card" aria-labelledby="metric-volume">
        <h2 id="metric-volume">Accepted volume by currency</h2>
        {summary.volumes.length === 0 ? (
          <p className="no-observations">No observations</p>
        ) : (
          <ul className="volume-list">
            {summary.volumes.map((volume) => (
              <li key={volume.currency}>
                <span className="metric-value numeric">
                  {formatAggregateMoney(volume.amount_minor, volume.currency)}
                </span>
                <span className="text-label">
                  {" "}
                  · {volume.payment_count} payment
                  {volume.payment_count === 1 ? "" : "s"}
                </span>
              </li>
            ))}
          </ul>
        )}
        <span className="text-label">
          Per currency; never summed across currencies · {source}
        </span>
      </section>
      <section className="metric-card" aria-labelledby="metric-success">
        <h2 id="metric-success">Success rate</h2>
        {summary.success_rate_percent === null ? (
          <p className="no-observations">No observations</p>
        ) : (
          <p className="metric-value">{summary.success_rate_percent}%</p>
        )}
        <span className="text-label">
          Succeeded of {summary.terminal_count} succeeded or failed payment
          {summary.terminal_count === 1 ? "" : "s"} · {source}
        </span>
      </section>
      <section className="metric-card" aria-labelledby="metric-count">
        <h2 id="metric-count">Payments</h2>
        <p className="metric-value">{summary.payment_count}</p>
        <span className="text-label">
          Accepted in window ·{" "}
          {(summary.status_counts.unknown ?? 0) > 0
            ? `${summary.status_counts.unknown} outcome unknown`
            : "none with unknown outcome"}
        </span>
      </section>
      <section className="metric-card" aria-labelledby="metric-p95">
        <h2 id="metric-p95">Observed p95 completion</h2>
        {summary.completion_latency_p95_ms === null ? (
          <p className="no-observations">No observations</p>
        ) : (
          <p className="metric-value">
            {formatDurationMs(summary.completion_latency_p95_ms)}
          </p>
        )}
        <span className="text-label">
          {summary.completion_latency_samples} sample
          {summary.completion_latency_samples === 1 ? "" : "s"} · {source}
        </span>
      </section>
    </div>
  );
}

/** Lists unknown outcomes. It links to inspection only; no retry action. */
function NeedsAttention() {
  const { query } = usePayments({ status: "unknown", limit: "10" });
  const items = query.data?.data.items ?? [];
  if (items.length === 0) return null;
  return (
    <section
      className="attention-banner"
      role="status"
      aria-labelledby="attention-title"
    >
      <h2 id="attention-title">
        <span aria-hidden="true">! </span>Needs attention: {items.length}{" "}
        payment{items.length === 1 ? "" : "s"} with unknown outcome
        {query.data?.data.next_cursor ? " (showing the first 10)" : ""}
      </h2>
      <p>
        These outcomes are being checked through reconciliation. They are not
        retried or sent to another provider.
      </p>
      <ul>
        {items.map((payment) => (
          <li key={payment.id}>
            <Link
              className="text-link mono"
              href={`/dashboard/payments/${payment.id}`}
            >
              Inspect {payment.id}
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}

export function Overview() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const selectId = useId();
  const requested = searchParams.get("window");
  const window: MetricWindow =
    requested === "7d" || requested === "30d" ? requested : "24h";
  const summary = useDashboardSummary(window);
  const volume = usePaymentVolume(window);
  const recent = usePayments({ limit: "5" });
  const methods = usePaymentMethods();

  return (
    <>
      <HubHeading />
      <QuickActions />
      <div className="hub-columns">
        <CredentialsCard />
        <DemoRunCard />
      </div>
      <ArchitectureStrip />

      <section className="hub-metrics" aria-labelledby="hub-metrics-title">
        <div className="page-heading-actions section-title">
          <div>
            <h2 id="hub-metrics-title">Live sandbox metrics</h2>
            <p className="muted">
              Workspace metrics computed by the API from recorded sandbox
              payments. Missing statistics show “No observations”, never
              invented values.
            </p>
          </div>
          <div className="field">
            <label htmlFor={selectId}>Time range</label>
            <select
              id={selectId}
              value={window}
              onChange={(event) =>
                router.push(`${pathname}?window=${event.target.value}`)
              }
            >
              {windows.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </div>
        </div>

        <NeedsAttention />

        <QueryState
          query={summary.query}
          workspace={summary.workspace}
          loadingLabel="Loading metrics"
          lines={2}
        >
          {(result) => (
            <>
              <p className="source-line">
                Source: {dataSourceLabels[result.data.data_source]} · Window{" "}
                {result.data.window_start.slice(0, 16).replace("T", " ")} –{" "}
                {result.data.window_end.slice(0, 16).replace("T", " ")} UTC ·
                Updated {new Date(result.receivedAt).toLocaleTimeString()}
              </p>
              <MetricCards summary={result.data} />
            </>
          )}
        </QueryState>

        <QueryState
          query={volume.query}
          workspace={volume.workspace}
          loadingLabel="Loading payment volume"
        >
          {(result) => <PaymentVolumeChart volume={result.data} />}
        </QueryState>

        <section className="recent-payments" aria-labelledby="recent-title">
          <div className="section-title">
            <h2 id="recent-title">Recent payments</h2>
            <Link className="text-link" href="/dashboard/payments">
              View all payments
            </Link>
          </div>
          <QueryState
            query={recent.query}
            workspace={recent.workspace}
            loadingLabel="Loading recent payments"
          >
            {(result) =>
              result.data.items.length === 0 ? (
                <EmptyState title="No payments yet">
                  Create a sandbox payment from the Payments page.
                </EmptyState>
              ) : (
                <PaymentTable
                  payments={result.data.items}
                  methods={methods.query.data?.data.items}
                  label="Recent payments"
                />
              )
            }
          </QueryState>
        </section>
      </section>
    </>
  );
}
