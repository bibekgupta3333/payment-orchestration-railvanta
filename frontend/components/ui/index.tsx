import type { ButtonHTMLAttributes, HTMLAttributes, ReactNode, TableHTMLAttributes } from "react";

export function Button({ className = "", ...props }: ButtonHTMLAttributes<HTMLButtonElement>) {
  return <button className={`button ${className}`} {...props} />;
}

export function Badge({ children }: { children: ReactNode }) {
  return <span className="badge">{children}</span>;
}

export const paymentStatusLabels = {
  created: "Created", processing: "Processing", succeeded: "Succeeded",
  failed: "Failed", canceled: "Canceled", unknown: "Outcome unknown",
} as const;

export function StatusPill({ status }: { status: keyof typeof paymentStatusLabels }) {
  return <span className={`status-pill status-${status}`}><span aria-hidden="true">{status === "unknown" ? "!" : "•"}</span> {paymentStatusLabels[status]}</span>;
}

export function Skeleton({ label = "Loading" }: { label?: string }) {
  return <div role="status" className="skeleton"><span className="sr-only">{label}</span></div>;
}

export function EmptyState({ title, children }: { title: string; children?: ReactNode }) {
  return <section className="state-panel"><h2>{title}</h2>{children && <p>{children}</p>}</section>;
}

export function ErrorState({ message, requestId, retry }: { message: string; requestId: string; retry?: () => void }) {
  return <section role="alert" className="state-panel"><h2>Unable to load this view</h2><p>{message}</p><p className="muted">Request ID: {requestId}</p>{retry && <Button onClick={retry}>Retry read</Button>}</section>;
}

export function UnavailableState({ title = "Unavailable until backend exists", children }: { title?: string; children?: ReactNode }) {
  return <section className="unavailable state-panel"><div className="state-icon" aria-hidden="true">◇</div><p className="eyebrow">Implementation pending</p><h2>{title}</h2><p>{children ?? "This capability is planned. No live payment data or actions are available in this foundation build."}</p><span className="text-label">Sandbox only · No real funds</span></section>;
}

export function Table({ label, children, ...props }: TableHTMLAttributes<HTMLTableElement> & { label: string }) {
  return <div className="table-scroll" role="region" aria-label={label} tabIndex={0}><table {...props}>{children}</table></div>;
}

export function Card({ className = "", ...props }: HTMLAttributes<HTMLElement>) {
  return <section className={`card ${className}`} {...props} />;
}
