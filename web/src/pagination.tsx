import { useEffect, useMemo, useState } from "react";
import { api, errorMessage } from "./api";
import { Icon, useResource } from "./ui";
import { t } from "./i18n";
import { pageRequest, readAllItems, type PageResult } from "./paging";
import "./pagination.css";

export function useDebouncedValue(value: string, delay = 300) {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}
export function usePagedResource<T>(
  path: string,
  filters: Record<string, string | undefined> = {},
  refreshKey: unknown = 0,
  size = 25,
) {
  const key = JSON.stringify(filters);
  const [paging, setPaging] = useState({ key, page: 0 });
  const page = paging.key === key ? paging.page : 0;
  const resource = useResource<PageResult<T>>(
    pageRequest(path, filters, page, size),
    { items: [], total: 0, limit: size, offset: 0 },
    refreshKey,
  );
  const empty = useMemo<PageResult<T>>(
    () => ({ items: [], total: 0, limit: size, offset: page * size }),
    [page, size],
  );
  const data = resource.loading || resource.error ? empty : resource.data;
  useEffect(() => {
    if (
      !resource.loading &&
      !resource.error &&
      page > 0 &&
      resource.data.total <= page * size
    )
      setPaging({
        key,
        page: Math.max(0, Math.ceil(resource.data.total / size) - 1),
      });
  }, [resource.data, resource.loading, resource.error, page, key, size]);
  return {
    ...resource,
    data,
    items: data.items,
    total: data.total,
    page,
    size,
    setPage: (value: number) => setPaging({ key, page: value }),
  };
}
export function useAllItems<T extends { id: string }>(
  path: string | null,
  initial: { items: T[] } = { items: [] },
  refreshKey: unknown = 0,
) {
  const [data, setData] = useState(initial),
    [loading, setLoading] = useState(Boolean(path)),
    [error, setError] = useState(""),
    [version, setVersion] = useState(0);
  useEffect(() => {
    if (!path) {
      setLoading(false);
      return;
    }
    let live = true;
    setLoading(true);
    setError("");
    void (async () => {
      try {
        const all = await readAllItems<T>(path, async (request) => {
          if (!live) throw new Error("Canceled");
          const page = await api<PageResult<T>>(request);
          if (!live) throw new Error("Canceled");
          return page;
        });
        if (live) setData({ items: all });
      } catch (failure) {
        if (live) setError(errorMessage(failure));
      } finally {
        if (live) setLoading(false);
      }
    })();
    return () => {
      live = false;
    };
  }, [path, refreshKey, version]);
  return {
    data,
    setData,
    loading,
    error,
    refresh: () => setVersion((value) => value + 1),
  };
}
export function Pagination({
  page,
  size,
  total,
  loading,
  onPage,
}: {
  page: number;
  size: number;
  total: number;
  loading?: boolean;
  onPage: (page: number) => void;
}) {
  if (loading || (!total && page === 0)) return null;
  return (
    <nav className="resource-pagination" aria-label={t("列表分页")}>
      <span>{t("共 {0} 条记录", total.toLocaleString())}</span>
      <div>
        <button
          className="button secondary small"
          disabled={loading || page === 0}
          onClick={() => onPage(page - 1)}
        >
          <Icon name="back" size={14} />
          {t("上一页")}
        </button>
        <span>
          {t("第 {0} / {1} 页", page + 1, Math.max(1, Math.ceil(total / size)))}
        </span>
        <button
          className="button secondary small"
          disabled={loading || (page + 1) * size >= total}
          onClick={() => onPage(page + 1)}
        >
          {t("下一页")}
          <Icon name="arrow" size={14} />
        </button>
      </div>
    </nav>
  );
}
