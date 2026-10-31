import { execFileSync } from "node:child_process";
import path from "node:path";

const root = new URL("../", import.meta.url);
const files = execFileSync("git", ["ls-files", "-z"], {
  cwd: root,
  encoding: "utf8",
})
  .split("\0")
  .filter(Boolean);

// This is a tracked-file policy check, not a claim of complete secret scanning.
const prohibited = files.filter((file) => {
  const name = path.basename(file);
  const environment =
    (name === ".env" || name.startsWith(".env.")) && name !== ".env.example";
  const database = /\.(?:db|sqlite|sqlite3)(?:-(?:wal|shm|journal))?$/i.test(name);
  const privateKey = /^(?:id_rsa|id_ed25519)$/.test(name) || /\.key$/i.test(name);
  return environment || database || privateKey;
});

if (prohibited.length) {
  throw new Error(`Prohibited tracked files:\n${prohibited.join("\n")}`);
}
console.log("No environment secrets, private-key files, or SQLite databases tracked.");
