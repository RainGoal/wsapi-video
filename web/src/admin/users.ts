export type UserFilters = {
  q: string;
  status: string;
  role: string;
  email_verified: string;
  created_from: string;
  created_to: string;
  sort: string;
  page: number;
};

const date = (value: string | null) => {
  if (!value || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return "";
  const parsed = new Date(`${value}T00:00:00Z`);
  return Number.isFinite(parsed.getTime()) &&
    parsed.toISOString().slice(0, 10) === value
    ? value
    : "";
};

export function userFilters(query: string): UserFilters {
  const params = new URLSearchParams(query);
  const oneOf = (key: string, values: string[], fallback = "") => {
    const value = params.get(key) || "";
    return values.includes(value) ? value : fallback;
  };
  const page = Number(params.get("page") || 0);
  return {
    q: (params.get("q") || "").slice(0, 200),
    status: oneOf("status", ["active", "disabled"]),
    role: oneOf("role", ["user", "admin"]),
    email_verified: oneOf(
      "email_verified",
      ["true", "false"],
      params.get("status") === "unverified" ? "false" : "",
    ),
    created_from: date(params.get("created_from")),
    created_to: date(params.get("created_to")),
    sort: oneOf("sort", ["created_asc", "created_desc"], "created_desc"),
    page: Number.isSafeInteger(page) && page >= 0 && page <= 400000 ? page : 0,
  };
}

export function userRecord(query: string): string {
  const values = new URLSearchParams(query).getAll("record");
  return values.length === 1 && values[0].length <= 200 ? values[0] : "";
}

export function userListParams(filters: UserFilters, api = false) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(filters)) {
    if (key !== "page" && value !== "") params.set(key, String(value));
  }
  if (api) {
    params.set("limit", "25");
    params.set("offset", String(filters.page * 25));
    // Match the local calendar dates shown in the table, including DST changes.
    for (const key of ["created_from", "created_to"] as const) {
      if (!filters[key]) continue;
      const boundary = new Date(`${filters[key]}T00:00:00`);
      if (key === "created_to") boundary.setDate(boundary.getDate() + 1);
      params.set(key, boundary.toISOString());
    }
  } else if (filters.page) params.set("page", String(filters.page));
  return params;
}

export function validInitialPassword(value: string) {
  return (
    Array.from(value).length >= 10 &&
    new TextEncoder().encode(value).length <= 72
  );
}

export type UserAuditData = {
  previous_values?: unknown;
  next_values?: unknown;
  previous_status?: string;
  next_status?: string;
  details?: Record<string, unknown>;
};

const auditFields = new Set([
  "email",
  "display_name",
  "admin_note",
  "role",
  "status",
  "email_verified",
  "must_change_password",
  "available",
  "frozen",
  "credits",
  "amount",
  "request_key",
  "demo",
  "out_trade_no",
  "upstream_id",
  "evidence",
  "settlement_status",
  "execution_status",
  "delivery_status",
  "procurement_status",
  "kind",
  "model",
  "task_id",
  "order_id",
  "user_id",
  "amount_fen",
  "purpose",
]);

function auditObject(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

export function userAuditValues(entry: UserAuditData) {
  const details = entry.details || {};
  const previous = {
    ...auditObject(
      entry.previous_values ??
        details.previous_values ??
        details.before ??
        details.previous,
    ),
  };
  const next = {
    ...auditObject(entry.next_values ?? details.next_values ?? details.after),
  };
  const previousStatus = entry.previous_status ?? details.previous_status;
  const nextStatus = entry.next_status ?? details.next_status;
  if (typeof previousStatus === "string" && previousStatus)
    previous.status ??= previousStatus;
  if (typeof nextStatus === "string" && nextStatus) next.status ??= nextStatus;
  const changes = Array.from(
    new Set([...Object.keys(previous), ...Object.keys(next)]),
  )
    .filter((key) => auditFields.has(key))
    .map((key) => ({ key, previous: previous[key], next: next[key] }));
  const facts = Object.entries(details)
    .filter(([key]) => auditFields.has(key))
    .map(([key, value]) => ({ key, value }));
  return { changes, facts };
}
