import { useState } from "react";
import { t } from "../../i18n";

export function CanvasGroupLabel({
  title,
  count,
  onRename,
}: {
  title: string;
  count: number;
  onRename: (title: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(title);
  function finish() {
    if (draft.trim() && draft.trim() !== title) onRename(draft.trim());
    setEditing(false);
  }
  return editing ? (
    <input
      className="group-name-input"
      aria-label={t("组名称")}
      autoFocus
      maxLength={200}
      value={draft}
      onFocus={(event) => event.currentTarget.select()}
      onChange={(event) => setDraft(event.target.value)}
      onPointerDown={(event) => event.stopPropagation()}
      onDoubleClick={(event) => event.stopPropagation()}
      onBlur={finish}
      onKeyDown={(event) => {
        event.stopPropagation();
        if (event.key === "Enter") finish();
        if (event.key === "Escape") setEditing(false);
      }}
    />
  ) : (
    <>
      <span
        className="group-title"
        role="button"
        tabIndex={0}
        title={t("双击修改组名称")}
        onDoubleClick={(event) => {
          event.stopPropagation();
          setDraft(title);
          setEditing(true);
        }}
        onKeyDown={(event) => {
          if (event.key !== "Enter" && event.key !== "F2") return;
          event.preventDefault();
          event.stopPropagation();
          setDraft(title);
          setEditing(true);
        }}
      >
        {title}
      </span>
      <small className="node-group-count">{t("{0} 个节点", count)}</small>
    </>
  );
}
