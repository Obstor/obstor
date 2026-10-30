"use client";

import { useActionState, useState } from "react";
import { loginAction } from "@/lib/actions";

export function LoginForm() {
  const [state, formAction, pending] = useActionState(
    (_prev: { error: string } | null, formData: FormData) => loginAction(formData),
    null,
  );
  const [accessKey, setAccessKey] = useState("");
  const [showSecret, setShowSecret] = useState(false);

  return (
    <form action={formAction} className="space-y-4">
      {state?.error && (
        <div
          role="alert"
          className="flex items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/5 px-4 py-3 text-sm text-red-400"
        >
          <span className="icon-[tabler--alert-circle] shrink-0 text-base" />
          {state.error}
        </div>
      )}
      <div>
        <label
          htmlFor="accessKey"
          className="font-body text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase"
        >
          Access Key
        </label>
        <input
          id="accessKey"
          name="accessKey"
          type="text"
          required
          // biome-ignore lint/a11y/noAutofocus: signing in is the only task on this page
          autoFocus
          autoComplete="username"
          autoCapitalize="none"
          autoCorrect="off"
          spellCheck={false}
          value={accessKey}
          onChange={(e) => setAccessKey(e.target.value)}
          className="bg-surface mt-1.5 h-10 w-full rounded-lg border border-stone-800 px-3 font-mono text-base text-stone-100 transition-colors outline-none placeholder:text-stone-500 focus:border-amber-500 disabled:opacity-50 sm:text-sm"
        />
      </div>
      <div>
        <label
          htmlFor="secretKey"
          className="font-body text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase"
        >
          Secret Key
        </label>
        <div className="relative mt-1.5">
          <input
            id="secretKey"
            name="secretKey"
            type={showSecret ? "text" : "password"}
            required
            autoComplete="current-password"
            autoCapitalize="none"
            autoCorrect="off"
            spellCheck={false}
            className="bg-surface font-body h-10 w-full rounded-lg border border-stone-800 px-3 pr-10 text-base text-stone-100 transition-colors outline-none placeholder:text-stone-500 focus:border-amber-500 disabled:opacity-50 sm:text-sm"
          />
          <button
            type="button"
            onClick={() => setShowSecret((s) => !s)}
            aria-label="Show secret key"
            aria-pressed={showSecret}
            title={showSecret ? "Hide secret key" : "Show secret key"}
            className="absolute inset-y-0 right-0 flex w-10 items-center justify-center rounded-r-lg text-lg text-stone-400 transition-colors hover:text-stone-100"
          >
            <span className={showSecret ? "icon-[tabler--eye-off]" : "icon-[tabler--eye]"} />
          </button>
        </div>
      </div>

      <button
        type="submit"
        disabled={pending}
        className="font-body inline-flex h-10 w-full shrink-0 items-center justify-center gap-2 rounded-lg bg-amber-500 px-4 text-[15px] leading-[calc(20/14)] font-semibold whitespace-nowrap text-black transition-colors hover:bg-amber-400 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
      >
        {pending ? "Signing in..." : "Sign In"}
      </button>
    </form>
  );
}
