import { useEffect, useId, useState } from "react";
import { api, errorMessage } from "./api";
import { t } from "./i18n";
import { Pagination } from "./pagination";
import { pageRequest, type PageResult } from "./paging";
import {
  assetReferenceTarget,
  validReferencePage,
  type AssetReference,
} from "./asset-references";
import type { Asset } from "./types";
import { Icon, LoadingError } from "./ui";
import "./asset-references.css";

type Navigate = (page: string, id?: string) => void;

export function AssetOrigin({
  asset,
  onNavigate,
}: {
  asset: Asset;
  onNavigate?: Navigate;
}) {
  return asset.task_id && onNavigate ? (
    <a
      className="asset-origin asset-origin-generated"
      href={`#/tasks/${encodeURIComponent(asset.task_id)}`}
      onClick={(event) => {
        event.preventDefault();
        onNavigate("tasks", encodeURIComponent(asset.task_id!));
      }}
      title={t("查看来源任务")}
    >
      <Icon name="spark" size={12} />
      {t("生成结果")}
      <Icon name="arrow" size={12} />
    </a>
  ) : (
    <span
      className={`asset-origin ${asset.task_id ? "asset-origin-generated" : ""}`}
    >
      <Icon name={asset.task_id ? "spark" : "upload"} size={12} />
      {asset.task_id ? t("生成结果") : t("上传文件")}
    </span>
  );
}

export function AssetReferences({
  assetId,
  onNavigate,
}: {
  assetId: string;
  onNavigate: Navigate;
}) {
  const [open, setOpen] = useState(false);
  const id = useId();
  return (
    <section className="asset-references">
      <button
        className="asset-references-toggle"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen(!open)}
      >
        <span>
          <Icon name="link" size={16} />
          {t("引用位置")}
        </span>
        <Icon name={open ? "up" : "down"} size={14} />
      </button>
      <div id={id} hidden={!open}>
        {open && (
          <ReferenceList
            key={assetId}
            assetId={assetId}
            onNavigate={onNavigate}
          />
        )}
      </div>
    </section>
  );
}

function ReferenceList({
  assetId,
  onNavigate,
}: {
  assetId: string;
  onNavigate: Navigate;
}) {
  const size = 25;
  const [page, setPage] = useState(0),
    [version, setVersion] = useState(0);
  const key = `${assetId}:${page}:${version}`;
  const [result, setResult] = useState<{
    key: string;
    data: PageResult<AssetReference> | null;
    error: string;
  } | null>(null);
  const current = result?.key === key ? result : null;
  const loading = current === null;
  const refresh = () => {
    setPage(0);
    setVersion((value) => value + 1);
  };
  useEffect(() => {
    let live = true;
    void api<PageResult<AssetReference>>(
      pageRequest(
        `/assets/${encodeURIComponent(assetId)}/references`,
        {},
        page,
        size,
      ),
    )
      .then((data) => {
        if (!validReferencePage(data, page * size, size))
          throw new Error(t("服务返回了无法识别的响应。"));
        if (!live) return;
        if (page > 0 && page * size >= data.total) {
          setPage(Math.max(0, Math.ceil(data.total / size) - 1));
          return;
        }
        setResult({ key, data, error: "" });
      })
      .catch((reason) => {
        if (live) setResult({ key, data: null, error: errorMessage(reason) });
      });
    return () => {
      live = false;
    };
  }, [assetId, page, key]);
  return (
    <div className="asset-references-body">
      <p className="field-hint">
        {t(
          "显示服务端已保存的引用；本地未保存更改不会计入。引用有变化时请刷新。",
        )}
      </p>
      <div className="asset-references-summary">
        <strong>
          {current?.data
            ? t("共 {0} 个引用位置", current.data.total.toLocaleString())
            : t("读取引用位置")}
        </strong>
        <button className="text-button" disabled={loading} onClick={refresh}>
          <Icon name="refresh" size={13} />
          {t("刷新")}
        </button>
      </div>
      <LoadingError
        loading={loading}
        error={current?.error || ""}
        retry={refresh}
      />
      {current?.data && (
        <>
          {current.data.items.length ? (
            <ul className="asset-reference-list">
              {current.data.items.map((reference) => (
                <ReferenceRow
                  key={`${reference.project_id}:${reference.owner_type}:${reference.owner_id}`}
                  reference={reference}
                  onNavigate={onNavigate}
                />
              ))}
            </ul>
          ) : (
            <p className="asset-references-empty">
              {t("当前没有已保存的引用。删除时仍以服务端实时检查为准。")}
            </p>
          )}
          <Pagination
            page={page}
            size={size}
            total={current.data.total}
            onPage={setPage}
          />
        </>
      )}
      <p className="asset-reference-help">
        {t(
          "画布和封面引用需在所属位置移除并保存。已归档项目修改前需恢复项目。历史引用、任务输入与来源保留为历史关联，不能手动解除。",
        )}
      </p>
    </div>
  );
}

function ReferenceRow({
  reference,
  onNavigate,
}: {
  reference: AssetReference;
  onNavigate: Navigate;
}) {
  const target = assetReferenceTarget(reference);
  const label =
    reference.owner_type === "canvas"
      ? t("画布文档")
      : reference.owner_type === "shot"
        ? t("历史引用")
        : reference.owner_type === "cover"
          ? t("项目封面")
          : t("任务输入或来源");
  const action =
    reference.owner_type === "task"
      ? t("查看任务")
      : reference.owner_type === "cover"
        ? t("查看项目素材")
        : t("查看画布");
  return (
    <li className="asset-reference-row">
      <div className="asset-reference-heading">
        <span className="asset-reference-kind">{label}</span>
        {reference.project_deleted ? (
          <span className="asset-reference-deleted">{t("项目已删除")}</span>
        ) : (
          reference.project_status === "archived" && (
            <span className="asset-reference-archived">{t("已归档")}</span>
          )
        )}
      </div>
      <strong>{reference.project_name || t("未命名项目")}</strong>
      {reference.owner_label && (
        <p className="asset-reference-owner-label">{reference.owner_label}</p>
      )}
      <dl>
        <div>
          <dt>{t("项目 ID")}</dt>
          <dd>
            <code>{reference.project_id}</code>
          </dd>
        </div>
        <div>
          <dt>{t("位置 ID")}</dt>
          <dd>
            <code>{reference.owner_id}</code>
          </dd>
        </div>
      </dl>
      {target ? (
        <a
          className="text-button"
          href={`#/${target.page}/${target.id}`}
          onClick={(event) => {
            event.preventDefault();
            onNavigate(target.page, target.id);
          }}
        >
          {action}
          <Icon name="arrow" size={13} />
        </a>
      ) : (
        <p className="asset-reference-help">
          {t("已删除项目无法打开；引用的保留与清理请按服务政策联系运营。")}
        </p>
      )}
    </li>
  );
}
