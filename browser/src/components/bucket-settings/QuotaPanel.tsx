import type { QuotaUnit } from "@/lib/bucket-config";
import { SelectMenu } from "../SelectMenu";
import { Collapse, OptionGrid, type PanelProps, SectionLabel, SettingRow, Toggle } from "./shared";

const QUOTA_TYPES = [
  { id: "hard", label: "Hard limit", desc: "Reject writes that would pass the limit" },
  { id: "fifo", label: "FIFO", desc: "Delete the oldest objects until the bucket fits" },
] as const;

const QUOTA_UNITS: readonly QuotaUnit[] = ["GB", "TB", "PB"];

export function QuotaPanel({ config, onChange, reason }: PanelProps) {
  return (
    <div className="space-y-4">
      <SettingRow
        label="Enable quota"
        description="Limit how much data this bucket can hold."
        note={reason}
      >
        <Toggle
          label="Enable quota"
          checked={config.quotaEnabled}
          onChange={(v) => onChange({ quotaEnabled: v })}
          disabled={reason !== null}
        />
      </SettingRow>

      <Collapse open={config.quotaEnabled}>
        <fieldset disabled={reason !== null} className="min-w-0 space-y-4 disabled:opacity-50">
          <div>
            <SectionLabel>Quota type</SectionLabel>
            <OptionGrid
              label="Quota type"
              options={QUOTA_TYPES}
              value={config.quotaType}
              onChange={(id) => onChange({ quotaType: id })}
              columns={2}
            />
          </div>

          <div>
            <SectionLabel>Quota size</SectionLabel>
            <div className="flex gap-2">
              <input
                type="number"
                min="0"
                step="any"
                inputMode="decimal"
                value={config.quotaSize}
                onChange={(e) => onChange({ quotaSize: e.target.value })}
                placeholder="100"
                aria-label="Quota size"
                className="bg-surface font-body h-9 w-full min-w-0 flex-1 rounded-lg border border-stone-800 px-3 text-base text-stone-100 transition-colors outline-none placeholder:text-stone-500 focus:border-amber-500 disabled:opacity-50 sm:text-sm"
              />
              <SelectMenu
                value={config.quotaUnit}
                onChange={(quotaUnit) => onChange({ quotaUnit })}
                label="Quota unit"
                options={QUOTA_UNITS.map((u) => ({ value: u, label: u }))}
                className="w-24"
              />
            </div>
            <p className="mt-1.5 text-[13px] text-stone-400">
              1 GB here is 1024 MB, the unit the console shows usage in. Up to 8 PB.
            </p>
          </div>

          <p className="text-[13px] leading-relaxed text-stone-400">
            {config.quotaType === "hard"
              ? "Usage comes from the periodic scan, so a bucket can pass its limit until the next scan finishes."
              : "Each usage scan deletes the oldest objects until the bucket fits. Locked objects are kept."}
          </p>
        </fieldset>
      </Collapse>
    </div>
  );
}
