import type { Metadata } from "next";
import { Suspense } from "react";
import { ReconciliationView } from "@/components/reconciliation/ReconciliationView";
import { Skeleton } from "@/components/ui";

export const metadata: Metadata = { title: "Reconciliation" };

export default function ReconciliationPage() {
  return (
    <Suspense fallback={<Skeleton label="Loading reconciliation" lines={3} />}>
      <ReconciliationView />
    </Suspense>
  );
}
