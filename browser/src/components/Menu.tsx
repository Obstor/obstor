"use client";

import Link from "next/link";
import {
  createContext,
  type KeyboardEvent,
  type ReactNode,
  type ToggleEvent,
  useCallback,
  useContext,
  useEffect,
  useId,
  useRef,
} from "react";
import { placeMenu } from "@/lib/menu";

const CloseMenu = createContext<() => void>(() => {});

// Close parent menu from custom rows
export function useCloseMenu(): () => void {
  return useContext(CloseMenu);
}
const FOCUSABLE = "a, button:not(:disabled), input";

export function Menu({
  id: panelId,
  label,
  trigger,
  triggerClassName,
  align = "end",
  className = "",
  children,
}: {
  id?: string; // ID for opening via code
  label?: string; // Icon trigger names
  trigger: ReactNode;
  triggerClassName: string;
  align?: "start" | "end";
  className?: string;
  children: ReactNode;
}) {
  const autoId = useId();
  const id = panelId ?? autoId;
  const button = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const close = useCallback(() => panel.current?.hidePopover(), []);
  // Allow scrolling inside panel
  const onScroll = useCallback(
    (e: Event) => {
      if (!panel.current?.contains(e.target as Node)) close();
    },
    [close],
  );
  useEffect(
    () => () => {
      window.removeEventListener("scroll", onScroll, true);
      window.removeEventListener("resize", close);
    },
    [onScroll, close],
  );

  // Position top-layer popover
  const place = () => {
    const b = button.current?.getBoundingClientRect();
    const p = panel.current;
    if (!b || !p) return;
    const { left, top } = placeMenu(
      b,
      { width: p.offsetWidth, height: p.offsetHeight },
      { width: window.innerWidth, height: window.innerHeight },
      align,
    );
    p.style.left = `${left}px`;
    p.style.top = `${top}px`;
    p.style.visibility = "";
  };

  const onToggle = (e: ToggleEvent<HTMLDivElement>) => {
    if (e.newState !== "open" || !panel.current) {
      window.removeEventListener("scroll", onScroll, true);
      window.removeEventListener("resize", close);
      return;
    }
    place();
    visible()[0]?.focus();
    window.addEventListener("scroll", onScroll, true);
    window.addEventListener("resize", close);
  };

  const visible = () =>
    [...(panel.current?.querySelectorAll<HTMLElement>(FOCUSABLE) ?? [])].filter((el) =>
      el.checkVisibility(),
    );

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    e.preventDefault();
    const items = visible();
    const i = items.indexOf(document.activeElement as HTMLElement);
    items[e.key === "ArrowDown" ? Math.min(i + 1, items.length - 1) : Math.max(i - 1, 0)]?.focus();
  };

  return (
    <>
      <button
        ref={button}
        type="button"
        popoverTarget={id}
        aria-label={label}
        title={label}
        className={triggerClassName}
      >
        {trigger}
      </button>
      <div
        ref={panel}
        id={id}
        popover="auto"
        role="menu"
        aria-label={label}
        // Hide until placed
        onBeforeToggle={(e) => {
          if (e.newState === "open" && panel.current) panel.current.style.visibility = "hidden";
        }}
        onToggle={onToggle}
        onKeyDown={onKeyDown}
        className={`bg-abyss fixed inset-auto m-0 min-w-44 rounded-lg border border-stone-800/70 p-1 text-stone-200 shadow-xl shadow-black/50 ${className}`}
      >
        <CloseMenu.Provider value={close}>{children}</CloseMenu.Provider>
      </div>
    </>
  );
}

export function MenuItem({
  icon,
  danger = false,
  className = "",
  onSelect,
  children,
}: {
  icon?: string;
  danger?: boolean;
  className?: string;
  onSelect: () => void;
  children: ReactNode;
}) {
  const close = useContext(CloseMenu);
  return (
    <button
      type="button"
      role="menuitem"
      onClick={() => {
        close();
        onSelect();
      }}
      className={`focus-visible:bg-surface-overlay flex w-full items-center gap-2.5 rounded-md px-2.5 py-1.5 text-left text-sm outline-none ${danger ? "text-red-400 hover:bg-red-500/10" : "hover:bg-surface-overlay text-stone-200"} ${className}`}
    >
      {icon && <span className={`${icon} shrink-0 text-base ${danger ? "" : "text-stone-400"}`} />}
      {children}
    </button>
  );
}

export function MenuLink({
  href,
  icon,
  active = false,
  replace = false,
  onSelect,
  children,
}: {
  href: string;
  icon?: string;
  active?: boolean;
  replace?: boolean; // Replace URL without scrolling
  onSelect?: () => void;
  children: ReactNode;
}) {
  const close = useContext(CloseMenu);
  return (
    <Link
      href={href}
      role="menuitem"
      replace={replace}
      scroll={!replace}
      prefetch={false}
      onClick={() => {
        close();
        onSelect?.();
      }}
      aria-current={active ? "true" : undefined}
      className={`hover:bg-surface-overlay focus-visible:bg-surface-overlay flex w-full items-center gap-2.5 rounded-md px-2.5 py-1.5 text-left text-sm outline-none ${active ? "text-amber-400" : "text-stone-200"}`}
    >
      {icon && <span className={`${icon} shrink-0 text-base text-stone-400`} />}
      <span className="min-w-0 flex-1 truncate">{children}</span>
      {active && <span className="icon-[tabler--check] shrink-0 text-base" />}
    </Link>
  );
}
