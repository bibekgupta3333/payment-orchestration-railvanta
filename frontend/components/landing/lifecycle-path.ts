/**
 * Pure model of the lifecycle animation: nodes, steps, per-use-case token
 * paths and the position of every token at time `t`. No WebGL or DOM, so it
 * is unit tested directly.
 */

export const NODES = [
  { id: "client", label: "Client", short: "Client" },
  { id: "api", label: "API", short: "API" },
  { id: "outbox", label: "Outbox", short: "Outbox" },
  { id: "router", label: "Router", short: "Router" },
  { id: "provider", label: "Provider", short: "Provider" },
  { id: "ledger", label: "Ledger", short: "Ledger" },
  { id: "recon", label: "Reconciliation", short: "Recon" },
] as const;

export const NODE_INDEX = {
  client: 0,
  api: 1,
  outbox: 2,
  router: 3,
  provider: 4,
  ledger: 5,
  recon: 6,
} as const;

/** One list item per node; the ordered list is the text alternative. */
export const STEPS = [
  {
    title: "Client sends the request",
    detail: "A sandbox payment request with an Idempotency-Key.",
  },
  {
    title: "API accepts it once",
    detail:
      "The key is stored with the request; a repeat replays the stored response.",
  },
  {
    title: "Outbox records the work",
    detail: "Acceptance and the dispatch job commit in one transaction.",
  },
  {
    title: "Dispatcher routes it",
    detail: "The rule engine picks a provider and records the reason.",
  },
  {
    title: "Provider is called outside any DB transaction",
    detail: "The deterministic sandbox mock provider answers.",
  },
  {
    title: "Ledger posts a balanced journal",
    detail: "Debits equal credits for every posted payment.",
  },
  {
    title: "Reconciliation compares the records",
    detail:
      "Provider records, payments and journals are compared; differences are recorded, never adjusted silently.",
  },
] as const;

export type UseCaseId = "happy" | "lost" | "duplicate";
export type Marker = "lost" | "unknown" | "lookup" | "replayed";
export type Tone = "normal" | "alert" | "replay" | "ok";

export const USE_CASES: {
  id: UseCaseId;
  label: string;
  caption: string;
}[] = [
  {
    id: "happy",
    label: "Happy path",
    caption:
      "The request is accepted once, routed, sent to the provider and posted to the ledger as a balanced journal.",
  },
  {
    id: "lost",
    label: "Lost response (unknown, then recovered)",
    caption:
      "The provider's response is lost, so the payment is marked Unknown. A lookup goes back to the same provider, never to a second one, and the outcome is then recorded.",
  },
  {
    id: "duplicate",
    label: "Duplicate request",
    caption:
      "A second request with the same Idempotency-Key gets the stored response replayed. No second payment is created.",
  },
];

export const MARKER_TEXT: Record<Marker, string> = {
  lost: "Response lost",
  unknown: "Unknown",
  lookup: "Lookup at the same provider",
  replayed: "Stored response replayed",
};

export interface Waypoint {
  /** Seconds since the loop started. */
  t: number;
  /** Position in node units (0 = Client, 4 = Provider); y in plate heights. */
  x: number;
  y: number;
  step: number;
  marker?: Marker;
  /** Overrides the tone implied by the marker (e.g. green once recovered). */
  tone?: Tone;
}

const wp = (
  t: number,
  x: number,
  step: number,
  y = 0,
  marker?: Marker,
  tone?: Tone,
): Waypoint => ({ t, x, y, step, marker, tone });

const MAIN_RUN: Waypoint[] = [
  wp(0, 0, 0),
  wp(1.2, 1, 1),
  wp(2.4, 2, 2),
  wp(3.6, 3, 3),
  wp(4.8, 4, 4),
  wp(6, 5, 5),
];

export const PATHS: Record<UseCaseId, Waypoint[][]> = {
  happy: [MAIN_RUN],
  lost: [
    [
      ...MAIN_RUN.slice(0, 5),
      wp(5.8, 4, 4, 0, "lost"),
      wp(7, 4, 4, 1.1, "unknown"),
      wp(8.2, 4, 4, 1.1, "unknown"),
      // The lookup arcs out and comes back down onto the SAME provider.
      wp(8.9, 4.6, 4, 0.8, "lookup", "ok"),
      wp(9.6, 4, 4, 0, "lookup", "ok"),
      wp(10.8, 5, 5, 0, undefined, "ok"),
    ],
  ],
  duplicate: [
    MAIN_RUN,
    [
      wp(6.4, 0, 0, 0, undefined, "replay"),
      wp(7.6, 1, 1, 0, "replayed", "replay"),
      wp(8.8, 0.35, 1, 0, "replayed", "replay"),
    ],
  ],
};

export const DURATION: Record<UseCaseId, number> = {
  happy: 7.2,
  lost: 12,
  duplicate: 10,
};

export interface TokenState {
  x: number;
  y: number;
  tone: Tone;
}

export interface Frame {
  tokens: TokenState[];
  /** Index into STEPS of the latest step reached, or -1 before the start. */
  step: number;
  marker: Marker | undefined;
  /** Node units the marker belongs to (for the HTML overlay). */
  markerX: number;
}

function tone(point: Waypoint): Tone {
  const { marker } = point;
  if (point.tone) return point.tone;
  if (marker === "lost" || marker === "unknown") return "alert";
  if (marker === "replayed") return "replay";
  return "normal";
}

function locate(path: Waypoint[], t: number) {
  if (t < path[0].t) return undefined;
  const last = path[path.length - 1];
  if (t >= last.t) return { x: last.x, y: last.y, from: last, to: last };
  let i = 0;
  while (path[i + 1].t <= t) i += 1;
  const from = path[i];
  const to = path[i + 1];
  const k = (t - from.t) / (to.t - from.t);
  const eased = k * k * (3 - 2 * k);
  return {
    x: from.x + (to.x - from.x) * eased,
    y: from.y + (to.y - from.y) * eased,
    from,
    to,
  };
}

export function frameAt(useCase: UseCaseId, time: number): Frame {
  const t =
    ((time % DURATION[useCase]) + DURATION[useCase]) % DURATION[useCase];
  const tokens: TokenState[] = [];
  let step = -1;
  let marker: Marker | undefined;
  let markerX = 0;
  let stepAt = -1;
  let markerAt = -1;
  for (const path of PATHS[useCase]) {
    const at = locate(path, t);
    if (!at) continue;
    const reached = at.from;
    tokens.push({ x: at.x, y: at.y, tone: tone(reached) });
    if (reached.t > stepAt) {
      stepAt = reached.t;
      step = reached.step;
    }
    if (reached.marker && reached.t > markerAt) {
      markerAt = reached.t;
      marker = reached.marker;
      markerX = reached.x;
    }
  }
  return { tokens, step, marker, markerX };
}

/** The frame shown when motion is reduced: every use case ends complete. */
export function staticFrame(useCase: UseCaseId): Frame {
  return frameAt(useCase, DURATION[useCase] - 0.001);
}

/** Half the visible width, in node units, at the track's depth. */
export const VIEW_HALF = 4;

/**
 * Horizontal screen position (percent) of a node-unit x. The camera sits on
 * x = 0 and only tilts vertically, so the mapping is exactly linear and the
 * HTML labels line up with the 3D nodes.
 */
export const screenPercent = (x: number) =>
  `${50 + ((x + 0.5 - NODES.length / 2) / VIEW_HALF) * 50}%`;
