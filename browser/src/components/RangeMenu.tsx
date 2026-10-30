import { TIME_RANGES, type TimeRange } from "@/lib/chart";
import { Menu, MenuLink } from "./Menu";

// Range is the only param on pages that use it
export function RangeMenu({
  selected,
  path,
  className = "inline-flex h-9 shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-lg px-3.5 font-body font-medium text-sm transition-colors disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 border border-stone-800 bg-surface text-stone-200 hover:border-stone-700 hover:bg-surface-overlay",
}: {
  selected: TimeRange;
  path: string;
  className?: string;
}) {
  return (
    <Menu
      triggerClassName={className}
      trigger={
        <>
          <span className="icon-[tabler--calendar] text-lg text-stone-400" />
          {selected.label}
          <span className="icon-[tabler--chevron-down] text-stone-400" />
        </>
      }
    >
      {TIME_RANGES.map((r) => (
        <MenuLink
          key={r.value}
          href={`${path}?range=${r.value}`}
          replace
          active={r.value === selected.value}
        >
          {r.label}
        </MenuLink>
      ))}
    </Menu>
  );
}
