"use client";

import { type FormEvent, useState } from "react";
import { SECRET_MAX, SECRET_MIN, secretFormError } from "@/lib/account";
import { changeSecretAction } from "@/lib/actions";
import { signOut } from "./Avatar";

// IAM users only, caller controls visibility
export function ChangeSecretForm({ accessKey }: { accessKey: string }) {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const invalid = secretFormError(current, next, confirm);
    if (invalid) {
      setError(invalid);
      return;
    }
    setError(null);
    setPending(true);
    let res: { ok: true } | { error: string };
    try {
      res = await changeSecretAction(current, next);
    } catch {
      res = { error: "Could not reach the console server. Try again." };
    }
    if ("error" in res) {
      setError(res.error);
      setPending(false);
      return;
    }
    await signOut(accessKey);
  };

  return (
    <form onSubmit={onSubmit} className="mt-4 space-y-4 border-t border-stone-800/70 pt-4">
      <h3 className="font-display text-[15px] font-semibold text-stone-100">Change secret key</h3>
      {error && (
        <div
          role="alert"
          className="flex items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/5 px-4 py-3 text-sm text-red-400"
        >
          <span aria-hidden="true" className="icon-[tabler--alert-circle] shrink-0 text-base" />
          {error}
        </div>
      )}
      <input
        type="text"
        name="username"
        autoComplete="username"
        value={accessKey}
        readOnly
        hidden
      />
      <div>
        <label
          htmlFor="currentSecret"
          className="font-body text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase"
        >
          Current secret key
        </label>
        <input
          id="currentSecret"
          name="currentSecret"
          type="password"
          autoComplete="current-password"
          value={current}
          onChange={(e) => setCurrent(e.target.value)}
          className="bg-surface mt-1.5 h-9 w-full rounded-lg border border-stone-800 px-3 font-mono text-base text-stone-100 transition-colors outline-none placeholder:text-stone-500 focus:border-amber-500 disabled:opacity-50 sm:text-sm"
        />
      </div>
      <div>
        <label
          htmlFor="newSecret"
          className="font-body text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase"
        >
          New secret key
        </label>
        <input
          id="newSecret"
          name="newSecret"
          type="password"
          autoComplete="new-password"
          aria-describedby="newSecretHint"
          value={next}
          onChange={(e) => setNext(e.target.value)}
          className="bg-surface mt-1.5 h-9 w-full rounded-lg border border-stone-800 px-3 font-mono text-base text-stone-100 transition-colors outline-none placeholder:text-stone-500 focus:border-amber-500 disabled:opacity-50 sm:text-sm"
        />
        <p id="newSecretHint" className="mt-1.5 text-[13px] text-stone-400">
          {SECRET_MIN} to {SECRET_MAX} characters. You are signed out after the change.
        </p>
      </div>
      <div>
        <label
          htmlFor="confirmSecret"
          className="font-body text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase"
        >
          Confirm new secret key
        </label>
        <input
          id="confirmSecret"
          name="confirmSecret"
          type="password"
          autoComplete="new-password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          className="bg-surface mt-1.5 h-9 w-full rounded-lg border border-stone-800 px-3 font-mono text-base text-stone-100 transition-colors outline-none placeholder:text-stone-500 focus:border-amber-500 disabled:opacity-50 sm:text-sm"
        />
      </div>
      <button
        type="submit"
        disabled={pending}
        className="font-body inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg bg-amber-500 px-3.5 text-sm font-semibold whitespace-nowrap text-black transition-colors hover:bg-amber-400 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
      >
        Change secret key
      </button>
    </form>
  );
}
