import { useEffect, useRef, useState, type FormEvent } from "react";
import { api, ApiError, errorMessage } from "../api";
import { useStudio } from "../App";
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
  CopyID,
  Details,
  Field,
  money,
  Pager,
  Reason,
  SaveBar,
  type PageResult,
} from "./common";
import {
  userFilters,
  userListParams,
  userRecord,
  userAuditValues,
  validInitialPassword,
  type UserFilters,
} from "./users";
import "./users.css";

type ListedUser = User & { available: number; frozen: number };
type MailDelivery = {
  status: "none" | "queued" | "sent" | "failed" | "unconfigured";
  purpose?: "verify" | "reset";
  attempts: number;
  sent_at?: string | null;
  next_attempt_at?: string | null;
  error_code?: string;
};
type UserDetail = {
  user: User;
  wallet: Wallet;
  active_sessions: number;
  admin_note: string;
  mail_delivery?: MailDelivery;
};
type UserAction = "disable" | "enable" | "revoke" | "verify" | "reset";

export function UsersPage({ query = "" }: { query?: string }) {
  const [filters, setFilters] = useState(() => userFilters(query));
  const [search, setSearch] = useState(filters.q);
  const [selected, setSelected] = useState(() => userRecord(query));
  const [selectedTab, setSelectedTab] = useState("overview");
  const [editor, setEditor] = useState<{ id?: string } | null>(null);
  const { notify } = useStudio();
  useEffect(() => {
    const next = userFilters(query);
    setFilters(next);
    setSearch(next.q);
    setSelected(userRecord(query));
    setSelectedTab("overview");
  }, [query]);
  useEffect(() => {
    const timer = setTimeout(() => {
      setFilters((current) =>
        current.q === search ? current : { ...current, q: search, page: 0 },
      );
    }, 300);
    return () => clearTimeout(timer);
  }, [search]);
  useEffect(() => {
    const params = userListParams(filters);
    if (selected) params.set("record", selected);
    window.history.replaceState(
      window.history.state,
      "",
      `${window.location.pathname}${window.location.search}#/admin/users?${params}`,
    );
  }, [filters, selected]);
  const invalidDates = Boolean(
    filters.created_from &&
    filters.created_to &&
    filters.created_from > filters.created_to,
  );
  const resource = useResource<PageResult<ListedUser>>(
    invalidDates ? null : `/admin/users?${userListParams(filters, true)}`,
    { items: [] },
  );
  function filter(key: keyof UserFilters, value: string) {
    setFilters((current) => ({ ...current, [key]: value, page: 0 }));
  }
  return (
    <>
      <div className="admin-section-toolbar">
        <div>
          <h2>{a("平台用户", "Platform users")}</h2>
          <p>
            {a(
              "开户、资料与账号安全，在同一处完成。",
              "Manage account creation, profiles and account security.",
            )}
          </p>
        </div>
        <div className="users-toolbar-actions">
          <button
            className="button secondary small"
            disabled={resource.loading}
            onClick={resource.refresh}
          >
            <Icon name="refresh" size={14} />
            {a("刷新", "Refresh")}
          </button>
          <button
            className="button primary small"
            onClick={() => setEditor({})}
          >
            <Icon name="plus" size={14} />
            {a("新增用户", "Create user")}
          </button>
        </div>
      </div>
      <div className="users-filters">
        <div className="search-box">
          <Icon name="search" />
          <input
            value={search}
            maxLength={200}
            aria-label={a("搜索用户", "Search users")}
            placeholder={a(
              "搜索邮箱、昵称或用户编号",
              "Search email, name or user ID",
            )}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>
        <label>
          <span>{a("账号状态", "Account status")}</span>
          <select
            value={filters.status}
            onChange={(e) => filter("status", e.target.value)}
          >
            <option value="">{a("全部状态", "All statuses")}</option>
            <option value="active">{a("正常", "Active")}</option>
            <option value="disabled">{a("已停用", "Disabled")}</option>
          </select>
        </label>
        <label>
          <span>{a("邮箱验证", "Email verification")}</span>
          <select
            value={filters.email_verified}
            onChange={(e) => filter("email_verified", e.target.value)}
          >
            <option value="">{a("全部", "All")}</option>
            <option value="true">{a("已验证", "Verified")}</option>
            <option value="false">{a("待验证", "Unverified")}</option>
          </select>
        </label>
        <label>
          <span>{a("角色", "Role")}</span>
          <select
            value={filters.role}
            onChange={(e) => filter("role", e.target.value)}
          >
            <option value="">{a("全部角色", "All roles")}</option>
            <option value="user">{a("普通用户", "User")}</option>
            <option value="admin">{a("管理员", "Administrator")}</option>
          </select>
        </label>
        <label>
          <span>{a("注册起日", "Registered from")}</span>
          <input
            type="date"
            value={filters.created_from}
            onChange={(e) => filter("created_from", e.target.value)}
          />
        </label>
        <label>
          <span>{a("注册止日", "Registered through")}</span>
          <input
            type="date"
            value={filters.created_to}
            onChange={(e) => filter("created_to", e.target.value)}
          />
        </label>
        <label>
          <span>{a("排序", "Sort")}</span>
          <select
            value={filters.sort}
            onChange={(e) => filter("sort", e.target.value)}
          >
            <option value="created_desc">
              {a("最新注册优先", "Newest first")}
            </option>
            <option value="created_asc">
              {a("最早注册优先", "Oldest first")}
            </option>
          </select>
        </label>
        <button
          className="text-button"
          onClick={() => {
            setSearch("");
            setFilters(userFilters(""));
          }}
        >
          {a("重置筛选", "Reset filters")}
        </button>
      </div>
      {invalidDates ? (
        <Notice error>
          {a(
            "注册起日不能晚于止日。",
            "The start date must not follow the end date.",
          )}
        </Notice>
      ) : (
        <LoadingError
          loading={resource.loading}
          error={resource.error}
          retry={resource.refresh}
        />
      )}
      {!invalidDates &&
        !resource.loading &&
        !resource.error &&
        (resource.data.items.length ? (
          <>
            <div className="table-wrap admin-table users-table">
              <table>
                <thead>
                  <tr>
                    {[
                      a("用户信息", "User"),
                      a("角色", "Role"),
                      a("账号状态", "Status"),
                      a("邮箱验证", "Verification"),
                      a("可用积分", "Available credits"),
                      a("注册时间", "Registered"),
                      a("操作", "Actions"),
                    ].map((label) => (
                      <th key={label}>{label}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {resource.data.items.map((user) => (
                    <tr key={user.id}>
                      <td>
                        <strong>{user.display_name}</strong>
                        <small className="cell-sub">{user.email}</small>
                        <small className="cell-sub mono muted">{user.id}</small>
                      </td>
                      <td>
                        {user.role === "admin"
                          ? a("管理员", "Administrator")
                          : a("普通用户", "User")}
                      </td>
                      <td>
                        <Status value={user.status} />
                      </td>
                      <td>
                        <span
                          className={`admin-pill ${user.email_verified ? "positive" : "neutral"}`}
                        >
                          {user.email_verified
                            ? a("已验证", "Verified")
                            : a("待验证", "Unverified")}
                        </span>
                      </td>
                      <td className="mono">
                        {Number.isFinite(user.available)
                          ? user.available.toLocaleString()
                          : "—"}
                      </td>
                      <td className="muted nowrap">
                        {formatDate(user.created_at)}
                      </td>
                      <td>
                        <div className="users-row-actions">
                          <button
                            className="button secondary small"
                            onClick={() => {
                              setSelectedTab("overview");
                              setSelected(user.id);
                            }}
                          >
                            {a("查看", "View")}
                          </button>
                          <button
                            className="text-button"
                            onClick={() => setEditor({ id: user.id })}
                          >
                            {a("编辑", "Edit")}
                          </button>
                          <button
                            className="text-button"
                            onClick={() => {
                              setSelectedTab("security");
                              setSelected(user.id);
                            }}
                            aria-label={a(
                              `管理 ${user.email} 的账号安全`,
                              `Manage account security for ${user.email}`,
                            )}
                          >
                            {a("更多", "More")}
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <Pager
              total={resource.data.total}
              count={resource.data.items.length}
              page={filters.page}
              onPage={(page) => setFilters((current) => ({ ...current, page }))}
            />
          </>
        ) : (
          <Empty
            icon="group"
            title={a("没有匹配的用户", "No matching users")}
            description={a(
              "调整筛选条件，或创建一个新账号。",
              "Adjust the filters or create a new account.",
            )}
          />
        ))}
      {selected && !editor && (
        <UserDetails
          key={selected}
          id={selected}
          initialTab={selectedTab}
          onClose={() => setSelected("")}
          onEdit={() => setEditor({ id: selected })}
          onChanged={resource.refresh}
        />
      )}
      {editor && (
        <UserEditor
          key={editor.id || "create"}
          id={editor.id}
          onClose={() => setEditor(null)}
          onSaved={(id, created) => {
            setEditor(null);
            setSelectedTab("overview");
            setSelected(id);
            resource.refresh();
            notify(
              created
                ? a(
                    "账号已创建，验证邮件已加入发送队列。",
                    "Account created; the verification email is queued.",
                  )
                : a("用户资料已保存并记录审计。", "Profile saved and audited."),
            );
          }}
        />
      )}
    </>
  );
}

function UserEditor({
  id,
  onClose,
  onSaved,
}: {
  id?: string;
  onClose: () => void;
  onSaved: (id: string, created: boolean) => void;
}) {
  const detail = useResource<UserDetail | null>(
    id ? `/admin/users/${encodeURIComponent(id)}` : null,
    null,
  );
  const [busy, setBusy] = useState(false);
  return (
    <Modal
      title={
        id
          ? a("编辑用户资料", "Edit user profile")
          : a("新增普通用户", "Create user")
      }
      onClose={onClose}
      closeDisabled={busy}
      wide
    >
      <LoadingError
        loading={detail.loading}
        error={detail.error}
        retry={detail.refresh}
      />
      {(!id ||
        (!detail.loading && !detail.error && detail.data?.user.id === id)) && (
        <UserForm
          initial={id ? detail.data! : undefined}
          onBusy={setBusy}
          onClose={onClose}
          onSaved={onSaved}
        />
      )}
    </Modal>
  );
}

function UserForm({
  initial,
  onBusy,
  onClose,
  onSaved,
}: {
  initial?: UserDetail;
  onBusy: (busy: boolean) => void;
  onClose: () => void;
  onSaved: (id: string, created: boolean) => void;
}) {
  const [email, setEmail] = useState(initial?.user.email || "");
  const [name, setName] = useState(initial?.user.display_name || "");
  const [password, setPassword] = useState("");
  const [note, setNote] = useState(initial?.admin_note || "");
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [error, setError] = useState("");
  const busyRef = useRef(false);
  const live = useRef(true);
  useEffect(() => {
    live.current = true;
    return () => {
      live.current = false;
    };
  }, []);
  const emailChanged = Boolean(
    initial && email.trim().toLowerCase() !== initial.user.email,
  );
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busyRef.current) return;
    const next: Record<string, string> = {};
    if (!name.trim() || Array.from(name.trim()).length > 80)
      next.name = a(
        "请填写 1–80 个字符的昵称。",
        "Enter a name of 1–80 characters.",
      );
    if (!initial && !validInitialPassword(password))
      next.password = a(
        "密码至少 10 个字符，且不超过 72 字节。",
        "Use at least 10 characters and at most 72 bytes.",
      );
    if (reason.trim().length < 4)
      next.reason = a(
        "请填写至少 4 个字符的操作原因。",
        "Enter a reason of at least 4 characters.",
      );
    setErrors(next);
    if (Object.keys(next).length) return;
    busyRef.current = true;
    setBusy(true);
    onBusy(true);
    setError("");
    try {
      const body = {
        email: email.trim(),
        display_name: name.trim(),
        admin_note: note.trim(),
        reason: reason.trim(),
        ...(!initial ? { password } : {}),
      };
      const result = await api<{ user?: User }>(
        initial
          ? `/admin/users/${encodeURIComponent(initial.user.id)}/profile`
          : "/admin/users",
        initial ? "PATCH" : "POST",
        body,
      );
      const id = initial?.user.id || result.user?.id;
      if (!id)
        throw new Error(
          a(
            "服务返回的用户资料不完整，请按邮箱查询创建结果。",
            "The response is incomplete. Search by email to confirm account creation.",
          ),
        );
      if (live.current) {
        setPassword("");
        onSaved(id, !initial);
      }
    } catch (failure) {
      if (!live.current) return;
      const message = errorMessage(failure);
      if (failure instanceof ApiError) {
        const field = /email/.test(failure.code)
          ? "email"
          : /password/.test(failure.code)
            ? "password"
            : /name/.test(failure.code)
              ? "name"
              : /note/.test(failure.code)
                ? "note"
                : /reason/.test(failure.code)
                  ? "reason"
                  : "";
        if (field) setErrors({ [field]: message });
      }
      setError(
        failure instanceof ApiError && failure.status === 0 && !initial
          ? `${message} ${a("创建结果尚未确认，请先按邮箱查询，避免重复开户。", "The creation result is unconfirmed. Search by email before retrying.")}`
          : message,
      );
    } finally {
      busyRef.current = false;
      if (live.current) {
        setBusy(false);
        onBusy(false);
      }
    }
  }
  return (
    <form className="users-editor" onSubmit={submit}>
      {!initial && (
        <Notice>
          {a(
            "新账号为普通用户，初始积分为 0。用户验证邮箱后登录，并须修改初始密码。",
            "New accounts are ordinary users with zero credits. They must verify their email and change the initial password on first sign-in.",
          )}
        </Notice>
      )}
      <fieldset disabled={busy}>
        <div className="admin-form-grid">
          <Field
            label={a("邮箱地址", "Email address")}
            error={errors.email}
            hint={
              initial?.user.role === "admin"
                ? a(
                    "此处暂不支持修改管理员邮箱。",
                    "Administrator email addresses cannot be edited here.",
                  )
                : undefined
            }
          >
            <input
              type="email"
              autoComplete="off"
              required
              maxLength={254}
              value={email}
              readOnly={initial?.user.role === "admin"}
              onChange={(e) => setEmail(e.target.value)}
            />
          </Field>
          <Field label={a("昵称", "Display name")} error={errors.name}>
            <input
              required
              maxLength={80}
              autoComplete="off"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>
        </div>
        {emailChanged && (
          <Notice>
            {a(
              "更换邮箱会让该用户退出所有设备，旧邮件链接失效。验证新邮箱后才能重新登录。",
              "Changing the email signs the user out on every device and invalidates old email links. They must verify the new address before signing in.",
            )}
          </Notice>
        )}
        {!initial && (
          <Field
            label={a("初始密码", "Initial password")}
            error={errors.password}
            hint={a(
              "至少 10 个字符，最多 72 字节。请通过可信渠道交付用户。",
              "At least 10 characters and at most 72 bytes. Share it with the user through a trusted channel.",
            )}
          >
            <input
              type="password"
              required
              minLength={10}
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </Field>
        )}
        <Field
          label={a("内部备注", "Internal note")}
          error={errors.note}
          hint={a(
            "仅管理员可见，最多 2000 个字符。",
            "Visible only to administrators. Up to 2,000 characters.",
          )}
        >
          <textarea
            rows={3}
            maxLength={2000}
            value={note}
            onChange={(e) => setNote(e.target.value)}
          />
        </Field>
        <Reason value={reason} onChange={setReason} />
        {errors.reason && (
          <p role="alert" className="field-error-message">
            {errors.reason}
          </p>
        )}
      </fieldset>
      <SaveBar
        busy={busy}
        error={error}
        onCancel={onClose}
        label={
          initial
            ? a("保存资料", "Save profile")
            : a("创建用户并发送验证邮件", "Create user and queue verification")
        }
      />
    </form>
  );
}

function UserDetails({
  id,
  initialTab,
  onClose,
  onEdit,
  onChanged,
}: {
  id: string;
  initialTab: string;
  onClose: () => void;
  onEdit: () => void;
  onChanged: () => void;
}) {
  const { user: admin, notify, navigate } = useStudio();
  const detail = useResource<UserDetail | null>(
    `/admin/users/${encodeURIComponent(id)}`,
    null,
  );
  const [tab, setTab] = useState(initialTab);
  const [action, setAction] = useState<UserAction | null>(null);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const busyRef = useRef(false);
  const live = useRef(true);
  useEffect(() => {
    live.current = true;
    return () => {
      live.current = false;
    };
  }, []);
  const ready = !detail.loading && !detail.error && detail.data?.user.id === id;
  const value = ready ? detail.data! : null;
  function choose(next: UserAction) {
    if (busyRef.current) return;
    setAction(next);
    setReason("");
    setError("");
  }
  async function perform(event: FormEvent) {
    event.preventDefault();
    if (busyRef.current || !value || !action || reason.trim().length < 4)
      return;
    busyRef.current = true;
    setBusy(true);
    setError("");
    const suffix =
      action === "revoke"
        ? "/revoke-sessions"
        : action === "verify"
          ? "/send-verification"
          : action === "reset"
            ? "/send-password-reset"
            : "";
    try {
      await api(
        `/admin/users/${encodeURIComponent(id)}${suffix}`,
        suffix ? "POST" : "PATCH",
        {
          reason: reason.trim(),
          ...(!suffix
            ? { status: action === "disable" ? "disabled" : "active" }
            : {}),
        },
      );
      if (!live.current) return;
      setAction(null);
      detail.refresh();
      onChanged();
      notify(
        action === "verify" || action === "reset"
          ? a(
              "邮件已加入发送队列，可刷新查看投递状态。",
              "The email is queued. Refresh to check delivery status.",
            )
          : a(
              "账号操作已完成并记录审计。",
              "Account operation completed and audited.",
            ),
      );
    } catch (failure) {
      if (live.current) setError(errorMessage(failure));
    } finally {
      busyRef.current = false;
      if (live.current) setBusy(false);
    }
  }
  const actionText: Record<UserAction, [string, string]> = {
    disable: ["停用账号", "Disable account"],
    enable: ["启用账号", "Enable account"],
    revoke: ["撤销全部会话", "Revoke all sessions"],
    verify: ["补发验证邮件", "Resend verification"],
    reset: ["发送密码重置邮件", "Send password reset"],
  };
  return (
    <Modal
      title={a("用户详情", "User details")}
      onClose={onClose}
      closeDisabled={busy}
      wide
    >
      <div className="users-detail">
        <LoadingError
          loading={detail.loading}
          error={detail.error}
          retry={detail.refresh}
        />
        {value && (
          <>
            <div className="users-person">
              <span className="user-avatar">
                {value.user.display_name.slice(0, 1)}
              </span>
              <div>
                <h3>{value.user.display_name}</h3>
                <p>{value.user.email}</p>
                <CopyID value={id} />
              </div>
              <Status value={value.user.status} />
            </div>
            <div className="users-detail-tools">
              <button
                className="button secondary small"
                disabled={busy}
                onClick={onEdit}
              >
                {a("编辑资料", "Edit profile")}
              </button>
              <button
                className="button secondary small"
                disabled={busy}
                onClick={() =>
                  navigate("admin", `adjust?user_id=${encodeURIComponent(id)}`)
                }
              >
                {a("积分调账", "Adjust credits")}
              </button>
              <button
                className="text-button"
                disabled={busy}
                onClick={detail.refresh}
              >
                <Icon name="refresh" size={13} />
                {a("刷新详情", "Refresh details")}
              </button>
            </div>
            <nav
              className="users-tabs"
              aria-label={a("用户详情分区", "User detail sections")}
            >
              {[
                ["overview", "概览", "Overview"],
                ["ledger", "积分流水", "Credit ledger"],
                ["business", "任务与订单", "Tasks & orders"],
                ["security", "安全与操作记录", "Security & audit"],
              ].map(([key, zh, en]) => (
                <button
                  key={key}
                  disabled={busy}
                  aria-current={tab === key ? "page" : undefined}
                  className={tab === key ? "active" : ""}
                  onClick={() => {
                    setTab(key);
                    setAction(null);
                  }}
                >
                  {a(zh, en)}
                </button>
              ))}
            </nav>
            {tab === "overview" && (
              <>
                <div className="admin-balance">
                  <div>
                    <span>{a("可用积分", "Available credits")}</span>
                    <strong>{value.wallet.available.toLocaleString()}</strong>
                  </div>
                  <div>
                    <span>{a("冻结积分", "Frozen credits")}</span>
                    <strong>{value.wallet.frozen.toLocaleString()}</strong>
                  </div>
                </div>
                <Details
                  values={[
                    [
                      a("角色", "Role"),
                      value.user.role === "admin"
                        ? a("管理员", "Administrator")
                        : a("普通用户", "User"),
                    ],
                    [
                      a("邮箱验证", "Email verification"),
                      value.user.email_verified
                        ? a("已验证", "Verified")
                        : a("待验证", "Unverified"),
                    ],
                    [
                      a("首次改密", "Initial password"),
                      value.user.must_change_password
                        ? a("待修改", "Change required")
                        : a("无需修改", "No change required"),
                    ],
                    [a("有效会话", "Active sessions"), value.active_sessions],
                    [
                      a("注册时间", "Registered"),
                      formatDate(value.user.created_at),
                    ],
                  ]}
                />
                <div className="users-note">
                  <span>{a("内部备注", "Internal note")}</span>
                  <p>{value.admin_note || a("暂无备注", "No note yet")}</p>
                </div>
                <MailStatus mail={value.mail_delivery} />
                {!value.user.email_verified && (
                  <Notice>
                    {a(
                      "该账号尚未完成邮箱验证，当前无法登录。可在“安全与操作记录”中补发验证邮件。",
                      "This account cannot sign in until the email is verified. Resend verification under Security & audit.",
                    )}
                  </Notice>
                )}
              </>
            )}
            {tab === "ledger" && <UserLedger id={id} />}
            {tab === "business" && <UserBusiness id={id} />}
            {tab === "security" && (
              <>
                <Details
                  values={[
                    [a("有效会话", "Active sessions"), value.active_sessions],
                    [
                      a("邮箱验证", "Email verification"),
                      value.user.email_verified
                        ? a("已验证", "Verified")
                        : a("待验证", "Unverified"),
                    ],
                  ]}
                />
                <MailStatus mail={value.mail_delivery} />
                <div className="admin-detail-actions users-security-actions">
                  {value.user.status === "active" && (
                    <button
                      className="button secondary small"
                      disabled={busy}
                      onClick={() =>
                        choose(value.user.email_verified ? "reset" : "verify")
                      }
                    >
                      {a(
                        ...actionText[
                          value.user.email_verified ? "reset" : "verify"
                        ],
                      )}
                    </button>
                  )}
                  {id !== admin.id && (
                    <>
                      <button
                        className="button secondary small"
                        disabled={busy}
                        onClick={() =>
                          choose(
                            value.user.status === "active"
                              ? "disable"
                              : "enable",
                          )
                        }
                      >
                        {a(
                          ...actionText[
                            value.user.status === "active"
                              ? "disable"
                              : "enable"
                          ],
                        )}
                      </button>
                      {value.active_sessions > 0 && (
                        <button
                          className="button secondary small"
                          disabled={busy}
                          onClick={() => choose("revoke")}
                        >
                          {a(...actionText.revoke)}
                        </button>
                      )}
                    </>
                  )}
                </div>
                {action && (
                  <form className="admin-action-form" onSubmit={perform}>
                    <h3>{a(...actionText[action])}</h3>
                    <Notice>
                      {action === "disable"
                        ? a(
                            "停用立即撤销登录会话，历史项目、订单和积分记录保留。",
                            "Disabling revokes sessions immediately and retains projects, orders and credit history.",
                          )
                        : action === "revoke"
                          ? a(
                              "该用户的所有设备将退出登录。",
                              "The user will be signed out on every device.",
                            )
                          : action === "enable"
                            ? a(
                                "恢复账号状态；未验证邮箱仍须完成验证后登录。",
                                "Restore the account. Unverified users still need to verify their email.",
                              )
                            : a(
                                "系统将排队发送一次性邮件链接。加入队列不代表已经投递成功。",
                                "The system queues an email with a one-time link. Queued does not mean delivered.",
                              )}
                    </Notice>
                    <fieldset disabled={busy}>
                      <Reason value={reason} onChange={setReason} />
                    </fieldset>
                    <SaveBar
                      busy={busy}
                      error={error}
                      dirty={reason.trim().length >= 4}
                      onCancel={() => setAction(null)}
                      label={a("确认执行并记录审计", "Confirm and audit")}
                    />
                  </form>
                )}
                <UserAudit id={id} />
              </>
            )}
          </>
        )}
      </div>
    </Modal>
  );
}

function MailStatus({ mail }: { mail?: MailDelivery }) {
  const labels: Record<MailDelivery["status"], [string, string]> = {
    none: ["暂无邮件记录", "No email record"],
    queued: ["等待投递", "Queued"],
    sent: ["邮件服务器已接收", "Accepted by mail server"],
    failed: ["投递失败，等待重试", "Delivery failed; retry pending"],
    unconfigured: ["邮件服务未配置", "Email service not configured"],
  };
  const status = mail?.status || "none";
  return (
    <div
      className={`users-mail ${status === "failed" || status === "unconfigured" ? "users-mail-warning" : ""}`}
    >
      <Icon name="info" size={16} />
      <div>
        <strong>
          {a("最近邮件", "Latest email")}
          {mail?.purpose
            ? ` · ${mail.purpose === "reset" ? a("密码重置", "Password reset") : a("邮箱验证", "Email verification")}`
            : ""}
        </strong>
        <p>{a(...labels[status])}</p>
        {status === "unconfigured" && (
          <small>
            {a(
              "账号已保存，请先配置邮件服务，再补发邮件。",
              "The account is saved. Configure email service, then resend.",
            )}
          </small>
        )}
        {mail?.next_attempt_at &&
          (status === "failed" || status === "queued") && (
            <small>
              {a("下次尝试：", "Next attempt: ")}
              {formatDate(mail.next_attempt_at)}
            </small>
          )}
        {status === "failed" && (
          <small>
            {a(
              "邮件服务暂未接受此邮件，请检查服务配置或等待自动重试。",
              "The mail service has not accepted this message. Check the service configuration or wait for an automatic retry.",
            )}
          </small>
        )}
        {mail && mail.attempts > 0 && status !== "sent" && (
          <small>
            {a(
              `已尝试 ${mail.attempts} 次`,
              `${mail.attempts} delivery attempts`,
            )}
          </small>
        )}
        {mail?.sent_at && <small>{formatDate(mail.sent_at)}</small>}
      </div>
    </div>
  );
}

function UserLedger({ id }: { id: string }) {
  const [page, setPage] = useState(0);
  const result = useResource<PageResult<Ledger>>(
    `/admin/users/${encodeURIComponent(id)}/ledger?limit=25&offset=${page * 25}`,
    { items: [] },
  );
  return (
    <>
      <LoadingError
        loading={result.loading}
        error={result.error}
        retry={result.refresh}
      />
      {!result.loading &&
        !result.error &&
        (result.data.items.length ? (
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
                  {result.data.items.map((entry) => (
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
                      <td className="users-wrap-cell">{entry.reason || "—"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <Pager
              total={result.data.total}
              count={result.data.items.length}
              page={page}
              onPage={setPage}
            />
          </>
        ) : (
          <Empty icon="wallet" title={a("暂无积分流水", "No credit entries")} />
        ))}
    </>
  );
}

function UserBusiness({ id }: { id: string }) {
  const { navigate } = useStudio();
  const [kind, setKind] = useState<"tasks" | "orders">("tasks");
  const [page, setPage] = useState(0);
  const result = useResource<PageResult<Task | PaymentOrder>>(
    `${kind === "tasks" ? "/admin/tasks" : "/admin/payment/orders"}?user_id=${encodeURIComponent(id)}&limit=10&offset=${page * 10}`,
    { items: [] },
  );
  return (
    <>
      <div className="users-business-toolbar">
        <div className="users-segment">
          <button
            className={kind === "tasks" ? "active" : ""}
            onClick={() => {
              setKind("tasks");
              setPage(0);
            }}
          >
            {a("生成任务", "Generation tasks")}
          </button>
          <button
            className={kind === "orders" ? "active" : ""}
            onClick={() => {
              setKind("orders");
              setPage(0);
            }}
          >
            {a("充值订单", "Payment orders")}
          </button>
        </div>
        <button
          className="text-button"
          onClick={() =>
            navigate("admin", `${kind}?user_id=${encodeURIComponent(id)}`)
          }
        >
          {a("进入完整列表", "Open full list")} →
        </button>
      </div>
      <LoadingError
        loading={result.loading}
        error={result.error}
        retry={result.refresh}
      />
      {!result.loading &&
        !result.error &&
        (result.data.items.length ? (
          <>
            <div className="table-wrap admin-table">
              <table>
                <thead>
                  <tr>
                    <th>
                      {kind === "tasks"
                        ? a("任务 / 模型", "Task / model")
                        : a("订单 / 金额", "Order / price")}
                    </th>
                    <th>{a("状态", "Status")}</th>
                    <th>{a("创建时间", "Created")}</th>
                    <th>{a("操作", "Actions")}</th>
                  </tr>
                </thead>
                <tbody>
                  {result.data.items.map((entry) => (
                    <tr key={entry.id}>
                      <td>
                        <strong>
                          {kind === "tasks"
                            ? String(entry.model)
                            : money(Number(entry.amount_fen))}
                        </strong>
                        <small className="cell-sub mono users-wrap-cell">
                          {entry.id}
                        </small>
                      </td>
                      <td>
                        {kind === "tasks" ? (
                          <>
                            <Status value={String(entry.execution_status)} />
                            <small className="cell-sub muted">
                              <Status value={String(entry.settlement_status)} />
                            </small>
                          </>
                        ) : (
                          <>
                            <Status value={String(entry.payment_status)} />
                            <small className="cell-sub muted">
                              <Status value={String(entry.credit_status)} />
                            </small>
                          </>
                        )}
                      </td>
                      <td className="muted nowrap">
                        {formatDate(entry.created_at)}
                      </td>
                      <td>
                        <button
                          className="button secondary small"
                          onClick={() =>
                            navigate(
                              "admin",
                              `${kind}?user_id=${encodeURIComponent(id)}&record=${encodeURIComponent(entry.id)}`,
                            )
                          }
                        >
                          {a("详情", "Details")}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <Pager
              total={result.data.total}
              count={result.data.items.length}
              page={page}
              size={10}
              onPage={setPage}
            />
          </>
        ) : (
          <Empty
            icon="tasks"
            title={
              kind === "tasks"
                ? a("暂无生成任务", "No generation tasks")
                : a("暂无充值订单", "No payment orders")
            }
          />
        ))}
    </>
  );
}

type AuditRecord = {
  id: string;
  action: string;
  actor_id?: string;
  admin_id?: string;
  target_id?: string;
  reason: string;
  created_at: string;
  previous_values?: unknown;
  next_values?: unknown;
  previous_status?: string;
  next_status?: string;
  details?: Record<string, unknown>;
};

function UserAudit({ id }: { id: string }) {
  const [page, setPage] = useState(0);
  const result = useResource<PageResult<AuditRecord>>(
    `/admin/audit?target_user_id=${encodeURIComponent(id)}&limit=10&offset=${page * 10}`,
    { items: [] },
  );
  const names: Record<string, [string, string]> = {
    "user.create": ["创建用户", "User created"],
    "user.update_profile": ["编辑用户资料", "Profile updated"],
    "user.set_status": ["更改账号状态", "Account status changed"],
    "user.revoke_sessions": ["撤销会话", "Sessions revoked"],
    "user.send_verification": ["发送验证邮件", "Verification email queued"],
    "user.send_password_reset": ["发送密码重置邮件", "Password reset queued"],
    "wallet.adjust": ["积分调账", "Credit adjustment"],
    "task.refund": ["退回任务积分", "Task credits refunded"],
    "task.release": ["释放冻结积分", "Credit hold released"],
    "task.bind_upstream": [
      "绑定已核查上游任务",
      "Verified provider task linked",
    ],
    "task.retry_archive": ["重试素材归档", "Media archival retried"],
    "task.procurement_review": ["采购核对", "Procurement review"],
    "payment.check_requested": ["核查支付订单", "Payment order checked"],
  };
  return (
    <section className="users-audit">
      <h3 className="admin-form-section">
        {a("管理员操作记录", "Administrator operations")}
      </h3>
      <LoadingError
        loading={result.loading}
        error={result.error}
        retry={result.refresh}
      />
      {!result.loading &&
        !result.error &&
        (result.data.items.length ? (
          <>
            {result.data.items.map((entry) => {
              const { changes, facts } = userAuditValues(entry);
              return (
                <article key={entry.id}>
                  <div className="users-audit-heading">
                    <strong>
                      {names[entry.action]
                        ? a(...names[entry.action])
                        : entry.action}
                    </strong>
                    <time>{formatDate(entry.created_at)}</time>
                  </div>
                  <small className="mono muted">
                    {a("管理员：", "Administrator: ")}
                    {entry.actor_id || entry.admin_id || "—"}
                  </small>
                  <p>{entry.reason}</p>
                  {entry.target_id &&
                    (entry.action.startsWith("task.") ||
                      entry.action.startsWith("payment.")) && (
                      <p className="users-audit-reference">
                        <span>
                          {entry.action.startsWith("task.")
                            ? a("关联任务：", "Related task: ")
                            : a("关联订单：", "Related order: ")}
                        </span>
                        <CopyID value={entry.target_id} />
                      </p>
                    )}
                  {facts.length > 0 && (
                    <dl className="users-audit-facts">
                      {facts.map(({ key, value }) => (
                        <div key={key}>
                          <dt>{auditFieldLabel(key)}</dt>
                          <dd>{auditValue(key, value)}</dd>
                        </div>
                      ))}
                    </dl>
                  )}
                  {changes.length > 0 && (
                    <details>
                      <summary>{a("查看变更前后", "View changes")}</summary>
                      <div className="users-audit-changes">
                        <table>
                          <thead>
                            <tr>
                              <th scope="col">{a("字段", "Field")}</th>
                              <th scope="col">{a("变更前", "Before")}</th>
                              <th scope="col">{a("变更后", "After")}</th>
                            </tr>
                          </thead>
                          <tbody>
                            {changes.map(({ key, previous, next }) => (
                              <tr key={key}>
                                <th scope="row">{auditFieldLabel(key)}</th>
                                <td>{auditValue(key, previous)}</td>
                                <td>{auditValue(key, next)}</td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    </details>
                  )}
                </article>
              );
            })}
            <Pager
              total={result.data.total}
              count={result.data.items.length}
              page={page}
              size={10}
              onPage={setPage}
            />
          </>
        ) : (
          <p className="admin-help">
            {a("暂无管理员操作记录。", "No administrator operations yet.")}
          </p>
        ))}
    </section>
  );
}

function auditFieldLabel(key: string) {
  const labels: Record<string, [string, string]> = {
    email: ["邮箱", "Email"],
    display_name: ["昵称", "Display name"],
    admin_note: ["内部备注", "Internal note"],
    role: ["角色", "Role"],
    status: ["状态", "Status"],
    email_verified: ["邮箱验证", "Email verification"],
    must_change_password: ["首次改密", "Initial password change"],
    available: ["可用积分", "Available credits"],
    frozen: ["冻结积分", "Frozen credits"],
    credits: ["积分数量", "Credits"],
    amount: ["积分调整", "Credit adjustment"],
    request_key: ["请求编号", "Request ID"],
    demo: ["数据环境", "Data environment"],
    out_trade_no: ["商户订单号", "Merchant order number"],
    upstream_id: ["上游任务号", "Provider task ID"],
    evidence: ["核查依据", "Verification evidence"],
    settlement_status: ["积分结算", "Credit settlement"],
    execution_status: ["任务执行", "Task execution"],
    delivery_status: ["交付状态", "Delivery status"],
    procurement_status: ["采购状态", "Procurement status"],
    kind: ["媒体类型", "Media type"],
    model: ["模型", "Model"],
    task_id: ["任务编号", "Task ID"],
    order_id: ["订单编号", "Order ID"],
    user_id: ["用户编号", "User ID"],
    amount_fen: ["金额", "Amount"],
    purpose: ["邮件用途", "Email purpose"],
  };
  return labels[key] ? a(...labels[key]) : key;
}

function auditValue(key: string, value: unknown): string {
  if (value === undefined) return "—";
  if (value === null) return a("未设置", "Not set");
  if (value === "") return a("未填写", "Empty");
  if (typeof value === "boolean") {
    if (key === "email_verified")
      return value ? a("已验证", "Verified") : a("待验证", "Unverified");
    if (key === "must_change_password")
      return value
        ? a("需修改", "Change required")
        : a("无需修改", "No change required");
    if (key === "demo")
      return value ? a("开发演示", "Demo") : a("正式数据", "Live data");
    return value ? a("是", "Yes") : a("否", "No");
  }
  if (typeof value === "number") {
    if (key === "amount_fen") return money(value);
    if (["amount", "credits", "available", "frozen"].includes(key))
      return `${key === "amount" && value > 0 ? "+" : ""}${value.toLocaleString()} ${a("积分", "credits")}`;
    return value.toLocaleString();
  }
  if (typeof value !== "string")
    return a("详见关联业务记录", "See the related business record");
  if (key === "role")
    return value === "admin"
      ? a("管理员", "Administrator")
      : value === "user"
        ? a("普通用户", "User")
        : value;
  if (key === "purpose")
    return value === "verify"
      ? a("邮箱验证", "Email verification")
      : value === "reset"
        ? a("密码重置", "Password reset")
        : value;
  if (key === "kind")
    return value === "image"
      ? a("图片", "Image")
      : value === "video"
        ? a("视频", "Video")
        : value === "audio"
          ? a("音频", "Audio")
          : value;
  if (key === "status" || key.endsWith("_status")) return statusLabel(value);
  return value;
}
