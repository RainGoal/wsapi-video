import { useEffect, useRef, useState, type FormEvent } from "react";
import { api, ApiError, errorMessage } from "../api";
import { Icon, LoadingError, Notice, formatDate } from "../ui";
import { a, Details, Field, Pager } from "./common";
import "./procurement-review.css";

const statuses = ["pending", "matched", "disputed", "resolved"] as const;
type ReviewStatus = (typeof statuses)[number];
type Review = {
  id: string;
  task_id: string;
  revision: number;
  admin_id: string;
  status: ReviewStatus;
  reason: string;
  evidence: string;
  created_at: string;
};
type ReviewPage = {
  items: Review[];
  total: number;
  limit: number;
  offset: number;
  current: Review;
};
function statusName(status: ReviewStatus) {
  return status === "matched"
    ? a("核对一致", "Matched")
    : status === "disputed"
      ? a("存在差异", "Disputed")
      : status === "resolved"
        ? a("人工决议", "Manual decision")
        : a("待核对", "Pending review");
}
function validRecord(record: Review, taskId: string): boolean {
  return (
    Boolean(record) &&
    typeof record.id === "string" &&
    Boolean(record.id) &&
    record.task_id === taskId &&
    typeof record.admin_id === "string" &&
    Boolean(record.admin_id) &&
    Number.isSafeInteger(record.revision) &&
    record.revision > 0 &&
    statuses.includes(record.status) &&
    typeof record.reason === "string" &&
    typeof record.evidence === "string" &&
    typeof record.created_at === "string" &&
    Number.isFinite(Date.parse(record.created_at))
  );
}
function validPage(
  data: ReviewPage,
  taskId: string,
  page: number,
  size: number,
): boolean {
  if (
    !data ||
    !Array.isArray(data.items) ||
    !Number.isSafeInteger(data.total) ||
    data.total < 0 ||
    data.offset !== page * size ||
    data.limit !== size ||
    !data.current ||
    data.items.length !== Math.min(size, Math.max(0, data.total - data.offset))
  )
    return false;
  if (data.total === 0) {
    return (
      data.current.revision === 0 &&
      data.current.status === "pending" &&
      [
        data.current.id,
        data.current.task_id,
        data.current.admin_id,
        data.current.reason,
        data.current.evidence,
        data.current.created_at,
      ].every((value) => value === "")
    );
  }
  return (
    validRecord(data.current, taskId) &&
    data.items.every(
      (item, index) =>
        validRecord(item, taskId) &&
        item.revision <= data.current.revision &&
        (index === 0 || data.items[index - 1].revision > item.revision),
    ) &&
    new Set(data.items.map((item) => item.id)).size === data.items.length
  );
}

