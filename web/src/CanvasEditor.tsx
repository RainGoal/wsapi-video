import { t } from "./i18n";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { InfiniteCanvas } from "./components/canvas/infinite-canvas";
import { CanvasNodePlaceholder } from "./components/canvas/canvas-node-placeholder";
import { CanvasNodeHoverToolbar } from "./components/canvas/canvas-node-hover-toolbar";
import { CanvasAppearance } from "./components/canvas/canvas-appearance";
import { CanvasViewControls } from "./components/canvas/canvas-view-controls";
import { CanvasGroupLabel } from "./components/canvas/canvas-group-label";
import { placeCanvasComposer } from "./lib/canvas/canvas-overlay-geometry";
import { CANVAS_STACKED_CONTROLS_WIDTH } from "./lib/canvas/canvas-view-geometry";
import { useThemeStore } from "./stores/use-theme-store";
import {
  ConnectionCreateMenu,
  NodeCreateMenu,
  type PendingConnectionCreate,
} from "./components/canvas/canvas-create-menus";
import {
  ActiveConnectionPath,
  ConnectionPath,
} from "./components/canvas/canvas-connections";
import {
  canOutputReference,
  canReceiveReference,
  validateReferenceConnection,
} from "./lib/canvas/canvas-references";
import {
  duplicateCanvasSelection,
  removeCanvasSelection,
} from "./lib/canvas/canvas-editing";
import {
  checkpointCanvasHistory,
  redoCanvasHistory,
  undoCanvasHistory,
  type CanvasHistory,
} from "./lib/canvas/canvas-history";
import "./canvas-interactions.css";
import "./canvas-node-composer-overlay.css";
import "./canvas-appearance.css";
import {
  applyGroupSelection,
  applyUngroupSelection,
  collectGroupMemberNodes,
  canGroupSelectedNodes,
  canUngroupSelectedNodes,
  findContainingGroupId,
  findGroupDropTarget,
  snapNodesIntoGroup,
  getGroupWrapRect,
  nodeBounds,
} from "./lib/canvas/canvas-node-geometry";
import {
  CanvasNodeType,
  type CanvasNodeData,
  type ConnectionHandle,
  type Graph,
  type Position,
} from "./types/canvas";
import type { Asset } from "./types";
import { Icon, Modal } from "./ui";
type Props = {
  graph: Graph;
  history: CanvasHistory;
  assets: Asset[];
  onChange: (graph: Graph) => void;
  onGenerate: (node?: CanvasNodeData, kind?: "image" | "video") => void;
  onAssetLibrary: () => void;
  onShowTask?: (taskId: string) => void;
  onCompare?: (assets: Asset[]) => void;
  renderComposer?: (
    node: CanvasNodeData,
    controls: { onPickReferences: () => void; onClose: () => void },
  ) => React.ReactNode;
  onPickReference?: (targetNodeId: string, assetId: string) => void;
};
export function CanvasEditor({
  graph,
  history,
  assets,
  onChange,
  onAssetLibrary,
  onShowTask,
  onCompare,
  renderComposer,
  onPickReference,
}: Props) {
  const layout = useRef<HTMLDivElement>(null),
    container = useRef<HTMLDivElement>(null),
    graphRef = useRef(graph);
  graphRef.current = graph;
  const theme = useThemeStore((state) => state.theme);
  const [selected, setSelected] = useState<string[]>([]),
    [connection, setConnection] = useState<ConnectionHandle | null>(null),
    [connectionCursor, setConnectionCursor] = useState<Position>({
      x: 0,
      y: 0,
    }),
    [connectionTarget, setConnectionTarget] = useState<string | null>(null),
    [connectionMessage, setConnectionMessage] = useState(""),
    [help, setHelp] = useState(false),
    [isNodeDragging, setIsNodeDragging] = useState(false),
    [dropTargetGroupId, setDropTargetGroupId] = useState<string | null>(null),
    [isResizing, setIsResizing] = useState(false),
    [selectedEdge, setSelectedEdge] = useState<string | null>(null),
    [box, setBox] = useState<{
      start: Position;
      end: Position;
    } | null>(null),
    [preview, setPreview] = useState<Asset | null>(null),
    [composerNodeId, setComposerNodeId] = useState<string | null>(null),
    [pickingReferenceFor, setPickingReferenceFor] = useState<string | null>(
      null,
    ),
    [createMenu, setCreateMenu] = useState<{
      position: Position;
      connection?: ConnectionHandle;
    } | null>(null),
    [stageSize, setStageSize] = useState({ width: 960, height: 640 }),
    [, setHistoryVersion] = useState(0);
  const drag = useRef<{
    pointerId: number;
    start: Position;
    graph: Graph;
    ids: string[];
    moved: boolean;
    touch: boolean;
    selection: string[];
    selectionBefore: string[];
    shiftKey: boolean;
    nodeId: string;
  } | null>(null);
  const resize = useRef<{
    pointerId: number;
    nodeId: string;
    start: Position;
    graph: Graph;
    moved: boolean;
  } | null>(null);
  const connectionRef = useRef(connection);
  const linkDrag = useRef<{
    pointerId: number;
    start: Position;
    moved: boolean;
  } | null>(null);
  connectionRef.current = connection;
  const closeCreateMenu = useCallback(() => setCreateMenu(null), []);
  useEffect(() => {
    const stage = container.current;
    if (!stage) return;
    const observer = new ResizeObserver(() =>
      setStageSize({ width: stage.clientWidth, height: stage.clientHeight }),
    );
    observer.observe(stage);
    return () => observer.disconnect();
  }, []);
  const commit = useCallback(
    (next: Graph, snapshot = true) => {
      if (snapshot) checkpointCanvasHistory(history, graphRef.current);
      graphRef.current = next;
      onChange(next);
      setHistoryVersion((v) => v + 1);
    },
    [history, onChange],
  );
  const undo = useCallback(() => {
    const previous = undoCanvasHistory(history, graphRef.current);
    if (!previous) return;
    graphRef.current = previous;
    onChange(previous);
    setHistoryVersion((v) => v + 1);
  }, [history, onChange]);
  const redo = useCallback(() => {
    const next = redoCanvasHistory(history, graphRef.current);
    if (!next) return;
    graphRef.current = next;
    onChange(next);
    setHistoryVersion((v) => v + 1);
  }, [history, onChange]);
  const remove = useCallback(() => {
    if (!selected.length && !selectedEdge) return;
    commit(removeCanvasSelection(graphRef.current, selected, selectedEdge));
    setSelected([]);
    setSelectedEdge(null);
  }, [commit, selected, selectedEdge]);
  const duplicate = useCallback(() => {
    if (!selected.length) return;
    const result = duplicateCanvasSelection(graphRef.current, selected);
    commit(result.graph);
    setSelected(result.selected);
  }, [selected, commit]);
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement;
      if (!layout.current?.contains(target)) return;
      if (event.key === "Escape") {
        if (pickingReferenceFor) {
          setPickingReferenceFor(null);
          return;
        }
        setCreateMenu(null);
        setComposerNodeId(null);
      }
      if (
        target.closest(
          "input,textarea,select,[contenteditable=true],[role=dialog]",
        )
      )
        return;
      const modifier = event.ctrlKey || event.metaKey;
      if (modifier && event.key.toLowerCase() === "z") {
        event.preventDefault();
        event.shiftKey ? redo() : undo();
      } else if (modifier && event.key.toLowerCase() === "y") {
        event.preventDefault();
        redo();
      } else if (modifier && event.key.toLowerCase() === "d") {
        event.preventDefault();
        duplicate();
      } else if (modifier && event.key.toLowerCase() === "a") {
        event.preventDefault();
        setSelected(graphRef.current.nodes.map((n) => n.id));
      } else if (modifier && event.key.toLowerCase() === "g") {
        event.preventDefault();
        event.shiftKey ? ungroup() : group();
      } else if (event.key === "Delete" || event.key === "Backspace") {
        event.preventDefault();
        remove();
      } else if (event.key === "Escape") {
        setSelected([]);
        setConnection(null);
        setConnectionTarget(null);
        connectionRef.current = null;
        linkDrag.current = null;
        setConnectionMessage("");
        setSelectedEdge(null);
      }
    };
    window.addEventListener("keydown", handler);
    return () => {
      window.removeEventListener("keydown", handler);
    };
  }, [undo, redo, duplicate, remove, pickingReferenceFor, selected]);
  const world = useCallback((x: number, y: number) => {
    const bounds = container.current!.getBoundingClientRect(),
      v = graphRef.current.viewport;
    return {
      x: (x - bounds.left - v.x) / v.k,
      y: (y - bounds.top - v.y) / v.k,
    };
  }, []);
  const cancelConnection = useCallback(() => {
    connectionRef.current = null;
    linkDrag.current = null;
    setConnection(null);
    setConnectionTarget(null);
  }, []);
  const finishConnection = useCallback(
    (nodeId: string) => {
      const current = connectionRef.current;
      if (!current) return;
      const fromNodeId =
        current.handleType === "source" ? current.nodeId : nodeId;
      const toNodeId =
        current.handleType === "target" ? current.nodeId : nodeId;
      const reason = validateReferenceConnection(
        graphRef.current,
        fromNodeId,
        toNodeId,
      );
      if (reason) {
        setConnectionMessage(
          reason === "duplicate"
            ? t("这两个节点已经建立引用")
            : reason === "self"
              ? t("请选择另一个节点")
              : t("请把素材、笔记或分组连接到创作节点的参考输入"),
        );
        return;
      }
      commit({
        ...graphRef.current,
        connections: [
          ...graphRef.current.connections,
          {
            id: crypto.randomUUID(),
            fromNodeId,
            toNodeId,
          },
        ],
      });
      setConnectionMessage(t("引用已连接，生成前仍需确认参数与报价"));
      cancelConnection();
    },
    [cancelConnection, commit],
  );
  function startConnection(
    event: React.PointerEvent<HTMLButtonElement>,
    node: CanvasNodeData,
    handleType: ConnectionHandle["handleType"],
  ) {
    if (event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    event.currentTarget.focus({ preventScroll: true });
    setComposerNodeId(null);
    setCreateMenu(null);
    setPickingReferenceFor(null);
    if (connectionRef.current) {
      if (connectionRef.current.nodeId === node.id) cancelConnection();
      else finishConnection(node.id);
      return;
    }
    const current = { nodeId: node.id, handleType };
    connectionRef.current = current;
    setConnection(current);
    setConnectionCursor(world(event.clientX, event.clientY));
    setConnectionMessage("");
    setSelectedEdge(null);
    linkDrag.current = {
      pointerId: event.pointerId,
      start: { x: event.clientX, y: event.clientY },
      moved: false,
    };
  }
  function keyboardConnection(
    node: CanvasNodeData,
    handleType: ConnectionHandle["handleType"],
  ) {
    if (connectionRef.current) finishConnection(node.id);
    else {
      const current = { nodeId: node.id, handleType };
      connectionRef.current = current;
      setConnection(current);
      setConnectionCursor({
        x: node.position.x + (handleType === "source" ? node.width + 80 : -80),
        y: node.position.y + node.height / 2,
      });
      setConnectionMessage("");
    }
  }
  useEffect(() => {
    function move(event: PointerEvent) {
      const current = connectionRef.current;
      if (!current) return;
      if (linkDrag.current && linkDrag.current.pointerId !== event.pointerId)
        return;
      if (
        linkDrag.current &&
        Math.hypot(
          event.clientX - linkDrag.current.start.x,
          event.clientY - linkDrag.current.start.y,
        ) > 4
      )
        linkDrag.current.moved = true;
      setConnectionCursor(world(event.clientX, event.clientY));
      const targetId = document
        .elementFromPoint(event.clientX, event.clientY)
        ?.closest<HTMLElement>("[data-node-id]")?.dataset.nodeId;
      const from = current.handleType === "source" ? current.nodeId : targetId;
      const to = current.handleType === "target" ? current.nodeId : targetId;
      setConnectionTarget(
        from && to && !validateReferenceConnection(graphRef.current, from, to)
          ? targetId || null
          : null,
      );
    }
    function end(event: PointerEvent) {
      const current = linkDrag.current;
      if (!current || current.pointerId !== event.pointerId) return;
      linkDrag.current = null;
      if (!current.moved) return;
      const targetId = document
        .elementFromPoint(event.clientX, event.clientY)
        ?.closest<HTMLElement>("[data-node-id]")?.dataset.nodeId;
      if (targetId) finishConnection(targetId);
      else {
        const source = connectionRef.current;
        const target = document.elementFromPoint(event.clientX, event.clientY);
        const isCanvas =
          target &&
          container.current?.contains(target) &&
          !target.closest("[data-canvas-no-zoom],[data-connection-id]");
        if (source && isCanvas)
          setCreateMenu({
            position: world(event.clientX, event.clientY),
            connection: source,
          });
        cancelConnection();
      }
    }
    const cancel = () => cancelConnection();
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", end);
    window.addEventListener("pointercancel", cancel);
    window.addEventListener("blur", cancel);
    return () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", end);
      window.removeEventListener("pointercancel", cancel);
      window.removeEventListener("blur", cancel);
    };
  }, [world, finishConnection, cancelConnection]);
  useEffect(() => {
    function move(event: PointerEvent) {
      if (!drag.current || drag.current.pointerId !== event.pointerId) return;
      const current = drag.current,
        k = graphRef.current.viewport.k;
      if (!current.moved) {
        if (
          Math.hypot(
            event.clientX - current.start.x,
            event.clientY - current.start.y,
          ) < 4
        )
          return;
        checkpointCanvasHistory(history, current.graph);
        current.moved = true;
        setIsNodeDragging(true);
        setHistoryVersion((v) => v + 1);
      }
      const next = {
        ...graphRef.current,
        nodes: graphRef.current.nodes.map((n) =>
          current.ids.includes(n.id) &&
          current.graph.nodes.some((original) => original.id === n.id)
            ? {
                ...n,
                position: {
                  x:
                    current.graph.nodes.find(
                      (original) => original.id === n.id,
                    )!.position.x +
                    (event.clientX - current.start.x) / k,
                  y:
                    current.graph.nodes.find(
                      (original) => original.id === n.id,
                    )!.position.y +
                    (event.clientY - current.start.y) / k,
                },
              }
            : n,
        ),
      };
      graphRef.current = next;
      setDropTargetGroupId(
        findGroupDropTarget(new Set(current.ids), next.nodes)?.id || null,
      );
      onChange(next);
    }
    function end(event?: PointerEvent) {
      if (
        !drag.current ||
        (event && drag.current.pointerId !== event.pointerId)
      )
        return;
      if (drag.current.moved) {
        const ids = new Set(drag.current.ids);
        const target =
          event?.type === "pointerup"
            ? findGroupDropTarget(ids, graphRef.current.nodes)
            : null;
        const nodes = target
          ? snapNodesIntoGroup(ids, graphRef.current.nodes, target)
          : graphRef.current.nodes.map((node) => {
              if (!ids.has(node.id) || node.type === CanvasNodeType.Group)
                return node;
              // Group movement retains membership; individual drops update it once.
              if (node.metadata?.groupId && ids.has(node.metadata.groupId))
                return node;
              return {
                ...node,
                metadata: {
                  ...node.metadata,
                  groupId: findContainingGroupId(node, graphRef.current.nodes),
                },
              };
            });
        const next = { ...graphRef.current, nodes };
        graphRef.current = next;
        onChange(next);
      }
      if (event && drag.current.touch && event.type !== "pointercancel")
        setSelected(drag.current.selection);
      if (event && !drag.current.moved && event.type !== "pointercancel") {
        const current = drag.current;
        setSelected(
          current.shiftKey
            ? current.selectionBefore.includes(current.nodeId)
              ? current.selectionBefore.filter((id) => id !== current.nodeId)
              : [...current.selectionBefore, current.nodeId]
            : [current.nodeId],
        );
        const clicked = graphRef.current.nodes.find(
          (node) => node.id === current.nodeId,
        );
        setComposerNodeId(
          !current.shiftKey &&
            clicked &&
            [
              CanvasNodeType.Image,
              CanvasNodeType.Video,
              CanvasNodeType.Config,
            ].includes(clicked.type)
            ? clicked.id
            : null,
        );
      }
      drag.current = null;
      setIsNodeDragging(false);
      setDropTargetGroupId(null);
    }
    function blur() {
      // Keep a completed move and its existing single history checkpoint.
      // A lost pointer must never resume dragging when the window regains focus.
      end();
      setBox(null);
    }
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", end);
    window.addEventListener("pointercancel", end);
    window.addEventListener("blur", blur);
    return () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", end);
      window.removeEventListener("pointercancel", end);
      window.removeEventListener("blur", blur);
    };
  }, [history, onChange]);
  useEffect(() => {
    function move(event: PointerEvent) {
      const current = resize.current;
      if (!current || current.pointerId !== event.pointerId) return;
      const original = current.graph.nodes.find(
        (node) => node.id === current.nodeId,
      );
      if (!original) return;
      const dx = (event.clientX - current.start.x) / current.graph.viewport.k;
      const dy = (event.clientY - current.start.y) / current.graph.viewport.k;
      if (!current.moved) {
        if (Math.hypot(dx, dy) < 3) return;
        checkpointCanvasHistory(history, current.graph);
        current.moved = true;
        setHistoryVersion((value) => value + 1);
      }
      const next = {
        ...graphRef.current,
        nodes: graphRef.current.nodes.map((node) =>
          node.id === current.nodeId
            ? {
                ...node,
                width: Math.max(120, original.width + dx),
                height: Math.max(80, original.height + dy),
              }
            : node,
        ),
      };
      graphRef.current = next;
      onChange(next);
    }
    function end(event?: PointerEvent) {
      if (event && event.pointerId !== resize.current?.pointerId) return;
      resize.current = null;
      setIsResizing(false);
    }
    const blur = () => end();
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", end);
    window.addEventListener("pointercancel", end);
    window.addEventListener("blur", blur);
    return () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", end);
      window.removeEventListener("pointercancel", end);
      window.removeEventListener("blur", blur);
    };
  }, [history, onChange]);
  useEffect(() => {
    if (!box) return;
    const start = box.start;
    function move(event: PointerEvent) {
      const end = world(event.clientX, event.clientY);
      setBox({ start, end });
      const left = Math.min(start.x, end.x),
        right = Math.max(start.x, end.x),
        top = Math.min(start.y, end.y),
        bottom = Math.max(start.y, end.y);
      setSelected(
        graphRef.current.nodes
          .filter(
            (n) =>
              n.position.x < right &&
              n.position.x + n.width > left &&
              n.position.y < bottom &&
              n.position.y + n.height > top,
          )
          .map((n) => n.id),
      );
    }
    function end() {
      setBox(null);
    }
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", end, { once: true });
    window.addEventListener("pointercancel", end, { once: true });
    return () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", end);
      window.removeEventListener("pointercancel", end);
    };
  }, [Boolean(box), world]);
  function add(
    type: CanvasNodeType,
    position?: Position,
    asset?: Asset,
    generationKind: "image" | "video" = "image",
  ) {
    const current = graphRef.current;
    const width =
        type === CanvasNodeType.Text
          ? 260
          : type === CanvasNodeType.Group
            ? 520
            : 320,
      height =
        type === CanvasNodeType.Text
          ? 190
          : type === CanvasNodeType.Group
            ? 340
            : 240;
    let placement = position;
    if (!placement) {
      const columns = Math.max(
        1,
        Math.floor(
          ((container.current?.clientWidth || 960) - 100) /
            current.viewport.k /
            352,
        ),
      );
      for (let index = 0; index <= current.nodes.length * 8 + 8; index++) {
        placement = {
          x:
            (80 - current.viewport.x) / current.viewport.k +
            (index % columns) * 352,
          y:
            (100 - current.viewport.y) / current.viewport.k +
            Math.floor(index / columns) * 272,
        };
        const candidate = placement;
        if (
          !current.nodes.some(
            (existing) =>
              existing.type !== CanvasNodeType.Group &&
              candidate.x < existing.position.x + existing.width + 16 &&
              candidate.x + width + 16 > existing.position.x &&
              candidate.y < existing.position.y + existing.height + 16 &&
              candidate.y + height + 16 > existing.position.y,
          )
        )
          break;
      }
    }
    const node: CanvasNodeData = {
      id: crypto.randomUUID(),
      type,
      title:
        asset?.file_name ||
        (type === CanvasNodeType.Text
          ? t("创作笔记")
          : type === CanvasNodeType.Config
            ? generationKind === "video"
              ? t("视频创作")
              : t("图片创作")
            : type === CanvasNodeType.Image
              ? t("图片创作")
              : type === CanvasNodeType.Video
                ? t("视频创作")
                : type === CanvasNodeType.Group
                  ? t("素材分组")
                  : t("素材")),
      position: placement!,
      width,
      height,
      metadata: asset
        ? { assetId: asset.id }
        : {
            content:
              type === CanvasNodeType.Text ? t("在这里记录你的创作想法…") : "",
            prompt: "",
            creationNode: true,
            ...([
              CanvasNodeType.Config,
              CanvasNodeType.Image,
              CanvasNodeType.Video,
            ].includes(type)
              ? {
                  generationKind:
                    type === CanvasNodeType.Video ? "video" : generationKind,
                }
              : {}),
          },
    };
    commit({ ...current, nodes: [...current.nodes, node] });
    setSelected([node.id]);
    setSelectedEdge(null);
    setComposerNodeId(null);
    layout.current?.focus({ preventScroll: true });
    return node;
  }
  function createGeneration(kind: "image" | "video") {
    add(
      kind === "video" ? CanvasNodeType.Video : CanvasNodeType.Image,
      undefined,
      undefined,
      kind,
    );
  }
  function createFromMenu(type: CanvasNodeType) {
    if (!createMenu) return;
    const pending = createMenu;
    const node = add(type, pending.position);
    if (pending.connection) {
      const fromNodeId =
        pending.connection.handleType === "source"
          ? pending.connection.nodeId
          : node.id;
      const toNodeId =
        pending.connection.handleType === "target"
          ? pending.connection.nodeId
          : node.id;
      if (
        !validateReferenceConnection(graphRef.current, fromNodeId, toNodeId)
      ) {
        commit(
          {
            ...graphRef.current,
            connections: [
              ...graphRef.current.connections,
              {
                id: crypto.randomUUID(),
                fromNodeId,
                toNodeId,
              },
            ],
          },
          false,
        );
      }
    }
    setCreateMenu(null);
  }
  function startDrag(event: React.PointerEvent, node: CanvasNodeData) {
    if (pickingReferenceFor && event.button === 0) {
      event.preventDefault();
      event.stopPropagation();
      return;
    }
    if (
      event.button !== 0 ||
      (event.target as HTMLElement).closest(
        "button,input,textarea,select,video,audio,[contenteditable=true]",
      )
    )
      return;
    event.stopPropagation();
    setCreateMenu(null);
    if (connection) {
      finishConnection(node.id);
      return;
    }
    const ids = selected.includes(node.id)
      ? selected
      : event.shiftKey || event.ctrlKey || event.metaKey
        ? [...selected, node.id]
        : [node.id];
    // Opening a mobile inspector on the first finger would cover the second finger's pinch target.
    if (event.pointerType !== "touch") setSelected(ids);
    setSelectedEdge(null);
    const members = collectGroupMemberNodes(new Set(ids), graph.nodes).map(
      (n) => n.id,
    );
    drag.current = {
      pointerId: event.pointerId,
      start: { x: event.clientX, y: event.clientY },
      graph: structuredClone(graph),
      ids: [...new Set([...ids, ...members])],
      moved: false,
      touch: event.pointerType === "touch",
      selection: ids,
      selectionBefore: selected,
      shiftKey: event.shiftKey || event.ctrlKey || event.metaKey,
      nodeId: node.id,
    };
  }
  function group() {
    const current = graphRef.current;
    if (!canGroupSelectedNodes(new Set(selected), current.nodes)) return;
    const members = collectGroupMemberNodes(new Set(selected), current.nodes);
    if (members.length < 2) return;
    const rect = getGroupWrapRect(members);
    const result = applyGroupSelection(
      new Set(selected),
      current.nodes,
      current.connections,
      {
        id: crypto.randomUUID(),
        type: CanvasNodeType.Group,
        title: t("素材分组"),
        position: { x: rect.x, y: rect.y },
        width: rect.width,
        height: rect.height,
      },
    );
    if (result) {
      commit({
        ...current,
        nodes: result.nodes,
        connections: result.connections,
      });
      setSelected(result.selectedIds);
    }
  }
  function ungroup() {
    const current = graphRef.current;
    const result = applyUngroupSelection(
      new Set(selected),
      current.nodes,
      current.connections,
    );
    if (result) {
      commit({
        ...current,
        nodes: result.nodes,
        connections: result.connections,
      });
      setSelected(result.selectedIds);
    }
  }
  function fit() {
    if (!graph.nodes.length || !container.current) {
      onChange({ ...graph, viewport: { x: 80, y: 80, k: 1 } });
      return;
    }
    const bounds = nodeBounds(graph.nodes),
      rect = container.current.getBoundingClientRect(),
      k = Math.max(
        0.05,
        Math.min(
          1,
          (rect.width - 120) / (bounds.right - bounds.left),
          (rect.height - 120) / (bounds.bottom - bounds.top),
        ),
      );
    onChange({
      ...graph,
      viewport: {
        x: rect.width / 2 - ((bounds.left + bounds.right) / 2) * k,
        y: rect.height / 2 - ((bounds.top + bounds.bottom) / 2) * k,
        k,
      },
    });
  }
  const groupCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const node of graph.nodes) {
      const id = node.metadata?.groupId;
      if (id) counts.set(id, (counts.get(id) || 0) + 1);
    }
    return counts;
  }, [graph.nodes]);
  const activeNode =
    selected.length === 1
      ? graph.nodes.find((n) => n.id === selected[0])
      : undefined;
  const composerNode =
    activeNode?.id === composerNodeId && !pickingReferenceFor
      ? activeNode
      : undefined;
  const composerWidth = Math.min(520, Math.max(0, stageSize.width - 24));
  const composerPosition = composerNode
    ? placeCanvasComposer(
        composerNode,
        graph.viewport,
        stageSize,
        composerWidth,
        stageSize.width < CANVAS_STACKED_CONTROLS_WIDTH ? 144 : 80,
      )
    : { x: 12, y: 62, maxHeight: 420 };
  const menuPosition = createMenu
    ? {
        x:
          (Math.max(
            12,
            Math.min(
              stageSize.width - 252,
              graph.viewport.x + createMenu.position.x * graph.viewport.k,
            ),
          ) -
            graph.viewport.x) /
          graph.viewport.k,
        y:
          (Math.max(
            12,
            Math.min(
              stageSize.height - (createMenu.connection ? 170 : 284),
              graph.viewport.y + createMenu.position.y * graph.viewport.k,
            ),
          ) -
            graph.viewport.y) /
          graph.viewport.k,
      }
    : undefined;
  const selectedMedia = selected.map((id) => {
    const node = graph.nodes.find((item) => item.id === id);
    if (
      !node ||
      (node.type !== CanvasNodeType.Image && node.type !== CanvasNodeType.Video)
    )
      return undefined;
    return assets.find(
      (asset) =>
        asset.id === node.metadata?.assetId && asset.kind === node.type,
    );
  });
  const comparisonAssets = [
    ...new Map(
      selectedMedia
        .filter((asset): asset is Asset => Boolean(asset))
        .map((asset) => [asset.id, asset]),
    ).values(),
  ];
  const canCompareSelection =
    selected.length >= 2 &&
    selectedMedia.every((asset) => asset !== undefined) &&
    comparisonAssets.length >= 2 &&
    comparisonAssets.every((asset) => asset.kind === comparisonAssets[0].kind);
  const activeEdge = graph.connections.find((edge) => edge.id === selectedEdge);
  const edgeFrom = graph.nodes.find(
    (node) => node.id === activeEdge?.fromNodeId,
  );
  const edgeTo = graph.nodes.find((node) => node.id === activeEdge?.toNodeId);
  function updateNode(values: Partial<CanvasNodeData>) {
    if (!activeNode) return;
    commit({
      ...graph,
      nodes: graph.nodes.map((n) =>
        n.id === activeNode.id ? { ...n, ...values } : n,
      ),
    });
  }
  return (
    <div
      className={`canvas-layout ${isNodeDragging ? "canvas-node-dragging" : ""}`}
      data-canvas-theme={theme}
      data-show-image-info={Boolean(graph.showImageInfo)}
      data-canvas-keyboard-scope="true"
      ref={layout}
      tabIndex={-1}
      onPointerDownCapture={(event) => {
        if (
          !(event.target as Element).closest(
            "button,input,textarea,select,a,[role=button]",
          )
        )
          layout.current?.focus({ preventScroll: true });
      }}
    >
      <div
        className={`canvas-stage ${connection ? "canvas-connecting" : ""} ${pickingReferenceFor ? "canvas-reference-picking" : ""}`}
      >
        <InfiniteCanvas
          containerRef={container}
          viewport={graph.viewport}
          tool="pan"
          backgroundMode={graph.backgroundMode || "lines"}
          onViewportChange={(viewport) =>
            onChange({ ...graphRef.current, viewport })
          }
          onCanvasDeselect={() => {
            if (pickingReferenceFor) {
              setPickingReferenceFor(null);
              return;
            }
            setSelected([]);
            setSelectedEdge(null);
            setComposerNodeId(null);
          }}
          onGestureStart={() => {
            drag.current = null;
            resize.current = null;
            setIsNodeDragging(false);
            setIsResizing(false);
            setDropTargetGroupId(null);
            setBox(null);
            cancelConnection();
            setCreateMenu(null);
          }}
          onCanvasMouseDown={(event) => {
            if (pickingReferenceFor) {
              setPickingReferenceFor(null);
              return;
            }
            setComposerNodeId(null);
            setCreateMenu(null);
            if (connectionRef.current) {
              cancelConnection();
              setConnectionMessage("");
              return;
            }
            const position = world(event.clientX, event.clientY);
            setBox({ start: position, end: position });
            setSelected([]);
            setSelectedEdge(null);
          }}
          onCanvasDoubleClick={(event) => {
            setComposerNodeId(null);
            setCreateMenu({ position: world(event.clientX, event.clientY) });
          }}
          onDrop={(event) => {
            event.preventDefault();
            const asset = assets.find(
              (a) =>
                a.id === event.dataTransfer.getData("application/frame-asset"),
            );
            if (asset)
              add(
                asset.kind === "video"
                  ? CanvasNodeType.Video
                  : asset.kind === "audio"
                    ? CanvasNodeType.Audio
                    : CanvasNodeType.Image,
                world(event.clientX, event.clientY),
                asset,
              );
          }}
        >
          <svg className="canvas-edges">
            {graph.connections.map((edge) => {
              const from = graph.nodes.find((n) => n.id === edge.fromNodeId),
                to = graph.nodes.find((n) => n.id === edge.toNodeId);
              return from && to ? (
                <ConnectionPath
                  key={edge.id}
                  connection={edge}
                  from={from}
                  to={to}
                  active={selectedEdge === edge.id}
                  label={t(
                    "{0} 引用至 {1}，按 Enter 查看连线",
                    from.title,
                    to.title,
                  )}
                  onSelect={() => {
                    setSelectedEdge(edge.id);
                    setSelected([]);
                  }}
                />
              ) : null;
            })}
            {connection && (
              <ActiveConnectionPath
                node={graph.nodes.find((node) => node.id === connection.nodeId)}
                handle={connection}
                mouseWorld={connectionCursor}
                target={graph.nodes.find(
                  (node) => node.id === connectionTarget,
                )}
              />
            )}
          </svg>
          {graph.nodes.map((node) => {
            const asset = assets.find((a) => a.id === node.metadata?.assetId);
            return (
              <div
                key={node.id}
                data-node-id={node.id}
                className={`canvas-node ${node.type === CanvasNodeType.Group ? "canvas-group" : ""} ${[CanvasNodeType.Image, CanvasNodeType.Video, CanvasNodeType.Config].includes(node.type) ? "is-media-node" : ""} ${selected.includes(node.id) ? "node-selected" : ""} ${dropTargetGroupId === node.id ? "node-group-drop-target" : ""} ${connectionTarget === node.id ? "node-reference-target" : ""} ${pickingReferenceFor && node.metadata?.assetId && node.id !== pickingReferenceFor ? "node-reference-pickable" : ""}`}
                style={{
                  left: node.position.x,
                  top: node.position.y,
                  width: node.width,
                  height: node.height,
                }}
                onPointerDown={(event) => startDrag(event, node)}
                onClickCapture={(event) => {
                  if (!pickingReferenceFor) {
                    if ((event.target as Element).closest("video")) {
                      const additive =
                        event.shiftKey || event.ctrlKey || event.metaKey;
                      const ids = additive
                        ? selected.includes(node.id)
                          ? selected.filter((id) => id !== node.id)
                          : [...selected, node.id]
                        : [node.id];
                      setSelected(ids);
                      setSelectedEdge(null);
                      setComposerNodeId(additive ? null : node.id);
                    }
                    return;
                  }
                  event.preventDefault();
                  event.stopPropagation();
                  if (
                    node.id !== pickingReferenceFor &&
                    typeof node.metadata?.assetId === "string"
                  ) {
                    onPickReference?.(
                      pickingReferenceFor,
                      node.metadata.assetId,
                    );
                    setPickingReferenceFor(null);
                    setComposerNodeId(pickingReferenceFor);
                  }
                }}
                tabIndex={0}
                aria-label={node.title}
                onKeyDown={(event) => {
                  if (
                    event.target !== event.currentTarget ||
                    event.key !== "Enter"
                  )
                    return;
                  event.preventDefault();
                  if (
                    pickingReferenceFor &&
                    node.id !== pickingReferenceFor &&
                    typeof node.metadata?.assetId === "string"
                  ) {
                    onPickReference?.(
                      pickingReferenceFor,
                      node.metadata.assetId,
                    );
                    setPickingReferenceFor(null);
                    return;
                  }
                  setSelected([node.id]);
                  setComposerNodeId(node.id);
                }}
                onDoubleClick={(event) => {
                  event.stopPropagation();
                  if (asset) setPreview(asset);
                }}
              >
                <div className="node-header">
                  <Icon
                    name={
                      node.type === "config"
                        ? node.metadata?.generationKind === "video"
                          ? "video"
                          : "image"
                        : node.type === "group"
                          ? "group"
                          : node.type === "text"
                            ? "text"
                            : node.type
                    }
                  />
                  {node.type === CanvasNodeType.Group ? (
                    <CanvasGroupLabel
                      title={node.title}
                      count={groupCounts.get(node.id) || 0}
                      onRename={(title) =>
                        commit({
                          ...graphRef.current,
                          nodes: graphRef.current.nodes.map((item) =>
                            item.id === node.id ? { ...item, title } : item,
                          ),
                        })
                      }
                    />
                  ) : (
                    <span>{node.title}</span>
                  )}
                  {node.type === CanvasNodeType.Config && (
                    <small className="node-generation-kind">
                      {node.metadata?.generationKind === "video"
                        ? t("视频")
                        : t("图片")}
                    </small>
                  )}
                  {asset && (
                    <button
                      title={t("预览素材")}
                      onClick={() => setPreview(asset)}
                    >
                      <Icon name="image" size={13} />
                    </button>
                  )}
                </div>
                {node.type !== CanvasNodeType.Group && (
                  <div className="node-body">
                    {asset ? (
                      <AssetVisual
                        asset={asset}
                        controls={asset.kind === "video"}
                      />
                    ) : node.metadata?.generationTaskId ? (
                      <div
                        className="node-task-state"
                        data-generation-status={String(
                          node.metadata.executionStatus || "queued",
                        )}
                      >
                        <Icon
                          name={
                            node.metadata.executionStatus === "failed" ||
                            ["error", "delivery_failed"].includes(
                              String(node.metadata.deliveryStatus),
                            )
                              ? "info"
                              : "clock"
                          }
                          size={26}
                        />
                        <strong>{generationNodeStatus(node)}</strong>
                        <small>
                          {t(
                            "任务 {0}",
                            String(node.metadata.generationTaskId).slice(-8),
                          )}
                        </small>
                        {Boolean(
                          node.metadata.errorMessage ||
                          node.metadata.error_message,
                        ) && (
                          <p>
                            {String(
                              node.metadata.errorMessage ||
                                node.metadata.error_message,
                            )}
                          </p>
                        )}
                        <span>{t("成品保存后可继续作为参考素材")}</span>
                        {onShowTask && (
                          <button
                            className="button secondary small"
                            onClick={() =>
                              onShowTask(
                                String(node.metadata!.generationTaskId),
                              )
                            }
                          >
                            {t("查看任务")}
                          </button>
                        )}
                      </div>
                    ) : node.type === CanvasNodeType.Text ? (
                      <p className="node-note">
                        {node.metadata?.content || t("点击并在属性栏编辑笔记")}
                      </p>
                    ) : node.type === CanvasNodeType.Config ||
                      (node.metadata?.creationNode &&
                        [CanvasNodeType.Image, CanvasNodeType.Video].includes(
                          node.type,
                        )) ? (
                      <CanvasNodePlaceholder node={node} />
                    ) : (
                      <div className="node-task-state">
                        <Icon name="image" size={26} />
                        <strong>{t("素材暂不可用")}</strong>
                        <small>
                          {t("请在素材库检查文件，或刷新项目后重试")}
                        </small>
                      </div>
                    )}
                  </div>
                )}
                {graph.showImageInfo &&
                  asset &&
                  node.type === CanvasNodeType.Image && (
                    <div className="node-media-info">
                      <span>
                        {asset.width > 0 && asset.height > 0
                          ? `${asset.width} × ${asset.height}`
                          : asset.file_name}
                      </span>
                      <span>
                        {asset.content_type.replace("image/", "").toUpperCase()}
                      </span>
                    </div>
                  )}
                {node.type === CanvasNodeType.Group && (
                  <button
                    className="group-resize-handle"
                    aria-label={t("调整组大小")}
                    title={t("拖动调整大小，或使用方向键")}
                    onPointerDown={(event) => {
                      if (event.button !== 0) return;
                      event.preventDefault();
                      event.stopPropagation();
                      event.currentTarget.focus({ preventScroll: true });
                      drag.current = null;
                      cancelConnection();
                      setSelected([node.id]);
                      setIsResizing(true);
                      resize.current = {
                        pointerId: event.pointerId,
                        nodeId: node.id,
                        start: { x: event.clientX, y: event.clientY },
                        graph: structuredClone(graphRef.current),
                        moved: false,
                      };
                    }}
                    onKeyDown={(event) => {
                      if (
                        ![
                          "ArrowLeft",
                          "ArrowRight",
                          "ArrowUp",
                          "ArrowDown",
                        ].includes(event.key)
                      )
                        return;
                      event.preventDefault();
                      event.stopPropagation();
                      const step = event.shiftKey ? 40 : 10;
                      const current = graphRef.current;
                      commit({
                        ...current,
                        nodes: current.nodes.map((item) =>
                          item.id === node.id
                            ? {
                                ...item,
                                width: Math.max(
                                  120,
                                  item.width +
                                    (event.key === "ArrowLeft"
                                      ? -step
                                      : event.key === "ArrowRight"
                                        ? step
                                        : 0),
                                ),
                                height: Math.max(
                                  80,
                                  item.height +
                                    (event.key === "ArrowUp"
                                      ? -step
                                      : event.key === "ArrowDown"
                                        ? step
                                        : 0),
                                ),
                              }
                            : item,
                        ),
                      });
                    }}
                  >
                    <svg
                      width="14"
                      height="14"
                      viewBox="0 0 14 14"
                      aria-hidden="true"
                    >
                      <path
                        d="m3 11 8-8m-3 8 3-3"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="1.5"
                      />
                    </svg>
                  </button>
                )}
                {canOutputReference(node) && (
                  <button
                    className={`node-port port-out ${connection?.nodeId === node.id ? "port-active" : ""}`}
                    style={{
                      transform: `translateY(-50%) scale(${Math.max(1, 1 / graph.viewport.k)})`,
                    }}
                    data-reference-port="source"
                    onPointerDown={(event) =>
                      startConnection(event, node, "source")
                    }
                    onClick={(event) => {
                      if (event.detail === 0)
                        keyboardConnection(node, "source");
                    }}
                    aria-label={t("从 {0} 创建连线", node.title)}
                    aria-pressed={connection?.nodeId === node.id}
                    title={t("拖到创作节点的参考输入，也可点击两端连接")}
                  >
                    <Icon name="arrow" size={13} />
                    <span className="port-caption">{t("引用")}</span>
                  </button>
                )}
                {canReceiveReference(node) && (
                  <button
                    className={`node-port port-in ${connection?.nodeId === node.id ? "port-active" : ""}`}
                    style={{
                      transform: `translateY(-50%) scale(${Math.max(1, 1 / graph.viewport.k)})`,
                    }}
                    data-reference-port="target"
                    onPointerDown={(event) =>
                      startConnection(event, node, "target")
                    }
                    onClick={(event) => {
                      if (event.detail === 0)
                        keyboardConnection(node, "target");
                    }}
                    aria-label={t("连接至 {0}", node.title)}
                    aria-pressed={connection?.nodeId === node.id}
                    title={t("接收素材、笔记或分组引用")}
                  >
                    <Icon name="plus" size={13} />
                    <span className="port-caption">{t("参考")}</span>
                  </button>
                )}
              </div>
            );
          })}
          {box && (
            <div
              className="selection-box"
              style={{
                left: Math.min(box.start.x, box.end.x),
                top: Math.min(box.start.y, box.end.y),
                width: Math.abs(box.end.x - box.start.x),
                height: Math.abs(box.end.y - box.start.y),
              }}
            />
          )}
          {createMenu &&
            menuPosition &&
            (createMenu.connection ? (
              <ConnectionCreateMenu
                pending={
                  {
                    connection: createMenu.connection,
                    position: menuPosition,
                  } satisfies PendingConnectionCreate
                }
                scale={graph.viewport.k}
                types={
                  createMenu.connection.handleType === "source"
                    ? [CanvasNodeType.Image, CanvasNodeType.Video]
                    : [CanvasNodeType.Text, CanvasNodeType.Group]
                }
                onCreate={createFromMenu}
                onClose={closeCreateMenu}
              />
            ) : (
              <NodeCreateMenu
                position={menuPosition}
                scale={graph.viewport.k}
                types={[
                  CanvasNodeType.Image,
                  CanvasNodeType.Video,
                  CanvasNodeType.Text,
                  CanvasNodeType.Group,
                ]}
                onCreate={createFromMenu}
                onClose={closeCreateMenu}
              />
            ))}
        </InfiniteCanvas>
        {activeNode &&
          activeNode.type !== CanvasNodeType.Text &&
          !pickingReferenceFor &&
          !isNodeDragging &&
          !isResizing &&
          !box &&
          !composerNode && (
            <CanvasNodeHoverToolbar
              node={activeNode}
              viewport={graph.viewport}
              bounds={stageSize}
              asset={assets.find(
                (asset) => asset.id === activeNode.metadata?.assetId,
              )}
              onEdit={() => setComposerNodeId(activeNode.id)}
              onPreview={() => {
                const asset = assets.find(
                  (asset) => asset.id === activeNode.metadata?.assetId,
                );
                if (asset) setPreview(asset);
              }}
              onDuplicate={duplicate}
              onDelete={remove}
              onUngroup={
                activeNode.type === CanvasNodeType.Group ? ungroup : undefined
              }
              onShowTask={
                activeNode.metadata?.generationTaskId && onShowTask
                  ? () =>
                      onShowTask(String(activeNode.metadata!.generationTaskId))
                  : undefined
              }
            />
          )}
        {composerNode &&
          renderComposer &&
          !isNodeDragging &&
          !isResizing &&
          !box && (
            <div
              className="canvas-node-composer-overlay"
              data-canvas-no-zoom
              data-composer-node-id={composerNode.id}
              style={{
                left: composerPosition.x,
                top: composerPosition.y,
                width: composerWidth,
                maxHeight: composerPosition.maxHeight,
              }}
              onPointerDown={(event) => event.stopPropagation()}
              onWheel={(event) => event.stopPropagation()}
            >
              {renderComposer(composerNode, {
                onClose: () => setComposerNodeId(null),
                onPickReferences: () => {
                  cancelConnection();
                  setCreateMenu(null);
                  setPickingReferenceFor(composerNode.id);
                  layout.current?.focus({ preventScroll: true });
                },
              })}
            </div>
          )}
        {pickingReferenceFor && (
          <div
            className="canvas-reference-picking-hint"
            role="status"
            data-canvas-no-zoom
          >
            <Icon name="cursor" size={16} />
            <span>{t("从画布选择参考素材")}</span>
            <button onClick={() => setPickingReferenceFor(null)}>
              {t("返回输入框")} <kbd>Esc</kbd>
            </button>
          </div>
        )}
        {selected.length > 1 &&
          !pickingReferenceFor &&
          !isNodeDragging &&
          !box && (
            <div className="canvas-selection-actions" data-canvas-no-zoom>
              <span>{t("已选 {0} 个节点", selected.length)}</span>
              {onCompare && selected.length > 1 && (
                <button
                  disabled={!canCompareSelection}
                  onClick={() => onCompare(comparisonAssets)}
                >
                  {t("对比")}
                </button>
              )}
              {selected.length > 1 && (
                <button
                  disabled={
                    !canGroupSelectedNodes(new Set(selected), graph.nodes)
                  }
                  onClick={group}
                >
                  {t("分组")}
                </button>
              )}
              {canUngroupSelectedNodes(new Set(selected), graph.nodes) && (
                <button onClick={ungroup}>{t("解除分组")}</button>
              )}
              <button onClick={duplicate} title={t("复制节点")}>
                <Icon name="copy" size={14} />
              </button>
              <button onClick={remove} title={t("删除选中节点")}>
                <Icon name="trash" size={14} />
              </button>
            </div>
          )}
        {!graph.nodes.length && (
          <div className="canvas-empty">
            <span className="eyebrow">YOUR CREATIVE TABLE</span>
            <h2>{t("把第一个想法，放在这里。")}</h2>
            <p>{t("自由创作图片和视频，连线组织素材与灵感。")}</p>
            <div>
              <button
                className="button secondary"
                onClick={() => createGeneration("image")}
              >
                <Icon name="image" />
                {t("创作图片")}
              </button>
              <button
                className="button primary"
                onClick={() => createGeneration("video")}
              >
                <Icon name="video" />
                {t("创作视频")}
              </button>
            </div>
            <small>{t("双击空白添加节点 · 拖动空白移动 · 滚轮缩放")}</small>
          </div>
        )}
        <div className="canvas-tool-dock">
          <button
            className="icon-button selected"
            onClick={() => {
              setCreateMenu(null);
              setPickingReferenceFor(null);
              cancelConnection();
              layout.current?.focus({ preventScroll: true });
            }}
            aria-label={t("移动画布")}
            title={t("拖动空白移动画布，点击节点编辑")}
            aria-pressed="true"
          >
            <Icon name="hand" />
          </button>
          <i />
          <button
            className="icon-button"
            onClick={() => add(CanvasNodeType.Text)}
            aria-label={t("添加文字笔记")}
            title={t("添加文字笔记")}
          >
            <Icon name="text" />
          </button>
          <button
            className="icon-button"
            onClick={onAssetLibrary}
            aria-label={t("打开素材库")}
            title={t("素材库")}
          >
            <Icon name="grid" />
          </button>
          <button
            className="canvas-create-button"
            onClick={() => createGeneration("image")}
            aria-label={t("创作图片")}
            title={t("创作图片")}
          >
            <Icon name="image" />
            <span>{t("图片")}</span>
          </button>
          <button
            className="canvas-create-button"
            onClick={() => createGeneration("video")}
            aria-label={t("创作视频")}
            title={t("创作视频")}
          >
            <Icon name="video" />
            <span>{t("视频")}</span>
          </button>
          <button
            className="icon-button"
            onClick={() => add(CanvasNodeType.Group)}
            aria-label={t("添加组")}
            title={t("添加组")}
          >
            <Icon name="group" />
          </button>
          <i />
          <button
            className="icon-button"
            onClick={undo}
            disabled={!history.past.length}
            aria-label={t("撤销")}
            title={t("撤销 Ctrl+Z")}
          >
            <Icon name="undo" />
          </button>
          <button
            className="icon-button"
            onClick={redo}
            disabled={!history.future.length}
            aria-label={t("重做")}
            title={t("重做 Ctrl+Shift+Z")}
          >
            <Icon name="redo" />
          </button>
          <i />
          <CanvasAppearance
            backgroundMode={graph.backgroundMode || "lines"}
            showImageInfo={Boolean(graph.showImageInfo)}
            onChange={(patch) => commit({ ...graphRef.current, ...patch })}
          />
        </div>
        <CanvasViewControls
          nodes={graph.nodes}
          viewport={graph.viewport}
          size={stageSize}
          onViewportChange={(viewport) =>
            onChange({ ...graphRef.current, viewport })
          }
          onFit={fit}
          onHelp={() => setHelp(true)}
        />
        <a
          className="canvas-attribution"
          href="https://github.com/basketikun/infinite-canvas/tree/3a6b64b602ad062b29fac66902ed18a7ef5fd9dc"
          target="_blank"
          rel="noreferrer"
        >
          Canvas by infinite-canvas · basketikun
        </a>
        {connection && (
          <div className="connection-hint" role="status">
            <Icon name="link" size={14} />
            <span>
              {connection.handleType === "source"
                ? t("拖到或点击创作节点的参考输入")
                : t("拖到或点击素材、笔记或分组的引用输出")}
            </span>
            <button onClick={cancelConnection}>
              {t("取消")} <kbd>Esc</kbd>
            </button>
          </div>
        )}
        {connectionMessage && (
          <div className="canvas-connection-status" role="status">
            <span>{connectionMessage}</span>
            <button
              onClick={() => setConnectionMessage("")}
              aria-label={t("关闭提示")}
            >
              <Icon name="close" size={13} />
            </button>
          </div>
        )}
      </div>
      {activeNode && activeNode.type === CanvasNodeType.Text && (
        <aside className="node-inspector">
          <div className="inspector-heading">
            <span>{t("节点属性")}</span>
            <button
              className="icon-button"
              onClick={() => setSelected([])}
              aria-label={t("关闭属性")}
            >
              <Icon name="close" size={16} />
            </button>
          </div>
          <label>
            {t("名称")}
            <input
              value={activeNode.title}
              onChange={(e) => updateNode({ title: e.target.value })}
            />
          </label>
          {activeNode.type === CanvasNodeType.Text && (
            <label>
              {t("创作笔记")}
              <textarea
                rows={9}
                value={activeNode.metadata?.content || ""}
                onChange={(e) =>
                  updateNode({
                    metadata: {
                      ...activeNode.metadata,
                      content: e.target.value,
                    },
                  })
                }
              />
            </label>
          )}
          <div className="inspector-dimensions">
            <label>
              {t("宽")}
              <input
                type="number"
                min="120"
                max="2000"
                value={activeNode.width}
                onChange={(e) =>
                  updateNode({
                    width: Math.min(
                      2000,
                      Math.max(120, Number(e.target.value)),
                    ),
                  })
                }
              />
            </label>
            <label>
              {t("高")}
              <input
                type="number"
                min="80"
                max="2000"
                value={activeNode.height}
                onChange={(e) =>
                  updateNode({
                    height: Math.min(
                      2000,
                      Math.max(80, Number(e.target.value)),
                    ),
                  })
                }
              />
            </label>
          </div>
          <div className="inspector-actions">
            {onCompare && (
              <button
                className="button secondary"
                disabled={!canCompareSelection}
                title={t("多选至少两个不同且可用的同类图片或视频素材")}
                onClick={() => {
                  if (canCompareSelection) onCompare(comparisonAssets);
                }}
              >
                {t("比较选中素材")}
              </button>
            )}
            <button className="button secondary" onClick={duplicate}>
              <Icon name="copy" size={15} />
              {t("复制节点")}
            </button>
            <button
              className="button secondary"
              disabled={!canGroupSelectedNodes(new Set(selected), graph.nodes)}
              onClick={group}
            >
              <Icon name="group" size={15} />
              {t("建立分组")}
            </button>
            <button
              className="button secondary"
              onClick={ungroup}
              disabled={
                !canUngroupSelectedNodes(new Set(selected), graph.nodes)
              }
            >
              {t("解除分组")}
            </button>
            <button className="button subtle danger-text" onClick={remove}>
              <Icon name="trash" size={15} />
              {t("删除选中节点")}
            </button>
          </div>
          <p className="inspector-note">
            {t(
              "节点操作只修改画布布局与引用。历史生成、作品与积分记录由任务服务保存。",
            )}
          </p>
        </aside>
      )}
      {activeEdge && edgeFrom && edgeTo && (
        <aside className="node-inspector edge-inspector">
          <div className="inspector-heading">
            <span>{t("引用连线")}</span>
            <button
              className="icon-button"
              onClick={() => setSelectedEdge(null)}
              aria-label={t("关闭属性")}
            >
              <Icon name="close" size={16} />
            </button>
          </div>
          <div className="edge-reference-flow">
            <span>{edgeFrom.title}</span>
            <Icon name="arrow" />
            <span>{edgeTo.title}</span>
          </div>
          <p className="inspector-note">
            {canReceiveReference(edgeTo) &&
            edgeTo.metadata?.generationSourceNodeId !== edgeFrom.id
              ? t(
                  "此连线将素材或笔记提供给创作节点。生成前仍需确认参数与报价。",
                )
              : t("此连线记录生成结果的来源。移除连线不会删除任务或成品。")}
          </p>
          <button
            className="button secondary full"
            onClick={() => {
              setSelected([edgeTo.id]);
              setSelectedEdge(null);
            }}
          >
            {t("查看目标节点")}
          </button>
          <button className="button subtle danger-text full" onClick={remove}>
            <Icon name="trash" size={15} />
            {t("删除连线")}
          </button>
          <p className="inspector-note">
            {t("删除连线可撤销，不会启动或取消生成，也不会扣费。")}
          </p>
        </aside>
      )}
      {help && (
        <Modal title={t("画布操作帮助")} onClose={() => setHelp(false)}>
          <div className="canvas-help-content">
            <p>
              {t(
                "从工具栏或双击空白创建图片、视频节点，点击节点输入提示词和参数。进度与成品直接显示在当前节点，再次生成也更新这个节点。",
              )}
            </p>
            <p>
              {t(
                "把素材、笔记或分组右侧的引用端口，拖到创作节点左侧的参考端口；拖到空白可续建节点。也可以依次点击两端连接。",
              )}
            </p>
            <p>
              {t(
                "在节点创作面板中添加素材、指定首尾帧并确认报价。点击空白收起，重新点击节点继续编辑。连线不会自动生成或扣费。",
              )}
            </p>
            <dl>
              <dt>{t("选择 / 多选")}</dt>
              <dd>
                {t(
                  "点击节点 · Shift / Ctrl / ⌘ 点击多选 · 按住 Ctrl 或空格拖动空白框选",
                )}
              </dd>
              <dt>{t("平移 / 缩放")}</dt>
              <dd>
                {t("拖动空白或按住鼠标中键移动 · 滚轮缩放 · 触屏双指缩放")}
              </dd>
              <dt>{t("分组 / 解组")}</dt>
              <dd>Ctrl / ⌘ G · Ctrl / ⌘ Shift G</dd>
              <dt>{t("撤销 / 重做")}</dt>
              <dd>Ctrl / ⌘ Z · Ctrl / ⌘ Shift Z</dd>
              <dt>{t("复制 / 删除")}</dt>
              <dd>Ctrl / ⌘ D · Delete</dd>
              <dt>{t("取消连线")}</dt>
              <dd>Esc</dd>
            </dl>
            <small>
              {t("画布自动保存，保存失败或版本冲突时请处理顶部提示。")}
            </small>
          </div>
        </Modal>
      )}
      {preview && (
        <Modal title={preview.file_name} onClose={() => setPreview(null)} wide>
          <div className="media-preview">
            <AssetVisual asset={preview} controls />
          </div>
          <div className="modal-actions">
            <a
              className="button secondary"
              href={`/api/v1/assets/${preview.id}/download?disposition=attachment`}
              download
            >
              <Icon name="download" />
              {t("下载原文件")}
            </a>
          </div>
        </Modal>
      )}
    </div>
  );
}
function generationNodeStatus(node: CanvasNodeData) {
  const execution = node.metadata?.executionStatus;
  if (execution === "submission_unknown") return t("提交结果待核查");
  if (
    node.metadata?.procurementStatus === "disputed" ||
    node.metadata?.deliveryStatus === "manual_review"
  )
    return t("成品待人工核查");
  if (execution === "failed") return t("生成失败");
  if (
    ["error", "delivery_failed"].includes(String(node.metadata?.deliveryStatus))
  )
    return t("交付异常");
  if (execution === "canceled" || execution === "cancelled")
    return t("任务已取消");
  if (node.metadata?.deliveryStatus === "archive_retry")
    return t("成品保存重试中");
  if (execution === "completed") return t("正在保存成品");
  if (execution === "running" || execution === "generating")
    return t("生成进行中");
  if (execution === "sending") return t("正在提交");
  return t("等待生成");
}

