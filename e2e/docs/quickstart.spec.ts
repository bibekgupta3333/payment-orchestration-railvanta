import { execFileSync, spawnSync } from "node:child_process";
import { copyFileSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { expect, test } from "@playwright/test";

/**
 * WBS 5.4.1: proves the root README quickstart works on a clean checkout.
 * Clones the repository (committed HEAD plus the working-tree README) into a
 * temp directory, runs the fenced commands between the quickstart markers as
 * written, then runs the smoke and central-demo specs there. Opt-in because it
 * is slow and needs network (`pnpm test:e2e:quickstart`). QUICKSTART_CHILD
 * stops the clone's own test runs from recursing into this spec.
 */
const enabled =
  process.env.RUN_QUICKSTART === "1" && !process.env.QUICKSTART_CHILD;
const repoRoot = path.resolve(__dirname, "..", "..");

function quickstartCommands(readme: string): string[] {
  const match =
    /<!-- quickstart:start -->([\s\S]*?)<!-- quickstart:end -->/.exec(readme);
  if (!match) throw new Error("README.md has no quickstart markers");
  const blocks = [...match[1].matchAll(/```(?:sh|bash)\n([\s\S]*?)```/g)];
  const commands = blocks
    .flatMap((block) => block[1].split("\n"))
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith("#"));
  if (commands.length === 0)
    throw new Error("quickstart markers contain no commands");
  return commands;
}

test.describe("README quickstart on a fresh clone", () => {
  test.skip(
    !enabled,
    "set RUN_QUICKSTART=1 (pnpm test:e2e:quickstart); slow and needs network",
  );

  let clone = "";
  test.beforeAll(() => {
    clone = path.join(
      mkdtempSync(path.join(tmpdir(), "railvanta-quickstart-")),
      "repo",
    );
    execFileSync("git", ["clone", "--quiet", repoRoot, clone], {
      stdio: "inherit",
    });
    // Include an uncommitted README so the doc under edit is the one verified.
    copyFileSync(
      path.join(repoRoot, "README.md"),
      path.join(clone, "README.md"),
    );
  });
  test.afterAll(() => {
    if (clone) rmSync(path.dirname(clone), { recursive: true, force: true });
  });

  const run = (command: string, timeoutMs: number) => {
    const env: NodeJS.ProcessEnv = {
      ...process.env,
      QUICKSTART_CHILD: "1",
      CI: "1",
    };
    delete env.RUN_QUICKSTART;
    delete env.E2E_BASE_URL;
    console.log(`$ ${command}`);
    const result = spawnSync("bash", ["-c", command], {
      cwd: clone,
      env,
      stdio: "inherit",
      timeout: timeoutMs,
    });
    expect(
      result.status,
      `"${command}" exited ${result.status} (${result.signal ?? "no signal"})`,
    ).toBe(0);
  };

  test("README quickstart commands, then smoke and central demo specs", () => {
    test.setTimeout(45 * 60_000);
    const commands = quickstartCommands(
      readFileSync(path.join(clone, "README.md"), "utf8"),
    );
    for (const command of commands) run(command, 20 * 60_000);
    run("pnpm test:e2e api/scaffold.spec.ts", 10 * 60_000);
    run(
      'pnpm test:e2e api/recovery.spec.ts --grep "central demo"',
      10 * 60_000,
    );
  });
});
