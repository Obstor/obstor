"use client";

import Link from "next/link";
import { RefreshButton } from "./RefreshButton";

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
              <RefreshButton
                onRefresh={onRetry}
                label="Reload"
                className="font-body inline-flex h-10 flex-1 items-center justify-center gap-2 rounded-lg bg-amber-500 px-4 text-[15px] font-semibold whitespace-nowrap text-black transition-colors hover:bg-amber-400 pointer-coarse:min-h-11"
              >
                Reload
              </RefreshButton>
            ) : (
              <Link
                href="/"
                className="font-body inline-flex h-10 flex-1 items-center justify-center gap-2 rounded-lg bg-amber-500 px-4 text-[15px] font-semibold whitespace-nowrap text-black transition-colors hover:bg-amber-400 pointer-coarse:min-h-11"
              >
                <span aria-hidden="true" className="icon-[tabler--home] text-base" />
                Overview
              </Link>
            )}
            <button
              type="button"
              onClick={() => history.back()}
              className="font-body bg-surface hover:bg-surface-overlay inline-flex h-10 flex-1 items-center justify-center gap-2 rounded-lg border border-stone-800 px-4 text-[15px] font-semibold whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 pointer-coarse:min-h-11"
            >
              <span aria-hidden="true" className="icon-[tabler--arrow-left] text-base" />
              Back
            </button>
          </div>
        </div>
      </div>
    </main>
  );
}
