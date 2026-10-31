import { randomBytes } from "node:crypto";
import { readdir, readFile } from "node:fs/promises";
import { spawnSync } from "node:child_process";

// Backend-only sentinels make accidental Next.js client exposure fail the build.
const secretNames = [
  "DEMO_OPERATOR_KEY",
  "DEMO_SESSION_TOKEN",
  "SESSION_SIGNING_KEY",
  "PROVIDER_API_SECRET",
];
const sentinels = Object.fromEntries(
  secretNames.map((name) => [name, `railvanta_build_probe_${randomBytes(24).toString("hex")}`]),
);
const result = spawnSync(
  "pnpm",
  ["--filter", "@railvanta/frontend", "--fail-if-no-match", "build"],
  {
    cwd: new URL("../", import.meta.url),
    env: { ...process.env, ...sentinels },
    stdio: "inherit",
  },
);
if (result.error) throw result.error;
if (result.status !== 0) process.exit(result.status ?? 1);

async function inspect(directory) {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const location = new URL(entry.name + (entry.isDirectory() ? "/" : ""), directory);
    if (entry.isDirectory()) await inspect(location);
    else {
      const contents = await readFile(location);
      if (Object.values(sentinels).some((secret) => contents.includes(secret))) {
        throw new Error("A backend-only build sentinel leaked into a client asset.");
      }
    }
  }
}
await inspect(new URL("../frontend/.next/static/", import.meta.url));
console.log("Client assets contain none of the backend-only build sentinels.");
