/** Adapted from canvas-node.tsx in basketikun/infinite-canvas (MIT),
 * commit 3a6b64b602ad062b29fac66902ed18a7ef5fd9dc.
 * Retains the upstream centered empty-media content; platform task results
 * and private asset delivery are rendered by CanvasEditor.
 */
import { t } from "../../i18n";
import type { CanvasNodeData } from "../../types/canvas";
import { Icon } from "../../ui";

export function CanvasNodePlaceholder({ node }: { node: CanvasNodeData }) {
  const video =
    node.type === "video" || node.metadata?.generationKind === "video";
  return (
    <div className="canvas-node-placeholder">
      <span className="canvas-node-placeholder-icon">
        <Icon name={video ? "video" : "image"} size={28} />
      </span>
      <span>{video ? t("空视频节点") : t("空图片节点")}</span>
      <small>{t("点击节点，输入提示词或添加参考素材")}</small>
    </div>
  );
}
