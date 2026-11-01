import type { Metadata } from "next";
import { Suspense } from "react";
import { EventExplorer } from "@/components/events/EventExplorer";
import { Skeleton } from "@/components/ui";

export const metadata: Metadata = { title: "Events" };

export default function EventsPage() {
  return (
    <Suspense fallback={<Skeleton label="Loading events" lines={3} />}>
      <EventExplorer />
    </Suspense>
  );
}
