import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Home from "@/app/page";
import { Lifecycle } from "@/components/landing/Lifecycle";
import { SandboxStart } from "@/components/landing/SandboxStart";
import {
  DURATION,
  NODE_INDEX,
  PATHS,
  frameAt,
  staticFrame,
} from "@/components/landing/lifecycle-path";

const three = vi.hoisted(() => ({
  webgl: true,
  render: vi.fn(),
  dispose: vi.fn(),
}));

vi.mock("three", () => {
  const geometry = class {
    dispose = three.dispose;
  };
  class Material {
    color = { set: vi.fn() };
    emissive = { set: vi.fn() };
    emissiveIntensity = 0;
    dispose = three.dispose;
  }
  class Mesh {
    position = { x: 0, z: 0, set: vi.fn() };
    scale = { setScalar: vi.fn() };
    children: Mesh[] = [];
    visible = true;
    material = new Material();
    rotation = { z: 0 };
    add(child: Mesh) {
      this.children.push(child);
    }
  }
  class WebGLRenderer {
    domElement = document.createElement("canvas");
    constructor() {
      if (!three.webgl) throw new Error("WebGL unavailable");
    }
    setPixelRatio = vi.fn();
    setSize = vi.fn();
    render = three.render;
    dispose = three.dispose;
  }
  return {
    WebGLRenderer,
    Scene: class {
      add = vi.fn();
    },
    PerspectiveCamera: class {
      position = { set: vi.fn() };
      aspect = 1;
      lookAt = vi.fn();
      updateProjectionMatrix = vi.fn();
    },
    AmbientLight: class {},
    DirectionalLight: class {
      position = { set: vi.fn() };
    },
    Vector3: class {},
    CubicBezierCurve3: class {},
    GridHelper: class {
      position = { y: 0 };
      geometry = { dispose: three.dispose };
      material = new Material();
    },
    CylinderGeometry: geometry,
    TubeGeometry: geometry,
    ConeGeometry: geometry,
    BoxGeometry: geometry,
    SphereGeometry: geometry,
    MeshStandardMaterial: Material,
    Mesh,
  };
});

vi.mock("three/examples/jsm/geometries/RoundedBoxGeometry.js", () => ({
  RoundedBoxGeometry: class {
    dispose = three.dispose;
  },
}));

const SECRET = "rvk_1a2b3c4d.0123456789abcdef0123456789abcdef0123456789abcdef";
const METHODS = [
  { id: "pm_first", rail: "card", brand: "visa", last_four: "4242" },
  { id: "pm_success", rail: "card", brand: "visa", last_four: "0121" },
];

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json", "x-request-id": "req_t" },
  });
}

interface Call {
  method: string;
  path: string;
  init?: RequestInit;
}

function mockApi(publicAccess: boolean | "unavailable") {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      const method = (init?.method ?? "GET").toUpperCase();
      calls.push({ method, path, init });
      if (path === "/v1/sandbox/status") {
        if (publicAccess === "unavailable") return json({}, 404);
        return json({ public_access: publicAccess, workspace_name: "Demo" });
      }
      if (path === "/v1/sandbox/session")
        return json(
          {
            workspace: { id: "w1", name: "Demo", environment: "sandbox" },
            role: "operator",
            expires_at: "2026-11-02T00:00:00Z",
          },
          201,
        );
      if (path === "/v1/api-keys")
        return json(
          {
            key: {
              id: "k1",
              name: "Landing demo key",
              prefix: "rvk_1a2b3c4d",
              role: "operator",
              created_at: "2026-11-01T00:00:00Z",
              expires_at: "2026-11-02T00:00:00Z",
              revoked_at: null,
              status: "active",
            },
            secret: SECRET,
          },
          201,
        );
      if (path === "/v1/payment-methods")
        return json({ items: METHODS, next_cursor: null });
      if (path === "/v1/payments")
        return json({ id: "pay_123", status: "processing" }, 202);
      return json({}, 404);
    }),
  );
  return calls;
}

function stubObservers() {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
}

function reduceMotion(reduce: boolean) {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: reduce && query.includes("reduce"),
    media: query,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
}

beforeEach(() => {
  three.webgl = true;
  three.render.mockClear();
  three.dispose.mockClear();
  stubObservers();
});
afterEach(() => vi.unstubAllGlobals());

