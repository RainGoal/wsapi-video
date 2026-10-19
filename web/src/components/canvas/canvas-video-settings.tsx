import { useEffect, useId, useRef, useState } from "react";
import { t } from "../../i18n";
import type { Capabilities, PricingBreakdown } from "../../types";
import { Icon } from "../../ui";
import { videoDurationRange } from "../../video-durations";
import { CanvasPopover } from "./canvas-popover";
import "./canvas-video-settings.css";

export type VideoReferenceMode = "reference" | "frames" | "mixed";

export interface CanvasVideoSettingsProps {
  productId: string;
  caps?: Capabilities;
  ratio: string;
  resolution: string;
  duration: number;
  audio: boolean;
  disabled?: boolean;
  price?: PricingBreakdown | null;
  referenceMode?: VideoReferenceMode;
  onRatioChange: (value: string) => void;
  onResolutionChange: (value: string) => void;
  onDurationChange: (value: number) => void;
  onAudioChange: (value: boolean) => void;
  onReferenceModeChange?: (value: "reference" | "frames") => void;
}

function RatioPreview({ ratio }: { ratio: string }) {
  const match = /^(\d+(?:\.\d+)?):(\d+(?:\.\d+)?)$/.exec(ratio);
  const proportion = match ? Number(match[1]) / Number(match[2]) : 0;
  if (!Number.isFinite(proportion) || proportion <= 0)
    return <Icon name="canvas" size={22} />;
  const width = Math.min(28, 24 * proportion);
  const height = width / proportion;
  return (
    <span className="video-ratio-preview" aria-hidden="true">
      <span style={{ width, height }} />
    </span>
  );
}

