import { AppShell } from "@/components/layout/app-shell";
import { SandboxSessionGuard } from "@/components/layout/SandboxSessionGuard";

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <AppShell>
      <SandboxSessionGuard>{children}</SandboxSessionGuard>
    </AppShell>
  );
}