export function AssetVisual({
  asset,
  controls = false,
}: {
  asset: Asset;
  controls?: boolean;
}) {
  return (
    <AssetMedia
      key={[asset.id, asset.kind, asset.preview_url, asset.url].join("|")}
      asset={asset}
      controls={controls}
    />
  );
}
function AssetMedia({ asset, controls }: { asset: Asset; controls: boolean }) {
  const [attempt, setAttempt] = useState(0);
  const [failedAttempt, setFailedAttempt] = useState<number | null>(null);
  const src =
    attempt > 0
      ? `/api/v1/assets/${asset.id}/content?preview_retry=${attempt}`
      : asset.preview_url || asset.url || `/api/v1/assets/${asset.id}/content`;
  const onError = () => setFailedAttempt(attempt);
  if (failedAttempt === attempt)
    return (
      <span className="asset-visual-error" role="status">
        <span>{t("预览加载失败")}</span>
        {controls ? (
          <button
            className="button secondary"
            onPointerDown={(event) => event.stopPropagation()}
            onClick={(event) => {
              event.stopPropagation();
              setAttempt((current) => current + 1);
            }}
          >
            {t("重试预览")}
          </button>
        ) : (
          <small>{t("打开预览后重试")}</small>
        )}
      </span>
    );
  return asset.kind === "video" ? (
    <video
      key={attempt}
      src={src}
      onError={onError}
      controls={controls}
      preload="metadata"
      playsInline
      muted={!controls}
    />
  ) : asset.kind === "audio" ? (
    <audio key={attempt} src={src} controls onError={onError} />
  ) : (
    <img
      key={attempt}
      src={src}
      alt={asset.file_name}
      loading="lazy"
      draggable={false}
      onError={onError}
    />
  );
}
