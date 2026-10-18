import type { Task } from "./types";

export type PendingSubmission = {
  quote_id: string;
  request_key: string;
  source_node_id?: string;
};
type RecoveryStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">;
export const submissionRecoveryKey = (userID: string, projectID: string) =>
  `frame:submission:${userID}:${projectID}`;

function parseSubmission(value: unknown): PendingSubmission {
  const entry = value as Partial<PendingSubmission> | null;
  if (
    !entry ||
    typeof entry.quote_id !== "string" ||
    !entry.quote_id ||
    entry.quote_id.length > 128 ||
    typeof entry.request_key !== "string" ||
    entry.request_key.length < 8 ||
    entry.request_key.length > 128 ||
    (entry.source_node_id !== undefined &&
      (typeof entry.source_node_id !== "string" ||
        !entry.source_node_id ||
        entry.source_node_id.length > 128))
  )
    throw new Error("Invalid pending submission record");
  return {
    quote_id: entry.quote_id,
    request_key: entry.request_key,
    ...(entry.source_node_id ? { source_node_id: entry.source_node_id } : {}),
  };
}

export function readPendingSubmission(storage: RecoveryStorage, key: string) {
  const raw = storage.getItem(key);
  return raw === null ? null : parseSubmission(JSON.parse(raw));
}

const sameSubmission = (a: PendingSubmission, b: PendingSubmission) =>
  a.quote_id === b.quote_id &&
  a.request_key === b.request_key &&
  a.source_node_id === b.source_node_id;

// All mutations must run under the same Web Lock in every tab.
export async function withSubmissionRecoveryLock(
  locks: Pick<LockManager, "request"> | undefined,
  key: string,
  action: () => void,
) {
  if (!locks) throw new Error("Submission recovery requires Web Locks");
  await locks.request(key, action);
}

// Verify persistence before sending; never replace another outstanding request.
export function persistPendingSubmission(
  storage: RecoveryStorage,
  key: string,
  value: PendingSubmission,
) {
  const entry = parseSubmission(value);
  const previous = readPendingSubmission(storage, key);
  if (previous && !sameSubmission(previous, entry))
    throw new Error("Another submission is awaiting confirmation");
  const serialized = JSON.stringify(entry);
  storage.setItem(key, serialized);
  if (storage.getItem(key) !== serialized)
    throw new Error("Pending submission could not be durably recorded");
}

export function clearPendingSubmission(
  storage: RecoveryStorage,
  key: string,
  entry: PendingSubmission,
) {
  const previous = readPendingSubmission(storage, key);
  if (previous && !sameSubmission(previous, entry))
    throw new Error("Cannot clear another pending submission");
  storage.removeItem(key);
  if (storage.getItem(key) !== null)
    throw new Error("Pending submission record was not cleared");
}

// Run under the recovery Web Lock. Deliver before cleanup so closing an editor
// cannot discard the only record of an accepted task. Canvas delivery is idempotent.
export function completePendingSubmission(
  storage: RecoveryStorage,
  key: string,
  entry: PendingSubmission,
  deliver: () => boolean | void,
) {
  const previous = readPendingSubmission(storage, key);
  if (previous && !sameSubmission(previous, entry))
    throw new Error("Cannot complete another pending submission");
  if (deliver() === false) return false;
  clearPendingSubmission(storage, key, entry);
  return true;
}

export const submissionWasRejected = (
  status: number,
  code: string,
  isRetry: boolean,
) =>
  // The server holds the quote lock and checks accepted tasks before this code.
  (status === 409 &&
    (code === "quote_expired" || code === "product_unavailable")) ||
  (!isRetry &&
    ((status === 422 &&
      (code === "invalid_input" || code === "invalid_json")) ||
      (status === 402 && code === "insufficient_credits")));

export function submissionHasValidTask(
  value: unknown,
  projectID: string,
): value is Task {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const task = value as Record<string, unknown>;
  return (
    typeof task.id === "string" &&
    /^task_[0-9a-f]{32}$/.test(task.id) &&
    task.project_id === projectID &&
    (task.kind === "image" || task.kind === "video") &&
    [
      "product_id",
      "model",
      "execution_status",
      "delivery_status",
      "settlement_status",
      "procurement_status",
    ].every(
      (field) => typeof task[field] === "string" && Boolean(task[field]),
    ) &&
    typeof task.prompt === "string" &&
    Number.isSafeInteger(task.credits) &&
    (task.credits as number) > 0 &&
    typeof task.demo === "boolean" &&
    Array.isArray(task.asset_ids) &&
    task.asset_ids.every((id) => typeof id === "string" && Boolean(id)) &&
    typeof task.created_at === "string" &&
    Number.isFinite(Date.parse(task.created_at)) &&
    typeof task.updated_at === "string" &&
    Number.isFinite(Date.parse(task.updated_at)) &&
    Boolean(task.input) &&
    typeof task.input === "object" &&
    !Array.isArray(task.input)
  );
}
