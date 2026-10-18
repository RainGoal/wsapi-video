import type { PricingBreakdown, Product, VideoBillingUnit } from "./types";

export const MAX_CREDITS = 1_000_000_000;

export function validCredits(value: number): boolean {
  return Number.isSafeInteger(value) && value >= 1 && value <= MAX_CREDITS;
}

export function productBillingUnit(
  product: Pick<Product, "kind" | "pricing">,
): VideoBillingUnit {
  return product.kind === "video" &&
    product.pricing?.billing_unit === "per_second"
    ? "per_second"
    : "per_video";
}

export function estimateProductPrice(
  product: Product,
  duration: number,
  resolution: string,
): PricingBreakdown | null {
  if (
    product.kind === "video" &&
    product.pricing &&
    product.pricing.billing_unit !== "per_video" &&
    product.pricing.billing_unit !== "per_second"
  )
    return null;
  const billing_unit = productBillingUnit(product);
  const selectedResolution = product.kind === "video" ? resolution.trim() : "";
  const override =
    product.kind === "video"
      ? Object.entries(product.pricing?.resolution_credits || {}).find(
          ([key]) =>
            key.trim().toLowerCase() === selectedResolution.toLowerCase(),
        )?.[1]
      : undefined;
  const unit_credits = override ?? product.credits;
  const quantity = billing_unit === "per_second" ? duration : 1;
  const total_credits = unit_credits * quantity;
  if (
    !validCredits(unit_credits) ||
    !validCredits(quantity) ||
    !validCredits(total_credits)
  )
    return null;
  return {
    billing_unit,
    unit_credits,
    quantity,
    resolution: selectedResolution,
    total_credits,
  };
}
