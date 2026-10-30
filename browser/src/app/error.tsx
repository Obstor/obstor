"use client";

import { ErrorScreen } from "@/components/ErrorScreen";

export default function ErrorPage({
  retry,
}: {
  error: Error & { digest?: string };
  retry: () => void;
}) {
  return (
    <ErrorScreen
      code="500"
      title="This page couldn't load"
      body="Reload to try again, or go back."
      onRetry={retry}
    />
  );
}
