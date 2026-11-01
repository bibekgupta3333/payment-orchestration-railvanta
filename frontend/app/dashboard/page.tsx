import { Suspense } from "react";
import { Overview } from "@/components/dashboard/Overview";
import { Skeleton } from "@/components/ui";

export default function OverviewPage() {
  return (
    <Suspense fallback={<Skeleton label="Loading overview" lines={3} />}>
      <Overview />
    </Suspense>
  );
}
