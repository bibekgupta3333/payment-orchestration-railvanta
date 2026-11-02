import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { execFileSync } from "node:child_process";
import { resolve } from "node:path";
import Page from "@/app/dashboard/api-reference/page";

const swagger = vi.hoisted(() => vi.fn());
vi.mock("swagger-ui-dist/swagger-ui-es-bundle.js", () => ({
  default: swagger,
}));
vi.mock("swagger-ui-dist/swagger-ui.css", () => ({}));

describe("API reference page", () => {
  it("renders the heading, help text and links, then starts Swagger UI on the same-origin spec", async () => {
    render(<Page />);
    expect(
      screen.getByRole("heading", { level: 1, name: "API reference" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/Sandbox only; no real funds\./),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/bearer keys are the supported way to send writes/),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Settings" })).toHaveAttribute(
      "href",
      "/dashboard/settings#api-keys",
    );

    await waitFor(() => expect(swagger).toHaveBeenCalledTimes(1));
    const options = swagger.mock.calls[0][0];
    expect(options.url).toBe("/openapi.json");
    expect(options.domNode).toBe(screen.getByTestId("swagger-host"));
    expect(options.persistAuthorization).toBe(false);
    expect(options).not.toHaveProperty("authorizations");
    expect(options.url).not.toMatch(/rvk_/);
  });

  it("keeps frontend/public/openapi.json in step with the OpenAPI YAML", () => {
    const root = resolve(process.cwd(), "..");
    expect(() =>
      execFileSync(process.execPath, ["scripts/generate-api.mjs", "--check"], {
        cwd: root,
        stdio: "pipe",
      }),
    ).not.toThrow();
  });
});
