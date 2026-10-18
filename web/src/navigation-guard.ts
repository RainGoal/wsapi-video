type NavigationWindow = Pick<
  Window,
  "history" | "location" | "addEventListener" | "removeEventListener"
>;
type Position = { index: number; href: string };
const indexKey = "__frame_history_index";

function entryIndex(state: unknown): number | null {
  const value =
    state && typeof state === "object"
      ? (state as Record<string, unknown>)[indexKey]
      : undefined;
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0
    ? value
    : null;
}
function indexedState(state: unknown, index: number) {
  return {
    ...(state && typeof state === "object" ? state : {}),
    [indexKey]: index,
  };
}

export function createNavigationGuard(
  browser: NavigationWindow,
  allow: () => boolean,
  changed: () => void,
) {
  const { history, location } = browser;
  const originalReplace = history.replaceState;
  let current: Position = {
    index: entryIndex(history.state) ?? 0,
    href: location.href,
  };
  let restoring = false;
  originalReplace.call(
    history,
    indexedState(history.state, current.index),
    "",
    location.href,
  );

  // Token/query cleanup replaces this entry without becoming a navigation.
  const replace: History["replaceState"] = (state, unused, url) => {
    const index = entryIndex(history.state) ?? current.index;
    originalReplace.call(history, indexedState(state, index), unused, url);
    if (!restoring && index === current.index)
      current = { index, href: location.href };
  };
  history.replaceState = replace;

  const handle = () => {
    const href = location.href;
    let index = entryIndex(history.state);
    if (restoring) {
      if (index === current.index && href === current.href) restoring = false;
      return;
    }
    if (index === current.index && href === current.href) return;
    // Native fragment navigation adds an entry without our history marker.
    if (index === null || index === current.index) {
      index = current.index + 1;
      originalReplace.call(
        history,
        indexedState(history.state, index),
        "",
        href,
      );
    }
    if (!allow()) {
      restoring = true;
      // Traverse back to the accepted entry; retain the rejected destination.
      history.go(current.index - index);
      return;
    }
    current = { index, href };
    changed();
  };
  browser.addEventListener("popstate", handle);
  browser.addEventListener("hashchange", handle);

  return {
    navigate(hash: string) {
      if (restoring) return;
      const target = new URL(location.href);
      target.hash = hash;
      if (target.href === location.href || !allow()) return;
      const index = current.index + 1;
      history.pushState(indexedState(null, index), "", target.href);
      current = { index, href: location.href };
      changed();
    },
    dispose() {
      browser.removeEventListener("popstate", handle);
      browser.removeEventListener("hashchange", handle);
      if (history.replaceState === replace)
        history.replaceState = originalReplace;
    },
  };
}

export type NavigationGuard = ReturnType<typeof createNavigationGuard>;
