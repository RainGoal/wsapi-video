// Adapted from infinite-canvas (MIT), commit 3a6b64b. See THIRD_PARTY_NOTICES.md.
import { useRef, useState, type ReactNode } from "react";
import { t } from "../../i18n";
import type { Product } from "../../types";
import { Icon } from "../../ui";
import { CanvasPopover } from "./canvas-popover";
import "../../node-composer.css";

export function CanvasNodePromptPanel({
  title,
  prompt,
  disabled,
  products,
  productId,
  onPromptChange,
  onProductChange,
  onClose,
  references,
  parameters,
  parameterSummary,
  action,
  children,
}: {
  title: string;
  prompt: string;
  disabled: boolean;
  products: Product[];
  productId: string;
  onPromptChange: (value: string) => void;
  onProductChange: (value: string) => void;
  onClose: () => void;
  references: ReactNode;
  parameters: ReactNode;
  parameterSummary: string;
  action: ReactNode;
  children?: ReactNode;
}) {
  const [settingsOpen, setSettingsOpen] = useState(false);
  const settings = useRef<HTMLDivElement>(null);
  return (
    <section
      className="canvas-node-prompt-panel"
      aria-label={title}
      data-canvas-no-zoom
      onPointerDown={(event) => event.stopPropagation()}
      onMouseDown={(event) => event.stopPropagation()}
      onDoubleClick={(event) => event.stopPropagation()}
      onWheel={(event) => event.stopPropagation()}
      onKeyDown={(event) => {
        event.stopPropagation();
        if (event.key === "Escape") {
          if (settingsOpen) setSettingsOpen(false);
          else onClose();
        }
      }}
    >
      <header className="node-composer-heading">
        <span>
          <Icon name="spark" size={14} />
          {title}
        </span>
        <button
          type="button"
          className="node-composer-icon"
          onClick={onClose}
          aria-label={t("收起创作面板")}
        >
          <Icon name="close" size={15} />
        </button>
      </header>
      {references}
      <textarea
        className="node-composer-prompt"
        aria-label={t("描述你想看到的画面")}
        value={prompt}
        onChange={(event) => onPromptChange(event.target.value)}
        disabled={disabled}
        placeholder={t(
          "主体是什么？画面、光线、镜头、风格，以及你想保留的细节…",
        )}
        maxLength={12000}
        rows={5}
      />
      <div className="node-composer-controls">
        <select
          aria-label={t("生成模型")}
          value={productId}
          onChange={(event) => onProductChange(event.target.value)}
          disabled={disabled || !products.length}
        >
          {!products.length && <option value="">{t("暂无可用模型")}</option>}
          {products.map((product) => (
            <option key={product.id} value={product.id}>
              {product.name}
              {product.demo ? t(" · 开发模拟") : ""}
            </option>
          ))}
        </select>
        <div className="node-composer-settings" ref={settings}>
          <button
            type="button"
            className="node-composer-parameters"
            aria-label={t("生成参数")}
            aria-expanded={settingsOpen}
            onClick={() => setSettingsOpen((open) => !open)}
            disabled={disabled || !products.length}
          >
            <Icon name="settings" size={14} />
            <span>{parameterSummary || t("参数")}</span>
            <Icon name="down" size={11} />
          </button>
          {settingsOpen && (
            <CanvasPopover
              anchor={settings}
              width={285}
              className="node-composer-settings-popover"
              label={t("生成参数")}
              onClose={() => setSettingsOpen(false)}
            >
              {parameters}
            </CanvasPopover>
          )}
        </div>
        {action}
      </div>
      {children}
    </section>
  );
}
