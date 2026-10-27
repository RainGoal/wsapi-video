import { useEffect, useRef, useState, type FormEvent } from "react";
import { api, errorMessage, isCurrentSession, sessionVersion } from "./api";
import { a } from "./admin/common";
import { Icon, LocaleSwitcher, Logo, Notice, Spinner } from "./ui";

export function ForcePasswordChange({
  email,
  endSession,
  logout,
}: {
  email: string;
  endSession: (version: number, message: string) => void;
  logout: () => Promise<void>;
}) {
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const busyRef = useRef(false);
  const live = useRef(true);
  useEffect(() => {
    live.current = true;
    return () => {
      live.current = false;
    };
  }, []);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busyRef.current || !live.current) return;
    const form = event.currentTarget;
    const values = new FormData(form);
    const password = String(values.get("password") || "");
    const currentPassword = String(values.get("current_password") || "");
    if (password !== values.get("confirm_password")) {
      setError(
        a("两次输入的新密码不一致。", "The new passwords do not match."),
      );
      return;
    }
    if (
      Array.from(password).length < 10 ||
      new TextEncoder().encode(password).length > 72
    ) {
      setError(
        a(
          "密码需至少 10 个字符，且不超过 72 字节。",
          "Use at least 10 characters and no more than 72 bytes.",
        ),
      );
      return;
    }
    if (password === currentPassword) {
      setError(
        a(
          "新密码不能与初始密码相同。",
          "Choose a password different from your initial password.",
        ),
      );
      return;
    }
    busyRef.current = true;
    const version = sessionVersion();
    const current = () => live.current && isCurrentSession(version);
    setBusy(true);
    setError("");
    try {
      await api("/auth/change-password", "POST", {
        current_password: currentPassword,
        password,
      });
      if (!isCurrentSession(version)) return;
      if (live.current) form.reset();
      endSession(version, "密码已修改，请重新登录。");
    } catch (reason) {
      if (current()) setError(errorMessage(reason));
    } finally {
      busyRef.current = false;
      if (current()) setBusy(false);
    }
  }

  async function signOut() {
    if (busyRef.current || !live.current) return;
    busyRef.current = true;
    const version = sessionVersion();
    setBusy(true);
    setError("");
    try {
      await logout();
    } catch (reason) {
      if (live.current && isCurrentSession(version))
        setError(errorMessage(reason));
    } finally {
      busyRef.current = false;
      if (live.current && isCurrentSession(version)) setBusy(false);
    }
  }

  return (
    <main className="auth-panel">
      <div className="auth-locale">
        <LocaleSwitcher />
      </div>
      <section className="auth-box" aria-labelledby="initial-password-title">
        <Logo />
        <h2 id="initial-password-title">
          {a("先设置你的专属密码。", "Choose your own password.")}
        </h2>
        <p className="muted">{email}</p>
        <Notice>
          {a(
            "此账号由管理员创建。请先更换初始密码，再进入工作室。",
            "Your administrator created this account. Replace the initial password before entering your studio.",
          )}
        </Notice>
        {error && <Notice error>{error}</Notice>}
        <form onSubmit={submit}>
          <fieldset
            disabled={busy}
            style={{ border: 0, margin: 0, padding: 0 }}
          >
            <label>
              {a("初始密码", "Initial password")}
              <input
                name="current_password"
                type="password"
                autoComplete="current-password"
                required
              />
            </label>
            <label>
              {a("新密码", "New password")}
              <input
                name="password"
                type="password"
                autoComplete="new-password"
                minLength={10}
                maxLength={72}
                required
              />
            </label>
            <label>
              {a("确认新密码", "Confirm new password")}
              <input
                name="confirm_password"
                type="password"
                autoComplete="new-password"
                minLength={10}
                maxLength={72}
                required
              />
            </label>
            <p className="muted">
              {a(
                "至少 10 个字符，最多 72 字节。更新后需重新登录。",
                "Use at least 10 characters, up to 72 bytes. Sign in again after updating.",
              )}
            </p>
            <button className="button primary auth-submit" disabled={busy}>
              {busy ? (
                <Spinner />
              ) : (
                <>
                  {a("保存新密码", "Save new password")}
                  <Icon name="arrow" />
                </>
              )}
            </button>
          </fieldset>
        </form>
        <div className="auth-switch">
          <button
            className="text-button"
            onClick={() => void signOut()}
            disabled={busy}
          >
            {a("退出当前账户", "Sign out")}
          </button>
        </div>
      </section>
    </main>
  );
}
