import type { ReactNode } from "react";
import { UnavailableState } from "@/components/ui";

export function FeaturePage({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children?: ReactNode;
}) {
  return (
    <>
      <div className="page-heading">
        <p className="eyebrow">Payment operations</p>
        <h1>{title}</h1>
        <p>{description}</p>
      </div>
      {children}
      <UnavailableState>
        Unavailable until backend exists for this capability. The foundation API
        is available, but payment execution and financial operations are not
        implemented. This view will show verified API data when that work is
        complete.
      </UnavailableState>
    </>
  );
}
