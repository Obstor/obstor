"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import type { TimeRange } from "@/lib/chart";
import { Segmented } from "./Segmented";

const OPTIONS: { id: TimeRange["value"]; label: string }[] = [
  { id: "24h", label: "Day" },
  { id: "1w", label: "Week" },
  { id: "1m", label: "Month" },
];

export function RangeTabs({ value, label }: { value: TimeRange["value"]; label: string }) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  return (
    <div className="flex shrink-0 max-[360px]:[&_button]:px-0.5 max-sm:[&_button]:text-xs min-[360px]:max-sm:[&_button]:px-1">
      <Segmented
        label={label}
        options={OPTIONS}
        value={value}
        onChange={(next) => {
          if (next === value) return;
          const params = new URLSearchParams(searchParams.toString());
          params.set("range", next);
          router.replace(`${pathname}?${params}`, { scroll: false });
        }}
      />
    </div>
  );
}
