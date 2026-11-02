import { describe, expect, it } from "vitest";
import { formatDurationMs } from "@/lib/format-duration";

describe("formatDurationMs", () => {
  it("keeps sub-second values in milliseconds", () => {
    expect(formatDurationMs(0)).toBe("0 ms");
    expect(formatDurationMs(205)).toBe("205 ms");
    expect(formatDurationMs(999)).toBe("999 ms");
  });

  it("switches to seconds, minutes and hours", () => {
    expect(formatDurationMs(1_000)).toBe("1 s");
    expect(formatDurationMs(6_700)).toBe("6.7 s");
    expect(formatDurationMs(90_000)).toBe("1.5 min");
    expect(formatDurationMs(4_465_210)).toBe("1.2 h");
  });

  it("does not format invalid values as numbers", () => {
    expect(formatDurationMs(Number.NaN)).toBe("—");
    expect(formatDurationMs(-5)).toBe("—");
  });
});