describe("lifecycle path model", () => {
  it("starts at the client and ends at the ledger on the happy path", () => {
    expect(frameAt("happy", 0).tokens[0].x).toBe(NODE_INDEX.client);
    const end = staticFrame("happy");
    expect(end.tokens[0].x).toBe(NODE_INDEX.ledger);
    expect(end.step).toBe(NODE_INDEX.ledger);
  });

  it("parks a lost response as Unknown and looks up the same provider", () => {
    const [path] = PATHS.lost;
    const markers = path.map((p) => p.marker);
    expect(markers).toContain("lost");
    expect(markers).toContain("unknown");
    expect(markers).toContain("lookup");
    // Every waypoint from the provider onwards until the lookup stays on the
    // single provider node; there is no second provider on the path.
    const lookup = path.map((p) => p.marker).lastIndexOf("lookup");
    // The lookup arcs out but lands exactly on the single provider node.
    expect(path[lookup].x).toBe(NODE_INDEX.provider);
    expect(path[lookup].y).toBe(0);
    const providerVisits = path.filter(
      (p) => p.x === NODE_INDEX.provider && p.y === 0 && !p.marker,
    );
    expect(providerVisits).toHaveLength(1);
    expect(
      Math.max(...path.slice(0, lookup + 1).map((p) => Math.floor(p.x))),
    ).toBe(NODE_INDEX.provider);
    expect(frameAt("lost", 9.6).tokens[0].tone).toBe("ok");
    expect(path[path.length - 1].x).toBe(NODE_INDEX.ledger);
    expect(frameAt("lost", 8).marker).toBe("unknown");
    expect(frameAt("lost", 8).tokens[0].tone).toBe("alert");
  });

  it("never lets the duplicate reach the outbox", () => {
    const duplicate = PATHS.duplicate[1];
    expect(Math.max(...duplicate.map((p) => p.x))).toBeLessThan(
      NODE_INDEX.outbox,
    );
    expect(duplicate.some((p) => p.marker === "replayed")).toBe(true);
    expect(frameAt("duplicate", 7.7).tokens).toHaveLength(2);
    expect(DURATION.duplicate).toBeGreaterThan(
      duplicate[duplicate.length - 1].t,
    );
  });
});

