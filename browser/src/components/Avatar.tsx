"use client";

import type { ReactNode } from "react";
import { accountLabel } from "@/lib/account";
import { logoutAction } from "@/lib/actions";
import { seenKey } from "@/lib/attention";
import { safeDisplayName } from "@/lib/safe-name";
import { Menu, MenuItem, MenuLink } from "./Menu";
import type { ShellUser } from "./Shell";

// Clear seen notifications
export async function signOut(accessKey: string): Promise<void> {
  try {
    localStorage.removeItem(seenKey(accessKey));
  } catch {
    // Nothing to clear
  }
  await logoutAction();
}

async function copyText(value: string) {
  try {
    await navigator.clipboard.writeText(value);
  } catch {
    // Clipboard denied, key is readable
  }
}

export function Avatar({ user }: { user: ShellUser }) {
  return (
    <AccountMenu
      user={user}
      trigger={<UserAvatar />}
      triggerClassName="flex h-9 w-9 shrink-0 items-center justify-center overflow-hidden rounded-full"
    />
  );
}

export function UserAvatar({ className = "h-9 w-9" }: { className?: string }) {
  return (
    <img
      src="https://dl.cloudment.net/images/users/avatar.svg"
      alt=""
      width={36}
      height={36}
      className={`shrink-0 rounded-full object-cover ${className}`}
    />
  );
}

export function AccountMenu({
  user,
  trigger,
  triggerClassName,
  onNavigate,
}: {
  user: ShellUser;
  trigger: ReactNode;
  triggerClassName: string;
  onNavigate?: () => void;
}) {
  const type = accountLabel(user.account);
  return (
    <Menu
      label="Account menu"
      align="end"
      triggerClassName={triggerClassName}
      className="w-64 max-w-[calc(100vw-16px)]"
      trigger={trigger}
    >
      <div role="none" className="mb-1 border-b border-stone-800/70 px-2.5 pt-1.5 pb-2.5">
        <p
          className="truncate font-mono text-sm text-stone-100"
          title={safeDisplayName(user.accessKey)}
        >
          <bdi>{user.accessKey ? safeDisplayName(user.accessKey) : "Signed in"}</bdi>
        </p>
        {type && <p className="mt-0.5 text-xs text-stone-400">{type}</p>}
      </div>
      <MenuLink href="/settings#account" icon="icon-[tabler--user-cog]" onSelect={onNavigate}>
        Account settings
      </MenuLink>
      {user.accessKey && (
        <MenuItem icon="icon-[tabler--copy]" onSelect={() => copyText(user.accessKey)}>
          Copy access key
        </MenuItem>
      )}
      <MenuItem icon="icon-[tabler--logout]" onSelect={() => signOut(user.accessKey)}>
        Sign out
      </MenuItem>
    </Menu>
  );
}
