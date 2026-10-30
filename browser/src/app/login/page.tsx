import { connection } from "next/server";
import { publicHost } from "@/lib/rpc";
import { LoginForm } from "./LoginForm";

export default async function LoginPage() {
  // Render per request at runtime
  await connection();
  const host = publicHost();
  return (
    <main className="bg-void flex min-h-dvh items-center justify-center px-4">
      <div className="motion-safe:animate-page-in w-full max-w-[400px]">
        <div className="bg-abyss rounded-xl p-8">
          <div className="mb-8 flex items-center gap-3">
            <span className="icon-[fluent-emoji-high-contrast--lobster] text-[40px] text-amber-500" />
            <span className="font-display text-[28px] font-semibold">Obstor</span>
          </div>
          <h1 className="font-display text-xl font-semibold">Sign in</h1>
          <p className="mt-1 mb-6 truncate font-mono text-sm text-stone-400" title={host}>
            {host}
          </p>
          <LoginForm />
        </div>
        <p className="mt-6 px-1 text-xs text-stone-500">Apache 2.0 licensed</p>
      </div>
    </main>
  );
}
