"use client";

import { unstable_rethrow } from "next/navigation";
import { startTransition, useEffect, useRef, useState } from "react";
import { logoutAction } from "@/lib/actions";

export default function SignOutPage() {
  const started = useRef(false);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    // Strict Mode replays effects in development
    if (started.current) return;
    started.current = true;

    startTransition(async () => {
      try {
        await logoutAction();
      } catch (err) {
        unstable_rethrow(err);
        setFailed(true);
      }
    });
  }, []);

  return (
    <>
      <main className="bg-void flex min-h-dvh items-center justify-center px-4">
        <div className="motion-safe:animate-page-in w-full max-w-[400px]">
          <div className="bg-abyss rounded-xl p-8">
            <div className="mb-8 flex items-center gap-3">
              <span className="icon-[fluent-emoji-high-contrast--lobster] text-[40px] text-amber-500" />
              <span className="font-display text-[28px] font-semibold">Obstor</span>
            </div>
            <div role="status" className="mb-6">
              <h1 className="font-display text-xl font-semibold">
                {failed ? "Sign out couldn't finish" : "Signing out…"}
              </h1>
              <p className="mt-1 text-sm text-stone-400">
                {failed
                  ? "Use the link below to finish signing out."
                  : "If this takes too long, continue to sign in below."}
              </p>
            </div>
            {/* Clears session cookie. */}
            <a
              href="/session-expired"
              className="font-body inline-flex h-10 w-full items-center justify-center rounded-lg bg-amber-500 px-4 text-[15px] font-semibold text-black transition-colors hover:bg-amber-400 pointer-coarse:min-h-11"
            >
              Continue to sign in
            </a>
          </div>
        </div>
      </main>
      <noscript>
        <meta httpEquiv="refresh" content="0;url=/session-expired" />
      </noscript>
    </>
  );
}
