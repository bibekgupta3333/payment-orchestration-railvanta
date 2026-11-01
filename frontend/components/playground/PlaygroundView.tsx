"use client";

import { useRef, useState } from "react";
import { Button, Card, Skeleton } from "@/components/ui";
import { QueryState } from "@/components/ui/query-state";
import { CreatePaymentDialog } from "@/components/payments/CreatePaymentDialog";
import { simulatedLabel } from "@/components/simulator/SimulatorView";
import { usePaymentMethods } from "@/lib/hooks/payments";
import { demos } from "@/lib/hooks/playground";
import { useScenarios } from "@/lib/hooks/routing";
import { DemoCard, type RunSignal, type SettledRun } from "./DemoCard";
import { RoutingDemo } from "./RoutingDemo";

function LivePaymentForm() {
  const [open, setOpen] = useState(false);
  const methods = usePaymentMethods();
  const items = methods.query.data?.data.items ?? [];
  const firstMock = items.find((method) => method.rail === "mock") ?? items[0];
  return (
    <div className="mt-3">
      <Button
        className="button-quiet"
        onClick={() => setOpen(true)}
        disabled={items.length === 0}
      >
        Create $100 payment
      </Button>
      <p className="muted small">
        Pick a sandbox method, then follow created → processing → succeeded on
        the payment page (provider, latency and journal are shown there).
      </p>
      <CreatePaymentDialog
        open={open}
        onClose={() => setOpen(false)}
        methods={items}
        resume={
          firstMock
            ? {
                amount_minor: "10000",
                currency: "USD",
                payment_method_id: firstMock.id,
                merchant_reference: "playground-demo",
              }
            : null
        }
      />
    </div>
  );
}

export function PlaygroundView() {
  const scenarios = useScenarios();
  const items = scenarios.query.data?.data.items;
  const ids = items ? new Set(items.map((item) => item.id)) : null;
  const implemented = (id: string) =>
    items?.find((item) => item.id === id)?.implemented ?? false;

  const [signal, setSignal] = useState<RunSignal | null>(null);
  const [running, setRunning] = useState(false);
  const queue = useRef<string[]>([]);
  const nonce = useRef(0);
  const [message, setMessage] = useState("");

  const sequence = demos.filter((demo) => demo.inRunAll).map((demo) => demo.id);

  const signalNext = () => {
    nonce.current += 1;
    setSignal({ id: queue.current[0], nonce: nonce.current });
  };

  const startAll = () => {
    queue.current = [...sequence];
    setRunning(true);
    signalNext();
  };

  const onSettled = ({ demoId, ok, run }: SettledRun) => {
    const passed = run.invariants.filter((i) => i.status === "pass").length;
    const title = demos.find((demo) => demo.id === demoId)?.title ?? demoId;
    setMessage(
      `${title} ${ok ? "finished" : "did not pass"}: ${passed} of ${run.invariants.length} invariants passed.`,
    );
    if (queue.current[0] !== demoId) return;
    queue.current = queue.current.slice(1);
    if (!ok || queue.current.length === 0) {
      queue.current = [];
      setRunning(false);
      if (!ok) setMessage(`Run all stopped at ${title}: it did not pass.`);
      return;
    }
    signalNext();
  };

  const runAllDisabled =
    !ids ||
    sequence.some((id) => {
      const scenario = demos.find((demo) => demo.id === id)?.scenario;
      return scenario ? !ids.has(scenario) || !implemented(scenario) : false;
    });

  return (
    <>
      <div className="simulated-banner" role="note">
        <strong>{simulatedLabel}</strong>
        <span>
          Every result below is read from the real Railvanta backend running
          against the deterministic Mock provider. No real funds move.
        </span>
      </div>
      <p
        role="status"
        aria-live="polite"
        className="sr-only"
        data-testid="announcer"
      >
        {message}
      </p>
      <Card aria-labelledby="runall-title">
        <h2 id="runall-title">Guided tour</h2>
        <p className="muted">
          Run demos 1, 2, 3, 5 and 6 in order; it stops at the first failure.
          Demo 4 is an interactive evaluator.
        </p>
        <Button onClick={startAll} disabled={running || runAllDisabled}>
          {running ? "Running all…" : "Run all in order"}
        </Button>
        {runAllDisabled && ids && (
          <p className="muted small" role="note">
            Disabled: some backend scenarios are not available yet.
          </p>
        )}
      </Card>
      <QueryState
        query={scenarios.query}
        workspace={scenarios.workspace}
        loadingLabel="Loading scenarios"
      >
        {() => (
          <div className="grid gap-4 lg:grid-cols-2">
            {demos.map((demo) => (
              <DemoCard
                key={demo.id}
                demo={demo}
                scenarioIds={ids}
                scenarioImplemented={implemented}
                signal={signal}
                onSettled={onSettled}
                disabled={running}
              >
                {demo.id === "live" && <LivePaymentForm />}
                {demo.id === "routing" && <RoutingDemo />}
              </DemoCard>
            ))}
          </div>
        )}
      </QueryState>
      {!items && !scenarios.query.isError && <Skeleton label="Loading demos" />}
    </>
  );
}
