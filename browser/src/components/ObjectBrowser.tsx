"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useReducer, useRef } from "react";
import {
  deleteObjectAction,
  getDownloadURL,
  getObjectChecksums,
  getShareLink,
  getUploadURL,
} from "@/lib/actions";
import { safeDisplayName } from "@/lib/safe-name";
import { Dialog } from "./Dialog";

interface FileEntry {
  name: string;
  size: string;
  sizeBytes: number;
  lastModified: string;
  etag: string;
  locations: string[];
}

interface Checksums {
  md5: string;
  sha1: string;
  sha256: string;
  sha512: string;
}

interface Props {
  bucketName: string;
  prefix: string;
  folders: string[];
  files: FileEntry[];
}

type SortField = "name" | "size" | "lastModified";

interface BrowserState {
  sortField: SortField;
  sortAsc: boolean;
  selected: Set<string>;
  dragging: boolean;
  uploading: { name: string; progress: number }[];
  shareModal: { name: string; url: string } | null;
  deleteConfirm: string | null;
  filter: string;
  hashDetails: string | null;
  checksums: Record<string, Checksums>;
  checksumsLoading: string | null;
}

type BrowserAction =
  | { type: "SET_SORT"; field: SortField }
  | { type: "SET_FILTER"; filter: string }
  | { type: "SET_SELECTED"; selected: Set<string> }
  | { type: "SET_DRAGGING"; dragging: boolean }
  | { type: "SET_UPLOADING"; uploading: { name: string; progress: number }[] }
  | { type: "SET_SHARE_MODAL"; modal: { name: string; url: string } | null }
  | { type: "SET_DELETE_CONFIRM"; name: string | null }
  | { type: "RESET_UPLOAD" }
  | { type: "TOGGLE_HASH"; name: string }
  | { type: "SET_CHECKSUMS_LOADING"; name: string | null }
  | { type: "SET_CHECKSUMS"; name: string; checksums: Checksums };

const initialState: BrowserState = {
  sortField: "name",
  sortAsc: true,
  selected: new Set(),
  dragging: false,
  uploading: [],
  shareModal: null,
  deleteConfirm: null,
  filter: "",
  hashDetails: null,
  checksums: {},
  checksumsLoading: null,
};

function browserReducer(state: BrowserState, action: BrowserAction): BrowserState {
  switch (action.type) {
    case "SET_SORT":
      if (state.sortField === action.field) {
        return { ...state, sortAsc: !state.sortAsc };
      }
      return { ...state, sortField: action.field, sortAsc: true };
    case "SET_FILTER":
      return { ...state, filter: action.filter };
    case "SET_SELECTED":
      return { ...state, selected: action.selected };
    case "SET_DRAGGING":
      return { ...state, dragging: action.dragging };
    case "SET_UPLOADING":
      return { ...state, uploading: action.uploading };
    case "SET_SHARE_MODAL":
      return { ...state, shareModal: action.modal };
    case "SET_DELETE_CONFIRM":
      return { ...state, deleteConfirm: action.name };
    case "RESET_UPLOAD":
      return { ...state, uploading: [] };
    case "TOGGLE_HASH":
      return { ...state, hashDetails: state.hashDetails === action.name ? null : action.name };
    case "SET_CHECKSUMS_LOADING":
      return { ...state, checksumsLoading: action.name };
    case "SET_CHECKSUMS":
      return {
        ...state,
        checksums: { ...state.checksums, [action.name]: action.checksums },
        checksumsLoading: null,
      };
  }
}

// ObjectToolbar

