import assert from "node:assert/strict";
import { sessionExpired } from "./login.ts";
import { parseRPCResponse, RPCError } from "./rpc-response.ts";

const failure = (status: number, body: string): RPCError => {
  try {
    parseRPCResponse(status, body);
  } catch (err) {
    assert.ok(err instanceof RPCError);
    return err;
  }
  throw new Error("Expected an RPC failure");
};

// Auth middleware returns HTTP 401
assert.equal(
  sessionExpired(failure(401, "session token invalid or expired, sign in again").code),
  true,
);

for (const code of ["WebTokenMissing", "WebTokenInvalid"]) {
  const body = JSON.stringify({ error: { message: "Sign in again", data: { code } } });
  for (const status of [200, 403]) {
    assert.equal(sessionExpired(failure(status, body).code), true);
  }
}

// Keep session when signed-in user denied a permission
const denied = failure(
  403,
  JSON.stringify({ error: { message: "Bucket access denied", data: { code: "AccessDenied" } } }),
);
assert.equal(denied.message, "Bucket access denied");
assert.equal(sessionExpired(denied.code), false);
assert.equal(sessionExpired(failure(403, "Forbidden").code), false);
assert.equal(
  sessionExpired(
    failure(200, '{"error":{"message":"Access denied","data":{"code":"AccessDenied"}}}').code,
  ),
  false,
);

assert.throws(() => parseRPCResponse(502, "Bad gateway"), /Server returned non-JSON: 502/);
assert.deepEqual(parseRPCResponse(200, '{"result":{"buckets":[]}}'), { buckets: [] });

console.log("RPC response tests passed");
