import { t } from "./i18n";
import "./creation.css";
import { useEffect, useRef, useState } from "react";
import { api, ApiError, errorMessage, items } from "./api";
import { useStudio } from "./App";
import type { Asset, Product, Quote, Task } from "./types";
import {
  canvasGenerationDefaults,
  canvasReferenceSelection,
  generationReferenceDefaults,
} from "./creation";
import type { CanvasNodeData } from "./types/canvas";
import type { CanvasReferenceIssue } from "./lib/canvas/canvas-references";
import { Icon, LoadingError, Modal, Notice, Spinner, useResource } from "./ui";
import {
  clearPendingSubmission,
  persistPendingSubmission,
  readPendingSubmission,
  submissionHasValidTask,
  submissionRecoveryKey,
  submissionWasRejected,
  withSubmissionRecoveryLock,
  type PendingSubmission,
} from "./submission-recovery";
export function Generate({
  projectId,
  assets,
  node,
  initialKind,
  initialReferenceIds = [],
  referenceNotes = [],
  referenceIssues = [],
  onClose,
  onCreated,
}: {
  projectId: string;
  assets: Asset[];
  node?: CanvasNodeData;
  initialKind?: "image" | "video";
  initialReferenceIds?: string[];
  referenceNotes?: { nodeId: string; title: string; content: string }[];
  referenceIssues?: CanvasReferenceIssue[];
  onClose: () => void;
  onCreated: (task: Task, recovered?: boolean) => void;
}) {
  const { user, system, refreshWallet, navigate } = useStudio();
  const products = useResource<
    | Product[]
    | {
        items: Product[];
      }
  >("/model-products", []);
  const initial = canvasGenerationDefaults(
    node,
    initialKind,
    initialReferenceIds,
    referenceNotes,
  );
  const initialSelection = canvasReferenceSelection(node, initial.referenceIds);
  const [kind, setKind] = useState<"image" | "video">(initial.kind),
    [productId, setProductId] = useState(node?.metadata?.productId || ""),
    [prompt, setPrompt] = useState(initial.prompt),
    [ratio, setRatio] = useState(""),
    [duration, setDuration] = useState(0),
    [resolution, setResolution] = useState(""),
    [size, setSize] = useState(""),
    [quality, setQuality] = useState(""),
    [audio, setAudio] = useState(false),
    [references, setReferences] = useState<string[]>(
      initialSelection.referenceIds,
    ),
    [startFrame, setStartFrame] = useState(initialSelection.startFrameId),
    [endFrame, setEndFrame] = useState(initialSelection.endFrameId),
    [quote, setQuote] = useState<Quote | null>(null),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [insufficient, setInsufficient] = useState(false),
    [recovery, setRecovery] = useState<PendingSubmission | null>(null),
    [recoveryReady, setRecoveryReady] = useState(false),
    [storageError, setStorageError] = useState(""),
    [settled, setSettled] = useState<{
      payload: PendingSubmission;
      task?: Task;
      recovered: boolean;
    } | null>(null);
  const requestKey = useRef("");
  const busyRef = useRef(false);
  const referenceRolesExplicit = useRef(
    initialSelection.restored &&
      node?.metadata?.generationKind === initial.kind,
  );
  const referenceMode = useRef(initial.kind);
  const live = useRef(true);
  const recoveryKey = submissionRecoveryKey(user.id, projectId);
  const storageFailure = () =>
    t(
      "无法安全读取或保存提交恢复记录。请恢复浏览器存储后重新读取；核查原任务前不要发起重复生成。",
    );
  function reloadRecovery() {
    try {
      setRecovery(readPendingSubmission(localStorage, recoveryKey));
      setStorageError("");
    } catch {
      setStorageError(storageFailure());
    } finally {
      setRecoveryReady(true);
    }
  }
  useEffect(() => {
    live.current = true;
    reloadRecovery();
    return () => {
      live.current = false;
    };
  }, [recoveryKey]);
  const locked =
    busy || !recoveryReady || !!recovery || !!storageError || !!settled;
  const available = items(products.data).filter((p) => p.kind === kind);
  const product = available.find((p) => p.id === productId) || available[0];
  const latestQuoteInput = useRef("");
  latestQuoteInput.current = JSON.stringify([
    product?.id,
    prompt,
    kind,
    duration,
    resolution,
    ratio,
    size,
    quality,
    audio,
    references,
    startFrame,
    endFrame,
  ]);
  useEffect(() => {
    if (!product) return;
    const caps = product.capabilities;
    setRatio((current) =>
      caps.aspect_ratios?.includes(current)
        ? current
        : caps.aspect_ratios?.[0] || "",
    );
    setDuration((current) =>
      caps.durations?.includes(current) ? current : caps.durations?.[0] || 0,
    );
    setResolution(caps.resolutions?.[0] || "");
    setSize(kind === "image" ? caps.sizes?.[0] || "" : "");
    setQuality(kind === "image" ? caps.qualities?.[0] || "" : "");
    setAudio(false);
    const selection = generationReferenceDefaults(
      kind,
      caps.supports_frames,
      [...references, startFrame, endFrame].filter(Boolean),
      assets,
      startFrame ||
        (node?.type === "image" ? node.metadata?.assetId : undefined),
      endFrame,
      referenceRolesExplicit.current && referenceMode.current === kind,
    );
    setStartFrame(selection.startFrameId);
    setReferences(selection.referenceIds);
    setEndFrame(selection.endFrameId);
    referenceRolesExplicit.current = true;
    referenceMode.current = kind;
  }, [product?.id]);
  useEffect(() => {
    setQuote(null);
    requestKey.current = "";
    setError("");
  }, [
    kind,
    product?.id,
    prompt,
    ratio,
    duration,
    resolution,
    size,
    quality,
    audio,
    references,
    startFrame,
    endFrame,
  ]);
  async function getQuote() {
    if (!product || busyRef.current || locked) return;
    busyRef.current = true;
    const submittedInput = latestQuoteInput.current;
    setBusy(true);
    setError("");
    try {
      const next = await api<Quote>("/quotes", "POST", {
        project_id: projectId,
        product_id: product.id,
        prompt,
        ...(ratio ? { aspect_ratio: ratio } : {}),
        ...(duration && kind === "video" ? { duration } : {}),
        ...(resolution ? { resolution } : {}),
        ...(kind === "image" && size ? { size } : {}),
        ...(kind === "image" && quality ? { quality } : {}),
        ...(product.capabilities.supports_audio ? { audio } : {}),
        reference_asset_ids: references,
        ...(startFrame ? { start_frame_asset_id: startFrame } : {}),
        ...(endFrame ? { end_frame_asset_id: endFrame } : {}),
      });
      if (!live.current) return;
      if (latestQuoteInput.current !== submittedInput) {
        setError(t("输入已更改，请重新获取当前参数的报价。"));
        return;
      }
      setQuote(next);
      requestKey.current = crypto.randomUUID();
    } catch (reason) {
      setError(errorMessage(reason));
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  }
  async function finishConfirmed(result: NonNullable<typeof settled>) {
    try {
      await withSubmissionRecoveryLock(navigator.locks, recoveryKey, () =>
        clearPendingSubmission(localStorage, recoveryKey, result.payload),
      );
    } catch {
      setSettled(result);
      setStorageError(
        t(
          "服务端结果已确认，但本地恢复记录未能清理。请重试清理；不会再次发送生成请求。",
        ),
      );
      return;
    }
    setRecovery(null);
    setSettled(null);
    setStorageError("");
    setQuote(null);
    requestKey.current = "";
    if (result.task && live.current) {
      refreshWallet();
      onCreated(result.task, result.recovered);
    }
  }
  async function submit(retry?: PendingSubmission) {
    if (busyRef.current) return;
    if (settled) {
      busyRef.current = true;
      setBusy(true);
      try {
        await finishConfirmed(settled);
      } finally {
        busyRef.current = false;
        if (live.current) setBusy(false);
      }
      return;
    }
    if (
      !recoveryReady ||
      storageError ||
      (!quote && !retry) ||
      (recovery && !retry)
    )
      return;
    const payload = retry || {
      quote_id: quote!.id,
      request_key: requestKey.current || crypto.randomUUID(),
    };
    busyRef.current = true;
    setBusy(true);
    try {
      await withSubmissionRecoveryLock(navigator.locks, recoveryKey, () =>
        persistPendingSubmission(localStorage, recoveryKey, payload),
      );
    } catch {
      busyRef.current = false;
      setBusy(false);
      reloadRecovery();
      setStorageError(
        navigator.locks
          ? storageFailure()
          : t(
              "当前浏览器无法安全保护跨标签提交。请使用支持 Web Locks 的新版浏览器，并通过 HTTPS 或本机地址访问。",
            ),
      );
      return;
    }
    if (!live.current) {
      busyRef.current = false;
      return;
    }
    setRecovery(payload);
    setBusy(true);
    setError("");
    setInsufficient(false);
    try {
      const task = await api<unknown>("/tasks", "POST", payload);
      if (!submissionHasValidTask(task, projectId))
        throw new ApiError(
          200,
          "invalid_response",
          t(
            "服务端未返回有效任务数据，提交结果尚未确认。请沿用原提交编号重试。",
          ),
        );
      if (!live.current) return;
      await finishConfirmed({ payload, task, recovered: Boolean(retry) });
    } catch (reason) {
      if (!live.current) return;
      const rejected =
        reason instanceof ApiError &&
        submissionWasRejected(reason.status, reason.code, Boolean(retry));
      setInsufficient(reason instanceof ApiError && reason.status === 402);
      if (rejected) {
        await finishConfirmed({ payload, recovered: Boolean(retry) });
      }
      setError(
        rejected
          ? errorMessage(reason)
          : t(
              "{0} 可使用“查询 / 重试原提交”恢复；该操作沿用同一去重键。",
              errorMessage(reason),
            ),
      );
    } finally {
      busyRef.current = false;
      if (live.current) setBusy(false);
    }
  }
  const caps = product?.capabilities;
  const selectedAssets = references.map((id) =>
    assets.find((asset) => asset.id === id),
  );
  const referenceProblems: string[] = [];
  if (selectedAssets.some((asset) => !asset))
    referenceProblems.push(t("部分参考素材已不可用，请移除后重新选择。"));
  if (caps) {
    if (
      selectedAssets.filter((asset) => asset?.kind === "image").length >
      caps.max_images
    )
      referenceProblems.push(t("参考图片超过当前模型上限，请减少选择。"));
    if (
      selectedAssets.filter((asset) => asset?.kind === "video").length >
      caps.max_videos
    )
      referenceProblems.push(t("参考视频超过当前模型上限，请减少选择。"));
    if (
      selectedAssets.filter((asset) => asset?.kind === "audio").length >
      caps.max_audios
    )
      referenceProblems.push(t("参考音频超过当前模型上限，请减少选择。"));
  }
  const frameIds = [startFrame, endFrame].filter(Boolean);
  if (
    new Set([...references, ...frameIds]).size !==
    references.length + frameIds.length
  )
    referenceProblems.push(t("首尾帧与参考素材不能重复，请调整选择。"));
  return (
    <Modal
      title={kind === "video" ? t("视频创作") : t("图片创作")}
      onClose={onClose}
      closeDisabled={busy}
      wide
    >
      <fieldset className="generation-fields" disabled={locked}>
        <div className="generate-layout">
          <div className="generate-form">
            <div className="segmented">
              <button
                className={kind === "image" ? "active" : ""}
                onClick={() => setKind("image")}
              >
                <Icon name="image" size={16} />
                {t("图片")}
              </button>
              <button
                className={kind === "video" ? "active" : ""}
                onClick={() => setKind("video")}
              >
                <Icon name="video" size={16} />
                {t("视频")}
              </button>
            </div>
            <LoadingError
              loading={products.loading}
              error={products.error}
              retry={products.refresh}
            />
            {!products.loading && !products.error && !available.length && (
              <Notice>
                {t("暂未配置已验证的")}
                {kind === "image" ? t("图片") : t("视频")}
                {t(
                  "商品。管理员完成网关、能力和价格配置并上架后，即可开始生成。",
                )}
              </Notice>
            )}
            {product && (
              <>
                <label>
                  {t("生成模型")}
                  <select
                    value={product.id}
                    onChange={(e) => setProductId(e.target.value)}
                  >
                    {available.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name}
                        {p.demo ? t(" · 开发模拟") : ""}
                      </option>
                    ))}
                  </select>
                  <small className="field-hint">
                    {product.model}
                    {t("· 能力版本")}
                    {product.version}
                  </small>
                </label>
                <label>
                  {t("描述你想看到的画面")}
                  <textarea
                    value={prompt}
                    onChange={(e) => setPrompt(e.target.value)}
                    placeholder={t(
                      "主体是什么？画面、光线、镜头、风格，以及你想保留的细节…",
                    )}
                    rows={6}
                    maxLength={12000}
                  />
                </label>
                <div className="parameter-grid">
                  {caps?.aspect_ratios?.length ? (
                    <label>
                      {t("画面比例")}
                      <select
                        value={ratio}
                        onChange={(e) => setRatio(e.target.value)}
                      >
                        {caps.aspect_ratios.map((v) => (
                          <option key={v}>{v}</option>
                        ))}
                      </select>
                    </label>
                  ) : null}
                  {kind === "video" && caps?.durations?.length ? (
                    <label>
                      {t("视频时长")}
                      <select
                        value={duration}
                        onChange={(e) => setDuration(Number(e.target.value))}
                      >
                        {caps.durations.map((v) => (
                          <option key={v} value={v}>
                            {v}
                            {t("秒")}
                          </option>
                        ))}
                      </select>
                    </label>
                  ) : null}
                  {caps?.resolutions?.length ? (
                    <label>
                      {t("分辨率")}
                      <select
                        value={resolution}
                        onChange={(e) => setResolution(e.target.value)}
                      >
                        {caps.resolutions.map((v) => (
                          <option key={v}>{v}</option>
                        ))}
                      </select>
                    </label>
                  ) : null}
                  {kind === "image" && caps?.sizes?.length ? (
                    <label>
                      {t("尺寸")}
                      <select
                        value={size}
                        onChange={(e) => setSize(e.target.value)}
                      >
                        {caps.sizes.map((v) => (
                          <option key={v}>{v}</option>
                        ))}
                      </select>
                    </label>
                  ) : null}
                  {kind === "image" && caps?.qualities?.length ? (
                    <label>
                      {t("质量")}
                      <select
                        value={quality}
                        onChange={(e) => setQuality(e.target.value)}
                      >
                        {caps.qualities.map((v) => (
                          <option key={v}>{v}</option>
                        ))}
                      </select>
                    </label>
                  ) : null}
                </div>
                {caps?.supports_audio && (
                  <label className="checkbox-label">
                    <input
                      type="checkbox"
                      checked={audio}
                      onChange={(e) => setAudio(e.target.checked)}
                    />
                    {t("生成原生音频")}
                  </label>
                )}
                {caps?.supports_frames && (
                  <div className="parameter-grid">
                    <label>
                      {t("首帧")}
                      <select
                        value={startFrame}
                        onChange={(e) => setStartFrame(e.target.value)}
                      >
                        <option value="">{t("不使用")}</option>
                        {assets
                          .filter((a) => a.kind === "image")
                          .map((a) => (
                            <option key={a.id} value={a.id}>
                              {a.file_name}
                            </option>
                          ))}
                      </select>
                    </label>
                    <label>
                      {t("尾帧")}
                      <select
                        value={endFrame}
                        onChange={(e) => setEndFrame(e.target.value)}
                      >
                        <option value="">{t("不使用")}</option>
                        {assets
                          .filter((a) => a.kind === "image")
                          .map((a) => (
                            <option key={a.id} value={a.id}>
                              {a.file_name}
                            </option>
                          ))}
                      </select>
                    </label>
                  </div>
                )}
              </>
            )}
            {error && (
              <Notice error>
                {error}
                {insufficient && (
                  <button
                    className="text-button"
                    onClick={() => navigate("wallet")}
                  >
                    {t("前往钱包充值")}
                  </button>
                )}
              </Notice>
            )}
          </div>
          <aside className="generation-context">
            <div className="eyebrow">REFERENCE & ESTIMATE</div>
            <p className="field-hint">
              {t(
                "画布连线保留创作关系。本次生成以当前提示词、参考素材和首尾帧选择为准，受理后会保留实际输入快照。",
              )}
            </p>
            {initialSelection.restored && (
              <p className="field-hint">
                {t(
                  "已恢复此节点上次受理的参考和首尾帧选择；修改画布连线后会按新的引用重新带入。",
                )}
              </p>
            )}
            {referenceNotes.length > 0 && (
              <div className="canvas-reference-notes">
                <h3>{t("画布笔记引用")}</h3>
                <p className="field-hint">
                  {t("连线笔记已加入提示词，可在左侧调整。")}
                </p>
                {referenceNotes.map((note) => (
                  <div key={note.nodeId}>
                    <strong>{note.title}</strong>
                    <p>{note.content}</p>
                    <button
                      className="text-button"
                      disabled={busy || prompt.includes(note.content)}
                      onClick={() =>
                        setPrompt((current) =>
                          current
                            ? `${current}\n\n${note.content}`
                            : note.content,
                        )
                      }
                    >
                      {t("加入提示词")}
                    </button>
                  </div>
                ))}
              </div>
            )}
            {referenceIssues.length > 0 && (
              <Notice>
                {t("部分画布引用尚无可用内容，请检查这些节点：")}{" "}
                {referenceIssues.map((issue) => issue.title).join("、")}
              </Notice>
            )}
            <h3>{t("参考素材")}</h3>
            <p className="muted">
              {t("选择已上传到项目的素材。可用类型与数量由当前模型能力决定。")}
            </p>
            {initialReferenceIds.length > 0 && (
              <p className="field-hint">
                {t("已带入本次创作的参考素材，可按模型能力调整。")}
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
            {references.length > 0 && (
              <div className="generation-selected-references">
                {references.map((id) => (
                  <button
                    key={id}
                    className="button subtle small"
                    disabled={busy}
                    onClick={() =>
                      setReferences((current) =>
                        current.filter((value) => value !== id),
                      )
                    }
                  >
                    {assets.find((asset) => asset.id === id)?.file_name ||
                      t("不可用素材")}{" "}
                    ×
                  </button>
                ))}
              </div>
            )}
            {referenceProblems.length > 0 && (
              <Notice error>{referenceProblems.join(" ")}</Notice>
            )}
            {product &&
            assets.filter((a) =>
              a.kind === "image"
                ? caps!.max_images > 0
                : a.kind === "video"
                  ? caps!.max_videos > 0
                  : caps!.max_audios > 0,
            ).length ? (
              <div className="reference-list">
                {assets
                  .filter((a) =>
                    a.kind === "image"
                      ? caps!.max_images > 0
                      : a.kind === "video"
                        ? caps!.max_videos > 0
                        : caps!.max_audios > 0,
                  )
                  .map((a) => (
                    <label key={a.id} className="reference-choice">
                      <input
                        type="checkbox"
                        checked={references.includes(a.id)}
                        onChange={(e) =>
                          setReferences(
                            e.target.checked
                              ? [...references, a.id]
                              : references.filter((id) => id !== a.id),
                          )
                        }
                      />
                      <Icon name={a.kind} />
                      <span>{a.file_name}</span>
                    </label>
                  ))}
              </div>
            ) : (
              <div className="reference-empty">
                <Icon name="image" size={24} />
                <span>
                  {assets.length
                    ? t("此规格暂不支持参考素材")
                    : t("上传参考图后，可在这里选用")}
                </span>
              </div>
            )}
            <div className="generation-summary">
              <div>
                <span>{t("本次生成")}</span>
                <strong>
                  1 {kind === "image" ? t("张图片") : t("条视频")}
                </strong>
              </div>
              <div>
                <span>{t("商品参考价")}</span>
                <strong>
                  {product ? product.credits.toLocaleString() : "—"}{" "}
                  <small>{t("积分")}</small>
                </strong>
              </div>
              <p>
                {t(
                  "服务端报价后确认总价。提交时冻结积分，作品保存完成后结算。",
                )}
              </p>
            </div>
            {(system?.demo || product?.demo) && (
              <Notice>
                {t(
                  "开发演示：使用测试积分与模拟图片，不能作为真实模型验证结果。",
                )}
              </Notice>
            )}
          </aside>
        </div>
      </fieldset>
      {storageError && (
        <Notice error>
          {storageError}
          {!settled && (
            <button
              className="text-button"
              disabled={busy}
              onClick={reloadRecovery}
            >
              {t("重新读取恢复记录")}
            </button>
          )}
        </Notice>
      )}
      {recovery && (
        <Notice>
          {settled
            ? t("提交结果已确认，等待清理本地记录。")
            : t("有一笔尚未确认的提交。")}
          <span className="submission-recovery-id">
            {t("原报价编号")} <code>{recovery.quote_id}</code>
          </span>
          <span className="submission-recovery-id">
            {t("原提交编号")} <code>{recovery.request_key}</code>
          </span>
          <button
            className="text-button"
            disabled={busy || (!!storageError && !settled)}
            onClick={() => void submit(recovery)}
          >
            {settled ? t("重试清理恢复记录") : t("查询 / 重试原提交")}
          </button>
          <button
            className="text-button muted"
            disabled={busy}
            onClick={() => navigate("tasks")}
          >
            {t("前往任务中心核查")}
          </button>
        </Notice>
      )}
      <div className="modal-actions generate-actions">
        <span>
          {quote ? (
            <>
              {t("本次固定报价")}
              <strong>
                {quote.credits}
                {t("积分")}
              </strong>
              <small>
                {t("有效至")}
                {new Date(quote.expires_at).toLocaleTimeString("zh-CN")}
              </small>
            </>
          ) : (
            t("每次生成均需确认报价")
          )}
        </span>
        <button
          className="button primary"
          disabled={
            busy ||
            !recoveryReady ||
            !!storageError ||
            !!settled ||
            !product ||
            !prompt.trim() ||
            !!recovery ||
            referenceProblems.length > 0
          }
          onClick={() => void (quote ? submit() : getQuote())}
        >
          {busy ? <Spinner /> : <Icon name="spark" />}
          {busy
            ? t("正在处理…")
            : quote
              ? t("确认生成 · {0} 积分", quote.credits)
              : t("获取准确报价")}
        </button>
      </div>
    </Modal>
  );
}
