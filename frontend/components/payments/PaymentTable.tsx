import Link from "next/link";
import { StatusPill, Table } from "@/components/ui";
import type { Payment, PaymentMethod } from "@/lib/api/schemas";
import { formatMoney } from "@/lib/money";

export function formatTimestamp(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return `${date.toISOString().slice(0, 16).replace("T", " ")} UTC`;
}

export function shortId(id: string): string {
  return id.length > 13 ? `${id.slice(0, 8)}…${id.slice(-4)}` : id;
}

/**
 * Payments in a labelled, keyboard-focusable scroll region. Wide content
 * scrolls inside the region so the page itself never scrolls horizontally.
 */
export function PaymentTable({
  payments,
  methods = [],
  label = "Payments",
  detailQuery = "",
}: {
  payments: Payment[];
  methods?: PaymentMethod[];
  label?: string;
  /** Current list query, preserved in detail links for back navigation. */
  detailQuery?: string;
}) {
  const rails = new Map(methods.map((method) => [method.id, method.rail]));
  return (
    <Table label={label} className="payment-table">
      <thead>
        <tr>
          <th scope="col">Payment</th>
          <th scope="col">Created</th>
          <th scope="col" className="numeric">
            Amount
          </th>
          <th scope="col">Rail</th>
          <th scope="col">Reference</th>
          <th scope="col">Status</th>
        </tr>
      </thead>
      <tbody>
        {payments.map((payment) => (
          <tr key={payment.id}>
            <td>
              <Link
                className="text-link mono payment-id"
                href={`/dashboard/payments/${payment.id}${detailQuery}`}
              >
                {payment.id}
              </Link>
            </td>
            <td className="nowrap">{formatTimestamp(payment.created_at)}</td>
            <td className="numeric nowrap">
              {formatMoney(payment.amount_minor, payment.currency)}
            </td>
            <td>{rails.get(payment.payment_method_id) ?? "—"}</td>
            <td className="reference">{payment.merchant_reference ?? "—"}</td>
            <td>
              <StatusPill status={payment.status} />
            </td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}
