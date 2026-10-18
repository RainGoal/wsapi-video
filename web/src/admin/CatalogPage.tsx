import { useRef, useState, type FormEvent } from "react";
import { api, errorMessage } from "../api";
import { useStudio } from "../App";
import type { Capabilities, PaymentTier, Product } from "../types";
import {
  productCapabilities,
  productDefinitionChanged,
} from "./catalog-product";
import { Empty, Icon, LoadingError, Modal, Notice, useResource } from "../ui";
import {
  a,
  Check,
  decimalFen,
  Field,
  money,
  Pager,
  Reason,
  SaveBar,
} from "./common";

type AdminProduct = Product & { active: boolean; verified: boolean };
const blankCapabilities: Capabilities = {
  aspect_ratios: ["1:1"],
  durations: [],
  resolutions: [],
  sizes: [],
  qualities: [],
  max_images: 0,
  max_videos: 0,
  max_audios: 0,
  supports_audio: false,
  supports_frames: false,
  supports_cancel: false,
};
const blankProduct = (): AdminProduct => ({
  id: "",
  name: "",
  kind: "image",
  model: "",
  credits: 10,
  version: 0,
  capabilities: { ...blankCapabilities },
  active: false,
  verified: false,
  demo: false,
});
const blankTier = (): PaymentTier => ({
  id: "",
  name: "",
  amount_fen: 1000,
  credits: 100,
  active: false,
  version: 0,
});
export function CatalogPage({ kind }: { kind: "products" | "tiers" }) {
  const { notify } = useStudio();
  const resource = useResource<{ items: (AdminProduct | PaymentTier)[] }>(
    kind === "products" ? "/admin/model-products" : "/admin/payment/tiers",
    { items: [] },
  );
  const [editing, setEditing] = useState<AdminProduct | PaymentTier | null>(
      null,
    ),
    [editingBusy, setEditingBusy] = useState(false),
    [search, setSearch] = useState(""),
    [status, setStatus] = useState(""),
    [page, setPage] = useState(0);
  const editor = useRef<{
    record: AdminProduct | PaymentTier | null;
    busy: boolean;
  }>({ record: null, busy: false });
  const rows = resource.data.items.filter(
    (row) =>
      (!search ||
        `${row.id} ${row.name} ${"model" in row ? row.model : ""}`
          .toLowerCase()
          .includes(search.toLowerCase())) &&
      (!status || (status === "active" ? row.active : !row.active)),
  );
  function beginEditing(record: AdminProduct | PaymentTier) {
    if (editor.current.busy) return;
    editor.current = { record, busy: false };
    setEditing(record);
    setEditingBusy(false);
  }
  function closeEditing() {
    if (editor.current.busy) return;
    editor.current.record = null;
    setEditing(null);
  }
  function reportBusy(record: AdminProduct | PaymentTier, busy: boolean) {
    if (editor.current.record !== record) return;
    editor.current.busy = busy;
    setEditingBusy(busy);
  }
  function done(record: AdminProduct | PaymentTier) {
    if (editor.current.record !== record) return;
    editor.current = { record: null, busy: false };
    setEditing(null);
    setEditingBusy(false);
    resource.refresh();
    notify(a("业务配置已保存并记录审计", "Configuration saved and audited"));
  }
  return (
    <>
      <div className="admin-section-toolbar">
        <div>
          <h2>
            {kind === "products"
              ? a("可售模型与能力规格", "Models and capabilities")
              : a("积分充值套餐", "Credit packages")}
          </h2>
          <p>
            {kind === "products"
              ? a(
                  "单次生成按固定商品积分报价，已接受任务保留原价格和能力快照。",
                  "Each generation is quoted using the product's fixed credit price. Accepted tasks retain their original price and capabilities.",
                )
              : a(
                  "金额以人民币元填写、服务端以整数分保存；支付确认后唯一入账。",
                  "Enter the RMB price; the server stores integer fen and credits each confirmed order exactly once.",
                )}
          </p>
        </div>
        <button
          className="button primary"
          onClick={() =>
            beginEditing(kind === "products" ? blankProduct() : blankTier())
          }
        >
          <Icon name="plus" size={16} />
          {kind === "products"
            ? a("新增模型商品", "New model product")
            : a("新增充值档位", "New credit package")}
        </button>
      </div>
      <Notice>
        {kind === "products"
          ? a(
              "只有完成真实生成、交付和计费核验的能力才应标记已验证。上架还需要对应服务凭证。",
              "Mark capabilities verified only after real generation, delivery and billing checks. Publishing also requires the matching gateway credentials.",
            )
          : a(
              "上架充值档位前必须先启用并配置真实支付渠道。开发演示环境不能收取真实款项。",
              "Enable and configure real payments before publishing packages. Demo environments cannot collect real money.",
            )}
      </Notice>
      <div className="admin-filter-bar">
        <div className="search-box">
          <Icon name="search" />
          <input
            aria-label={a("搜索配置", "Search configuration")}
            placeholder={a("搜索名称、模型或编号", "Search name, model or ID")}
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
              setPage(0);
            }}
          />
        </div>
        <select
          aria-label={a("上架状态", "Publication status")}
          value={status}
          onChange={(e) => {
            setStatus(e.target.value);
            setPage(0);
          }}
        >
          <option value="">{a("全部状态", "All statuses")}</option>
          <option value="active">{a("已上架", "Published")}</option>
          <option value="inactive">{a("已下架", "Unpublished")}</option>
        </select>
        <button
          className="icon-button"
          title={a("刷新列表", "Refresh list")}
          onClick={resource.refresh}
        >
          <Icon name="refresh" />
        </button>
      </div>
      <LoadingError
        loading={resource.loading}
        error={resource.error}
        retry={resource.refresh}
      />
      {!resource.loading &&
        !resource.error &&
        (rows.length ? (
          <>
            <div className="table-wrap admin-table">
              <table>
                <thead>
                  <tr>
                    <th>
                      {kind === "products"
                        ? a("商品 / 模型", "Product / model")
                        : a("充值档位", "Credit package")}
                    </th>
                    <th>
                      {kind === "products"
                        ? a("类型 / 能力", "Type / capabilities")
                        : a("金额", "Price")}
                    </th>
                    <th>{a("积分", "Credits")}</th>
                    <th>{a("发布状态", "Publication")}</th>
                    <th>{a("版本", "Version")}</th>
                    <th>{a("操作", "Actions")}</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.slice(page * 25, page * 25 + 25).map((row) => (
                    <tr key={row.id}>
                      <td>
                        <strong>{row.name}</strong>
                        <small className="cell-sub">
                          {"model" in row ? String(row.model) : row.id}
                        </small>
                        {"demo" in row && row.demo === true && (
                          <span className="demo-badge">
                            {a("演示商品", "Demo product")}
                          </span>
                        )}
                      </td>
                      <td>
                        {"kind" in row ? (
                          <>
                            <span>
                              {row.kind === "image"
                                ? a("图片", "Image")
                                : a("视频", "Video")}
                            </span>
                            <small className="cell-sub muted">
                              {(
                                row as AdminProduct
                              ).capabilities.aspect_ratios.join(" · ") || "—"}
                            </small>
                          </>
                        ) : (
                          money(row.amount_fen)
                        )}
                      </td>
                      <td>
                        <strong className="mono">
                          {row.credits.toLocaleString()}
                        </strong>
                      </td>
                      <td>
                        <span
                          className={`admin-pill ${row.active ? "positive" : "neutral"}`}
                        >
                          {row.active
                            ? a("已上架", "Published")
                            : a("已下架", "Unpublished")}
                        </span>
                        {"verified" in row && (
                          <small className="cell-sub muted">
                            {row.verified
                              ? a("能力已验证", "Capabilities verified")
                              : a("能力待验证", "Needs verification")}
                          </small>
                        )}
                      </td>
                      <td className="mono muted">v{row.version}</td>
                      <td>
                        <button
                          className="button secondary small"
                          onClick={() => beginEditing(structuredClone(row))}
                        >
                          {a("编辑配置", "Edit")}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <Pager
              total={rows.length}
              count={Math.min(25, rows.length - page * 25)}
              page={page}
              onPage={setPage}
            />
          </>
        ) : (
          <Empty
            icon="settings"
            title={a("没有匹配的配置", "No matching configuration")}
            description={a(
              "调整筛选条件，或新建并验证业务配置。",
              "Adjust your filters or create and verify a new configuration.",
            )}
          />
        ))}
      {editing && (
        <Modal
          title={
            kind === "products"
              ? a(
                  editing.version ? "编辑模型商品" : "新增模型商品",
                  editing.version ? "Edit model product" : "New model product",
                )
              : a(
                  editing.version ? "编辑充值档位" : "新增充值档位",
                  editing.version
                    ? "Edit credit package"
                    : "New credit package",
                )
          }
          onClose={closeEditing}
          closeDisabled={editingBusy}
          wide
        >
          {kind === "products" ? (
            <ProductForm
              initial={editing as AdminProduct}
              onDone={() => done(editing)}
              onCancel={closeEditing}
              onBusyChange={(busy) => reportBusy(editing, busy)}
            />
          ) : (
            <TierForm
              initial={editing as PaymentTier}
              onDone={() => done(editing)}
              onCancel={closeEditing}
              onBusyChange={(busy) => reportBusy(editing, busy)}
            />
          )}
        </Modal>
      )}
    </>
  );
}
type LimitKey =
  | "max_reference_duration_seconds"
  | "max_reference_bytes"
  | "max_reference_width"
  | "max_reference_height";
