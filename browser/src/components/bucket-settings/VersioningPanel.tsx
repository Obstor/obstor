import { type PanelProps, SettingRow, Toggle } from "./shared";

export function VersioningPanel({ config, onChange, reason }: PanelProps) {
  return (
    <SettingRow
      label="Versioning"
      description="Keep every version of each object. Turning it off later suspends it and keeps stored versions."
      note={reason}
    >
      <Toggle
        label="Versioning"
        checked={config.versioning}
        onChange={(v) => onChange({ versioning: v })}
        disabled={reason !== null}
      />
    </SettingRow>
  );
}
