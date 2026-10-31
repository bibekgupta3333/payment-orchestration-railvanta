import { access } from "node:fs/promises";

// Prevent recursive pnpm scripts from succeeding because a workspace is absent.
for (const relative of [
  "backend/go.mod",
  "frontend/package.json",
  "e2e/package.json",
]) {
  try {
    await access(new URL(`../${relative}`, import.meta.url));
  } catch {
    throw new Error(`Missing required workspace file: ${relative}`);
  }
}
console.log("Required Go, frontend, and E2E workspaces exist.");
