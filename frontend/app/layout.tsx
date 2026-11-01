import type { Metadata } from "next";
import { Providers } from "@/components/providers";
import "@fontsource-variable/inter";
import "./globals.css";
import "./events-docs.css";

export const metadata: Metadata = {
  title: {
    default: "Railvanta — Sandbox payment infrastructure",
    template: "%s | Railvanta",
  },
  description:
    "A sandbox for explainable payment routing, idempotency, and financial correctness. Sandbox only; no real funds.",
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body>
        <a className="skip-link" href="#main-content">
          Skip to main content
        </a>
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
