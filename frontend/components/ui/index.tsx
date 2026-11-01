import type {
  ButtonHTMLAttributes,
  HTMLAttributes,
  ReactNode,
  TableHTMLAttributes,
} from "react";

export function Button({
  className = "",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement>) {
  return <button className={`button ${className}`} {...props} />;
}

export function Badge({ children }: { children: ReactNode }) {
  return <span className="badge">{children}</span>;
}

export const paymentStatusLabels = {
  created: "Created",
  processing: "Processing",
  succeeded: "Succeeded",
  failed: "Failed",
  canceled: "Canceled",
  unknown: "Outcome unknown",
} as const;
export type PaymentStatusName = keyof typeof paymentStatusLabels;

const statusIcons: Record<PaymentStatusName, string> = {
  created: "○",
  processing: "◔",
  succeeded: "✓",
  failed: "✕",
  canceled: "⊘",
  unknown: "!",
};

/** Status text is always paired with an icon; color is never the only cue. */
export function StatusPill({ status }: { status: PaymentStatusName }) {
  return (
    <span className={`status-pill status-${status}`}>
      <span aria-hidden="true">{statusIcons[status]}</span>{" "}
      {paymentStatusLabels[status]}
    </span>
  );
}

export function Skeleton({
  label = "Loading",
  lines = 1,
}: {
  label?: string;
  lines?: number;
}) {
  return (
    <div role="status" className="skeleton-group" aria-live="polite">
      <span className="sr-only">{label}</span>
      {Array.from({ length: lines }, (_, index) => (
        <div key={index} className="skeleton" aria-hidden="true" />
      ))}
    </div>
  );
}

export function EmptyState({
  title,
  children,
  action,
}: {
  title: string;
  children?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <section className="state-panel">
      <h2>{title}</h2>
      {children && <p>{children}</p>}
      {action}
    </section>
  );
}

export function ErrorState({
  title = "Unable to load this view",
  message,
  requestId,
  retry,
}: {
  title?: string;
  message: string;
  requestId: string;
  retry?: () => void;
}) {
  return (
    <section role="alert" className="state-panel error-panel">
      <h2>{title}</h2>
      <p>{message}</p>
      <p className="muted">
        Request ID: <code className="mono">{requestId}</code>
      </p>
      {retry && <Button onClick={retry}>Retry</Button>}
    </section>
  );
}

/**
 * Shown when cached data is still displayed but the latest refresh failed.
 * The data is explicitly labelled as last-known, never as current.
 */
export function StaleNotice({
  updatedAt,
  requestId,
  retry,
}: {
  updatedAt: number;
  requestId: string;
  retry: () => void;
}) {
  return (
    <section role="alert" className="stale-notice">
      <p>
        <strong>Showing stale data.</strong> The latest refresh failed; values
        below are from {new Date(updatedAt).toLocaleTimeString()} and may no
        longer be current. Request ID: <code className="mono">{requestId}</code>
      </p>
      <Button className="button-quiet button-small" onClick={retry}>
        Retry
      </Button>
    </section>
  );
}

export function UnavailableState({
  title = "Unavailable until backend exists",
  children,
}: {
  title?: string;
  children?: ReactNode;
}) {
  return (
    <section className="unavailable state-panel">
      <div className="state-icon" aria-hidden="true">
        ◇
      </div>
      <p className="eyebrow">Implementation pending</p>
      <h2>{title}</h2>
      <p>
        {children ??
          "This capability is planned. No live payment data or actions are available in this foundation build."}
      </p>
      <span className="text-label">Sandbox only · No real funds</span>
    </section>
  );
}

export function Table({
  label,
  children,
  ...props
}: TableHTMLAttributes<HTMLTableElement> & { label: string }) {
  return (
    <div className="table-scroll" role="region" aria-label={label} tabIndex={0}>
      <table {...props}>{children}</table>
    </div>
  );
}

export function Card({
  className = "",
  ...props
}: HTMLAttributes<HTMLElement>) {
  return <section className={`card ${className}`} {...props} />;
}
