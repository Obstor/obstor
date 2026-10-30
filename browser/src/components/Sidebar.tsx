"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { humanSize, modeLabel } from "@/lib/format";
import { objectsHref } from "@/lib/objects";
import { shortcutBuckets } from "@/lib/pins";
import type { BucketEntry } from "@/lib/rpc";
import { safeDisplayName } from "@/lib/safe-name";
import { AccountMenu, UserAvatar } from "./Avatar";
import { useBuckets } from "./BucketProvider";
import type { ShellUser } from "./Shell";

export interface NavState {
  canManage: boolean;
  noPolicy: number;
  canSeeFleet: boolean;
  attention: boolean; // offline node/drive
}

export interface ShellServer {
  version: string;
  region: string;
  mode: string;
}

export interface SidebarProps {
  user: ShellUser;
  buckets: BucketEntry[];
  used: number | null;
  nav: NavState;
  host: string;
  server: ShellServer;
}

interface NavItem {
  href: string;
  label: string;
  icon: string;
  active: boolean;
  badge?: number;
  badgeTitle?: string;
  dot?: boolean;
}

export function Sidebar({
  user,
  buckets,
  used,
  nav,
  host,
  server,
  onClose,
}: SidebarProps & { onClose?: () => void }) {
  const pathname = usePathname();
  const { pins } = useBuckets();
  const [, section = "", open = ""] = pathname.split("/");
  const openBucket = section === "buckets" && open ? decodeURIComponent(open) : null;
  const shortcuts = shortcutBuckets(buckets, pins);
  const mode = modeLabel(server.mode);
  const username = user.accessKey ? safeDisplayName(user.accessKey) : "Signed in";

  const items: NavItem[] = [
    { href: "/", label: "Overview", icon: "icon-[tabler--home]", active: section === "" },
    {
      href: "/buckets",
      label: "Buckets",
      icon: "icon-[tabler--bucket]",
      active: section === "buckets",
      badge: buckets.length,
    },
  ];
  if (nav.canManage) {
    items.push({
      href: "/access",
      label: "Access",
      icon: "icon-[tabler--key]",
      active: section === "access",
      badge: nav.noPolicy || undefined,
      badgeTitle: `${nav.noPolicy} users with no direct policy`,
    });
  }
  if (nav.canSeeFleet) {
    items.push({
      href: "/cluster",
      label: "Cluster",
      icon: "icon-[tabler--server-2]",
      active: section === "cluster",
      dot: nav.attention,
    });
  }
  items.push({
    href: "/activity",
    label: "Activity",
    icon: "icon-[tabler--activity]",
    active: section === "activity",
  });
  items.push({
    href: "/settings",
    label: "Settings",
    icon: "icon-[tabler--settings]",
    active: section === "settings",
  });

  return (
    <div className="flex h-full min-h-0 w-full flex-col">
      <div className="flex items-center gap-3 px-6 pt-6 pb-5">
        <Link href="/" onClick={onClose} className="flex min-w-0 items-center gap-3">
          <span className="icon-[fluent-emoji-high-contrast--lobster] shrink-0 text-[40px] text-amber-500" />
          <div className="min-w-0">
            <p className="font-display text-[24px] leading-tight font-semibold">Obstor</p>
            <p
              className="mt-0.5 truncate font-mono text-[13px] text-stone-500"
              title="Bytes stored by objects, before parity"
            >
              {used === null ? "—" : humanSize(used)} used
            </p>
          </div>
        </Link>
        {onClose && (
          <button
            type="button"
            onClick={onClose}
            aria-label="Close navigation"
            className="hover:bg-surface -mr-2 ml-auto inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-lg text-stone-400 transition-colors hover:text-stone-100 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11"
          >
            <span className="icon-[tabler--x]" />
          </button>
        )}
      </div>

      <div className="mx-4 mb-4 flex items-start gap-3 rounded-lg border border-stone-800/70 px-3 py-2.5">
        <span className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-amber-500/15 text-amber-500">
          <span className="icon-[tabler--server-2] text-lg" />
        </span>
        <div className="min-w-0">
          <p className="truncate font-mono text-sm text-stone-100" title={host}>
            {host}
          </p>
          {(server.region || mode) && (
            <p className="truncate text-[13px] text-stone-400">
              {server.region}
              {server.region && mode && <span className="text-stone-500"> | </span>}
              {mode}
            </p>
          )}
        </div>
      </div>

      <nav aria-label="Main" className="px-3">
        {items.map((it) => (
          <Link
            key={it.href}
            href={it.href}
            onClick={onClose}
            aria-current={it.active ? "page" : undefined}
            className={`flex h-11 items-center gap-3 rounded-lg px-3 text-[15px] transition-colors ${
              it.active
                ? "bg-amber-500/10 text-amber-400"
                : "hover:bg-surface text-stone-200 hover:text-stone-100"
            }`}
          >
            <span
              className={`${it.icon} text-[22px] ${it.active ? "text-amber-500" : "text-stone-400"}`}
            />
            <span className="flex-1">{it.label}</span>
            {it.badge !== undefined && (
              <>
                <span
                  aria-hidden="true"
                  title={it.badgeTitle}
                  className="bg-surface-overlay inline-flex h-6 min-w-6 items-center justify-center rounded-md px-1.5 font-mono text-xs text-stone-300"
                >
                  {it.badge}
                </span>
                {it.badgeTitle && <span className="sr-only">, {it.badgeTitle}</span>}
              </>
            )}
            {it.dot && (
              <>
                <span
                  aria-hidden="true"
                  title="A node or drive is offline"
                  className="h-2 w-2 rounded-full bg-amber-500"
                />
                <span className="sr-only">, node or drive offline</span>
              </>
            )}
          </Link>
        ))}
      </nav>

      <div className="mx-6 my-4 border-t border-stone-800/70" />

      <div className="min-h-0 flex-1 overflow-y-auto px-3">
        <p className="font-body px-3 pb-2 text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase">
          {shortcuts.pinned ? "Pinned buckets" : "Recent buckets"}
        </p>
        {shortcuts.names.map((name) => (
          <Link
            key={name}
            href={objectsHref(name)}
            onClick={onClose}
            className={`flex h-9 items-center gap-3 rounded-lg px-3 transition-colors ${
              openBucket === name
                ? "bg-amber-500/10 text-stone-100"
                : "hover:bg-surface text-stone-300"
            }`}
          >
            <span className="icon-[tabler--bucket] shrink-0 text-lg text-amber-500" />
            <span className="truncate text-sm" title={safeDisplayName(name)}>
              <bdi>{safeDisplayName(name)}</bdi>
            </span>
          </Link>
        ))}
        {shortcuts.names.length === 0 && (
          <p className="px-3 text-sm text-stone-400">No buckets yet.</p>
        )}
        <Link
          href="/buckets"
          onClick={onClose}
          className="mt-1 flex h-9 items-center gap-2 px-3 text-sm text-stone-400 hover:text-stone-200"
        >
          View all buckets
          <span className="icon-[tabler--arrow-right]" />
        </Link>
      </div>

      <footer className="shrink-0 divide-y divide-stone-800/70">
        <a
          href="https://obstor.net/docs"
          target="_blank"
          rel="noopener noreferrer"
          className="flex h-12 items-center gap-3 px-6 text-sm text-stone-400 transition-colors hover:text-stone-100"
        >
          <span aria-hidden="true" className="icon-[tabler--book] text-lg" />
          Documentation
        </a>
        <div className="flex items-center gap-3 px-4 py-3">
          <UserAvatar className="h-8 w-8" />
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm text-stone-200" title={username}>
              <bdi>{username}</bdi>
            </p>
            {server.version && (
              <p className="mt-0.5 truncate text-xs text-stone-500">{server.version}</p>
            )}
          </div>
          <AccountMenu
            user={user}
            trigger={<span aria-hidden="true" className="icon-[tabler--dots-vertical]" />}
            triggerClassName="inline-flex items-center justify-center rounded-lg text-lg transition-colors disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11 text-stone-400 hover:bg-surface hover:text-stone-100 h-8 w-8 shrink-0"
            onNavigate={onClose}
          />
        </div>
      </footer>
    </div>
  );
}
