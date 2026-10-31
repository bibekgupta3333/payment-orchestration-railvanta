import type { NextConfig } from "next";
import path from "node:path";

const apiOrigin = process.env.API_INTERNAL_ORIGIN ?? "http://127.0.0.1:8080";
const parsedOrigin = new URL(apiOrigin);
if (
  !["http:", "https:"].includes(parsedOrigin.protocol) ||
  parsedOrigin.username ||
  parsedOrigin.password ||
  parsedOrigin.pathname !== "/" ||
  parsedOrigin.search ||
  parsedOrigin.hash
) {
  throw new Error(
    "API_INTERNAL_ORIGIN must be an HTTP origin without credentials or a path",
  );
}

const nextConfig: NextConfig = {
  output: "standalone",
  outputFileTracingRoot: path.join(__dirname, ".."),
  poweredByHeader: false,
  async rewrites() {
    return [
      { source: "/v1/:path*", destination: `${parsedOrigin.origin}/v1/:path*` },
    ];
  },
};

export default nextConfig;
