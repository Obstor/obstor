"use client";

import Link from "next/link";
import { type AttentionItem, bellLabel, type Severity, unseenItems } from "@/lib/attention";
import { safeDisplayName } from "@/lib/safe-name";
import { useAttention } from "./AttentionProvider";
import { Menu, useCloseMenu } from "./Menu";

export const BELL_MENU_ID = "attention-bell";

const SEVERITY_ICON: Record<Severity, string> = {
  critical: "icon-[tabler--alert-octagon] text-red-400",
  warning: "icon-[tabler--alert-triangle] text-amber-400",
  info: "icon-[tabler--info-circle] text-stone-400",
};

export function Bell() {
  const { items, seen, markSeen } = useAttention();
  const unseen = seen === null ? [] : unseenItems(items, seen);
  const unseenIds = new Set(unseen.map((item) => item.id));

  return (
    <Menu
      id={BELL_MENU_ID}
      label={bellLabel(unseen.length)}
      align="end"
      triggerClassName="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-lg transition-colors disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11 text-stone-400 hover:bg-surface hover:text-stone-100 relative"
      className="w-80 max-w-[calc(100vw-16px)]"
      trigger={
        <>
          <span className="icon-[tabler--bell]" />
          {unseen.length > 0 && (
            <span
              aria-hidden="true"
              className="absolute top-2 right-2 h-2 w-2 rounded-full bg-amber-500"
            />
          )}
        </>
      }
    >
      <div className="flex items-center justify-between gap-3 px-2.5 py-2">
        <p className="font-body text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase">
          Notifications
        </p>
        <button
          type="button"
          role="menuitem"
          onClick={() => markSeen(items.map((item) => item.id))}
          disabled={seen === null || unseen.length === 0}
          className="hover:bg-surface-overlay focus-visible:bg-surface-overlay ml-auto inline-flex shrink-0 items-center justify-center rounded-md px-2 py-1 text-xs text-stone-200 outline-none disabled:text-stone-500"
        >
          Mark all read
        </button>
      </div>
      {items.length === 0 ? (
        <p className="px-2.5 py-6 text-center text-sm text-stone-400">Nothing needs attention.</p>
      ) : (
        <div className="max-h-[min(60vh,420px)] overflow-y-auto border-t border-stone-800/70 pt-1">
          {items.map((item) => (
            <BellRow
              key={item.id}
              item={item}
              unseen={unseenIds.has(item.id)}
              onSeen={() => markSeen([item.id])}
            />
          ))}
        </div>
      )}
    </Menu>
  );
}

function BellRow({
  item,
  unseen,
  onSeen,
}: {
  item: AttentionItem;
  unseen: boolean;
  onSeen: () => void;
}) {
  const close = useCloseMenu();
  const select = () => {
    close();
    onSeen();
  };
  const body = (
    <>
      <span
        className={`${SEVERITY_ICON[item.severity] ?? SEVERITY_ICON.info} mt-0.5 shrink-0 text-base`}
      />
      <span className="min-w-0 flex-1">
        <span
          className={`block text-sm wrap-anywhere text-stone-100 ${unseen ? "font-medium" : ""}`}
        >
          <bdi>{safeDisplayName(item.title)}</bdi>
        </span>
        <span className="mt-0.5 block text-xs wrap-anywhere text-stone-400">
          <bdi>{safeDisplayName(item.detail)}</bdi>
        </span>
      </span>
    </>
  );

  if (item.href) {
    return (
      <Link
        href={item.href}
        role="menuitem"
        prefetch={false}
        onClick={select}
        className="hover:bg-surface-overlay focus-visible:bg-surface-overlay flex w-full items-start gap-3 rounded-md px-2.5 py-2 text-left outline-none"
      >
        {body}
      </Link>
    );
  }
  return (
    <button
      type="button"
      role="menuitem"
      onClick={select}
      className="hover:bg-surface-overlay focus-visible:bg-surface-overlay flex w-full items-start gap-3 rounded-md px-2.5 py-2 text-left outline-none"
    >
      {body}
    </button>
  );
}
