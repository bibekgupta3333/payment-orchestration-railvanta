import type { Metadata } from "next";
import { Suspense } from "react";
import { LedgerExplorer } from "@/components/ledger/LedgerExplorer";
import { Skeleton } from "@/components/ui";

export const metadata: Metadata = { title: "Ledger" };

export default function LedgerPage() {
  return (
    <Suspense fallback={<Skeleton label="Loading ledger" lines={3} />}>
      <LedgerExplorer />
    </Suspense>
  );
}
