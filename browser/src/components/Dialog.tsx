"use client";
import { useEffect, useRef } from "react";

export function Dialog({
  open,
  onClose,
  className = "",
  label,
  children,
}: {
  open: boolean;
  onClose: () => void;
  className?: string; // panel sizing
  label?: string; // aria label
  children: React.ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open)
      d.showModal(); // throws if already open
    else if (!open && d.open) d.close();
  }, [open]);

  return (
    // biome-ignore lint/a11y/useKeyWithClickEvents: backdrop click is mouse-only, ESC is native diaglog tag
    <dialog
      ref={ref}
      aria-label={label}
      onClose={onClose} // native ESC close
      onClick={(e) => {
        if (e.target === ref.current) onClose(); // backdrop click
      }}
      className="dialog-reset"
    >
      <div className={`relative w-full ${className}`}>{children}</div>
    </dialog>
  );
}
