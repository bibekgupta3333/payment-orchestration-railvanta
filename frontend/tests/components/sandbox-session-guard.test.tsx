import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SandboxSessionGuard } from "@/components/layout/SandboxSessionGuard";

const workspace = {
  id: "00000000-0000-7000-8000-000000000001",
  name: "Demo workspace",
  environment: "sandbox",
};
const unauthorized = {
  code: "unauthorized",
  message: "A valid sandbox session or API key is required",
  request_id: "r",
  field_errors: [],
};

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json", "x-request-id": "req_t" },
  });
}

function mockApi({
  signedIn,
  publicAccess,
}: {
  signedIn: boolean;
  publicAccess: boolean;
}) {
  const fetchMock = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input).split("?")[0];
      if (path === "/v1/workspace")
        return signedIn ? json(workspace) : json(unauthorized, 401);
      if (path === "/v1/sandbox/status")
        return json({ public_access: publicAccess, session_ttl_hours: 12 });
      if (path === "/v1/sandbox/session" && init?.method === "POST")
        return json(
          { workspace, role: "operator", expires_at: "2026-11-02T00:00:00Z" },
          201,
        );
      return json(unauthorized, 404);
    },
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function renderGuard() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <SandboxSessionGuard>
        <p>dashboard page</p>
      </SandboxSessionGuard>
    </QueryClientProvider>,
  );
}

const posted = (fetchMock: ReturnType<typeof mockApi>) =>
  fetchMock.mock.calls.some(
    ([input, init]) =>
      String(input).startsWith("/v1/sandbox/session") &&
      init?.method === "POST",
  );

describe("SandboxSessionGuard", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("renders the page directly when a session already exists", async () => {
    const fetchMock = mockApi({ signedIn: true, publicAccess: true });
    renderGuard();
    expect(await screen.findByText("dashboard page")).toBeInTheDocument();
    expect(posted(fetchMock)).toBe(false);
  });

  it("starts a sandbox session on 401 when public access is on", async () => {
    const fetchMock = mockApi({ signedIn: false, publicAccess: true });
    renderGuard();
    expect(await screen.findByText("dashboard page")).toBeInTheDocument();
    expect(posted(fetchMock)).toBe(true);
  });

  it("does not start a session when public access is off", async () => {
    const fetchMock = mockApi({ signedIn: false, publicAccess: false });
    renderGuard();
    expect(await screen.findByText("dashboard page")).toBeInTheDocument();
    expect(posted(fetchMock)).toBe(false);
  });
});
