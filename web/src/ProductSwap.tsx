import {
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
  type CSSProperties,
} from "react";
import { useStudio } from "./App";
import { api, errorMessage, isCurrentSession, sessionVersion } from "./api";
import { t, useLocale } from "./i18n";
import { validGenerationQuote } from "./generation-quote";
import { estimateProductPrice } from "./pricing";
import { Pagination, usePagedResource } from "./pagination";
import {
  Icon,
  LoadingError,
  Notice,
  Spinner,
  Status,
  formatDate,
  useResource,
} from "./ui";
import type { Asset, Product, Quote, Task } from "./types";
import {
  emptySwapDraft,
  readSwapDraft,
  swapDraftKey,
  swapProducts,
  swapQuoteInput,
  type SwapAnalysis,
  type SwapDraft,
  type SwapSettings,
} from "./product-swap";
import { useProductSwapSubmission } from "./use-product-swap-submission";
import "./product-swap.css";

type ToolWorkspace = {
  project_id: string;
  analysis_available: boolean;
  media_processing_available: boolean;
  max_duration_seconds: number;
};
type SwapTask = Task & {
  input?: {
    product_swap?: SwapSettings;
    duration?: number;
    resolution?: string;
  };
};
const angleOptions = () => [
  ["unknown", t("标注角度")],
  ["front", t("正面")],
  ["side", t("侧面")],
  ["back", t("背面")],
  ["detail", t("细节")],
];

export function ProductSwap() {
  useLocale();
  const [workspace, setWorkspace] = useState<ToolWorkspace | null>(null);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    let live = true;
    const version = sessionVersion();
    setError("");
    void api<ToolWorkspace>("/product-swap/workspace", "POST", {})
      .then((value) => {
        if (!value.project_id || !Number.isFinite(value.max_duration_seconds))
          throw new Error(t("工作区响应无效。"));
        if (live && isCurrentSession(version)) setWorkspace(value);
      })
      .catch((failure) => {
        if (live && isCurrentSession(version)) setError(errorMessage(failure));
      });
    return () => {
      live = false;
    };
  }, [retry]);
  if (!workspace)
    return (
      <div className="swap-page">
        <SwapHeading />
        <LoadingError
          loading={!error}
          error={error}
          retry={() => setRetry((v) => v + 1)}
        />
      </div>
    );
  return <SwapWorkspace key={workspace.project_id} workspace={workspace} />;
}

function SwapHeading() {
  return (
    <header className="swap-heading">
      <div>
        <div className="eyebrow">COMMERCE / PRODUCT SWAP</div>
        <h1>{t("好画面，换上你的商品。")}</h1>
        <p>{t("上传原视频和商品图，让 Seedance 参照原片段完成商品替换。")}</p>
      </div>
      <span className="swap-tool-tag">
        <Icon name="refresh" size={14} />
        {t("电商工具")}
      </span>
    </header>
  );
}

