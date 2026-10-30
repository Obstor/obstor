import { Fragment, type ReactNode } from "react";
import { Card } from "@/components/Card";
import { ChangeSecretForm } from "@/components/ChangeSecretForm";
import { ConsoleAddress } from "@/components/ConsoleAddress";
import { CopyButton } from "@/components/CopyButton";
import { Uptime } from "@/components/LocalTime";
import { PageHeader } from "@/components/PageHeader";
import { accountLabel } from "@/lib/account";
import { commitLabel, modeLabel, versionLabel } from "@/lib/format";
import { publicURL, serverInfo, sessionAccessKey } from "@/lib/rpc";
import { safeDisplayName } from "@/lib/safe-name";

function Row({ term, children }: { term: string; children: ReactNode }) {
  return (
    <div className="flex min-h-10 flex-wrap items-center justify-between gap-x-4 gap-y-1 border-t border-stone-800/70 py-2 first:border-t-0">
      <dt className="font-body text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase">
        {term}
      </dt>
      <dd className="flex min-w-0 items-center gap-1 font-mono text-[13px] break-all text-stone-200">
        {children}
      </dd>
    </div>
  );
}

function Copyable({ value, label }: { value: string; label: string }) {
  return (
    <>
      <span className="min-w-0 break-all">{value}</span>
      <CopyButton value={value} label={label} />
    </>
  );
}

function Metadata({ value }: { value: string }) {
  return (
    <span className="min-w-0">
      {value.split(" | ").map((part, index) => (
        <Fragment key={part}>
          {index > 0 && <span className="text-stone-500"> | </span>}
          {part}
        </Fragment>
      ))}
    </span>
  );
}

function Flag({ on }: { on: boolean }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span
        aria-hidden="true"
        className={
          on
            ? "icon-[tabler--circle-check] text-base text-green-400"
            : "icon-[tabler--circle-minus] text-base text-stone-500"
        }
      />
      {on ? "On" : "Off"}
    </span>
  );
}

// Read-only for settings, account is everyone
export default async function SettingsPage() {
  const [info, accessKey] = await Promise.all([serverInfo(), sessionAccessKey()]);
  const settings = info?.canSeeSettings ?? false;
  const account = info?.account ?? "";
  const accountType = accountLabel(account);
  const commit = commitLabel(info?.commit ?? "");
  const version = info ? [versionLabel(info.releaseTag), commit].filter(Boolean).join(" | ") : "";

  return (
    <div className="space-y-4">
      <PageHeader
        title="Settings"
        subtitle={settings ? "Server details and your account." : "Your account."}
      />

      {!info && (
        <div role="alert" className="rounded-lg border border-red-500/20 bg-red-500/5 px-4 py-3">
          <p className="flex items-center gap-2 text-sm text-red-400">
            <span aria-hidden="true" className="icon-[tabler--alert-circle] text-base" />
            Server details could not be loaded.
          </p>
        </div>
      )}

      <div className="grid gap-4 lg:grid-cols-2">
        {info && settings && (
          <>
            <Card title="Server" icon="icon-[tabler--server-2]">
              <dl>
                <Row term="Version">
                  <Metadata value={version} />
                </Row>
                <Row term="Platform">
                  <Metadata value={info.ObstorPlatform} />
                </Row>
                <Row term="Runtime">
                  <Metadata value={info.ObstorRuntime} />
                </Row>
                <Row term="Mode">{modeLabel(info.mode) || info.mode}</Row>
                <Row term="Region">{info.region || "Not set"}</Row>
                <Row term="Uptime">
                  <Uptime iso={info.startedAt} />
                </Row>
              </dl>
            </Card>

            <Card title="Endpoints" icon="icon-[tabler--plug-connected]">
              <dl>
                <Row term="S3">
                  <Copyable value={publicURL()} label="Copy S3 address" />
                </Row>
                <Row term="Console">
                  <ConsoleAddress />
                </Row>
                <Row term="SFTP">
                  {!info.sftpEnabled ? (
                    "Off"
                  ) : info.sftpAddress ? (
                    <Copyable value={info.sftpAddress} label="Copy SFTP address" />
                  ) : (
                    "On"
                  )}
                </Row>
              </dl>
            </Card>

            <Card title="Security" icon="icon-[tabler--shield-lock]">
              <dl>
                <Row term="KMS">
                  <Flag on={info.kms} />
                </Row>
                <Row term="Auto-encryption">
                  <Flag on={info.autoEncryption} />
                </Row>
                <Row term="TLS">
                  <Flag on={info.tls} />
                </Row>
                <Row term="Trusted proxies">
                  <Flag on={info.trustedProxies} />
                </Row>
              </dl>
            </Card>
          </>
        )}

        <div id="account" className="scroll-mt-6">
          <Card title="Account" icon="icon-[tabler--user]" className="h-full">
            <dl>
              <Row term="Access key">
                {accessKey ? (
                  <>
                    <span className="min-w-0 break-all">
                      <bdi>{safeDisplayName(accessKey)}</bdi>
                    </span>
                    <CopyButton value={accessKey} label="Copy access key" />
                  </>
                ) : (
                  "Unknown"
                )}
              </Row>
              {accountType && <Row term="Account type">{accountType}</Row>}
            </dl>
            {account === "iam" && <ChangeSecretForm accessKey={accessKey} />}
            {account === "root" && (
              <p className="mt-4 flex items-start gap-2 border-t border-stone-800/70 pt-4 text-[13px] text-stone-400">
                <span
                  aria-hidden="true"
                  className="icon-[tabler--info-circle] mt-0.5 shrink-0 text-base"
                />
                <span>
                  Root credentials come from <code className="font-mono">OBSTOR_ROOT_USER</code> and{" "}
                  <code className="font-mono">OBSTOR_ROOT_PASSWORD</code>.
                </span>
              </p>
            )}
          </Card>
        </div>
      </div>
    </div>
  );
}