function ObjectToolbar({
  filter,
  selectedCount,
  onFilterChange,
  onBulkDelete,
  onUploadClick,
}: {
  filter: string;
  selectedCount: number;
  onFilterChange: (value: string) => void;
  onBulkDelete: () => void;
  onUploadClick: () => void;
}) {
  return (
    <div className="flex items-center justify-between border-amber-200/10 border-b px-4 py-3">
      <div className="flex items-center gap-3">
        {/* Search */}
        <div className="relative">
          <span className="icon-[lucide--search] absolute top-1/2 left-2.5 -translate-y-1/2 text-stone-600 text-xs" />
          <input
            type="text"
            value={filter}
            onChange={(e) => onFilterChange(e.target.value)}
            placeholder="Filter objects"
            className="rounded-md border border-amber-200/5 bg-surface py-1.5 pr-3 pl-8 font-mono text-xs outline-none placeholder:text-stone-600 focus:border-amber-500"
          />
        </div>

        {selectedCount > 0 && (
          <button
            type="button"
            onClick={onBulkDelete}
            className="flex items-center gap-1.5 rounded-md border border-red-500/20 bg-red-500/5 px-3 py-1.5 font-body text-red-400 text-xs transition-colors hover:bg-red-500/10"
          >
            <span className="icon-[lucide--trash-2] block text-xs" />
            Delete {selectedCount}
          </button>
        )}
      </div>

      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={onUploadClick}
          className="flex items-center gap-1.5 rounded-md bg-amber-500 px-3 py-1.5 font-body font-medium text-black text-xs transition-colors hover:bg-amber-400"
        >
          <span className="icon-[lucide--upload] block text-xs" />
          Upload
        </button>
      </div>
    </div>
  );
}

// UploadProgress
function UploadProgress({ uploading }: { uploading: { name: string; progress: number }[] }) {
  if (uploading.length === 0) return null;
  return (
    <div className="border-amber-200/10 border-b bg-surface px-4 py-3">
      <div className="mb-2 flex items-center gap-2">
        <span className="icon-[lucide--upload] text-amber-500 text-xs" />
        <span className="font-body text-stone-400 text-xs">
          Uploading {uploading.length} file{uploading.length > 1 ? "s" : ""}
        </span>
      </div>
      {uploading.map((u) => (
        <div key={u.name} className="mb-1.5 last:mb-0">
          <div className="mb-1 flex items-center justify-between">
            <span className="truncate font-mono text-[11px] text-stone-400">
              <bdi>{safeDisplayName(u.name)}</bdi>
            </span>
            <span className="font-mono text-[10px] text-stone-600">{u.progress}%</span>
          </div>
          <div className="h-1 overflow-hidden rounded-full bg-surface-overlay">
            <div
              className="h-full rounded-full bg-amber-500 transition-all"
              style={{ width: `${u.progress}%` }}
            />
          </div>
        </div>
      ))}
    </div>
  );
}

