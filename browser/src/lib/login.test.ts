// Run via node --experimental-strip-types src/lib/login.test.ts
import assert from "node:assert/strict";
import { classifyLoginResponse, cookieSecure, loginErrorMessage, sessionExpired } from "./login.ts";

const JSON_TYPE = "application/json; charset=utf-8";
const ENDPOINT = "http://127.0.0.1:9200";

// Token needs 2xx JSON reply with result.token
assert.deepEqual(
  classifyLoginResponse(
    200,
    JSON_TYPE,
    '{"jsonrpc":"2.0","result":{"token":"t0k","uiVersion":"x"},"id":1}',
  ),
  { ok: true, token: "t0k" },
);
assert.deepEqual(
  classifyLoginResponse(200, "text/html; charset=utf-8", "<!doctype html><title>x</title>"),
  { ok: false, failure: { kind: "http", status: 200 } },
);
assert.deepEqual(classifyLoginResponse(502, "text/html", "Bad Gateway"), {
  ok: false,
  failure: { kind: "http", status: 502 },
});
assert.deepEqual(
  classifyLoginResponse(
    503,
    JSON_TYPE,
    '{"jsonrpc":"2.0","error":{"code":0,"message":"x","data":{"code":"AccessDenied"}},"id":1}',
  ),
  { ok: false, failure: { kind: "http", status: 503 } },
);
assert.deepEqual(classifyLoginResponse(200, null, '{"result":{"token":"t"}}'), {
  ok: false,
  failure: { kind: "http", status: 200 },
});
assert.deepEqual(classifyLoginResponse(200, JSON_TYPE, "{not json"), {
  ok: false,
  failure: { kind: "http", status: 200 },
});
assert.deepEqual(classifyLoginResponse(200, JSON_TYPE, '{"jsonrpc":"2.0","result":{},"id":1}'), {
  ok: false,
  failure: { kind: "http", status: 200 },
});
assert.deepEqual(
  classifyLoginResponse(
    200,
    JSON_TYPE,
    '{"jsonrpc":"2.0","error":{"code":0,"message":"authentication failed, check your access credentials","data":{"code":"AccessDenied"}},"id":1}',
  ),
  {
    ok: false,
    failure: {
      kind: "rpc",
      code: "AccessDenied",
      message: "authentication failed, check your access credentials",
    },
  },
);
assert.deepEqual(
  classifyLoginResponse(
    200,
    JSON_TYPE,
    '{"jsonrpc":"2.0","error":{"code":0,"message":"rpc: unknown method","data":null},"id":1}',
  ),
  { ok: false, failure: { kind: "rpc", code: "", message: "rpc: unknown method" } },
);

// One message per failure, unknown code keeps server message
assert.equal(
  loginErrorMessage({ kind: "network" }, ENDPOINT),
  "Cannot reach the Obstor server at http://127.0.0.1:9200.",
);
assert.equal(
  loginErrorMessage({ kind: "http", status: 502 }, ENDPOINT),
  "Server returned HTTP 502.",
);
assert.equal(
  loginErrorMessage(
    {
      kind: "rpc",
      code: "XObstorServerNotInitialized",
      message: "server not initialized, please try again",
    },
    ENDPOINT,
  ),
  "Server is starting. Try again in a few seconds.",
);
assert.equal(
  loginErrorMessage(
    {
      kind: "rpc",
      code: "AccessDenied",
      message: "authentication failed, check your access credentials",
    },
    ENDPOINT,
  ),
  "Invalid access key or secret key",
);
assert.equal(
  loginErrorMessage(
    { kind: "rpc", code: "XObstorSomethingNew", message: "Login is paused by the operator" },
    ENDPOINT,
  ),
  "Login is paused by the operator",
);
assert.equal(
  loginErrorMessage({ kind: "rpc", code: "", message: "rpc: unknown method" }, ENDPOINT),
  "rpc: unknown method",
);
assert.equal(
  loginErrorMessage({ kind: "rpc", code: "", message: "" }, ENDPOINT),
  "Sign in failed.",
);

// Secure unless origin is http, on|off overrides
assert.equal(cookieSecure(null, undefined), true);
assert.equal(cookieSecure("http://10.0.0.5:9001", undefined), false);
assert.equal(cookieSecure("HTTP://localhost:9001", undefined), false);
assert.equal(cookieSecure("https://x", undefined), true);
assert.equal(cookieSecure("null", undefined), true);
assert.equal(cookieSecure("http://10.0.0.5:9001", "on"), true);
assert.equal(cookieSecure("https://x", "off"), false);
assert.equal(cookieSecure("https://x", " OFF "), false);
assert.equal(cookieSecure("http://10.0.0.5:9001", "yes"), false);

// Only token failures return to login, permission denial keeps session
assert.equal(sessionExpired("WebTokenInvalid"), true);
assert.equal(sessionExpired("WebTokenMissing"), true);
assert.equal(sessionExpired("AccessDenied"), false);
assert.equal(sessionExpired(""), false);

console.log("login tests passed");
