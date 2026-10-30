import { type PanelProps, SettingRow, Toggle } from "./shared";

// Locking enables versioning
export function LockingPanel({ config, onChange, reason }: PanelProps) {
  return (
    <SettingRow
      label="Object locking"
      description="Lets objects be locked against deletion. Versioning stays on."
      note={reason}
    >
      <Toggle
        label="Object locking"
        checked={config.objectLock}
        onChange={(v) =>
          onChange(v ? { objectLock: true, versioning: true } : { objectLock: false })
        }
        disabled={reason !== null}
      />
    </SettingRow>
  );
}
