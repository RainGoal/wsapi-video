/** Adapted from basketikun/infinite-canvas (MIT), commit
 * 3a6b64b602ad062b29fac66902ed18a7ef5fd9dc, canvas-node-hover-toolbar.tsx.
 * Keeps the upstream screen-positioned node toolbar and action structure.
 * Only actions backed by the platform's canvas, task and asset APIs remain.
 */
import { t } from "../../i18n";
import type { Asset } from "../../types";
import type { CanvasNodeData, ViewportTransform } from "../../types/canvas";
import { Icon } from "../../ui";

export function CanvasNodeHoverToolbar({
  node,
  viewport,
  bounds,
  asset,
  onEdit,
  onPreview,
  onDuplicate,
  onDelete,
  onShowTask,
  onUngroup,
}: {
  node: CanvasNodeData;
  viewport: ViewportTransform;
  bounds: { width: number; height: number };
  asset?: Asset;
  onEdit: () => void;
  onPreview: () => void;
  onDuplicate: () => void;
  onDelete: () => void;
  onShowTask?: () => void;
  onUngroup?: () => void;
}) {
  const left = Math.max(
    120,
    Math.min(
      bounds.width - 120,
      viewport.x + (node.position.x + node.width / 2) * viewport.k,
    ),
  );
  const nodeTop = viewport.y + node.position.y * viewport.k;
  const desiredTop =
    nodeTop >= 104 ? nodeTop - 10 : nodeTop + node.height * viewport.k + 52;
  const top = Math.max(50, Math.min(bounds.height - 80, desiredTop));
  return (
    <div
      className="canvas-node-hover-toolbar"
      data-node-toolbar={node.id}
      data-canvas-no-zoom
      style={{ left, top }}
      onPointerDown={(event) => event.stopPropagation()}
    >
      {["image", "video", "config"].includes(node.type) && (
        <ToolbarAction title={t("编辑创作")} icon="text" onClick={onEdit} />
      )}
      {asset && (
        <ToolbarAction title={t("预览素材")} icon="image" onClick={onPreview} />
      )}
      {asset && (
        <a
          title={t("下载原文件")}
          aria-label={t("下载原文件")}
          href={`/api/v1/assets/${asset.id}/download?disposition=attachment`}
          download
        >
          <Icon name="download" size={16} />
        </a>
      )}
      {onShowTask && (
        <ToolbarAction
          title={t("查看任务")}
          icon="tasks"
          onClick={onShowTask}
        />
      )}
      {onUngroup && (
        <ToolbarAction title={t("解除分组")} icon="group" onClick={onUngroup} />
      )}
      <ToolbarAction title={t("复制节点")} icon="copy" onClick={onDuplicate} />
      <ToolbarAction
        title={t("删除选中节点")}
        icon="trash"
        onClick={onDelete}
        danger
      />
    </div>
  );
}
function ToolbarAction({
  title,
  icon,
  onClick,
  danger = false,
}: {
  title: string;
  icon: string;
  onClick: () => void;
  danger?: boolean;
}) {
  return (
    <button
      type="button"
      title={title}
      aria-label={title}
      className={danger ? "danger-text" : undefined}
      onClick={onClick}
    >
      <Icon name={icon} size={16} />
    </button>
  );
}
