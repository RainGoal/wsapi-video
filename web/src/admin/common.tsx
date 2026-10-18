import { getLocale } from "../i18n";
import {
  cloneElement,
  isValidElement,
  type ReactElement,
  type ReactNode,
  useId,
  useState,
} from "react";
import { Icon, Notice } from "../ui";

export const a = (zh: string, en: string) => (getLocale() === "en" ? en : zh);
export type PageResult<T> = {
  items: T[];
  total?: number;
  limit?: number;
  offset?: number;
};
export const money = (fen: number) => `¥ ${(fen / 100).toFixed(2)}`;
export { decimalFen } from "./validation";
export function Field({
  label,
  hint,
  error,
  children,
}: {
  label: string;
  hint?: string;
  error?: string;
  children: ReactNode;
}) {
  const id = useId();
  const control = isValidElement(children)
    ? cloneElement(
        children as ReactElement<{
          "aria-labelledby"?: string;
          "aria-describedby"?: string;
          "aria-invalid"?: boolean;
        }>,
        {
          "aria-labelledby": id,
          "aria-describedby":
            [hint ? `${id}-hint` : "", error ? `${id}-error` : ""]
              .filter(Boolean)
              .join(" ") || undefined,
          "aria-invalid": Boolean(error),
        },
      )
    : children;
  return (
    <label className={`admin-field ${error ? "field-error" : ""}`}>
      <span id={id}>{label}</span>
      {control}
      {hint && <small id={`${id}-hint`}>{hint}</small>}
      {error && (
        <span id={`${id}-error`} className="field-error-message" role="alert">
          {error}
        </span>
      )}
    </label>
  );
}
export function Check({
  label,
  checked,
  onChange,
  disabled = false,
}: {
  label: string;
  checked: boolean;
  onChange: (v: boolean) => void;
  disabled?: boolean;
}) {
  return (
    <label className="admin-check">
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
      />
      <span>{label}</span>
    </label>
  );
}
export function Pager({
  total,
  count,
  page,
  onPage,
  size = 25,
}: {
  total?: number;
  count: number;
  page: number;
  onPage: (p: number) => void;
  size?: number;
}) {
  const known = total !== undefined;
  return (
    <div className="admin-pagination">
      <span>
        {known
          ? a(`共 ${total} 条记录`, `${total} records`)
          : a(`本页 ${count} 条记录`, `${count} records on this page`)}
      </span>
      <div>
        <button
          className="button secondary small"
          disabled={page === 0}
          onClick={() => onPage(page - 1)}
        >
          <Icon name="back" size={14} />
          {a("上一页", "Previous")}
        </button>
        <span>
          {page + 1}
          {known ? ` / ${Math.max(1, Math.ceil(total / size))}` : ""}
        </span>
        <button
          className="button secondary small"
          disabled={known ? (page + 1) * size >= total : count < size}
          onClick={() => onPage(page + 1)}
        >
          {a("下一页", "Next")}
          <Icon name="arrow" size={14} />
        </button>
      </div>
    </div>
  );
}
export function Details({ values }: { values: [string, ReactNode][] }) {
  return (
    <dl className="admin-details">
      {values.map(([label, value]) => (
        <div key={label}>
          <dt>{label}</dt>
          <dd>{value ?? "—"}</dd>
        </div>
      ))}
    </dl>
  );
}
export function CopyID({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      className="admin-copy mono"
      title={a("复制编号", "Copy ID")}
      onClick={() =>
        void navigator.clipboard.writeText(value).then(() => {
          setCopied(true);
          setTimeout(() => setCopied(false), 1500);
        })
      }
    >
      {value}
      <Icon name={copied ? "check" : "copy"} size={12} />
    </button>
  );
}
export function Reason({
  value,
  onChange,
}: {
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <Field
      label={a("操作原因与依据", "Reason and evidence")}
      hint={a(
        "必填，将与管理员身份、操作时间一并写入审计。",
        "Required. Saved with the administrator and operation time in the audit log.",
      )}
    >
      <textarea
        required
        minLength={4}
        maxLength={1000}
        rows={3}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={a(
          "说明具体变更、处理原因或核查依据",
          "Describe the change, reason or verification evidence",
        )}
      />
    </Field>
  );
}
export function SaveBar({
  busy,
  error,
  dirty = true,
  onCancel,
  label,
}: {
  busy: boolean;
  error?: string;
  dirty?: boolean;
  onCancel?: () => void;
  label?: string;
}) {
  return (
    <>
      {error && <Notice error>{error}</Notice>}
      <div className="admin-save-bar">
        <span>{a("所有变更均保留审计记录", "Every change is audited")}</span>
        <div>
          {onCancel && (
            <button
              type="button"
              className="button secondary"
              disabled={busy}
              onClick={onCancel}
            >
              {a("取消", "Cancel")}
            </button>
          )}
          <button className="button primary" disabled={busy || !dirty}>
            {busy
              ? a("正在保存…", "Saving…")
              : label || a("保存变更", "Save changes")}
          </button>
        </div>
      </div>
    </>
  );
}
