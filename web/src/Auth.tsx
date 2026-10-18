import { t } from "./i18n";
import {
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import {
  api,
  errorMessage,
  isCurrentSession,
  sessionVersion,
  waitForSessionChanges,
} from "./api";
import type { Identity, System } from "./types";
import { Icon, LocaleSwitcher, Logo, Notice, Spinner } from "./ui";
type AuthMode =
  | "login"
  | "register"
  | "forgot-password"
  | "resend-verification"
  | "reset-password"
  | "verify";
export function Auth({
  system,
  onLogin,
  onPolicies,
  sessionNotice,
}: {
  system: System | null;
  onLogin: (identity: Identity) => void;
  onPolicies: () => void;
  sessionNotice?: ReactNode;
}) {
  const params = new URLSearchParams(window.location.search);
  const initial: AuthMode =
    params.has("reset_token") ||
    window.location.pathname.includes("reset-password")
      ? "reset-password"
      : params.has("verification_token") ||
          window.location.pathname.includes("verify")
        ? "verify"
        : "login";
  const [mode, setMode] = useState<AuthMode>(initial),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [message, setMessage] = useState(""),
    [devURL, setDevURL] = useState("");
  const busyRef = useRef(false);
  const live = useRef(true);
  useEffect(() => {
    live.current = true;
    return () => {
      live.current = false;
    };
  }, []);
  const titles: Record<AuthMode, string> = {
    login: t("回到你的创作现场。"),
    register: t("把想法，变成作品。"),
    "forgot-password": t("找回你的工作室。"),
    "resend-verification": t("重新发送验证邮件。"),
    "reset-password": t("设置新的登录密码。"),
    verify: t("验证你的邮箱。"),
  };
  const change = (value: AuthMode) => {
    if (busyRef.current) return;
    setMode(value);
    setError("");
    setMessage("");
    setDevURL("");
  };
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busyRef.current || !live.current) return;
    busyRef.current = true;
    const version = sessionVersion();
    const current = () => live.current && isCurrentSession(version);
    setBusy(true);
    setError("");
    setMessage("");
    setDevURL("");
    const form = new FormData(event.currentTarget);
    try {
      if (mode === "login") {
        // Let earlier responses finish applying their session cookies first.
        await waitForSessionChanges();
        if (!current()) return;
        const result = await api<Identity>("/auth/login", "POST", {
          email: form.get("email"),
          password: form.get("password"),
        });
        if (!current()) return;
        onLogin(result);
      } else {
        const token =
          params.get("token") ||
          params.get("reset_token") ||
          params.get("verification_token") ||
          form.get("token");
        const body =
          mode === "verify"
            ? { token }
            : mode === "reset-password"
              ? { token, password: form.get("password") }
              : mode === "forgot-password" || mode === "resend-verification"
                ? { email: form.get("email") }
                : {
                    email: form.get("email"),
                    password: form.get("password"),
                    display_name: form.get("display_name"),
                  };
        const result = await api<{
          message: string;
          dev_verification_url?: string;
          dev_reset_url?: string;
        }>(`/auth/${mode}`, "POST", body);
        if (!current()) return;
        setMessage(
          result.message ||
            (mode === "register"
              ? t("请查收验证邮件，验证后即可登录。")
              : t("操作成功。")),
        );
        setDevURL(result.dev_verification_url || result.dev_reset_url || "");
        if (mode === "verify" || mode === "reset-password") {
          window.history.replaceState({}, "", "/");
          setMode("login");
        }
      }
    } catch (reason) {
      if (current()) setError(errorMessage(reason));
    } finally {
      busyRef.current = false;
      if (live.current) setBusy(false);
    }
  }
  return (
    <main className="auth-screen">
      <section className="auth-story">
        <Logo />
        <div className="story-copy">
          <div className="eyebrow">A SPACE FOR YOUR NEXT FRAME</div>
          <h1>
            {t("每个好故事，")}
            <br />
            {t("始于")}
            <span>{t("一帧想象。")}</span>
          </h1>
          <p>
            {t("从一张图片，到一段视频。")}
            <br />
            {t("让灵感、素材和每一次探索，留在同一张创作桌上。")}
          </p>
        </div>
        <div className="story-art" aria-hidden="true">
          <div className="art-axis" />
          <div className="art-frame frame-a">
            <span>01 / CONCEPT</span>
            <div className="art-orbit" />
          </div>
          <div className="art-frame frame-b">
            <span>02 / DIRECTION</span>
            <div className="art-lines" />
          </div>
          <div className="art-frame frame-c">
            <span>03 / MOTION</span>
            <div className="art-play">▷</div>
          </div>
          <div className="art-caption">AN IDEA, IN THE MAKING.</div>
        </div>
        <footer>
          <span>YOUR CREATIVE SPACE, CONNECTED.</span>
          <span>{t("图片 · 视频 · 无限画布")}</span>
        </footer>
      </section>
      <section className="auth-panel">
        <div className="auth-locale">
          <LocaleSwitcher />
        </div>
        <div className="auth-mobile-brand">
          <Logo />
        </div>
        <div className="auth-box">
          <span className="eyebrow">WELCOME TO THE STUDIO</span>
          <h2>{titles[mode]}</h2>
          <p className="muted">
            {mode === "login"
              ? t("登录账户，继续探索你的下一种可能。")
              : mode === "register"
                ? t("创建独立账户，建立自己的图片与视频项目。")
                : t("你的项目和作品会安全保留。")}
          </p>
          {system?.demo && (
            <Notice>
              {t("开发演示环境 · 测试积分与模拟生成不代表真实服务")}
            </Notice>
          )}
          {sessionNotice}
          {error && <Notice error>{error}</Notice>}
          {message && <Notice>{message}</Notice>}
          {devURL && (
            <Notice>
              {t("开发邮件链接：")}
              <a href={devURL}>{t("打开验证 / 重置页面")}</a>
            </Notice>
          )}
          <form onSubmit={submit}>
            {mode === "register" && (
              <label>
                {t("你的名字")}
                <input
                  name="display_name"
                  autoComplete="name"
                  placeholder={t("工作室或创作者名称")}
                  required
                  maxLength={80}
                />
              </label>
            )}
            {(mode === "login" ||
              mode === "register" ||
              mode === "forgot-password" ||
              mode === "resend-verification") && (
              <label>
                {t("邮箱地址")}
                <input
                  type="email"
                  name="email"
                  autoComplete="email"
                  placeholder="you@studio.com"
                  required
                />
              </label>
            )}
            {(mode === "login" ||
              mode === "register" ||
              mode === "reset-password") && (
              <label>
                {t("登录密码")}
                <input
                  type="password"
                  name="password"
                  autoComplete={
                    mode === "login" ? "current-password" : "new-password"
                  }
                  placeholder={
                    mode === "login" ? t("输入密码") : t("至少 10 个字符")
                  }
                  minLength={mode === "login" ? undefined : 10}
                  required
                />
              </label>
            )}
            {(mode === "verify" || mode === "reset-password") &&
              !params.get("token") &&
              !params.get("reset_token") &&
              !params.get("verification_token") && (
                <label>
                  {t("邮件中的验证令牌")}
                  <input name="token" required autoComplete="off" />
                </label>
              )}
            {mode === "login" && (
              <div className="form-meta">
                <span>
                  <Icon name="lock" size={13} />
                  {t("独立账户 · 安全登录")}
                </span>
                <button
                  type="button"
                  className="text-button"
                  onClick={() => change("forgot-password")}
                  disabled={busy}
                >
                  {t("忘记密码？")}
                </button>
              </div>
            )}
            <button className="button primary auth-submit" disabled={busy}>
              {busy ? (
                <Spinner />
              ) : (
                <>
                  {mode === "login"
                    ? t("进入工作室")
                    : mode === "register"
                      ? t("创建账户")
                      : mode === "verify"
                        ? t("验证邮箱")
                        : mode === "reset-password"
                          ? t("保存新密码")
                          : mode === "resend-verification"
                            ? t("发送验证邮件")
                            : t("发送重置邮件")}
                  <Icon name="arrow" />
                </>
              )}
            </button>
          </form>
          <div className="auth-switch">
            {mode === "login" ? (
              <>
                {t("还没有账户？")}{" "}
                <button
                  className="text-button"
                  onClick={() => change("register")}
                  disabled={busy || system?.registration_enabled === false}
                >
                  {t("创建创作账户")}
                </button>
              </>
            ) : (
              <>
                {t("已有账户？")}{" "}
                <button
                  className="text-button"
                  onClick={() => change("login")}
                  disabled={busy}
                >
                  {t("返回登录")}
                </button>
              </>
            )}
          </div>
          {mode === "login" && (
            <button
              className="text-button verify-link"
              onClick={() => change("resend-verification")}
              disabled={busy}
            >
              {t("没有收到验证邮件？")}
            </button>
          )}
          {mode === "login" && (
            <button
              className="text-button verify-link"
              onClick={() => change("verify")}
              disabled={busy}
            >
              {t("输入邮箱验证令牌")}
            </button>
          )}
        </div>
        <button className="text-button auth-policies-link" onClick={onPolicies}>
          {t("服务政策与支持")}
        </button>
        <div className="auth-bottom">
          <span className="live-dot" />
          {t("专注创作的每一步")} <span>FRAME / STUDIO</span>
        </div>
      </section>
    </main>
  );
}
