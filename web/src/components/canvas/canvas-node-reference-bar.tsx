// Adapted from infinite-canvas (MIT), commit 3a6b64b. See THIRD_PARTY_NOTICES.md.
import { useEffect, useRef, useState } from "react";
import { t } from "../../i18n";
import type { Asset, Capabilities } from "../../types";
import { Icon, Spinner } from "../../ui";
import { CanvasPopover } from "./canvas-popover";
import { CanvasSelect } from "./canvas-select";

export type ReferenceRole = "reference" | "start" | "end";

function ReferenceThumbnail({ asset }: { asset?: Asset }) {
  const [attempt, setAttempt] = useState(0);
  useEffect(() => setAttempt(0), [asset?.id, asset?.preview_url, asset?.url]);
  if (!asset || attempt > 1)
    return <Icon name={asset?.kind || "image"} size={20} />;
  const fallback = `/api/v1/assets/${encodeURIComponent(asset.id)}/content?disposition=inline`;
  const src = attempt ? fallback : asset.preview_url || asset.url || fallback;
  const onError = () => setAttempt((current) => current + 1);
  if (asset.kind === "image")
    return (
      <img
        src={src}
        alt=""
        loading="lazy"
        draggable={false}
        onError={onError}
      />
    );
  if (asset.kind === "video")
    return (
      <video src={src} muted playsInline preload="metadata" onError={onError} />
    );
  return <Icon name="audio" size={20} />;
}

