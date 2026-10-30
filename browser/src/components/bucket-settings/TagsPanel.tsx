import { cleanTags, MAX_USER_TAGS, tagError } from "@/lib/bucket-config";
import { Note, type PanelProps } from "./shared";

export function TagsPanel({ config, onChange, reason }: PanelProps) {
  const tags = cleanTags(config.tags);
  const problem = tagError(tags);
  const addTag = () => onChange({ tags: [...config.tags, { key: "", value: "" }] });
  const removeTag = (i: number) => onChange({ tags: config.tags.filter((_, idx) => idx !== i) });
  const updateTag = (i: number, field: "key" | "value", val: string) =>
    onChange({ tags: config.tags.map((t, idx) => (idx === i ? { ...t, [field]: val } : t)) });

  return (
    <fieldset disabled={reason !== null} className="min-w-0 space-y-4">
      <div className="flex items-center justify-between gap-3">
        <p className="text-[13px] text-stone-400">
          {tags.length} of {MAX_USER_TAGS}
        </p>
        <button
          type="button"
          onClick={addTag}
          className="bg-surface font-body hover:bg-surface-overlay inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-3.5 text-sm font-medium whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
        >
          <span className="icon-[tabler--plus] text-base" />
          Add tag
        </button>
      </div>
      {reason && <Note text={reason} />}

      {config.tags.length === 0 ? (
        <div className="bg-abyss flex flex-col items-center justify-center rounded-lg border border-stone-800/70 py-8">
          <span className="icon-[tabler--tag] mb-2 text-xl text-stone-500" />
          <p className="text-[13px] text-stone-400">No tags configured</p>
        </div>
      ) : (
        <div className="space-y-2">
          {config.tags.map((tag, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: rows where a input keystroke remounts from the tag text
            <div key={i} className="flex items-center gap-2">
              <input
                type="text"
                value={tag.key}
                onChange={(e) => updateTag(i, "key", e.target.value)}
                placeholder="Key"
                aria-label={`Tag ${i + 1} key`}
                className="bg-surface h-9 w-full min-w-0 flex-1 rounded-lg border border-stone-800 px-3 font-mono text-base text-stone-100 transition-colors outline-none placeholder:text-stone-500 focus:border-amber-500 disabled:opacity-50 sm:text-sm"
              />
              <input
                type="text"
                value={tag.value}
                onChange={(e) => updateTag(i, "value", e.target.value)}
                placeholder="Value"
                aria-label={`Tag ${i + 1} value`}
                className="bg-surface h-9 w-full min-w-0 flex-1 rounded-lg border border-stone-800 px-3 font-mono text-base text-stone-100 transition-colors outline-none placeholder:text-stone-500 focus:border-amber-500 disabled:opacity-50 sm:text-sm"
              />
              <button
                type="button"
                onClick={() => removeTag(i)}
                aria-label={`Remove tag ${i + 1}`}
                className="hover:bg-surface inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-lg text-stone-400 transition-colors hover:text-red-400 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11"
              >
                <span className="icon-[tabler--x] text-base" />
              </button>
            </div>
          ))}
        </div>
      )}

      {problem && <p className="text-[13px] text-amber-400">{problem}</p>}
      <p className="text-[13px] leading-relaxed text-stone-400">
        Letters, digits, spaces and + - . _ : / @ = are allowed. Keys starting with __obstor_ are
        reserved.
      </p>
    </fieldset>
  );
}
