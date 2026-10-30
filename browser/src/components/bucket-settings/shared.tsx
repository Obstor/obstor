"use client";

import { type ReactNode, useState } from "react";
import { Checkbox } from "@/components/Checkbox";
import type { BucketConfig } from "@/lib/bucket-config";
import { safeDisplayName } from "@/lib/safe-name";
import { Dialog } from "../Dialog";

// Draft, change callback and disabled reason for config panels
export type PanelProps = {
  config: BucketConfig;
  onChange: (patch: Partial<BucketConfig>) => void;
  reason: string | null;
};

export function Toggle({
  checked,
  onChange,
  disabled,
  label,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  disabled?: boolean;
  label: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={`relative inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full transition-colors duration-200 disabled:cursor-not-allowed disabled:opacity-40 motion-reduce:transition-none ${
        checked ? "bg-amber-500" : "bg-stone-700"
      }`}
    >
      <span
        className={`pointer-events-none inline-block h-4 w-4 rounded-full bg-stone-100 shadow-sm transition-transform duration-200 ease-[cubic-bezier(0.16,1,0.3,1)] motion-reduce:transition-none ${
          checked ? "translate-x-6" : "translate-x-1"
        }`}
      />
    </button>
  );
}

export function Note({ text }: { text: string }) {
  return (
    <p className="mt-1.5 flex items-start gap-1.5 text-[13px] leading-relaxed text-stone-400">
      <span className="icon-[tabler--info-circle] mt-0.5 shrink-0 text-base" />
      <span>{text}</span>
    </p>
  );
}

export function SettingRow({
  label,
  description,
  note,
  children,
}: {
  label: string;
  description?: string;
  note?: string | null;
  children: ReactNode;
}) {
  return (
    <div className="bg-abyss flex items-center justify-between gap-4 rounded-lg border border-stone-800/70 px-4 py-3">
      <div className="min-w-0">
        <p className="text-[14px] font-medium text-stone-100">{label}</p>
        {description && (
          <p className="mt-0.5 text-[13px] leading-relaxed text-stone-400">{description}</p>
        )}
        {note && <Note text={note} />}
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  );
}

export function SectionLabel({ children }: { children: ReactNode }) {
  return (
    <p className="font-body mb-2 text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase">
      {children}
    </p>
  );
}

// Grid row collapse
export function Collapse({
  open,
  className = "",
  children,
}: {
  open: boolean;
  className?: string;
  children: ReactNode;
}) {
  return (
    <div
      className={`grid transition-[grid-template-rows] duration-200 ease-[cubic-bezier(0.16,1,0.3,1)] motion-reduce:transition-none ${open ? "grid-rows-[1fr]" : "grid-rows-[0fr]"}`}
      inert={!open}
    >
      <div
        className={`overflow-hidden transition-opacity duration-200 motion-reduce:transition-none ${open ? "opacity-100" : "opacity-0"} ${className}`}
      >
        {children}
      </div>
    </div>
  );
}

export function OptionGrid<T extends string>({
  label,
  options,
  value,
  onChange,
  columns,
}: {
  label: string;
  options: readonly { id: T; label: string; desc: string }[];
  value: T;
  onChange: (id: T) => void;
  columns: 2 | 3;
}) {
  return (
    <fieldset
      aria-label={label}
      className={`grid min-w-0 grid-cols-1 gap-2 ${columns === 2 ? "sm:grid-cols-2" : "sm:grid-cols-3"}`}
    >
      {options.map((o) => {
        const on = o.id === value;
        return (
          <button
            key={o.id}
            type="button"
            aria-pressed={on}
            onClick={() => onChange(o.id)}
            className={`bg-abyss rounded-lg border px-3 py-2.5 text-left transition-colors disabled:cursor-not-allowed ${
              on ? "border-amber-500/50" : "border-stone-800/70 hover:border-stone-700"
            }`}
          >
            <span
              className={`block text-[14px] font-medium ${on ? "text-amber-400" : "text-stone-100"}`}
            >
              {o.label}
            </span>
            <span className="mt-0.5 block text-[13px] text-stone-400">{o.desc}</span>
          </button>
        );
      })}
    </fieldset>
  );
}

// Sticky save bar while dirty or save error
export function SaveBar({
  dirty,
  saving,
  error,
  onDiscard,
  onSave,
}: {
  dirty: boolean;
  saving: boolean;
  error: string;
  onDiscard: () => void;
  onSave: () => void;
}) {
  if (!dirty && !error) return null;
  return (
    <div className="pointer-events-none sticky bottom-0 z-10 pt-4 pb-4">
      <div className="bg-abyss pointer-events-auto flex flex-wrap items-center gap-3 rounded-xl border border-stone-800/70 px-4 py-3 shadow-lg shadow-black/40">
        <div className="min-w-0 flex-1 space-y-1">
          {dirty && <p className="text-[14px] font-medium text-stone-100">Unsaved changes</p>}
          {error && (
            <p
              role="alert"
              className="flex items-start gap-2 text-[13px] leading-relaxed text-red-400"
            >
              <span className="icon-[tabler--alert-circle] mt-0.5 shrink-0 text-base" />
              <span className="break-words">{error}</span>
            </p>
          )}
        </div>
        <div className="ml-auto flex shrink-0 items-center gap-2">
          <button
            type="button"
            onClick={onDiscard}
            disabled={saving || !dirty}
            className="bg-surface font-body hover:bg-surface-overlay inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-3.5 text-sm font-medium whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
          >
            Discard
          </button>
          <button
            type="button"
            onClick={onSave}
            disabled={saving || !dirty}
            className="font-body inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg bg-amber-500 px-3.5 text-sm font-semibold whitespace-nowrap text-black transition-colors hover:bg-amber-400 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
          >
            {saving ? "Saving..." : "Save"}
          </button>
        </div>
      </div>
    </div>
  );
}

// Existing policies or users to bucket access
export function AttachList({
  title,
  items,
  onCancel,
  onConfirm,
}: {
  title: string;
  items: { id: string; label: string; meta: string }[];
  onCancel: () => void;
  onConfirm: (ids: string[]) => void;
}) {
  const [picked, setPicked] = useState<string[]>([]);
  const toggle = (id: string) =>
    setPicked((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id]));

  return (
    <Dialog open onClose={onCancel} className="max-w-md" label={title}>
      <div className="bg-abyss w-full rounded-xl p-5">
        <h2 className="font-display mb-3 text-lg font-semibold">{title}</h2>
        <div className="mb-4 max-h-72 space-y-0.5 overflow-y-auto">
          {items.map((it) => (
            <label
              key={it.id}
              className="hover:bg-surface-overlay flex cursor-pointer items-center gap-3 rounded-md px-2 py-2 transition-colors"
            >
              <Checkbox checked={picked.includes(it.id)} onChange={() => toggle(it.id)} />
              <bdi className="min-w-0 flex-1 truncate font-mono text-[13px] text-stone-200">
                {safeDisplayName(it.label)}
              </bdi>
              <span className="shrink-0 text-[13px] text-stone-400">{it.meta}</span>
            </label>
          ))}
        </div>
        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={onCancel}
            className="bg-surface font-body hover:bg-surface-overlay inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-3.5 text-sm font-medium whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
          >
            Cancel
          </button>
          <button
            type="button"
            disabled={picked.length === 0}
            onClick={() => onConfirm(picked)}
            className="font-body inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg bg-amber-500 px-3.5 text-sm font-semibold whitespace-nowrap text-black transition-colors hover:bg-amber-400 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
          >
            Attach
          </button>
        </div>
      </div>
    </Dialog>
  );
}
