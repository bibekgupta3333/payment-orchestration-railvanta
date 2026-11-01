import { describe, expect, it } from "vitest";
import {
  compareMinor,
  formatAggregateMoney,
  formatMoney,
  minorToMajorInput,
  parseMajorAmount,
  shareOfPeak,
} from "@/lib/money";

describe("exact minor-unit money formatting", () => {
  it.each([
    ["25000", "USD", "USD 250.00"],
    ["1234", "JPY", "JPY 1,234"],
    ["1234", "KWD", "KWD 1.234"],
    ["5", "KWD", "KWD 0.005"],
    ["0", "USD", "USD 0.00"],
    ["9007199254740993", "USD", "USD 90,071,992,547,409.93"],
    ["9223372036854775807", "USD", "USD 92,233,720,368,547,758.07"],
    ["9223372036854775807", "JPY", "JPY 9,223,372,036,854,775,807"],
  ])("preserves %s in %s", (minor, currency, expected) =>
    expect(formatMoney(minor, currency)).toBe(expected),
  );

  it.each(["1.5", "-1", "+1", " 1", "01", "", "9223372036854775808", "1e3"])(
    "rejects invalid exact representation %s",
    (amount) => {
      expect(() => formatMoney(amount, "USD")).toThrow();
    },
  );
  it("rejects unsupported currency metadata", () =>
    expect(() => formatMoney("100", "XYZ")).toThrow("Unsupported currency"));

  it("formats per-currency aggregates beyond int64 exactly", () => {
    expect(formatAggregateMoney("18446744073709551614", "USD")).toBe(
      "USD 184,467,440,737,095,516.14",
    );
    expect(() => formatAggregateMoney("-1", "USD")).toThrow();
  });
});

describe("major-unit input parsing", () => {
  it.each([
    ["250", "USD", "25000"],
    ["250.5", "USD", "25050"],
    ["0.01", "USD", "1"],
    ["1234", "JPY", "1234"],
    ["1.234", "KWD", "1234"],
    ["92233720368547758.07", "USD", "9223372036854775807"],
    // 2^53 + 1 survives because no Number conversion happens.
    ["90071992547409.93", "USD", "9007199254740993"],
  ])("parses %s %s to %s minor units", (input, currency, expected) => {
    expect(parseMajorAmount(input, currency)).toEqual({
      ok: true,
      amountMinor: expected,
    });
  });
  it.each([
    ["", "USD"],
    ["0", "USD"],
    ["1.001", "USD"],
    ["1.5", "JPY"],
    ["-1", "USD"],
    ["1,000", "USD"],
    ["1e3", "USD"],
    ["92233720368547758.08", "USD"],
    ["10", "XYZ"],
  ])("rejects %s %s", (input, currency) => {
    expect(parseMajorAmount(input, currency).ok).toBe(false);
  });
});

describe("minor to major input", () => {
  it.each([
    ["25050", "USD", "250.50"],
    ["5", "KWD", "0.005"],
    ["1234", "JPY", "1234"],
    ["9007199254740993", "USD", "90071992547409.93"],
  ])("renders %s %s as %s and round-trips", (minor, currency, expected) => {
    expect(minorToMajorInput(minor, currency)).toBe(expected);
    expect(parseMajorAmount(expected, currency)).toEqual({
      ok: true,
      amountMinor: minor,
    });
  });
});

describe("exact comparison and chart index", () => {
  it("compares beyond 2^53 without precision loss", () => {
    expect(compareMinor("9007199254740993", "9007199254740992")).toBe(1);
    expect(compareMinor("5", "5")).toBe(0);
  });
  it("computes an integer per-mille share with BigInt", () => {
    expect(shareOfPeak("9223372036854775807", "9223372036854775807")).toBe(
      1000,
    );
    expect(shareOfPeak("1", "3")).toBe(333);
    expect(shareOfPeak("0", "0")).toBe(0);
  });
});
