import { useEffect, useRef, useState, type FormEvent } from "react";
import { api, ApiError, errorMessage } from "../api";
import { useStudio } from "../App";
import { Icon, LoadingError, Notice, formatDate, useResource } from "../ui";
import { a, Check, Field, Reason, SaveBar } from "./common";
import { settingsPayload } from "./validation";
import { createSettingsOperationScope } from "./settings-operation";
import { t } from "../i18n";
import {
  policyCodePointCount,
  policyFields,
  policyLengthErrors,
  policyLimit,
  type PolicyValues,
} from "../policies";

type Settings = {
  site: PolicyValues & {
    brand: string;
    registration_enabled: boolean;
    support_email: string;
  };
  media: {
    origin: string;
    output_origins: string[];
    image_key_configured?: boolean;
    video_key_configured?: boolean;
  };
  payment: {
    enabled: boolean;
    origin: string;
    pid: string;
    notify_url: string;
    return_url: string;
    key_configured?: boolean;
  };
  smtp: {
    host: string;
    port: number;
    username: string;
    from: string;
    password_configured?: boolean;
  };
  storage: {
    driver: string;
    endpoint: string;
    region: string;
    bucket: string;
    access_key_configured?: boolean;
    secret_key_configured?: boolean;
    location_locked?: boolean;
  };
};
type SettingsDocument = {
  revision: number;
  updated_at: string;
  settings: Settings;
  readiness: Record<string, unknown>;
};
type SecretName =
  | "media.image_key"
  | "media.video_key"
  | "payment.key"
  | "smtp.password"
  | "storage.access_key"
  | "storage.secret_key";
