"use client";

import Link from "next/link";

const BUTTON =
  "inline-flex h-10 pointer-coarse:min-h-11 flex-1 items-center justify-center gap-2 whitespace-nowrap rounded-lg px-4 font-body font-semibold text-[15px] transition-colors";
const PRIMARY = `${BUTTON} bg-amber-500 text-black hover:bg-amber-400`;
const SECONDARY = `${BUTTON} border border-stone-800 bg-surface text-stone-200 hover:border-stone-700 hover:bg-surface-overlay`;

// Shared error card in login layout
export function ErrorScreen({
  code,
  title,
  body,
  onRetry,
}: {
  code: string;
  title: string;
  body: string;
  onRetry?: () => void;
}) {
  return (
    <main className="bg-void flex min-h-dvh items-center justify-center px-4">
      <div className="motion-safe:animate-page-in w-full max-w-[400px]">
        <div className="bg-abyss rounded-xl p-8">
          <div className="mb-8 flex items-center gap-3">
            <span className="icon-[fluent-emoji-high-contrast--lobster] text-[40px] text-amber-500" />
            <span className="font-display text-[28px] font-semibold">Obstor</span>
          </div>
          <p className="font-mono text-sm text-stone-500">{code}</p>
          <h1 className="font-display mt-1 text-xl font-semibold">{title}</h1>
          <p className="mt-1 mb-6 text-sm text-stone-400">{body}</p>
          <div className="flex gap-3">
            {onRetry ? (
              <button type="button" onClick={onRetry} className={PRIMARY}>
                <span aria-hidden="true" className="icon-[tabler--refresh] text-base" />
                Reload
              </button>
            ) : (
              <Link href="/" className={PRIMARY}>
                <span aria-hidden="true" className="icon-[tabler--home] text-base" />
                Overview
              </Link>
            )}
            <button type="button" onClick={() => history.back()} className={SECONDARY}>
              <span aria-hidden="true" className="icon-[tabler--arrow-left] text-base" />
              Back
            </button>
          </div>
        </div>
      </div>
    </main>
  );
}
