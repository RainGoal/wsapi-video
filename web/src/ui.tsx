import { getLocale, setLocale, t, useLocale } from "./i18n";
import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { api, errorMessage } from "./api";
const paths: Record<string, ReactNode> = {
  grid: (
    <>
      <rect x="3" y="3" width="7" height="7" rx="1" />
      <rect x="14" y="3" width="7" height="7" rx="1" />
      <rect x="3" y="14" width="7" height="7" rx="1" />
      <rect x="14" y="14" width="7" height="7" rx="1" />
    </>
  ),
  plus: <path d="M12 4v16M4 12h16" />,
  play: <path d="m8 4 12 8-12 8Z" />,
  pause: <path d="M8 4v16M16 4v16" />,
  arrow: <path d="M5 12h14m-6-6 6 6-6 6" />,
  back: <path d="m14 5-7 7 7 7" />,
  close: <path d="m6 6 12 12M6 18 18 6" />,
  canvas: (
    <>
      <path d="M8 3H3v5m13-5h5v5M3 16v5h5m13-5v5h-5" />
      <rect x="7" y="7" width="10" height="10" rx="1" />
    </>
  ),
  shots: (
    <>
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="M3 9h18M8 4v5m8-5v5m-8 5h8m-8 3h5" />
    </>
  ),
  image: (
    <>
      <rect x="3" y="3" width="18" height="18" rx="2" />
      <circle cx="8" cy="8" r="1.5" />
      <path d="m3 17 5-5 4 4 4-6 5 7" />
    </>
  ),
  video: (
    <>
      <rect x="3" y="5" width="14" height="14" rx="2" />
      <path d="m17 10 4-3v10l-4-3" />
    </>
  ),
  tasks: (
    <>
      <path d="M9 5h12M9 12h12M9 19h12m-18-15 1 1 2-2m-3 9 1 1 2-2m-3 9 1 1 2-2" />
    </>
  ),
  wallet: (
    <>
      <path d="M20 8V5H5a2 2 0 0 1 0-4h14v4M5 5H3v16h18V8H5" />
      <path d="M21 12h-6v5h6m-3-2.5h.1" />
    </>
  ),
  upload: (
    <>
      <path d="M12 16V3m-5 5 5-5 5 5M4 16v5h16v-5" />
    </>
  ),
  download: (
    <>
      <path d="M12 3v13m-5-5 5 5 5-5M4 17v4h16v-4" />
    </>
  ),
  search: (
    <>
      <circle cx="10.5" cy="10.5" r="6.5" />
      <path d="m16 16 5 5" />
    </>
  ),
  settings: (
    <>
      <circle cx="12" cy="12" r="3" />
      <path d="m9 3-1 3-3 1-2 3 2 2-1 3 3 2 1 3h4l1-3 3-1 2-3-2-2 1-3-3-2-1-3Z" />
    </>
  ),
  logout: (
    <>
      <path d="M9 4H4v16h5m5-12 4 4-4 4m-5-4h13" />
    </>
  ),
  lock: (
    <>
      <rect x="4" y="10" width="16" height="11" rx="2" />
      <path d="M8 10V6a4 4 0 0 1 8 0v4m-4 4v3" />
    </>
  ),
  check: <path d="m5 12 4 4L19 6" />,
  clock: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 2" />
    </>
  ),
  refresh: (
    <>
      <path d="M20 7v5h-5M4 17v-5h5" />
      <path d="M6 7a7 7 0 0 1 12-1l2 3M4 15l2 3a7 7 0 0 0 12-1" />
    </>
  ),
  trash: (
    <>
      <path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7m4-7v7" />
    </>
  ),
  copy: (
    <>
      <rect x="8" y="8" width="13" height="13" rx="2" />
      <path d="M16 8V3H3v13h5" />
    </>
  ),
  text: <path d="M4 5h16M12 5v16M8 21h8" />,
  cursor: <path d="m5 3 14 10-7 1-3 7Z" />,
  hand: (
    <path d="M8 12V5a2 2 0 0 1 4 0v6-8a2 2 0 0 1 4 0v9-6a2 2 0 0 1 4 0v10c0 7-10 9-13 3L3 13a2 2 0 0 1 3-2l2 2" />
  ),
  undo: <path d="m8 5-5 5 5 5m-5-5h11a6 6 0 0 1 6 6v3" />,
  redo: <path d="m16 5 5 5-5 5m5-5H10a6 6 0 0 0-6 6v3" />,
  group: (
    <>
      <rect x="2" y="2" width="20" height="20" rx="2" strokeDasharray="3 3" />
      <rect x="6" y="6" width="5" height="7" />
      <rect x="13" y="11" width="5" height="7" />
    </>
  ),
  spark: (
    <>
      <path d="m12 3 2.6 6.4L21 12l-6.4 2.6L12 21l-2.6-6.4L3 12l6.4-2.6Z" />
      <path d="m20 2 .6 1.4L22 4l-1.4.6L20 6l-.6-1.4L18 4l1.4-.6Z" />
    </>
  ),
  more: (
    <>
      <circle cx="5" cy="12" r="1" />
      <circle cx="12" cy="12" r="1" />
      <circle cx="19" cy="12" r="1" />
    </>
  ),
  up: <path d="m5 15 7-7 7 7" />,
  down: <path d="m5 9 7 7 7-7" />,
  info: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 11v6m0-11v1" />
    </>
  ),
  link: (
    <>
      <path d="m9 15 6-6m-7 4-2 2a4 4 0 0 0 6 6l3-3m1-7 2-2a4 4 0 0 0-6-6L9 6" />
    </>
  ),
};
export function Icon({
  name,
  size = 18,
  ...props
}: {
  name: string;
  size?: number;
  className?: string;
}) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      {...props}
    >
      {paths[name] || paths.grid}
    </svg>
  );
}
export function Logo({ compact = false }: { compact?: boolean }) {
  return (
    <div className="logo">
      <span className="brand-mark">
        <i />
        <i />
        <i />
        <i />
      </span>
      {!compact && (
        <>
          <strong>FRAME</strong>
          <span className="brand-slash">/</span>
          <span className="brand-caption">STUDIO</span>
        </>
      )}
    </div>
  );
}
export function Spinner() {
  return <span className="spinner" aria-label={t("加载中")} />;
}
export function LocaleSwitcher() {
  const locale = useLocale();
  return (
    <button
      className="locale-switch"
      aria-label={locale === "en" ? "Switch to Chinese" : "Switch to English"}
      onClick={() => setLocale(locale === "en" ? "zh-CN" : "en")}
    >
      {locale === "en" ? "中文" : "EN"}
    </button>
  );
}
export function Empty({
  icon = "grid",
  title,
  description,
  action,
}: {
  icon?: string;
  title: string;
  description?: string;
  action?: ReactNode;
}) {
  return (
    <div className="empty">
      <div className="empty-symbol">
        <Icon name={icon} size={30} />
      </div>
      <h3>{title}</h3>
      {description && <p>{description}</p>}
      {action}
    </div>
  );
}
export function Notice({
  children,
  error = false,
}: {
  children: ReactNode;
  error?: boolean;
}) {
  return (
    <div
      className={`notice ${error ? "notice-error" : ""}`}
      role={error ? "alert" : "status"}
    >
      <Icon name="info" size={16} />
      <span>{children}</span>
    </div>
  );
}
export function Modal({
  title,
  children,
  onClose,
  wide = false,
  closeDisabled = false,
}: {
  title: string;
  children: ReactNode;
  onClose: () => void;
  wide?: boolean;
  closeDisabled?: boolean;
}) {
  const id = useId();
  const closeRef = useRef(onClose);
  closeRef.current = () => {
    if (!closeDisabled) onClose();
  };
  useEffect(() => {
    const fn = (event: KeyboardEvent) => {
      if (event.key === "Escape") closeRef.current();
      if (event.key === "Tab") {
        const focusable = document
          .querySelector(`[data-dialog="${id}"]`)
          ?.querySelectorAll<HTMLElement>(
            'button:not(:disabled),a[href],input:not(:disabled),textarea:not(:disabled),select:not(:disabled),[tabindex="0"]',
          );
        if (!focusable?.length) return;
        const first = focusable[0],
          last = focusable[focusable.length - 1];
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first.focus();
        }
      }
    };
    const active = document.activeElement as HTMLElement | null;
    document.addEventListener("keydown", fn);
    const frame = requestAnimationFrame(() =>
      document
        .querySelector<HTMLDialogElement>(`[data-dialog="${id}"]`)
        ?.querySelector<HTMLElement>("input,button,select")
        ?.focus(),
    );
    return () => {
      cancelAnimationFrame(frame);
      document.removeEventListener("keydown", fn);
      active?.focus();
    };
  }, [id]);
  return (
    <div
      className="modal-backdrop"
      onMouseDown={(event) => {
        if (event.currentTarget === event.target) closeRef.current();
      }}
    >
      <section
        className={`modal ${wide ? "modal-wide" : ""}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby={id}
        data-dialog={id}
      >
        <header>
          <h2 id={id}>{title}</h2>
          <button
            className="icon-button"
            onClick={() => closeRef.current()}
            disabled={closeDisabled}
            aria-label={t("关闭")}
          >
            <Icon name="close" />
          </button>
        </header>
        {children}
      </section>
    </div>
  );
}
export function useResource<T>(
  path: string | null,
  initial: T,
  refreshKey: unknown = 0,
) {
  const [data, setData] = useState<T>(initial),
    [loading, setLoading] = useState(Boolean(path)),
    [error, setError] = useState(""),
    [version, setVersion] = useState(0);
  const [settled, setSettled] = useState<{
    path: string;
    version: number;
    refreshKey: unknown;
  } | null>(null);
  const current =
    settled?.path === path &&
    settled.version === version &&
    Object.is(settled.refreshKey, refreshKey);
  useEffect(() => {
    let live = true;
    if (!path) {
      setLoading(false);
      return;
    }
    setLoading(true);
    setError("");
    api<T>(path)
      .then((value) => {
        if (live) setData(value);
      })
      .catch((reason) => {
        if (live) setError(errorMessage(reason));
      })
      .finally(() => {
        if (live) {
          setSettled({ path, version, refreshKey });
          setLoading(false);
        }
      });
    return () => {
      live = false;
    };
  }, [path, version, refreshKey]);
  return {
    data,
    setData,
    loading: Boolean(path) && (loading || !current),
    error: current ? error : "",
    refresh: () => setVersion((value) => value + 1),
  };
}
export function formatDate(value?: string) {
  return value
    ? new Intl.DateTimeFormat(getLocale(), {
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
      }).format(new Date(value))
    : "—";
}
export function formatBytes(value: number) {
  return value > 1048576
    ? `${(value / 1048576).toFixed(1)} MB`
    : `${Math.ceil(value / 1024)} KB`;
}
const statuses: Record<string, string> = {
  pending: "等待处理",
  submitting: "正在提交",
  sending: "正在提交",
  submit_unknown: "提交待确认",
  submission_unknown: "提交待确认",
  queued: "排队中",
  processing: "生成中",
  generating: "生成中",
  in_progress: "生成中",
  completed: "生成完成",
  succeeded: "生成完成",
  failed: "生成失败",
  canceled: "已取消",
  cancelled: "已取消",
  ready: "可下载",
  archived: "已保存",
  archiving: "正在保存",
  archive_retry: "保存重试",
  retrying: "保存重试",
  delivery_failed: "交付异常",
  error: "交付异常",
  archive_review: "交付待核查",
  disputed: "采购待核查",
  resolved: "已核查",
  matched: "已对账",
  frozen: "已冻结",
  consumed: "已消费",
  released: "已释放",
  refunded: "已退款",
  paid: "已支付",
  credited: "已到账",
  expired: "已过期",
  active: "正常",
  disabled: "已停用",
};
export function statusLabel(value: string) {
  return t(statuses[value] || value || "—");
}
export function Status({ value }: { value: string }) {
  return <span className={`status status-${value}`}>{statusLabel(value)}</span>;
}
export function LoadingError({
  loading,
  error,
  retry,
}: {
  loading: boolean;
  error: string;
  retry?: () => void;
}) {
  return loading ? (
    <div className="loading">
      <Spinner />
      <span>{t("正在加载")}</span>
    </div>
  ) : error ? (
    <div className="load-error">
      <Notice error>{error}</Notice>
      {retry && (
        <button className="button secondary" onClick={retry}>
          {t("重新加载")}
        </button>
      )}
    </div>
  ) : null;
}