function SwapWorkspace({ workspace }: { workspace: ToolWorkspace }) {
  const { user, refreshWallet, navigate } = useStudio();
  const initial = useRef<{ draft: SwapDraft; error: string } | null>(null);
  if (!initial.current) {
    try {
      initial.current = {
        draft: readSwapDraft(localStorage.getItem(swapDraftKey(user.id))),
        error: "",
      };
    } catch {
      initial.current = {
        draft: emptySwapDraft(),
        error: t(
          "本地草稿无法读取。请检查浏览器存储；原提交恢复记录会单独保留。",
        ),
      };
    }
  }
  const [draft, setDraft] = useState(initial.current.draft);
  const [draftError, setDraftError] = useState(initial.current.error);
  const [assetMap, setAssetMap] = useState<Record<string, Asset>>({});
  const [hydrated, setHydrated] = useState(false);
  const [action, setAction] = useState("");
  const [error, setError] = useState("");
  const [analysis, setAnalysis] = useState<SwapAnalysis | null>(null);
  const [quote, setQuote] = useState<{
    value: Quote;
    fingerprint: string;
  } | null>(null);
  const [selected, setSelected] = useState<SwapTask | null>(null);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [historyVersion, setHistoryVersion] = useState(0);
  const [tick, setTick] = useState(0);
  const live = useRef(false);
  const running = useRef(false);
  const videoInput = useRef<HTMLInputElement>(null);
  const imageInput = useRef<HTMLInputElement>(null);
  const products = useResource<{ items: Product[] }>("/model-products", {
    items: [],
  });
  const available = swapProducts(products.data.items);
  const product = available.find((p) => p.id === draft.product_id);
  const caps = product?.capabilities;
  const source = assetMap[draft.swap.source_video_id];
  const input = swapQuoteInput(workspace.project_id, draft);
  const fingerprint = JSON.stringify(input);
  const latestInput = useRef(fingerprint);
  latestInput.current = fingerprint;
  const selectedID = selected?.id || draft.last_task_id;
  const selectedResource = useResource<SwapTask | null>(
    selectedID ? `/tasks/${selectedID}` : null,
    null,
    tick,
  );
  const task =
    selectedResource.data?.id === selectedID ? selectedResource.data : selected;
  const resultID =
    task?.delivery_status === "ready" ? task.asset_ids[0] : undefined;
  const result = useResource<Asset | null>(
    resultID ? `/assets/${resultID}` : null,
    null,
  );
  const taskSourceID = task?.input?.product_swap?.source_video_id;
  const taskSource = useResource<Asset | null>(
    taskSourceID ? `/assets/${taskSourceID}` : null,
    null,
  );
  const history = usePagedResource<SwapTask>(
    "/tasks",
    { project_id: workspace.project_id },
    historyVersion,
    8,
  );
  const submit = useProductSwapSubmission(
    user.id,
    workspace.project_id,
    (accepted) => {
      setSelected(accepted as SwapTask);
      setDraft((old) => ({ ...old, last_task_id: accepted.id }));
      setQuote(null);
      setHistoryVersion((v) => v + 1);
      refreshWallet();
    },
  );
  const locked = Boolean(
    action || submit.busy || submit.pending || submit.confirmed,
  );

  useEffect(() => {
    live.current = true;
    const version = sessionVersion();
    const saved = initial.current!.draft.swap;
    const ids = [
      saved.source_video_id,
      ...saved.images.map((image) => image.asset_id),
    ].filter(Boolean);
    void Promise.allSettled(
      ids.map((id) => api<Asset>(`/assets/${encodeURIComponent(id)}`)),
    ).then((values) => {
      if (!live.current || !isCurrentSession(version)) return;
      const loaded: Record<string, Asset> = {};
      for (const value of values)
        if (value.status === "fulfilled") loaded[value.value.id] = value.value;
      setAssetMap(loaded);
      if (values.some((value) => value.status === "rejected"))
        setError(t("部分草稿素材已不可读取，请重新上传或移除后继续。"));
      setHydrated(true);
    });
    return () => {
      live.current = false;
    };
  }, []);

  useEffect(() => {
    if (!hydrated || initial.current!.error) return;
    try {
      localStorage.setItem(swapDraftKey(user.id), JSON.stringify(draft));
      setDraftError("");
    } catch {
      setDraftError(
        t("草稿未能保存到浏览器，离开前请保留素材；生成提交仍需可靠恢复记录。"),
      );
    }
  }, [draft, hydrated, user.id]);

  useEffect(() => {
    if (!task || task.settlement_status !== "frozen") return;
    const timer = window.setInterval(() => {
      setTick((v) => v + 1);
      setHistoryVersion((v) => v + 1);
    }, 4000);
    return () => clearInterval(timer);
  }, [task?.id, task?.settlement_status]);
  useEffect(() => {
    if (task && task.settlement_status !== "frozen") refreshWallet();
  }, [task?.id, task?.settlement_status]);

  function change(next: Partial<SwapDraft>) {
    setDraft((old) => ({ ...old, ...next }));
    setQuote(null);
  }
  function changeSwap(next: Partial<SwapSettings>, assetsChanged = false) {
    setDraft((old) => ({
      ...old,
      ...(assetsChanged ? { last_task_id: undefined } : {}),
      swap: {
        ...old.swap,
        ...next,
        ...(assetsChanged ? { analysis_id: undefined } : {}),
      },
    }));
    setQuote(null);
    if (assetsChanged) {
      setAnalysis(null);
      setSelected(null);
    }
  }
  function chooseProduct(id: string) {
    const p = available.find((value) => value.id === id);
    if (!p) return;
    const durations = p.capabilities.durations.filter(
      (value) => value <= workspace.max_duration_seconds,
    );
    const closest = [...durations].sort(
      (a, b) =>
        Math.abs(a - (source?.duration_seconds || 5)) -
        Math.abs(b - (source?.duration_seconds || 5)),
    )[0];
    const aspect = source
      ? p.capabilities.aspect_ratios.find((value) => {
          const [w, h] = value.split(":").map(Number);
          return (
            w > 0 &&
            h > 0 &&
            Math.abs(w / h - source.width / source.height) < 0.03
          );
        })
      : undefined;
    change({
      product_id: id,
      duration: closest,
      resolution: p.capabilities.resolutions[0],
      aspect_ratio: aspect || p.capabilities.aspect_ratios[0],
    });
  }
  useEffect(() => {
    if (!products.loading && available.length && !product)
      chooseProduct(available[0].id);
  }, [products.loading, products.data]);

  async function run(
    name: string,
    work: (current: () => boolean) => Promise<void>,
  ) {
    if (running.current || locked || !hydrated) return;
    running.current = true;
    setAction(name);
    setError("");
    const version = sessionVersion();
    const current = () => live.current && isCurrentSession(version);
    try {
      await work(current);
    } catch (failure) {
      if (current()) setError(errorMessage(failure));
    } finally {
      running.current = false;
      if (current()) setAction("");
    }
  }

  async function upload(
    event: ChangeEvent<HTMLInputElement>,
    kind: "video" | "image",
  ) {
    const files = Array.from(event.target.files || []);
    event.target.value = "";
    if (!files.length) return;
    await run("upload", async (current) => {
      const room =
        Math.min(6, caps?.max_images || 6) - draft.swap.images.length;
      if (kind === "image" && files.length > room)
        throw new Error(t("商品图数量超过当前可用上限，请减少选择。"));
      for (const file of files) {
        if (!current()) return;
        if (!file.type.startsWith(`${kind}/`))
          throw new Error(t("所选文件类型不正确。"));
        const body = new FormData();
        body.append("project_id", workspace.project_id);
        body.append("file", file);
        const asset = await api<Asset>("/assets", "POST", body);
        if (!current()) return;
        if (asset.kind !== kind) throw new Error(t("素材内容与所选类型不符。"));
        setAssetMap((old) => ({ ...old, [asset.id]: asset }));
        if (kind === "video") {
          changeSwap({ source_video_id: asset.id }, true);
          if (caps) {
            const duration = [...caps.durations]
              .filter((d) => d <= workspace.max_duration_seconds)
              .sort(
                (a, b) =>
                  Math.abs(a - asset.duration_seconds) -
                  Math.abs(b - asset.duration_seconds),
              )[0];
            const aspect = caps.aspect_ratios.find((value) => {
              const [w, h] = value.split(":").map(Number);
              return Math.abs(w / h - asset.width / asset.height) < 0.03;
            });
            change({ duration, ...(aspect ? { aspect_ratio: aspect } : {}) });
          }
        } else {
          setDraft((old) => ({
            ...old,
            last_task_id: undefined,
            swap: {
              ...old.swap,
              analysis_id: undefined,
              images: [
                ...old.swap.images,
                { asset_id: asset.id, angle: "unknown" },
              ],
            },
          }));
          setQuote(null);
          setAnalysis(null);
          setSelected(null);
        }
      }
    });
  }

  async function analyze() {
    await run("analysis", async (current) => {
      const snapshot = fingerprint;
      const value = await api<SwapAnalysis>("/product-swap/analyze", "POST", {
        project_id: workspace.project_id,
        source_video_id: draft.swap.source_video_id,
        images: draft.swap.images,
      });
      if (!current() || latestInput.current !== snapshot) return;
      if (
        !value.id ||
        !Array.isArray(value.sampled_at) ||
        !Array.isArray(value.risks) ||
        !Array.isArray(value.missing_angles)
      )
        throw new Error(t("识图服务返回的数据不完整。"));
      setAnalysis(value);
      changeSwap({
        analysis_id: value.id,
        ...(!draft.swap.target.trim() ? { target: value.target } : {}),
      });
    });
  }
  async function getQuote() {
    await run("quote", async (current) => {
      const snapshot = fingerprint;
      const value = await api<Quote>("/quotes", "POST", input);
      if (!current() || latestInput.current !== snapshot) return;
      if (!validGenerationQuote(value, draft.product_id) || !value.prompt)
        throw new Error(t("服务端未返回有效的替换报价，请重试。"));
      setQuote({ value, fingerprint: snapshot });
    });
  }
  const problems: string[] = [];
  if (source && source.duration_seconds > workspace.max_duration_seconds)
    problems.push(
      t(
        "原视频超过 {0} 秒，请先剪成需要替换的片段。",
        workspace.max_duration_seconds,
      ),
    );
  if (caps && draft.swap.images.length > caps.max_images)
    problems.push(t("商品图数量超过当前模型上限。"));
  if (
    source &&
    draft.swap.audio_mode === "original" &&
    Math.abs(draft.duration - source.duration_seconds) > 0.25
  )
    problems.push(
      t(
        "保留原声需要输出时长与原片段相差不超过 0.25 秒。可调整时长或选择静音。",
      ),
    );
  if (
    !workspace.media_processing_available &&
    draft.swap.audio_mode !== "generated"
  )
    problems.push(t("媒体处理服务未配置，暂不能保留原声或输出静音视频。"));
  const hasAssets = Boolean(
    source &&
    draft.swap.images.length &&
    draft.swap.images.every((image) => assetMap[image.asset_id]),
  );
  const canQuote = Boolean(
    product &&
    hasAssets &&
    draft.swap.target.trim() &&
    draft.duration &&
    draft.resolution &&
    draft.aspect_ratio &&
    !problems.length &&
    hydrated,
  );
  const estimate =
    product && estimateProductPrice(product, draft.duration, draft.resolution);
  const quoteMatches = quote?.fingerprint === fingerprint;
  const validQuote =
    quoteMatches && validGenerationQuote(quote.value, draft.product_id);
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (!quote) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [quote]);
  const quoteLive = validQuote && Date.parse(quote.value.expires_at) > now;
  const displayedSource = taskSourceID
    ? taskSource.data?.id === taskSourceID
      ? taskSource.data
      : null
    : source;
  const displayedResult = result.data?.id === resultID ? result.data : null;
  const step = task
    ? 3
    : hasAssets && draft.swap.target.trim()
      ? 2
      : hasAssets
        ? 1
        : 0;

  return (
    <div className="swap-page">
      <SwapHeading />
      <div className="swap-topline">
        <ol className="swap-steps">
          {[t("上传素材"), t("确认替换"), t("报价生成"), t("对比下载")].map(
            (label, index) => (
              <li
                key={index}
                className={
                  index === step ? "active" : index < step ? "done" : ""
                }
              >
                <span>
                  {index < step ? (
                    <Icon name="check" size={12} />
                  ) : (
                    `0${index + 1}`
                  )}
                </span>
                {label}
              </li>
            ),
          )}
        </ol>
        <button
          className="text-button"
          onClick={() => setHistoryOpen(!historyOpen)}
        >
          <Icon name="clock" size={15} />
          {t("替换记录")}
          <span className="swap-count">{history.total}</span>
        </button>
      </div>
      {draftError && (
        <Notice error>
          {draftError}
          {initial.current.error && (
            <button
              className="text-button"
              onClick={() => {
                initial.current!.error = "";
                setDraftError("");
                change(emptySwapDraft());
              }}
            >
              {t("使用空白草稿")}
            </button>
          )}
        </Notice>
      )}
      {submit.pending && (
        <Notice>
          <strong>{t("有一笔提交等待确认。")}</strong>{" "}
          {t("请查询原提交结果，恢复操作使用同一提交编号。")}
        </Notice>
      )}
      {(error || submit.error || submit.storageError) && (
        <Notice error>
          {[error, submit.error, submit.storageError].filter(Boolean).join(" ")}
        </Notice>
      )}
      {submit.storageError && (
        <button
          className="button secondary small"
          disabled={submit.busy}
          onClick={submit.reload}
        >
          {t("重新读取恢复记录")}
        </button>
      )}
      <div className="swap-grid">
        <fieldset className="swap-materials" disabled={locked || !hydrated}>
          <section className="swap-card">
            <CardTitle
              number="01"
              title={t("原视频")}
              detail={t("保留好画面的起点")}
            />
            <button
              className={`swap-source-upload ${source ? "has-source" : ""}`}
              onClick={() => videoInput.current?.click()}
            >
              {source ? (
                <>
                  <video
                    src={source.preview_url}
                    muted
                    playsInline
                    preload="metadata"
                  />
                  <span className="swap-replace-file">
                    <Icon name="upload" size={14} />
                    {t("更换视频")}
                  </span>
                </>
              ) : (
                <>
                  <span className="swap-upload-icon">
                    <Icon name="video" size={26} />
                  </span>
                  <strong>{t("上传原视频")}</strong>
                  <span>MP4 / WebM</span>
                </>
              )}
            </button>
            <input
              ref={videoInput}
              type="file"
              accept="video/mp4,video/webm"
              className="swap-file-input"
              aria-label={t("上传原视频")}
              onChange={(event) => void upload(event, "video")}
            />
            {source ? (
              <div className="swap-file-meta">
                <strong title={source.file_name}>{source.file_name}</strong>
                <span>
                  {source.duration_seconds.toFixed(2)}s · {source.width} ×{" "}
                  {source.height}
                </span>
              </div>
            ) : (
              <p className="swap-help">
                {t(
                  "上传已剪好的短片段，最长 {0} 秒。",
                  workspace.max_duration_seconds,
                )}
              </p>
            )}
            {draft.swap.source_video_id && !source && (
              <Notice error>{t("原视频不可读取，请重新上传。")}</Notice>
            )}
          </section>
          <section className="swap-card">
            <CardTitle
              number="02"
              title={t("你的商品")}
              detail={t("多一面参考，多一分准确")}
            />
            <div className="swap-products">
              {draft.swap.images.map((image, index) => (
                <div className="swap-product" key={image.asset_id}>
                  <div className="swap-product-image">
                    {assetMap[image.asset_id] ? (
                      <img
                        src={assetMap[image.asset_id].preview_url}
                        alt={assetMap[image.asset_id].file_name}
                      />
                    ) : (
                      <span>{t("素材不可用")}</span>
                    )}
                    <button
                      className="swap-remove"
                      onClick={() =>
                        changeSwap(
                          {
                            images: draft.swap.images.filter(
                              (value) => value.asset_id !== image.asset_id,
                            ),
                          },
                          true,
                        )
                      }
                      aria-label={t("移除商品图 {0}", index + 1)}
                    >
                      <Icon name="close" size={13} />
                    </button>
                  </div>
                  <select
                    aria-label={t("商品图 {0} 的角度", index + 1)}
                    value={image.angle}
                    onChange={(event) =>
                      changeSwap(
                        {
                          images: draft.swap.images.map((value) =>
                            value.asset_id === image.asset_id
                              ? {
                                  ...value,
                                  angle: event.target
                                    .value as typeof value.angle,
                                }
                              : value,
                          ),
                        },
                        true,
                      )
                    }
                  >
                    {angleOptions().map(([value, label]) => (
                      <option key={value} value={value}>
                        {label}
                      </option>
                    ))}
                  </select>
                </div>
              ))}
              {draft.swap.images.length <
                Math.min(6, caps?.max_images || 6) && (
                <button
                  className="swap-add-product"
                  onClick={() => imageInput.current?.click()}
                >
                  <Icon name="plus" size={22} />
                  <span>
                    {draft.swap.images.length ? t("补充角度") : t("上传商品图")}
                  </span>
                </button>
              )}
            </div>
            <input
              ref={imageInput}
              type="file"
              accept="image/png,image/jpeg,image/webp"
              multiple
              className="swap-file-input"
              aria-label={t("上传商品图")}
              onChange={(event) => void upload(event, "image")}
            />
            <p className="swap-help">
              {t("建议提供同款商品的正面、侧面和背面，包装文字保持清晰。")}
            </p>
          </section>
          <div className="swap-tip">
            <Icon name="spark" size={18} />
            <p>{t("单商品、外形相近、遮挡较少的短片段，更适合开始尝试。")}</p>
          </div>
        </fieldset>
        <div className="swap-preview-column">
          <section className="swap-card swap-preview-card">
            <div className="swap-preview-heading">
              <span className="eyebrow">PREVIEW</span>
              <span>{task ? t("本次替换结果") : t("原片段预览")}</span>
            </div>
            <SwapPreview
              source={displayedSource || null}
              result={displayedResult}
              key={`${displayedSource?.id}:${displayedResult?.id}`}
            />
            <p className="swap-preview-caption">
              <Icon name="lock" size={13} />
              {t("素材与结果仅当前账号可访问")}
            </p>
          </section>
          {task && (
            <section className="swap-card swap-task" aria-live="polite">
              <div>
                <Icon
                  name={task.delivery_status === "ready" ? "check" : "clock"}
                />
                <strong>
                  {task.delivery_status === "ready"
                    ? t("视频已保存，可以对比和下载")
                    : t("替换任务")}
                </strong>
                <Status
                  value={
                    task.delivery_status === "ready"
                      ? "ready"
                      : task.execution_status
                  }
                />
              </div>
              <p>
                {task.error_message ||
                  (task.settlement_status === "frozen"
                    ? t("任务由后台持续处理，离开页面后仍可在替换记录中查看。")
                    : t("任务状态已更新。"))}
              </p>
              <div className="swap-task-links">
                <button
                  className="text-button"
                  onClick={() => navigate("tasks", task.id)}
                >
                  {t("查看任务详情")}
                  <Icon name="arrow" size={13} />
                </button>
                {displayedResult && (
                  <a
                    className="button primary small"
                    href={displayedResult.download_url}
                  >
                    <Icon name="download" size={14} />
                    {t("下载视频")}
                  </a>
                )}
              </div>
              <LoadingError
                loading={selectedResource.loading && !selectedResource.data}
                error={
                  selectedResource.error || result.error || taskSource.error
                }
                retry={() => {
                  selectedResource.refresh();
                  result.refresh();
                  taskSource.refresh();
                }}
              />
            </section>
          )}
          <div className="swap-expectation">
            <span>SEEDANCE</span>
            <p>
              {t(
                "模型会依据参考素材重新生成视频。人物、动作和背景可能发生变化，请通过对比确认成片。",
              )}
            </p>
          </div>
        </div>
        <fieldset className="swap-settings" disabled={locked || !hydrated}>
          <section className="swap-card swap-analysis">
            <div className="swap-analysis-heading">
              <span>
                <Icon name="spark" size={16} />
                {t("素材分析")}
              </span>
              <span
                className={`swap-analysis-dot ${analysis ? "ready" : ""}`}
              />
            </div>
            {analysis ? (
              <>
                <p className="swap-analysis-label">{t("建议替换对象")}</p>
                <strong>{analysis.target}</strong>
                <dl>
                  <dt>{t("动作与镜头")}</dt>
                  <dd>{analysis.action}</dd>
                  <dt>{t("遮挡关系")}</dt>
                  <dd>{analysis.occlusion}</dd>
                  <dt>{t("新商品外观")}</dt>
                  <dd>{analysis.product_description}</dd>
                </dl>
                {[...analysis.risks, ...analysis.missing_angles].map(
                  (value, index) => (
                    <p className="swap-analysis-risk" key={index}>
                      <Icon name="info" size={13} />
                      {value}
                    </p>
                  ),
                )}
                <p className="swap-help">
                  {t(
                    "基于 {0} 张抽样帧，无法覆盖全部动作。请确认后使用。",
                    analysis.sampled_at.length,
                  )}
                </p>
                <button
                  className="text-button"
                  onClick={() => {
                    setAnalysis(null);
                    changeSwap({ analysis_id: undefined });
                  }}
                >
                  {t("移除分析结果")}
                </button>
              </>
            ) : (
              <>
                <p className="swap-analysis-empty">
                  {workspace.analysis_available
                    ? t("识别商品位置、动作与遮挡，为替换提供画面依据。")
                    : t("识图服务尚未配置。你可以手动描述替换对象，继续生成。")}
                </p>
                <div className="swap-analysis-placeholders">
                  <span>{t("替换对象")}</span>
                  <span>{t("动作 / 遮挡")}</span>
                  <span>{t("缺图建议")}</span>
                </div>
              </>
            )}
            <button
              className="button secondary swap-analyze-button"
              disabled={
                !workspace.analysis_available ||
                !hasAssets ||
                Boolean(
                  source &&
                  source.duration_seconds > workspace.max_duration_seconds,
                )
              }
              onClick={() => void analyze()}
            >
              {action === "analysis" ? (
                <Spinner />
              ) : (
                <Icon name="spark" size={14} />
              )}
              {action === "analysis"
                ? t("正在抽帧分析…")
                : analysis
                  ? t("重新分析素材")
                  : t("分析素材")}
            </button>
            <p className="swap-help">{t("可选步骤，不收取生成积分。")}</p>
          </section>
          <section className="swap-card swap-form">
            <CardTitle number="03" title={t("替换设置")} />
            <label>
              {t("要替换哪个商品")}
              <textarea
                rows={2}
                maxLength={400}
                value={draft.swap.target}
                onChange={(event) => changeSwap({ target: event.target.value })}
                placeholder={t("例如：人物右手中拿着的白色瓶子")}
              />
            </label>
            <label>
              {t("补充要求")}
              <textarea
                rows={3}
                maxLength={1200}
                value={draft.swap.instructions}
                onChange={(event) =>
                  changeSwap({ instructions: event.target.value })
                }
                placeholder={t("例如：保持瓶身比例，品牌文字以商品图为准")}
              />
            </label>
            <LoadingError
              loading={products.loading}
              error={products.error}
              retry={products.refresh}
            />
            {!products.loading && !products.error && !available.length && (
              <Notice>
                {t(
                  "暂无启用商品替换的 Seedance 商品，请管理员完成能力验证并上架。",
                )}
                {user.role === "admin" && (
                  <button
                    className="text-button"
                    onClick={() => navigate("admin", "products")}
                  >
                    {t("配置模型商品")}
                  </button>
                )}
              </Notice>
            )}
            <label>
              {t("视频模型")}
              <select
                value={product?.id || ""}
                onChange={(event) => chooseProduct(event.target.value)}
              >
                <option value="" disabled>
                  {t("选择已验证的 Seedance 商品")}
                </option>
                {available.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </label>
            <div className="swap-parameter-grid">
              <label>
                {t("输出时长")}
                <select
                  value={draft.duration || ""}
                  onChange={(event) =>
                    change({ duration: Number(event.target.value) })
                  }
                >
                  <option value="" disabled>
                    —
                  </option>
                  {caps?.durations
                    .filter((value) => value <= workspace.max_duration_seconds)
                    .map((value) => (
                      <option key={value} value={value}>
                        {value} s
                      </option>
                    ))}
                </select>
              </label>
              <label>
                {t("分辨率")}
                <select
                  value={draft.resolution}
                  onChange={(event) =>
                    change({ resolution: event.target.value })
                  }
                >
                  <option value="" disabled>
                    —
                  </option>
                  {caps?.resolutions.map((value) => (
                    <option key={value}>{value}</option>
                  ))}
                </select>
              </label>
            </div>
            <label>
              {t("画幅比例")}
              <select
                value={draft.aspect_ratio}
                onChange={(event) =>
                  change({ aspect_ratio: event.target.value })
                }
              >
                <option value="" disabled>
                  —
                </option>
                {caps?.aspect_ratios.map((value) => (
                  <option key={value}>{value}</option>
                ))}
              </select>
            </label>
            <label>
              {t("音轨处理")}
              <select
                value={draft.swap.audio_mode}
                onChange={(event) =>
                  changeSwap({
                    audio_mode: event.target
                      .value as SwapSettings["audio_mode"],
                  })
                }
              >
                <option
                  value="original"
                  disabled={!workspace.media_processing_available}
                >
                  {t("保留原声")}
                </option>
                <option
                  value="muted"
                  disabled={!workspace.media_processing_available}
                >
                  {t("静音")}
                </option>
                <option value="generated">{t("使用模型输出音轨")}</option>
              </select>
            </label>
            <p className="swap-help">
              {t("保留原声会在生成后合成原片段音轨，不改变音速。")}
            </p>
            <label className="swap-checkbox">
              <input
                type="checkbox"
                checked={draft.swap.preserve_scene}
                onChange={(event) =>
                  changeSwap({ preserve_scene: event.target.checked })
                }
              />
              <span>{t("尽量保留背景、光照和镜头")}</span>
            </label>
            <details className="swap-prompt">
              <summary>
                {t("查看生成指令")}
                <Icon name="down" size={13} />
              </summary>
              <p>
                {quoteMatches
                  ? quote.value.prompt
                  : t("确认替换设置并获取报价后，展示服务器生成的完整指令。")}
              </p>
            </details>
          </section>
        </fieldset>
      </div>
      {problems.length > 0 && <Notice error>{problems.join(" ")}</Notice>}
      <footer className="swap-footer">
        <div className="swap-footer-copy">
          <strong>
            {action === "upload"
              ? t("正在上传并校验素材…")
              : action === "analysis"
                ? t("正在读取画面与商品细节…")
                : t("把好内容，变成你的商品视频。")}
          </strong>
          <span>{t("提交时冻结积分，视频保存完成后结算。")}</span>
        </div>
        <div className="swap-price">
          <strong>
            {quoteMatches
              ? quote.value.credits.toLocaleString()
              : estimate
                ? estimate.total_credits.toLocaleString()
                : "—"}
            <small>{t("积分")}</small>
          </strong>
          <span>
            {quoteLive
              ? t(
                  "报价有效至 {0}",
                  new Date(quote.value.expires_at).toLocaleTimeString(),
                )
              : quote
                ? t("报价已过期，请重新获取")
                : estimate
                  ? estimate.billing_unit === "per_second"
                    ? t(
                        "{0} 积分/秒 × {1} 秒，待报价确认",
                        estimate.unit_credits,
                        estimate.quantity,
                      )
                    : t("按当前商品价预估，待报价确认")
                  : t("选择模型后显示价格")}
          </span>
        </div>
        {submit.pending || submit.confirmed ? (
          <button
            className="button primary swap-generate"
            disabled={submit.busy || !submit.ready}
            onClick={() => void submit.submit()}
          >
            {submit.busy ? <Spinner /> : <Icon name="refresh" />}
            {submit.confirmed ? t("重试清理恢复记录") : t("查询 / 重试原提交")}
          </button>
        ) : (
          <button
            className="button primary swap-generate"
            disabled={
              locked ||
              !canQuote ||
              !submit.ready ||
              Boolean(initial.current.error)
            }
            onClick={() => {
              if (quoteLive) void submit.submit(quote.value, draft.product_id);
              else void getQuote();
            }}
          >
            {action === "quote" || submit.busy ? (
              <Spinner />
            ) : (
              <Icon name="spark" />
            )}
            {action === "quote"
              ? t("正在获取报价…")
              : submit.busy
                ? t("正在提交…")
                : quoteLive
                  ? t("确认并生成 · {0} 积分", quote.value.credits)
                  : t("获取替换报价")}
            <Icon name="arrow" size={16} />
          </button>
        )}
      </footer>
      {historyOpen && (
        <section className="swap-card swap-history">
          <div className="swap-history-heading">
            <h2>{t("替换记录")}</h2>
            <button className="text-button" onClick={history.refresh}>
              <Icon name="refresh" size={13} />
              {t("刷新")}
            </button>
          </div>
          <LoadingError
            loading={history.loading}
            error={history.error}
            retry={history.refresh}
          />
          {!history.loading && !history.error && !history.items.length && (
            <p className="swap-help">{t("你的替换视频会保存在这里。")}</p>
          )}
          {history.items.map((entry) => (
            <button
              className={`swap-history-row ${selected?.id === entry.id ? "selected" : ""}`}
              key={entry.id}
              onClick={() => {
                setSelected(entry);
                setDraft((old) => ({ ...old, last_task_id: entry.id }));
                setTick((v) => v + 1);
              }}
            >
              <Icon name="video" />
              <span>
                <strong>{entry.input?.product_swap?.target || entry.id}</strong>
                <small>
                  {formatDate(entry.created_at)} · {entry.input?.duration}s ·{" "}
                  {entry.input?.resolution} · {entry.credits} {t("积分")}
                </small>
              </span>
              <Status
                value={
                  entry.delivery_status === "ready"
                    ? "ready"
                    : entry.execution_status
                }
              />
              <Icon name="arrow" size={15} />
            </button>
          ))}
          <Pagination
            page={history.page}
            size={history.size}
            total={history.total}
            loading={history.loading}
            onPage={history.setPage}
          />
        </section>
      )}
    </div>
  );
}

