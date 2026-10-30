"use client";

import { type FormEvent, useEffect, useState } from "react";
import { deleteObjectsAction, getObjectChecksums, getShareLink } from "@/lib/actions";
import { plural } from "@/lib/format";
import { type Entry, entryName, isFolder } from "@/lib/objects";
import { safeDisplayName } from "@/lib/safe-name";
import { CopyButton } from "./CopyButton";
import { Dialog } from "./Dialog";

function Title({ title, sub }: { title: string; sub?: string }) {
  return (
    <div className="mb-4 min-w-0 pr-8">
      <h3 className="font-display text-lg font-semibold">{title}</h3>
      {sub && (
        <p className="mt-0.5 truncate font-mono text-xs text-stone-400" title={sub}>
          <bdi>{sub}</bdi>
        </p>
      )}
    </div>
  );
}

function CloseX({ onClose }: { onClose: () => void }) {
  return (
    <button
      type="button"
      aria-label="Close"
      onClick={onClose}
      className="hover:bg-surface absolute top-4 right-4 inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-lg text-stone-400 transition-colors hover:text-stone-100 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11"
    >
      <span className="icon-[tabler--x]" />
    </button>
  );
}

export function ShareDialog({
  bucket,
  name,
  onClose,
}: {
  bucket: string;
  name: string;
  onClose: () => void;
}) {
  const [link, setLink] = useState<{ url: string } | { error: string } | null>(null);
  useEffect(() => {
    getShareLink(bucket, name).then(setLink);
  }, [bucket, name]);

  return (
    <Dialog open onClose={onClose} className="max-w-lg" label="Share link">
      <div className="bg-abyss relative w-full rounded-xl p-6">
        <Title title="Share link" sub={safeDisplayName(name)} />
        {!link ? (
          <p className="text-sm text-stone-400">Creating link...</p>
        ) : "error" in link ? (
          <p className="text-sm text-red-400">{link.error}</p>
        ) : (
          <div className="bg-void flex items-center gap-2 rounded-lg border border-stone-800/70 py-1 pr-1 pl-3">
            <input
              readOnly
              value={link.url}
              aria-label="Share link"
              className="min-w-0 flex-1 bg-transparent font-mono text-xs text-stone-300 outline-none"
            />
            <CopyButton value={link.url} label="Copy link" />
          </div>
        )}
        <p className="mt-3 text-xs text-stone-400">The link expires in 5 minutes.</p>
        <CloseX onClose={onClose} />
      </div>
    </Dialog>
  );
}

export function ChecksumsDialog({
  bucket,
  entry,
  onClose,
}: {
  bucket: string;
  entry: Entry;
  onClose: () => void;
}) {
  const [sums, setSums] = useState<Awaited<ReturnType<typeof getObjectChecksums>> | null>(null);
  useEffect(() => {
    getObjectChecksums(bucket, entry.name).then(setSums);
  }, [bucket, entry.name]);
  const rows =
    sums && !("error" in sums)
      ? (
          [
            ["ETag", entry.etag],
            ["MD5", sums.md5],
            ["SHA-1", sums.sha1],
            ["SHA-256", sums.sha256],
            ["SHA-512", sums.sha512],
          ] as const
        ).filter(([, v]) => v)
      : [];

  return (
    <Dialog open onClose={onClose} className="max-w-2xl" label="Checksums">
      <div className="bg-abyss relative w-full rounded-xl p-6">
        <Title title="Checksums" sub={safeDisplayName(entry.name)} />
        {!sums ? (
          <p className="text-sm text-stone-400">Computing checksums...</p>
        ) : "error" in sums ? (
          <p className="text-sm text-red-400">{sums.error}</p>
        ) : (
          <dl className="grid grid-cols-[72px_1fr] items-center gap-x-3 gap-y-2">
            {rows.map(([k, v]) => (
              <div key={k} className="contents">
                <dt className="font-body text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase">
                  {k}
                </dt>
                <dd className="flex min-w-0 items-center gap-2">
                  <span className="truncate font-mono text-xs text-stone-300" title={v}>
                    {v}
                  </span>
                  <CopyButton value={v} label={`Copy ${k}`} />
                </dd>
              </div>
            ))}
          </dl>
        )}
        <CloseX onClose={onClose} />
      </div>
    </Dialog>
  );
}

