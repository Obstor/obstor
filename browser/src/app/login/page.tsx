import { LoginForm } from "./LoginForm";

export default function LoginPage() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-void px-6">
      <div className="w-full max-w-sm">
        <div className="mb-8 text-center">
          <span className="icon-[fluent-emoji-high-contrast--lobster] h-12 w-12 text-2xl text-amber-500" />
          <h1 className="font-display font-semibold text-3xl">Obstor</h1>
          <p className="mt-1 font-body text-sm text-stone-600">Sign in to your storage dashboard</p>
        </div>

        <LoginForm />

        <p className="mt-8 text-center font-mono text-[10px] text-stone-600">
          Obstor Dashboard | Apache 2.0 Licensed
        </p>
      </div>
    </div>
  );
}
