// Run via node --experimental-strip-types src/lib/bucket-tabs.test.ts
import assert from "node:assert/strict";
import {
  BUCKET_TABS,
  type ClickInfo,
  guardsClick,
  type LinkInfo,
  parseTab,
  rovingIndex,
  tabHref,
} from "./bucket-tabs.ts";
import { objectsHref } from "./objects.ts";

// Exact tab ID only, others open Objects
assert.equal(parseTab("activity"), "activity");
assert.equal(parseTab("settings"), "settings");
assert.equal(parseTab("access"), "access");
assert.equal(parseTab("objects"), "objects");
assert.equal(parseTab("SETTINGS"), "objects");
assert.equal(parseTab("ACTIVITY"), "objects");

// Activity is appended as the fourth tab
assert.deepEqual(
  BUCKET_TABS.map((t) => t.id),
  ["objects", "settings", "access", "activity"],
);
assert.equal(parseTab(""), "objects");
assert.equal(parseTab(undefined), "objects");
assert.equal(parseTab(["settings", "access"]), "objects");

// Tab links keep the folder so Objects comes back to it
assert.equal(tabHref("alpha", "objects"), "/buckets/alpha");
assert.equal(tabHref("alpha", "settings"), "/buckets/alpha?tab=settings");
assert.equal(tabHref("alpha", "access", "docs/"), "/buckets/alpha?prefix=docs%2F&tab=access");
assert.equal(tabHref("a b", "settings"), "/buckets/a%20b?tab=settings");
assert.equal(tabHref("alpha", "objects", "docs/"), objectsHref("alpha", "docs/"));
assert.equal(tabHref("alpha", "activity"), "/buckets/alpha?tab=activity");
assert.equal(tabHref("alpha", "activity", "docs/"), "/buckets/alpha?prefix=docs%2F&tab=activity");

// Arrow keys wrap, Home and End jump, other keys leave focus alone
assert.equal(rovingIndex("ArrowRight", 2, 3), 0);
assert.equal(rovingIndex("ArrowLeft", 0, 3), 2);
assert.equal(rovingIndex("Home", 2, 3), 0);
assert.equal(rovingIndex("End", 0, 3), 2);
assert.equal(rovingIndex("Enter", 1, 3), null);
assert.equal(rovingIndex("ArrowRight", 0, 0), null);
assert.equal(rovingIndex("End", 0, BUCKET_TABS.length), 3);

// Guard plain links to another page on same origin
const here = "http://localhost:9001/buckets/alpha?tab=settings";
const plain: ClickInfo = {
  button: 0,
  metaKey: false,
  ctrlKey: false,
  shiftKey: false,
  altKey: false,
  defaultPrevented: false,
};
const guards = (href: string, click: Partial<ClickInfo> = {}, link: Partial<LinkInfo> = {}) =>
  guardsClick({ ...plain, ...click }, { href, target: "", download: false, ...link }, here);
assert.equal(guards("http://localhost:9001/buckets"), true);
assert.equal(guards("/access"), true);
assert.equal(guards("http://localhost:9001/buckets/alpha?tab=access"), true);
assert.equal(guards("/access", {}, { target: "_self" }), true);
assert.equal(guards("/access", { metaKey: true }), false);
assert.equal(guards("/access", { ctrlKey: true }), false);
assert.equal(guards("/access", { shiftKey: true }), false);
assert.equal(guards("/access", { altKey: true }), false);
assert.equal(guards("/access", { button: 1 }), false);
assert.equal(guards("/access", { defaultPrevented: true }), false);
assert.equal(guards("/access", {}, { target: "_blank" }), false);
assert.equal(guards("/access", {}, { download: true }), false);
assert.equal(guards("https://obstor.net/docs"), false);
assert.equal(guards("https://github.com/obstor/obstor"), false);
assert.equal(guards("http://localhost:9001/buckets/alpha?tab=settings#top"), false);
assert.equal(guards(here), false);
assert.equal(guards(""), false);
assert.equal(guards("javascript:void(0)"), false);
assert.equal(guards("mailto:a@b.c"), false);

console.log("bucket-tabs tests passed");
