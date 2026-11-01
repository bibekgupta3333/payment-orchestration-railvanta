"use client";

import { formatDurationMs } from "@/lib/format-duration";
import Link from "next/link";
import { Card, EmptyState, Table } from "@/components/ui";
import { QueryState } from "@/components/ui/query-state";
import type { Provider } from "@/lib/api/routing-schemas";
import {
  useProviderDetail,
  useProviderHealth,
  useProviders,
} from "@/lib/hooks/routing";
import { HealthBadge, ModeBadge } from "./HealthBadge";

function Capabilities({ provider }: { provider: Provider }) {
  if (!provider.capabilities) {
    return (
      <p className="muted small">
        No adapter is configured in this deployment, so no capabilities are
        declared and payments cannot be routed here.
      </p>
    );
  }
  const { capabilities } = provider;
  return (
    <dl className="detail-list">
      <div>
        <dt>Idempotent replay</dt>
        <dd>
          {capabilities.idempotent_replay ? "Supported" : "Not supported"}
        </dd>
      </div>
      <div>
        <dt>Lookup by idempotency key</dt>
        <dd>
          {capabilities.lookup_by_idempotency_key
            ? "Supported"
            : "Not supported"}
        </dd>
      </div>
      <div>
        <dt>Rails</dt>
        <dd>{capabilities.rails.join(", ") || "None declared"}</dd>
      </div>
    </dl>
  );
}

export function ProvidersView() {
  const providers = useProviders();
  const health = useProviderHealth();
  return (
    <QueryState
      query={providers.query}
      workspace={providers.workspace}
      loadingLabel="Loading providers"
    >
      {(result) => (
        <>
          <p className="sandbox-notice" role="note">
            Mock is a deterministic simulator and its data is labelled
            Simulated. Stripe and Plaid have no adapter in this build and are
            Not configured.
          </p>
          <div className="capability-grid">
            {result.data.items.map((provider) => {
              const item = health.query.data?.data.items.find(
                (candidate) => candidate.code === provider.code,
              );
              return (
                <Card
                  key={provider.code}
                  aria-labelledby={`p-${provider.code}`}
                >
                  <div className="card-heading">
                    <h2 id={`p-${provider.code}`}>
                      <Link href={`/dashboard/providers/${provider.id}`}>
                        {provider.display_name}
                      </Link>
                    </h2>
                    <ModeBadge mode={provider.mode} />
                  </div>
                  <Capabilities provider={provider} />
                  <p className="small">
                    {provider.enabled ? "Enabled" : "Disabled"} ·{" "}
                    {provider.rule_count} routing rule
                    {provider.rule_count === 1 ? "" : "s"}
                  </p>
                  <p>
                    {item ? (
                      <HealthBadge health={item} />
                    ) : health.query.isError ? (
                      <span className="muted small">
                        Health could not be loaded; no status is shown.
                      </span>
                    ) : (
                      <span className="muted small">Loading health</span>
                    )}
                  </p>
                </Card>
              );
            })}
          </div>
        </>
      )}
    </QueryState>
  );
}

function formatMs(value: number | null): string {
  return value === null ? "No samples" : formatDurationMs(value);
}

export function ProviderDetailView({ id }: { id: string }) {
  const detail = useProviderDetail(id);
  return (
    <QueryState
      query={detail.query}
      workspace={detail.workspace}
      loadingLabel="Loading provider"
    >
      {(result) => {
        const { provider, health, rules } = result.data;
        return (
          <>
            <div className="card-heading">
              <h2>{provider.display_name}</h2>
              <ModeBadge mode={provider.mode} />
            </div>
            <div className="detail-grid">
              <Card aria-labelledby="capabilities-heading">
                <h2 id="capabilities-heading">Capabilities</h2>
                <Capabilities provider={provider} />
              </Card>
              <Card aria-labelledby="health-heading">
                <h2 id="health-heading">Health</h2>
                <p>
                  <HealthBadge health={health} />
                </p>
                <p className="muted small">
                  Computed from attempt submitted and completed times.
                  Unresolved (unknown) attempts count against the success rate.
                </p>
              </Card>
            </div>
            <h2>Health by window</h2>
            {health.observation_count === 0 ? (
              <EmptyState title="No observations">
                No attempts have been sent to this provider in the last 7 days,
                so no rate or latency is shown.
              </EmptyState>
            ) : (
              <Table label="Provider health windows">
                <thead>
                  <tr>
                    <th scope="col">Window</th>
                    <th scope="col">Observations</th>
                    <th scope="col">Success rate</th>
                    <th scope="col">p95 latency</th>
                    <th scope="col">Errors</th>
                  </tr>
                </thead>
                <tbody>
                  {health.windows.map((window) => (
                    <tr key={window.window}>
                      <th scope="row">{window.window}</th>
                      <td className="numeric">{window.observations}</td>
                      <td className="numeric">
                        {window.success_rate_percent === null
                          ? "No observations"
                          : `${window.success_rate_percent}%`}
                      </td>
                      <td className="numeric">{formatMs(window.p95_ms)}</td>
                      <td>
                        {Object.entries(window.error_counts)
                          .map(([code, count]) => `${code}: ${count}`)
                          .join(", ") || "None"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
            <h2>Routing rules</h2>
            {rules.length === 0 ? (
              <EmptyState title="No routing rules">
                No rule targets this provider, so nothing is routed to it.
              </EmptyState>
            ) : (
              <Table label="Rules for this provider">
                <thead>
                  <tr>
                    <th scope="col">Priority</th>
                    <th scope="col">Rail</th>
                    <th scope="col">Currency</th>
                    <th scope="col">Amount range (minor units)</th>
                    <th scope="col">Enabled</th>
                  </tr>
                </thead>
                <tbody>
                  {rules.map((rule) => (
                    <tr key={rule.id}>
                      <td className="numeric">{rule.priority}</td>
                      <td>{rule.rail}</td>
                      <td>{rule.currency}</td>
                      <td className="numeric">
                        {rule.min_amount_minor} –{" "}
                        {rule.max_amount_minor ?? "unbounded"}
                      </td>
                      <td>{rule.enabled ? "Yes" : "No"}</td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
          </>
        );
      }}
    </QueryState>
  );
}
