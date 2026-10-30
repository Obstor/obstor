import { ErrorScreen } from "@/components/ErrorScreen";

export default function NotFound() {
  return (
    <ErrorScreen code="404" title="Page not found" body="This page was moved or does not exist." />
  );
}
