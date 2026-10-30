// Error code distinguishes rejected session from permission denial
export class RPCError extends Error {
  code: string;
  constructor(message: string, code: string) {
    super(message);
    this.name = "RPCError";
    this.code = code;
  }
}

export function parseRPCResponse<T>(status: number, text: string): T {
  if (status === 401) throw new RPCError("Unauthorized", "WebTokenInvalid");

  let data: { result?: T; error?: { message?: string; data?: { code?: unknown } | null } };
  try {
    data = JSON.parse(text);
  } catch {
    if (status === 403) throw new RPCError("Access denied", "AccessDenied");
    throw new Error(`Server returned non-JSON: ${status}`);
  }
  if (data.error) {
    const code = data.error.data?.code;
    throw new RPCError(data.error.message || "RPC error", typeof code === "string" ? code : "");
  }
  if (status === 403) throw new RPCError("Access denied", "AccessDenied");
  return data.result as T;
}
