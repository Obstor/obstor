"use client";
import { useEffect, useRef } from "react";

export function Dialog({
  open,
  onClose,
  className = "",
  label,
  side = "center",
  children,
}: {
  open: boolean;
  onClose: () => void;
  className?: string; // panel sizing
  label?: string; // aria label
  side?: "center" | "left"; // Full-height drawer
  children: React.ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const opener = useRef<HTMLElement | null>(null);
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) {
      opener.current = document.activeElement as HTMLElement | null;
      d.showModal(); // throws if already open
    } else if (!open && d.open) d.close();
  }, [open]);
  useEffect(
    () => () => {
      const el = opener.current;
      const active = document.activeElement;
      if (el?.isConnected && (!active || active === document.body)) el.focus();
    },
    [],
  );

  return (
    // biome-ignore lint/a11y/useKeyWithClickEvents: backdrop click is mouse-only, ESC is native diaglog tag
    <dialog
      ref={ref}
      aria-label={label}
      onClose={onClose} // native ESC close
      onClick={(e) => {
        if (e.target === ref.current) onClose(); // backdrop click
      }}
      className={`fixed inset-0 m-0 flex h-dvh max-h-dvh w-full max-w-full border-0 bg-transparent text-stone-100 not-open:hidden backdrop:bg-[rgb(8_10_12/0.8)] ${side === "left" ? "items-stretch justify-start p-0" : "items-center justify-center p-4"}`}
    >
      <div
        className={`relative transition-[opacity,transform] duration-200 ease-[cubic-bezier(0.16,1,0.3,1)] motion-reduce:transition-none starting:opacity-0 ${side === "left" ? "h-full starting:transform-[translateX(-16px)]" : "w-full starting:transform-[scale(0.96)_translateY(8px)]"} ${className}`}
      >
        {children}
      </div>
    </dialog>
  );
}
