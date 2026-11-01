import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiKeys } from "@/components/settings/ApiKeys";

const workspace = {
  id: "00000000-0000-7000-8000-000000000001",
  name: "Demo workspace",
  environment: "sandbox",
};

const SECRET = "rvk_1a2b3c4d.0123456789abcdef0123456789abcdef0123456789abcdef";

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json", "x-request-id": "req_test" },
  });
}

type KeyFixture = Record<string, string | null>;

const baseKey: KeyFixture = {
  id: "00000000-0000-7000-8000-0000000000a1",
  name: "Seeded key",
  prefix: "operator",
  role: "operator",
  created_at: "2026-11-01T10:00:00Z",
  expires_at: null,
  revoked_at: null,
  status: "active",
};

interface Calls {
  posts: unknown[];
  deletes: string[];
}

function setup(
  role: "read" | "operator",
  options: { failCreate?: boolean } = {},
) {
  const calls: Calls = { posts: [], deletes: [] };
  let items: KeyFixture[] = [baseKey];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input).split("?")[0];
      const method = (init?.method ?? "GET").toUpperCase();
      if (path === "/v1/workspace") return json(workspace);
      if (path === "/v1/api-keys" && method === "GET")
        return json({ items, limit: 20 });
      if (path === "/v1/api-keys" && method === "POST") {
        calls.posts.push(JSON.parse(String(init?.body)));
        if (options.failCreate)
          return json(
            {
              code: "api_key_limit",
              message:
                "This workspace already has the maximum number of active API keys",
              request_id: "req_limit",
              field_errors: [],
            },
            409,
          );
        const created = {
          ...baseKey,
          id: "00000000-0000-7000-8000-0000000000b2",
          name: "Demo key",
          prefix: "rvk_1a2b3c4d",
          expires_at: "2026-11-02T10:00:00Z",
        };
        items = [created, ...items];
        return json({ key: created, secret: SECRET }, 201);
      }
      if (path.startsWith("/v1/api-keys/") && method === "DELETE") {
        const keyId = path.split("/").pop() ?? "";
        calls.deletes.push(keyId);
        items = items.map((item) =>
          item.id === keyId
            ? { ...item, status: "revoked", revoked_at: "2026-11-01T11:00:00Z" }
            : item,
        );
        return json(items.find((item) => item.id === keyId));
      }
      return json(
        {
          code: "not_found",
          message: "Missing",
          request_id: "r",
          field_errors: [],
        },
        404,
      );
    }),
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <ApiKeys role={role} />
    </QueryClientProvider>,
  );
  return calls;
}

afterEach(() => vi.unstubAllGlobals());

describe("API key generator", () => {
  it("shows the secret once, with a curl example, and hides it on dismissal", async () => {
    const calls = setup("operator");
    const user = userEvent.setup();
    expect(await screen.findByText("Seeded key")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Generate API key" }));
    const panel = await screen.findByTestId("new-api-key");
    expect(within(panel).getByText(SECRET)).toBeVisible();
    expect(
      within(panel).getByText(/only time the full key is shown/i),
    ).toBeVisible();
    expect(
      within(panel).getByText(/Authorization: Bearer rvk_1a2b3c4d\./),
    ).toBeVisible();
    expect(calls.posts[0]).toEqual({
      name: "Default demo key",
      role: "operator",
      expires_in_hours: 24,
    });

    // The listing carries the prefix only, never the secret.
    const table = await screen.findByRole("region", { name: "API key list" });
    expect(within(table).getByText("rvk_1a2b3c4d")).toBeVisible();
    expect(within(table).queryByText(SECRET)).toBeNull();

    await user.click(screen.getByRole("button", { name: /hide the key/i }));
    expect(screen.queryByTestId("new-api-key")).toBeNull();
    expect(document.body.textContent).not.toContain(
      "0123456789abcdef0123456789abcdef0123456789abcdef",
    );
  });

  it("sends the chosen role and expiry", async () => {
    const calls = setup("operator");
    const user = userEvent.setup();
    await screen.findByText("Seeded key");
    await user.clear(screen.getByLabelText("Key name"));
    await user.type(screen.getByLabelText("Key name"), "CI reader");
    await user.selectOptions(screen.getByLabelText("Role"), "read");
    await user.selectOptions(screen.getByLabelText("Expires after"), "168");
    await user.click(screen.getByRole("button", { name: "Generate API key" }));
    await screen.findByTestId("new-api-key");
    expect(calls.posts[0]).toEqual({
      name: "CI reader",
      role: "read",
      expires_in_hours: 168,
    });
  });

  it("revokes only after an explicit confirmation", async () => {
    const calls = setup("operator");
    const user = userEvent.setup();
    await screen.findByText("Seeded key");
    await user.click(
      screen.getByRole("button", { name: /Revoke key Seeded key/ }),
    );
    expect(calls.deletes).toHaveLength(0);
    await user.click(screen.getByRole("button", { name: "Confirm revoke" }));
    await waitFor(() => expect(calls.deletes).toEqual([baseKey.id]));
    expect(await screen.findByText("Revoked")).toBeVisible();
  });

  it("shows the server error with its request ID and keeps no key", async () => {
    setup("operator", { failCreate: true });
    const user = userEvent.setup();
    await screen.findByText("Seeded key");
    await user.click(screen.getByRole("button", { name: "Generate API key" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/maximum number of active API keys/);
    expect(alert).toHaveTextContent("req_limit");
    expect(screen.queryByTestId("new-api-key")).toBeNull();
  });

  it("offers no generator to a read-only role", () => {
    setup("read");
    expect(screen.getByText(/requires the operator role/i)).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Generate API key" }),
    ).toBeNull();
    expect(screen.queryByLabelText("Key name")).toBeNull();
  });
});
