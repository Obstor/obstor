"use client";

import { type UploadStatus, uploadProgress } from "@/lib/objects";
import { safeDisplayName } from "@/lib/safe-name";

export interface Upload {
  id: string;
  name: string;
  progress: number;
  status: UploadStatus;
  error?: string;
}

export function UploadTray({ uploads, onClear }: { uploads: Upload[]; onClear: () => void }) {
  if (uploads.length === 0) return null;
  const { active, current, total } = uploadProgress(uploads.map((u) => u.status));
  const failed = uploads.filter((u) => u.status === "failed").length;
  const done = uploads.filter((u) => u.status === "done").length;

  return (
    <section
      aria-label="Uploads"
      className="bg-abyss fixed right-4 bottom-4 z-40 w-[min(380px,calc(100vw-2rem))] rounded-xl shadow-xl shadow-black/50"
    >
      <header className="flex items-center gap-3 border-b border-stone-800/70 px-4 py-3">
        <span className="icon-[tabler--upload] text-lg text-amber-500" />
        <p aria-live="polite" className="flex-1 text-sm">
          {active
            ? `Uploading ${current} of ${total}`
            : `${done} uploaded${failed > 0 ? ` | ${failed} failed` : ""}`}
        </p>
        <button
          type="button"
          aria-label="Close uploads"
          title="Close uploads"
          disabled={active}
          onClick={onClear}
          className="hover:bg-surface inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-lg text-stone-400 transition-colors hover:text-stone-100 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11"
        >
          <span className="icon-[tabler--x]" />
        </button>
      </header>
      <ul className="max-h-64 space-y-3 overflow-y-auto p-4">
        {uploads.map((u) => (
          <li key={u.id}>
            <div className="mb-1 flex items-center justify-between gap-3 text-xs">
              <span className="truncate" title={safeDisplayName(u.name)}>
                <bdi>{safeDisplayName(u.name)}</bdi>
              </span>
              <span
                aria-hidden={u.status === "uploading"}
                className={`shrink-0 font-mono ${u.status === "failed" ? "text-red-400" : "text-stone-400"}`}
              >
                {u.status === "failed"
                  ? "Failed"
                  : u.status === "queued"
                    ? "Waiting"
                    : `${u.progress}%`}
              </span>
            </div>
            {u.status === "failed" ? (
              <p className="text-xs text-red-400">{u.error}</p>
            ) : (
              <div
                role="progressbar"
                aria-label={safeDisplayName(u.name)}
                aria-valuenow={u.progress}
                aria-valuemin={0}
                aria-valuemax={100}
                className="h-1 overflow-hidden rounded-full bg-stone-800"
              >
                <div
                  className="h-full rounded-full bg-amber-500 transition-[width]"
                  style={{ width: `${u.progress}%` }}
                />
              </div>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
