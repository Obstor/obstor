"use client";

import type { PublicAccess } from "@/lib/bucket-config";
import type { AccessLabel } from "@/lib/rpc";
import { Note, OptionGrid, type PanelProps } from "./shared";

// Same access labels as bucket list
const OPTIONS: readonly { id: PublicAccess; label: AccessLabel; desc: string }[] = [
  { id: "private", label: "Private", desc: "No public access" },
  { id: "public-read", label: "Public read", desc: "Anyone can read objects" },
  { id: "public-read-write", label: "Public read/write", desc: "Anyone can read and write" },
];

export function PublicAccessPanel({ config, onChange, reason }: PanelProps) {
  return (
    <div>
      <fieldset disabled={reason !== null} className="min-w-0 disabled:opacity-50">
        <OptionGrid
          label="Public access"
          options={OPTIONS}
          value={config.publicAccess}
          onChange={(id) => onChange({ publicAccess: id })}
          columns={3}
        />
      </fieldset>
      {reason && <Note text={reason} />}
    </div>
  );
}
