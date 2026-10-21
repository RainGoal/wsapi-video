import { useRef, useState } from "react";
import { t } from "../../i18n";
import "./canvas-video.css";

export function CanvasVideo({
  src,
  onError,
}: {
  src: string;
  onError: () => void;
}) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [playing, setPlaying] = useState(false);
  const [muted, setMuted] = useState(false);
  const [duration, setDuration] = useState(0);
  const [currentTime, setCurrentTime] = useState(0);
  const [playFailed, setPlayFailed] = useState(false);
  const playLabel = playing ? t("暂停视频") : t("播放视频");

  async function togglePlayback() {
    const video = videoRef.current;
    if (!video) return;
    if (!video.paused) {
      video.pause();
      return;
    }
    setPlayFailed(false);
    try {
      await video.play();
    } catch {
      setPlayFailed(true);
    }
  }

  // Keep playback controls separate so the picture can use the canvas drag gesture.
  return (
    <div className="canvas-video">
      <video
        ref={videoRef}
        src={src}
        onError={onError}
        preload="metadata"
        playsInline
        tabIndex={-1}
        onPlay={() => setPlaying(true)}
        onPause={() => setPlaying(false)}
        onEnded={() => setPlaying(false)}
        onLoadedMetadata={(event) => {
          const value = event.currentTarget.duration;
          setDuration(Number.isFinite(value) ? value : 0);
        }}
        onTimeUpdate={(event) =>
          setCurrentTime(event.currentTarget.currentTime)
        }
        onVolumeChange={(event) => setMuted(event.currentTarget.muted)}
      />
      <div
        className="canvas-video-controls"
        onPointerDown={(event) => event.stopPropagation()}
        onDoubleClick={(event) => event.stopPropagation()}
      >
        <button
          type="button"
          aria-label={playLabel}
          title={playLabel}
          onClick={() => void togglePlayback()}
        >
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path
              d={playing ? "M6 5h4v14H6zm8 0h4v14h-4z" : "m8 5 11 7-11 7z"}
            />
          </svg>
        </button>
        <input
          type="range"
          min={0}
          max={duration || 1}
          step={0.01}
          value={Math.min(currentTime, duration)}
          disabled={duration <= 0}
          aria-label={t("视频播放进度")}
          onChange={(event) => {
            const value = Number(event.currentTarget.value);
            if (videoRef.current) videoRef.current.currentTime = value;
            setCurrentTime(value);
          }}
        />
        <span className="canvas-video-time">
          {formatTime(currentTime)} / {formatTime(duration)}
        </span>
        <button
          type="button"
          aria-label={muted ? t("开启声音") : t("静音视频")}
          title={muted ? t("开启声音") : t("静音视频")}
          onClick={() => {
            if (videoRef.current)
              videoRef.current.muted = !videoRef.current.muted;
          }}
        >
          <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path d="M11 5 6 9H3v6h3l5 4Z" />
            <path
              d={
                muted
                  ? "m16 9 5 6m-5 0 5-6"
                  : "M15 8a6 6 0 0 1 0 8m3-11a10 10 0 0 1 0 14"
              }
            />
          </svg>
        </button>
      </div>
      {playFailed && (
        <span className="canvas-video-play-error" role="status">
          {t("视频未能播放，请重试或打开预览")}
        </span>
      )}
    </div>
  );
}

function formatTime(seconds: number) {
  const whole = Math.max(0, Math.floor(seconds));
  return `${Math.floor(whole / 60)}:${String(whole % 60).padStart(2, "0")}`;
}
