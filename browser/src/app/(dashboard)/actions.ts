"use server";

import { rpc } from "@/lib/rpc";

export async function mintEnrollToken(): Promise<{ token: string; command: string }> {
  return rpc<{ token: string; command: string }>("FleetEnrollToken");
}
