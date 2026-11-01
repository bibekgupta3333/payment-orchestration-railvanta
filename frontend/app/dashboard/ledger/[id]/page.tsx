import type { Metadata } from "next";
import { Suspense } from "react";
import { JournalDetail } from "@/components/ledger/JournalDetail";
import { Skeleton } from "@/components/ui";

export const metadata: Metadata = { title: "Journal detail" };

export default async function JournalDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return (
    <Suspense fallback={<Skeleton label="Loading journal" lines={4} />}>
      <JournalDetail id={id} />
    </Suspense>
  );
}
