import type { Metadata } from "next";
import { Suspense } from "react";
import { RoutingView } from "@/components/routing/RoutingView";
import { Skeleton } from "@/components/ui";

export const metadata: Metadata = { title: "Routing" };

export default function Page() {
  return (
    <>
      <div className="page-heading">
        <p className="eyebrow">Payment operations</p>
        <h1>Routing</h1>
        <p>
          Inspect routing rules and evaluate which provider a payment would
          reach, with the capability checks behind the decision.
        </p>
      </div>
      <Suspense fallback={<Skeleton label="Loading routing" lines={3} />}>
        <RoutingView />
      </Suspense>
    </>
  );
}
