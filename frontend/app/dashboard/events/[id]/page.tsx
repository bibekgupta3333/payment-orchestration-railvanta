import type { Metadata } from "next";
import { Suspense } from "react";
import { EventDetailPage } from "@/components/events/EventDetail";
import { Skeleton } from "@/components/ui";

export const metadata: Metadata = { title: "Event details" };

export default async function EventDetailRoute({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return (
    <Suspense fallback={<Skeleton label="Loading event" lines={4} />}>
      <EventDetailPage id={id} />
    </Suspense>
  );
}
