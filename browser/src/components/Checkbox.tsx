import type { ComponentProps } from "react";

export function Checkbox({ className = "", ...props }: Omit<ComponentProps<"input">, "type">) {
  return (
    <span className="relative inline-flex h-4 w-4 shrink-0 align-middle">
      <input
        {...props}
        type="checkbox"
        className={`peer h-4 w-4 shrink-0 cursor-pointer appearance-none rounded border-[1.5px] border-stone-600 bg-transparent transition-colors duration-150 checked:border-amber-500 checked:bg-amber-500 indeterminate:border-amber-500 indeterminate:bg-amber-500 not-checked:not-indeterminate:hover:border-stone-400 ${className}`}
      />
      <svg
        aria-hidden="true"
        viewBox="0 0 16 16"
        className="pointer-events-none absolute inset-0 hidden h-4 w-4 fill-none stroke-black stroke-[1.5] peer-checked:block peer-indeterminate:hidden"
      >
        <path d="m4 8 3 3 5-7" />
      </svg>
      <svg
        aria-hidden="true"
        viewBox="0 0 16 16"
        className="pointer-events-none absolute inset-0 hidden h-4 w-4 fill-none stroke-black stroke-[1.5] peer-indeterminate:block"
      >
        <path d="M4 8h8" />
      </svg>
    </span>
  );
}