// ObjectTable
function ObjectTable({
  bucketName,
  prefix,
  sortField,
  sortAsc,
  selected,
  filteredFolders,
  sortedFiles,
  filesCount,
  hashDetails,
  checksums,
  checksumsLoading,
  onToggleSort,
  onSelectAll,
  onToggleSelect,
  onDownload,
  onShare,
  onDeleteConfirm,
  onToggleHash,
  displayName,
}: {
  bucketName: string;
  prefix: string;
  sortField: SortField;
  sortAsc: boolean;
  selected: Set<string>;
  filteredFolders: string[];
  sortedFiles: FileEntry[];
  filesCount: number;
  hashDetails: string | null;
  checksums: Record<string, Checksums>;
  checksumsLoading: string | null;
  onToggleSort: (field: SortField) => void;
  onSelectAll: () => void;
  onToggleSelect: (name: string) => void;
  onDownload: (name: string) => void;
  onShare: (name: string) => void;
  onDeleteConfirm: (name: string) => void;
  onToggleHash: (name: string) => void;
  displayName: (name: string) => string;
}) {
  return (
    <>
      {/* Column headers */}
      <div className="grid grid-cols-[auto_1fr_100px_160px_140px_100px] items-center gap-4 border-amber-200/10 border-b px-4 py-2">
        <input
          type="checkbox"
          checked={selected.size === filesCount && filesCount > 0}
          onChange={onSelectAll}
          className="h-3.5 w-3.5 accent-amber-500"
        />
        <button
          type="button"
          onClick={() => onToggleSort("name")}
          className="flex items-center gap-1 font-mono text-[10px] text-stone-600 uppercase tracking-wider hover:text-stone-400"
        >
          Name
          {sortField === "name" && (
            <span
              className={`text-[10px] text-amber-500 ${sortAsc ? "icon-[lucide--chevron-up]" : "icon-[lucide--chevron-down]"}`}
            />
          )}
        </button>
        <button
          type="button"
          onClick={() => onToggleSort("size")}
          className="flex items-center gap-1 font-mono text-[10px] text-stone-600 uppercase tracking-wider hover:text-stone-400"
        >
          Size
          {sortField === "size" && (
            <span
              className={`text-[10px] text-amber-500 ${sortAsc ? "icon-[lucide--chevron-up]" : "icon-[lucide--chevron-down]"}`}
            />
          )}
        </button>
        <button
          type="button"
          onClick={() => onToggleSort("lastModified")}
          className="flex items-center gap-1 font-mono text-[10px] text-stone-600 uppercase tracking-wider hover:text-stone-400"
        >
          Modified
          {sortField === "lastModified" && (
            <span
              className={`text-[10px] text-amber-500 ${sortAsc ? "icon-[lucide--chevron-up]" : "icon-[lucide--chevron-down]"}`}
            />
          )}
        </button>
        <span className="font-mono text-[10px] text-stone-600 uppercase tracking-wider">
          Locations
        </span>
        <span />
      </div>

      {/* Rows */}
      <div className="divide-y divide-amber-200/10">
        {/* Go up */}
        {prefix && (
          <Link
            href={`/${encodeURIComponent(bucketName)}${
              prefix.split("/").filter(Boolean).length > 1
                ? `?prefix=${encodeURIComponent(`${prefix.split("/").slice(0, -2).join("/")}/`)}`
                : ""
            }`}
            className="grid grid-cols-[auto_1fr_100px_160px_140px_100px] items-center gap-4 px-4 py-2.5 transition-colors hover:bg-surface"
          >
            <span className="h-3.5 w-3.5" />
            <span className="flex items-center gap-2 font-mono text-sm text-stone-400">
              <span className="icon-[lucide--corner-left-up] text-sm text-stone-600" />
              ..
            </span>
            <span />
            <span />
            <span />
            <span />
          </Link>
        )}

        {/* Folders */}
        {filteredFolders.map((folder) => (
          <Link
            key={folder}
            href={`/${encodeURIComponent(bucketName)}?prefix=${encodeURIComponent(folder)}`}
            className="grid grid-cols-[auto_1fr_100px_160px_140px_100px] items-center gap-4 px-4 py-2.5 transition-colors hover:bg-surface"
          >
            <span className="h-3.5 w-3.5" />
            <span className="flex items-center gap-2 truncate font-mono text-sm">
              <span className="icon-[lucide--folder] text-amber-500 text-sm" />
              <bdi>{displayName(folder)}</bdi>
            </span>
            <span className="font-mono text-stone-600 text-xs">-</span>
            <span className="font-mono text-stone-600 text-xs">-</span>
            <span />
            <span />
          </Link>
        ))}

        {/* Files */}
        {sortedFiles.map((file) => (
          <div key={file.name}>
            <div className="grid grid-cols-[auto_1fr_100px_160px_140px_100px] items-center gap-4 px-4 py-2.5 transition-colors hover:bg-surface">
              <input
                type="checkbox"
                checked={selected.has(file.name)}
                onChange={() => onToggleSelect(file.name)}
                className="h-3.5 w-3.5 accent-amber-500"
              />
              <span className="flex items-center gap-2 truncate font-mono text-sm">
                <span className="icon-[lucide--file] text-sm text-stone-600" />
                <bdi>{displayName(file.name)}</bdi>
              </span>
              <span className="font-mono text-stone-400 text-xs">{file.size}</span>
              <span className="font-mono text-stone-600 text-xs">{file.lastModified}</span>
              <div className="flex flex-col gap-0.5">
                {(file.locations || []).map((loc) => (
                  <span
                    key={loc}
                    className="w-fit rounded bg-surface-overlay px-1.5 py-0.5 font-mono text-[9px] text-stone-600 leading-tight"
                  >
                    <bdi>{safeDisplayName(loc)}</bdi>
                  </span>
                ))}
              </div>
              <div className="flex items-center gap-1">
                <button
                  type="button"
                  onClick={() => onToggleHash(file.name)}
                  className={`flex h-7 w-7 items-center justify-center rounded-md transition-colors ${
                    hashDetails === file.name
                      ? "bg-amber-500/10 text-amber-500"
                      : "text-stone-600 hover:bg-surface-overlay hover:text-stone-400"
                  }`}
                  title="Checksums"
                >
                  <span className="icon-[lucide--hash] block text-xs" />
                </button>
                <button
                  type="button"
                  onClick={() => onDownload(file.name)}
                  className="flex h-7 w-7 items-center justify-center rounded-md text-stone-600 transition-colors hover:bg-surface-overlay hover:text-stone-400"
                  title="Download"
                >
                  <span className="icon-[lucide--download] block text-xs" />
                </button>
                <button
                  type="button"
                  onClick={() => onShare(file.name)}
                  className="flex h-7 w-7 items-center justify-center rounded-md text-stone-600 transition-colors hover:bg-surface-overlay hover:text-stone-400"
                  title="Share"
                >
                  <span className="icon-[lucide--link] block text-xs" />
                </button>
                <button
                  type="button"
                  onClick={() => onDeleteConfirm(file.name)}
                  className="flex h-7 w-7 items-center justify-center rounded-md text-stone-600 transition-colors hover:bg-red-500/10 hover:text-red-400"
                  title="Delete"
                >
                  <span className="icon-[lucide--trash-2] block text-xs" />
                </button>
              </div>
            </div>

            {/* Hash details */}
            {hashDetails === file.name && (
              <div className="border-amber-200/10 border-t bg-surface px-4 py-3 pl-12">
                {checksumsLoading === file.name ? (
                  <span className="font-mono text-[11px] text-stone-600">
                    Computing checksums...
                  </span>
                ) : checksums[file.name] ? (
                  <div className="grid grid-cols-[60px_1fr] gap-x-3 gap-y-1.5">
                    {file.etag && (
                      <>
                        <span className="font-mono text-[10px] text-stone-600 uppercase tracking-wider">
                          ETag
                        </span>
                        <span className="truncate font-mono text-[11px] text-stone-400">
                          {file.etag}
                        </span>
                      </>
                    )}
                    <span className="font-mono text-[10px] text-stone-600 uppercase tracking-wider">
                      MD5
                    </span>
                    <span className="truncate font-mono text-[11px] text-stone-400">
                      {checksums[file.name].md5}
                    </span>
                    <span className="font-mono text-[10px] text-stone-600 uppercase tracking-wider">
                      SHA-1
                    </span>
                    <span className="truncate font-mono text-[11px] text-stone-400">
                      {checksums[file.name].sha1}
                    </span>
                    <span className="font-mono text-[10px] text-stone-600 uppercase tracking-wider">
                      SHA-256
                    </span>
                    <span className="truncate font-mono text-[11px] text-stone-400">
                      {checksums[file.name].sha256}
                    </span>
                    <span className="font-mono text-[10px] text-stone-600 uppercase tracking-wider">
                      SHA-512
                    </span>
                    <span className="truncate font-mono text-[11px] text-stone-400">
                      {checksums[file.name].sha512}
                    </span>
                  </div>
                ) : (
                  <span className="font-mono text-[11px] text-stone-600">Loading...</span>
                )}
              </div>
            )}
          </div>
        ))}
      </div>
    </>
  );
}

