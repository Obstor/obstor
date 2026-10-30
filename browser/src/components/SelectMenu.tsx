"use client";

import { Menu, useCloseMenu } from "./Menu";

export function SelectMenu<T extends string | number>({
  label,
  value,
  options,
  onChange,
  className = "",
}: {
  label: string;
  value: T;
  options: readonly { value: T; label: string }[];
  onChange: (value: T) => void;
  className?: string;
}) {
  const selected = options.find((option) => option.value === value);
  return (
    <Menu
      label={`${label}: ${selected?.label ?? ""}`}
      align="start"
      triggerClassName={`inline-flex h-9 shrink-0 items-center gap-2 whitespace-nowrap rounded-lg px-3.5 font-body font-medium text-sm transition-colors disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 border border-stone-800 bg-surface text-stone-200 hover:border-stone-700 hover:bg-surface-overlay min-w-0 max-w-full justify-between ${className}`}
      trigger={
        <>
          <span className="min-w-0 truncate">
            <bdi>{selected?.label}</bdi>
          </span>
          <span
            aria-hidden="true"
            className="icon-[tabler--chevron-down] shrink-0 text-stone-400"
          />
        </>
      }
      className="max-h-[min(60dvh,24rem)] max-w-[calc(100vw-1rem)] overflow-y-auto"
    >
      {options.map((option) => (
        <SelectOption
          key={option.value}
          selected={option.value === value}
          onSelect={() => onChange(option.value)}
        >
          {option.label}
        </SelectOption>
      ))}
    </Menu>
  );
}

function SelectOption({
  selected,
  onSelect,
  children,
}: {
  selected: boolean;
  onSelect: () => void;
  children: string;
}) {
  const close = useCloseMenu();
  return (
    <button
      type="button"
      role="menuitemradio"
      aria-checked={selected}
      onClick={() => {
        close();
        onSelect();
      }}
      className={`hover:bg-surface-overlay focus-visible:bg-surface-overlay flex w-full items-center gap-2.5 rounded-md px-2.5 py-1.5 text-left text-sm outline-none ${selected ? "text-amber-400" : "text-stone-200"}`}
    >
      <span className="min-w-0 flex-1 truncate">
        <bdi>{children}</bdi>
      </span>
      {selected && <span aria-hidden="true" className="icon-[tabler--check] shrink-0 text-base" />}
    </button>
  );
}
