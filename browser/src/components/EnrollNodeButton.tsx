"use client";

import { useState } from "react";
import { mintEnrollToken } from "@/app/(dashboard)/actions";
import { Dialog } from "./Dialog";

export function EnrollNodeButton() {
  const [open, setOpen] = useState(false);
  const [command, setCommand] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [copied, setCopied] = useState(false);

  async function onOpen() {
    setOpen(true);
    setLoading(true);
    try {
      const r = await mintEnrollToken();
      setCommand(r.command);
    } catch {
      setCommand(null);
    } finally {
      setLoading(false);
    }
  }

  async function onCopy() {
    if (!command) return;
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // clipboard rejected due to permissions
    }
  }

  return (
    <>
      <button
        type="button"
        onClick={onOpen}
        className="flex items-center gap-1.5 rounded-md border border-amber-200/15 px-2 py-1 font-mono text-[11px] text-stone-400 hover:bg-white/5"
      >
        <span className="icon-[tabler--plus] block text-[12px] text-amber-500" />
        Enroll node
      </button>
      <Dialog open={open} onClose={() => setOpen(false)} className="max-w-xl" label="Enroll a node">
        <div className="w-full rounded-xl border border-amber-200/5 bg-abyss p-5">
          <div className="mb-3 flex items-center justify-between">
            <p className="font-display font-semibold text-sm">Enroll a node</p>
            <button
              type="button"
              aria-label="Close"
              onClick={() => setOpen(false)}
              className="text-stone-500 hover:text-stone-300"
            >
              <span className="icon-[tabler--x] text-sm" />
            </button>
          </div>
          <p className="mb-3 text-[12px] text-stone-500">
            Run this on the new obstor deployment to report into this controller. The token expires
            in 15 minutes.
          </p>
          <div className="flex items-start gap-2 rounded-lg border border-amber-200/5 bg-void/40 p-3 font-mono text-[11px] text-stone-300">
            <span className="min-w-0 break-all">
              {loading ? "Generating..." : (command ?? "Failed to mint token.")}
            </span>
            {command && (
              <button
                type="button"
                onClick={onCopy}
                className="shrink-0 text-amber-500 hover:text-amber-400"
              >
                <span className={copied ? "icon-[tabler--check]" : "icon-[tabler--copy]"} />
              </button>
            )}
          </div>
        </div>
      </Dialog>
    </>
  );
}
