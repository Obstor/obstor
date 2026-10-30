import { type PanelProps, SettingRow, Toggle } from "./shared";

// Auto encryption shows on and read-only
export function EncryptionPanel({
  config,
  onChange,
  reason,
  autoEncryption,
}: PanelProps & { autoEncryption: boolean }) {
  return (
    <SettingRow
      label="Server-side encryption (SSE-S3)"
      description="Encrypt new objects with a key from the server's KMS. Stored objects are not changed."
      note={reason}
    >
      <Toggle
        label="Server-side encryption"
        checked={autoEncryption || config.encryption}
        onChange={(v) => onChange({ encryption: v })}
        disabled={reason !== null}
      />
    </SettingRow>
  );
}
