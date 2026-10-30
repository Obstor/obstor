"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { type DragEvent, type ReactNode, useRef, useState } from "react";
import { getDownloadURL, getUploadURL } from "@/lib/actions";
import { type BucketTab, tabHref } from "@/lib/bucket-tabs";
import { type Entry, folderKey, hostPath, objectsHref, s3ErrorMessage } from "@/lib/objects";
import type { AccessLabel } from "@/lib/rpc";
import { safeDisplayName } from "@/lib/safe-name";
import { useBuckets } from "./BucketProvider";
import { AccessTag } from "./BucketTable";
import { BucketTabs } from "./BucketTabs";
import { CopyButton } from "./CopyButton";
import { Menu, MenuItem, MenuLink } from "./Menu";
import {
  ChecksumsDialog,
  DeleteObjectsDialog,
  NewFolderDialog,
  ShareDialog,
} from "./ObjectDialogs";
import { ObjectTable } from "./ObjectTable";
import { type Upload, UploadTray } from "./UploadTray";

type DialogState =
  | { kind: "share"; name: string }
  | { kind: "checksums"; entry: Entry }
  | { kind: "delete"; names: string[] }
  | { kind: "folder" }
  | null;

type Picked = { file: File; path: string };

// Capture bucket and prefix on drop so navigation cannot redirect uploads
type Queued = { id: string; item: Picked; bucket: string; prefix: string };

// XHR reports upload progress
function put(url: string, body: Blob, onProgress: (pct: number) => void): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("PUT", url);
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress(Math.round((e.loaded / e.total) * 100));
    };
    xhr.onload = () =>
      xhr.status < 400
        ? resolve()
        : reject(new Error(s3ErrorMessage(xhr.responseText, xhr.status)));
    xhr.onerror = () => reject(new Error("Network error"));
    xhr.send(body);
  });
}

// Walk dropped directories, keep relative paths
function readEntry(entry: FileSystemEntry, base: string): Promise<Picked[]> {
  return new Promise((resolve) => {
    if (entry.isFile) {
      (entry as FileSystemFileEntry).file(
        (file) => resolve([{ file, path: base + file.name }]),
        () => resolve([]),
      );
    } else if (entry.isDirectory) {
      const reader = (entry as FileSystemDirectoryEntry).createReader();
      const found: Promise<Picked[]>[] = [];
      const next = () =>
        reader.readEntries(
          (batch) => {
            if (batch.length === 0) {
              Promise.all(found).then((all) => resolve(all.flat()));
              return;
            }
            for (const e of batch) found.push(readEntry(e, `${base}${entry.name}/`));
            next();
          },
          () => resolve([]),
        );
      next();
    } else {
      resolve([]);
    }
  });
}

