// Restore entry before discard prompt, block Next.js popstate until approved traversal
const MARKER = "__obstorLeaveGuard";

interface Entry {
  session: string;
  index: number;
}

export interface HistoryLeave {
  href: string;
  resume: () => void;
}

interface HistoryWindow {
  history: History;
  location: { href: string };
  navigation?: { currentEntry: { index: number } | null };
  addEventListener: Window["addEventListener"];
  removeEventListener: Window["removeEventListener"];
}

function entry(state: unknown): Entry | null {
  if (!state || typeof state !== "object") return null;
  const value = (state as Record<string, unknown>)[MARKER];
  if (!value || typeof value !== "object") return null;
  const candidate = value as Entry;
  return typeof candidate.session === "string" && Number.isInteger(candidate.index)
    ? candidate
    : null;
}

function route(href: string): string {
  const url = new URL(href);
  return url.origin + url.pathname + url.search;
}

export function installLeaveHistory(
  win: HistoryWindow,
  dirty: () => boolean,
  blocked: (leave: HistoryLeave) => void,
): () => void {
  const history = win.history;
  const push = history.pushState;
  const replace = history.replaceState;
  const initial = entry(history.state);
  const session = initial?.session ?? Math.random().toString(36).slice(2);
  let index = initial?.index ?? 0;
  let href = win.location.href;
  let frameworkState: unknown = history.state;
  let disposed = false;
  let restoring: { index: number; leave: HistoryLeave } | null = null;
  let restoreTask: ReturnType<typeof setTimeout> | null = null;
  const position = () => win.navigation?.currentEntry?.index ?? entry(history.state)?.index;
  const stamped = (data: unknown, at: number) => ({
    ...(data && typeof data === "object" ? data : {}),
    [MARKER]: { session, index: at },
  });
  replace.call(history, stamped(history.state, index), "");
  index = position() ?? index;

  const trackedPush: History["pushState"] = (data, unused, url) => {
    push.call(history, disposed ? data : stamped(data, index + 1), unused, url);
    index = position() ?? index + 1;
    href = win.location.href;
    frameworkState = history.state;
  };
  const trackedReplace: History["replaceState"] = (data, unused, url) => {
    replace.call(history, disposed ? data : stamped(data, index), unused, url);
    href = win.location.href;
    frameworkState = history.state;
  };
  history.pushState = trackedPush;
  history.replaceState = trackedReplace;

  // Combine rapid traversals before correcting position to avoid opposing go calls
  const restore = () => {
    if (restoreTask !== null) return;
    restoreTask = setTimeout(() => {
      restoreTask = null;
      if (disposed || !restoring) return;
      const next = position();
      if (next === undefined) return;
      if (next !== restoring.index) {
        history.go(restoring.index - next);
        return;
      }
      const leave = restoring.leave;
      restoring = null;
      if (dirty()) blocked(leave);
      else leave.resume();
    }, 0);
  };

  const hash = () => {
    if (restoring || href === win.location.href) return;
    // State may be null or inherited
    const marker = entry(history.state);
    if (!win.navigation && (!marker || marker.session !== session || marker.index === index)) {
      index++;
    } else {
      index = position() ?? index;
    }
    // Back and Forward do not reload document
    replace.call(history, stamped(history.state ?? frameworkState, index), "");
    href = win.location.href;
    frameworkState = history.state;
  };

  const pop = (event: PopStateEvent) => {
    const marker = entry(event.state);
    const next =
      win.navigation?.currentEntry?.index ??
      (marker?.session === session ? marker.index : undefined);
    if (restoring) {
      event.stopImmediatePropagation();
      restore();
      return;
    }
    if (route(href) === route(win.location.href) && href !== win.location.href) {
      hash();
      return;
    }
    // Root guard stamps entries in this document
    if (next === undefined) return;
    if (!dirty() || route(href) === route(win.location.href) || next === index) {
      index = next;
      href = win.location.href;
      frameworkState = history.state;
      return;
    }
    event.stopImmediatePropagation();
    const delta = next - index;
    const destination = win.location.href;
    restoring = {
      index,
      leave: { href: destination, resume: () => history.go(delta) },
    };
    restore();
  };
  win.addEventListener("popstate", pop, { capture: true });
  win.addEventListener("hashchange", hash);
  return () => {
    disposed = true;
    if (restoreTask !== null) clearTimeout(restoreTask);
    win.removeEventListener("popstate", pop, { capture: true });
    win.removeEventListener("hashchange", hash);
    if (history.pushState === trackedPush) history.pushState = push;
    if (history.replaceState === trackedReplace) history.replaceState = replace;
  };
}
