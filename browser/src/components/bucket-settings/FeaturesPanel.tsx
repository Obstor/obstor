import { type PanelProps, SettingRow, Toggle } from "./shared";

// Both protocols use same IAM perms
export function FeaturesPanel({ config, onChange, reason }: PanelProps) {
  return (
    <div className="space-y-2">
      <SettingRow
        label="S3 API"
        description="Access the bucket through the S3-compatible API."
        note={reason}
      >
        <Toggle
          label="S3 API"
          checked={config.s3Enabled}
          onChange={(v) => onChange({ s3Enabled: v })}
          disabled={reason !== null}
        />
      </SettingRow>
      <SettingRow label="SFTP access" description="Allow file access over SFTP." note={reason}>
        <Toggle
          label="SFTP access"
          checked={config.sftpEnabled}
          onChange={(v) => onChange({ sftpEnabled: v })}
          disabled={reason !== null}
        />
      </SettingRow>
    </div>
  );
}
