import { execFileSync } from "node:child_process";
import { expect, test } from "@playwright/test";
import { ApiClient } from "../support/api";
import { compose, dataVolume } from "../support/containers";
import {
  createPayment,
  journalsFor,
  listAttempts,
  waitForStatus,
} from "../support/payments";
import {
  fixtures,
  inspectPayment,
  killApi,
  restartApi,
  state,
} from "../support/state";

test.skip(
  !process.env.E2E_BASE_URL,
  "container-only: needs the docker compose stack (set E2E_BASE_URL)",
);

function containerId(service: string): string {
  return compose(["ps", "-a", "-q", service]);
}

test("containers run as non-root, only the API mounts the data volume, health stays private", async ({
  request,
}) => {
  for (const service of ["api", "web", "proxy"]) {
    const user = execFileSync(
      "docker",
      ["inspect", containerId(service), "--format", "{{.Config.User}}"],
      { encoding: "utf8" },
    ).trim();
    const uid = compose(["exec", "-T", service, "id", "-u"]);
    expect(
      Number(uid),
      `${service} runs as uid ${uid} (${user})`,
    ).toBeGreaterThan(0);
  }
  const mounts = (service: string) =>
    execFileSync(
      "docker",
      [
        "inspect",
        containerId(service),
        "--format",
        "{{range .Mounts}}{{.Name}} {{end}}",
      ],
      { encoding: "utf8" },
    );
  expect(mounts("api")).toContain(dataVolume());
  expect(mounts("web")).not.toContain(dataVolume());
  expect(mounts("proxy")).not.toContain(dataVolume());
  // Health is served on the container-loopback listener only.
  for (const probe of ["startup", "live", "ready"]) {
    compose([
      "exec",
      "-T",
      "api",
      "wget",
      "-q",
      "-O",
      "-",
      `http://127.0.0.1:8081/health/${probe}`,
    ]);
    const publicResponse = await request.get(
      `${state().apiOrigin}/health/${probe}`,
    );
    expect(publicResponse.status()).toBe(404);
  }
  const published = execFileSync(
    "docker",
    [
      "inspect",
      containerId("api"),
      "--format",
      "{{json .NetworkSettings.Ports}}",
    ],
    { encoding: "utf8" },
  );
  expect(published).not.toContain("HostPort");
});

test("SIGTERM drains and exits cleanly; the container restarts healthy", async () => {
  test.setTimeout(90_000);
  await restartApi();
  const logs = compose(["logs", "--no-color", "api"]);
  expect(logs).toContain("sandbox API ready");
  const exitCode = execFileSync(
    "docker",
    [
      "inspect",
      containerId("api"),
      "--format",
      "{{.State.ExitCode}} {{.State.Status}}",
    ],
    { encoding: "utf8" },
  ).trim();
  expect(exitCode).toBe("0 running");
});

test("killing the API container mid-dispatch recovers without a second charge or duplicate posting", async ({
  request,
}) => {
  test.setTimeout(120_000);
  const api = new ApiClient(request);
  const accepted = await createPayment(api, fixtures.delayedSuccessMethod);
  await expect
    .poll(async () => (await listAttempts(api, accepted.id))[0]?.status, {
      timeout: 15_000,
      intervals: [50, 100],
    })
    .toBe("submitted");
  const before = await inspectPayment(accepted.id);
  expect(before.charges).toHaveLength(1);
  const key = before.attempts[0].provider_idempotency_key;

  await killApi(); // docker compose kill api (SIGKILL), then start

  await waitForStatus(api, accepted.id, "succeeded", 60_000);
  const after = await inspectPayment(accepted.id);
  expect(after.attempts).toHaveLength(1);
  expect(after.attempts[0].provider_idempotency_key).toBe(key);
  expect(after.charges).toHaveLength(1);
  expect(after.journals).toBe(1);
  expect(await journalsFor(api, accepted.id)).toHaveLength(1);
  const check = compose(["exec", "-T", "api", "railvanta-api", "check"]);
  expect(check).toContain("checks=ok");
});
