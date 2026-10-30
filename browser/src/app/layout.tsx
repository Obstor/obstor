import type { Metadata } from "next";
import { LeaveGuard } from "@/components/LeaveGuard";
import { fontVars } from "./fonts";
import "./globals.css";

export const metadata: Metadata = {
  title: "Obstor Dashboard",
  description: "Obstor object storage management dashboard",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className={`antialiased ${fontVars}`}>
      <body className="bg-void font-body text-stone-100 selection:bg-amber-500 selection:text-black">
        <LeaveGuard>{children}</LeaveGuard>
      </body>
    </html>
  );
}
