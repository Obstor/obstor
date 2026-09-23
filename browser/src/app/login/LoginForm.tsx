"use client";

import { useActionState } from "react";
import { loginAction } from "@/lib/actions";

export function LoginForm() {
  const [state, formAction, pending] = useActionState(
    async (_prev: { error?: string } | null, formData: FormData) => {
      return await loginAction(formData);
    },
    null,
  );

  return (
    <form action={formAction} className="space-y-4">
      {state?.error && (
        <div className="flex items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/5 px-4 py-3">
          <span className="icon-[tabler--alert-circle] shrink-0 text-red-400 text-sm" />
          <span className="font-body text-red-400 text-sm">{state.error}</span>
        </div>
      )}

      <div>
        <label
          htmlFor="accessKey"
          className="mb-1.5 block font-body font-medium text-stone-400 text-xs"
        >
          Access Key
        </label>
        <input
          id="accessKey"
          name="accessKey"
          type="text"
          required
          className="w-full rounded-lg border border-amber-200/5 bg-surface px-4 py-2.5 font-mono text-sm outline-none transition-colors placeholder:text-stone-600 focus:border-amber-500"
        />
      </div>

      <div>
        <label
          htmlFor="secretKey"
          className="mb-1.5 block font-body font-medium text-stone-400 text-xs"
        >
          Secret Key
        </label>
        <input
          id="secretKey"
          name="secretKey"
          type="password"
          required
          className="w-full rounded-lg border border-amber-200/5 bg-surface px-4 py-2.5 font-mono text-sm outline-none transition-colors placeholder:text-stone-600 focus:border-amber-500"
        />
      </div>

      <button
        type="submit"
        disabled={pending}
        className="w-full rounded-lg bg-amber-500 py-2.5 font-body font-medium text-black text-sm transition-all hover:bg-amber-400 disabled:opacity-50"
      >
        {pending ? "Signing in..." : "Sign In"}
      </button>
    </form>
  );
}