export function BucketView({
  bucket,
  prefix,
  entries,
  error,
  created,
  access,
  url,
  buckets,
  showLocations,
  tab,
  children,
}: {
  bucket: string;
  prefix: string;
  entries: Entry[];
  error: string;
  created: string;
  access: AccessLabel | null;
  url: string;
  buckets: string[];
  showLocations: boolean;
  tab: BucketTab;
  children?: ReactNode;
}) {
  const router = useRouter();
  const { pins, togglePin, confirmDelete } = useBuckets();
  const fileInput = useRef<HTMLInputElement>(null);
  const seq = useRef(0);
  const queue = useRef<Queued[]>([]);
  const running = useRef(false);
  const [dialog, setDialog] = useState<DialogState>(null);
  const [uploads, setUploads] = useState<Upload[]>([]);
  const [dragging, setDragging] = useState(false);
  const [downloadError, setDownloadError] = useState("");
  const pinned = pins.includes(bucket);
  const name = safeDisplayName(bucket);

  const patch = (id: string, p: Partial<Upload>) =>
    setUploads((list) => list.map((u) => (u.id === id ? { ...u, ...p } : u)));

  // One runner drains queue, later drops append
  const drain = async () => {
    if (running.current) return;
    running.current = true;
    for (let next = queue.current.shift(); next; next = queue.current.shift()) {
      const { id, item } = next;
      patch(id, { status: "uploading" });
      try {
        const res = await getUploadURL(next.bucket, next.prefix, item.path);
        if ("error" in res) throw new Error(res.error);
        await put(res.url, item.file, (progress) => patch(id, { progress }));
        patch(id, { status: "done", progress: 100 });
      } catch (err) {
        patch(id, {
          status: "failed",
          error: err instanceof Error ? err.message : "Upload failed",
        });
      }
    }
    running.current = false;
    router.refresh();
  };

  const upload = (items: Picked[]) => {
    if (items.length === 0) return;
    const batch: Queued[] = items.map((item) => {
      seq.current += 1;
      return { id: String(seq.current), item, bucket, prefix };
    });
    setUploads((list) => [
      ...list,
      ...batch.map((q): Upload => ({ id: q.id, name: q.item.path, progress: 0, status: "queued" })),
    ]);
    queue.current.push(...batch);
    void drain();
  };

  const onDrop = async (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    setDragging(false);
    // Read entries before await while drop event is live
    const roots = [...e.dataTransfer.items]
      .map((it) => it.webkitGetAsEntry?.())
      .filter((x): x is FileSystemEntry => !!x);
    const loose = [...e.dataTransfer.files].map((f) => ({ file: f, path: f.name }));
    upload(
      roots.length > 0 ? (await Promise.all(roots.map((r) => readEntry(r, "")))).flat() : loose,
    );
  };

  const download = async (objectName: string) => {
    setDownloadError("");
    // Open tab before await so Safari allows it
    const win = window.open("", "_blank");
    const res = await getDownloadURL(bucket, objectName).catch(() => ({ error: "Network error" }));
    if ("error" in res) {
      win?.close();
      setDownloadError(`Download failed: ${res.error}`);
      return;
    }
    if (!win) {
      setDownloadError("The browser blocked the download tab. Allow pop-ups for this site.");
      return;
    }
    win.opener = null;
    win.location.href = res.url;
  };

  const createFolder = async (input: string): Promise<string | null> => {
    const key = folderKey(prefix, input);
    if (!key) return "Enter a folder name.";
    setDialog(null);
    router.push(objectsHref(bucket, key));
    return null;
  };

  return (
    <div
      className={
        tab === "objects" ? "flex flex-col gap-4 lg:h-[calc(100dvh-7rem)]" : "flex flex-col gap-4"
      }
    >
      <header className="shrink-0">
        <nav
          aria-label="Breadcrumb"
          className="mb-3 flex items-center gap-2 text-sm text-stone-400"
        >
          <Link href="/buckets" className="hover:text-stone-200">
            Buckets
          </Link>
          <span className="text-stone-600">/</span>
          <BucketSwitcher current={bucket} buckets={buckets} />
        </nav>
        <div className="flex flex-nowrap items-center gap-3">
          <div className="flex min-w-0 flex-1 items-center gap-2 sm:gap-3">
            <span className="icon-[tabler--bucket] hidden shrink-0 text-[32px] text-amber-500 sm:inline-block" />
            <h1
              className="font-display min-w-0 truncate text-2xl font-bold sm:text-[34px] sm:leading-tight"
              title={name}
            >
              <bdi>{name}</bdi>
            </h1>
            {access && (
              <span className="shrink-0">
                <AccessTag access={access} pill />
              </span>
            )}
            <button
              type="button"
              aria-pressed={pinned}
              aria-label={pinned ? "Unpin bucket" : "Pin bucket"}
              title={pinned ? "Unpin bucket" : "Pin bucket"}
              onClick={() => togglePin(bucket)}
              className="hover:bg-surface inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-lg text-stone-500 transition-colors hover:text-stone-200 disabled:pointer-events-none disabled:opacity-40 max-sm:hidden pointer-coarse:min-h-11 pointer-coarse:min-w-11"
            >
              <span
                className={
                  pinned ? "icon-[tabler--pinned-filled] text-amber-500" : "icon-[tabler--pin]"
                }
              />
            </button>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            {tab !== "settings" && (
              <Link
                href={tabHref(bucket, "settings", prefix)}
                aria-label="Bucket settings"
                title="Bucket settings"
                className="bg-surface font-body hover:bg-surface-overlay inline-flex h-10 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-4 text-[15px] leading-[calc(20/14)] font-semibold whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 max-sm:w-10 max-sm:px-0 pointer-coarse:min-h-11"
              >
                <span className="icon-[tabler--settings] text-lg" />
                <span className="max-sm:hidden">Bucket settings</span>
              </Link>
            )}
            {tab === "objects" && (
              <button
                type="button"
                aria-label="Upload objects"
                title="Upload objects"
                onClick={() => fileInput.current?.click()}
                className="font-body inline-flex h-10 shrink-0 items-center justify-center gap-2 rounded-lg bg-amber-500 px-4 text-[15px] leading-[calc(20/14)] font-semibold whitespace-nowrap text-black transition-colors hover:bg-amber-400 disabled:pointer-events-none disabled:opacity-40 max-sm:w-10 max-sm:px-0 pointer-coarse:min-h-11"
              >
                <span className="icon-[tabler--upload] text-lg" />
                <span className="max-sm:hidden">Upload objects</span>
              </button>
            )}
            <Menu
              label="More bucket actions"
              triggerClassName="inline-flex shrink-0 items-center justify-center rounded-lg text-lg transition-colors disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11 border border-stone-800 bg-surface text-stone-200 hover:border-stone-700 hover:bg-surface-overlay h-10 w-10"
              trigger={<span className="icon-[tabler--dots-vertical]" />}
            >
              <MenuItem
                icon={pinned ? "icon-[tabler--pinned-off]" : "icon-[tabler--pin]"}
                className="sm:hidden"
                onSelect={() => togglePin(bucket)}
              >
                {pinned ? "Unpin bucket" : "Pin bucket"}
              </MenuItem>
              <MenuItem icon="icon-[tabler--trash]" danger onSelect={() => confirmDelete(bucket)}>
                Delete bucket
              </MenuItem>
            </Menu>
          </div>
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-2 text-sm text-stone-400">
          {created && <span>Created {created}</span>}
          <span className="bg-surface inline-flex h-8 max-w-full min-w-0 items-center gap-1.5 rounded-lg border border-stone-800/70 pr-0.5 pl-2.5 font-mono text-[13px] text-stone-200">
            <span className="icon-[tabler--link] shrink-0 text-base text-stone-400" />
            <span className="truncate" title={safeDisplayName(url)}>
              <bdi>{safeDisplayName(hostPath(url))}</bdi>
            </span>
            <CopyButton value={url} label="Copy bucket URL" />
          </span>
        </div>
      </header>

      <BucketTabs bucket={bucket} prefix={prefix} tab={tab}>
        {tab === "objects" ? (
          <>
            {downloadError && (
              <div
                role="alert"
                className="flex shrink-0 items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/5 px-4 py-3 text-sm text-red-400"
              >
                <span className="icon-[tabler--alert-circle] text-base" />
                <span className="flex-1">{downloadError}</span>
                <button
                  type="button"
                  aria-label="Dismiss"
                  title="Dismiss"
                  onClick={() => setDownloadError("")}
                  className="hover:bg-surface inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg text-lg text-stone-400 transition-colors hover:text-stone-100 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11"
                >
                  <span className="icon-[tabler--x]" />
                </button>
              </div>
            )}
            {error ? (
              <div className="flex items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/5 px-4 py-3 text-sm text-red-400">
                <span className="icon-[tabler--alert-circle] text-base" />
                {error}
              </div>
            ) : (
              // biome-ignore lint/a11y/noStaticElementInteractions: drop zone is pointer-only; Upload objects is the keyboard path
              <div
                onDragOver={(e) => {
                  e.preventDefault();
                  setDragging(true);
                }}
                onDragLeave={(e) => {
                  if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setDragging(false);
                }}
                onDrop={onDrop}
                className="relative flex flex-col lg:min-h-0 lg:flex-1"
              >
                <ObjectTable
                  key={prefix}
                  bucket={bucket}
                  prefix={prefix}
                  entries={entries}
                  showLocations={showLocations}
                  onNewFolder={() => setDialog({ kind: "folder" })}
                  onDownload={download}
                  onShare={(objectName) => setDialog({ kind: "share", name: objectName })}
                  onChecksums={(entry) => setDialog({ kind: "checksums", entry })}
                  onDelete={(names) => setDialog({ kind: "delete", names })}
                />
                {dragging && (
                  <div className="pointer-events-none absolute inset-0 z-30 flex items-center justify-center rounded-xl border-2 border-dashed border-amber-500 bg-amber-500/10">
                    <p className="flex items-center gap-2 font-medium text-amber-400">
                      <span className="icon-[tabler--upload] text-xl" />
                      Drop files to upload
                    </p>
                  </div>
                )}
              </div>
            )}

            <input
              ref={fileInput}
              type="file"
              multiple
              hidden
              onChange={(e) => {
                const files = [...(e.target.files ?? [])];
                e.target.value = "";
                upload(files.map((f) => ({ file: f, path: f.name })));
              }}
            />
          </>
        ) : (
          children
        )}
      </BucketTabs>
      {tab === "objects" && (
        <>
          {/* Objects tab only, tray would cover the settings save bar */}
          <UploadTray uploads={uploads} onClear={() => setUploads([])} />
          {dialog?.kind === "share" && (
            <ShareDialog bucket={bucket} name={dialog.name} onClose={() => setDialog(null)} />
          )}
          {dialog?.kind === "checksums" && (
            <ChecksumsDialog bucket={bucket} entry={dialog.entry} onClose={() => setDialog(null)} />
          )}
          {dialog?.kind === "delete" && (
            <DeleteObjectsDialog
              bucket={bucket}
              prefix={prefix}
              names={dialog.names}
              onClose={() => setDialog(null)}
              onDone={() => {
                setDialog(null);
                router.refresh();
              }}
            />
          )}
          {dialog?.kind === "folder" && (
            <NewFolderDialog
              prefix={prefix}
              onCreate={createFolder}
              onClose={() => setDialog(null)}
            />
          )}
        </>
      )}
    </div>
  );
}

