import {
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import { api, ApiError, errorMessage } from "../api";
import { useStudio } from "../App";
import { ProcurementReview } from "./ProcurementReview";
import {
  adjustmentHasValidWallet,
  adjustmentWasRejected,
  clearPendingAdjustment,
  persistPendingAdjustment,
  readPendingAdjustment,
  withAdjustmentRecoveryLock,
  type PendingAdjustment,
} from "./adjustment-recovery";
import type { Ledger, PaymentOrder, Task, User, Wallet } from "../types";
import {
  Empty,
  Icon,
  LoadingError,
  Modal,
  Notice,
  Status,
  formatDate,
  statusLabel,
  useResource,
} from "../ui";
import {
  a,
  Check,
  CopyID,
  Details,
  Field,
  money,
  Pager,
  Reason,
  SaveBar,
  type PageResult,
} from "./common";

type Kind = "users" | "tasks" | "orders" | "audit";
type Row = Record<string, unknown>;
type AdminTask = Task & {
  user_id: string;
  upstream_id: string;
  credential_ref: string;
  attempts: number;
  attempt_history?: {
    id: string;
    attempt: number;
    action: string;
    status: string;
    created_at: string;
    completed_at?: string;
  }[];
};
type AdminOrder = PaymentOrder & {
  user_id: string;
  merchant_pid: string;
  provider_origin: string;
  paid_at?: string;
  credited_at?: string;
};
type UserDetail = { user: User; wallet: Wallet; active_sessions: number };
type Action = {
  name: string;
  label: string;
  endpoint: string;
  method?: string;
  body?: Record<string, unknown>;
  description: string;
  bind?: boolean;
};
const endpoints: Record<Kind, string> = {
  users: "/admin/users",
  tasks: "/admin/tasks",
  orders: "/admin/payment/orders",
  audit: "/admin/audit",
};
const labels: Record<Kind, [string, string]> = {
  users: ["平台用户", "Platform users"],
  tasks: ["全部生成任务", "Generation tasks"],
  orders: ["支付与到账记录", "Payments and credit records"],
  audit: ["不可变操作记录", "Immutable operation records"],
};
export function RecordsPage({
  kind,
  initialRecordId = "",
  initialStatus = "",
}: {
  kind: Kind;
  initialRecordId?: string;
  initialStatus?: string;
}) {
  const [search, setSearch] = useState(initialRecordId),
    [q, setQ] = useState(initialRecordId),
    [status, setStatus] = useState(initialStatus),
    [mediaKind, setMediaKind] = useState(""),
    [page, setPage] = useState(0),
    [selected, setSelected] = useState<Row | null>(
      initialRecordId && ["tasks", "orders"].includes(kind)
        ? { id: initialRecordId }
        : null,
    );
  useEffect(() => {
    const timer = setTimeout(() => {
      setQ(search);
      setPage(0);
    }, 300);
    return () => clearTimeout(timer);
  }, [search]);
  const params = new URLSearchParams({
    limit: "25",
    offset: String(page * 25),
  });
  if (q) params.set("q", q);
  if (status) params.set(kind === "audit" ? "action" : "status", status);
  if (mediaKind) params.set("kind", mediaKind);
  const resource = useResource<PageResult<Row>>(
    `${endpoints[kind]}?${params}`,
    { items: [] },
  );
  const filters: Record<Kind, [string, string, string][]> = {
    users: [
      ["active", "正常", "Active"],
      ["disabled", "已停用", "Disabled"],
      ["unverified", "待验证", "Unverified"],
    ],
    tasks: [
      ["submission_unknown", "提交待确认", "Submission unknown"],
      ["generating", "生成中", "Generating"],
      ["completed", "生成完成", "Completed"],
      ["failed", "生成失败", "Failed"],
      ["review_required", "需人工核查", "Review required"],
      [
        "procurement_review_disputed",
        "人工采购差异",
        "Manual procurement disputes",
      ],
      ["frozen", "积分冻结", "Credits frozen"],
      ["frozen_overdue", "冻结积分逾期", "Overdue credit holds"],
      ["lease_expired", "工作租约过期", "Expired worker leases"],
      ["archive_failed", "归档异常", "Archival exceptions"],
    ],
    orders: [
      ["pending", "等待支付", "Pending"],
      ["paid", "已支付", "Paid"],
      ["expired", "已过期", "Expired"],
      ["review_required", "待核查", "Review required"],
    ],
    audit: [
      ["product.save", "商品配置", "Product changes"],
      ["payment_tier.save", "充值档位", "Package changes"],
      ["settings.update", "系统配置", "System settings"],
      ["wallet.adjust", "积分调整", "Credit adjustments"],
      ["task.refund", "任务退款", "Task refunds"],
      ["user.set_status", "用户状态", "User status"],
      ["user.revoke_sessions", "撤销会话", "Session revocation"],
    ],
  };
  return (
    <>
      <div className="admin-section-toolbar">
        <div>
          <h2>{a(...labels[kind])}</h2>
          <p>
            {kind === "tasks"
              ? a(
                  "执行、交付与结算分别记录。先核查原任务，再执行有依据的异常处理。",
                  "Execution, delivery and settlement are tracked independently. Verify the original task before taking an operational action.",
                )
              : kind === "audit"
                ? a(
                    "系统配置、账户、商品、资金和任务处理操作均保留管理员身份与原因。",
                    "Configuration, accounts, products, credits and task actions retain their administrator identity and reason.",
                  )
                : kind === "orders"
                  ? a(
                      "核查会主动查询支付渠道。只有服务端确认支付成功才会补偿到账。",
                      "Reconciliation queries the payment provider. Credit is granted only after server-verified payment.",
                    )
                  : a(
                      "查看用户余额与账本，管理账户状态和有效会话。",
                      "Inspect user balances and ledgers, account status and active sessions.",
                    )}
          </p>
        </div>
        <button
          className="button secondary small"
          disabled={resource.loading}
          onClick={resource.refresh}
        >
          <Icon name="refresh" size={14} />
          {a("刷新列表", "Refresh")}
        </button>
      </div>
      <div className="admin-filter-bar">
        <div className="search-box">
          <Icon name="search" />
          <input
            value={search}
            maxLength={200}
            aria-label={a("搜索记录", "Search records")}
            placeholder={
              kind === "users"
                ? a("搜索邮箱、姓名或用户编号", "Search email, name or user ID")
                : a("搜索编号、名称或关键字", "Search ID, name or keyword")
            }
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>
        <select
          value={status}
          aria-label={a("状态筛选", "Status filter")}
          onChange={(e) => {
            setStatus(e.target.value);
            setPage(0);
          }}
        >
          <option value="">
            {kind === "audit"
              ? a("全部操作", "All actions")
              : a("全部状态", "All statuses")}
          </option>
          {filters[kind].map(([value, zh, en]) => (
            <option key={value} value={value}>
              {a(zh, en)}
            </option>
          ))}
        </select>
        {kind === "tasks" && (
          <select
            aria-label={a("媒体类型", "Media type")}
            value={mediaKind}
            onChange={(e) => {
              setMediaKind(e.target.value);
              setPage(0);
            }}
          >
            <option value="">{a("全部类型", "All media")}</option>
            <option value="image">{a("图片", "Images")}</option>
            <option value="video">{a("视频", "Videos")}</option>
          </select>
        )}
        {(search || status || mediaKind) && (
          <button
            className="text-button"
            onClick={() => {
              setSearch("");
              setStatus("");
              setMediaKind("");
              setPage(0);
            }}
          >
            {a("清除筛选", "Reset filters")}
          </button>
        )}
      </div>
      <LoadingError
        loading={resource.loading}
        error={resource.error}
        retry={resource.refresh}
      />
      {!resource.loading &&
        !resource.error &&
        (resource.data.items.length ? (
          <>
            <div className="table-wrap admin-table">
              <table>
                <thead>
                  <tr>
                    {(kind === "users"
                      ? [
                          a("用户", "User"),
                          a("角色 / 验证", "Role / verification"),
                          a("账户状态", "Account status"),
                          a("注册时间", "Registered"),
                          a("操作", "Actions"),
                        ]
                      : kind === "tasks"
                        ? [
                            a("任务 / 模型", "Task / model"),
                            a("执行 / 交付", "Execution / delivery"),
                            a("积分 / 结算", "Credits / settlement"),
                            a("时间 / 异常", "Time / exception"),
                            a("操作", "Actions"),
                          ]
                        : kind === "orders"
                          ? [
                              a("订单 / 用户", "Order / user"),
                              a("金额 / 积分", "Price / credits"),
                              a("支付 / 到账", "Payment / credit"),
                              a("创建时间", "Created"),
                              a("操作", "Actions"),
                            ]
                          : [
                              a("操作时间", "Time"),
                              a("管理员 / 操作", "Administrator / action"),
                              a("对象", "Target"),
                              a("操作原因", "Reason"),
                              a("详情", "Details"),
                            ]
                    ).map((label) => (
                      <th key={label}>{label}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {resource.data.items.map((row) => (
                    <tr key={String(row.id)}>
                      {kind === "users" ? (
                        <>
                          <td>
                            <strong>{String(row.display_name || "—")}</strong>
                            <small className="cell-sub">
                              {String(row.email)}
                            </small>
                          </td>
                          <td>
                            {row.role === "admin"
                              ? a("管理员", "Administrator")
                              : a("创作者", "Creator")}
                            <small
                              className={`cell-sub ${row.email_verified ? "text-green" : "muted"}`}
                            >
                              {row.email_verified
                                ? a("邮箱已验证", "Email verified")
                                : a("邮箱未验证", "Email unverified")}
                            </small>
                          </td>
                          <td>
                            <Status value={String(row.status)} />
                          </td>
                          <td className="muted nowrap">
                            {formatDate(String(row.created_at))}
                          </td>
                        </>
                      ) : kind === "tasks" ? (
                        <>
                          <td>
                            <strong>{String(row.model)}</strong>
                            <small className="cell-sub mono">
                              {String(row.id)}
                            </small>
                            {row.demo === true && (
                              <span className="demo-badge">DEMO</span>
                            )}
                          </td>
                          <td>
                            <Status value={String(row.execution_status)} />
                            <small className="cell-sub muted">
                              {statusLabel(String(row.delivery_status))}
                            </small>
                          </td>
                          <td>
                            <strong className="mono">
                              {Number(row.credits).toLocaleString()}
                            </strong>
                            <small className="cell-sub">
                              {statusLabel(String(row.settlement_status))}
                            </small>
                          </td>
                          <td>
                            <span className="muted nowrap">
                              {formatDate(String(row.created_at))}
                            </span>
                            {Boolean(row.error_message) && (
                              <small className="cell-sub admin-cell-error">
                                {String(row.error_message)}
                              </small>
                            )}
                          </td>
                        </>
                      ) : kind === "orders" ? (
                        <>
                          <td>
                            <strong className="mono">
                              {String(row.out_trade_no)}
                            </strong>
                            <small className="cell-sub mono muted">
                              {String(row.user_id || "—")}
                            </small>
                          </td>
                          <td>
                            <strong>{money(Number(row.amount_fen))}</strong>
                            <small className="cell-sub muted">
                              {Number(row.credits).toLocaleString()} CREDITS
                            </small>
                          </td>
                          <td>
                            <Status value={String(row.payment_status)} />
                            <small className="cell-sub">
                              {statusLabel(String(row.credit_status))}
                            </small>
                          </td>
                          <td className="muted nowrap">
                            {formatDate(String(row.created_at))}
                          </td>
                        </>
                      ) : (
                        <>
                          <td className="muted nowrap">
                            {formatDate(String(row.created_at))}
                          </td>
                          <td>
                            <strong>{String(row.action)}</strong>
                            <small className="cell-sub mono muted">
                              {String(row.admin_id || row.actor_id || "—")}
                            </small>
                          </td>
                          <td className="mono admin-cell-id">
                            {String(row.target_id || row.target_user_id || "—")}
                          </td>
                          <td className="admin-cell-reason">
                            {String(row.reason || "—")}
                          </td>
                        </>
                      )}
                      <td>
                        <button
                          className="button secondary small"
                          onClick={() => setSelected(row)}
                        >
                          {kind === "tasks"
                            ? a("查看与处理", "Inspect")
                            : a("查看详情", "Details")}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <Pager
              total={resource.data.total}
              count={resource.data.items.length}
              page={page}
              onPage={setPage}
            />
          </>
        ) : (
          <Empty
            icon={kind === "audit" ? "lock" : "tasks"}
            title={a("没有匹配的记录", "No matching records")}
            description={a(
              "调整搜索和筛选条件后重试。",
              "Adjust your search and filters.",
            )}
          />
        ))}
      {selected && (
        <RecordDetail
          kind={kind}
          row={selected}
          onClose={() => setSelected(null)}
          onChanged={resource.refresh}
        />
      )}
    </>
  );
}
function RecordDetail({
  kind,
  row,
  onClose,
  onChanged,
}: {
  kind: Kind;
  row: Row;
  onClose: () => void;
  onChanged: () => void;
}) {
  const { user, notify, refreshWallet } = useStudio();
  const resource = useResource<unknown>(
    kind === "audit"
      ? null
      : `${endpoints[kind]}/${encodeURIComponent(String(row.id))}`,
    kind === "audit" ? row : null,
  );
  const [action, setAction] = useState<Action | null>(null),
    [reason, setReason] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [upstream, setUpstream] = useState(""),
    [evidence, setEvidence] = useState("");
  const detailId =
    kind === "users"
      ? (resource.data as UserDetail | null)?.user?.id
      : (resource.data as { id?: string } | null)?.id;
  const detailReady =
    !resource.loading &&
    !resource.error &&
    (kind === "audit" || detailId === String(row.id));
  async function perform(e: FormEvent) {
    e.preventDefault();
    if (!detailReady || !currentAction || busy) return;
    setBusy(true);
    setError("");
    try {
      await api(currentAction.endpoint, currentAction.method || "POST", {
        ...currentAction.body,
        reason,
        ...(currentAction.bind ? { upstream_id: upstream, evidence } : {}),
      });
      setAction(null);
      setReason("");
      resource.refresh();
      onChanged();
      refreshWallet();
      notify(a("运营操作已完成并记录审计", "Operation completed and audited"));
    } catch (failure) {
      setError(errorMessage(failure));
    } finally {
      setBusy(false);
    }
  }
  function choose(value: Action) {
    if (!detailReady || busy) return;
    setAction(value);
    setReason("");
    setError("");
    setEvidence("");
    setUpstream("");
  }
  const actions: Action[] = [];
  let content: ReactNode = null;
  if (detailReady && resource.data && kind === "users") {
    const d = resource.data as UserDetail,
      u = d.user;
    if (u.id !== user.id) {
      actions.push({
        name: "status",
        label:
          u.status === "active"
            ? a("停用账户", "Disable account")
            : a("启用账户", "Enable account"),
        endpoint: `/admin/users/${u.id}`,
        method: "PATCH",
        body: { status: u.status === "active" ? "disabled" : "active" },
        description:
          u.status === "active"
            ? a(
                "停用后该用户无法登录，已有会话将被撤销。历史项目与资金记录保留。",
                "Disabling prevents login and revokes existing sessions. Historical projects and financial records are retained.",
              )
            : a("恢复用户登录权限。", "Restore the user's sign-in access."),
      });
      if (d.active_sessions > 0)
        actions.push({
          name: "sessions",
          label: a("撤销全部会话", "Revoke all sessions"),
          endpoint: `/admin/users/${u.id}/revoke-sessions`,
          description: a(
            "立即让该用户的全部设备退出登录。",
            "Immediately signs this user out on every device.",
          ),
        });
    }
    content = (
      <>
        <div className="admin-person">
          <span className="user-avatar">
            {u.display_name?.slice(0, 1) || u.email.slice(0, 1)}
          </span>
          <div>
            <h3>{u.display_name || a("创作者", "Creator")}</h3>
            <p>{u.email}</p>
          </div>
          <Status value={u.status} />
        </div>
        <Details
          values={[
            [a("用户编号", "User ID"), <CopyID value={u.id} />],
            [
              a("角色", "Role"),
              u.role === "admin"
                ? a("管理员", "Administrator")
                : a("创作者", "Creator"),
            ],
            [
              a("邮箱验证", "Email verification"),
              u.email_verified
                ? a("已验证", "Verified")
                : a("未验证", "Unverified"),
            ],
            [a("有效会话", "Active sessions"), d.active_sessions],
            [a("注册时间", "Registered"), formatDate(u.created_at)],
          ]}
        />
        <div className="admin-balance">
          <div>
            <span>{a("可用积分", "Available credits")}</span>
            <strong>{d.wallet.available.toLocaleString()}</strong>
          </div>
          <div>
            <span>{a("冻结积分", "Frozen credits")}</span>
            <strong>{d.wallet.frozen.toLocaleString()}</strong>
          </div>
        </div>
        <UserLedger userID={u.id} />
      </>
    );
  } else if (detailReady && resource.data && kind === "tasks") {
    const t = resource.data as AdminTask;
    if (t.settlement_status === "consumed")
      actions.push({
        name: "refund",
        label: a("退回已消费积分", "Refund consumed credits"),
        endpoint: `/admin/tasks/${t.id}/refund`,
        description: a(
          `将 ${t.credits} 积分退回用户可用余额，并新增反向流水；历史消费流水保留。`,
          `Return ${t.credits} credits to the user's balance using a reversal entry. Historical charges are retained.`,
        ),
      });
    if (t.settlement_status === "frozen") {
      if (t.execution_status === "submission_unknown")
        actions.push({
          name: "bind_upstream",
          label: a("绑定已核查原任务", "Bind verified provider task"),
          endpoint: `/admin/tasks/${t.id}/resolve`,
          body: { action: "bind_upstream" },
          bind: true,
          description: a(
            "只绑定已通过原服务凭证核实的既有任务。不得重新创建生成任务。",
            "Bind only an existing provider task verified with the original credential. Do not create another generation.",
          ),
        });
      if (t.execution_status === "completed" && t.delivery_status !== "ready")
        actions.push({
          name: "retry_archive",
          label: a("重试保存成品", "Retry media archiving"),
          endpoint: `/admin/tasks/${t.id}/resolve`,
          body: { action: "retry_archive" },
          description: a(
            "只重新下载并归档原任务的成品，不再生成、不重复扣费。",
            "Download and archive the original task outputs again. No new generation or charge is created.",
          ),
        });
      if (
        t.execution_status === "submission_unknown" ||
        t.procurement_status === "disputed" ||
        t.execution_status === "failed"
      )
        actions.push({
          name: "release",
          label: a("核查并释放冻结积分", "Release frozen credits"),
          endpoint: `/admin/tasks/${t.id}/resolve`,
          body: { action: "release" },
          description: a(
            `核查供应端受理与采购结果后，将 ${t.credits} 冻结积分释放给用户。此决议会结束任务自动处理。`,
            `After verifying provider acceptance and procurement results, return ${t.credits} frozen credits to the user. This ends automatic task processing.`,
          ),
        });
    }
    content = (
      <>
        <div className="admin-task-state">
          <div>
            <small>{a("执行", "Execution")}</small>
            <Status value={t.execution_status} />
          </div>
          <div>
            <small>{a("交付", "Delivery")}</small>
            <Status value={t.delivery_status} />
          </div>
          <div>
            <small>{a("结算", "Settlement")}</small>
            <Status value={t.settlement_status} />
          </div>
          <div>
            <small>{a("采购异常处置", "Procurement exception handling")}</small>
            <Status value={t.procurement_status} />
          </div>
        </div>
        {t.error_message && (
          <Notice
            error={
              Boolean(t.error_code) ||
              ["failed", "submission_unknown"].includes(t.execution_status) ||
              ["error", "archive_review"].includes(t.delivery_status)
            }
          >
            {t.error_code ? `${t.error_code} · ` : ""}
            {t.error_message}
          </Notice>
        )}
        <Details
          values={[
            [a("任务编号", "Task ID"), <CopyID value={t.id} />],
            [a("用户编号", "User ID"), <CopyID value={t.user_id} />],
            [a("项目编号", "Project ID"), <CopyID value={t.project_id} />],
            [a("模型商品", "Model product"), `${t.model} / ${t.product_id}`],
            [
              a("固定报价", "Quoted credits"),
              `${t.credits.toLocaleString()} CREDITS`,
            ],
            [a("上游任务编号", "Provider task ID"), t.upstream_id || "—"],
            [a("凭证引用", "Credential reference"), t.credential_ref || "—"],
            [a("尝试次数", "Attempts"), t.attempts],
            [a("创建时间", "Created"), formatDate(t.created_at)],
            [a("更新时间", "Updated"), formatDate(t.updated_at)],
          ]}
        />
        <ProcurementReview key={t.id} taskId={t.id} />
        <h3 className="admin-form-section">
          {a("生成输入", "Generation input")}
        </h3>
        <p className="admin-prompt">{t.prompt}</p>
        <details className="admin-disclosure">
          <summary>
            {a("查看输入与商品快照", "Input and product snapshot")}
          </summary>
          <pre className="snapshot">
            {JSON.stringify(
              { input: t.input, product_snapshot: t.product_snapshot },
              null,
              2,
            )}
          </pre>
        </details>
        {Boolean(t.attempt_history?.length) && (
          <>
            <h3 className="admin-form-section">
              {a("处理尝试记录", "Processing history")}
            </h3>
            <div className="admin-timeline">
              {t.attempt_history?.map((attempt) => (
                <div key={attempt.id}>
                  <i />
                  <div>
                    <strong>
                      {attempt.action} · {attempt.status}
                    </strong>
                    <small>
                      {a("第", "Attempt ")}
                      {attempt.attempt} · {formatDate(attempt.created_at)}
                      {attempt.completed_at
                        ? ` → ${formatDate(attempt.completed_at)}`
                        : ""}
                    </small>
                  </div>
                </div>
              ))}
            </div>
          </>
        )}
        {!actions.length && (
          <Notice>
            {a(
              "当前状态不需要人工资金动作。生成、交付和结算由服务端按状态推进。",
              "This state requires no manual credit action. The server advances generation, delivery and settlement.",
            )}
          </Notice>
        )}
      </>
    );
  } else if (detailReady && resource.data && kind === "orders") {
    const o = resource.data as AdminOrder;
    if (o.credit_status !== "credited")
      actions.push({
        name: "check",
        label: a("向支付渠道核查 / 补偿", "Reconcile with payment provider"),
        endpoint: `/admin/payment/orders/${o.id}/check`,
        description: a(
          "主动查单并校验商户、订单和精确金额。仅真实支付成功才补偿到账，同一订单不会重复入账。",
          "Query the provider and verify merchant, order and exact amount. Only confirmed payment grants credits; the same order cannot be credited twice.",
        ),
      });
    content = (
      <>
        <div className="admin-task-state">
          <div>
            <small>{a("支付状态", "Payment")}</small>
            <Status value={o.payment_status} />
          </div>
          <div>
            <small>{a("积分到账", "Credit delivery")}</small>
            <Status value={o.credit_status} />
          </div>
        </div>
        <div className="admin-balance">
          <div>
            <span>{a("应付金额", "Amount")}</span>
            <strong>{money(o.amount_fen)}</strong>
          </div>
          <div>
            <span>{a("到账积分", "Credits")}</span>
            <strong>{o.credits.toLocaleString()}</strong>
          </div>
        </div>
        <Details
          values={[
            [a("平台订单编号", "Platform order ID"), <CopyID value={o.id} />],
            [
              a("商户订单号", "Merchant order reference"),
              <CopyID value={o.out_trade_no} />,
            ],
            [a("用户编号", "User ID"), <CopyID value={o.user_id} />],
            [a("支付方式", "Payment method"), o.method],
            [a("商户 PID", "Merchant PID"), o.merchant_pid || "—"],
            [a("支付服务", "Payment provider"), o.provider_origin || "—"],
            [a("创建时间", "Created"), formatDate(o.created_at)],
            [a("过期时间", "Expires"), formatDate(o.expires_at)],
            [a("支付时间", "Paid at"), formatDate(o.paid_at)],
            [a("到账时间", "Credited at"), formatDate(o.credited_at)],
          ]}
        />
      </>
    );
  } else if (kind === "audit")
    content = (
      <>
        <Details
          values={[
            [a("记录编号", "Record ID"), <CopyID value={String(row.id)} />],
            [a("操作", "Action"), String(row.action)],
            [
              a("管理员编号", "Administrator ID"),
              String(row.admin_id || row.actor_id || "—"),
            ],
            [
              a("操作对象", "Target"),
              String(row.target_id || row.target_user_id || "—"),
            ],
            [a("原因", "Reason"), String(row.reason || "—")],
            [a("操作时间", "Time"), formatDate(String(row.created_at))],
          ]}
        />
        <h3 className="admin-form-section">{a("审计详情", "Audit details")}</h3>
        <pre className="snapshot">
          {JSON.stringify(row.details || row, null, 2)}
        </pre>
      </>
    );
  const currentAction =
    action && detailReady
      ? actions.find(
          (item) =>
            item.name === action.name &&
            item.endpoint === action.endpoint &&
            item.method === action.method &&
            JSON.stringify(item.body) === JSON.stringify(action.body),
        )
      : null;
  return (
    <Modal
      title={
        kind === "users"
          ? a("用户与积分详情", "User and credit details")
          : kind === "tasks"
            ? a("生成任务详情", "Generation task details")
            : kind === "orders"
              ? a("支付订单详情", "Payment order details")
              : a("审计记录详情", "Audit record details")
      }
      onClose={onClose}
      wide
    >
      <LoadingError
        loading={resource.loading}
        error={resource.error}
        retry={resource.refresh}
      />
      {detailReady && content}
      {detailReady && actions.length > 0 && (
        <div className="admin-detail-actions">
          {actions.map((item) => (
            <button
              type="button"
              className={`button ${currentAction?.name === item.name ? "primary" : "secondary"}`}
              key={item.name}
              disabled={busy}
              onClick={() => choose(item)}
            >
              {item.label}
            </button>
          ))}
        </div>
      )}
      {currentAction && (
        <form className="admin-action-form" onSubmit={perform}>
          <h3>{currentAction.label}</h3>
          <Notice>{currentAction.description}</Notice>
          {currentAction.bind && (
            <>
              <Field label={a("原上游任务编号", "Original provider task ID")}>
                <input
                  required
                  value={upstream}
                  onChange={(e) => setUpstream(e.target.value)}
                />
              </Field>
              <Field
                label={a("可追溯核查证据", "Traceable verification evidence")}
              >
                <textarea
                  required
                  minLength={10}
                  maxLength={4000}
                  rows={3}
                  value={evidence}
                  onChange={(e) => setEvidence(e.target.value)}
                />
              </Field>
            </>
          )}
          <Reason value={reason} onChange={setReason} />
          <SaveBar
            busy={busy}
            error={error}
            onCancel={() => setAction(null)}
            label={a("确认执行并记录审计", "Confirm and audit")}
          />
        </form>
      )}
    </Modal>
  );
}
function UserLedger({ userID }: { userID: string }) {
  const [page, setPage] = useState(0);
  const ledger = useResource<PageResult<Ledger>>(
    `/admin/users/${userID}/ledger?limit=25&offset=${page * 25}`,
    { items: [] },
  );
  return (
    <>
      <h3 className="admin-form-section">{a("积分流水", "Credit ledger")}</h3>
      <LoadingError
        loading={ledger.loading}
        error={ledger.error}
        retry={ledger.refresh}
      />
      {!ledger.loading &&
        !ledger.error &&
        (ledger.data.items.length ? (
          <>
            <div className="table-wrap admin-table">
              <table>
                <thead>
                  <tr>
                    <th>{a("时间 / 事件", "Time / event")}</th>
                    <th>{a("可用变动", "Available change")}</th>
                    <th>{a("冻结变动", "Frozen change")}</th>
                    <th>{a("原因", "Reason")}</th>
                  </tr>
                </thead>
                <tbody>
                  {ledger.data.items.map((entry) => (
                    <tr key={entry.id}>
                      <td>
                        <strong>{entry.event_type}</strong>
                        <small className="cell-sub muted">
                          {formatDate(entry.created_at)}
                        </small>
                      </td>
                      <td className="mono">
                        {entry.available_delta > 0 ? "+" : ""}
                        {entry.available_delta}
                      </td>
                      <td className="mono">
                        {entry.frozen_delta > 0 ? "+" : ""}
                        {entry.frozen_delta}
                      </td>
                      <td>{entry.reason}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <Pager
              total={ledger.data.total}
              count={ledger.data.items.length}
              page={page}
              onPage={setPage}
            />
          </>
        ) : (
          <p className="admin-help">
            {a("该用户暂无积分流水。", "This user has no credit entries.")}
          </p>
        ))}
    </>
  );
}
export function AdjustmentPage() {
  const { user } = useStudio();
  return <AdjustmentForm key={user.id} adminID={user.id} />;
}
function AdjustmentForm({ adminID }: { adminID: string }) {
  const { notify, refreshWallet } = useStudio();
  const storageFailure = () =>
    a(
      "无法可靠读取或保存本地调账恢复记录，已阻止发送。请恢复浏览器存储后重载；已有未确认记录时先核对账本，不要另建调账。",
      "The local recovery record cannot be reliably read or saved. Sending is blocked. Restore browser storage and reload; reconcile any unconfirmed adjustment before creating another.",
    );
  const [initialRecovery] = useState(() => {
    try {
      return {
        pending: readPendingAdjustment(localStorage, adminID),
        error: "",
      };
    } catch {
      return { pending: null, error: storageFailure() };
    }
  });
  const [search, setSearch] = useState(""),
    [q, setQ] = useState(""),
    [selected, setSelected] = useState<PendingAdjustment["target"] | null>(
      initialRecovery.pending?.target || null,
    ),
    [direction, setDirection] = useState(
      (initialRecovery.pending?.payload.amount || 0) < 0 ? "subtract" : "add",
    ),
    [amount, setAmount] = useState(
      initialRecovery.pending
        ? String(Math.abs(initialRecovery.pending.payload.amount))
        : "",
    ),
    [reason, setReason] = useState(
      initialRecovery.pending?.payload.reason || "",
    ),
    [confirmed, setConfirmed] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [pending, setPending] = useState<PendingAdjustment | null>(
      initialRecovery.pending,
    ),
    [storageError, setStorageError] = useState(initialRecovery.error),
    [settled, setSettled] = useState<"success" | "rejected" | null>(null);
  const busyRef = useRef(false),
    live = useRef(true);
  useEffect(() => {
    live.current = true;
    return () => {
      live.current = false;
    };
  }, []);
  const locked = busy || Boolean(pending) || Boolean(storageError);
  useEffect(() => {
    const timer = setTimeout(() => setQ(search), 300);
    return () => clearTimeout(timer);
  }, [search]);
  const users = useResource<PageResult<User>>(
    q ? `/admin/users?q=${encodeURIComponent(q)}&limit=10&offset=0` : null,
    { items: [] },
  );
  const detail = useResource<UserDetail | null>(
    selected ? `/admin/users/${selected.id}` : null,
    null,
  );
  const userSearchReady =
    Boolean(q) && search === q && !users.loading && !users.error;
  const selectedDetail =
    selected &&
    !detail.loading &&
    !detail.error &&
    detail.data?.user.id === selected.id
      ? detail.data
      : null;
  const numericAmount = Number(amount) * (direction === "subtract" ? -1 : 1);
  const invalid =
    !selected ||
    !selectedDetail ||
    !Number.isSafeInteger(numericAmount) ||
    numericAmount === 0 ||
    Math.abs(numericAmount) > 1000000000 ||
    reason.trim().length < 4 ||
    !confirmed;
  async function clearConfirmedRequest(
    entry: PendingAdjustment,
    outcome: "success" | "rejected",
  ) {
    try {
      await withAdjustmentRecoveryLock(navigator.locks, adminID, () => {
        if (live.current) clearPendingAdjustment(localStorage, entry);
      });
    } catch {
      if (!live.current) return;
      setSettled(outcome);
      setStorageError(
        a(
          "服务端结果已确认，但本地恢复记录未能清理。恢复浏览器存储后重试清理；不会再次发送调账。",
          "The server result is confirmed, but the local recovery record could not be cleared. Restore browser storage and retry cleanup; no adjustment will be sent again.",
        ),
      );
      return;
    }
    if (!live.current) return;
    setPending(null);
    setSettled(null);
    setStorageError("");
    setConfirmed(false);
    if (outcome === "success") {
      setAmount("");
      setReason("");
    }
  }
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busyRef.current || !live.current) return;
    if (settled && pending) {
      busyRef.current = true;
      setBusy(true);
      try {
        await clearConfirmedRequest(pending, settled);
      } finally {
        busyRef.current = false;
        if (live.current) setBusy(false);
      }
      return;
    }
    if (storageError || (invalid && !pending)) return;
    const isRetry = Boolean(pending);
    const entry: PendingAdjustment = pending || {
      admin_id: adminID,
      target: {
        id: selected!.id,
        email: selected!.email,
        display_name: selected!.display_name,
      },
      payload: {
        user_id: selected!.id,
        amount: numericAmount,
        reason,
        request_key: crypto.randomUUID(),
      },
    };
    busyRef.current = true;
    setBusy(true);
    try {
      await withAdjustmentRecoveryLock(navigator.locks, adminID, () => {
        if (live.current) persistPendingAdjustment(localStorage, entry);
      });
    } catch {
      busyRef.current = false;
      if (!live.current) return;
      setBusy(false);
      setStorageError(
        navigator.locks
          ? storageFailure()
          : a(
              "当前浏览器无法安全保护跨标签调账。请使用支持 Web Locks 的新版浏览器，并通过 HTTPS 或本机地址访问。",
              "This browser cannot safely protect adjustments across tabs. Use a current browser with Web Locks over HTTPS or a local address.",
            ),
      );
      return;
    }
    if (!live.current) {
      busyRef.current = false;
      return;
    }
    setPending(entry);
    setError("");
    try {
      const wallet = await api<unknown>(
        "/admin/wallet/adjust",
        "POST",
        entry.payload,
      );
      if (!adjustmentHasValidWallet(wallet))
        throw new ApiError(
          200,
          "invalid_response",
          a(
            "服务端未返回有效钱包数据，调账结果尚未确认。请保留原请求编号重试。",
            "The server returned invalid wallet data. The adjustment remains unconfirmed; retry with the original request ID.",
          ),
        );
      if (!live.current) return;
      await clearConfirmedRequest(entry, "success");
      if (!live.current) return;
      detail.refresh();
      refreshWallet();
      notify(
        a("积分调账已入账并保留审计", "Credit adjustment posted and audited"),
      );
    } catch (failure) {
      if (!live.current) return;
      setError(errorMessage(failure));
      if (
        failure instanceof ApiError &&
        adjustmentWasRejected(failure.status, failure.code, isRetry)
      ) {
        await clearConfirmedRequest(entry, "rejected");
      }
    } finally {
      busyRef.current = false;
      if (live.current) setBusy(false);
    }
  }
  function edit(fn: () => void) {
    if (locked || busyRef.current) return;
    fn();
    setConfirmed(false);
  }
  return (
    <div className="admin-adjust-layout">
      <section className="admin-panel">
        <div className="eyebrow">CREDIT OPERATIONS</div>
        <h2>{a("人工积分调整", "Manual credit adjustment")}</h2>
        <p className="admin-help">
          {a(
            "调账只新增可追溯流水，不改写历史记录。冻结积分由具体任务处理，不能在此直接改动。",
            "Adjustments add traceable entries without rewriting history. Frozen credits are handled through their tasks and cannot be edited here.",
          )}
        </p>
        <form onSubmit={submit}>
          <Field
            label={a("查找用户", "Find a user")}
            hint={a(
              "搜索邮箱、姓名或完整用户编号后选择。",
              "Search an email, name or full user ID, then select the user.",
            )}
          >
            <input
              value={search}
              maxLength={200}
              disabled={locked}
              onChange={(e) =>
                edit(() => {
                  setSearch(e.target.value);
                  setSelected(null);
                })
              }
              placeholder={a("输入用户邮箱或编号", "Enter user email or ID")}
            />
          </Field>
          {q && !selected && (
            <div className="admin-user-results">
              <LoadingError
                loading={users.loading || search !== q}
                error={search === q ? users.error : ""}
                retry={users.refresh}
              />
              {userSearchReady &&
                users.data.items.map((u) => (
                  <button
                    type="button"
                    key={u.id}
                    onClick={() => edit(() => setSelected(u))}
                  >
                    <strong>{u.display_name || u.email}</strong>
                    <span>{u.email}</span>
                    <small className="mono">{u.id}</small>
                  </button>
                ))}
              {userSearchReady && !users.data.items.length && (
                <p>{a("未找到用户", "No users found")}</p>
              )}
            </div>
          )}
          {selected && (
            <div className="admin-selected-user">
              <div>
                <strong>{selected.display_name || selected.email}</strong>
                <span>{selected.email}</span>
                <CopyID value={selected.id} />
              </div>
              {!locked && (
                <button
                  type="button"
                  className="text-button"
                  onClick={() => edit(() => setSelected(null))}
                >
                  {a("更换", "Change")}
                </button>
              )}
            </div>
          )}
          <div className="admin-form-grid">
            <Field label={a("调整方向", "Adjustment direction")}>
              <select
                value={direction}
                disabled={locked}
                onChange={(e) => edit(() => setDirection(e.target.value))}
              >
                <option value="add">
                  {a("增加可用积分", "Add available credits")}
                </option>
                <option value="subtract">
                  {a("扣减可用积分", "Subtract available credits")}
                </option>
              </select>
            </Field>
            <Field label={a("积分数量", "Credit amount")}>
              <input
                type="number"
                required
                min={1}
                max={1000000000}
                step={1}
                value={amount}
                disabled={locked}
                onChange={(e) => edit(() => setAmount(e.target.value))}
              />
            </Field>
          </div>
          <fieldset disabled={locked}>
            <Reason
              value={reason}
              onChange={(value) => edit(() => setReason(value))}
            />
            {!pending && (
              <Check
                label={a(
                  "我已核对目标用户、积分方向和数量，确认此次调整。",
                  "I have verified the user, direction and credit amount.",
                )}
                checked={confirmed}
                disabled={!selectedDetail}
                onChange={(value) => {
                  if (!locked && !busyRef.current) setConfirmed(value);
                }}
              />
            )}
          </fieldset>
          {pending && !busy && (
            <Notice>
              {settled
                ? a(
                    "服务端结果已确认，仅需清理本地恢复记录。",
                    "The server result is confirmed; only local recovery cleanup remains.",
                  )
                : a(
                    "已恢复并锁定上次请求的用户、方向、数量和原因。结果尚未确认；仅重试原请求，勿另建调账。离开页面或退出后，同一管理员仍可恢复。",
                    "The previous user, direction, amount and reason are restored and locked. Its result is unconfirmed. Retry only this request. The same administrator can recover it after leaving or signing out.",
                  )}
            </Notice>
          )}
          {pending && (
            <Details
              values={[
                [
                  a("原调账请求编号", "Original adjustment request ID"),
                  <CopyID value={pending.payload.request_key} />,
                ],
              ]}
            />
          )}
          {storageError && !settled && (
            <button
              type="button"
              className="button secondary"
              onClick={() => window.location.reload()}
            >
              {a("重新读取恢复记录", "Reload recovery record")}
            </button>
          )}
          <SaveBar
            busy={busy}
            error={storageError || error}
            dirty={
              Boolean(settled) ||
              (!storageError && (!invalid || Boolean(pending)))
            }
            label={
              settled
                ? a("重试清理本地记录", "Retry local cleanup")
                : pending
                  ? a("重试同一调账请求", "Retry the same adjustment")
                  : a("确认调账并记录审计", "Confirm adjustment and audit")
            }
          />
        </form>
      </section>
      <aside className="admin-panel admin-adjust-preview">
        <div className="eyebrow">ADJUSTMENT PREVIEW</div>
        <h2>{a("调账核对", "Review adjustment")}</h2>
        {selected ? (
          <>
            <LoadingError
              loading={detail.loading}
              error={detail.error}
              retry={detail.refresh}
            />
            {selectedDetail && (
              <>
                <dl className="admin-stat-list">
                  <div>
                    <dt>{a("当前可用", "Available now")}</dt>
                    <dd>{selectedDetail.wallet.available.toLocaleString()}</dd>
                  </div>
                  <div>
                    <dt>{a("当前冻结", "Frozen now")}</dt>
                    <dd>{selectedDetail.wallet.frozen.toLocaleString()}</dd>
                  </div>
                  <div>
                    <dt>{a("本次调整", "This adjustment")}</dt>
                    <dd
                      className={numericAmount < 0 ? "text-red" : "text-green"}
                    >
                      {Number.isFinite(numericAmount)
                        ? `${numericAmount > 0 ? "+" : ""}${numericAmount.toLocaleString()}`
                        : "—"}
                    </dd>
                  </div>
                  {!pending && (
                    <div className="admin-adjust-total">
                      <dt>{a("调整后可用", "Available after")}</dt>
                      <dd>
                        {Number.isSafeInteger(numericAmount)
                          ? (
                              selectedDetail.wallet.available + numericAmount
                            ).toLocaleString()
                          : "—"}
                      </dd>
                    </div>
                  )}
                </dl>
                {pending && (
                  <Notice>
                    {a(
                      "当前余额可能已包含本次调账，不能据此判断上次是否入账。请沿用原请求核查结果。",
                      "The current balance may already include this adjustment. It cannot establish the previous result; reconcile using the original request.",
                    )}
                  </Notice>
                )}
                {!pending &&
                  selectedDetail.wallet.available + numericAmount < 0 && (
                    <Notice error>
                      {a(
                        "可用余额不足，服务端将拒绝此次扣减。",
                        "The balance is insufficient. The server will reject this debit.",
                      )}
                    </Notice>
                  )}
              </>
            )}
          </>
        ) : (
          <p className="admin-help">
            {a(
              "选择用户后显示余额与调整预览。",
              "Select a user to see balances and preview the adjustment.",
            )}
          </p>
        )}
        <div className="admin-help">
          {a(
            "服务端在事务内校验余额并使用请求编号去重。网络异常时重复提交同一请求不会重复入账。",
            "The server validates balances transactionally and deduplicates request IDs. Retrying the same request after a network failure cannot double-credit it.",
          )}
        </div>
      </aside>
    </div>
  );
}
