import type { ReactNode } from "react";

// Search and optional filter
export function Toolbar({ children }: { children: ReactNode }) {
  return <div className="flex flex-wrap items-center gap-2 p-4">{children}</div>;
}

export function SearchField({
  value,
  onChange,
  placeholder,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder: string;
}) {
  return (
    <div className="relative w-full sm:w-80">
      <span
        aria-hidden="true"
        className="icon-[tabler--search] pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-stone-400"
      />
      <input
        type="search"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        aria-label={placeholder}
        className="bg-surface font-body h-9 w-full rounded-lg border border-stone-800 px-3 pl-9 text-base text-stone-100 transition-colors outline-none placeholder:text-stone-500 focus:border-amber-500 disabled:opacity-50 sm:text-sm"
      />
    </div>
  );
}
