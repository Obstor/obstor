"use client";

import { usePathname } from "next/navigation";
import { type ReactNode, useState } from "react";
import { Dialog } from "./Dialog";
import { Sidebar, type SidebarProps } from "./Sidebar";
import { Topbar } from "./Topbar";

export interface ShellUser {
  accessKey: string;
  account: string;
}

export function Shell({ children, user, ...sidebar }: SidebarProps & { children: ReactNode }) {
  const pathname = usePathname();
  const [drawer, setDrawer] = useState(false);
  const [path, setPath] = useState(pathname);
  // Close drawer when navigation lands
  if (path !== pathname) {
    setPath(pathname);
    setDrawer(false);
  }

  return (
    <div className="flex h-dvh overflow-hidden">
      <aside className="bg-abyss hidden w-[264px] shrink-0 border-r border-stone-800/70 lg:flex">
        <Sidebar {...sidebar} user={user} />
      </aside>
      <Dialog
        open={drawer}
        onClose={() => setDrawer(false)}
        side="left"
        label="Navigation"
        className="w-[280px] max-w-[85vw]"
      >
        {drawer && (
          <div className="bg-abyss flex h-full border-r border-stone-800/70">
            <Sidebar {...sidebar} user={user} onClose={() => setDrawer(false)} />
          </div>
        )}
      </Dialog>
      <div className="flex min-w-0 flex-1 flex-col">
        <Topbar
          onMenu={() => setDrawer(true)}
          buckets={sidebar.buckets.map((b) => b.name)}
          nav={sidebar.nav}
          user={user}
        />
        {/* Keep positioned sr-only spans inside scroller */}
        <main className="relative flex-1 overflow-y-auto">
          <div
            key={pathname}
            className="motion-safe:animate-page-in mx-auto w-full max-w-[1440px] px-4 py-6 sm:px-8"
          >
            {children}
          </div>
        </main>
      </div>
    </div>
  );
}
