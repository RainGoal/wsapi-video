export type Route = { page: string; id?: string; query?: string };

export function routeFromLocation(location: {
  hash: string;
  pathname: string;
}): Route {
  const path = location.hash
    ? location.hash.replace(/^#\/?/, "")
    : /^\/wallet\/?$/.test(location.pathname)
      ? "wallet"
      : "";
  const queryIndex = path.indexOf("?");
  const routePath = queryIndex < 0 ? path : path.slice(0, queryIndex);
  const [page, id] = routePath.split("/");
  return {
    page: page || "projects",
    id,
    ...(queryIndex < 0 ? {} : { query: path.slice(queryIndex + 1) }),
  };
}

export function hasPaymentReturn(search: string): boolean {
  return new URLSearchParams(search).has("out_trade_no");
}

export function matchingReturnedOrder<T extends { out_trade_no: string }>(
  search: string,
  ownOrders: readonly T[],
): T | undefined {
  const references = new URLSearchParams(search).getAll("out_trade_no");
  if (references.length !== 1 || !references[0]) return undefined;
  return ownOrders.find((order) => order.out_trade_no === references[0]);
}
