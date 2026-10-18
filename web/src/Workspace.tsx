import { t } from "./i18n";
import { useCallback, useEffect, useRef, useState } from "react";
import { api, ApiError, errorMessage, items } from "./api";
import { useStudio } from "./App";
import { AssetVisual, CanvasEditor } from "./CanvasEditor";
import { Generate } from "./Generate";
import { CandidateCompare } from "./CandidateCompare";
import { AssetOrigin, AssetReferences } from "./AssetReferences";
import { workspaceLocation, type WorkspaceView } from "./asset-references";
import { useAllItems } from "./pagination";
import {
  addAcceptedTask,
  canvasReferenceSelection,
  canvasGenerationDefaults,
  readGenerationDraft,
  reconcileGeneratedNodes,
} from "./creation";
import {
  collectCanvasReferences,
  validateReferenceConnection,
} from "./lib/canvas/canvas-references";
import {
  checkpointCanvasHistory,
  createCanvasHistory,
  resetCanvasHistory,
} from "./lib/canvas/canvas-history";
import {
  CanvasNodeType,
  emptyGraph,
  type CanvasNodeData,
  type GenerationDraft,
  type Graph,
} from "./types/canvas";
import type { Asset, CanvasDocument, Project, Task } from "./types";
import {
  Empty,
  Icon,
  LoadingError,
  Modal,
  Notice,
  Status,
  formatBytes,
  useResource,
} from "./ui";
export function Workspace({
  projectId,
  query,
}: {
  projectId: string;
  query?: string;
}) {
  const { user, navigate, notify, refreshWallet } = useStudio();
  const project = useResource<Project | null>(`/projects/${projectId}`, null);
  const assets = useAllItems<Asset>(`/assets?project_id=${projectId}`);
  const tasks = useAllItems<Task>(`/tasks?project_id=${projectId}`);
  const location = workspaceLocation(query);
  const view = location.view || "canvas";
  const [assetKind, setAssetKind] = useState<"" | Asset["kind"]>("");
  const [canvasComparison, setCanvasComparison] = useState<Asset[] | null>(
    null,
  );
  useEffect(() => {
    if (view !== "canvas") setCanvasComparison(null);
  }, [view]);
  const setView = (next: WorkspaceView) =>
    navigate("project", `${projectId}?view=${next}`);
  const [graph, setGraph] = useState<Graph>(emptyGraph),
    [loaded, setLoaded] = useState(false),
    [loadError, setLoadError] = useState(""),
    [saveState, setSaveState] = useState<
      "saved" | "pending" | "saving" | "failed" | "conflict"
    >("saved"),
    [saveError, setSaveError] = useState(""),
    [draft, setDraft] = useState<{
      revision: number;
      graph: Graph;
    } | null>(null),
    [generator, setGenerator] = useState<{
      node?: CanvasNodeData;
      kind?: "image" | "video";
    } | null>(null),
    [library, setLibrary] = useState(false),
    [queue, setQueue] = useState(false),
    [uploading, setUploading] = useState(false);
  const revision = useRef(0),
    canvasHistory = useRef(createCanvasHistory()),
    saved = useRef(""),
    graphRef = useRef(graph),
    saving = useRef(false),
    needsSaveConfirmation = useRef(false),
    uploadInput = useRef<HTMLInputElement>(null);
  const draftEdit = useRef<{
    nodeId: string;
    graph: Graph;
    time: number;
  } | null>(null);
  const workspaceLive = useRef(true);
  useEffect(() => {
    workspaceLive.current = true;
    return () => {
      workspaceLive.current = false;
    };
  }, []);
  graphRef.current = graph;
  const draftKey = `frame:draft:${user.id}:${projectId}`;
  const load = useCallback(async () => {
    setLoadError("");
    setLoaded(false);
    try {
      const document = await api<CanvasDocument>(
        `/projects/${projectId}/canvas`,
      );
      const next: Graph = {
        nodes: document.graph.nodes || [],
        connections: document.graph.edges || [],
        viewport: document.graph.viewport
          ? {
              x: document.graph.viewport.x,
              y: document.graph.viewport.y,
              k: document.graph.viewport.zoom || 1,
            }
          : emptyGraph().viewport,
      };
      revision.current = document.revision;
      saved.current = JSON.stringify(next);
      needsSaveConfirmation.current = false;
      resetCanvasHistory(canvasHistory.current);
      setGraph(next);
      setSaveState("saved");
      setSaveError("");
      setDraft(null);
      try {
        const local = JSON.parse(localStorage.getItem(draftKey) || "null");
        if (local?.graph && JSON.stringify(local.graph) !== saved.current)
          setDraft(local);
      } catch {
        /* A blocked or invalid draft must not block cloud project loading. */
      }
      setLoaded(true);
    } catch (error) {
      setLoadError(errorMessage(error));
    }
  }, [projectId, draftKey]);
  useEffect(() => {
    void load();
  }, [load]);
  const change = useCallback((next: Graph) => {
    graphRef.current = next;
    setGraph(next);
    setSaveState((current) =>
      current === "conflict" ? current : saving.current ? "saving" : "pending",
    );
  }, []);
  const commitCanvas = useCallback(
    (next: Graph) => {
      if (next === graphRef.current) return;
      checkpointCanvasHistory(canvasHistory.current, graphRef.current);
      change(next);
    },
    [change],
  );
  function updateNodeDraft(nodeId: string, nextDraft: GenerationDraft) {
    const current = graphRef.current;
    const node = current.nodes.find((item) => item.id === nodeId);
    if (
      !node ||
      JSON.stringify(node.metadata?.generationDraft) ===
        JSON.stringify(nextDraft)
    )
      return;
    const now = Date.now();
    const previous = draftEdit.current;
    if (
      !previous ||
      previous.nodeId !== nodeId ||
      previous.graph !== current ||
      now - previous.time > 1200
    )
      checkpointCanvasHistory(canvasHistory.current, current);
    const next = {
      ...current,
      nodes: current.nodes.map((item) =>
        item.id === nodeId
          ? {
              ...item,
              metadata: { ...item.metadata, generationDraft: nextDraft },
            }
          : item,
      ),
    };
    draftEdit.current = { nodeId, graph: next, time: now };
    change(next);
  }
  const save = useCallback(async () => {
    if (!loaded || saving.current || saveState === "conflict") return;
    const snapshot = graphRef.current,
      encoded = JSON.stringify(snapshot);
    if (encoded === saved.current && !needsSaveConfirmation.current) {
      setSaveState("saved");
      return;
    }
    saving.current = true;
    needsSaveConfirmation.current = true;
    setSaveState("saving");
    try {
      const result = await api<CanvasDocument>(
        `/projects/${projectId}/canvas`,
        "PUT",
        {
          graph_schema_version: 1,
          revision: revision.current,
          graph: {
            nodes: snapshot.nodes,
            edges: snapshot.connections,
            viewport: {
              x: snapshot.viewport.x,
              y: snapshot.viewport.y,
              zoom: snapshot.viewport.k,
            },
          },
        },
      );
      revision.current = result.revision;
      saved.current = encoded;
      needsSaveConfirmation.current = false;
      setSaveState(
        JSON.stringify(graphRef.current) === encoded ? "saved" : "pending",
      );
      setSaveError("");
      if (JSON.stringify(graphRef.current) === encoded) {
        try {
          localStorage.removeItem(draftKey);
        } catch {
          /* Browser storage must not invalidate a confirmed cloud receipt. */
        }
      }
    } catch (error) {
      setSaveState(
        error instanceof ApiError && error.status === 409
          ? "conflict"
          : "failed",
      );
      setSaveError(errorMessage(error));
    } finally {
      saving.current = false;
    }
  }, [loaded, saveState, projectId, draftKey]);
  useEffect(() => {
    if (!loaded) return;
    if (
      JSON.stringify(graph) === saved.current &&
      !saving.current &&
      !needsSaveConfirmation.current
    ) {
      // Undo before a save starts returns to the confirmed cloud document.
      // Keep separately offered recovery drafts and unresolved conflicts intact.
      if (saveState === "pending" && !draft) {
        try {
          localStorage.removeItem(draftKey);
        } catch {
          /* Cloud state is still confirmed when browser storage is unavailable. */
        }
        setSaveError("");
        setSaveState("saved");
      }
      return;
    }
    try {
      localStorage.setItem(
        draftKey,
        JSON.stringify({ revision: revision.current, graph }),
      );
    } catch {
      /* Cloud save remains available if local storage is full. */
    }
    if (
      saveState === "conflict" ||
      saveState === "failed" ||
      saveState === "saving"
    )
      return;
    const timer = window.setTimeout(() => void save(), 900);
    return () => clearTimeout(timer);
  }, [graph, loaded, draftKey, saveState, save, draft]);
  useEffect(() => {
    const handler = (event: BeforeUnloadEvent) => {
      if (
        needsSaveConfirmation.current ||
        JSON.stringify(graphRef.current) !== saved.current
      ) {
        event.preventDefault();
      }
    };
    window.addEventListener("beforeunload", handler);
    return () => window.removeEventListener("beforeunload", handler);
  }, []);
  useEffect(() => {
    const list = items(tasks.data);
    const known = new Set(assets.data.items.map((asset) => asset.id));
    const pending = list.filter(
      (task) =>
        !["completed", "failed", "canceled"].includes(task.execution_status) ||
        (task.execution_status === "completed" &&
          (!["ready", "error"].includes(task.delivery_status) ||
            (task.delivery_status === "ready" &&
              task.asset_ids.some((id) => !known.has(id))))),
    );
    // A fast task can already be ready on the first refresh, before its asset
    // enters the loaded library. Fetch that asset even without a running task.
    if (!pending.length) return;
    if (tasks.loading || assets.loading) return;
    let live = true;
    const timer = window.setTimeout(async () => {
      try {
        const updated: Task[] = [];
        for (let index = 0; index < pending.length; index += 8) {
          updated.push(
            ...(await Promise.all(
              pending
                .slice(index, index + 8)
                .map((task) => api<Task>(`/tasks/${task.id}`)),
            )),
          );
          if (!live) return;
        }
        const missing = [
          ...new Set(updated.flatMap((task) => task.asset_ids)),
        ].filter((id) => !known.has(id));
        const arrived: Asset[] = [];
        for (let index = 0; index < missing.length; index += 8) {
          arrived.push(
            ...(await Promise.all(
              missing
                .slice(index, index + 8)
                .map((id) => api<Asset>(`/assets/${id}`)),
            )),
          );
          if (!live) return;
        }
        if (!live) return;
        if (arrived.length)
          assets.setData((current) => ({
            items: [
              ...arrived,
              ...current.items.filter((asset) => !missing.includes(asset.id)),
            ],
          }));
        tasks.setData((current) => ({
          items: current.items.map(
            (task) => updated.find((item) => item.id === task.id) || task,
          ),
        }));
        refreshWallet();
      } catch {
        if (live) tasks.refresh();
      }
    }, 3500);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [tasks.data, assets.data, tasks.loading, assets.loading]);
  async function duplicateDraft() {
    try {
      const current = graphRef.current;
      const copy = await api<Project>(
        `/projects/${projectId}/duplicate`,
        "POST",
        {
          name: t("{0} · 冲突副本", project.data?.name || t("项目")),
          graph: {
            nodes: current.nodes,
            edges: current.connections,
            viewport: {
              x: current.viewport.x,
              y: current.viewport.y,
              zoom: current.viewport.k,
            },
          },
        },
      );
      notify(t("画布草稿与素材引用已另存为项目副本"));
      navigate("project", copy.id);
    } catch (reason) {
      notify(errorMessage(reason), true);
    }
  }
  function insert(asset: Asset) {
    const current = graphRef.current,
      node: CanvasNodeData = {
        id: crypto.randomUUID(),
        type:
          asset.kind === "video"
            ? CanvasNodeType.Video
            : asset.kind === "audio"
              ? CanvasNodeType.Audio
              : CanvasNodeType.Image,
        title: asset.file_name,
        position: {
          x:
            (160 - current.viewport.x) / current.viewport.k +
            (current.nodes.length % 5) * 36,
          y:
            (130 - current.viewport.y) / current.viewport.k +
            (current.nodes.length % 5) * 30,
        },
        width: 320,
        height: 250,
        metadata: { assetId: asset.id },
      };
    while (
      current.nodes.some(
        (existing) =>
          existing.type !== CanvasNodeType.Group &&
          node.position.x < existing.position.x + existing.width + 20 &&
          node.position.x + node.width + 20 > existing.position.x &&
          node.position.y < existing.position.y + existing.height + 20 &&
          node.position.y + node.height + 20 > existing.position.y,
      )
    )
      node.position.y += node.height + 32;
    commitCanvas({ ...current, nodes: [...current.nodes, node] });
    setView("canvas");
    notify(t("素材已加入画布"));
  }
  async function upload(files: FileList | null) {
    if (!files?.length) return [];
    setUploading(true);
    const uploaded: Asset[] = [];
    for (const file of Array.from(files)) {
      const form = new FormData();
      form.append("file", file);
      form.append("project_id", projectId);
      try {
        uploaded.push(await api<Asset>("/assets", "POST", form));
      } catch (reason) {
        notify(`${file.name}：${errorMessage(reason)}`, true);
      }
    }
    setUploading(false);
    if (uploaded.length) {
      assets.setData((current) => ({
        ...current,
        items: [...uploaded, ...current.items],
      }));
      notify(t("已上传 {0} 个素材", uploaded.length));
    }
    if (uploadInput.current) uploadInput.current.value = "";
    return uploaded;
  }
  async function uploadNodeReferences(nodeId: string, files: FileList) {
    const uploaded = await upload(files);
    if (!workspaceLive.current || !uploaded.length) return uploaded;
    const node = graphRef.current.nodes.find((item) => item.id === nodeId);
    if (node) {
      const linked = collectCanvasReferences(graphRef.current, nodeId).assetIds;
      const defaults = canvasGenerationDefaults(node, undefined, linked);
      const selection = canvasReferenceSelection(node, defaults.referenceIds);
      const currentDraft = readGenerationDraft(
        node.metadata?.generationDraft,
      ) || {
        kind: defaults.kind,
        productId: node.metadata?.productId || "",
        prompt: defaults.prompt,
        ratio: "",
        duration: 0,
        resolution: "",
        size: "",
        quality: "",
        audio: false,
        referenceIds: selection.referenceIds,
        startFrameId: selection.startFrameId,
        endFrameId: selection.endFrameId,
        connectedAssetIds: defaults.referenceIds,
      };
      updateNodeDraft(nodeId, {
        ...currentDraft,
        referenceIds: [
          ...new Set([
            ...currentDraft.referenceIds,
            ...uploaded.map((asset) => asset.id),
          ]),
        ],
      });
    }
    return uploaded;
  }
  async function setCover(asset: Asset) {
    if (!project.data) return;
    try {
      const updated = await api<Project>(`/projects/${projectId}`, "PATCH", {
        revision: project.data.revision,
        cover_asset_id: asset.id,
      });
      project.setData(updated);
      notify(t("项目封面已更新"));
    } catch (reason) {
      project.refresh();
      throw reason;
    }
  }
  const taskList = items(tasks.data),
    activeTasks = taskList.filter(
      (t) => !["completed", "failed", "canceled"].includes(t.execution_status),
    );
  // Task delivery is a live view, not a canvas edit. Two viewers must not
  // contend on canvas revisions just because the same worker finishes a task.
  const canvasGraph = reconcileGeneratedNodes(
    graph,
    taskList,
    assets.data.items,
  );
  const projectAssetList = assetKind
    ? assets.data.items.filter((asset) => asset.kind === assetKind)
    : assets.data.items;
  function referencesFor(node?: CanvasNodeData) {
    const linked = node
      ? collectCanvasReferences(canvasGraph, node.id)
      : { assetIds: [], notes: [], issues: [] };
    return {
      ...linked,
      assetIds: [
        ...new Set(
          [
            ...linked.assetIds,
            ...(!node?.metadata?.generationTaskId && node?.metadata?.assetId
              ? [node.metadata.assetId]
              : []),
          ].filter((id): id is string => Boolean(id)),
        ),
      ],
    };
  }
  const canvasReferences = referencesFor(generator?.node);
  function acceptTask(task: Task, sourceId?: string) {
    if (!workspaceLive.current) return false;
    const current = graphRef.current;
    const liveGraph = reconcileGeneratedNodes(
      current,
      taskList,
      assets.data.items,
    );
    const source = liveGraph.nodes.find((item) => item.id === sourceId);
    // Accepted media becomes the output in place; its old content is no longer
    // a connected source. Explicitly selected input assets stay in the draft.
    const connected = source
      ? collectCanvasReferences(liveGraph, source.id).assetIds
      : undefined;
    commitCanvas(
      addAcceptedTask(
        current,
        task,
        task.kind === "video" ? t("视频创作") : t("图片创作"),
        sourceId,
        connected,
      ),
    );
    tasks.refresh();
    refreshWallet();
    notify(t("生成任务已受理，离开页面后仍会继续执行"));
    return true;
  }
  function pickReference(targetId: string, assetId: string) {
    const current = graphRef.current;
    const liveGraph = reconcileGeneratedNodes(
      current,
      taskList,
      assets.data.items,
    );
    const target = liveGraph.nodes.find((item) => item.id === targetId);
    if (!target) return;
    const source = liveGraph.nodes.find(
      (item) => item.id !== targetId && item.metadata?.assetId === assetId,
    );
    if (!source) return;
    const invalid = validateReferenceConnection(liveGraph, source.id, targetId);
    if (invalid && invalid !== "duplicate") return;
    const links = collectCanvasReferences(liveGraph, targetId);
    const connectedIds = [
      ...new Set([
        ...links.assetIds,
        ...(!target.metadata?.generationTaskId && target.metadata?.assetId
          ? [target.metadata.assetId]
          : []),
      ]),
    ];
    const defaults = canvasGenerationDefaults(target);
    const selection = canvasReferenceSelection(target, connectedIds);
    const previous = readGenerationDraft(target.metadata?.generationDraft);
    const nextDraft: GenerationDraft = {
      kind: defaults.kind,
      productId: target.metadata?.productId || "",
      prompt: defaults.prompt,
      ratio: "",
      duration: 0,
      resolution: "",
      size: "",
      quality: "",
      audio: false,
      ...previous,
      referenceIds: [...new Set([...selection.referenceIds, assetId])].filter(
        (id) => id !== selection.startFrameId && id !== selection.endFrameId,
      ),
      startFrameId: selection.startFrameId,
      endFrameId: selection.endFrameId,
      connectedAssetIds: [
        ...new Set([
          ...connectedIds,
          ...(target.metadata?.generationSourceNodeId === source.id
            ? []
            : [assetId]),
        ]),
      ],
    };
    const connection = !invalid
      ? { id: crypto.randomUUID(), fromNodeId: source.id, toNodeId: targetId }
      : undefined;
    commitCanvas({
      ...current,
      nodes: current.nodes.map((item) =>
        item.id === targetId
          ? {
              ...item,
              metadata: { ...item.metadata, generationDraft: nextDraft },
            }
          : item,
      ),
      connections: connection
        ? [...current.connections, connection]
        : current.connections,
    });
  }
  return (
    <div className="workspace">
      <div className="workspace-toolbar">
        <div className="project-heading">
          <button
            className="icon-button"
            onClick={() => navigate("projects")}
            aria-label={t("返回项目")}
          >
            <Icon name="back" />
          </button>
          <div>
            <h1>{project.data?.name || t("创作工作台")}</h1>
            <span className={`save-indicator save-${saveState}`}>
              <i />
              {saveState === "saved"
                ? t("所有更改已保存")
                : saveState === "pending"
                  ? t("有待保存的更改")
                  : saveState === "saving"
                    ? t("正在保存到云端…")
                    : saveState === "conflict"
                      ? t("发现版本冲突")
                      : t("保存失败，草稿已保留")}
            </span>
          </div>
        </div>
        <div className="view-tabs">
          <button
            className={view === "canvas" ? "active" : ""}
            onClick={() => setView("canvas")}
          >
            <Icon name="canvas" size={16} />
            {t("画布")}
          </button>
          <button
            className={view === "assets" ? "active" : ""}
            onClick={() => setView("assets")}
          >
            <Icon name="image" size={16} />
            {t("素材")}
            <small>{assets.data.items.length}</small>
          </button>
        </div>
        <div className="workspace-actions">
          <button
            className={`button subtle small ${queue ? "selected" : ""}`}
            onClick={() => setQueue(!queue)}
          >
            <Icon name="tasks" size={16} />
            <span className="hide-small">{t("任务")}</span>
            {activeTasks.length > 0 && (
              <span className="count-dot">{activeTasks.length}</span>
            )}
          </button>
        </div>
      </div>
      {(project.error || loadError) && (
        <LoadingError
          loading={false}
          error={project.error || loadError}
          retry={() => {
            project.refresh();
            void load();
          }}
        />
      )}
      <input
        ref={uploadInput}
        type="file"
        accept="image/*,video/*,audio/*"
        multiple
        hidden
        onChange={(e) => void upload(e.target.files)}
      />
      {draft && (
        <Modal title={t("恢复本地草稿")} onClose={() => navigate("projects")}>
          <Notice>
            {t("发现此设备上的未保存草稿（版本")}
            {draft.revision}）。
          </Notice>
          <p className="field-hint">
            {t(
              "继续编辑前，请选择画布版本。关闭窗口会返回项目空间，草稿继续保留。",
            )}
          </p>
          <div className="modal-actions">
            <button
              className="button primary"
              onClick={() => {
                resetCanvasHistory(canvasHistory.current);
                change(draft.graph);
                if (draft.revision !== revision.current) {
                  revision.current = draft.revision;
                  setSaveState("conflict");
                }
                setDraft(null);
              }}
            >
              {t("恢复本地草稿")}
            </button>
            <button
              className="button secondary"
              onClick={() => {
                localStorage.removeItem(draftKey);
                setDraft(null);
              }}
            >
              {t("使用云端版本")}
            </button>
          </div>
        </Modal>
      )}
      {saveState === "conflict" && (
        <div className="workspace-notice">
          <Notice error>
            {t("此项目已在另一设备更新。你的草稿已保留，尚未覆盖云端。")}
            <button
              className="text-button"
              onClick={() => {
                localStorage.removeItem(draftKey);
                setDraft(null);
                void load();
              }}
            >
              {t("重载云端版本")}
            </button>
            <button
              className="text-button"
              onClick={() => void duplicateDraft()}
            >
              {t("另存画布副本")}
            </button>
          </Notice>
        </div>
      )}
      {saveState === "failed" && (
        <div className="workspace-notice">
          <Notice error>
            {saveError}
            <button className="text-button" onClick={() => void save()}>
              {t("重新保存")}
            </button>
          </Notice>
        </div>
      )}
      {(assets.loading || tasks.loading || assets.error || tasks.error) && (
        <div className="workspace-notice">
          <LoadingError
            loading={assets.loading || tasks.loading}
            error={assets.error || tasks.error}
            retry={() => {
              assets.refresh();
              tasks.refresh();
            }}
          />
        </div>
      )}
      {loaded && !project.error && !draft && (
        <div className="workspace-body">
          {view === "canvas" && (
            <CanvasEditor
              graph={canvasGraph}
              history={canvasHistory.current}
              assets={assets.data.items}
              onChange={change}
              onGenerate={(node, kind) => setGenerator({ node, kind })}
              onShowTask={(taskId) => navigate("tasks", taskId)}
              onAssetLibrary={() => setLibrary(!library)}
              onCompare={setCanvasComparison}
              onPickReference={pickReference}
              renderComposer={(node, controls) => {
                const references = referencesFor(node);
                return (
                  <Generate
                    key={node.id}
                    presentation="inline"
                    projectId={projectId}
                    assets={assets.data.items}
                    node={node}
                    initialReferenceIds={references.assetIds}
                    referenceNotes={references.notes}
                    referenceIssues={references.issues}
                    onClose={controls.onClose}
                    onDraftChange={(nextDraft) =>
                      updateNodeDraft(node.id, nextDraft)
                    }
                    onPickReferences={controls.onPickReferences}
                    onUploadReferences={(files) =>
                      uploadNodeReferences(node.id, files)
                    }
                    onCreated={(task, recovered, sourceId) =>
                      acceptTask(
                        task,
                        sourceId || (recovered ? undefined : node.id),
                      )
                    }
                  />
                );
              }}
            />
          )}
          {view === "assets" && (
            <div className="workspace-assets">
              <div className="section-heading">
                <div>
                  <h2>{t("项目素材")}</h2>
                  <p>
                    {t(
                      "参考素材与生成作品在这里归档，可随时加入画布继续创作。",
                    )}
                  </p>
                </div>
                <button
                  className="button secondary"
                  disabled={uploading}
                  onClick={() => uploadInput.current?.click()}
                >
                  <Icon name="upload" size={16} />
                  {uploading ? t("上传中…") : t("上传素材")}
                </button>
              </div>
              <div className="filter-bar">
                <div
                  className="segmented"
                  role="group"
                  aria-label={t("按素材类型筛选")}
                >
                  {[
                    { value: "", label: t("全部") },
                    { value: "image", label: t("图片") },
                    { value: "video", label: t("视频") },
                    { value: "audio", label: t("音频") },
                  ].map(({ value, label }) => (
                    <button
                      key={value}
                      type="button"
                      className={assetKind === value ? "active" : ""}
                      aria-pressed={assetKind === value}
                      onClick={() => setAssetKind(value as "" | Asset["kind"])}
                    >
                      {label}
                    </button>
                  ))}
                </div>
              </div>
              {!assets.loading &&
              !assets.error &&
              assets.data.items.length > 0 &&
              !projectAssetList.length ? (
                <Empty
                  icon={assetKind || "image"}
                  title={t("该类型暂无素材")}
                  description={t(
                    "项目中还没有这种类型的素材。切换到“全部”可查看其他素材。",
                  )}
                  action={
                    <button
                      className="button secondary"
                      onClick={() => setAssetKind("")}
                    >
                      {t("显示全部素材")}
                    </button>
                  }
                />
              ) : (
                <AssetGrid
                  assets={projectAssetList}
                  onInsert={insert}
                  onChanged={assets.refresh}
                  onCover={setCover}
                  loading={assets.loading}
                  error={assets.error}
                />
              )}
            </div>
          )}
          {library && view === "canvas" && (
            <aside className="asset-drawer">
              <div className="inspector-heading">
                <span>
                  {t("项目素材 ·")}
                  {assets.data.items.length}
                </span>
                <button
                  className="icon-button"
                  onClick={() => setLibrary(false)}
                  aria-label={t("关闭素材库")}
                >
                  <Icon name="close" size={16} />
                </button>
              </div>
              <button
                className="button secondary full"
                disabled={uploading}
                onClick={() => uploadInput.current?.click()}
              >
                <Icon name="upload" size={15} />
                {uploading ? t("正在上传…") : t("上传参考素材")}
              </button>
              <p className="field-hint">{t("拖入画布，或点击素材上的 +。")}</p>
              <AssetGrid
                assets={assets.data.items}
                onInsert={insert}
                onChanged={assets.refresh}
                onCover={setCover}
                compact
                loading={assets.loading}
                error={assets.error}
              />
            </aside>
          )}
        </div>
      )}
      {!loaded && !loadError && <LoadingError loading error="" />}
      {queue && (
        <div className="task-strip">
          <div className="task-strip-title">
            <strong>{t("项目任务")}</strong>
            <span>
              {activeTasks.length}
              {t("个进行中")}
            </span>
            <button className="text-button" onClick={tasks.refresh}>
              <Icon name="refresh" size={14} />
              {t("刷新")}
            </button>
            <button
              className="icon-button"
              onClick={() => setQueue(false)}
              aria-label={t("收起任务")}
            >
              <Icon name="close" size={14} />
            </button>
          </div>
          {tasks.error && <Notice error>{tasks.error}</Notice>}
          {!taskList.length ? (
            <p className="muted task-strip-empty">
              {t("还没有生成任务。每次生成会在这里保留输入、费用和交付状态。")}
            </p>
          ) : (
            <div className="task-strip-list">
              {taskList.slice(0, 8).map((task) => (
                <button
                  key={task.id}
                  className="queue-task"
                  onClick={() => navigate("tasks", task.id)}
                >
                  <Icon name={task.kind === "video" ? "video" : "image"} />
                  <span>
                    <strong>{task.prompt.slice(0, 40) || task.model}</strong>
                    <small>
                      {task.credits}
                      {t("积分 ·")}
                      {task.id.slice(-8)}
                    </small>
                  </span>
                  <Status
                    value={
                      task.delivery_status === "ready"
                        ? "ready"
                        : task.execution_status
                    }
                  />
                </button>
              ))}
            </div>
          )}
        </div>
      )}
      {loaded && !draft && view === "canvas" && canvasComparison && (
        <CandidateCompare
          assets={canvasComparison}
          onClose={() => setCanvasComparison(null)}
        />
      )}
      {loaded && !draft && generator && (
        <Generate
          projectId={projectId}
          assets={assets.data.items}
          node={generator.node}
          initialKind={generator.kind}
          initialReferenceIds={canvasReferences.assetIds}
          referenceNotes={canvasReferences.notes}
          referenceIssues={canvasReferences.issues}
          onClose={() => setGenerator(null)}
          onCreated={(task, recovered, sourceId) => {
            if (
              !acceptTask(
                task,
                sourceId || (recovered ? undefined : generator.node?.id),
              )
            )
              return false;
            setGenerator(null);
            return true;
          }}
        />
      )}
    </div>
  );
}
export function AssetGrid({
  assets,
  onInsert,
  onChanged,
  onCover,
  loading = false,
  error = "",
  compact = false,
}: {
  assets: Asset[];
  onInsert?: (asset: Asset) => void;
  onChanged?: () => void;
  onCover?: (asset: Asset) => Promise<void>;
  loading?: boolean;
  error?: string;
  compact?: boolean;
}) {
  const [preview, setPreview] = useState<Asset | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false),
    [busy, setBusy] = useState(false),
    [actionError, setActionError] = useState("");
  const { notify, navigate } = useStudio();
  function closePreview() {
    setPreview(null);
    setConfirmDelete(false);
    setActionError("");
  }
  function followReference(page: string, id?: string) {
    closePreview();
    navigate(page, id);
  }
  async function act(action: "cover" | "delete") {
    if (!preview) return;
    setBusy(true);
    setActionError("");
    try {
      if (action === "cover") await onCover?.(preview);
      else {
        await api(`/assets/${preview.id}`, "DELETE", {});
        onChanged?.();
        notify(t("素材已删除"));
      }
      closePreview();
    } catch (reason) {
      setActionError(errorMessage(reason));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <LoadingError loading={loading} error={error} />
      {!loading &&
        !error &&
        (assets.length ? (
          <div className={`asset-grid ${compact ? "asset-grid-compact" : ""}`}>
            {assets.map((asset) => (
              <article
                className="asset-card"
                key={asset.id}
                draggable
                onDragStart={(event) => {
                  event.dataTransfer.setData(
                    "application/frame-asset",
                    asset.id,
                  );
                  event.dataTransfer.effectAllowed = "copy";
                }}
              >
                <button
                  className="asset-thumbnail"
                  onClick={() => setPreview(asset)}
                >
                  <AssetVisual asset={asset} />
                  <span className="asset-kind">
                    <Icon name={asset.kind} size={12} />
                    {asset.kind === "image"
                      ? t("图片")
                      : asset.kind === "video"
                        ? t("视频")
                        : t("音频")}
                  </span>
                </button>
                <div className="asset-card-info">
                  <strong title={asset.file_name}>{asset.file_name}</strong>
                  <AssetOrigin asset={asset} onNavigate={followReference} />
                  <small>
                    {asset.width > 0
                      ? `${asset.width} × ${asset.height} · `
                      : ""}
                    {formatBytes(asset.size_bytes)}
                  </small>
                  <div>
                    {onInsert && (
                      <button
                        className="text-button"
                        onClick={() => onInsert(asset)}
                      >
                        <Icon name="plus" size={13} />
                        {t("加入画布")}
                      </button>
                    )}
                    <a
                      href={
                        asset.download_url ||
                        `/api/v1/assets/${asset.id}/download?disposition=attachment`
                      }
                      className="icon-button"
                      title={t("下载素材")}
                    >
                      <Icon name="download" size={14} />
                    </a>
                  </div>
                </div>
              </article>
            ))}
          </div>
        ) : (
          <Empty
            icon="image"
            title={t("素材会在这里汇集")}
            description={t(
              "上传参考文件，或完成第一次生成。所有版本都会留在项目中。",
            )}
          />
        ))}
      {preview && (
        <Modal title={preview.file_name} onClose={closePreview} wide>
          <div className="media-preview">
            <AssetVisual asset={preview} controls />
          </div>
          <div className="asset-preview-origin">
            <AssetOrigin asset={preview} onNavigate={followReference} />
          </div>
          <AssetReferences
            key={preview.id}
            assetId={preview.id}
            onNavigate={followReference}
          />
          {actionError && <Notice error>{actionError}</Notice>}
          {confirmDelete && (
            <Notice error>
              {t(
                "确认删除此素材？已被画布、封面、任务或历史记录引用的素材将无法删除。",
              )}
            </Notice>
          )}
          <div className="modal-actions">
            {onChanged && (
              <button
                className="button secondary"
                disabled={busy}
                onClick={() =>
                  confirmDelete ? void act("delete") : setConfirmDelete(true)
                }
              >
                {confirmDelete ? t("确认删除素材") : t("删除素材")}
              </button>
            )}
            {confirmDelete && (
              <button
                className="button secondary"
                disabled={busy}
                onClick={() => setConfirmDelete(false)}
              >
                {t("取消")}
              </button>
            )}
            {onCover && preview.kind === "image" && (
              <button
                className="button secondary"
                disabled={busy}
                onClick={() => void act("cover")}
              >
                {t("设为项目封面")}
              </button>
            )}
            {onInsert && (
              <button
                className="button secondary"
                onClick={() => {
                  onInsert(preview);
                  setPreview(null);
                }}
              >
                <Icon name="plus" />
                {t("加入画布")}
              </button>
            )}
            <a
              className="button primary"
              href={
                preview.download_url ||
                `/api/v1/assets/${preview.id}/download?disposition=attachment`
              }
            >
              <Icon name="download" />
              {t("下载原文件")}
            </a>
          </div>
        </Modal>
      )}
    </>
  );
}
