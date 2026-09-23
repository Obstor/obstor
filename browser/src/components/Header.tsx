"use client";

import { usePathname } from "next/navigation";
import { safeDisplayName } from "@/lib/safe-name";

export function Header() {
  const pathname = usePathname();
  const parts = pathname.split("/").filter(Boolean);
  const segment = parts[0] ? decodeURIComponent(parts[0]) : null;
  // Console route, duplicate names are refused at create time.
  const bucket = segment && segment !== "access" ? segment : null;

  return (
    <header className="flex h-14 shrink-0 items-center justify-between border-amber-200/10 border-b bg-abyss px-6">
      <div className="flex items-center gap-2 font-mono text-sm text-stone-400">
        {bucket ? (
          <>
            <span className="icon-[tabler--server] text-stone-600" />
            <span className="text-stone-100">
              <bdi>{safeDisplayName(bucket)}</bdi>
            </span>
          </>
        ) : segment === "access" ? (
          <>
            <span className="icon-[tabler--key] text-stone-600" />
            <span className="text-stone-100">Access</span>
          </>
        ) : (
          <>
            <span className="icon-[tabler--layout-dashboard] text-stone-600" />
            <span className="text-stone-100">Overview</span>
          </>
        )}
      </div>

      <div className="flex items-center gap-2">
        <a
          href="https://github.com/obstor/obstor"
          target="_blank"
          rel="noopener noreferrer"
          className="flex h-8 w-8 items-center justify-center rounded-md text-stone-600 transition-colors hover:bg-surface hover:text-stone-400"
          title="GitHub"
        >
          <span className="icon-[tabler--brand-github] block text-sm" />
        </a>
        <a
          href="https://obstor.net/docs"
          target="_blank"
          rel="noopener noreferrer"
          className="flex h-8 w-8 items-center justify-center rounded-md text-stone-600 transition-colors hover:bg-surface hover:text-stone-400"
          title="Documentation"
        >
          <span className="icon-[tabler--book] block text-sm" />
        </a>
      </div>
    </header>
  );
}
