import type { ProviderHealth } from "@/lib/api/routing-schemas";

const sourceLabels = {
  observed: "Observed",
  simulated: "Simulated data",
  seeded: "Seeded data",
} as const;

/**
 * Health is shown only with evidence. With no observations the badge says so
 * instead of showing a zero rate, and a small sample never reads as healthy.
 * Status is text plus an icon; color is never the only cue.
 */
export function HealthBadge({ health }: { health: ProviderHealth }) {
  const source = sourceLabels[health.data_source];
  if (health.status === "no_observations") {
    return (
      <span className="status-pill health-none">
        <span aria-hidden="true">○</span> No observations
      </span>
    );
  }
  if (health.status === "insufficient_data") {
    return (
      <span className="status-pill health-none">
        <span aria-hidden="true">◔</span> Not enough evidence ·{" "}
        {health.observation_count} in 7d · {source}
      </span>
    );
  }
  const icon = { healthy: "✓", degraded: "!", unhealthy: "✕" }[health.status];
  const label = {
    healthy: "Healthy (24h)",
    degraded: "Degraded (24h)",
    unhealthy: "Unhealthy (24h)",
  }[health.status];
  return (
    <span className={`status-pill health-${health.status}`}>
      <span aria-hidden="true">{icon}</span> {label} · {source}
    </span>
  );
}

export function ModeBadge({
  mode,
}: {
  mode: "simulated" | "configured" | "not_configured";
}) {
  const label = {
    simulated: "Simulated",
    configured: "Configured",
    not_configured: "Not configured",
  }[mode];
  return <span className={`badge badge-mode-${mode}`}>{label}</span>;
}
