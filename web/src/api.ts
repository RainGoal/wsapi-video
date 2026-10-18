import { t } from "./i18n";
export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public data?: unknown,
  ) {
    super(message);
  }
}
let csrf = "";
let session = 0;
export function setCSRF(value: string) {
  csrf = value;
  session += 1;
}
export const sessionVersion = () => session;
export const isCurrentSession = (version: number) => version === session;
export function csrfToken() {
  return csrf;
}
const sessionChanges = new Set<Promise<unknown>>();
export async function waitForSessionChanges() {
  while (sessionChanges.size) await Promise.allSettled([...sessionChanges]);
}
export function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  const pending = request<T>(path, method, body);
  if (
    (method === "POST" &&
      ["/auth/login", "/auth/logout", "/auth/change-password"].includes(
        path,
      )) ||
    (method === "DELETE" && path.startsWith("/auth/sessions/"))
  ) {
    sessionChanges.add(pending);
    const settled = () => {
      sessionChanges.delete(pending);
    };
    void pending.then(settled, settled);
  }
  return pending;
}
async function request<T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  const version = sessionVersion();
  const form = body instanceof FormData;
  const headers: Record<string, string> = { Accept: "application/json" };
  if (body !== undefined && !form) headers["Content-Type"] = "application/json";
  if (method !== "GET" && csrf) headers["X-CSRF-Token"] = csrf;
  let response: Response;
  try {
    response = await fetch(`/api/v1${path}`, {
      method,
      credentials: "same-origin",
      headers,
      ...(body === undefined
        ? {}
        : { body: form ? body : JSON.stringify(body) }),
    });
  } catch {
    throw new ApiError(
      0,
      "network_error",
      t("无法连接服务，请检查网络后重试。"),
    );
  }
  const result = await response.json().catch(() => null);
  const publicAuth =
    /^\/auth\/(login|register|verify|resend-verification|forgot-password|reset-password)$/.test(
      path,
    );
  if (
    response.status === 401 &&
    !publicAuth &&
    path !== "/me" &&
    path !== "/auth/logout" &&
    isCurrentSession(version)
  )
    window.dispatchEvent(new Event("frame:unauthorized"));
  if (!response.ok)
    throw new ApiError(
      response.status,
      result?.code || "request_failed",
      result?.message || t("请求失败（{0}）", response.status),
      result?.data,
    );
  if (!result || result.code !== "ok")
    throw new ApiError(
      response.status,
      result?.code || "invalid_response",
      result?.message || t("服务返回了无法识别的响应。"),
    );
  return result.data as T;
}
export const errorMessage = (error: unknown) =>
  error instanceof Error ? t(error.message) : t("操作失败，请重试。");
export function items<T>(
  value:
    | T[]
    | {
        items: T[];
      },
): T[] {
  return Array.isArray(value) ? value : value.items || [];
}
export function query(values: Record<string, string | undefined>) {
  const params = new URLSearchParams();
  Object.entries(values).forEach(([key, value]) => {
    if (value) params.set(key, value);
  });
  return params.size ? `?${params}` : "";
}
