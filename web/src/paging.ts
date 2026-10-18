export type PageResult<T> = {
  items: T[];
  total: number;
  limit: number;
  offset: number;
};
export function pageRequest(
  path: string,
  filters: Record<string, string | undefined>,
  page: number,
  size: number,
) {
  const [pathname, search = ""] = path.split("?");
  const params = new URLSearchParams(search);
  for (const [name, value] of Object.entries(filters)) {
    if (value) params.set(name, value);
    else params.delete(name);
  }
  params.set("limit", String(size));
  params.set("offset", String(Math.max(0, page) * size));
  return `${pathname}?${params}`;
}
export function returnedOrderReference(search: string): string | undefined {
  const refs = new URLSearchParams(search).getAll("out_trade_no");
  return refs.length === 1 && refs[0] ? refs[0] : undefined;
}

export async function readAllItems<T extends { id: string }>(
  path: string,
  fetchPage: (path: string) => Promise<PageResult<T>>,
): Promise<T[]> {
  const result = new Map<string, T>();
  let offset = 0;
  let total = 0;
  do {
    const [pathname, search = ""] = pageRequest(path, {}, 0, 100).split("?");
    const params = new URLSearchParams(search);
    params.set("offset", String(offset));
    const page = await fetchPage(`${pathname}?${params}`);
    if (
      !Array.isArray(page.items) ||
      !Number.isSafeInteger(page.total) ||
      page.total < 0 ||
      page.offset !== offset ||
      !Number.isSafeInteger(page.limit) ||
      page.limit < 1 ||
      page.items.length > page.limit
    )
      throw new Error("服务返回了无法识别的响应。");
    for (const item of page.items) {
      if (typeof item?.id !== "string" || !item.id)
        throw new Error("服务返回了无法识别的响应。");
      result.set(item.id, item);
    }
    offset += page.items.length;
    total = page.total;
    if (!page.items.length && offset < total)
      throw new Error("列表未完整加载，请重试。");
  } while (offset < total);
  if (result.size < total)
    throw new Error("列表在加载期间发生变化，请刷新后重试。");
  return [...result.values()];
}
