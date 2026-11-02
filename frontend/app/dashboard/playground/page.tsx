import type { Metadata } from "next";
import { PlaygroundView } from "@/components/playground/PlaygroundView";

export const metadata: Metadata = { title: "Playground" };

export default function Page() {
  return (
    <>
      <div className="page-heading">
        <p className="eyebrow">Payment operations</p>
        <h1>Payment Failure &amp; Recovery Playground</h1>
        <p>
          Six one-click demos of what goes wrong in payments and how Railvanta
          stays correct, each backed by evidence from the real backend.
        </p>
      </div>
      <PlaygroundView />
    </>
  );
}
