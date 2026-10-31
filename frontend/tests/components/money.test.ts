import { describe, expect, it } from "vitest";
import { formatMoney } from "@/lib/money";

describe("exact minor-unit money formatting", () => {
  it.each([
    ["25000", "USD", "USD 250.00"],
    ["1234", "JPY", "JPY 1,234"],
    ["1234", "KWD", "KWD 1.234"],
    ["0", "USD", "USD 0.00"],
    ["9007199254740993", "USD", "USD 90,071,992,547,409.93"],
    ["9223372036854775807", "USD", "USD 92,233,720,368,547,758.07"],
  ])("preserves %s in %s", (minor, currency, expected) =>
    expect(formatMoney(minor, currency)).toBe(expected),
  );

  it.each(["1.5", "-1", "+1", " 1", "01", "", "9223372036854775808"])(
    "rejects invalid exact representation %s",
    (amount) => {
      expect(() => formatMoney(amount, "USD")).toThrow();
    },
  );
  it("rejects unsupported currency metadata", () =>
    expect(() => formatMoney("100", "XYZ")).toThrow("Unsupported currency"));
});