// Share Modal
function ShareDialog({
  shareModal,
  onClose,
}: {
  shareModal: { name: string; url: string };
  onClose: () => void;
}) {
  return (
    <Dialog open onClose={onClose} className="max-w-md">
      <div className="w-full rounded-xl border border-amber-200/5 bg-surface p-6">
        <div className="mb-4 flex items-center justify-between">
          <h3 className="font-display font-semibold text-base">Share Object</h3>
          <button
            type="button"
            onClick={onClose}
            className="flex h-7 w-7 items-center justify-center rounded-md text-stone-600 hover:bg-surface-overlay hover:text-stone-100"
          >
            <span className="icon-[lucide--x] block text-sm" />
          </button>
        </div>
        <p className="mb-3 truncate font-mono text-stone-600 text-xs">
          <bdi>{safeDisplayName(shareModal.name)}</bdi>
        </p>
        <div className="mb-4 flex items-center gap-2 rounded-lg border border-amber-200/5 bg-abyss p-3">
          <input
            type="text"
            value={shareModal.url}
            readOnly
            className="flex-1 bg-transparent font-mono text-stone-400 text-xs outline-none"
          />
          <button
            type="button"
            onClick={() => navigator.clipboard.writeText(shareModal.url)}
            className="shrink-0 rounded-md bg-amber-500 px-3 py-1.5 font-body font-medium text-black text-xs"
          >
            Copy
          </button>
        </div>
        <p className="font-body text-[11px] text-stone-600">This link expires in 5 minutes.</p>
      </div>
    </Dialog>
  );
}

