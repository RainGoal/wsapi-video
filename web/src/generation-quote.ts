import type { Quote } from "./types";

// A quote is only eligible for a new submission while its server expiry is live.
export function validGenerationQuote(
  value: unknown,
  productID: string,
  now = Date.now(),
): value is Quote {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const quote = value as Record<string, unknown>;
  if (quote.pricing !== undefined) {
    if (
      !quote.pricing ||
      typeof quote.pricing !== "object" ||
      Array.isArray(quote.pricing)
    )
      return false;
    const pricing = quote.pricing as Record<string, unknown>;
    if (
      !["per_video", "per_second"].includes(pricing.billing_unit as string) ||
      !Number.isSafeInteger(pricing.unit_credits) ||
      (pricing.unit_credits as number) < 1 ||
      !Number.isSafeInteger(pricing.quantity) ||
      (pricing.quantity as number) < 1 ||
      typeof pricing.resolution !== "string" ||
      pricing.total_credits !== quote.credits ||
      (pricing.unit_credits as number) * (pricing.quantity as number) !==
        quote.credits
    )
      return false;
  }
  return (
    typeof quote.id === "string" &&
    /^quote_[0-9a-f]{32}$/.test(quote.id) &&
    quote.product_id === productID &&
    Number.isSafeInteger(quote.credits) &&
    (quote.credits as number) >= 1 &&
    (quote.credits as number) <= 1_000_000_000 &&
    typeof quote.expires_at === "string" &&
    Date.parse(quote.expires_at) > now &&
    typeof quote.input_hash === "string" &&
    Boolean(quote.input_hash) &&
    typeof quote.demo === "boolean"
  );
}
