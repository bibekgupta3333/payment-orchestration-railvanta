import type { Metadata } from "next";
import { Suspense } from "react";
import { PaymentDetail } from "@/components/payments/PaymentDetail";
import { Skeleton } from "@/components/ui";

export const metadata: Metadata = { title: "Payment detail" };

export default async function PaymentDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return (
    <Suspense fallback={<Skeleton label="Loading payment" lines={4} />}>
      <PaymentDetail id={id} />
    </Suspense>
  );
}
