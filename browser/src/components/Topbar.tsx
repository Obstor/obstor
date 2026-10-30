"use client";

import Link from "next/link";
import { type KeyboardEvent, useEffect, useState } from "react";
import { objectsHref } from "@/lib/objects";
import { safeDisplayName } from "@/lib/safe-name";
import { Avatar } from "./Avatar";
import { Bell } from "./Bell";
import { Dialog } from "./Dialog";
import { useLeaveGuard } from "./LeaveGuard";
import type { ShellUser } from "./Shell";
import type { NavState } from "./Sidebar";

interface Hit {
  href: string;
  label: string;
  icon: string;
  bucket?: boolean;
}

export function Topbar({
  onMenu,
  buckets,
  nav,
  user,
}: {
  onMenu: () => void;
  buckets: string[];
  nav: NavState;
  user: ShellUser;
}) {
  const [open, setOpen] = useState(false);
  const [mac, setMac] = useState<boolean | null>(null);

  useEffect(() => {
    setMac(/Mac|iPhone|iPad/.test(navigator.userAgent));
    const onKey = (e: globalThis.KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen(true);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const pages: Hit[] = [
    { href: "/", label: "Overview", icon: "icon-[tabler--home]" },
    { href: "/buckets", label: "Buckets", icon: "icon-[tabler--bucket]" },
  ];
  if (nav.canManage) pages.push({ href: "/access", label: "Access", icon: "icon-[tabler--key]" });
  if (nav.canSeeFleet)
    pages.push({ href: "/cluster", label: "Cluster", icon: "icon-[tabler--server-2]" });
  pages.push({ href: "/activity", label: "Activity", icon: "icon-[tabler--activity]" });
  pages.push({ href: "/settings", label: "Settings", icon: "icon-[tabler--settings]" });

  return (
    <header className="bg-abyss flex h-16 shrink-0 items-center gap-3 border-b border-stone-800/70 px-4 sm:px-8">
      {/* Center search */}
      <div className="flex flex-1 items-center">
        <button
          type="button"
          onClick={onMenu}
          aria-label="Open navigation"
          className="hover:bg-surface inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-lg text-stone-400 transition-colors hover:text-stone-100 disabled:pointer-events-none disabled:opacity-40 lg:hidden pointer-coarse:min-h-11 pointer-coarse:min-w-11"
        >
          <span className="icon-[tabler--menu-2]" />
        </button>
      </div>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="bg-surface flex h-10 w-[560px] min-w-0 items-center gap-3 rounded-lg border border-stone-800/70 px-3 text-left text-sm text-stone-400 transition-colors hover:border-stone-700"
      >
        <span className="icon-[tabler--search] shrink-0 text-lg" />
        <span className="flex-1 truncate">Jump to bucket or page</span>
        {mac !== null && (
          <kbd className="bg-surface hidden h-6 items-center gap-0.5 rounded-md border border-stone-800 px-2 font-mono text-[11px] text-stone-400 pointer-fine:inline-flex">
            {mac ? (
              <>
                <span className="icon-[tabler--command]" />K
              </>
            ) : (
              "Ctrl K"
            )}
          </kbd>
        )}
      </button>
      <nav aria-label="Resources" className="flex flex-1 items-center justify-end gap-1">
        <a
          href="https://obstor.net/docs"
          target="_blank"
          rel="noopener noreferrer"
          aria-label="Docs"
          className="font-body hover:bg-surface inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg px-3.5 text-sm font-medium whitespace-nowrap text-stone-400 transition-colors hover:text-stone-100 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
        >
          <span className="icon-[tabler--book] text-lg" />
          <span className="hidden sm:inline">Docs</span>
        </a>
        <a
          href="https://github.com/obstor/obstor"
          target="_blank"
          rel="noopener noreferrer"
          aria-label="GitHub"
          title="GitHub"
          className="hover:bg-surface hidden h-9 w-9 shrink-0 items-center justify-center rounded-lg text-lg text-stone-400 transition-colors hover:text-stone-100 disabled:pointer-events-none disabled:opacity-40 sm:inline-flex pointer-coarse:min-h-11 pointer-coarse:min-w-11"
        >
          <span className="icon-[tabler--brand-github]" />
        </a>
        <Bell />
        <Avatar user={user} />
      </nav>
      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        label="Jump to"
        className="mt-[12vh] max-w-xl self-start"
      >
        {open && <JumpSearch pages={pages} buckets={buckets} onClose={() => setOpen(false)} />}
      </Dialog>
    </header>
  );
}

function JumpSearch({
  pages,
  buckets,
  onClose,
}: {
  pages: Hit[];
  buckets: string[];
  onClose: () => void;
}) {
  const { navigate } = useLeaveGuard();
  const [q, setQ] = useState("");
  const [active, setActive] = useState(0);
  const needle = q.trim().toLowerCase();
  const hits: Hit[] = [
    ...pages.filter((p) => p.label.toLowerCase().includes(needle)),
    ...buckets
      .filter((b) => b.toLowerCase().includes(needle))
      .map((b) => ({
        href: objectsHref(b),
        label: safeDisplayName(b),
        icon: "icon-[tabler--bucket]",
        bucket: true,
      })),
  ].slice(0, 50);
  const current = Math.min(active, Math.max(0, hits.length - 1));

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      setActive(Math.max(0, Math.min(hits.length - 1, current + (e.key === "ArrowDown" ? 1 : -1))));
    } else if (e.key === "Enter" && !e.nativeEvent.isComposing && hits[current]) {
      onClose();
      navigate(hits[current].href);
    }
  };

  return (
    <div className="bg-abyss w-full overflow-hidden rounded-xl">
      <div className="flex items-center gap-3 border-b border-stone-800/70 px-4">
        <span className="icon-[tabler--search] text-lg text-stone-400" />
        <input
          value={q}
          onChange={(e) => {
            setQ(e.target.value);
            setActive(0);
          }}
          onKeyDown={onKeyDown}
          role="combobox"
          aria-expanded={hits.length > 0}
          aria-controls="jump-list"
          aria-activedescendant={hits[current] ? `jump-${current}` : undefined}
          aria-autocomplete="list"
          placeholder="Jump to bucket or page"
          aria-label="Jump to bucket or page"
          className="h-12 flex-1 bg-transparent text-base outline-none placeholder:text-stone-500 sm:text-sm"
        />
      </div>
      <div className="max-h-[50vh] overflow-y-auto p-1.5">
        {hits.length === 0 && (
          <p className="px-3 py-6 text-center text-sm text-stone-400">Nothing matches.</p>
        )}
        <div id="jump-list" role="listbox" aria-label="Results">
          {hits.map((h, i) => (
            <Link
              key={h.href}
              id={`jump-${i}`}
              role="option"
              aria-selected={i === current}
              href={h.href}
              prefetch={false}
              onClickCapture={onClose} // Close before LeaveGuard can stop click
              onMouseMove={() => setActive(i)}
              ref={i === current ? (el) => el?.scrollIntoView({ block: "nearest" }) : undefined}
              className={`flex h-10 items-center gap-3 rounded-md px-3 text-sm ${
                i === current ? "bg-surface-overlay text-stone-100" : "text-stone-300"
              }`}
            >
              <span
                className={`${h.icon} shrink-0 text-lg ${h.bucket ? "text-amber-500" : "text-stone-400"}`}
              />
              <span className="truncate">
                <bdi>{h.label}</bdi>
              </span>
            </Link>
          ))}
        </div>
      </div>
    </div>
  );
}
