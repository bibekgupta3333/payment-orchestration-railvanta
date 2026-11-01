import type { Metadata } from "next";
import { Suspense } from "react";
import { ProvidersView } from "@/components/provider-views/ProvidersView";
import { Skeleton } from "@/components/ui";

export const metadata: Metadata = { title: "Providers" };

export default function Page() {
  return (
    <>
      <div className="page-heading">
        <p className="eyebrow">Payment operations</p>
        <h1>Providers</h1>
        <p>
          Inspect declared capabilities and measured health. Health is shown
          only when attempts exist to measure.
        </p>
      </div>
      <Suspense fallback={<Skeleton label="Loading providers" lines={3} />}>
        <ProvidersView />
      </Suspense>
    </>
  );
}
