import type { ReactNode } from "react";

// Title row with text left, buttons right
export function PageHeader({
  title,
  subtitle,
  actions,
}: {
  title: ReactNode;
  subtitle?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <header className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
      <div className="min-w-0">
        <h1 className="font-display text-[34px] leading-tight font-bold text-stone-100">{title}</h1>
        {subtitle && <p className="mt-1 text-[15px] text-stone-400">{subtitle}</p>}
      </div>
      {actions && (
        <div className="flex w-full flex-wrap items-center gap-2 sm:w-auto">{actions}</div>
      )}
    </header>
  );
}
