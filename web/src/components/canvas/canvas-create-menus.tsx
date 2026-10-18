/**
 * Adapted from basketikun/infinite-canvas (MIT), commit
 * 3a6b64b602ad062b29fac66902ed18a7ef5fd9dc, canvas-create-menus.tsx.
 * Keeps the upstream create-menu layout and inverse-scale positioning while
 * using the platform's supported node types, icons and translations.
 */
import { useEffect, useRef, type ReactNode } from "react";
import { t } from "../../i18n";
import { Icon } from "../../ui";
import {
  CanvasNodeType,
  type ConnectionHandle,
  type Position,
} from "../../types/canvas";
import "./canvas-create-menus.css";

export type PendingConnectionCreate = {
  connection: ConnectionHandle;
  position: Position;
};
type CreateMenuProps = {
  position: Position;
  scale?: number;
  types: CanvasNodeType[];
  onCreate: (type: CanvasNodeType) => void;
  onClose: () => void;
};

export function ConnectionCreateMenu({
  pending,
  ...props
}: Omit<CreateMenuProps, "position"> & { pending: PendingConnectionCreate }) {
  return (
    <NodeCreateMenu
      {...props}
      position={pending.position}
      title={
        pending.connection.handleType === "source"
          ? t("引用该节点生成")
          : t("添加参考节点")
      }
      connection
    />
  );
}

export function ConnectionCreateOption({
  icon,
  title,
  description,
  onClick,
}: {
  icon: ReactNode;
  title: string;
  description?: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      role="menuitem"
      className="canvas-create-option"
      onClick={onClick}
    >
      <span className="canvas-create-option-icon">{icon}</span>
      <span className="canvas-create-option-copy">
        <strong>{title}</strong>
        {description ? <small>{description}</small> : null}
      </span>
    </button>
  );
}

export function NodeCreateMenu({
  position,
  scale = 1,
  types,
  onCreate,
  onClose,
  title = t("添加节点"),
  connection = false,
}: CreateMenuProps & { title?: string; connection?: boolean }) {
  const menuRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const handlePointerDown = (event: PointerEvent) => {
      if (menuRef.current && !menuRef.current.contains(event.target as Node))
        onClose();
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        onClose();
      } else if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        const options = [
          ...(menuRef.current?.querySelectorAll<HTMLButtonElement>(
            '[role="menuitem"]',
          ) || []),
        ];
        const index = options.indexOf(
          document.activeElement as HTMLButtonElement,
        );
        options[
          (index + (event.key === "ArrowDown" ? 1 : -1) + options.length) %
            options.length
        ]?.focus();
      }
    };
    menuRef.current
      ?.querySelector<HTMLButtonElement>('[role="menuitem"]')
      ?.focus({ preventScroll: true });
    document.addEventListener("pointerdown", handlePointerDown, true);
    menuRef.current?.addEventListener("keydown", handleKeyDown);
    const menu = menuRef.current;
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown, true);
      menu?.removeEventListener("keydown", handleKeyDown);
    };
  }, [onClose]);
  return (
    <div
      ref={menuRef}
      className="canvas-create-menu"
      role="menu"
      aria-label={title}
      data-canvas-no-zoom
      data-connection-create-menu={connection || undefined}
      style={{
        left: position.x,
        top: position.y,
        transform: `scale(${1 / scale})`,
        transformOrigin: "top left",
      }}
      onPointerDown={(event) => event.stopPropagation()}
      onWheel={(event) => event.stopPropagation()}
    >
      <div className="canvas-create-menu-heading">
        <span>{title}</span>
        <button type="button" onClick={onClose} aria-label={t("关闭菜单")}>
          <Icon name="close" size={15} />
        </button>
      </div>
      <div className="canvas-create-options">
        {types.map((type) => (
          <ConnectionCreateOption
            key={type}
            icon={<Icon name={type === "config" ? "image" : type} size={18} />}
            title={
              type === CanvasNodeType.Image
                ? t("图片")
                : type === CanvasNodeType.Video
                  ? t("视频")
                  : type === CanvasNodeType.Group
                    ? t("分组")
                    : type === CanvasNodeType.Config
                      ? t("创作配置")
                      : t("文字笔记")
            }
            description={
              type === CanvasNodeType.Image || type === CanvasNodeType.Video
                ? t("创建后点击节点开始创作")
                : type === CanvasNodeType.Text
                  ? t("记录想法，也可作为提示词参考")
                  : undefined
            }
            onClick={() => onCreate(type)}
          />
        ))}
      </div>
    </div>
  );
}
