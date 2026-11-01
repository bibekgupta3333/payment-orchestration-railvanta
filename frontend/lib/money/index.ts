/**
 * Exact money helpers. Amounts stay decimal strings of integer minor units at
 * the API boundary and are only ever manipulated as BigInt; no money value is
 * converted through a floating-point Number.
 */
export const currencyExponents = {
  USD: 2,
  EUR: 2,
  GBP: 2,
  JPY: 0,
  KWD: 3,
} as const;
export type Currency = keyof typeof currencyExponents;
export const currencies = Object.keys(currencyExponents) as Currency[];

export const maximumAmount = 9223372036854775807n;
const canonicalMinor = /^(0|[1-9]\d*)$/;

export function isCurrency(value: string): value is Currency {
  return Object.hasOwn(currencyExponents, value);
}

function exponentOf(currency: string): number {
  if (!isCurrency(currency)) throw new Error("Unsupported currency");
  return currencyExponents[currency];
}

function render(amount: bigint, currency: string): string {
  const exponent = exponentOf(currency);
  const divisor = 10n ** BigInt(exponent);
  const whole = (amount / divisor)
    .toString()
    .replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  const fractional = exponent
    ? `.${(amount % divisor).toString().padStart(exponent, "0")}`
    : "";
  return `${currency} ${whole}${fractional}`;
}

/** Formats one payment amount (non-negative, at most int64). */
export function formatMoney(amountMinor: string, currency: string): string {
  if (!canonicalMinor.test(amountMinor)) {
    throw new Error(
      "Amount must be a canonical non-negative minor-unit string",
    );
  }
  exponentOf(currency);
  const amount = BigInt(amountMinor);
  if (amount > maximumAmount) throw new Error("Amount exceeds int64");
  return render(amount, currency);
}

/** Formats an exact per-currency aggregate, which may exceed int64. */
export function formatAggregateMoney(
  amountMinor: string,
  currency: string,
): string {
  if (!canonicalMinor.test(amountMinor)) {
    throw new Error(
      "Amount must be a canonical non-negative minor-unit string",
    );
  }
  return render(BigInt(amountMinor), currency);
}

export type ParsedAmount =
  { ok: true; amountMinor: string } | { ok: false; message: string };

/**
 * Converts a major-unit amount typed by a person ("250.5" USD) to canonical
 * minor units ("25050") with string arithmetic only.
 */
export function parseMajorAmount(
  input: string,
  currency: string,
): ParsedAmount {
  if (!isCurrency(currency)) return { ok: false, message: "Choose a currency" };
  const exponent = currencyExponents[currency];
  const value = input.trim();
  if (value === "") return { ok: false, message: "Enter an amount" };
  const match = /^(\d+)(?:\.(\d*))?$/.exec(value);
  if (!match)
    return {
      ok: false,
      message: "Use digits with an optional decimal point, for example 250.00",
    };
  const fraction = match[2] ?? "";
  if (fraction.length > exponent) {
    return {
      ok: false,
      message:
        exponent === 0
          ? `${currency} has no minor units; enter a whole number`
          : `${currency} allows at most ${exponent} decimal places`,
    };
  }
  const minor = BigInt(match[1] + fraction.padEnd(exponent, "0"));
  if (minor <= 0n)
    return { ok: false, message: "Enter an amount greater than zero" };
  if (minor > maximumAmount)
    return { ok: false, message: "Amount exceeds the supported maximum" };
  return { ok: true, amountMinor: minor.toString() };
}

/** Renders minor units as an editable major-unit string ("25050" USD → "250.50"). */
export function minorToMajorInput(
  amountMinor: string,
  currency: string,
): string {
  if (!canonicalMinor.test(amountMinor)) throw new Error("Invalid amount");
  const exponent = exponentOf(currency);
  if (exponent === 0) return amountMinor;
  const padded = amountMinor.padStart(exponent + 1, "0");
  return `${padded.slice(0, -exponent)}.${padded.slice(-exponent)}`;
}

/** Exact comparison of two canonical minor-unit strings. */
export function compareMinor(left: string, right: string): -1 | 0 | 1 {
  const a = BigInt(left);
  const b = BigInt(right);
  return a < b ? -1 : a > b ? 1 : 0;
}

/**
 * Integer share of `amount` relative to `peak` in thousandths (0–1000). This is
 * a dimensionless chart index computed with BigInt, not a money value.
 */
export function shareOfPeak(amountMinor: string, peakMinor: string): number {
  const peak = BigInt(peakMinor);
  if (peak === 0n) return 0;
  return Number((BigInt(amountMinor) * 1000n) / peak);
}
