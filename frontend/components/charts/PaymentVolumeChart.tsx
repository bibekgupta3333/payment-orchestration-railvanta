"use client";

import {
  Bar,
  BarChart,
  CartesianGrid,
  Legend,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { Table } from "@/components/ui";
import type { PaymentVolume } from "@/lib/api/schemas";
import { compareMinor, formatAggregateMoney, shareOfPeak } from "@/lib/money";

const palette = ["#08776f", "#3b5b8c", "#b45309", "#7c3aed", "#be123c"];

export const dataSourceLabels = {
  observed: "Observed sandbox API data",
  simulated: "Simulated data",
  seeded: "Seeded demo data",
} as const;

function bucketLabel(timestamp: number, bucket: PaymentVolume["bucket"]) {
  const iso = new Date(timestamp).toISOString();
  return bucket === "hour"
    ? `${iso.slice(5, 10)} ${iso.slice(11, 16)}`
    : iso.slice(0, 10);
}

/**
 * Accepted volume per currency. Bars show each bucket as a share of that
 * currency's own peak (an integer index computed with BigInt), so currencies
 * are never summed or put on one money axis and no amount passes through a
 * floating-point Number. Exact amounts are in the tooltip, summary and table.
 */
export function PaymentVolumeChart({ volume }: { volume: PaymentVolume }) {
  const currencies = [...new Set(volume.items.map((item) => item.currency))];
  const peaks = new Map<string, string>();
  const totals = new Map<string, bigint>();
  for (const item of volume.items) {
    const peak = peaks.get(item.currency);
    if (!peak || compareMinor(item.amount_minor, peak) > 0)
      peaks.set(item.currency, item.amount_minor);
    totals.set(
      item.currency,
      (totals.get(item.currency) ?? 0n) + BigInt(item.amount_minor),
    );
  }
  const rows = new Map<number, Record<string, string | number>>();
  for (const item of volume.items) {
    const row = rows.get(item.timestamp) ?? {
      label: bucketLabel(item.timestamp, volume.bucket),
    };
    row[item.currency] = shareOfPeak(
      item.amount_minor,
      peaks.get(item.currency) ?? "0",
    );
    row[`${item.currency}__exact`] = formatAggregateMoney(
      item.amount_minor,
      item.currency,
    );
    rows.set(item.timestamp, row);
  }
  const data = [...rows.entries()]
    .sort(([a], [b]) => a - b)
    .map(([, row]) => row);
  const source = dataSourceLabels[volume.data_source];
  const range = `${volume.window_start.slice(0, 16).replace("T", " ")} – ${volume.window_end.slice(0, 16).replace("T", " ")} UTC`;
  const summary =
    currencies.length === 0
      ? "No payments were accepted in this window."
      : currencies
          .map(
            (currency) =>
              `${formatAggregateMoney((totals.get(currency) ?? 0n).toString(), currency)} accepted`,
          )
          .join("; ") +
        ` across ${rows.size} ${volume.bucket === "hour" ? "hourly" : "daily"} bucket${rows.size === 1 ? "" : "s"} with activity. Currencies are reported separately.`;

  return (
    <figure className="card chart-card" aria-labelledby="volume-title">
      <div className="section-title">
        <h2 id="volume-title">Payment volume by currency</h2>
        <span className="badge">{source}</span>
      </div>
      <p className="text-label">
        Source: {source} · Range: {range} · Last updated{" "}
        {new Date(volume.generated_at).toLocaleTimeString()}
      </p>
      <p className="chart-summary">{summary}</p>
      {currencies.length > 0 && (
        <>
          <div className="chart-frame" aria-hidden="true">
            <ResponsiveContainer width="100%" height={240}>
              <BarChart
                data={data}
                accessibilityLayer={false}
                margin={{ top: 8, right: 8, left: 0, bottom: 8 }}
              >
                <CartesianGrid strokeDasharray="3 3" stroke="var(--line)" />
                <XAxis dataKey="label" tick={{ fontSize: 11 }} />
                <YAxis
                  domain={[0, 1000]}
                  tickFormatter={(value: number) => `${value / 10}%`}
                  tick={{ fontSize: 11 }}
                  width={44}
                />
                <Tooltip
                  formatter={(_value, name, item) => [
                    String(
                      (item.payload as Record<string, unknown>)[
                        `${String(name)}__exact`
                      ] ?? "—",
                    ),
                    String(name),
                  ]}
                />
                <Legend />
                {currencies.map((currency, index) => (
                  <Bar
                    key={currency}
                    dataKey={currency}
                    name={currency}
                    fill={palette[index % palette.length]}
                    isAnimationActive={false}
                  />
                ))}
              </BarChart>
            </ResponsiveContainer>
          </div>
          <p className="text-label">
            Bar height is each bucket&apos;s share of that currency&apos;s own
            peak; exact amounts are in the table.
          </p>
          <details className="chart-table">
            <summary>Show volume data table</summary>
            <Table label="Payment volume data">
              <thead>
                <tr>
                  <th scope="col">Bucket start (UTC)</th>
                  <th scope="col">Currency</th>
                  <th scope="col" className="numeric">
                    Accepted volume
                  </th>
                  <th scope="col" className="numeric">
                    Payments
                  </th>
                </tr>
              </thead>
              <tbody>
                {volume.items.map((item) => (
                  <tr key={`${item.timestamp}-${item.currency}`}>
                    <td>{bucketLabel(item.timestamp, volume.bucket)}</td>
                    <td>{item.currency}</td>
                    <td className="numeric">
                      {formatAggregateMoney(item.amount_minor, item.currency)}
                    </td>
                    <td className="numeric">{item.payment_count}</td>
                  </tr>
                ))}
              </tbody>
            </Table>
          </details>
        </>
      )}
    </figure>
  );
}