// DeleteDialog
function DeleteDialog({
  displayName,
  onConfirm,
  onCancel,
}: {
  displayName: string;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  return (
    <Dialog open onClose={onCancel} className="max-w-sm">
      <div className="w-full rounded-xl border border-amber-200/5 bg-surface p-6">
        <div className="mb-1 flex items-center gap-2">
          <span className="icon-[lucide--alert-triangle] text-base text-red-400" />
          <h3 className="font-display font-semibold text-base">Delete Object</h3>
        </div>
        <p className="mb-4 font-body text-sm text-stone-400">
          Are you sure you want to delete{" "}
          <span className="font-mono">
            <bdi>{displayName}</bdi>
          </span>
          ? This cannot be undone.
        </p>
        <div className="flex gap-2">
          <button
            type="button"
            onClick={onConfirm}
            className="flex-1 rounded-md bg-red-500 px-4 py-2 font-body font-medium text-sm text-white"
          >
            Delete
          </button>
          <button
            type="button"
            onClick={onCancel}
            className="rounded-md border border-amber-200/5 px-4 py-2 font-body text-sm text-stone-600"
          >
            Cancel
          </button>
        </div>
      </div>
    </Dialog>
  );
}

// ObjectBrowser (orchestrator)
export function ObjectBrowser({ bucketName, prefix, folders, files }: Props) {
  const router = useRouter();
  const fileInputRef = useRef<HTMLInputElement>(null);

  const [state, dispatch] = useReducer(browserReducer, initialState);

  const {
    sortField,
    sortAsc,
    selected,
    dragging,
    uploading,
    shareModal,
    deleteConfirm,
    filter,
    hashDetails,
    checksums,
    checksumsLoading,
  } = state;

  // Sorting
  const sortedFiles = [...files]
    .filter((f) => f.name.toLowerCase().includes(filter.toLowerCase()))
    .sort((a, b) => {
      let cmp = 0;
      if (sortField === "name") cmp = a.name.localeCompare(b.name);
      else if (sortField === "size") cmp = a.sizeBytes - b.sizeBytes;
      else cmp = new Date(a.lastModified).getTime() - new Date(b.lastModified).getTime();
      return sortAsc ? cmp : -cmp;
    });

  const filteredFolders = folders.filter((f) => f.toLowerCase().includes(filter.toLowerCase()));

  const toggleSort = (field: SortField) => {
    dispatch({ type: "SET_SORT", field });
  };

  const toggleSelect = (name: string) => {
    const next = new Set(selected);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    dispatch({ type: "SET_SELECTED", selected: next });
  };

  const selectAll = () => {
    if (selected.size === files.length) dispatch({ type: "SET_SELECTED", selected: new Set() });
    else dispatch({ type: "SET_SELECTED", selected: new Set(files.map((f) => f.name)) });
  };

  // Recursively read all files from a directory entry
  const readEntryFiles = useCallback(
    (entry: FileSystemEntry, basePath: string): Promise<{ file: File; path: string }[]> => {
      return new Promise((resolve) => {
        if (entry.isFile) {
          (entry as FileSystemFileEntry).file(
            (file) => {
              resolve([{ file, path: basePath + file.name }]);
            },
            () => resolve([]),
          );
        } else if (entry.isDirectory) {
          const reader = (entry as FileSystemDirectoryEntry).createReader();
          const results: Promise<{ file: File; path: string }[]>[] = [];
          const readBatch = () => {
            reader.readEntries(
              (entries) => {
                if (entries.length === 0) {
                  Promise.all(results).then((arrays) => resolve(arrays.flat()));
                } else {
                  for (const e of entries) {
                    results.push(readEntryFiles(e, `${basePath}${entry.name}/`));
                  }
                  readBatch();
                }
              },
              () => resolve([]),
            );
          };
          readBatch();
        } else {
          resolve([]);
        }
      });
    },
    [],
  );

  // Upload files with their relative paths preserved
  const uploadFiles = useCallback(
    async (items: { file: File; path: string }[]) => {
      if (items.length === 0) return;
      const entries = items.map((f) => ({ name: f.path, progress: 0 }));
      dispatch({ type: "SET_UPLOADING", uploading: entries });

      for (let i = 0; i < items.length; i++) {
        const { file, path: relativePath } = items[i];
        const _objectName = prefix + relativePath;

        try {
          // Get presigned PUT URL from backend
          const urlResult = await getUploadURL(bucketName, prefix, relativePath);
          if (!urlResult.url) throw new Error("Failed to get upload URL");

          await new Promise<void>((resolve, reject) => {
            const xhr = new XMLHttpRequest();
            xhr.open("PUT", urlResult.url);
            xhr.upload.onprogress = (e) => {
              if (e.lengthComputable) {
                dispatch({
                  type: "SET_UPLOADING",
                  uploading: entries.map((u, idx) =>
                    idx === i ? { ...u, progress: Math.round((e.loaded / e.total) * 100) } : u,
                  ),
                });
              }
            };
            xhr.onload = () =>
              xhr.status < 400 ? resolve() : reject(new Error(`HTTP ${xhr.status}`));
            xhr.onerror = () => reject(new Error("Upload failed"));
            xhr.send(file);
          });
        } catch {
          // Continue uploading remaining files
        }
      }

      dispatch({ type: "RESET_UPLOAD" });
      router.refresh();
    },
    [bucketName, prefix, router],
  );

  const handleDrop = useCallback(
    async (e: React.DragEvent) => {
      e.preventDefault();
      dispatch({ type: "SET_DRAGGING", dragging: false });

      // Use webkitGetAsEntry to support directory drops
      const dataItems = e.dataTransfer.items;
      if (dataItems?.length) {
        const allFiles: { file: File; path: string }[] = [];
        const promises: Promise<void>[] = [];

        for (let i = 0; i < dataItems.length; i++) {
          const entry = dataItems[i].webkitGetAsEntry?.();
          if (entry) {
            promises.push(
              readEntryFiles(entry, "").then((files) => {
                allFiles.push(...files);
              }),
            );
          }
        }

        await Promise.all(promises);
        if (allFiles.length > 0) {
          uploadFiles(allFiles);
          return;
        }
      }

      // Fallback for browsers without webkitGetAsEntry
      const files = Array.from(e.dataTransfer.files);
      if (files.length > 0) {
        uploadFiles(files.map((f) => ({ file: f, path: f.name })));
      }
    },
    [uploadFiles, readEntryFiles],
  );

  // Actions
  const handleToggleHash = async (objectName: string) => {
    dispatch({ type: "TOGGLE_HASH", name: objectName });
    if (hashDetails === objectName) return;
    if (checksums[objectName]) return;
    dispatch({ type: "SET_CHECKSUMS_LOADING", name: objectName });
    const result = await getObjectChecksums(bucketName, objectName);
    if ("md5" in result) {
      dispatch({ type: "SET_CHECKSUMS", name: objectName, checksums: result });
    }
  };

  const handleDownload = async (objectName: string) => {
    const result = await getDownloadURL(bucketName, objectName);
    if (result.url) {
      window.open(result.url, "_blank");
    }
  };

  const handleShare = async (objectName: string) => {
    const result = await getShareLink(bucketName, objectName);
    if (result.url)
      dispatch({ type: "SET_SHARE_MODAL", modal: { name: objectName, url: result.url } });
  };

  const handleDelete = async (objectName: string) => {
    await deleteObjectAction(bucketName, objectName);
    dispatch({ type: "SET_DELETE_CONFIRM", name: null });
    const next = new Set(selected);
    next.delete(objectName);
    dispatch({ type: "SET_SELECTED", selected: next });
    router.refresh();
  };

  const handleBulkDelete = async () => {
    for (const name of selected) {
      await deleteObjectAction(bucketName, name);
    }
    dispatch({ type: "SET_SELECTED", selected: new Set() });
    router.refresh();
  };

  const getDisplayName = (name: string) => {
    const withoutPrefix = name.startsWith(prefix) ? name.slice(prefix.length) : name;
    return safeDisplayName(withoutPrefix.replace(/\/$/, ""));
  };

  return (
    <section
      role="application"
      onDragOver={(e) => {
        e.preventDefault();
        dispatch({ type: "SET_DRAGGING", dragging: true });
      }}
      onDragLeave={() => dispatch({ type: "SET_DRAGGING", dragging: false })}
      onDrop={handleDrop}
      className={`overflow-hidden rounded-xl border transition-colors ${
        dragging ? "border-amber-500 bg-amber-500/10" : "border-amber-200/10 bg-abyss"
      }`}
    >
      {/* Toolbar */}
      <ObjectToolbar
        filter={filter}
        selectedCount={selected.size}
        onFilterChange={(value) => dispatch({ type: "SET_FILTER", filter: value })}
        onBulkDelete={handleBulkDelete}
        onUploadClick={() => fileInputRef.current?.click()}
      />
      <input
        ref={fileInputRef}
        type="file"
        multiple
        className="hidden"
        onChange={(e) => {
          if (e.target.files?.length)
            uploadFiles(Array.from(e.target.files).map((f) => ({ file: f, path: f.name })));
          e.target.value = "";
        }}
      />

      {/* Upload progress */}
      <UploadProgress uploading={uploading} />

      {/* Table */}
      <ObjectTable
        bucketName={bucketName}
        prefix={prefix}
        sortField={sortField}
        sortAsc={sortAsc}
        selected={selected}
        filteredFolders={filteredFolders}
        sortedFiles={sortedFiles}
        filesCount={files.length}
        hashDetails={hashDetails}
        checksums={checksums}
        checksumsLoading={checksumsLoading}
        onToggleSort={toggleSort}
        onSelectAll={selectAll}
        onToggleSelect={toggleSelect}
        onDownload={handleDownload}
        onShare={handleShare}
        onDeleteConfirm={(name) => dispatch({ type: "SET_DELETE_CONFIRM", name })}
        onToggleHash={handleToggleHash}
        displayName={getDisplayName}
      />

      {/* Drop overlay when dragging */}
      {dragging && (
        <div className="flex items-center justify-center bg-amber-500/10 py-8">
          <div className="text-center">
            <span className="icon-[lucide--upload-cloud] mx-auto mb-2 block text-3xl text-amber-500" />
            <p className="font-body text-amber-500 text-sm">Drop files to upload</p>
          </div>
        </div>
      )}

      {/* Empty state */}
      {!dragging && sortedFiles.length === 0 && filteredFolders.length === 0 && !prefix && (
        <div className="flex items-center justify-center py-12">
          <div className="text-center">
            <span className="icon-[lucide--upload-cloud] mx-auto mb-2 block text-2xl text-stone-600" />
            <p className="font-body text-stone-600 text-xs">
              Drag and drop files here or use the Upload button
            </p>
          </div>
        </div>
      )}

      {/* Share modal */}
      {shareModal && (
        <ShareDialog
          shareModal={shareModal}
          onClose={() => dispatch({ type: "SET_SHARE_MODAL", modal: null })}
        />
      )}

      {/* Delete confirmation */}
      {deleteConfirm && (
        <DeleteDialog
          displayName={getDisplayName(deleteConfirm)}
          onConfirm={() => handleDelete(deleteConfirm)}
          onCancel={() => dispatch({ type: "SET_DELETE_CONFIRM", name: null })}
        />
      )}
    </section>
  );
}
