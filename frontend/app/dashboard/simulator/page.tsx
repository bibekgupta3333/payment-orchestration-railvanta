import type { Metadata } from "next";
import { Suspense } from "react";
import { Skeleton } from "@/components/ui";
import { SimulatorView } from "@/components/simulator/SimulatorView";

export const metadata: Metadata = { title: "Simulator" };

export default function Page() {
  return (
    <>
      <div className="page-heading">
        <p className="eyebrow">Payment operations</p>
        <h1>Simulator</h1>
        <p>
          Reproduce deterministic provider faults through the real payment
          pipeline and inspect the invariants the backend checks.
        </p>
      </div>
      <Suspense fallback={<Skeleton label="Loading simulator" lines={3} />}>
        <SimulatorView />
      </Suspense>
    </>
  );
}
