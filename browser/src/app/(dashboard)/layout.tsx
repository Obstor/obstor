import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { Header } from "@/components/Header";
import { Sidebar } from "@/components/Sidebar";
import { getAccessBadgeAction } from "@/lib/actions";
import { type BucketEntry, humanSize, listBuckets, rpc } from "@/lib/rpc";

interface StorageResult {
  used: number;
}

interface ServerResult {
  ObstorVersion: string;
  ObstorPlatform: string;
}

export default async function DashboardLayout({ children }: { children: React.ReactNode }) {
  const cookieStore = await cookies();
  const token = cookieStore.get("obstor_token");
  if (!token) redirect("/login");

  let buckets: BucketEntry[] = [];
  let storageUsed = "0 B";
  let serverVersion = "";
  let serverPlatform = "";
  let authFailed = false;

  try {
    buckets = await listBuckets();
  } catch (err) {
    const msg = err instanceof Error ? err.message : "";
    if (msg.includes("Unauthorized") || msg.includes("token")) authFailed = true;
  }

  if (authFailed) {
    redirect("/login");
  }

  try {
    const storageRes = await rpc<StorageResult>("StorageInfo");
    storageUsed = humanSize(storageRes.used);
  } catch {
    // non-critical
  }

  try {
    const serverRes = await rpc<ServerResult>("ServerInfo");
    serverVersion = serverRes.ObstorVersion;
    serverPlatform = serverRes.ObstorPlatform;
  } catch {
    // non-critical
  }

  const access = await getAccessBadgeAction();

  return (
    <div className="flex h-screen overflow-hidden">
      <Sidebar
        buckets={buckets}
        storageUsed={storageUsed}
        bucketCount={buckets.length}
        serverVersion={serverVersion}
        serverPlatform={serverPlatform}
        access={access}
      />
      <div className="flex flex-1 flex-col overflow-hidden">
        <Header />
        <main className="flex-1 overflow-y-auto bg-void p-6">{children}</main>
      </div>
    </div>
  );
}
