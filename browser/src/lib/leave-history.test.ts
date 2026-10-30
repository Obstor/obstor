import assert from "node:assert/strict";
import { type HistoryLeave, installLeaveHistory } from "./leave-history.ts";

// Browser history stub, blocked traversal keeps form and stack
function fixture(nativeNavigation: boolean) {
  const stack = [{ href: "http://console.test/login", state: { __NA: true, tree: "login" } }];
  let at = 0;
  let downstream = 0;
  let reloads = 0;
  const listeners: { type: string; fn: (event: PopStateEvent) => void; capture: boolean }[] = [];
  const location = { href: stack[at].href };
  const navigation = { currentEntry: { index: at } };
  const update = () => {
    location.href = stack[at].href;
    navigation.currentEntry.index = at;
  };
  let traversals = 0;
  const emit = (type: string) => {
    let stopped = false;
    const event = {
      state: history.state,
      stopImmediatePropagation() {
        stopped = true;
      },
    };
    for (const listener of listeners
      .filter((item) => item.type === type)
      .toSorted((a, b) => Number(b.capture) - Number(a.capture))) {
      listener.fn(event as PopStateEvent);
      if (stopped) break;
    }
  };
  const history = {
    get state() {
      return stack[at].state;
    },
    pushState(state: (typeof stack)[0]["state"], _unused: string, url?: string | URL | null) {
      stack.splice(at + 1);
      stack.push({ state, href: new URL(url?.toString() ?? location.href, location.href).href });
      at++;
      update();
    },
    replaceState(state: (typeof stack)[0]["state"], _unused: string, url?: string | URL | null) {
      stack[at] = { state, href: new URL(url?.toString() ?? location.href, location.href).href };
      update();
    },
    go(delta: number) {
      assert.ok(++traversals < 100, "history restoration must not loop");
      queueMicrotask(() => {
        const next = at + delta;
        if (next < 0 || next >= stack.length || next === at) return;
        at = next;
        update();
        emit("popstate");
      });
    },
  } as unknown as History;
  const win = {
    history,
    location,
    ...(nativeNavigation ? { navigation } : {}),
    addEventListener(
      type: string,
      fn: (event: PopStateEvent) => void,
      options?: { capture?: boolean },
    ) {
      listeners.push({ type, fn, capture: options?.capture ?? false });
    },
    removeEventListener(_type: string, fn: (event: PopStateEvent) => void) {
      const index = listeners.findIndex((listener) => listener.fn === fn);
      if (index >= 0) listeners.splice(index, 1);
    },
  };
  // Register before capture listener, as Next.js may on remount
  win.addEventListener("popstate", (event) => {
    if (event.state && !event.state.__NA) reloads++;
    downstream++;
  });
  let dirty = false;
  let leave: HistoryLeave | null = null;
  const install = () =>
    installLeaveHistory(
      win as unknown as Window,
      () => dirty,
      (target) => {
        leave = target;
      },
    );
  let cleanup = install();
  const push = (path: string) => history.pushState({ __NA: true, tree: path }, "", path);
  const flush = async () => {
    for (let i = 0; i < 6; i++) await new Promise<void>((resolve) => setTimeout(resolve, 0));
  };
  return {
    history,
    stack,
    location,
    push,
    flush,
    hash(fragment: string, copyState: boolean) {
      const state = copyState ? structuredClone(history.state) : null;
      stack.splice(at + 1);
      stack.push({ href: new URL(fragment, location.href).href, state });
      at++;
      update();
      emit("popstate");
      emit("hashchange");
    },
    dirty(value: boolean) {
      dirty = value;
    },
    leave: () => leave,
    cancel() {
      leave = null;
    },
    discard() {
      assert.ok(leave);
      dirty = false;
      const target = leave;
      leave = null;
      target.resume();
    },
    count: () => downstream,
    reloads: () => reloads,
    remount() {
      cleanup();
      cleanup = install();
    },
    cleanup: () => cleanup(),
  };
}

