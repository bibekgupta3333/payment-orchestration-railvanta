import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { mkdtempSync, readFileSync, readdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { expect, test } from "@playwright/test";
import { ApiClient } from "../support/api";
import {
  compose,
  dataVolume,
  waitApiReady,
  waitProxyReady,
} from "../support/containers";
import {
  createPayment,
  getPayment,
  journalsFor,
  listAttempts,
  waitForStatus,
} from "../support/payments";
import { fixtures, state } from "../support/state";

const root = resolve(__dirname, "../..");

test.skip(
  !process.env.E2E_BASE_URL,
  "container-only: needs the docker compose stack (set E2E_BASE_URL)",
);

function script(name: string, ...args: string[]): string {
  return execFileSync(join(root, "scripts", name), args, {
    cwd: root,
    env: process.env,
    encoding: "utf8",
    timeout: 240_000,
  });
}

test("backup, stop, restore into a clean volume, start: payments, journals and idempotency replays survive", async ({
  request,
}) => {
  test.setTimeout(420_000);
  const api = new ApiClient(request);
  const backups = mkdtempSync(join(tmpdir(), "railvanta-backups-"));
  try {
    // State recorded before the backup: a succeeded and a failed payment.
    const key = `restore_${randomUUID()}`;
    const reference = `restore_${randomUUID()}`;
    const data = {
      amount_minor: "4242",
      currency: "USD",
      payment_method_id: fixtures.successMethod,
      merchant_reference: reference,
    };
    const first = await api.call("POST", "/v1/payments", {
      data,
      headers: { "Idempotency-Key": key },
    });
    expect(first.status()).toBe(202);
    const firstBody = await first.body();
    const succeeded = JSON.parse(firstBody.toString()) as { id: string };
    await waitForStatus(api, succeeded.id, "succeeded");
    const declined = await createPayment(api, fixtures.declineMethod);
    await waitForStatus(api, declined.id, "failed");
    const journalsBefore = await journalsFor(api, succeeded.id);
    expect(journalsBefore).toHaveLength(1);
    const attemptsBefore = await listAttempts(api, succeeded.id);

    const output = script("backup-db.sh", backups);
    const snapshot = readdirSync(backups).find((f) => f.endsWith(".db"));
    expect(snapshot, output).toBeTruthy();
    const manifest = JSON.parse(
      readFileSync(join(backups, `${snapshot}.json`), "utf8"),
    ) as Record<string, unknown>;
    expect(manifest).toMatchObject({
      format: 1,
      schema_version: expect.any(Number),
      sha256: expect.stringMatching(/^[0-9a-f]{64}$/),
      release: { version: expect.any(String) },
    });
    expect(Date.parse(manifest.snapshot_at as string)).not.toBeNaN();

    // Written after the snapshot: must not exist after restoring it.
    const later = await createPayment(api, fixtures.successMethod);
    await waitForStatus(api, later.id, "succeeded");

    const oldVolume = dataVolume();
    const restoreOutput = script(
      "restore-backup.sh",
      join(backups, snapshot as string),
      "--start",
    );
    const newVolume = /^RAILVANTA_DATA_VOLUME=(.+)$/m.exec(restoreOutput)?.[1];
    expect(newVolume, restoreOutput).toBeTruthy();
    expect(newVolume).not.toBe(oldVolume);
    expect(restoreOutput).toContain("checks=ok");
    process.env.RAILVANTA_DATA_VOLUME = newVolume;
    await waitApiReady();
    await waitProxyReady(state().apiOrigin);

    // The old volume is preserved, not overwritten.
    execFileSync("docker", ["volume", "inspect", oldVolume]);

    expect(await getPayment(api, succeeded.id)).toMatchObject({
      id: succeeded.id,
      status: "succeeded",
      amount_minor: "4242",
    });
    expect((await getPayment(api, declined.id)).status).toBe("failed");
    const journalsAfter = await journalsFor(api, succeeded.id);
    expect(journalsAfter).toEqual(journalsBefore);
    expect(await listAttempts(api, succeeded.id)).toEqual(attemptsBefore);

    const replay = await api.call("POST", "/v1/payments", {
      data,
      headers: { "Idempotency-Key": key },
    });
    expect(replay.status()).toBe(202);
    expect(replay.headers()["idempotent-replayed"]).toBe("true");
    expect((await replay.body()).equals(firstBody)).toBe(true);
    // A changed body under the same key is still a conflict, not a new payment.
    const conflict = await api.call("POST", "/v1/payments", {
      data: { ...data, amount_minor: "4243" },
      headers: { "Idempotency-Key": key },
    });
    expect(conflict.status()).toBe(409);

    const missing = await api.call("GET", `/v1/payments/${later.id}`);
    expect(missing.status()).toBe(404);

    const check = compose(["exec", "-T", "api", "railvanta-api", "check"]);
    expect(check).toContain("checks=ok");
  } finally {
    rmSync(backups, { recursive: true, force: true });
  }
});

test("restore refuses a tampered backup and leaves the stack untouched", async () => {
  test.setTimeout(240_000);
  const backups = mkdtempSync(join(tmpdir(), "railvanta-backups-"));
  try {
    script("backup-db.sh", backups);
    const snapshot = readdirSync(backups).find((f) => f.endsWith(".db"));
    expect(snapshot).toBeTruthy();
    const manifestPath = join(backups, `${snapshot}.json`);
    const manifest = JSON.parse(readFileSync(manifestPath, "utf8")) as {
      sha256: string;
    };
    execFileSync("python3", [
      "-c",
      "import sys,json; p=sys.argv[1]; m=json.load(open(p)); m['sha256']='0'*64; json.dump(m,open(p,'w'))",
      manifestPath,
    ]);
    expect(manifest.sha256).toHaveLength(64);
    let failed = false;
    try {
      script("restore-backup.sh", join(backups, snapshot as string));
    } catch (error) {
      failed = true;
      expect(String((error as { stderr?: string }).stderr)).toContain(
        "checksum mismatch",
      );
    }
    expect(failed).toBe(true);
    // The restore fenced the stack; bring it back on the untouched volume.
    compose(["up", "--detach", "--wait"], 180_000);
    await waitApiReady();
    await waitProxyReady(state().apiOrigin);
  } finally {
    rmSync(backups, { recursive: true, force: true });
  }
});
