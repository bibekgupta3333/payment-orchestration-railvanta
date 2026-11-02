"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui";
import { LifecycleScene } from "./LifecycleScene";
import {
  MARKER_TEXT,
  NODES,
  STEPS,
  USE_CASES,
  screenPercent,
  type Frame,
  type UseCaseId,
} from "./lifecycle-path";

const ANNOUNCE_EVERY_MS = 3000;
const leftPercent = screenPercent;

/** Static labels over the canvas (or the fallback); decorative like the canvas. */
function NodeLabels() {
  return (
    <div className="lifecycle-labels" aria-hidden="true">
      {NODES.map((node, index) => (
        <span key={node.id} style={{ left: leftPercent(index) }}>
          <b>{index + 1}</b>
          <i>{node.short}</i>
        </span>
      ))}
    </div>
  );
}

/** Same flow without WebGL: plain CSS boxes and arrows. */
function FlowFallback() {
  return (
    <div className="lifecycle-fallback" data-testid="lifecycle-fallback">
      {NODES.map((node, index) => (
        <span key={node.id}>
          {index > 0 && <em>→</em>}
          <b>{node.short}</b>
        </span>
      ))}
    </div>
  );
}

export function Lifecycle() {
  const [useCase, setUseCase] = useState<UseCaseId>("happy");
  const [playing, setPlaying] = useState(true);
  const [reduced, setReduced] = useState<boolean | undefined>(undefined);
  const [unavailable, setUnavailable] = useState(false);
  const [frame, setFrame] = useState<Frame | undefined>(undefined);
  const [announcement, setAnnouncement] = useState("");
  const latest = useRef<Frame | undefined>(undefined);
  const announced = useRef<string>("");

  useEffect(() => {
    const query = window.matchMedia("(prefers-reduced-motion: reduce)");
    const update = () => setReduced(query.matches);
    update();
    query.addEventListener?.("change", update);
    return () => query.removeEventListener?.("change", update);
  }, []);

  const onFrame = useCallback((next: Frame) => {
    latest.current = next;
    setFrame(next);
  }, []);
  const onUnavailable = useCallback(() => setUnavailable(true), []);

  const animating = reduced === false && playing && !unavailable;
  useEffect(() => {
    if (!animating) return;
    const id = window.setInterval(() => {
      const current = latest.current;
      if (!current || current.step < 0) return;
      const text = `Step ${current.step + 1} of ${STEPS.length}: ${STEPS[current.step].title}${
        current.marker ? `. ${MARKER_TEXT[current.marker]}` : ""
      }`;
      if (text === announced.current) return;
      announced.current = text;
      setAnnouncement(text);
    }, ANNOUNCE_EVERY_MS);
    return () => window.clearInterval(id);
  }, [animating]);

  const chosen = USE_CASES.find((item) => item.id === useCase) ?? USE_CASES[0];
  const still = reduced !== false || unavailable;
  const step = still ? -1 : (frame?.step ?? -1);
  const marker = still ? undefined : frame?.marker;

  return (
    <section className="lifecycle" aria-labelledby="lifecycle-title">
      <div className="section-title">
        <span className="eyebrow">Payment lifecycle</span>
        <span className="text-label">Sandbox · simulated, no real funds</span>
      </div>
      <h2 id="lifecycle-title">From request to balanced ledger</h2>
      <div className="lifecycle-grid">
        <div className="lifecycle-visual" aria-hidden="true">
          {unavailable ? (
            <FlowFallback />
          ) : (
            reduced !== undefined && (
              <LifecycleScene
                useCase={useCase}
                playing={playing}
                reducedMotion={reduced}
                onFrame={onFrame}
                onUnavailable={onUnavailable}
              />
            )
          )}
          {!unavailable && <NodeLabels />}
          {marker && frame && (
            <span
              className={`lifecycle-marker lifecycle-marker-${marker}`}
              style={{ left: leftPercent(frame.markerX) }}
            >
              {MARKER_TEXT[marker]}
            </span>
          )}
        </div>
        <div className="lifecycle-copy">
          <div role="group" aria-label="Use case" className="segmented">
            {USE_CASES.map((item) => (
              <button
                key={item.id}
                type="button"
                aria-pressed={item.id === useCase}
                onClick={() => {
                  setUseCase(item.id);
                  setAnnouncement(`${item.label}. ${item.caption}`);
                  announced.current = "";
                }}
              >
                {item.label}
              </button>
            ))}
          </div>
          <p className="lifecycle-caption" data-testid="lifecycle-caption">
            {chosen.caption}
          </p>
          {!still && (
            <Button
              type="button"
              className="button-quiet button-small"
              onClick={() => setPlaying((value) => !value)}
            >
              {playing ? "Pause animation" : "Play animation"}
            </Button>
          )}
          <ol className="lifecycle-steps" aria-label="Payment lifecycle steps">
            {STEPS.map((item, index) => (
              <li
                key={item.title}
                aria-current={step === index ? "step" : undefined}
                className={step === index ? "is-current" : undefined}
              >
                <strong>{item.title}</strong>
                <span>{item.detail}</span>
              </li>
            ))}
          </ol>
          <div className="sr-only" role="status" aria-live="polite">
            {announcement}
          </div>
        </div>
      </div>
    </section>
  );
}
