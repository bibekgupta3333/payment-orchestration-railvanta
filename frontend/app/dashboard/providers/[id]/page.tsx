import type { Metadata } from "next";
import Link from "next/link";
import { Suspense } from "react";
import { ProviderDetailView } from "@/components/provider-views/ProvidersView";
import { Skeleton } from "@/components/ui";

export const metadata: Metadata = { title: "Provider detail" };

export default async function DetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return (
    <>
      <Link href="/dashboard/providers" className="back-link">
        ← Providers
      </Link>
      <div className="page-heading">
        <p className="eyebrow">Payment operations</p>
        <h1>Provider details</h1>
      </div>
      <Suspense fallback={<Skeleton label="Loading provider" lines={4} />}>
        <ProviderDetailView id={id} />
      </Suspense>
    </>
  );
}