export function ProcurementReview({ taskId }: { taskId: string }) {
  const size = 25;
  const [page, setPage] = useState(0),
    [version, setVersion] = useState(0);
  const key = `${taskId}:${page}:${version}`;
  const [result, setResult] = useState<{
    key: string;
    data: ReviewPage | null;
    error: string;
  } | null>(null);
  const [status, setStatus] = useState<ReviewStatus>("pending"),
    [reason, setReason] = useState(""),
    [evidence, setEvidence] = useState("");
  const [busy, setBusy] = useState(false),
    [needsRefresh, setNeedsRefresh] = useState(false);
  const [issue, setIssue] = useState<"conflict" | "unknown" | null>(null),
    [submitError, setSubmitError] = useState("");
  const [reasonError, setReasonError] = useState(""),
    [evidenceError, setEvidenceError] = useState("");
  const [receipt, setReceipt] = useState<Review | null>(null);
  const edited = useRef(false),
    submitting = useRef(false),
    mounted = useRef(true);
  const currentResult = result?.key === key ? result : null;
  const reading = currentResult === null;
  const snapshot = !busy && !needsRefresh ? currentResult?.data : null;
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  function refresh() {
    if (submitting.current) return;
    setPage(0);
    setVersion((value) => value + 1);
  }
  // A pending read never exposes the previous page as the current conclusion.
  useEffect(() => {
    let live = true;
    void api<ReviewPage>(
      `/admin/tasks/${encodeURIComponent(taskId)}/procurement-reviews?limit=${size}&offset=${page * size}`,
    )
      .then((data) => {
        if (!validPage(data, taskId, page, size))
          throw new Error(
            a(
              "核对记录响应无效，请刷新。",
              "Invalid review response. Refresh the records.",
            ),
          );
        if (!live) return;
        if (page > 0 && page * size >= data.total) {
          setPage(Math.max(0, Math.ceil(data.total / size) - 1));
          return;
        }
        setResult({ key, data, error: "" });
        setNeedsRefresh(false);
        setSubmitError("");
        if (!edited.current) setStatus(data.current.status);
      })
      .catch((failure) => {
        if (live) setResult({ key, data: null, error: errorMessage(failure) });
      });
    return () => {
      live = false;
    };
  }, [taskId, page, key]);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (submitting.current || !snapshot || reading) return;
    const cleanReason = reason.trim(),
      cleanEvidence = evidence.trim();
    const invalidReason =
      new TextEncoder().encode(cleanReason).byteLength < 3 ||
      Array.from(cleanReason).length > 2000;
    const invalidEvidence = Array.from(cleanEvidence).length > 2000;
    setReasonError(
      invalidReason
        ? a(
            "请补充具体核对原因，最多 2,000 个字符。",
            "Provide a specific review reason, up to 2,000 characters.",
          )
        : "",
    );
    setEvidenceError(
      invalidEvidence
        ? a(
            "核对依据最多 2,000 个字符。",
            "Review evidence must not exceed 2,000 characters.",
          )
        : "",
    );
    if (invalidReason || invalidEvidence) return;
    const expectedRevision = snapshot.current.revision;
    submitting.current = true;
    setBusy(true);
    setNeedsRefresh(true);
    setSubmitError("");
    setReceipt(null);
    setIssue(null);
    try {
      const saved = await api<Review>(
        `/admin/tasks/${encodeURIComponent(taskId)}/procurement-reviews`,
        "POST",
        {
          expected_revision: expectedRevision,
          status,
          reason: cleanReason,
          evidence: cleanEvidence,
        },
      );
      if (
        !validRecord(saved, taskId) ||
        saved.revision !== expectedRevision + 1 ||
        saved.status !== status
      )
        throw new Error(
          a(
            "未收到可确认的保存回执。",
            "No verifiable save receipt was returned.",
          ),
        );
      if (!mounted.current) return;
      setReceipt(saved);
      setReason("");
      setEvidence("");
      edited.current = false;
      setPage(0);
      setVersion((value) => value + 1);
    } catch (failure) {
      if (!mounted.current) return;
      if (failure instanceof ApiError && failure.status === 409)
        setIssue("conflict");
      else {
        setSubmitError(errorMessage(failure));
        if (
          !(failure instanceof ApiError) ||
          ![400, 401, 403, 404, 422].includes(failure.status) ||
          failure.code === "invalid_response"
        )
          setIssue("unknown");
      }
    } finally {
      submitting.current = false;
      if (mounted.current) setBusy(false);
    }
  }
  return (
    <section
      className="procurement-review"
      aria-label={a("采购人工核对", "Manual procurement review")}
    >
      <div className="procurement-review-heading">
        <h3>{a("采购人工核对", "Manual procurement review")}</h3>
        <button
          type="button"
          className="button secondary small"
          disabled={busy || reading}
          onClick={refresh}
        >
          <Icon name="refresh" size={14} />
          {a("刷新核对记录", "Refresh reviews")}
        </button>
      </div>
      <p className="admin-help">
        {a(
          "记录人工核对结论与依据，保留每次修订。不改变用户积分、任务状态或供应端账单，也不调用外部网关。",
          "Record manual conclusions and evidence with immutable revisions. This does not change user credits, task state or provider bills, or call external gateways.",
        )}
      </p>
      <LoadingError
        loading={reading && !busy}
        error={currentResult?.error || ""}
        retry={refresh}
      />
      {busy && (
        <Notice>
          {a("正在保存人工核对记录…", "Saving the manual review…")}
        </Notice>
      )}
      {receipt && (
        <Notice>
          {a(
            `核对记录 v${receipt.revision} 已保存；当前结论以最新读取为准。`,
            `Review v${receipt.revision} was saved. The current conclusion is established by the latest read.`,
          )}
        </Notice>
      )}
      {issue === "conflict" && (
        <Notice error>
          {!snapshot
            ? a(
                "记录已由其他管理员更新。输入已保留，请刷新当前记录，核对后再提交。",
                "Another administrator updated the records. Your input is retained. Refresh and review the current conclusion before submitting again.",
              )
            : a(
                "当前记录已刷新，原输入已保留。请核对最新结论后再手动提交。",
                "The current record was refreshed and your input is retained. Review it before submitting manually.",
              )}
        </Notice>
      )}
      {issue === "unknown" && (
        <Notice error>
          {!snapshot
            ? a(
                "保存结果暂未确认。请先刷新并核对最新记录，避免重复提交；输入不会自动重发。",
                "The save outcome is unconfirmed. Refresh and inspect the latest records before submitting again. Your input will not be resent automatically.",
              )
            : a(
                "当前记录已刷新。请核对本次内容是否已经写入，再决定是否手动提交；输入不会自动重发。",
                "The current record was refreshed. Check whether this review was already saved before deciding to submit manually. Input is not resent automatically.",
              )}
        </Notice>
      )}
      {submitError && <Notice error>{submitError}</Notice>}
      {needsRefresh && !busy && !issue && (
        <Notice>
          {a(
            "请刷新当前记录后再提交，输入会继续保留。",
            "Refresh the current record before submitting again. Your input is retained.",
          )}
        </Notice>
      )}
      {snapshot && (
        <>
          <div className="procurement-review-current">
            <div className="procurement-review-summary">
              <strong>{a("最新结论", "Current conclusion")}</strong>
              <span className={`status status-${snapshot.current.status}`}>
                {statusName(snapshot.current.status)}
              </span>
              <span className="mono">v{snapshot.current.revision}</span>
            </div>
            {snapshot.current.revision === 0 ? (
              <p className="admin-help">
                {a(
                  "尚无人工核对记录，初始状态为待核对。",
                  "No manual review has been recorded. The initial state is pending review.",
                )}
              </p>
            ) : (
              <ReviewRecord record={snapshot.current} />
            )}
          </div>
          <h4>{a("核对历史", "Review history")}</h4>
          {snapshot.items.length ? (
            <ol className="procurement-review-history">
              {snapshot.items.map((record) => (
                <li key={record.id}>
                  <details>
                    <summary>
                      <span className="mono">v{record.revision}</span>
                      <span>{statusName(record.status)}</span>
                      <time dateTime={record.created_at}>
                        {formatDate(record.created_at)}
                      </time>
                    </summary>
                    <ReviewRecord record={record} />
                  </details>
                </li>
              ))}
            </ol>
          ) : (
            <p className="admin-help">
              {a("没有已保存的核对历史。", "There is no saved review history.")}
            </p>
          )}
          <Pager
            total={snapshot.total}
            count={snapshot.items.length}
            page={page}
            size={size}
            onPage={setPage}
          />
        </>
      )}
      <form
        className="procurement-review-form"
        onSubmit={(event) => void submit(event)}
      >
        <h4>{a("追加核对结论", "Append a review")}</h4>
        <p className="admin-help">
          {snapshot
            ? a(
                `本次提交基于当前记录 v${snapshot.current.revision}。历史翻页不会改变当前版本。`,
                `This submission uses current revision v${snapshot.current.revision}. Browsing history does not change that revision.`,
              )
            : a(
                "读取并确认当前记录后才可提交。输入将保留在本次详情窗口中。",
                "Read and verify the current record before submitting. Input is retained in this detail window.",
              )}
        </p>
        <Field label={a("人工核对状态", "Manual review status")}>
          <select
            value={status}
            disabled={busy}
            onChange={(event) => {
              edited.current = true;
              setStatus(event.target.value as ReviewStatus);
            }}
          >
            {statuses.map((value) => (
              <option value={value} key={value}>
                {statusName(value)}
              </option>
            ))}
          </select>
        </Field>
        <Field
          label={a("核对原因", "Review reason")}
          hint={a(
            `必填 · ${Array.from(reason.trim()).length.toLocaleString()} / 2,000 字符`,
            `Required · ${Array.from(reason.trim()).length.toLocaleString()} / 2,000 characters`,
          )}
          error={reasonError}
        >
          <textarea
            required
            rows={3}
            value={reason}
            disabled={busy}
            onChange={(event) => {
              edited.current = true;
              setReason(event.target.value);
              setReasonError("");
            }}
          />
        </Field>
        <Field
          label={a("核对依据（可选）", "Review evidence (optional)")}
          hint={a(
            `可填写外部记录编号或核对说明，请勿填写密钥或完整签名地址。${Array.from(evidence.trim()).length.toLocaleString()} / 2,000 字符`,
            `Use external record references or review notes. Do not include keys or full signed URLs. ${Array.from(evidence.trim()).length.toLocaleString()} / 2,000 characters`,
          )}
          error={evidenceError}
        >
          <textarea
            rows={3}
            value={evidence}
            disabled={busy}
            onChange={(event) => {
              edited.current = true;
              setEvidence(event.target.value);
              setEvidenceError("");
            }}
          />
        </Field>
        <div className="procurement-review-submit">
          <button
            className="button primary"
            disabled={busy || !snapshot || reading}
          >
            {busy
              ? a("正在保存…", "Saving…")
              : a("保存人工核对记录", "Save manual review")}
          </button>
        </div>
      </form>
    </section>
  );
}

function ReviewRecord({ record }: { record: Review }) {
  return (
    <div className="procurement-review-record">
      <Details
        values={[
          [a("记录编号", "Record ID"), record.id],
          [a("管理员编号", "Administrator ID"), record.admin_id],
          [a("记录版本", "Revision"), `v${record.revision}`],
          [
            a("记录时间", "Recorded at"),
            <time dateTime={record.created_at}>
              {formatDate(record.created_at)}
            </time>,
          ],
        ]}
      />
      <dl className="procurement-review-text">
        <div>
          <dt>{a("核对原因", "Reason")}</dt>
          <dd>{record.reason}</dd>
        </div>
        <div>
          <dt>{a("核对依据", "Evidence")}</dt>
          <dd>{record.evidence || a("未填写", "Not provided")}</dd>
        </div>
      </dl>
    </div>
  );
}