export function DeleteObjectsDialog({
  bucket,
  prefix,
  names,
  onClose,
  onDone,
}: {
  bucket: string;
  prefix: string;
  names: string[];
  onClose: () => void;
  onDone: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const folders = names.filter(isFolder).length;
  const title =
    names.length === 1
      ? folders
        ? "Delete folder"
        : "Delete object"
      : `Delete ${plural(names.length, "item")}`;

  const confirm = async () => {
    setBusy(true);
    setError("");
    const res = await deleteObjectsAction(bucket, names);
    setBusy(false);
    if ("error" in res) setError(res.error);
    else onDone();
  };

  return (
    <Dialog open onClose={onClose} className="max-w-md" label={title}>
      <div className="bg-abyss relative w-full rounded-xl p-6">
        <Title
          title={title}
          sub={names.length === 1 ? safeDisplayName(entryName(names[0], prefix)) : undefined}
        />
        <p className="mb-5 text-sm text-stone-400">
          {folders > 0 ? "Folders are deleted with everything in them. " : ""}This cannot be undone.
        </p>
        {error && (
          <p
            role="alert"
            className="mb-4 rounded-lg border border-red-500/20 bg-red-500/5 px-3 py-2 font-mono text-xs text-red-400"
          >
            {error}
          </p>
        )}
        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="bg-surface font-body hover:bg-surface-overlay inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-3.5 text-sm font-medium whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
          >
            Cancel
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={confirm}
            className="font-body inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg bg-red-500 px-3.5 text-sm font-medium whitespace-nowrap text-white transition-colors hover:bg-red-500/90 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
          >
            {busy ? "Deleting..." : "Delete"}
          </button>
        </div>
        <CloseX onClose={onClose} />
      </div>
    </Dialog>
  );
}

export function NewFolderDialog({
  prefix,
  onCreate,
  onClose,
}: {
  prefix: string;
  onCreate: (name: string) => Promise<string | null>; // error message or null on success
  onClose: () => void;
}) {
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError((await onCreate(name)) ?? "");
    setBusy(false);
  };

  return (
    <Dialog open onClose={onClose} className="max-w-md" label="New folder">
      <form onSubmit={submit} className="bg-abyss relative w-full rounded-xl p-6">
        <Title title="New folder" sub={prefix ? `in ${safeDisplayName(prefix)}` : undefined} />
        <label
          htmlFor="folder-name"
          className="font-body text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase"
        >
          Name
        </label>
        <input
          id="folder-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="reports"
          className="bg-surface font-body mt-1.5 h-9 w-full rounded-lg border border-stone-800 px-3 text-base text-stone-100 transition-colors outline-none placeholder:text-stone-500 focus:border-amber-500 disabled:opacity-50 sm:text-sm"
        />
        <p className="mt-2 text-xs text-stone-400">
          The folder appears once you upload a file into it.
        </p>
        {error && (
          <p role="alert" className="mt-3 text-sm text-red-400">
            {error}
          </p>
        )}
        <div className="mt-5 flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="bg-surface font-body hover:bg-surface-overlay inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-3.5 text-sm font-medium whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={busy}
            className="font-body inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg bg-amber-500 px-3.5 text-sm font-semibold whitespace-nowrap text-black transition-colors hover:bg-amber-400 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
          >
            {busy ? "Creating..." : "Create folder"}
          </button>
        </div>
        <CloseX onClose={onClose} />
      </form>
    </Dialog>
  );
}
