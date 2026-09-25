"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";
import { deleteBucketAction, logoutAction } from "@/lib/actions";
import { safeDisplayName } from "@/lib/safe-name";
import { BucketModal } from "./BucketModal";
import { Dialog } from "./Dialog";

interface Props {
  buckets: { name: string; creationDate: string }[];
  storageUsed: string;
  bucketCount: number;
  serverVersion: string;
  serverPlatform: string;
  access: { canManage: boolean; noPolicy: number };
}

export function Sidebar({
  buckets,
  storageUsed,
  bucketCount,
  serverVersion,
  serverPlatform,
  access,
}: Props) {
  const pathname = usePathname();
  const firstSegment = decodeURIComponent(pathname.split("/")[1] || "");
  const isAccess = firstSegment === "access";
  const activeBucket = isAccess ? "" : firstSegment;
  const [filter, setFilter] = useState("");
  const [modalOpen, setModalOpen] = useState(false);
  const [editBucket, setEditBucket] = useState<string | null>(null);
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null);
  const [deleteError, setDeleteError] = useState("");

  // Layout state survives the post-delete redirect, so close once the bucket is gone
  if (deleteConfirm && !buckets.some((b) => b.name === deleteConfirm)) setDeleteConfirm(null);

  const attentionTitle = `${access.noPolicy} accounts with no direct policy`;

  const filtered = buckets.filter((b) => b.name.toLowerCase().includes(filter.toLowerCase()));

  const openCreate = () => {
    setEditBucket(null);
    setModalOpen(true);
  };

  const openEdit = (bucketName: string, e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setEditBucket(bucketName);
    setModalOpen(true);
  };

  const handleDelete = async (bucketName: string) => {
    setDeleteError("");
    const res = await deleteBucketAction(bucketName);
    // Redirects on success, so reaching here means it failed.
    setDeleteError(res.error);
  };

  return (
    <>
      <aside className="flex h-full w-64 shrink-0 flex-col border-amber-200/10 border-r bg-abyss">
        {/* Logo */}
        <Link
          href="/"
          className="flex items-center gap-2.5 border-amber-200/10 border-b px-4 py-4 transition-colors hover:bg-surface/30"
        >
          <span className="icon-[fluent-emoji-high-contrast--lobster] h-8 w-8 text-amber-500 text-lg" />
          <div>
            <p className="font-display font-semibold text-sm leading-tight">Obstor</p>
            <p className="font-mono text-[10px] text-stone-600">{storageUsed} used</p>
          </div>
        </Link>

        {/* Search + create */}
        <div className="border-amber-200/10 border-b px-3 py-3">
          <div className="flex items-center gap-2">
            <div className="relative flex-1">
              <span className="icon-[tabler--search] pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-stone-600 text-xs" />
              <input
                type="text"
                placeholder="Filter buckets..."
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                className="w-full rounded-md border border-amber-200/5 bg-surface py-1.5 pr-3 pl-8 font-mono text-xs outline-none transition-colors placeholder:text-stone-600 focus:border-amber-500"
              />
            </div>
            <button
              type="button"
              onClick={openCreate}
              aria-label="Create bucket"
              className="flex h-[30px] w-[30px] shrink-0 items-center justify-center rounded-md bg-amber-500 text-black transition-colors hover:bg-amber-400"
              title="Create bucket"
            >
              <span className="icon-[tabler--plus] block text-sm" />
            </button>
          </div>
        </div>

        {/* Access */}
        {access.canManage && (
          <div className="border-amber-200/10 border-b px-1.5 py-1.5">
            <Link
              href="/access"
              className={`flex items-center gap-2.5 rounded-md px-3 py-2 transition-colors ${
                isAccess ? "bg-surface" : "hover:bg-surface/50"
              }`}
            >
              <span
                className={`icon-[tabler--key] text-xs ${
                  isAccess ? "text-amber-500" : "text-stone-600"
                }`}
              />
              <span
                className={`flex-1 font-mono text-xs ${
                  isAccess ? "text-stone-100" : "text-stone-400"
                }`}
              >
                Access
              </span>
              {access.noPolicy > 0 && (
                <span
                  title={attentionTitle}
                  className="rounded border border-amber-200/15 px-1.5 py-0.5 font-mono text-[9px] text-stone-500"
                >
                  {access.noPolicy}
                </span>
              )}
            </Link>
          </div>
        )}

        {/* Bucket list */}
        <nav className="flex-1 overflow-y-auto py-1">
          {filtered.map((b) => {
            const isActive = activeBucket === b.name;
            return (
              <div
                key={b.name}
                className={`group relative mx-1.5 mb-0.5 rounded-md ${
                  isActive ? "bg-surface" : "hover:bg-surface/50"
                }`}
              >
                <Link
                  href={`/${encodeURIComponent(b.name)}`}
                  className="flex items-center gap-2.5 px-3 py-2"
                >
                  <span
                    className={`icon-[tabler--server-2] text-xs ${
                      isActive ? "text-amber-500" : "text-stone-600"
                    }`}
                  />
                  <span
                    className={`truncate font-mono text-xs ${
                      isActive ? "text-stone-100" : "text-stone-400"
                    }`}
                  >
                    <bdi>{safeDisplayName(b.name)}</bdi>
                  </span>
                </Link>

                {/* Action buttons (visible on hover or when active) */}
                <div
                  className={`absolute top-1/2 right-1.5 flex -translate-y-1/2 items-center gap-0.5 ${
                    isActive ? "opacity-100" : "opacity-0 group-hover:opacity-100"
                  } transition-opacity`}
                >
                  <button
                    type="button"
                    onClick={(e) => openEdit(b.name, e)}
                    className="flex h-6 w-6 items-center justify-center rounded text-stone-600 transition-colors hover:bg-surface-overlay hover:text-amber-500"
                    title="Bucket settings"
                  >
                    <span className="icon-[tabler--settings] block text-[11px]" />
                  </button>
                  <button
                    type="button"
                    onClick={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                      setDeleteConfirm(b.name);
                    }}
                    className="flex h-6 w-6 items-center justify-center rounded text-stone-600 transition-colors hover:bg-red-500/10 hover:text-red-400"
                    title="Delete bucket"
                  >
                    <span className="icon-[tabler--trash] block text-[11px]" />
                  </button>
                </div>
              </div>
            );
          })}

          {filtered.length === 0 && (
            <div className="px-4 py-5">
              <p className="font-body text-stone-600 text-xs">
                {filter ? "No buckets match that filter." : "No buckets yet."}
              </p>
              {!filter && (
                <button
                  type="button"
                  onClick={openCreate}
                  className="mt-3 font-mono text-[11px] text-amber-500 transition-colors hover:text-amber-400"
                >
                  Create bucket
                </button>
              )}
            </div>
          )}
        </nav>

        {/* Footer */}
        <div className="border-amber-200/10 border-t px-4 py-3">
          <div className="flex items-center justify-between">
            <div>
              <p className="font-mono text-[10px] text-stone-600">
                {bucketCount} bucket{bucketCount !== 1 ? "s" : ""} | v{serverVersion}
              </p>
              <p className="font-mono text-[10px] text-stone-600">{serverPlatform}</p>
            </div>
            <button
              type="button"
              onClick={() => logoutAction()}
              className="flex h-7 w-7 items-center justify-center rounded-md text-stone-600 transition-colors hover:bg-surface hover:text-red-400"
              title="Sign out"
            >
              <span className="icon-[tabler--logout] block text-xs" />
            </button>
          </div>
        </div>
      </aside>

      {/* Bucket Create/Edit Modal */}
      <BucketModal open={modalOpen} onClose={() => setModalOpen(false)} editBucket={editBucket} />

      {/* Delete Confirmation */}
      {deleteConfirm && (
        <Dialog
          open
          onClose={() => {
            setDeleteConfirm(null);
            setDeleteError("");
          }}
          className="max-w-sm"
        >
          <div className="w-full rounded-xl border border-amber-200/5 bg-abyss p-6 shadow-2xl shadow-black/50">
            <div className="mb-4 flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-full bg-red-500/10">
                <span className="icon-[tabler--alert-triangle] text-base text-red-400" />
              </div>
              <div>
                <h3 className="font-display font-semibold text-sm">Delete Bucket</h3>
                <p className="font-mono text-[11px] text-stone-600">
                  <bdi>{safeDisplayName(deleteConfirm)}</bdi>
                </p>
              </div>
            </div>
            <p className="mb-5 font-body text-stone-400 text-xs leading-relaxed">
              This will permanently delete the bucket and all objects inside it. This action cannot
              be undone.
            </p>
            {deleteError && (
              <p className="mb-3 rounded-lg border border-red-500/20 bg-red-500/5 px-3 py-2 font-mono text-[11px] text-red-400">
                Could not delete bucket: {deleteError}
              </p>
            )}
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => handleDelete(deleteConfirm)}
                className="flex-1 rounded-lg bg-red-500 px-4 py-2 font-body font-medium text-sm text-white transition-colors hover:bg-red-500/90"
              >
                Delete
              </button>
              <button
                type="button"
                onClick={() => setDeleteConfirm(null)}
                className="rounded-lg border border-amber-200/5 px-4 py-2 font-body text-sm text-stone-600 transition-colors hover:bg-surface-overlay"
              >
                Cancel
              </button>
            </div>
          </div>
        </Dialog>
      )}
    </>
  );
}
