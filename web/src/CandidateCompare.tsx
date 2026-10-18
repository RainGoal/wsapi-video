import { useState } from "react";
import { t } from "./i18n";
import { AssetVisual } from "./CanvasEditor";
import type { Asset } from "./types";
import { Icon, Modal } from "./ui";
import "./creation.css";

export function CandidateCompare({
  assets,
  selectedId,
  onSelect,
  onClose,
}: {
  assets: Asset[];
  selectedId?: string;
  onSelect?: (asset: Asset) => void;
  onClose: () => void;
}) {
  const [leftId, setLeftId] = useState(selectedId || assets[0]?.id || "");
  const [rightId, setRightId] = useState(
    assets.find((asset) => asset.id !== (selectedId || assets[0]?.id))?.id ||
      "",
  );
  return (
    <Modal title={t("比较候选")} onClose={onClose} wide>
      <div className="candidate-compare">
        {(["left", "right"] as const).map((side) => {
          const id = side === "left" ? leftId : rightId;
          const otherId = side === "left" ? rightId : leftId;
          const asset = assets.find((item) => item.id === id);
          return (
            <section key={side}>
              <label>
                {side === "left" ? t("左侧版本") : t("右侧版本")}
                <select
                  value={id}
                  onChange={(event) =>
                    side === "left"
                      ? setLeftId(event.target.value)
                      : setRightId(event.target.value)
                  }
                >
                  {assets.map((item) => (
                    <option
                      key={item.id}
                      value={item.id}
                      disabled={item.id === otherId}
                    >
                      {item.file_name}
                    </option>
                  ))}
                </select>
              </label>
              {asset && (
                <>
                  <div className="candidate-compare-media">
                    <AssetVisual asset={asset} controls />
                  </div>
                  <p className="field-hint">
                    {asset.width} × {asset.height}
                    {asset.duration_seconds > 0
                      ? ` · ${asset.duration_seconds}s`
                      : ""}
                  </p>
                  {onSelect && (
                    <button
                      className={`button ${asset.id === selectedId ? "secondary" : "primary"} full`}
                      disabled={asset.id === selectedId}
                      onClick={() => onSelect(asset)}
                    >
                      <Icon
                        name={asset.id === selectedId ? "check" : "image"}
                        size={16}
                      />
                      {asset.id === selectedId
                        ? t("已选成品")
                        : t("设为选定成品")}
                    </button>
                  )}
                </>
              )}
            </section>
          );
        })}
      </div>
    </Modal>
  );
}
