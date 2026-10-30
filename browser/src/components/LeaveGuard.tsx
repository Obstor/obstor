"use client";

import { useRouter } from "next/navigation";
import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { guardsClick } from "@/lib/bucket-tabs";
import { type HistoryLeave, installLeaveHistory } from "@/lib/leave-history";
import { Dialog } from "./Dialog";

interface Guard {
  navigate: (href: string) => void;
  mark: (id: string, dirty: boolean) => void;
}

const Ctx = createContext<Guard | null>(null);

function useGuard(): Guard {
  const guard = useContext(Ctx);
  if (!guard) throw new Error("useLeaveGuard needs LeaveGuard");
  return guard;
}

// Track all same-document history entries, including login
export function LeaveGuard({ children }: { children: ReactNode }) {
  const router = useRouter();
  const [ids, setIds] = useState<ReadonlySet<string>>(() => new Set());
  const [target, setTarget] = useState<HistoryLeave | null>(null);
  const active = ids.size > 0;
  const dirtyIds = useRef(new Set<string>());

  const mark = useCallback((id: string, dirty: boolean) => {
    if (dirty) dirtyIds.current.add(id);
    else dirtyIds.current.delete(id);
    setIds((prev) => {
      if (prev.has(id) === dirty) return prev;
      const next = new Set(prev);
      if (dirty) next.add(id);
      else next.delete(id);
      return next;
    });
  }, []);

  const navigate = useCallback(
    (href: string) => {
      if (active) setTarget({ href, resume: () => router.push(href) });
      else router.push(href);
    },
    [active, router],
  );

  useLayoutEffect(
    () => installLeaveHistory(window, () => dirtyIds.current.size > 0, setTarget),
    [],
  );

  useEffect(() => {
    if (!active) setTarget(null);
  }, [active]);

  useEffect(() => {
    if (!active) return;
    const onClick = (e: MouseEvent) => {
      const a = e.target instanceof Element ? e.target.closest("a[href]") : null;
      if (!(a instanceof HTMLAnchorElement)) return; // SVG links are not console routes
      const link = { href: a.href, target: a.target, download: a.hasAttribute("download") };
      if (!guardsClick(e, link, window.location.href)) return;
      e.preventDefault();
      e.stopPropagation(); // Stop next/link from navigating
      const url = new URL(a.href, window.location.href);
      const href = url.pathname + url.search + url.hash;
      setTarget({ href, resume: () => router.push(href) });
    };
    const onUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = ""; // Safari and older chrome need to show prompt
    };
    document.addEventListener("click", onClick, { capture: true });
    window.addEventListener("beforeunload", onUnload);
    return () => {
      document.removeEventListener("click", onClick, { capture: true });
      window.removeEventListener("beforeunload", onUnload);
    };
  }, [active, router]);

  const leave = (target: HistoryLeave) => {
    dirtyIds.current.clear();
    setIds(new Set());
    setTarget(null);
    target.resume();
  };

  const value = useMemo(() => ({ navigate, mark }), [navigate, mark]);

  return (
    <Ctx.Provider value={value}>
      {children}
      {target !== null && (
        <Dialog
          open
          onClose={() => setTarget(null)}
          className="max-w-md"
          label="Discard unsaved changes"
        >
          <div className="bg-abyss w-full rounded-xl p-6">
            <div className="mb-4 flex items-center gap-3">
              <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-amber-500/10">
                <span className="icon-[tabler--alert-triangle] text-xl text-amber-500" />
              </span>
              <h3 className="font-display text-lg font-semibold">Discard unsaved changes?</h3>
            </div>
            <p className="mb-5 text-sm text-stone-400">
              Changes on this page have not been saved. Leaving discards them.
            </p>
            <div className="flex justify-end gap-2">
              <button
                type="button"
                onClick={() => setTarget(null)}
                className="bg-surface font-body hover:bg-surface-overlay inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-3.5 text-sm font-medium whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
              >
                Keep editing
              </button>
              <button
                type="button"
                onClick={() => leave(target)}
                className="font-body inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg bg-red-500 px-3.5 text-sm font-medium whitespace-nowrap text-white transition-colors hover:bg-red-500/90 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
              >
                Discard changes
              </button>
            </div>
          </div>
        </Dialog>
      )}
    </Ctx.Provider>
  );
}

export function useLeaveGuard(): { navigate: (href: string) => void } {
  const { navigate } = useGuard();
  return { navigate };
}

export function useDirty(dirty: boolean): void {
  const { mark } = useGuard();
  const id = useId();
  useEffect(() => {
    if (!dirty) return;
    mark(id, true);
    return () => mark(id, false);
  }, [dirty, id, mark]);
}