export function CanvasVideoSettings({
  productId,
  caps,
  ratio,
  resolution,
  duration,
  audio,
  disabled = false,
  price,
  referenceMode = "reference",
  onRatioChange,
  onResolutionChange,
  onDurationChange,
  onAudioChange,
  onReferenceModeChange,
}: CanvasVideoSettingsProps) {
  const id = useId();
  const anchor = useRef<HTMLButtonElement>(null);
  const skipDurationBlur = useRef(false);
  const [openProductId, setOpenProductId] = useState<string | null>(null);
  const [durationInput, setDurationInput] = useState(String(duration));
  const open = openProductId === productId && !disabled;
  const durations = [...new Set(caps?.durations ?? [])].sort((a, b) => a - b);
  const range = videoDurationRange(durations);
  const resolutionOptions = [...new Set(caps?.resolutions ?? [])];
  const ratioOptions = [...new Set(caps?.aspect_ratios ?? [])];
  const hasReferences = Boolean(
    caps && (caps.max_images || caps.max_videos || caps.max_audios),
  );
  const modeLabel =
    referenceMode === "mixed"
      ? t("混合参考")
      : referenceMode === "frames"
        ? t("首尾帧")
        : t("普通参考");
  const summary =
    [
      ...(resolution && resolutionOptions.includes(resolution)
        ? [resolution]
        : []),
      ...(ratio && ratioOptions.includes(ratio)
        ? [ratio === "auto" ? t("自动") : ratio]
        : []),
      ...(duration > 0 && durations.includes(duration)
        ? [t("{0} 秒", duration)]
        : []),
      ...(caps?.supports_frames ? [modeLabel] : []),
      ...(caps?.supports_audio && audio ? [t("音频")] : []),
    ].join(" · ") || t("模型默认");

  function show() {
    skipDurationBlur.current = false;
    setDurationInput(String(duration));
    setOpenProductId(productId);
  }

  function close(restoreFocus = true) {
    skipDurationBlur.current = true;
    setOpenProductId(null);
    setDurationInput(String(duration));
    if (restoreFocus) anchor.current?.focus({ preventScroll: true });
  }

  function changeDuration(value: number) {
    setDurationInput(String(value));
    if (value !== duration) onDurationChange(value);
  }

  function commitDuration() {
    if (!range) return;
    const parsed = durationInput.trim() ? Number(durationInput) : duration;
    const next = Number.isFinite(parsed)
      ? Math.max(range.min, Math.min(range.max, Math.round(parsed)))
      : duration;
    changeDuration(next);
  }

  useEffect(() => {
    setOpenProductId(null);
  }, [productId, disabled]);

  useEffect(() => setDurationInput(String(duration)), [duration, productId]);

  return (
    <div
      className="canvas-video-settings"
      data-canvas-no-zoom
      onPointerDown={(event) => event.stopPropagation()}
      onMouseDown={(event) => event.stopPropagation()}
      onClick={(event) => event.stopPropagation()}
      onDoubleClick={(event) => event.stopPropagation()}
      onWheel={(event) => event.stopPropagation()}
      onKeyDown={(event) => {
        if (event.key !== "Tab" && !(event.key === "Escape" && !open))
          event.stopPropagation();
      }}
    >
      <button
        ref={anchor}
        type="button"
        className="video-settings-trigger"
        aria-label={`${t("视频设置")} · ${summary}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        disabled={disabled}
        title={`${t("视频设置")} · ${summary}`}
        onClick={() => {
          if (open) close();
          else show();
        }}
        onKeyDown={(event) => {
          if (["ArrowDown", "ArrowUp"].includes(event.key)) {
            event.preventDefault();
            if (!disabled) show();
          }
        }}
      >
        <Icon name="settings" size={15} />
        <span>{summary}</span>
        <Icon name="down" size={12} />
      </button>
      {open && (
        <CanvasPopover
          anchor={anchor}
          width={384}
          className="canvas-select-menu video-settings-popover"
          label={t("视频设置")}
          role="dialog"
          id={id}
          onClose={() => close(false)}
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              event.preventDefault();
              close();
            }
            if (event.key !== "Tab") return;
            const controls = event.currentTarget.querySelectorAll<HTMLElement>(
              "button:not(:disabled), input:not(:disabled)",
            );
            const target = document.activeElement;
            if (event.shiftKey && target === controls[0]) {
              event.preventDefault();
              close();
            } else if (
              !event.shiftKey &&
              target === controls[controls.length - 1]
            ) {
              if (target?.getAttribute("type") === "number") commitDuration();
              close();
            }
          }}
        >
          <header className="video-settings-heading">
            <h3>{t("视频设置")}</h3>
            <button
              type="button"
              className="video-settings-close"
              aria-label={t("关闭视频设置")}
              onClick={() => close()}
            >
              <Icon name="close" size={16} />
            </button>
          </header>
          <fieldset className="video-settings-section">
            <legend>{t("清晰度")}</legend>
            <div className="video-settings-options">
              {resolutionOptions.length ? (
                resolutionOptions.map((value) => (
                  <button
                    key={value}
                    type="button"
                    aria-pressed={resolution === value}
                    onClick={() => onResolutionChange(value)}
                  >
                    {value}
                  </button>
                ))
              ) : (
                <span className="video-settings-default">{t("模型默认")}</span>
              )}
            </div>
          </fieldset>
          <fieldset className="video-settings-section">
            <legend>{t("画面比例")}</legend>
            <div className="video-settings-ratios">
              {ratioOptions.length ? (
                ratioOptions.map((value) => (
                  <button
                    key={value}
                    type="button"
                    aria-pressed={ratio === value}
                    onClick={() => onRatioChange(value)}
                  >
                    <RatioPreview ratio={value} />
                    <span>{value === "auto" ? t("自动") : value}</span>
                  </button>
                ))
              ) : (
                <span className="video-settings-default">{t("模型默认")}</span>
              )}
            </div>
          </fieldset>
          <fieldset className="video-settings-section">
            <legend>{t("视频时长")}</legend>
            {range ? (
              <>
                <div className="video-settings-duration">
                  <input
                    type="range"
                    min={range.min}
                    max={range.max}
                    step={1}
                    value={duration}
                    aria-label={t("视频时长")}
                    aria-valuetext={t("{0} 秒", duration)}
                    onChange={(event) =>
                      changeDuration(Number(event.target.value))
                    }
                  />
                  <label className="video-settings-seconds">
                    <input
                      type="number"
                      min={range.min}
                      max={range.max}
                      step={1}
                      inputMode="numeric"
                      value={durationInput}
                      aria-label={t("时长秒数")}
                      onChange={(event) => {
                        const value = event.target.value;
                        setDurationInput(value);
                        const number = Number(value);
                        if (
                          value.trim() &&
                          Number.isInteger(number) &&
                          number >= range.min &&
                          number <= range.max &&
                          number !== duration
                        )
                          onDurationChange(number);
                      }}
                      onBlur={() => {
                        if (!skipDurationBlur.current) commitDuration();
                      }}
                      onKeyDown={(event) => {
                        if (event.key === "Enter") {
                          event.preventDefault();
                          commitDuration();
                        }
                      }}
                    />
                    <span>{t("秒")}</span>
                  </label>
                </div>
                <p className="video-settings-hint">
                  {t("可选 {0}–{1} 秒，逐秒调整", range.min, range.max)}
                </p>
              </>
            ) : (
              <div className="video-settings-options">
                {durations.length > 1 ? (
                  durations.map((value) => (
                    <button
                      key={value}
                      type="button"
                      aria-pressed={duration === value}
                      onClick={() => changeDuration(value)}
                    >
                      {t("{0} 秒", value)}
                    </button>
                  ))
                ) : (
                  <span className="video-settings-default">
                    {durations.length
                      ? t("固定 {0} 秒", durations[0])
                      : t("模型默认")}
                  </span>
                )}
              </div>
            )}
          </fieldset>
          {caps?.supports_frames && (
            <fieldset className="video-settings-section">
              <legend>{t("参考模式")}</legend>
              <div className="video-settings-options video-settings-modes">
                {hasReferences && (
                  <button
                    type="button"
                    aria-pressed={referenceMode === "reference"}
                    disabled={!onReferenceModeChange}
                    onClick={() => onReferenceModeChange?.("reference")}
                  >
                    <Icon name="image" size={15} />
                    {t("普通参考")}
                  </button>
                )}
                <button
                  type="button"
                  aria-pressed={referenceMode === "frames"}
                  disabled={!onReferenceModeChange}
                  onClick={() => onReferenceModeChange?.("frames")}
                >
                  <Icon name="shots" size={15} />
                  {t("首尾帧")}
                </button>
              </div>
              <p className="video-settings-hint">
                {referenceMode === "mixed"
                  ? t("已保留混合参考，可在素材缩略图调整参考角色。")
                  : referenceMode === "frames"
                    ? t("添加图片作为首帧或尾帧，引导视频开始与结束的画面。")
                    : t(
                        "使用图片、视频或音频引导创作，以模型支持的素材类型为准。",
                      )}
              </p>
            </fieldset>
          )}
          {caps?.supports_audio && (
            <label className="video-settings-audio">
              <span>
                <strong>{t("生成原生音频")}</strong>
                <small>{t("同时生成与画面匹配的声音")}</small>
              </span>
              <input
                type="checkbox"
                role="switch"
                checked={audio}
                onChange={(event) => onAudioChange(event.target.checked)}
              />
              <span
                className="video-settings-switch-track"
                aria-hidden="true"
              />
            </label>
          )}
          {price && (
            <div className="video-settings-pricing">
              <div>
                <span>
                  {t(
                    price.billing_unit === "per_second"
                      ? "按秒计费"
                      : "按次计费",
                  )}
                </span>
                {price.resolution && <span>{price.resolution}</span>}
              </div>
              <strong>
                {price.billing_unit === "per_second"
                  ? t(
                      "{0} 积分/秒 × {1} 秒",
                      price.unit_credits,
                      price.quantity,
                    )
                  : t("{0} 积分/次", price.unit_credits)}
              </strong>
              <p className="video-settings-hint">
                {t(
                  price.billing_unit === "per_second"
                    ? "按所选请求时长计费"
                    : "同一分辨率每次固定积分，不随时长变化。",
                )}
              </p>
            </div>
          )}
        </CanvasPopover>
      )}
    </div>
  );
}