function CardTitle({
  number,
  title,
  detail,
}: {
  number: string;
  title: string;
  detail?: string;
}) {
  return (
    <header className="swap-card-title">
      <span>{number}</span>
      <div>
        <h2>{title}</h2>
        {detail && <p>{detail}</p>}
      </div>
    </header>
  );
}

function SwapPreview({
  source,
  result,
}: {
  source: Asset | null;
  result: Asset | null;
}) {
  const original = useRef<HTMLVideoElement>(null);
  const replacement = useRef<HTMLVideoElement>(null);
  const playback = useRef({ revision: 0, active: false });
  const [compare, setCompare] = useState(Boolean(result));
  const [split, setSplit] = useState(50);
  const [time, setTime] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [error, setError] = useState("");
  const duration = Math.min(
    source?.duration_seconds || 0,
    result?.duration_seconds || Infinity,
  );
  useEffect(
    () => () => {
      playback.current.revision += 1;
      playback.current.active = false;
    },
    [],
  );
  function pause() {
    playback.current.revision += 1;
    playback.current.active = false;
    original.current?.pause();
    replacement.current?.pause();
    setPlaying(false);
  }
  async function play() {
    if (playback.current.active) {
      pause();
      return;
    }
    const first = original.current;
    const second = compare ? replacement.current : null;
    if (!first) return;
    setError("");
    if (first.currentTime >= duration - 0.05) seek(0);
    const revision = ++playback.current.revision;
    playback.current.active = true;
    setPlaying(true);
    try {
      if (second && Math.abs(second.currentTime - first.currentTime) > 0.04)
        second.currentTime = first.currentTime;
      await Promise.all([first.play(), ...(second ? [second.play()] : [])]);
    } catch {
      if (playback.current.revision !== revision) return;
      pause();
      setError(t("浏览器无法播放该视频，请下载查看或更换兼容编码。"));
    }
  }
  function seek(value: number) {
    if (original.current) original.current.currentTime = value;
    if (replacement.current) replacement.current.currentTime = value;
    setTime(value);
  }
  return (
    <>
      <div className="swap-preview-tabs">
        <button
          className={!compare ? "active" : ""}
          onClick={() => {
            pause();
            setCompare(false);
          }}
        >
          {t("原视频")}
        </button>
        <button
          disabled={!result}
          className={compare ? "active" : ""}
          onClick={() => {
            pause();
            setCompare(true);
          }}
        >
          {t("替换对比")}
        </button>
        {source && (
          <span>
            {source.width} × {source.height}
          </span>
        )}
      </div>
      <div
        className={`swap-stage ${source ? "has-video" : ""}`}
        style={{ "--swap-split": `${split}%` } as CSSProperties}
      >
        {source ? (
          <>
            <video
              ref={original}
              src={source.preview_url}
              playsInline
              muted
              preload="metadata"
              onError={() =>
                setError(t("原视频预览失败，请下载查看或重新上传。"))
              }
              onTimeUpdate={() => {
                const value = original.current?.currentTime || 0;
                setTime(value);
                if (value >= duration) pause();
                if (
                  compare &&
                  replacement.current &&
                  Math.abs(replacement.current.currentTime - value) > 0.12
                )
                  replacement.current.currentTime = value;
              }}
              onEnded={pause}
            />
            {result && (
              <video
                ref={replacement}
                className="swap-result-layer"
                src={result.preview_url}
                playsInline
                muted
                preload="metadata"
                style={{
                  clipPath: compare
                    ? `inset(0 0 0 ${split}%)`
                    : "inset(0 0 0 100%)",
                }}
                onError={() => setError(t("结果视频预览失败，请下载查看。"))}
                onEnded={pause}
              />
            )}
            {compare && result && (
              <>
                <span className="swap-stage-label before">{t("原视频")}</span>
                <span className="swap-stage-label after">{t("替换后")}</span>
                <div className="swap-divider">
                  <span>‹ ›</span>
                </div>
                <input
                  className="swap-comparison-range"
                  type="range"
                  min={5}
                  max={95}
                  value={split}
                  aria-label={t("拖动对比分割线")}
                  onChange={(event) => setSplit(Number(event.target.value))}
                />
              </>
            )}
          </>
        ) : (
          <div className="swap-preview-empty">
            <div className="swap-viewfinder">
              <Icon name="video" size={30} />
            </div>
            <strong>{t("从一段好视频开始")}</strong>
            <p>{t("上传原片段，在这里预览画面和替换结果。")}</p>
          </div>
        )}
      </div>
      <div className="swap-playbar">
        <button
          className="icon-button"
          disabled={!source}
          aria-label={playing ? t("暂停预览") : t("播放预览")}
          onClick={() => void play()}
        >
          <Icon name={playing ? "pause" : "play"} size={15} />
        </button>
        <span>{time.toFixed(1)}s</span>
        <input
          type="range"
          min={0}
          max={duration || 1}
          step={0.01}
          value={Math.min(time, duration || 1)}
          disabled={!source}
          aria-label={t("视频预览进度")}
          onChange={(event) => seek(Number(event.target.value))}
        />
        <span>{duration.toFixed(1)}s</span>
      </div>
      {compare && result && (
        <p className="swap-help swap-sync-note">
          {t("拖动分割线查看差异；两段视频同步静音预览，音轨请下载确认。")}
        </p>
      )}
      {error && <Notice error>{error}</Notice>}
    </>
  );
}