type TestResult = { ok: boolean; message: string; checked_at: string };
const sections = [
  ["site", "站点与注册", "Site & registration", "grid"],
  ["media", "生成网关", "Generation gateway", "spark"],
  ["payment", "易支付", "EasyPay", "wallet"],
  ["smtp", "邮件服务", "Email delivery", "text"],
  ["storage", "私有存储", "Private storage", "image"],
] as const;
export function SettingsPage() {
  const { notify, system, refreshSystem } = useStudio();
  const operations = useRef(createSettingsOperationScope()).current;
  useEffect(() => operations.mount(), [operations]);
  const resource = useResource<SettingsDocument | null>(
    "/admin/settings",
    null,
  );
  const [baseline, setBaseline] = useState<SettingsDocument | null>(null),
    [draft, setDraft] = useState<Settings | null>(null),
    [section, setSection] = useState<keyof Settings>("site"),
    [secrets, setSecrets] = useState<Partial<Record<SecretName, string>>>({}),
    [cleared, setCleared] = useState<SecretName[]>([]),
    [reason, setReason] = useState(""),
    [busy, setBusy] = useState(false),
    [testing, setTesting] = useState(false),
    [error, setError] = useState(""),
    [fieldErrors, setFieldErrors] = useState<Record<string, string>>({}),
    [results, setResults] = useState<Record<string, TestResult>>({});
  useEffect(() => {
    if (resource.data) {
      setBaseline(resource.data);
      setDraft(structuredClone(resource.data.settings));
      setResults({});
    }
  }, [resource.data]);
  const dirty = Boolean(
    baseline &&
    draft &&
    (JSON.stringify(draft) !== JSON.stringify(baseline.settings) ||
      Object.values(secrets).some(Boolean) ||
      cleared.length),
  );
  function update<K extends keyof Settings>(
    group: K,
    key: keyof Settings[K],
    value: unknown,
  ) {
    setDraft((current) =>
      current
        ? { ...current, [group]: { ...current[group], [key]: value } }
        : null,
    );
    setFieldErrors((current) => ({
      ...current,
      [`${group}.${String(key)}`]: "",
    }));
  }
  function reload() {
    if (!operations.available()) return;
    if (
      dirty &&
      !window.confirm(
        a(
          "重新加载会丢弃尚未保存的配置，确定继续？",
          "Reloading discards your unsaved settings. Continue?",
        ),
      )
    )
      return;
    setBaseline(null);
    setDraft(null);
    setSecrets({});
    setCleared([]);
    setReason("");
    setError("");
    setFieldErrors({});
    setResults({});
    resource.refresh();
  }
  useEffect(() => {
    if (!dirty) return;
    const prevent = (e: BeforeUnloadEvent) => {
      e.preventDefault();
    };
    const navigate = (e: Event) => {
      if (
        !window.confirm(
          a(
            "还有未保存的配置，离开会丢弃这些变更。确定离开？",
            "You have unsaved settings. Leave and discard these changes?",
          ),
        )
      )
        e.preventDefault();
    };
    window.addEventListener("beforeunload", prevent);
    window.addEventListener("frame:before-navigate", navigate);
    return () => {
      window.removeEventListener("beforeunload", prevent);
      window.removeEventListener("frame:before-navigate", navigate);
    };
  }, [dirty]);
  async function save(e: FormEvent) {
    e.preventDefault();
    if (
      !draft ||
      !baseline ||
      !dirty ||
      reason.trim().length < 4 ||
      !operations.available()
    )
      return;
    const invalidPolicies = policyLengthErrors(draft.site);
    if (invalidPolicies.length) {
      setFieldErrors(
        Object.fromEntries(
          invalidPolicies.map((key) => [
            `site.${key}`,
            a(
              "请将政策内容控制在 2000 个字符以内。",
              "Keep this policy within 2,000 Unicode characters.",
            ),
          ]),
        ),
      );
      setSection("site");
      setError(
        a(
          "部分政策内容超出长度限制，请修改后保存。",
          "Some policies exceed the length limit. Shorten them before saving.",
        ),
      );
      return;
    }
    await operations.run({
      onStart: () => {
        setBusy(true);
        setError("");
        setFieldErrors({});
      },
      request: () =>
        api<SettingsDocument>("/admin/settings", "PUT", {
          revision: baseline.revision,
          reason,
          ...settingsPayload(
            draft as unknown as Record<string, Record<string, unknown>>,
            secrets,
            cleared,
          ),
        }),
      onSuccess: (saved) => {
        setBaseline(saved);
        setDraft(structuredClone(saved.settings));
        setSecrets({});
        setCleared([]);
        setReason("");
        setResults({});
        resource.setData(saved);
        refreshSystem();
        notify(
          a(
            "系统配置已保存，服务已应用新配置",
            "Settings saved and applied to services",
          ),
        );
      },
      onError: (failure) => {
        setError(errorMessage(failure));
        if (failure instanceof ApiError) {
          const data = failure.data as
            | { fields?: Record<string, string> }
            | undefined;
          setFieldErrors(data?.fields || {});
          const firstGroup = Object.keys(data?.fields || {})[0]?.split(".")[0];
          if (sections.some(([id]) => id === firstGroup))
            setSection(firstGroup as keyof Settings);
        }
      },
      onFinish: () => setBusy(false),
    });
  }
  async function test() {
    if (dirty || section === "site") return;
    await operations.run({
      onStart: () => {
        setTesting(true);
        setError("");
      },
      request: () =>
        api<TestResult>(`/admin/settings/test/${section}`, "POST", {}),
      onSuccess: (result) =>
        setResults((old) => ({ ...old, [section]: result })),
      onError: (failure) => setError(errorMessage(failure)),
      onFinish: () => setTesting(false),
    });
  }
  function secret(
    name: SecretName,
    label: string,
    configured: boolean | undefined,
  ) {
    const clear = cleared.includes(name);
    return (
      <div className="admin-secret" key={name}>
        <Field
          label={label}
          error={fieldErrors[name]}
          hint={
            clear
              ? a(
                  "保存后将删除此凭证。",
                  "This credential will be removed when saved.",
                )
              : configured
                ? a(
                    "已保存加密凭证。留空保留原值，填写则替换。",
                    "An encrypted credential is saved. Leave blank to retain it, or enter a replacement.",
                  )
                : a("尚未配置凭证。", "No credential configured.")
          }
        >
          <input
            type="password"
            autoComplete="new-password"
            value={secrets[name] || ""}
            disabled={clear}
            onChange={(e) =>
              setSecrets((old) => ({ ...old, [name]: e.target.value }))
            }
            placeholder={
              configured
                ? a(
                    "留空保留原凭证",
                    "Leave blank to keep the saved credential",
                  )
                : a("输入服务凭证", "Enter credential")
            }
          />
        </Field>
        <div className="admin-secret-state">
          <span className={configured ? "text-green" : "muted"}>
            <Icon name="lock" size={13} />
            {configured
              ? a("已配置", "Configured")
              : a("未配置", "Not configured")}
          </span>
          {configured && (
            <Check
              label={a("清除此凭证", "Remove this credential")}
              checked={clear}
              onChange={(value) => {
                setCleared((old) =>
                  value ? [...old, name] : old.filter((item) => item !== name),
                );
                if (value) setSecrets((old) => ({ ...old, [name]: "" }));
              }}
            />
          )}
        </div>
      </div>
    );
  }
  if (!draft || !baseline)
    return (
      <LoadingError
        loading={resource.loading || !resource.error}
        error={resource.error}
        retry={resource.refresh}
      />
    );
  const input = <K extends keyof Settings>(
    group: K,
    key: keyof Settings[K],
    label: string,
    options: {
      hint?: string;
      type?: string;
      required?: boolean;
      disabled?: boolean;
      placeholder?: string;
    } = {},
  ) => (
    <Field
      label={label}
      hint={options.hint}
      error={fieldErrors[`${group}.${String(key)}`]}
    >
      <input
        value={String(draft[group][key] ?? "")}
        type={options.type || "text"}
        required={options.required}
        disabled={options.disabled}
        placeholder={options.placeholder}
        onChange={(e) =>
          update(
            group,
            key,
            options.type === "number" ? Number(e.target.value) : e.target.value,
          )
        }
      />
    </Field>
  );
  const result = dirty ? undefined : results[section];
  return (
    <div className="admin-settings-layout">
      <aside className="admin-settings-nav">
        {sections.map(([id, zh, en, icon]) => (
          <button
            key={id}
            disabled={busy || testing}
            className={section === id ? "active" : ""}
            onClick={() => {
              setSection(id);
              setError("");
            }}
          >
            <Icon name={icon} size={17} />
            <span>{a(zh, en)}</span>
            <Icon name="arrow" size={13} />
          </button>
        ))}
        <p>
          {a(
            "凭证加密存储，读取时只返回配置状态。",
            "Credentials are encrypted at rest. Only their configured state is returned.",
          )}
        </p>
      </aside>
      <form className="admin-panel admin-settings-form" onSubmit={save}>
        <fieldset className="admin-edit-fieldset" disabled={busy || testing}>
          <div className="admin-panel-title">
            <div>
              <div className="eyebrow">
                CONFIGURATION / {String(section).toUpperCase()}
              </div>
              <h2>
                {a(
                  sections.find((x) => x[0] === section)![1],
                  sections.find((x) => x[0] === section)![2],
                )}
              </h2>
            </div>
            <span className="admin-version">v{baseline.revision}</span>
          </div>
          {section === "site" && (
            <>
              <p className="admin-help">
                {a(
                  "管理平台品牌、注册入口和对外联系信息。",
                  "Manage your platform identity, registration and support contact.",
                )}
              </p>
              {input("site", "brand", a("平台名称", "Platform name"), {
                required: true,
              })}
              {input("site", "support_email", a("支持邮箱", "Support email"), {
                type: "email",
              })}
              <Check
                label={a("允许新用户注册", "Allow new registrations")}
                checked={draft.site.registration_enabled}
                onChange={(v) => update("site", "registration_enabled", v)}
              />
              <Notice>
                {a(
                  "生产注册需要可用的邮件服务。关闭注册不会影响已有用户登录。",
                  "Production registration requires working email delivery. Disabling registration does not prevent existing users from signing in.",
                )}
              </Notice>
              <h3>{t("服务政策")}</h3>
              <p className="admin-help">
                {a(
                  "以下内容作为纯文本公开展示。留空表示待运营公布；填写政策不会自动启用配额或清理任务。",
                  "These policies are published as plain text. Blank fields appear as awaiting publication; entering text does not enable storage quotas or cleanup jobs.",
                )}
              </p>
              {policyFields.map(({ key, title }) => (
                <Field
                  key={key}
                  label={t(title)}
                  error={fieldErrors[`site.${key}`]}
                  hint={a(
                    `纯文本，最多 ${policyLimit} 个字符 · 当前 ${policyCodePointCount(draft.site[key] || "")}`,
                    `Plain text, up to ${policyLimit.toLocaleString("en")} Unicode characters · ${policyCodePointCount(draft.site[key] || "")} entered`,
                  )}
                >
                  <textarea
                    rows={4}
                    value={draft.site[key] || ""}
                    onChange={(e) => update("site", key, e.target.value)}
                    placeholder={t("待运营公布")}
                  />
                </Field>
              ))}
            </>
          )}
          {section === "media" && (
            <>
              <p className="admin-help">
                {a(
                  "仅连接固定 Sub2API 服务。图片和视频独立凭证，轮换后原任务仍使用原凭证核查。",
                  "Connect the fixed Sub2API service. Image and video credentials are separate; existing tasks keep their original credential reference.",
                )}
              </p>
              {input(
                "media",
                "origin",
                a("Sub2API 服务地址", "Sub2API origin"),
                {
                  type: "url",
                  placeholder: "https://api.example.com",
                  hint: a(
                    "填写 HTTPS origin，不包含 /v1 路径。",
                    "Use an HTTPS origin without a /v1 path.",
                  ),
                },
              )}
              <Field
                label={a("结果下载来源白名单", "Allowed output origins")}
                error={fieldErrors["media.output_origins"]}
                hint={a(
                  "每行一个 HTTPS origin。按供应端实际返回域名配置，禁止使用通配符。",
                  "One HTTPS origin per line. Use verified provider output domains; wildcards are not allowed.",
                )}
              >
                <textarea
                  rows={3}
                  value={draft.media.output_origins.join("\n")}
                  onChange={(e) =>
                    update(
                      "media",
                      "output_origins",
                      e.target.value.split(/\n/),
                    )
                  }
                  placeholder="https://files.example.com"
                />
              </Field>
              {secret(
                "media.image_key",
                a("图片服务 Key", "Image service key"),
                draft.media.image_key_configured,
              )}
              {secret(
                "media.video_key",
                a("视频服务 Key", "Video service key"),
                draft.media.video_key_configured,
              )}
            </>
          )}
          {section === "payment" && (
            <>
              <p className="admin-help">
                {a(
                  "配置真实易支付商户。支付回调必须验签并主动查单后才会到账。",
                  "Configure your EasyPay merchant. Credit is granted only after signature verification and an active provider query.",
                )}
              </p>
              {system?.demo && (
                <Notice>
                  {a(
                    "当前是开发演示环境，真实收款仍被服务端禁用。",
                    "This is a demo environment. Real payment collection is disabled by the server.",
                  )}
                </Notice>
              )}
              <Notice>
                {a(
                  "回调域名由部署环境 PUBLIC_URL 管理。开发模式服务仅供本地使用；公网支付和邮件验收须使用独立的 production 预发布环境及 HTTPS 入口，再从该地址登录并保存匹配的通知与返回地址。仅修改 PUBLIC_URL 不会切换运行模式，本机回环地址也无法接收支付方通知。",
                  "Callback origins are controlled by the deployment PUBLIC_URL. Development services are local only. Test public payment callbacks and email in a separate production-mode staging environment over HTTPS, then sign in there and save matching callback and return URLs. Changing PUBLIC_URL alone does not change the runtime mode; loopback addresses cannot receive provider notifications.",
                )}
              </Notice>
              <div>
                <label className="admin-check">
                  <input
                    type="checkbox"
                    checked={draft.payment.enabled}
                    aria-invalid={Boolean(fieldErrors["payment.enabled"])}
                    aria-describedby={
                      fieldErrors["payment.enabled"]
                        ? "payment-enabled-error"
                        : undefined
                    }
                    onChange={(e) =>
                      update("payment", "enabled", e.target.checked)
                    }
                  />
                  <span>{a("启用易支付收款", "Enable EasyPay")}</span>
                </label>
                {fieldErrors["payment.enabled"] && (
                  <span
                    id="payment-enabled-error"
                    className="field-error-message"
                    role="alert"
                  >
                    {fieldErrors["payment.enabled"]}
                  </span>
                )}
              </div>
              {input(
                "payment",
                "origin",
                a("易支付服务地址", "EasyPay origin"),
                {
                  type: "url",
                  placeholder: "https://pay.example.com",
                },
              )}
              {input("payment", "pid", a("商户编号 PID", "Merchant PID"))}
              {secret(
                "payment.key",
                a("商户签名密钥", "Merchant signing key"),
                draft.payment.key_configured,
              )}
              {input(
                "payment",
                "notify_url",
                a("异步通知地址", "Payment callback URL"),
                {
                  type: "url",
                  hint: a(
                    "必须严格等于部署 PUBLIC_URL 后追加 /api/v1/payment/notify/easypay。",
                    "Must exactly match the deployment PUBLIC_URL followed by /api/v1/payment/notify/easypay.",
                  ),
                },
              )}
              {input(
                "payment",
                "return_url",
                a("支付返回地址", "Payment return URL"),
                {
                  type: "url",
                  hint: a(
                    "必须严格等于部署 PUBLIC_URL 后追加 /wallet。返回页只查询订单，不执行前端入账。",
                    "Must exactly match the deployment PUBLIC_URL followed by /wallet. The return page queries the order; it does not grant credits.",
                  ),
                },
              )}
            </>
          )}
          {section === "smtp" && (
            <>
              <p className="admin-help">
                {a(
                  "用于发送账户验证与密码重置邮件。连接检测不发送邮件。",
                  "Used for verification and password reset emails. The connection check does not send a message.",
                )}
              </p>
              <div className="admin-form-grid">
                {input("smtp", "host", a("SMTP 主机", "SMTP host"), {
                  placeholder: "smtp.example.com",
                })}
                {input("smtp", "port", a("端口", "Port"), { type: "number" })}
              </div>
              {input("smtp", "username", a("登录用户名", "SMTP username"))}
              {secret(
                "smtp.password",
                a("SMTP 密码 / 授权码", "SMTP password / app token"),
                draft.smtp.password_configured,
              )}
              {input("smtp", "from", a("发件人邮箱", "From address"), {
                type: "email",
              })}
            </>
          )}
          {section === "storage" && (
            <>
              <p className="admin-help">
                {a(
                  "所有参考素材和生成成品存放在私有存储中，用户通过受控下载访问。",
                  "References and generated media are private. Users access them through authorized downloads.",
                )}
              </p>
              {draft.storage.location_locked && (
                <Notice>
                  {a(
                    "已有素材，存储位置已锁定。迁移需先完成对象复制和完整性校验，不可在此直接切换。",
                    "Existing assets lock the storage location. Migrate and verify all objects before changing the location.",
                  )}
                </Notice>
              )}
              <Field
                label={a("存储驱动", "Storage driver")}
                error={fieldErrors["storage.driver"]}
              >
                <select
                  value={draft.storage.driver}
                  disabled={draft.storage.location_locked}
                  onChange={(e) => update("storage", "driver", e.target.value)}
                >
                  <option value="local" disabled={system?.development !== true}>
                    {a(
                      "本地私有目录（仅开发环境）",
                      "Private local directory (development only)",
                    )}
                  </option>
                  <option value="s3">S3 / S3-compatible</option>
                </select>
              </Field>
              {draft.storage.driver === "local" ? (
                <Notice>
                  {a(
                    "本地私有目录仅用于开发环境。正式环境须使用私有 S3 / R2 存储。",
                    "Private local storage is for development only. Production requires private S3 / R2 storage.",
                  )}
                </Notice>
              ) : (
                <>
                  {input(
                    "storage",
                    "endpoint",
                    a("S3 Endpoint", "S3 endpoint"),
                    {
                      type: "url",
                      disabled: draft.storage.location_locked,
                      placeholder: "https://s3.example.com",
                    },
                  )}
                  <div className="admin-form-grid">
                    {input("storage", "region", a("区域", "Region"), {
                      disabled: draft.storage.location_locked,
                    })}
                    {input(
                      "storage",
                      "bucket",
                      a("私有 Bucket", "Private bucket"),
                      { disabled: draft.storage.location_locked },
                    )}
                  </div>
                  {secret(
                    "storage.access_key",
                    a("Access Key", "Access key"),
                    draft.storage.access_key_configured,
                  )}
                  {secret(
                    "storage.secret_key",
                    a("Secret Key", "Secret key"),
                    draft.storage.secret_key_configured,
                  )}
                </>
              )}
            </>
          )}
          {section !== "site" && (
            <div className="admin-connection-test">
              <div>
                <strong>{a("连接检测", "Connection check")}</strong>
                <p>
                  {dirty
                    ? a(
                        "有未保存的变更，请先保存，再检测已生效配置。",
                        "Save changes before testing the applied configuration.",
                      )
                    : a(
                        "只检测已保存的配置，不创建任务、扣款或发送邮件。",
                        "Tests saved settings without creating a task, charging money or sending email.",
                      )}
                </p>
              </div>
              <button
                type="button"
                className="button secondary"
                disabled={testing || dirty || busy}
                onClick={() => void test()}
              >
                <Icon name="refresh" size={15} />
                {testing
                  ? a("检测中…", "Checking…")
                  : a("检测连接", "Test connection")}
              </button>
            </div>
          )}
          {result && (
            <Notice error={!result.ok}>
              {result.message}
              <small className="cell-sub">
                {formatDate(result.checked_at)}
              </small>
            </Notice>
          )}
          <Reason value={reason} onChange={setReason} />
          <SaveBar
            busy={busy}
            error={error}
            dirty={dirty && reason.trim().length >= 4}
            label={a("保存全部变更", "Save all changes")}
          />
          <div className="admin-settings-meta">
            <span>
              {dirty
                ? a("有未保存变更", "Unsaved changes")
                : a("已与服务器同步", "Synced with server")}{" "}
              · {formatDate(baseline.updated_at)}
            </span>
            <button
              type="button"
              className="text-button"
              onClick={reload}
              disabled={busy}
            >
              {a("重新加载配置", "Reload settings")}
            </button>
          </div>
        </fieldset>
      </form>
    </div>
  );
}
