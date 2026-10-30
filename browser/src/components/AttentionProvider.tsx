"use client";

import { usePathname } from "next/navigation";
import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { getAttentionAction } from "@/lib/actions";
import {
  type AttentionItem,
  markSeen as addSeen,
  parseSeen,
  pruneSeen,
  seenKey,
} from "@/lib/attention";

const REFRESH_MS = 60_000;

interface Attention {
  items: AttentionItem[];
  seen: string[] | null;
  markSeen: (ids: string[]) => void;
}

const AttentionContext = createContext<Attention>({
  items: [],
  seen: null,
  markSeen: () => {},
});

export function AttentionProvider({
  accessKey,
  initial,
  children,
}: {
  accessKey: string;
  initial: AttentionItem[];
  children: ReactNode;
}) {
  const pathname = usePathname();
  const key = seenKey(accessKey);
  const [items, setItems] = useState(initial);
  // Read local storage after mount
  const [seen, setSeen] = useState<string[] | null>(null);
  const request = useRef(0);
  const lastPath = useRef(pathname);
  // Empty list may mean a failed call, keep seen IDs
  const prunable = useRef(initial.length > 0);

  // Layout refresh brings new server items
  useEffect(() => {
    prunable.current = initial.length > 0;
    setItems(initial);
  }, [initial]);

  const refresh = useCallback(async () => {
    const id = ++request.current;
    let res: AttentionItem[] | { error: string };
    try {
      res = await getAttentionAction();
    } catch {
      return;
    }
    // Keep items on error, ignore replies from older requests
    if (id !== request.current || !Array.isArray(res)) return;
    prunable.current = true;
    setItems(res);
  }, []);

  useEffect(() => {
    if (lastPath.current === pathname) return;
    lastPath.current = pathname;
    void refresh();
  }, [pathname, refresh]);

  useEffect(() => {
    const onVisible = () => {
      if (document.visibilityState === "visible") void refresh();
    };
    const timer = window.setInterval(onVisible, REFRESH_MS);
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [refresh]);

  useEffect(() => {
    let raw: string | null = null;
    try {
      raw = localStorage.getItem(key);
    } catch {
      // Storage blocked, items show as unseen
    }
    setSeen(parseSeen(raw));
  }, [key]);

  useEffect(() => {
    if (seen === null) return;
    const kept = prunable.current ? pruneSeen(seen, items) : seen;
    if (kept.length !== seen.length) {
      setSeen(kept);
      return;
    }
    try {
      localStorage.setItem(key, JSON.stringify(seen));
    } catch {
      // Storage blocked, seen IDs last until reload
    }
  }, [items, seen, key]);

  const markSeen = useCallback((ids: string[]) => {
    setSeen((prev) => addSeen(prev ?? [], ids));
  }, []);
  const value = useMemo(() => ({ items, seen, markSeen }), [items, seen, markSeen]);

  return <AttentionContext.Provider value={value}>{children}</AttentionContext.Provider>;
}

export function useAttention(): Attention {
  return useContext(AttentionContext);
}