export function CanvasNodeReferenceBar({
  assets,
  referenceIds,
  startFrameId,
  endFrameId,
  caps,
  referenceMode = "reference",
  disabled,
  uploading,
  onRemove,
  onSelect,
  onPickReferences,
  onUploadReferences,
}: {
  assets: Asset[];
  referenceIds: string[];
  startFrameId: string;
  endFrameId: string;
  caps?: Capabilities;
  referenceMode?: "reference" | "frames" | "mixed";
  disabled: boolean;
  uploading: boolean;
  onRemove: (id: string) => void;
  onSelect: (id: string, role: ReferenceRole) => void;
  onPickReferences?: (role: ReferenceRole) => void;
  onUploadReferences?: (files: FileList, role: ReferenceRole) => void;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [role, setRole] = useState<ReferenceRole>("reference");
  const root = useRef<HTMLDivElement>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const selected = [
    ...new Set([...referenceIds, startFrameId, endFrameId].filter(Boolean)),
  ];
  const supported = (asset: Asset) =>
    role !== "reference"
      ? asset.kind === "image" && caps?.supports_frames
      : asset.kind === "image"
        ? (caps?.max_images || 0) > 0
        : asset.kind === "video"
          ? (caps?.max_videos || 0) > 0
          : (caps?.max_audios || 0) > 0;
  const library = assets.filter(
    (asset) =>
      supported(asset) &&
      asset.file_name.toLocaleLowerCase().includes(search.toLocaleLowerCase()),
  );
  useEffect(() => {
    if (!caps?.supports_frames) setRole("reference");
  }, [caps?.supports_frames]);
  useEffect(() => {
    setRole(referenceMode === "frames" ? "start" : "reference");
  }, [referenceMode]);
  return (
    <div className="canvas-node-reference-bar" ref={root}>
      <div className="node-reference-heading">
        <span>{t("参考素材")}</span>
        <span>
          {selected.length ? t("已选择 {0} 项", selected.length) : t("可选")}
        </span>
      </div>
      <div className="node-reference-strip">
        {caps?.supports_frames &&
          referenceMode !== "reference" &&
          (["start", "end"] as const).map((frame) =>
            (frame === "start" ? startFrameId : endFrameId) ? null : (
              <button
                type="button"
                className="node-reference-frame-slot"
                key={frame}
                disabled={disabled || uploading}
                onClick={() => {
                  setRole(frame);
                  setOpen(true);
                }}
              >
                <Icon name="plus" size={16} />
                <span>{frame === "start" ? t("添加首帧") : t("添加尾帧")}</span>
              </button>
            ),
          )}
        {selected.map((id) => {
          const asset = assets.find((candidate) => candidate.id === id);
          const selectedRole =
            startFrameId === id
              ? "start"
              : endFrameId === id
                ? "end"
                : "reference";
          return (
            <div
              key={id}
              className="node-reference-item"
              title={asset?.file_name || t("不可用素材")}
            >
              <div className="node-reference-thumbnail">
                <ReferenceThumbnail asset={asset} />
              </div>
              <button
                type="button"
                className="node-reference-remove"
                disabled={disabled}
                aria-label={t("移除参考素材 {0}", asset?.file_name || id)}
                onClick={() => onRemove(id)}
              >
                <Icon name="close" size={11} />
              </button>
              {caps?.supports_frames && asset?.kind === "image" ? (
                <CanvasSelect
                  className="node-reference-role-select"
                  label={t("参考角色 {0}", asset.file_name)}
                  value={selectedRole}
                  onChange={(value) => onSelect(id, value as ReferenceRole)}
                  disabled={disabled}
                  options={[
                    { value: "reference", label: t("参考") },
                    { value: "start", label: t("首帧") },
                    { value: "end", label: t("尾帧") },
                  ]}
                />
              ) : (
                <span className="node-reference-role">{t("参考")}</span>
              )}
            </div>
          );
        })}
        <button
          type="button"
          className="node-reference-add"
          aria-label={t("添加参考素材")}
          title={t("添加参考素材")}
          aria-expanded={open}
          disabled={disabled || uploading}
          onClick={() => {
            if (!open && referenceMode === "frames")
              setRole(
                !startFrameId ? "start" : !endFrameId ? "end" : "reference",
              );
            setOpen((value) => !value);
          }}
        >
          {uploading ? <Spinner /> : <Icon name="plus" size={19} />}
        </button>
        {!selected.length && (
          <span className="node-reference-empty">
            {t("图片、视频与创作灵感")}
          </span>
        )}
      </div>
      {open && (
        <CanvasPopover
          anchor={root}
          width={394}
          className="node-reference-popover"
          label={t("参考素材")}
          onClose={() => setOpen(false)}
        >
          <div className="node-reference-sources">
            {onPickReferences && (
              <button
                type="button"
                className="button subtle small"
                disabled={disabled}
                onClick={() => {
                  setOpen(false);
                  onPickReferences(role);
                }}
              >
                <Icon name="cursor" size={14} />
                {t("从画布选择")}
              </button>
            )}
            {onUploadReferences && (
              <button
                type="button"
                className="button subtle small"
                disabled={disabled || uploading}
                onClick={() => fileInput.current?.click()}
              >
                <Icon name="upload" size={14} />
                {t("上传素材")}
              </button>
            )}
          </div>
          {caps?.supports_frames && (
            <div
              className="node-reference-role-picker"
              role="group"
              aria-label={t("参考角色")}
            >
              {(["reference", "start", "end"] as const).map((value) => (
                <button
                  type="button"
                  key={value}
                  className={role === value ? "active" : ""}
                  onClick={() => setRole(value)}
                >
                  {value === "start"
                    ? t("首帧")
                    : value === "end"
                      ? t("尾帧")
                      : t("普通参考")}
                </button>
              ))}
            </div>
          )}
          <input
            aria-label={t("搜索项目素材")}
            placeholder={t("搜索项目素材")}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
          <div className="node-reference-library">
            {library.map((asset) => (
              <button
                type="button"
                key={asset.id}
                className={selected.includes(asset.id) ? "selected" : ""}
                title={asset.file_name}
                disabled={disabled}
                onClick={() => {
                  if (role === "reference" && referenceIds.includes(asset.id))
                    onRemove(asset.id);
                  else onSelect(asset.id, role);
                  if (role !== "reference") setOpen(false);
                }}
              >
                <span className="node-reference-library-thumb">
                  <ReferenceThumbnail asset={asset} />
                  {selected.includes(asset.id) && (
                    <span className="node-reference-check">
                      <Icon name="check" size={12} />
                    </span>
                  )}
                </span>
                <span>{asset.file_name}</span>
              </button>
            ))}
          </div>
          {!library.length && (
            <p className="field-hint">
              {assets.length
                ? t("没有符合当前类型的素材")
                : t("上传参考图后，可在这里选用")}
            </p>
          )}
          {caps && (
            <p className="field-hint">
              {t(
                "参考上限：图片 {0}，视频 {1}，音频 {2}",
                caps.max_images,
                caps.max_videos,
                caps.max_audios,
              )}
            </p>
          )}
        </CanvasPopover>
      )}
      {onUploadReferences && (
        <input
          ref={fileInput}
          type="file"
          multiple
          accept={role === "reference" ? "image/*,video/*,audio/*" : "image/*"}
          className="node-reference-file"
          aria-label={t("上传参考素材")}
          onChange={(event) => {
            if (event.target.files?.length)
              onUploadReferences(event.target.files, role);
            event.target.value = "";
            setOpen(false);
          }}
        />
      )}
    </div>
  );
}
