"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState, type ReactNode } from "react";
import { Badge, Button } from "@/components/ui";
import { Dialog } from "@/components/ui/dialog";
import { ThemeSelect } from "@/components/theme";

export const navigation = [
  { href: "/dashboard", label: "Overview", icon: "◫" },
  { href: "/dashboard/payments", label: "Payments", icon: "↗" },
  { href: "/dashboard/routing", label: "Routing", icon: "⑂" },
  { href: "/dashboard/providers", label: "Providers", icon: "◇" },
  { href: "/dashboard/simulator", label: "Simulator", icon: "▷" },
  { href: "/dashboard/ledger", label: "Ledger", icon: "▤" },
  { href: "/dashboard/reconciliation", label: "Reconciliation", icon: "⇄" },
  { href: "/dashboard/events", label: "Events", icon: "≋" },
  { href: "/dashboard/settings", label: "Settings", icon: "⚙" },
  { href: "/docs", label: "API docs", icon: "⌘" },
] as const;

function Navigation({ onNavigate }: { onNavigate?: () => void }) {
  const pathname = usePathname();
  return (
    <nav aria-label="Main navigation">
      <ul>
        {navigation.map(({ href, label, icon }) => {
          const active =
            href === "/dashboard"
              ? pathname === href
              : pathname.startsWith(href);
          return (
            <li key={href}>
              <Link
                href={href}
                aria-current={active ? "page" : undefined}
                onClick={onNavigate}
              >
                <span className="nav-icon" aria-hidden="true">
                  {icon}
                </span>
                {label}
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}

export function AppShell({ children }: { children: ReactNode }) {
  const [menuOpen, setMenuOpen] = useState(false);
  return (
    <div className="app-shell">
      <aside className="desktop-sidebar">
        <Link href="/" className="brand">
          <span className="brand-mark" aria-hidden="true">
            ◇
          </span>
          Railvanta
        </Link>
        <p className="sidebar-label">Payment operations</p>
        <Navigation />
        <div className="sidebar-note">
          <span className="availability-dot" aria-hidden="true" />
          Foundation build
          <p>
            Sandbox infrastructure.
            <br />
            No real funds.
          </p>
        </div>
      </aside>
      <div className="app-body">
        <header className="topbar">
          <div className="topbar-context">
            <Button
              className="menu-trigger button-quiet"
              aria-label="Open navigation"
              aria-expanded={menuOpen}
              onClick={() => setMenuOpen(true)}
            >
              <span aria-hidden="true">☰</span>
            </Button>
            <span className="workspace-name">Sandbox workspace</span>
            <Badge>Sandbox</Badge>
          </div>
          <div className="topbar-actions">
            <ThemeSelect />
            <details className="account-menu">
              <summary>
                <span className="avatar" aria-hidden="true">
                  S
                </span>
                <span className="account-label">Account</span>
              </summary>
              <div className="account-panel">
                <strong>Local sandbox</strong>
                <p>
                  Authentication is managed by the API. No account actions are
                  available here yet.
                </p>
              </div>
            </details>
          </div>
        </header>
        <main id="main-content" className="workspace-content" tabIndex={-1}>
          {children}
        </main>
        <footer className="workspace-footer">
          <span>Railvanta · Foundation M1</span>
          <span>Sandbox only / No real funds</span>
        </footer>
      </div>
      <Dialog
        open={menuOpen}
        onClose={() => setMenuOpen(false)}
        title="Navigation menu"
        className="navigation-dialog"
      >
        <Link href="/" className="brand" onClick={() => setMenuOpen(false)}>
          Railvanta
        </Link>
        <p className="sidebar-label">Sandbox workspace</p>
        <Navigation onNavigate={() => setMenuOpen(false)} />
        <ThemeSelect />
      </Dialog>
    </div>
  );
}