function ProductForm({
  initial,
  onDone,
  onCancel,
  onBusyChange,
}: {
  initial: AdminProduct;
  onDone: () => void;
  onCancel: () => void;
  onBusyChange: (busy: boolean) => void;
}) {
  const { system } = useStudio();
  const [draft, setDraft] = useState(initial),
    [reason, setReason] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const [lists, setLists] = useState({
    aspect_ratios: initial.capabilities.aspect_ratios.join(", "),
    durations: initial.capabilities.durations.join(", "),
    resolutions: initial.capabilities.resolutions.join(", "),
    sizes: initial.capabilities.sizes.join(", "),
    qualities: initial.capabilities.qualities.join(", "),
  });
  const capabilities = productCapabilities(
    draft.kind,
    draft.capabilities,
    lists,
  );
  const definitionChanged = productDefinitionChanged(initial, {
    ...draft,
    capabilities,
  });
  const verified = !definitionChanged && draft.verified;
  const active = verified && draft.active;
  function cap(key: keyof Capabilities | LimitKey, value: unknown) {
    setDraft((old) => ({
      ...old,
      capabilities: { ...old.capabilities, [key]: value },
    }));
  }
  const list = (key: keyof typeof lists, label: string, hint: string) => (
    <Field label={label} hint={hint}>
      <input
        value={lists[key]}
        onChange={(e) => setLists((old) => ({ ...old, [key]: e.target.value }))}
      />
    </Field>
  );
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    onBusyChange(true);
    setError("");
    setBusy(true);
    try {
      if (
        capabilities.durations.some(
          (v) => !Number.isSafeInteger(v) || v < 1 || v > 600,
        )
      )
        throw new Error(
          a(
            "视频时长必须为 1–600 的整数秒。",
            "Video durations must be integer seconds between 1 and 600.",
          ),
        );
      if (!definitionChanged && draft.active && !draft.verified)
        throw new Error(
          a(
            "请完成能力验证后再上架。",
            "Verify capabilities before publishing.",
          ),
        );
      await api(
        `/admin/model-products/${encodeURIComponent(draft.id.trim())}`,
        "PUT",
        {
          ...draft,
          id: draft.id.trim(),
          name: draft.name.trim(),
          model: draft.model.trim(),
          capabilities,
          active,
          verified,
          reason,
        },
      );
      onDone();
    } catch (failure) {
      setError(errorMessage(failure));
    } finally {
      setBusy(false);
      onBusyChange(false);
    }
  }
  return (
    <form className="admin-editor-form" onSubmit={submit}>
      <fieldset className="admin-edit-fieldset" disabled={busy}>
        <div className="admin-form-grid">
          <Field
            label={a("商品编号", "Product ID")}
            hint={a(
              "创建后固定，用于识别报价和历史任务。",
              "Fixed after creation; identifies quotes and historical tasks.",
            )}
          >
            <input
              value={draft.id}
              disabled={initial.version > 0}
              onChange={(e) => setDraft({ ...draft, id: e.target.value })}
              required
              maxLength={100}
              pattern="[a-zA-Z0-9_-]+"
              placeholder="image-model-standard"
            />
          </Field>
          <Field label={a("商品名称", "Product name")}>
            <input
              value={draft.name}
              onChange={(e) => setDraft({ ...draft, name: e.target.value })}
              required
              maxLength={200}
            />
          </Field>
          <Field label={a("媒体类型", "Media type")}>
            <select
              value={draft.kind}
              onChange={(e) =>
                setDraft({
                  ...draft,
                  kind: e.target.value as "image" | "video",
                })
              }
            >
              <option value="image">{a("图片", "Image")}</option>
              <option value="video">{a("视频", "Video")}</option>
            </select>
          </Field>
          <Field label={a("上游模型名称", "Provider model ID")}>
            <input
              value={draft.model}
              onChange={(e) => setDraft({ ...draft, model: e.target.value })}
              required
              maxLength={200}
            />
          </Field>
          <Field
            label={a("单次生成积分", "Credits per generation")}
            hint={a(
              "正整数；同一商品的所有可选规格使用此固定价格。",
              "Positive integer. All selectable specifications for this product use this fixed price.",
            )}
          >
            <input
              type="number"
              step={1}
              min={1}
              max={1000000000}
              value={draft.credits}
              onChange={(e) =>
                setDraft({ ...draft, credits: Number(e.target.value) })
              }
              required
            />
          </Field>
          <Field label={a("当前版本", "Current version")}>
            <input
              value={initial.version ? `v${initial.version}` : a("新建", "New")}
              disabled
            />
          </Field>
        </div>
        <h3 className="admin-form-section">
          {a("可选生成规格", "Generation specifications")}
        </h3>
        <div className="admin-form-grid">
          {list(
            "aspect_ratios",
            a("画面比例", "Aspect ratios"),
            a(
              "逗号分隔，例如 1:1, 16:9, 9:16",
              "Comma separated, e.g. 1:1, 16:9, 9:16",
            ),
          )}
          {draft.kind === "image" ? (
            list(
              "sizes",
              a("图片尺寸", "Image sizes"),
              a(
                "供应端支持的精确值，例如 1024x1024",
                "Exact provider values, e.g. 1024x1024",
              ),
            )
          ) : (
            <>
              {list(
                "durations",
                a("视频时长（秒）", "Video durations (seconds)"),
                a(
                  "整数秒，逗号分隔，例如 5, 10",
                  "Integer seconds separated by commas, e.g. 5, 10",
                ),
              )}
              {list(
                "resolutions",
                a("视频分辨率", "Video resolutions"),
                a(
                  "供应端支持的精确值，例如 720p, 1080p",
                  "Exact provider values, e.g. 720p, 1080p",
                ),
              )}
            </>
          )}
          {draft.kind === "image" &&
            list(
              "qualities",
              a("质量档位", "Quality options"),
              a(
                "填写真实支持值；没有该参数时留空。",
                "Use supported values; leave blank when unsupported.",
              ),
            )}
        </div>
        <h3 className="admin-form-section">
          {a("参考素材限制", "Reference media limits")}
        </h3>
        <div className="admin-form-grid">
          {(
            [
              ...[["max_images", a("图片数量上限", "Maximum images"), 16]],
              ...(draft.kind === "video"
                ? [
                    ["max_videos", a("视频数量上限", "Maximum videos"), 8],
                    ["max_audios", a("音频数量上限", "Maximum audio files"), 8],
                  ]
                : []),
            ] as [keyof Capabilities, string, number][]
          ).map(([key, label, max]) => (
            <Field key={key} label={label}>
              <input
                type="number"
                min={0}
                max={max}
                step={1}
                value={Number(draft.capabilities[key] || 0)}
                onChange={(e) => cap(key, Number(e.target.value))}
              />
            </Field>
          ))}
          {(
            [
              [
                "max_reference_duration_seconds",
                a(
                  "参考总时长上限（秒）",
                  "Maximum total reference duration (s)",
                ),
                86400,
              ],
              [
                "max_reference_bytes",
                a(
                  "参考总大小上限（字节）",
                  "Maximum total reference size (bytes)",
                ),
                1099511627776,
              ],
              [
                "max_reference_width",
                a("参考宽度上限（像素）", "Maximum reference width (px)"),
                32768,
              ],
              [
                "max_reference_height",
                a("参考高度上限（像素）", "Maximum reference height (px)"),
                32768,
              ],
            ] as [LimitKey, string, number][]
          ).map(([key, label, max]) => (
            <Field
              key={key}
              label={label}
              hint={
                key === "max_reference_bytes"
                  ? a(
                      "所有参考素材合计，包含首尾帧。0 不设商品额外上限，素材仍需通过平台验证。",
                      "Total across all references, including first and last frames. 0 adds no product limit; platform media validation still applies.",
                    )
                  : key === "max_reference_duration_seconds"
                    ? a(
                        "视频与音频参考的总时长。0 不设商品额外上限，素材仍需通过平台验证。",
                        "Combined duration of video and audio references. 0 adds no product limit; platform media validation still applies.",
                      )
                    : a(
                        "0 不设商品额外上限，素材仍需通过平台验证。",
                        "0 adds no product limit; platform media validation still applies.",
                      )
              }
            >
              <input
                type="number"
                min={0}
                max={max}
                step={1}
                value={Number(
                  (draft.capabilities as unknown as Record<string, unknown>)[
                    key
                  ] || 0,
                )}
                onChange={(e) => cap(key, Number(e.target.value))}
              />
            </Field>
          ))}
        </div>
        {draft.kind === "video" && (
          <div className="admin-checkbox-grid">
            <Check
              label={a("支持生成音频", "Supports generated audio")}
              checked={draft.capabilities.supports_audio}
              onChange={(v) => cap("supports_audio", v)}
            />
            <Check
              label={a("支持首尾帧", "Supports start/end frames")}
              checked={draft.capabilities.supports_frames}
              onChange={(v) => cap("supports_frames", v)}
            />
            <Check
              label={a("支持取消任务", "Supports cancellation")}
              checked={draft.capabilities.supports_cancel}
              onChange={(v) => cap("supports_cancel", v)}
            />
          </div>
        )}
        <h3 className="admin-form-section">
          {a("验证与发布", "Verification and publication")}
        </h3>
        {definitionChanged && (
          <Notice>
            {a(
              "模型或能力已变更。保存后商品将下架并清除旧验证。请重新打开已保存的版本，完成真实生成、归档与计费核验后再验证上架。",
              "The model or capabilities changed. Saving unpublishes this product and clears its previous verification. Reopen the saved version and verify real generation, archiving and billing before publishing it again.",
            )}
          </Notice>
        )}
        <Check
          label={a(
            "已完成真实生成、归档与计费验证",
            "Real generation, archiving and billing have been verified",
          )}
          checked={verified}
          disabled={definitionChanged}
          onChange={(v) =>
            setDraft({
              ...draft,
              verified: v,
              active: v ? draft.active : false,
            })
          }
        />
        <Check
          label={a(
            "上架商品，允许用户报价与生成",
            "Publish this product for quotes and generation",
          )}
          checked={active}
          disabled={definitionChanged || !verified}
          onChange={(v) => setDraft({ ...draft, active: v })}
        />
        {system?.demo && (
          <Check
            label={a(
              "开发演示商品（仅图片，不调用真实模型）",
              "Demo product (images only; no real model call)",
            )}
            checked={draft.demo}
            disabled={initial.version > 0}
            onChange={(v) => setDraft({ ...draft, demo: v })}
          />
        )}
        <Reason value={reason} onChange={setReason} />
        <SaveBar busy={busy} error={error} onCancel={onCancel} />
      </fieldset>
    </form>
  );
}
function TierForm({
  initial,
  onDone,
  onCancel,
  onBusyChange,
}: {
  initial: PaymentTier;
  onDone: () => void;
  onCancel: () => void;
  onBusyChange: (busy: boolean) => void;
}) {
  const [draft, setDraft] = useState(initial),
    [amount, setAmount] = useState((initial.amount_fen / 100).toFixed(2)),
    [reason, setReason] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    const fen = decimalFen(amount);
    if (fen === null) {
      setError(
        a(
          "金额须大于0、最多两位小数，最高100万元。",
          "Price must be positive, have at most 2 decimal places and not exceed ¥1,000,000.",
        ),
      );
      return;
    }
    onBusyChange(true);
    setBusy(true);
    setError("");
    try {
      await api(
        `/admin/payment/tiers/${encodeURIComponent(draft.id.trim())}`,
        "PUT",
        { ...draft, amount_fen: fen, reason },
      );
      onDone();
    } catch (failure) {
      setError(errorMessage(failure));
    } finally {
      setBusy(false);
      onBusyChange(false);
    }
  }
  return (
    <form className="admin-editor-form" onSubmit={submit}>
      <fieldset className="admin-edit-fieldset" disabled={busy}>
        <div className="admin-form-grid">
          <Field label={a("档位编号", "Package ID")}>
            <input
              required
              maxLength={100}
              pattern="[a-zA-Z0-9_-]+"
              value={draft.id}
              disabled={initial.version > 0}
              onChange={(e) => setDraft({ ...draft, id: e.target.value })}
            />
          </Field>
          <Field label={a("档位名称", "Package name")}>
            <input
              required
              maxLength={100}
              value={draft.name}
              onChange={(e) => setDraft({ ...draft, name: e.target.value })}
            />
          </Field>
          <Field
            label={a("售价（人民币元）", "Price (RMB)")}
            hint={a(
              "精确到分，不使用浮点近似入账。",
              "Exact to fen; no floating-point rounding in payment credit.",
            )}
          >
            <input
              required
              inputMode="decimal"
              pattern="[0-9]+(\.[0-9]{1,2})?"
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
            />
          </Field>
          <Field label={a("到账积分", "Credits granted")}>
            <input
              required
              type="number"
              min={1}
              max={1000000000}
              step={1}
              value={draft.credits}
              onChange={(e) =>
                setDraft({ ...draft, credits: Number(e.target.value) })
              }
            />
          </Field>
        </div>
        <div className="admin-package-preview">
          <div>
            <small>{draft.name || a("充值套餐预览", "Package preview")}</small>
            <strong>
              {decimalFen(amount) ? money(decimalFen(amount)!) : "—"}
            </strong>
          </div>
          <Icon name="arrow" />
          <div>
            <small>{a("到账积分", "Credits granted")}</small>
            <strong>
              {draft.credits.toLocaleString()} <span>CREDITS</span>
            </strong>
          </div>
        </div>
        <Check
          label={a(
            "上架并允许创建支付订单",
            "Publish and allow payment orders",
          )}
          checked={draft.active}
          onChange={(v) => setDraft({ ...draft, active: v })}
        />
        <Reason value={reason} onChange={setReason} />
        <SaveBar busy={busy} error={error} onCancel={onCancel} />
      </fieldset>
    </form>
  );
}