for (const nativeNavigation of [true, false]) {
  const f = fixture(nativeNavigation);
  f.push("/buckets/alpha?tab=objects");
  f.push("/buckets/alpha?tab=settings");
  const states = f.stack.map((item) => item.state);
  f.dirty(true);
  f.history.go(-1);
  await f.flush();
  assert.equal(f.location.href, "http://console.test/buckets/alpha?tab=settings");
  assert.equal(f.count(), 0, "dirty form survives Back and restoration");
  assert.equal(f.leave()?.href, "http://console.test/buckets/alpha?tab=objects");
  assert.deepEqual(
    f.stack.map((item) => item.state),
    states,
    "Next.js state remains intact",
  );
  f.cancel();
  f.history.go(-2);
  await f.flush();
  assert.equal(f.location.href, "http://console.test/buckets/alpha?tab=settings");
  assert.equal(
    f.leave()?.href,
    "http://console.test/login",
    "multi-entry Back retains destination",
  );
  f.discard();
  await f.flush();
  assert.equal(f.location.href, "http://console.test/login");
  assert.equal(f.stack.length, 3, "discard traverses without adding a duplicate entry");
  assert.equal(f.count(), 1);

  f.dirty(true);
  f.history.go(2);
  await f.flush();
  assert.equal(f.location.href, "http://console.test/login");
  assert.equal(f.count(), 1, "Forward is guarded too");
  f.discard();
  await f.flush();
  assert.equal(f.location.href, "http://console.test/buckets/alpha?tab=settings");
  assert.equal(f.count(), 2);

  f.remount();
  f.dirty(true);
  f.history.replaceState({ ...f.history.state, tree: "saved-form" }, "");
  f.dirty(false); // Saved form allows Back and Forward
  f.history.go(-1);
  await f.flush();
  assert.equal(f.location.href, "http://console.test/buckets/alpha?tab=objects");
  assert.equal(f.leave(), null);
  f.history.go(1);
  await f.flush();
  assert.equal(f.history.state.tree, "saved-form");
  f.cleanup();

  // Fragment entries count toward delta
  for (const copyState of [true, false]) {
    const hash = fixture(nativeNavigation);
    hash.push("/buckets/alpha?tab=objects");
    hash.hash("#folder", copyState);
    hash.push("/buckets/alpha?tab=settings");
    hash.dirty(true);
    hash.history.go(-2);
    await hash.flush();
    assert.equal(hash.location.href, "http://console.test/buckets/alpha?tab=settings");
    assert.equal(hash.leave()?.href, "http://console.test/buckets/alpha?tab=objects");
    hash.discard();
    await hash.flush();
    assert.equal(hash.location.href, "http://console.test/buckets/alpha?tab=objects");
    assert.equal(hash.stack.length, 4);
    hash.history.go(1);
    await hash.flush();
    assert.equal(hash.location.href, "http://console.test/buckets/alpha?tab=objects#folder");
    assert.equal(hash.reloads(), 0, "fragment entries preserve Next.js route state");
    hash.cleanup();
  }

  const rapid = fixture(nativeNavigation);
  for (let i = 1; i <= 6; i++) rapid.push(`/entry-${i}`);
  rapid.history.go(-4);
  await rapid.flush();
  rapid.dirty(true);
  const before = rapid.count();
  rapid.history.go(-1);
  rapid.history.go(-1);
  await rapid.flush();
  assert.equal(rapid.location.href, "http://console.test/entry-2");
  assert.equal(rapid.count(), before, "rapid Back retains the mounted form");
  rapid.cancel();
  rapid.history.go(1);
  rapid.history.go(1);
  await rapid.flush();
  assert.equal(rapid.location.href, "http://console.test/entry-2");
  assert.equal(rapid.count(), before, "rapid Forward retains the mounted form");
  rapid.discard();
  await rapid.flush();
  assert.equal(rapid.location.href, "http://console.test/entry-3");
  assert.equal(rapid.stack.length, 7);
  rapid.cleanup();

  const saved = fixture(nativeNavigation);
  saved.push("/buckets/alpha?tab=settings");
  saved.dirty(true);
  saved.history.go(-1);
  await Promise.resolve(); // Save finishes during history restoration
  saved.dirty(false);
  await saved.flush();
  assert.equal(saved.location.href, "http://console.test/login");
  assert.equal(saved.leave(), null, "a save during restoration does not leave a stale dialog");
  saved.cleanup();
}
console.log("leave-history tests passed");