describe("lifecycle section", () => {
  it("switches the caption and highlights the current step", async () => {
    reduceMotion(false);
    const user = userEvent.setup();
    render(<Lifecycle />);
    const caption = screen.getByTestId("lifecycle-caption");
    expect(caption).toHaveTextContent(/accepted once, routed/);
    await user.click(
      screen.getByRole("button", { name: /Lost response \(unknown/ }),
    );
    expect(caption).toHaveTextContent(/never to a second one/);
    expect(
      screen.getByRole("button", { name: /Lost response/ }),
    ).toHaveAttribute("aria-pressed", "true");
    await user.click(screen.getByRole("button", { name: "Duplicate request" }));
    expect(caption).toHaveTextContent(/stored response replayed/);
    // The scene reports the first frame, which highlights step 1.
    const list = screen.getByRole("list", {
      name: "Payment lifecycle steps",
    });
    await waitFor(() =>
      expect(within(list).getAllByRole("listitem")[0]).toHaveAttribute(
        "aria-current",
        "step",
      ),
    );
    expect(within(list).getAllByRole("listitem")).toHaveLength(7);
    expect(screen.getByRole("status")).toHaveTextContent(/stored response/);
  });

  it("renders a static frame without starting a loop when motion is reduced", async () => {
    reduceMotion(true);
    const raf = vi.fn(() => 1);
    vi.stubGlobal("requestAnimationFrame", raf);
    render(<Lifecycle />);
    await waitFor(() => expect(three.render).toHaveBeenCalled());
    expect(raf).not.toHaveBeenCalled();
    expect(
      screen.queryByRole("button", { name: /Pause animation/ }),
    ).toBeNull();
    const items = screen.getAllByRole("listitem");
    expect(items).toHaveLength(7);
    expect(items.some((item) => item.hasAttribute("aria-current"))).toBe(false);
  });

  it("offers Play and Pause and disposes the renderer on unmount", async () => {
    reduceMotion(false);
    const raf = vi.fn(() => 1);
    vi.stubGlobal("requestAnimationFrame", raf);
    vi.stubGlobal("cancelAnimationFrame", vi.fn());
    const user = userEvent.setup();
    const { unmount } = render(<Lifecycle />);
    await waitFor(() => expect(raf).toHaveBeenCalled());
    await user.click(screen.getByRole("button", { name: "Pause animation" }));
    expect(
      screen.getByRole("button", { name: "Play animation" }),
    ).toBeInTheDocument();
    unmount();
    expect(three.dispose).toHaveBeenCalled();
  });

  it("falls back to a static flow when WebGL is unavailable", async () => {
    reduceMotion(false);
    three.webgl = false;
    render(<Lifecycle />);
    expect(await screen.findByTestId("lifecycle-fallback")).toBeVisible();
    expect(screen.getAllByRole("listitem")).toHaveLength(7);
  });
});

describe("one-step sandbox API panel", () => {
  it("explains that public access is off and disables Start sandbox", async () => {
    const calls = mockApi(false);
    render(<SandboxStart />);
    expect(await screen.findByTestId("sandbox-off")).toHaveTextContent(
      "Public sandbox access is off. Start the stack with pnpm demo:up (it turns it on) and open the dashboard with pnpm demo:open",
    );
    expect(
      screen.getByRole("button", { name: "Start sandbox" }),
    ).toBeDisabled();
    expect(screen.getByText(/<sandbox payment method ID>/)).toBeInTheDocument();
    expect(calls.filter((c) => c.method === "POST")).toHaveLength(0);
  });

  it("treats a missing status endpoint as not available", async () => {
    mockApi("unavailable");
    render(<SandboxStart />);
    expect(await screen.findByTestId("sandbox-off")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Start sandbox" }),
    ).toBeDisabled();
  });

  it("mints nothing on load, then one session and one key on click", async () => {
    const calls = mockApi(true);
    const user = userEvent.setup();
    render(<SandboxStart />);
    const start = screen.getByRole("button", { name: "Start sandbox" });
    await waitFor(() => expect(start).toBeEnabled());
    expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual([
      "GET /v1/sandbox/status",
    ]);

    await user.click(start);
    const panel = await screen.findByTestId("new-api-key");
    expect(panel).toHaveTextContent(SECRET);
    const posts = calls.filter((c) => c.method === "POST");
    expect(posts.map((c) => c.path)).toEqual([
      "/v1/sandbox/session",
      "/v1/api-keys",
    ]);
    expect(JSON.parse(String(posts[1].init?.body))).toEqual({
      name: "Landing demo key",
      role: "operator",
      expires_in_hours: 24,
    });
    // The curl uses the real key and the success sandbox method.
    const curl = screen.getByLabelText("API example: create a sandbox payment");
    expect(curl).toHaveTextContent(`Bearer ${SECRET}`);
    expect(curl).toHaveTextContent('"payment_method_id": "pm_success"');
    expect(
      screen.getByRole("link", { name: "Open the dashboard" }),
    ).toHaveAttribute("href", "/dashboard");
    expect(screen.getByRole("link", { name: "API reference" })).toHaveAttribute(
      "href",
      "/dashboard/api-reference",
    );
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
  });

  it("runs the example with the bearer key, replays it, and hides the key", async () => {
    const calls = mockApi(true);
    const user = userEvent.setup();
    render(<SandboxStart />);
    const start = screen.getByRole("button", { name: "Start sandbox" });
    await waitFor(() => expect(start).toBeEnabled());
    await user.click(start);
    await screen.findByTestId("new-api-key");

    await user.click(screen.getByRole("button", { name: "Run it" }));
    const result = await screen.findByTestId("run-result");
    expect(result).toHaveTextContent("HTTP 202");
    expect(result).toHaveTextContent('"id": "pay_123"');
    const run = calls.filter((c) => c.path === "/v1/payments");
    expect(run).toHaveLength(1);
    const headers = run[0].init?.headers as Record<string, string>;
    expect(headers.Authorization).toBe(`Bearer ${SECRET}`);
    expect(run[0].init?.credentials).toBe("omit");

    await user.click(
      screen.getByRole("button", {
        name: /Run it again \(same Idempotency-Key\)/,
      }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("run-result")).toHaveTextContent(/replayed/),
    );
    const again = calls.filter((c) => c.path === "/v1/payments");
    expect(again).toHaveLength(2);
    expect(
      (again[1].init?.headers as Record<string, string>)["Idempotency-Key"],
    ).toBe(headers["Idempotency-Key"]);

    await user.click(screen.getByRole("button", { name: /hide the key/i }));
    expect(screen.queryByTestId("new-api-key")).toBeNull();
    expect(document.body.textContent).not.toContain(SECRET);
  });

  it("shows an alert and lets the visitor try again when starting fails", async () => {
    mockApi(true);
    const fetchMock = vi.mocked(fetch);
    const original = fetchMock.getMockImplementation();
    fetchMock.mockImplementation(async (input, init) =>
      String(input) === "/v1/sandbox/session"
        ? json(
            {
              code: "sandbox_access_disabled",
              message: "off",
              request_id: "r",
              field_errors: [],
            },
            403,
          )
        : original!(input, init),
    );
    const user = userEvent.setup();
    render(<SandboxStart />);
    const start = screen.getByRole("button", { name: "Start sandbox" });
    await waitFor(() => expect(start).toBeEnabled());
    await user.click(start);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /Public sandbox access is off/,
    );
    expect(screen.getByRole("button", { name: "Try again" })).toBeEnabled();
  });
});

describe("landing page", () => {
  it("keeps the established content and the new sections", () => {
    reduceMotion(true);
    mockApi(false);
    render(<Home />);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent(
      "Payments,",
    );
    expect(
      screen.getByRole("link", { name: /Explore the dashboard/ }),
    ).toHaveAttribute("href", "/dashboard");
    expect(screen.getByText("Sandbox only / no real funds.")).toBeVisible();
    expect(screen.getAllByText("Planned").length).toBeGreaterThan(0);
    expect(
      screen.getByRole("heading", { name: "From request to balanced ledger" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Call the API in one step" }),
    ).toBeInTheDocument();
    // The hero call to action and the panel button.
    expect(
      screen.getAllByRole("button", { name: /Start sandbox/ }),
    ).toHaveLength(2);
  });
});
