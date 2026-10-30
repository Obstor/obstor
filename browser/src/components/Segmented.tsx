// Grouped toggle buttons
export function Segmented<T extends string>({
  label,
  options,
  value,
  onChange,
}: {
  label: string;
  options: { id: T; label: string; count?: number }[];
  value: T;
  onChange: (id: T) => void;
}) {
  return (
    <fieldset
      aria-label={label}
      className="bg-surface inline-flex h-9 min-w-0 shrink-0 items-center gap-0.5 rounded-lg border border-stone-800 p-0.5"
    >
      {options.map((o) => {
        const active = o.id === value;
        return (
          <button
            key={o.id}
            type="button"
            aria-pressed={active}
            onClick={() => onChange(o.id)}
            className={`font-body inline-flex h-full items-center gap-1.5 rounded-md px-3 text-[13px] font-medium transition-colors ${
              active ? "bg-surface-overlay text-stone-100" : "text-stone-400 hover:text-stone-200"
            }`}
          >
            {o.label}
            {o.count !== undefined && (
              <span
                className={`font-mono text-[12px] ${o.count === 0 ? "text-stone-600" : "text-stone-500"}`}
              >
                {o.count}
              </span>
            )}
          </button>
        );
      })}
    </fieldset>
  );
}
