import { t as translate } from "./i18n";
import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  api,
  errorMessage,
  isCurrentSession,
  items,
  query,
  sessionVersion,
} from "./api";
import { PageHeading, useStudio } from "./App";
import { AssetGrid } from "./Workspace";
import { hasPaymentReturn, matchingReturnedOrder } from "./routing";
import { returnedOrderReference } from "./paging";
import {
  Pagination,
  useAllItems,
  useDebouncedValue,
  usePagedResource,
} from "./pagination";
import type {
  Asset,
  Ledger,
  PaymentOrder,
  PaymentTier,
  Project,
  Task,
  Wallet,
} from "./types";
import {
  Empty,
  Icon,
  LoadingError,
  Logo,
  Modal,
  Notice,
  Status,
  formatDate,
  statusLabel,
  useResource,
} from "./ui";
export function AssetsPage() {
  const { notify } = useStudio();
  const projects = useAllItems<Project>("/projects?status=all", {
    items: [],
  });
  const [projectId, setProjectId] = useState(""),
    [kind, setKind] = useState(""),
    [search, setSearch] = useState(""),
    [uploading, setUploading] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const q = useDebouncedValue(search);
  const assets = usePagedResource<Asset>("/assets", {
    project_id: projectId,
    kind,
    q,
  });
  async function upload(files: FileList | null) {
    if (!files?.length || !projectId) return;
    setUploading(true);
    for (const file of Array.from(files)) {
      const body = new FormData();
      body.append("file", file);
      body.append("project_id", projectId);
      try {
        await api("/assets", "POST", body);
        notify(translate("{0} 已上传", file.name));
      } catch (reason) {
        notify(`${file.name}：${errorMessage(reason)}`, true);
      }
    }
    setUploading(false);
    assets.refresh();
    if (input.current) input.current.value = "";
  }
  return (
    <>
      <PageHeading
        eyebrow="YOUR VISUAL LIBRARY"
        title={translate("每个素材，都有下一种可能")}
        description={translate(
          "参考图、生成作品与视频，集中归档，随时回到创作中。",
        )}
      >
        <button
          className="button primary"
          disabled={!projectId || uploading}
          title={!projectId ? translate("先选择素材所属项目") : ""}
          onClick={() => input.current?.click()}
        >
          <Icon name="upload" />
          {uploading ? translate("正在上传…") : translate("上传素材")}
        </button>
      </PageHeading>
      <div className="filter-bar">
        <select
          aria-label={translate("按项目筛选")}
          value={projectId}
          onChange={(e) => setProjectId(e.target.value)}
        >
          <option value="">{translate("全部项目 · 上传前先选择项目")}</option>
          {projects.data.items.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
              {p.status === "archived" ? translate("（已归档）") : ""}
            </option>
          ))}
        </select>
        <div className="segmented">
          <button className={!kind ? "active" : ""} onClick={() => setKind("")}>
            {translate("全部")}
          </button>
          <button
            className={kind === "image" ? "active" : ""}
            onClick={() => setKind("image")}
          >
            {translate("图片")}
          </button>
          <button
            className={kind === "video" ? "active" : ""}
            onClick={() => setKind("video")}
          >
            {translate("视频")}
          </button>
          <button
            className={kind === "audio" ? "active" : ""}
            onClick={() => setKind("audio")}
          >
            {translate("音频")}
          </button>
        </div>
        <div className="search-input">
          <Icon name="search" size={16} />
          <input
            aria-label={translate("搜索素材")}
            placeholder={translate("搜索文件名")}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>
      </div>
      <LoadingError
        loading={projects.loading}
        error={projects.error}
        retry={projects.refresh}
      />
      <input
        type="file"
        accept="image/*,video/*,audio/*"
        hidden
        multiple
        ref={input}
        onChange={(e) => void upload(e.target.files)}
      />
      <AssetGrid
        assets={assets.data.items}
        onChanged={assets.refresh}
        loading={assets.loading}
        error={assets.error}
      />
      {!assets.error && (
        <Pagination
          page={assets.page}
          size={assets.size}
          total={assets.total}
          loading={assets.loading}
          onPage={assets.setPage}
        />
      )}
    </>
  );
}
export function TasksPage({ initialTaskId }: { initialTaskId?: string }) {
  const { notify, refreshWallet, navigate } = useStudio();
  const [kind, setKind] = useState(""),
    [status, setStatus] = useState(""),
    [projectId, setProjectId] = useState(""),
    [search, setSearch] = useState(""),
    [detailTask, setSelected] = useState<Task | null>(null),
    [canceling, setCanceling] = useState(false);
  const q = useDebouncedValue(search);
  const projects = useAllItems<Project>("/projects?status=all");
  const tasks = usePagedResource<Task>("/tasks", {
    kind,
    status,
    project_id: projectId,
    q,
  });
  const [detailError, setDetailError] = useState("");
  const [detailVersion, setDetailVersion] = useState(0);
  const [detailRetry, setDetailRetry] = useState(0);
  const [detailLoad, setDetailLoad] = useState<{
    id: string;
    status: "loading" | "ready" | "error";
    error: string;
  } | null>(null);
  const matchingDetail = detailLoad?.id === initialTaskId ? detailLoad : null;
  const detailLoading =
    Boolean(initialTaskId) &&
    (!matchingDetail || matchingDetail.status === "loading");
  const selected =
    matchingDetail?.status === "ready" && detailTask?.id === initialTaskId
      ? detailTask
      : null;
  const selectedId = useRef(selected?.id);
  selectedId.current = selected?.id;
  async function refreshDetail(taskId: string) {
    try {
      const task = await api<Task>(`/tasks/${encodeURIComponent(taskId)}`);
      if (selectedId.current !== taskId) return;
      if (task.id !== taskId)
        throw new Error(translate("服务返回了无法识别的响应。"));
      setSelected((current) =>
        current?.id === taskId &&
        Date.parse(task.updated_at) >= Date.parse(current.updated_at)
          ? task
          : current,
      );
      setDetailError("");
      refreshWallet();
    } catch (failure) {
      if (selectedId.current === taskId) setDetailError(errorMessage(failure));
    }
  }
  useEffect(() => {
    setDetailError("");
    if (!initialTaskId) {
      setSelected(null);
      setDetailLoad(null);
      return;
    }
    const taskId = initialTaskId;
    let live = true;
    setSelected((current) => (current?.id === taskId ? current : null));
    setDetailLoad({ id: taskId, status: "loading", error: "" });
    api<Task>(`/tasks/${encodeURIComponent(taskId)}`)
      .then((task) => {
        if (!live) return;
        if (task.id !== taskId)
          throw new Error(translate("服务返回了无法识别的响应。"));
        setSelected((current) =>
          current?.id === taskId &&
          Date.parse(current.updated_at) > Date.parse(task.updated_at)
            ? current
            : task,
        );
        setDetailLoad({ id: taskId, status: "ready", error: "" });
      })
      .catch((failure) => {
        if (live)
          setDetailLoad({
            id: taskId,
            status: "error",
            error: errorMessage(failure),
          });
      });
    return () => {
      live = false;
    };
  }, [initialTaskId, detailRetry]);
  useEffect(() => {
    if (
      tasks.loading ||
      tasks.error ||
      !tasks.data.items.some(
        (t) =>
          !["completed", "failed", "canceled"].includes(t.execution_status) ||
          (t.execution_status === "completed" &&
            ["pending", "archiving", "archive_retry"].includes(
              t.delivery_status,
            )),
      )
    )
      return;
    const timer = window.setTimeout(tasks.refresh, 4000);
    return () => clearTimeout(timer);
  }, [tasks.data, tasks.loading, tasks.error]);
  useEffect(() => {
    setSelected((current) => {
      if (!current) return null;
      const latest = tasks.data.items.find((t) => t.id === current.id);
      return latest &&
        new Date(latest.updated_at).getTime() >=
          new Date(current.updated_at).getTime()
        ? latest
        : current;
    });
  }, [tasks.data]);
  useEffect(() => {
    if (
      !selected ||
      (["completed", "failed", "canceled"].includes(
        selected.execution_status,
      ) &&
        !(
          selected.execution_status === "completed" &&
          ["pending", "archiving", "archive_retry"].includes(
            selected.delivery_status,
          )
        ))
    )
      return;
    let live = true;
    const taskId = selected.id;
    const timer = setTimeout(() => {
      void api<Task>(`/tasks/${taskId}`)
        .then((task) => {
          if (live) {
            if (task.id !== taskId)
              throw new Error(translate("服务返回了无法识别的响应。"));
            setSelected((current) =>
              current?.id === taskId &&
              Date.parse(task.updated_at) >= Date.parse(current.updated_at)
                ? task
                : current,
            );
            setDetailError("");
            refreshWallet();
          }
        })
        .catch((failure) => {
          if (live) setDetailError(errorMessage(failure));
        })
        .finally(() => {
          if (live) setDetailVersion((version) => version + 1);
        });
    }, 4000);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [
    selected?.id,
    selected?.execution_status,
    selected?.delivery_status,
    detailVersion,
  ]);
  async function cancel(task: Task) {
    setCanceling(true);
    try {
      const result = await api<Task>(`/tasks/${task.id}/cancel`, "POST", {});
      if (result.id !== task.id)
        throw new Error(translate("服务返回了无法识别的响应。"));
      setSelected((current) =>
        current?.id === task.id &&
        Date.parse(result.updated_at) >= Date.parse(current.updated_at)
          ? result
          : current,
      );
      tasks.refresh();
      refreshWallet();
      notify(translate("取消请求已处理，资金结果以任务结算状态为准"));
    } catch (reason) {
      notify(errorMessage(reason), true);
    } finally {
      setCanceling(false);
    }
  }
  return (
    <>
      <PageHeading
        eyebrow="EVERY EXPLORATION, RECORDED"
        title={translate("创作在继续")}
        description={translate(
          "追踪每次生成的输入、交付和积分状态。离开页面，任务也会继续执行。",
        )}
      >
        <button
          className="button secondary"
          onClick={() => {
            tasks.refresh();
            refreshWallet();
            if (selected) void refreshDetail(selected.id);
          }}
        >
          <Icon name="refresh" />
          {translate("刷新状态")}
        </button>
      </PageHeading>
      <div className="filter-bar">
        <select
          value={projectId}
          onChange={(e) => setProjectId(e.target.value)}
          aria-label={translate("按项目筛选")}
        >
          <option value="">{translate("全部项目")}</option>
          {projects.data.items.map((project) => (
            <option key={project.id} value={project.id}>
              {project.name}
              {project.status === "archived" ? translate("（已归档）") : ""}
            </option>
          ))}
        </select>
        <select
          value={kind}
          onChange={(e) => setKind(e.target.value)}
          aria-label={translate("任务类型")}
        >
          <option value="">{translate("所有类型")}</option>
          <option value="image">{translate("图片任务")}</option>
          <option value="video">{translate("视频任务")}</option>
        </select>
        <select
          value={status}
          onChange={(e) => setStatus(e.target.value)}
          aria-label={translate("任务状态")}
        >
          <option value="">{translate("所有执行状态")}</option>
          {[
            "queued",
            "sending",
            "submission_unknown",
            "generating",
            "completed",
            "failed",
            "canceled",
          ].map((v) => (
            <option key={v} value={v}>
              {statusLabel(v)}
            </option>
          ))}
        </select>
        <span className="muted">
          {!tasks.loading && !tasks.error && (
            <>
              {tasks.total}
              {translate("条生成记录")}
            </>
          )}
        </span>
        <div className="search-input">
          <Icon name="search" size={16} />
          <input
            aria-label={translate("搜索生成任务")}
            placeholder={translate("搜索任务编号、模型或提示词")}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            maxLength={200}
          />
        </div>
      </div>
      {projects.error && (
        <LoadingError
          loading={false}
          error={projects.error}
          retry={projects.refresh}
        />
      )}
      <LoadingError
        loading={tasks.loading}
        error={tasks.error}
        retry={tasks.refresh}
      />
      {!tasks.loading && !tasks.error && !tasks.data.items.length ? (
        <Empty
          icon="tasks"
          title={translate("你的探索，会被认真记录")}
          description={translate(
            "进入项目开始第一次生成。任务快照、作品和费用都可以在这里追溯。",
          )}
          action={
            <button
              className="button secondary"
              onClick={() => navigate("projects")}
            >
              {translate("前往项目空间")}
              <Icon name="arrow" size={16} />
            </button>
          }
        />
      ) : (
        tasks.data.items.length > 0 && (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>{translate("生成任务")}</th>
                  <th>{translate("执行状态")}</th>
                  <th>{translate("作品交付")}</th>
                  <th>{translate("积分结算")}</th>
                  <th>{translate("创建时间")}</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {tasks.data.items.map((t) => (
                  <tr key={t.id}>
                    <td>
                      <div className="task-title">
                        <span className="table-symbol">
                          <Icon name={t.kind === "video" ? "video" : "image"} />
                        </span>
                        <span>
                          <strong>{t.prompt || t.model}</strong>
                          <small>
                            {t.model} ·{" "}
                            <span className="mono">{t.id.slice(-10)}</span>
                            {t.demo ? translate(" · 模拟任务") : ""}
                          </small>
                        </span>
                      </div>
                    </td>
                    <td>
                      <Status value={t.execution_status} />
                    </td>
                    <td>
                      <Status value={t.delivery_status} />
                    </td>
                    <td>
                      <strong>
                        {t.credits}
                        {translate("积分")}
                      </strong>
                      <small className="cell-sub">
                        {statusLabel(t.settlement_status)}
                      </small>
                    </td>
                    <td className="muted nowrap">{formatDate(t.created_at)}</td>
                    <td>
                      <button
                        className="text-button"
                        onClick={() => {
                          setSelected(t);
                          navigate("tasks", t.id);
                        }}
                      >
                        {translate("详情")}
                        <Icon name="arrow" size={14} />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )
      )}
      {!tasks.error && (
        <Pagination
          page={tasks.page}
          size={tasks.size}
          total={tasks.total}
          loading={tasks.loading}
          onPage={tasks.setPage}
        />
      )}
      {initialTaskId && (
        <Modal
          title={translate("生成任务详情")}
          onClose={() => {
            setSelected(null);
            navigate("tasks");
          }}
          wide
        >
          <LoadingError
            loading={detailLoading}
            error={matchingDetail?.error || ""}
            retry={() => setDetailRetry((version) => version + 1)}
          />
          {selected && (
            <>
              {detailError && (
                <Notice error>
                  {detailError}
                  <button
                    className="text-button"
                    onClick={() => void refreshDetail(selected.id)}
                  >
                    {translate("刷新状态")}
                  </button>
                </Notice>
              )}
              {selected.demo && (
                <Notice>
                  {translate(
                    "此记录为开发环境模拟任务，结果不可作为真实模型验收依据。",
                  )}
                </Notice>
              )}
              <div className="detail-grid">
                <div>
                  <span>{translate("任务编号")}</span>
                  <strong className="mono">{selected.id}</strong>
                </div>
                <div>
                  <span>{translate("生成模型")}</span>
                  <strong>{selected.model}</strong>
                </div>
                <div>
                  <span>{translate("执行")}</span>
                  <Status value={selected.execution_status} />
                </div>
                <div>
                  <span>{translate("交付")}</span>
                  <Status value={selected.delivery_status} />
                </div>
                <div>
                  <span>{translate("固定费用")}</span>
                  <strong>
                    {selected.credits}
                    {translate("积分")}
                  </strong>
                </div>
                <div>
                  <span>{translate("结算")}</span>
                  <Status value={selected.settlement_status} />
                </div>
              </div>
              <label>
                {translate("生成提示词")}
                <div className="readonly-text">{selected.prompt}</div>
              </label>
              <label>
                {translate("输入与规格快照")}
                <pre className="snapshot">
                  {JSON.stringify(selected.input || {}, null, 2)}
                </pre>
              </label>
              {selected.error_message && (
                <Notice
                  error={
                    Boolean(selected.error_code) ||
                    ["failed", "submission_unknown"].includes(
                      selected.execution_status,
                    ) ||
                    ["error", "archive_review"].includes(
                      selected.delivery_status,
                    )
                  }
                >
                  {translate(selected.error_message)}
                </Notice>
              )}
              {selected.asset_ids?.length > 0 && (
                <TaskAssets
                  key={`${selected.id}:${JSON.stringify(selected.asset_ids)}`}
                  ids={selected.asset_ids}
                />
              )}
              <div className="modal-actions">
                <button
                  className="button secondary"
                  onClick={() => {
                    if (
                      selected.input &&
                      typeof selected.input === "object" &&
                      "product_swap" in selected.input
                    )
                      navigate("product-swap");
                    else navigate("project", selected.project_id);
                    setSelected(null);
                  }}
                >
                  {translate(
                    selected.input &&
                      typeof selected.input === "object" &&
                      "product_swap" in selected.input
                      ? "返回商品替换"
                      : "打开所属项目",
                  )}
                  <Icon name="arrow" size={16} />
                </button>
                {(selected.execution_status === "queued" ||
                  (selected.execution_status === "generating" &&
                    selected.product_snapshot?.capabilities
                      .supports_cancel)) && (
                  <button
                    className="button secondary"
                    disabled={canceling}
                    onClick={() => void cancel(selected)}
                  >
                    {canceling
                      ? translate("正在处理…")
                      : translate("请求取消任务")}
                  </button>
                )}
              </div>
            </>
          )}
        </Modal>
      )}
    </>
  );
}
function TaskAssets({ ids }: { ids: string[] }) {
  const [assets, setAssets] = useState<Asset[]>([]),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(true);
  useEffect(() => {
    let live = true;
    setLoading(true);
    setError("");
    Promise.all(ids.map((id) => api<Asset>(`/assets/${id}`)))
      .then((result) => {
        if (live) setAssets(result);
      })
      .catch((reason) => {
        if (live) setError(errorMessage(reason));
      })
      .finally(() => {
        if (live) setLoading(false);
      });
    return () => {
      live = false;
    };
  }, [ids]);
  return (
    <>
      <h3>{translate("已保存作品")}</h3>
      <AssetGrid assets={assets} error={error} loading={loading} />
    </>
  );
}
export function WalletPage() {
  const { system, notify, refreshWallet } = useStudio();
  const [paymentSearch] = useState(() => window.location.search);
  const returnedFromPayment = hasPaymentReturn(paymentSearch);
  const returnedReference = returnedOrderReference(paymentSearch);
  const returnHandled = useRef(false);
  const [ledgerSearch, setLedgerSearch] = useState(""),
    [eventType, setEventType] = useState(""),
    [orderSearch, setOrderSearch] = useState(""),
    [orderStatus, setOrderStatus] = useState(""),
    [checkingOrderId, setCheckingOrderId] = useState("");
  const ledgerQ = useDebouncedValue(ledgerSearch),
    orderQ = useDebouncedValue(orderSearch);
  const wallet = useResource<Wallet | null>("/wallet", null);
  const ledger = usePagedResource<Ledger>("/wallet/ledger", {
    q: ledgerQ,
    event_type: eventType,
  });
  const orders = usePagedResource<PaymentOrder>("/payment/orders", {
    q: orderQ,
    status: orderStatus,
  });
  const returnedOrder = useResource<{ items: PaymentOrder[] }>(
    returnedReference
      ? `/payment/orders?out_trade_no=${encodeURIComponent(returnedReference)}&limit=1&offset=0`
      : null,
    { items: [] },
  );
  const returnedOwnOrder = matchingReturnedOrder(
    paymentSearch,
    returnedOrder.data.items,
  );
  const tiers = useResource<{
    items: PaymentTier[];
  }>("/payment/tiers", {
    items: [],
  });
  const [selectedTier, setSelectedTier] = useState<PaymentTier | null>(null),
    [method, setMethod] = useState("alipay"),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [tab, setTab] = useState<"ledger" | "orders">(
      returnedFromPayment ? "orders" : "ledger",
    );
  const orderBusy = useRef(false);
  const checkBusy = useRef(false);
  const live = useRef(true);
  useEffect(() => {
    live.current = true;
    return () => {
      live.current = false;
    };
  }, []);
  const closeCheckout = () => {
    if (!orderBusy.current) setSelectedTier(null);
  };
  function refresh() {
    wallet.refresh();
    ledger.refresh();
    orders.refresh();
    returnedOrder.refresh();
    refreshWallet();
  }
  async function createOrder() {
    if (!selectedTier || orderBusy.current || !live.current) return;
    orderBusy.current = true;
    const version = sessionVersion();
    const current = () => live.current && isCurrentSession(version);
    setBusy(true);
    setError("");
    try {
      const order = await api<PaymentOrder>("/payment/orders", "POST", {
        tier_id: selectedTier.id,
        method,
      });
      if (!current()) return;
      orders.refresh();
      setSelectedTier(null);
      setTab("orders");
      if (order.checkout_url) window.location.assign(order.checkout_url);
      else notify(translate("订单已创建，请在订单记录中查询支付状态"));
    } catch (reason) {
      if (current()) setError(errorMessage(reason));
    } finally {
      orderBusy.current = false;
      if (current()) setBusy(false);
    }
  }
  async function check(order: PaymentOrder) {
    if (checkBusy.current || !live.current) return;
    checkBusy.current = true;
    const version = sessionVersion();
    const current = () => live.current && isCurrentSession(version);
    setCheckingOrderId(order.id);
    try {
      await api(`/payment/orders/${order.id}/check`, "POST", {});
      if (!current()) return;
      refresh();
      notify(translate("已向支付服务核查订单，到账以账本为准"));
    } catch (reason) {
      if (current()) notify(errorMessage(reason), true);
    } finally {
      checkBusy.current = false;
      if (current()) setCheckingOrderId("");
    }
  }
  useEffect(() => {
    if (
      !returnedFromPayment ||
      returnHandled.current ||
      returnedOrder.loading ||
      returnedOrder.error
    )
      return;
    returnHandled.current = true;
    const ownOrder = matchingReturnedOrder(
      paymentSearch,
      returnedOrder.data.items,
    );
    // Consume the reference so remounting the wallet cannot repeat this check.
    const remaining = new URLSearchParams(window.location.search);
    remaining.delete("out_trade_no");
    window.history.replaceState(
      {},
      "",
      `${window.location.pathname}${remaining.size ? `?${remaining}` : ""}${window.location.hash}`,
    );
    if (ownOrder) void check(ownOrder);
  }, [
    paymentSearch,
    returnedFromPayment,
    returnedOrder.loading,
    returnedOrder.error,
    returnedOrder.data,
  ]);
  useEffect(() => {
    if (
      orders.loading ||
      returnedOrder.loading ||
      ![...orders.data.items, ...returnedOrder.data.items].some(
        (o) =>
          o.payment_status === "pending" ||
          (o.payment_status === "paid" && o.credit_status !== "credited"),
      )
    )
      return;
    const timer = window.setTimeout(() => {
      orders.refresh();
      returnedOrder.refresh();
      wallet.refresh();
      ledger.refresh();
      refreshWallet();
    }, 6000);
    return () => clearTimeout(timer);
  }, [orders.data, returnedOrder.data, orders.loading, returnedOrder.loading]);
  return (
    <>
      <PageHeading
        eyebrow="CLEAR PRICING. MORE CREATING."
        title={translate("为下一个想法，留足空间")}
        description={translate("积分的每一次冻结、消费与退回，都有清晰记录。")}
      >
        <button className="button secondary" onClick={refresh}>
          <Icon name="refresh" />
          {translate("刷新余额")}
        </button>
      </PageHeading>
      {system?.demo && (
        <Notice>
          {translate("开发演示环境：余额为测试积分，真实充值已关闭。")}
        </Notice>
      )}
      {returnedFromPayment && (
        <Notice>
          {translate("已返回钱包。支付状态与积分到账以本账户订单和账本为准。")}
        </Notice>
      )}
      {returnedFromPayment && (
        <LoadingError
          loading={returnedOrder.loading}
          error={returnedOrder.error}
          retry={returnedOrder.refresh}
        />
      )}
      {returnedFromPayment &&
        !returnedOrder.loading &&
        !returnedOrder.error &&
        (returnedOwnOrder ? (
          <div className="payment-return-summary">
            <span>
              {translate("充值订单")}{" "}
              <strong className="mono">{returnedOwnOrder.out_trade_no}</strong>
            </span>
            <span>
              ¥ {(returnedOwnOrder.amount_fen / 100).toFixed(2)} ·{" "}
              {returnedOwnOrder.credits.toLocaleString()} {translate("积分")}
            </span>
            <span>
              {translate("支付")}{" "}
              <Status value={returnedOwnOrder.payment_status} />
            </span>
            <span>
              {translate("到账")}{" "}
              <Status value={returnedOwnOrder.credit_status} />
            </span>
            <button
              className="text-button"
              disabled={Boolean(checkingOrderId)}
              onClick={() => void check(returnedOwnOrder)}
            >
              {checkingOrderId === returnedOwnOrder.id
                ? translate("正在处理…")
                : translate("核查订单")}
            </button>
          </div>
        ) : (
          <Notice>
            {translate("未找到本账户对应订单，请在订单记录中核对。")}
          </Notice>
        ))}
      <div className="wallet-overview">
        <div className="wallet-primary">
          <span>
            {translate("当前可用积分")}
            <Icon name="spark" size={18} />
          </span>
          <strong>
            {wallet.data ? wallet.data.available.toLocaleString() : "—"}
            <small>CREDITS</small>
          </strong>
          <p>{translate("以服务端报价为准，每次生成前确认总价。")}</p>
        </div>
        <div className="wallet-secondary">
          <Icon name="lock" size={24} />
          <span>{translate("任务冻结积分")}</span>
          <strong>
            {wallet.data ? wallet.data.frozen.toLocaleString() : "—"}
          </strong>
          <p>{translate("作品保存后结算；失败按任务规则释放。")}</p>
        </div>
      </div>
      <LoadingError
        loading={wallet.loading}
        error={wallet.error}
        retry={wallet.refresh}
      />
      <div className="section-heading">
        <div>
          <h2>{translate("充值积分")}</h2>
          <p>{translate("支付成功并通过核验后，积分会自动加入此账户。")}</p>
        </div>
        <span className="subtle-label">{translate("人民币结算 / 易支付")}</span>
      </div>
      {!system?.payment_enabled && (
        <Notice>
          {translate(
            "充值暂未开放。管理员完成商户配置并上架充值档位后，可在这里购买积分。",
          )}
        </Notice>
      )}
      <LoadingError
        loading={tiers.loading}
        error={tiers.error}
        retry={tiers.refresh}
      />
      {tiers.data.items.length > 0 && (
        <div className="tier-grid">
          {tiers.data.items.map((tier) => (
            <button
              key={tier.id}
              className="tier-card"
              onClick={() => {
                if (orderBusy.current) return;
                setSelectedTier(tier);
                setError("");
              }}
              disabled={busy || !system?.payment_enabled || system?.demo}
            >
              <span>{tier.name}</span>
              <strong>
                {tier.credits.toLocaleString()}
                <small>{translate("积分")}</small>
              </strong>
              <footer>
                <span>¥ {(tier.amount_fen / 100).toFixed(2)}</span>
                <Icon name="arrow" size={18} />
              </footer>
            </button>
          ))}
        </div>
      )}
      <div className="collection-toolbar wallet-tabs">
        <div className="tab-list">
          <button
            className={tab === "ledger" ? "active" : ""}
            onClick={() => setTab("ledger")}
          >
            {translate("积分流水")}
          </button>
          <button
            className={tab === "orders" ? "active" : ""}
            onClick={() => setTab("orders")}
          >
            {translate("充值订单")}
          </button>
        </div>
      </div>
      <div className="filter-bar">
        {tab === "ledger" ? (
          <select
            aria-label={translate("流水类型")}
            value={eventType}
            onChange={(e) => setEventType(e.target.value)}
          >
            <option value="">{translate("全部流水类型")}</option>
            {[
              "adjustment",
              "freeze",
              "consumed",
              "released",
              "refund",
              "recharge",
            ].map((value) => (
              <option value={value} key={value}>
                {ledgerLabel(value)}
              </option>
            ))}
          </select>
        ) : (
          <select
            aria-label={translate("订单状态")}
            value={orderStatus}
            onChange={(e) => setOrderStatus(e.target.value)}
          >
            <option value="">{translate("全部订单状态")}</option>
            {["pending", "paid", "credited", "expired"].map((value) => (
              <option key={value} value={value}>
                {statusLabel(value)}
              </option>
            ))}
          </select>
        )}
        <div className="search-input">
          <Icon name="search" size={16} />
          <input
            aria-label={
              tab === "ledger"
                ? translate("搜索积分流水")
                : translate("搜索充值订单")
            }
            placeholder={
              tab === "ledger"
                ? translate("搜索原因、任务或订单编号")
                : translate("搜索订单编号")
            }
            value={tab === "ledger" ? ledgerSearch : orderSearch}
            maxLength={200}
            onChange={(e) =>
              tab === "ledger"
                ? setLedgerSearch(e.target.value)
                : setOrderSearch(e.target.value)
            }
          />
        </div>
      </div>
      {tab === "ledger" ? (
        <>
          <LoadingError
            loading={ledger.loading}
            error={ledger.error}
            retry={ledger.refresh}
          />
          {!ledger.loading &&
            !ledger.error &&
            (ledger.data.items.length ? (
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>{translate("时间")}</th>
                      <th>{translate("流水事项")}</th>
                      <th>{translate("可用积分变动")}</th>
                      <th>{translate("冻结积分变动")}</th>
                      <th>{translate("备注")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {ledger.data.items.map((row) => (
                      <tr key={row.id}>
                        <td className="muted nowrap">
                          {formatDate(row.created_at)}
                        </td>
                        <td>{ledgerLabel(row.event_type)}</td>
                        <td
                          className={row.available_delta > 0 ? "positive" : ""}
                        >
                          {row.available_delta > 0 ? "+" : ""}
                          {row.available_delta}
                        </td>
                        <td>
                          {row.frozen_delta > 0 ? "+" : ""}
                          {row.frozen_delta}
                        </td>
                        <td className="muted">{row.reason || "—"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <Empty
                icon="wallet"
                title={translate("还没有积分流水")}
                description={translate("充值、生成、释放与退款会记录在这里。")}
              />
            ))}
          {!ledger.error && (
            <Pagination
              page={ledger.page}
              size={ledger.size}
              total={ledger.total}
              loading={ledger.loading}
              onPage={ledger.setPage}
            />
          )}
        </>
      ) : (
        <>
          <LoadingError
            loading={orders.loading}
            error={orders.error}
            retry={orders.refresh}
          />
          {!orders.loading &&
            !orders.error &&
            (orders.data.items.length ? (
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>{translate("充值订单")}</th>
                      <th>{translate("金额 / 积分")}</th>
                      <th>{translate("支付")}</th>
                      <th>{translate("到账")}</th>
                      <th />
                    </tr>
                  </thead>
                  <tbody>
                    {orders.data.items.map((order) => (
                      <tr key={order.id}>
                        <td>
                          <strong className="mono">{order.out_trade_no}</strong>
                          <small className="cell-sub">
                            {formatDate(order.created_at)}
                          </small>
                        </td>
                        <td>
                          ¥ {(order.amount_fen / 100).toFixed(2)}
                          <small className="cell-sub">
                            {order.credits}
                            {translate("积分")}
                          </small>
                        </td>
                        <td>
                          <Status value={order.payment_status} />
                        </td>
                        <td>
                          <Status value={order.credit_status} />
                        </td>
                        <td>
                          <button
                            className="text-button"
                            disabled={Boolean(checkingOrderId)}
                            onClick={() => void check(order)}
                          >
                            {checkingOrderId === order.id
                              ? translate("正在处理…")
                              : translate("核查订单")}
                          </button>
                          {order.payment_status === "pending" &&
                            order.checkout_url && (
                              <a
                                className="text-button"
                                href={order.checkout_url}
                              >
                                {translate("继续支付")}
                              </a>
                            )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <Empty
                icon="wallet"
                title={translate("还没有充值订单")}
                description={translate(
                  "你可以随时查看支付状态与积分到账记录。",
                )}
              />
            ))}
          {!orders.error && (
            <Pagination
              page={orders.page}
              size={orders.size}
              total={orders.total}
              loading={orders.loading}
              onPage={orders.setPage}
            />
          )}
        </>
      )}
      {selectedTier && (
        <Modal
          title={translate("确认充值")}
          onClose={closeCheckout}
          closeDisabled={busy}
        >
          <div className="checkout-summary">
            <strong>
              {selectedTier.credits.toLocaleString()}{" "}
              <small>{translate("积分")}</small>
            </strong>
            <span>
              {translate("支付 ¥")}
              {(selectedTier.amount_fen / 100).toFixed(2)}
            </span>
          </div>
          <label>
            {translate("支付方式")}
            <select
              value={method}
              onChange={(e) => {
                if (!orderBusy.current) setMethod(e.target.value);
              }}
              disabled={busy}
            >
              <option value="alipay">{translate("支付宝")}</option>
              <option value="wxpay">{translate("微信支付")}</option>
            </select>
          </label>
          <Notice>
            {translate(
              "积分到账由服务端验签与查单确认。请保留订单编号，遇到延迟可主动核查。",
            )}
          </Notice>
          {error && <Notice error>{error}</Notice>}
          <div className="modal-actions">
            <button
              className="button secondary"
              onClick={closeCheckout}
              disabled={busy}
            >
              {translate("取消")}
            </button>
            <button
              className="button primary"
              onClick={() => void createOrder()}
              disabled={busy}
            >
              {busy ? translate("创建订单中…") : translate("前往收银台")}
              <Icon name="arrow" size={16} />
            </button>
          </div>
        </Modal>
      )}
    </>
  );
}
function ledgerLabel(value: string) {
  const map: Record<string, string> = {
    initial: translate("账户初始积分"),
    demo_grant: translate("演示测试积分"),
    freeze: translate("生成冻结"),
    consume: translate("生成消费"),
    consumed: translate("生成消费"),
    release: translate("释放冻结"),
    released: translate("释放冻结"),
    refund: translate("任务退款"),
    recharge: translate("充值到账"),
    adjustment: translate("运营调账"),
  };
  return map[value] || value;
}
type Session = {
  id: string;
  user_agent: string;
  created_at: string;
  last_seen_at: string;
  expires_at: string;
  current: boolean;
};
export function AccountPage() {
  const { user, notify, logout, endSession, system } = useStudio();
  const sessions = useResource<{
    items: Session[];
  }>("/auth/sessions", {
    items: [],
  });
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const busyRef = useRef(false);
  const live = useRef(true);
  useEffect(() => {
    live.current = true;
    return () => {
      live.current = false;
    };
  }, []);
  async function password(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busyRef.current || !live.current) return;
    busyRef.current = true;
    const version = sessionVersion();
    const current = () => live.current && isCurrentSession(version);
    const form = event.currentTarget,
      values = new FormData(form);
    setBusy(true);
    setError("");
    try {
      await api("/auth/change-password", "POST", {
        current_password: values.get("current_password"),
        password: values.get("password"),
      });
      if (!isCurrentSession(version)) return;
      if (live.current) form.reset();
      endSession(version, "密码已修改，请重新登录。");
    } catch (reason) {
      if (current()) setError(errorMessage(reason));
    } finally {
      busyRef.current = false;
      if (current()) setBusy(false);
    }
  }
  async function revoke(session: Session) {
    const version = sessionVersion();
    try {
      await api(`/auth/sessions/${session.id}`, "DELETE");
      if (!live.current || !isCurrentSession(version)) return;
      sessions.refresh();
      notify(translate("会话已撤销"));
    } catch (reason) {
      if (live.current && isCurrentSession(version))
        notify(errorMessage(reason), true);
    }
  }
  return (
    <>
      <PageHeading
        eyebrow="YOUR ACCOUNT & SECURITY"
        title={translate("安心，专注于创作")}
        description={translate(
          "管理你的账户与已登录设备。同一账户的设备共享项目和积分钱包。",
        )}
      />
      <div className="settings-grid">
        <section className="settings-panel">
          <h2>{translate("账户信息")}</h2>
          <dl className="account-details">
            <div>
              <dt>{translate("显示名称")}</dt>
              <dd>{user.display_name}</dd>
            </div>
            <div>
              <dt>{translate("邮箱地址")}</dt>
              <dd>{user.email}</dd>
            </div>
            <div>
              <dt>{translate("邮箱验证")}</dt>
              <dd>
                {user.email_verified
                  ? translate("已验证")
                  : translate("待验证")}
              </dd>
            </div>
            <div>
              <dt>{translate("注册时间")}</dt>
              <dd>{formatDate(user.created_at)}</dd>
            </div>
          </dl>
          <button
            className="button secondary"
            onClick={() =>
              void logout().catch((reason) =>
                notify(errorMessage(reason), true),
              )
            }
          >
            <Icon name="logout" size={16} />
            {translate("退出当前账户")}
          </button>
        </section>
        <section className="settings-panel">
          <h2>{translate("修改登录密码")}</h2>
          <form onSubmit={password}>
            {error && <Notice error>{error}</Notice>}
            <label>
              {translate("当前密码")}
              <input
                name="current_password"
                type="password"
                autoComplete="current-password"
                required
              />
            </label>
            <label>
              {translate("新密码")}
              <input
                name="password"
                type="password"
                autoComplete="new-password"
                minLength={10}
                maxLength={72}
                placeholder={translate("至少 10 个字符")}
                required
              />
            </label>
            <button className="button primary" disabled={busy}>
              {busy ? translate("保存中…") : translate("更新密码")}
            </button>
          </form>
        </section>
      </div>
      <section className="settings-panel sessions-panel">
        <div className="section-heading">
          <div>
            <h2>{translate("登录设备与会话")}</h2>
            <p>
              {translate("撤销不再使用的设备，账户积分与历史作品会继续保留。")}
            </p>
          </div>
          <button
            className="icon-button"
            onClick={sessions.refresh}
            aria-label={translate("刷新登录设备")}
          >
            <Icon name="refresh" />
          </button>
        </div>
        <LoadingError
          loading={sessions.loading}
          error={sessions.error}
          retry={sessions.refresh}
        />
        {sessions.data.items.map((session) => (
          <div className="session-row" key={session.id}>
            <span className="table-symbol">
              <Icon name="lock" />
            </span>
            <div>
              <strong>
                {session.user_agent || translate("未知设备")}
                {session.current && (
                  <span className="subtle-label">{translate("当前设备")}</span>
                )}
              </strong>
              <small>
                {translate("最近活跃")}
                {formatDate(session.last_seen_at)}
                {translate("· 到期")} {formatDate(session.expires_at)}
              </small>
            </div>
            {!session.current && (
              <button
                className="button secondary small"
                onClick={() => void revoke(session)}
              >
                {translate("撤销登录")}
              </button>
            )}
          </div>
        ))}
      </section>
      <div className="about-block">
        <Logo />
        <p>
          {translate("图片与视频创作工作室 ·")}
          {system?.version || "0.1.0"}
        </p>
        <p>
          {translate("画布交互基于")}{" "}
          <a
            href="https://github.com/basketikun/infinite-canvas/tree/3a6b64b602ad062b29fac66902ed18a7ef5fd9dc"
            target="_blank"
            rel="noreferrer"
          >
            infinite-canvas · basketikun
          </a>
          {translate("，固定版本 3a6b64b，采用")}{" "}
          <a
            href="/infinite-canvas-LICENSE.txt"
            target="_blank"
            rel="noreferrer"
          >
            {translate("MIT 许可证")}
          </a>
          。
        </p>
        <p>
          <a href="/third-party-notices.txt" target="_blank" rel="noreferrer">
            {translate("第三方组件与许可证清单")}
          </a>
        </p>
      </div>
    </>
  );
}
