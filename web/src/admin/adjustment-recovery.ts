export type AdjustmentPayload = {
  user_id: string;
  amount: number;
  reason: string;
  request_key: string;
};
export type PendingAdjustment = {
  admin_id: string;
  target: { id: string; email: string; display_name: string };
  payload: AdjustmentPayload;
};
type RecoveryStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">;
const recoveryKey = (adminID: string) =>
  `frame:admin-adjustment:${encodeURIComponent(adminID)}`;

// Every tab uses the same lock for the synchronous read/write and read/delete
// checks. Network requests must run after releasing this lock.
export async function withAdjustmentRecoveryLock(
  locks: Pick<LockManager, "request"> | undefined,
  adminID: string,
  action: () => void,
) {
  if (!adminID || !locks)
    throw new Error("Adjustment recovery requires administrator Web Locks");
  await locks.request(recoveryKey(adminID), action);
}

export function readPendingAdjustment(
  storage: RecoveryStorage,
  adminID: string,
): PendingAdjustment | null {
  if (!adminID) throw new Error("Missing administrator identity");
  const raw = storage.getItem(recoveryKey(adminID));
  if (raw === null) return null;
  return parsePendingAdjustment(JSON.parse(raw), adminID);
}

function parsePendingAdjustment(
  entry: any,
  adminID: string,
): PendingAdjustment {
  const p = entry?.payload;
  if (
    entry?.admin_id !== adminID ||
    typeof entry.target?.id !== "string" ||
    !entry.target.id ||
    typeof entry.target.email !== "string" ||
    typeof entry.target.display_name !== "string" ||
    p?.user_id !== entry.target.id ||
    !Number.isSafeInteger(p.amount) ||
    p.amount === 0 ||
    Math.abs(p.amount) > 1000000000 ||
    typeof p.reason !== "string" ||
    p.reason.trim().length < 4 ||
    p.reason.length > 1000 ||
    typeof p.request_key !== "string" ||
    p.request_key.length < 8 ||
    p.request_key.length > 128
  )
    throw new Error("Invalid pending adjustment recovery record");
  return {
    admin_id: adminID,
    target: {
      id: entry.target.id,
      email: entry.target.email,
      display_name: entry.target.display_name,
    },
    payload: {
      user_id: p.user_id,
      amount: p.amount,
      reason: p.reason,
      request_key: p.request_key,
    },
  };
}

const samePayload = (a: AdjustmentPayload, b: AdjustmentPayload) =>
  a.user_id === b.user_id &&
  a.amount === b.amount &&
  a.reason === b.reason &&
  a.request_key === b.request_key;

// Write and verify before sending. Never replace another unconfirmed operation.
export function persistPendingAdjustment(
  storage: RecoveryStorage,
  entry: PendingAdjustment,
) {
  entry = parsePendingAdjustment(entry, entry.admin_id);
  const previous = readPendingAdjustment(storage, entry.admin_id);
  if (previous && !samePayload(previous.payload, entry.payload))
    throw new Error("Another adjustment is awaiting confirmation");
  const serialized = JSON.stringify(entry);
  storage.setItem(recoveryKey(entry.admin_id), serialized);
  if (storage.getItem(recoveryKey(entry.admin_id)) !== serialized)
    throw new Error("Pending adjustment could not be durably recorded");
}

export function clearPendingAdjustment(
  storage: RecoveryStorage,
  entry: PendingAdjustment,
) {
  const previous = readPendingAdjustment(storage, entry.admin_id);
  if (previous && !samePayload(previous.payload, entry.payload))
    throw new Error("Cannot clear a different unconfirmed adjustment");
  storage.removeItem(recoveryKey(entry.admin_id));
  if (storage.getItem(recoveryKey(entry.admin_id)) !== null)
    throw new Error("Pending adjustment recovery record was not cleared");
}

// A retry's rejection cannot establish whether an earlier attempt committed.
// These two codes are emitted before the first request opens a transaction.
export const adjustmentWasRejected = (
  status: number,
  code: string,
  isRetry: boolean,
) =>
  !isRetry &&
  status === 422 &&
  (code === "invalid_input" || code === "invalid_json");

export function adjustmentHasValidWallet(value: unknown): value is {
  available: number;
  frozen: number;
  demo: boolean;
} {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const wallet = value as Record<string, unknown>;
  return (
    Number.isSafeInteger(wallet.available) &&
    (wallet.available as number) >= 0 &&
    Number.isSafeInteger(wallet.frozen) &&
    (wallet.frozen as number) >= 0 &&
    typeof wallet.demo === "boolean"
  );
}
