import { useEffect, useRef, useState } from "react";
import {
  api,
  ApiError,
  errorMessage,
  isCurrentSession,
  sessionVersion,
} from "./api";
import { validGenerationQuote } from "./generation-quote";
import {
  completePendingSubmission,
  persistPendingSubmission,
  readPendingSubmission,
  submissionHasValidTask,
  submissionRecoveryKey,
  submissionWasRejected,
  withSubmissionRecoveryLock,
  type PendingSubmission,
} from "./submission-recovery";
import type { Quote, Task } from "./types";
import { t } from "./i18n";

export function useProductSwapSubmission(
  userID: string,
  projectID: string,
  onAccepted: (task: Task) => void,
) {
  const key = submissionRecoveryKey(userID, projectID);
  const [pending, setPending] = useState<PendingSubmission | null>(null);
  const [error, setError] = useState("");
  const [storageError, setStorageError] = useState("");
  const [busy, setBusy] = useState(false);
  const [ready, setReady] = useState(false);
  const live = useRef(false);
  const running = useRef(false);
  const confirmed = useRef<{ payload: PendingSubmission; task?: Task } | null>(
    null,
  );
  const accepted = useRef(onAccepted);
  accepted.current = onAccepted;

  function reload() {
    try {
      setPending(readPendingSubmission(localStorage, key));
      if (!navigator.locks) throw new Error("Web Locks unavailable");
      setStorageError("");
      setReady(true);
    } catch {
      setStorageError(
        t(
          "无法读取提交恢复记录。请启用本地存储，并使用支持 Web Locks 的浏览器通过 HTTPS 或本机地址访问。",
        ),
      );
      setReady(false);
    }
  }
  useEffect(() => {
    live.current = true;
    reload();
    const changed = () => reload();
    window.addEventListener("storage", changed);
    window.addEventListener("frame:submission-recovery", changed);
    return () => {
      live.current = false;
      window.removeEventListener("storage", changed);
      window.removeEventListener("frame:submission-recovery", changed);
    };
  }, [key]);

  async function submit(quote?: Quote, productID?: string) {
    if (running.current || !ready) return;
    if (
      !pending &&
      !confirmed.current &&
      (!productID || !validGenerationQuote(quote, productID))
    ) {
      setError(t("报价已失效，请重新获取报价。"));
      return;
    }
    const version = sessionVersion();
    const current = () => live.current && isCurrentSession(version);
    running.current = true;
    setBusy(true);
    setError("");
    let payload: PendingSubmission | null = pending || null;
    const retry = Boolean(payload);
    let sent = false;
    try {
      if (!confirmed.current) {
        payload ||= { quote_id: quote!.id, request_key: crypto.randomUUID() };
        const value = payload;
        await withSubmissionRecoveryLock(navigator.locks, key, () => {
          if (!current())
            throw new Error(t("登录状态已变化，请重新打开页面。"));
          if (!retry && !validGenerationQuote(quote, productID!))
            throw new Error(t("报价已失效，请重新获取报价。"));
          persistPendingSubmission(localStorage, key, value);
        });
        if (!current()) return;
        setPending(value);
        window.dispatchEvent(new Event("frame:submission-recovery"));
        sent = true;
        const task = await api<unknown>("/tasks", "POST", value);
        if (!submissionHasValidTask(task, projectID))
          throw new Error(t("任务回执不完整，提交结果尚未确认。"));
        if (!current()) return;
        confirmed.current = { payload: value, task };
      }
      await finish();
    } catch (failure) {
      if (!current()) return;
      if (
        sent &&
        payload &&
        failure instanceof ApiError &&
        submissionWasRejected(failure.status, failure.code, retry)
      ) {
        confirmed.current = { payload };
        await finish();
      }
      setError(errorMessage(failure));
      if (!sent && !confirmed.current) reload();
    } finally {
      running.current = false;
      if (current()) setBusy(false);
    }

    async function finish() {
      const receipt = confirmed.current;
      if (!receipt) return;
      try {
        await withSubmissionRecoveryLock(navigator.locks, key, () => {
          if (!current()) return;
          // Keep the receipt if the page disappears before delivery.
          completePendingSubmission(localStorage, key, receipt.payload, () => {
            if (!current()) return false;
            if (receipt.task) accepted.current(receipt.task);
          });
        });
        if (!current()) return;
        confirmed.current = null;
        setPending(null);
        setStorageError("");
        window.dispatchEvent(new Event("frame:submission-recovery"));
      } catch {
        if (current())
          setStorageError(
            t(
              "结果已确认，但恢复记录未能清理。点击重试清理，不会再次请求生成。",
            ),
          );
      }
    }
  }
  return {
    pending,
    busy,
    ready,
    error,
    storageError,
    reload,
    submit,
    confirmed: Boolean(confirmed.current),
  };
}
