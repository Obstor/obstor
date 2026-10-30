"use client";

import { useEffect, useRef, useState } from "react";
import { Checkbox } from "@/components/Checkbox";
import { createBucketAction, getBucketFeaturesAction } from "@/lib/actions";
import {
  ALL_READABLE,
  type BucketConfig,
  type BucketFeatures,
  controlReason,
  createdConfig,
} from "@/lib/bucket-config";
import { tabHref } from "@/lib/bucket-tabs";
import { safeDisplayName } from "@/lib/safe-name";
import { LockingPanel } from "./bucket-settings/LockingPanel";
import { Note } from "./bucket-settings/shared";
import { Dialog } from "./Dialog";
import { useLeaveGuard } from "./LeaveGuard";

// Name and object locking, other settings use bucket tabs
export function CreateBucketModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <Dialog open={open} onClose={onClose} className="max-w-md" label="Create bucket">
      {open && <CreateBucketForm onClose={onClose} />}
    </Dialog>
  );
}

// Remount on open, clear form and ignore replies after close
function CreateBucketForm({ onClose }: { onClose: () => void }) {
  const { navigate } = useLeaveGuard();
  const [name, setName] = useState("");
  const [config, setConfig] = useState<BucketConfig>(() => createdConfig(false));
  const [features, setFeatures] = useState<BucketFeatures | null>(null);
  const [loading, setLoading] = useState(true);
  const [configure, setConfigure] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const alive = useRef(false);

  useEffect(() => {
    alive.current = true;
    const fail = (message: string) => {
      if (!alive.current) return;
      setError(`Could not load server features: ${message}`);
      setLoading(false);
    };
    getBucketFeaturesAction().then(
      (res) => {
        if ("error" in res) {
          fail(res.error);
          return;
        }
        if (!alive.current) return;
        setFeatures(res);
        setLoading(false);
      },
      (err) => fail(err instanceof Error ? err.message : "request failed"),
    );
    return () => {
      alive.current = false;
    };
  }, []);

  // Fieldset holds toggle disabled until features load
  const reason = loading
    ? null
    : controlReason("objectLock", { features, readable: ALL_READABLE, config, isEdit: false });

  const create = async () => {
    if (busy) return;
    if (!name) {
      setError("Bucket name is required.");
      return;
    }
    setBusy(true);
    setError("");
    try {
      const res = await createBucketAction(name, config.objectLock);
      if (!alive.current) return;
      if ("error" in res) {
        setError(res.error);
        return;
      }
      onClose();
      if (configure) navigate(tabHref(name, "settings"));
    } catch (err) {
      if (alive.current) setError(err instanceof Error ? err.message : "Failed to create bucket");
    } finally {
      if (alive.current) setBusy(false);
    }
  };

  return (
    <div className="bg-abyss w-full rounded-xl p-6">
      <h2 className="font-display text-[18px] font-semibold">Create bucket</h2>
      <div className="mt-5 space-y-5">
        <div>
          <p className="font-body mb-2 text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase">
            Name
          </p>
          <input
            type="text"
            value={name}
            onChange={(e) => {
              setName(e.target.value.toLowerCase().replace(/[^a-z0-9.-]/g, ""));
              setError("");
            }}
            onKeyDown={(e) => {
              if (e.key !== "Enter") return;
              e.preventDefault();
              void create();
            }}
            placeholder="my-bucket"
            aria-label="Bucket name"
            autoComplete="off"
            spellCheck={false}
            // biome-ignore lint/a11y/noAutofocus: the dialog opens on the name field
            autoFocus
            className="bg-surface h-9 w-full rounded-lg border border-stone-800 px-3 font-mono text-base text-stone-100 transition-colors outline-none placeholder:text-stone-500 focus:border-amber-500 disabled:opacity-50 sm:text-sm"
          />
          {name && (
            <p className="mt-1.5 font-mono text-[13px] break-all text-stone-400">
              arn:aws:s3:::<bdi>{safeDisplayName(name)}</bdi>
            </p>
          )}
        </div>
        <fieldset disabled={loading} aria-label="Object locking" className="min-w-0">
          <LockingPanel
            config={config}
            onChange={(patch) => setConfig((c) => ({ ...c, ...patch }))}
            reason={reason}
          />
        </fieldset>
        <div>
          <label className="flex items-center gap-2 text-sm">
            <Checkbox checked={configure} onChange={(e) => setConfigure(e.target.checked)} />
            Configure after create
          </label>
          <Note text="Opens the new bucket's Settings tab." />
        </div>
      </div>
      {error && (
        <p
          role="alert"
          className="mt-4 flex items-start gap-2 text-[13px] leading-relaxed text-red-400"
        >
          <span className="icon-[tabler--alert-circle] mt-0.5 shrink-0" />
          <span className="break-words">{error}</span>
        </p>
      )}
      <div className="mt-6 flex justify-end gap-2">
        <button
          type="button"
          onClick={onClose}
          className="bg-surface font-body hover:bg-surface-overlay inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-3.5 text-sm font-medium whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
        >
          Cancel
        </button>
        <button
          type="button"
          onClick={create}
          disabled={busy || !name}
          className="font-body inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg bg-amber-500 px-3.5 text-sm font-semibold whitespace-nowrap text-black transition-colors hover:bg-amber-400 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
        >
          {busy ? "Creating..." : "Create bucket"}
        </button>
      </div>
    </div>
  );
}
