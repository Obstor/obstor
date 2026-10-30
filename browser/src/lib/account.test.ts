// Run via node --experimental-strip-types src/lib/account.test.ts
import assert from "node:assert/strict";
import { accountLabel, SECRET_MAX, SECRET_MIN, secretFormError } from "./account.ts";

assert.equal(accountLabel("root"), "Root");
assert.equal(accountLabel("iam"), "IAM user");
assert.equal(accountLabel("temporary"), "Temporary credentials");
assert.equal(accountLabel("service"), "Service account");
assert.equal(accountLabel(""), "");
assert.equal(accountLabel("ldap"), "");
assert.equal(accountLabel("toString"), "");

assert.equal(SECRET_MIN, 8);
assert.equal(SECRET_MAX, 40);

const LENGTH = "New secret keys are 8 to 40 characters.";
const MISMATCH = "The new secret keys do not match.";
const s = (n: number) => "k".repeat(n);

assert.equal(secretFormError("", s(8), s(8)), "Enter your current secret key.");
// Check current secret first
assert.equal(secretFormError("", s(7), s(9)), "Enter your current secret key.");
assert.equal(secretFormError("current", s(7), s(7)), LENGTH);
assert.equal(secretFormError("current", s(41), s(41)), LENGTH);
assert.equal(secretFormError("current", "", ""), LENGTH);
assert.equal(secretFormError("current", s(8), s(8)), null);
assert.equal(secretFormError("current", s(40), s(40)), null);
assert.equal(secretFormError("current", "abcdefgh", "abcdefgi"), MISMATCH);

// Spaces count toward length and comparison
assert.equal(secretFormError("current", "  spaced secret  ", "  spaced secret  "), null);
assert.equal(secretFormError("current", "  spaced secret  ", "spaced secret"), MISMATCH);
assert.equal(secretFormError("  ", s(8), s(8)), null);
assert.equal(secretFormError("current", "  short  ", "  short  "), null);
assert.equal(secretFormError("current", " short ", " short "), LENGTH);

console.log("account tests passed");
