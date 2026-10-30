"use client";

import { pageWindow } from "@/lib/objects";

export function Pager({
  page,
  pages,
  onPage,
}: {
  page: number;
  pages: number;
  onPage: (page: number) => void;
}) {
  return (
    <nav aria-label="Pagination" className="flex items-center gap-1">
      <button
        type="button"
        aria-label="Previous page"
        disabled={page <= 1}
        onClick={() => onPage(page - 1)}
        className="bg-surface hover:bg-surface-overlay inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-stone-800 text-lg text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11"
      >
        <span className="icon-[tabler--chevron-left]" />
      </button>
      {pageWindow(page, pages).map((n) => (
        <button
          key={n}
          type="button"
          aria-current={n === page ? "page" : undefined}
          onClick={() => onPage(n)}
          className={`inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg font-mono text-sm transition-colors disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11 ${
            n === page
              ? "border border-amber-500/40 bg-amber-500/10 text-amber-400"
              : "hover:bg-surface text-stone-400 hover:text-stone-100"
          }`}
        >
          {n}
        </button>
      ))}
      <button
        type="button"
        aria-label="Next page"
        disabled={page >= pages}
        onClick={() => onPage(page + 1)}
        className="bg-surface hover:bg-surface-overlay inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-stone-800 text-lg text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11"
      >
        <span className="icon-[tabler--chevron-right]" />
      </button>
    </nav>
  );
}
