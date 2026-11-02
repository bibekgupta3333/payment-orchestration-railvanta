/**
 * Human-readable duration for a latency in milliseconds. Values under one second
 * keep the exact "N ms" form; longer ones are shown as seconds, minutes or hours
 * so a payment that completed after an hour does not read as "4465210 ms".
 */
export function formatDurationMs(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return "—";
  if (ms < 1_000) return `${Math.round(ms)} ms`;
  const trim = (value: number) => value.toFixed(1).replace(/\.0$/, "");
  if (ms < 60_000) return `${trim(ms / 1_000)} s`;
  if (ms < 3_600_000) return `${trim(ms / 60_000)} min`;
  return `${trim(ms / 3_600_000)} h`;
}
