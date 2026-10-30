"use client";

import { usePathname, useRouter } from "next/navigation";
import { createContext, type ReactNode, useContext, useEffect, useState } from "react";
import { deleteBucketAction } from "@/lib/actions";
import { tabHref } from "@/lib/bucket-tabs";
import { PINS_KEY, parsePins, togglePin } from "@/lib/pins";
import { safeDisplayName } from "@/lib/safe-name";
import { CreateBucketModal } from "./CreateBucketModal";
import { Dialog } from "./Dialog";
import { useLeaveGuard } from "./LeaveGuard";

interface BucketActions {
  pins: string[];
  togglePin: (name: string) => void;
  openCreate: () => void;
  openSettings: (name: string) => void;
  confirmDelete: (name: string) => void;
}

const Ctx = createContext<BucketActions | null>(null);

export function useBuckets(): BucketActions {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useBuckets needs BucketProvider");
  return ctx;
}

// Bucket actions create/delete dialog
export function BucketProvider({ buckets, children }: { buckets: string[]; children: ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const { navigate } = useLeaveGuard();
  const [pins, setPins] = useState<string[]>([]);
  const [creating, setCreating] = useState(false);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    try {
      setPins(parsePins(localStorage.getItem(PINS_KEY)));
    } catch {
      // Storage blocked, pins start empty
    }
  }, []);

  const savePins = (next: string[]) => {
    setPins(next);
    try {
      localStorage.setItem(PINS_KEY, JSON.stringify(next));
    } catch {
      // Storage blocked, pins last until reload
    }
  };

  const remove = async (name: string) => {
    setBusy(true);
    setError("");
    try {
      const res = await deleteBucketAction(name);
      if ("error" in res) {
        setError(res.error);
        return;
      }
      savePins(pins.filter((p) => p !== name));
      setDeleting(null);
      const [, section, open] = pathname.split("/");
      if (section === "buckets" && open && decodeURIComponent(open) === name)
        router.push("/buckets");
      else router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to delete bucket");
    } finally {
      setBusy(false);
    }
  };

  const value: BucketActions = {
    pins,
    togglePin: (name) => savePins(togglePin(pins, name, new Set(buckets))),
    openCreate: () => setCreating(true),
    openSettings: (name) => navigate(tabHref(name, "settings")),
    confirmDelete: (name) => {
      setError("");
      setDeleting(name);
    },
  };

  return (
    <Ctx.Provider value={value}>
      {children}
      <CreateBucketModal open={creating} onClose={() => setCreating(false)} />
      {deleting && (
        <Dialog open onClose={() => setDeleting(null)} className="max-w-md" label="Delete bucket">
          <div className="bg-abyss w-full rounded-xl p-6">
            <div className="mb-4 flex items-center gap-3">
              <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-red-500/10">
                <span className="icon-[tabler--alert-triangle] text-xl text-red-400" />
              </span>
              <div className="min-w-0">
                <h3 className="font-display text-lg font-semibold">Delete bucket</h3>
                <p className="truncate font-mono text-xs text-stone-400">
                  <bdi>{safeDisplayName(deleting)}</bdi>
                </p>
              </div>
            </div>
            <p className="mb-5 text-sm text-stone-400">
              This permanently deletes the bucket and every object in it. It cannot be undone.
            </p>
            {error && (
              <p
                role="alert"
                className="mb-4 rounded-lg border border-red-500/20 bg-red-500/5 px-3 py-2 font-mono text-xs text-red-400"
              >
                Could not delete bucket: {error}
              </p>
            )}
            <div className="flex justify-end gap-2">
              <button
                type="button"
                onClick={() => setDeleting(null)}
                className="bg-surface font-body hover:bg-surface-overlay inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-3.5 text-sm font-medium whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
              >
                Cancel
              </button>
              <button
                type="button"
                disabled={busy}
                onClick={() => remove(deleting)}
                className="font-body inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg bg-red-500 px-3.5 text-sm font-medium whitespace-nowrap text-white transition-colors hover:bg-red-500/90 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
              >
                {busy ? "Deleting..." : "Delete bucket"}
              </button>
            </div>
          </div>
        </Dialog>
      )}
    </Ctx.Provider>
  );
}
