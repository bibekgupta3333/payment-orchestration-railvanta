export const currencyExponents = {
  USD: 2,
  EUR: 2,
  GBP: 2,
  JPY: 0,
  KWD: 3,
} as const;
export type Currency = keyof typeof currencyExponents;

const maximumAmount = 9223372036854775807n;

export function formatMoney(amountMinor: string, currency: string): string {
  if (!/^(0|[1-9]\d*)$/.test(amountMinor)) {
    throw new Error(
      "Amount must be a canonical non-negative minor-unit string",
    );
  }
  if (!Object.hasOwn(currencyExponents, currency)) {
    throw new Error("Unsupported currency");
  }
  const amount = BigInt(amountMinor);
  if (amount > maximumAmount) throw new Error("Amount exceeds int64");
  const exponent = currencyExponents[currency as Currency];
  const divisor = 10n ** BigInt(exponent);
  const whole = (amount / divisor)
    .toString()
    .replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  const fractional = exponent
    ? `.${(amount % divisor).toString().padStart(exponent, "0")}`
    : "";
  return `${currency} ${whole}${fractional}`;
}