function BucketSwitcher({ current, buckets }: { current: string; buckets: string[] }) {
  const [q, setQ] = useState("");
  const needle = q.trim().toLowerCase();
  const shown = needle ? buckets.filter((b) => b.toLowerCase().includes(needle)) : buckets;
  return (
    <Menu
      align="start"
      className="w-72"
      triggerClassName="inline-flex min-w-0 items-center gap-1 rounded-md px-1 text-stone-200 hover:bg-surface"
      trigger={
        <>
          <bdi className="inline-block max-w-[40vw] truncate align-bottom">
            {safeDisplayName(current)}
          </bdi>
          <span className="icon-[tabler--chevron-down] shrink-0 text-stone-400" />
        </>
      }
    >
      {buckets.length > 10 && (
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Filter buckets"
          aria-label="Filter buckets"
          className="bg-surface font-body mb-1 h-8 w-full rounded-lg border border-stone-800 px-3 text-base text-stone-100 transition-colors outline-none placeholder:text-stone-500 focus:border-amber-500 disabled:opacity-50 sm:text-sm"
        />
      )}
      <div className="max-h-72 overflow-y-auto">
        {shown.map((b) => (
          <MenuLink
            key={b}
            href={objectsHref(b)}
            icon="icon-[tabler--bucket]"
            active={b === current}
          >
            <bdi>{safeDisplayName(b)}</bdi>
          </MenuLink>
        ))}
      </div>
    </Menu>
  );
}
