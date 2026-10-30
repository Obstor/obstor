"use client";

import { fontVars } from "./fonts";
import "./globals.css";
import { ErrorScreen } from "@/components/ErrorScreen";

// Replaces root layout with its own html tag
export default function GlobalError({
  retry,
}: {
  error: Error & { digest?: string };
  retry: () => void;
}) {
  return (
    <html lang="en" className={`antialiased ${fontVars}`}>
      <body className="bg-void font-body text-stone-100">
        <ErrorScreen
          code="500"
          title="This page couldn't load"
          body="Reload to try again, or go back."
          onRetry={retry}
        />
      </body>
    </html>
  );
}
