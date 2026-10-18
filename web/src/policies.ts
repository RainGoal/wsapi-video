export const policyFields = [
  { key: "retention_policy", title: "作品保存期限" },
  { key: "storage_quota_policy", title: "存储容量政策" },
  { key: "cleanup_policy", title: "素材清理规则" },
  { key: "submission_recovery_policy", title: "提交待确认处理期限" },
  { key: "refund_policy", title: "退款政策" },
] as const;

export type PolicyKey = (typeof policyFields)[number]["key"];
export type PolicyValues = Partial<Record<PolicyKey, string>>;
export const policyLimit = 2000;

export function policyText(value: unknown): string {
  return typeof value === "string" && value.trim() ? value : "";
}

export function policyCodePointCount(value: string): number {
  return Array.from(value.trim()).length;
}

export function policyLengthErrors(values: PolicyValues): PolicyKey[] {
  return policyFields
    .filter(({ key }) => policyCodePointCount(values[key] || "") > policyLimit)
    .map(({ key }) => key);
}
