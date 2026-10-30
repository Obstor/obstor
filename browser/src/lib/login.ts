// Login replies, messages and cookie policy

export type LoginFailure =
  | { kind: "network" }
  | { kind: "http"; status: number }
  | { kind: "rpc"; code: string; message: string };

export type LoginResult = { ok: true; token: string } | { ok: false; failure: LoginFailure };

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null;
}

// Other replies use HTTP status
export function classifyLoginResponse(
  status: number,
  contentType: string | null,
  body: string,
): LoginResult {
  const http: LoginResult = { ok: false, failure: { kind: "http", status } };
  if (status < 200 || status > 299) return http;
  if (!contentType?.toLowerCase().includes("application/json")) return http;
  let data: unknown;
  try {
    data = JSON.parse(body);
  } catch {
    return http;
  }
  if (!isRecord(data)) return http;
  const err = data.error;
  if (isRecord(err)) {
    const code = isRecord(err.data) && typeof err.data.code === "string" ? err.data.code : "";
    const message = typeof err.message === "string" ? err.message : "";
    return { ok: false, failure: { kind: "rpc", code, message } };
  }
  const result = data.result;
  if (isRecord(result) && typeof result.token === "string" && result.token !== "") {
    return { ok: true, token: result.token };
  }
  return http;
}

export function loginErrorMessage(f: LoginFailure, endpoint: string): string {
  switch (f.kind) {
    case "network":
      return `Cannot reach the Obstor server at ${endpoint}.`;
    case "http":
      return `Server returned HTTP ${f.status}.`;
    case "rpc":
      if (f.code === "XObstorServerNotInitialized") {
        return "Server is starting. Try again in a few seconds.";
      }
      if (f.code === "AccessDenied") return "Invalid access key or secret key";
      return f.message || "Sign in failed.";
  }
}

// Secure unless origin is http
export function cookieSecure(origin: string | null, override: string | undefined): boolean {
  const o = override?.trim().toLowerCase();
  if (o === "on") return true;
  if (o === "off") return false;
  return !(origin ?? "").toLowerCase().startsWith("http://");
}

// Missing or invalid token expires session
export function sessionExpired(code: string): boolean {
  return code === "WebTokenInvalid" || code === "WebTokenMissing";
}
