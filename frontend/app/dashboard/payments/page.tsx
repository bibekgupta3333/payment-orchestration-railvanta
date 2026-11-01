import type { Metadata } from "next";
import { Suspense } from "react";
import { PaymentExplorer } from "@/components/payments/PaymentExplorer";
import { Skeleton } from "@/components/ui";

export const metadata: Metadata = { title: "Payments" };

export default function PaymentsPage() {
  return (
    <Suspense fallback={<Skeleton label="Loading payments" lines={3} />}>
      <PaymentExplorer />
    </Suspense>
  );
}
