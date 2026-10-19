// Adapted from infinite-canvas (MIT), commit 3a6b64b. See THIRD_PARTY_NOTICES.md.
import type { ReactNode } from "react";
import { t } from "../../i18n";
import type { Product } from "../../types";
import { Icon } from "../../ui";
import { CanvasSelect } from "./canvas-select";
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
  action: ReactNode;
  children?: ReactNode;
}) {
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
        if (event.key === "Escape") onClose();
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
        <CanvasSelect
          className="node-composer-model"
          label={t("生成模型")}
          value={productId}
          onChange={onProductChange}
          disabled={disabled || !products.length}
          placeholder={t("暂无可用模型")}
          options={products.map((product) => ({
            value: product.id,
            label: product.name + (product.demo ? t(" · 开发模拟") : ""),
          }))}
        />
        {parameters}
        <div className="node-composer-action">{action}</div>
      </div>
      {children}
    </section>
  );
}
