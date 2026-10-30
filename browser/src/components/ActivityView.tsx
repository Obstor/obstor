"use client";

import Link from "next/link";
import { type FormEvent, useEffect, useRef, useState } from "react";
import { listActivityAction } from "@/lib/actions";
import {
  ACTIVITY_GROUPS,
  type ActivityCursor,
  type ActivityEvent,
  type ActivityGroup,
  type ActivityPage,
  activityFailed,
  activityLabel,
  activitySourceLabel,
  activityTarget,
  activityWho,
} from "@/lib/activity";
import { plural } from "@/lib/format";
import { objectsHref } from "@/lib/objects";
import { safeDisplayName } from "@/lib/safe-name";
import { RelativeTime } from "./LocalTime";
import { SelectMenu } from "./SelectMenu";
import { SearchField, Toolbar } from "./Toolbar";

type Scope = "all" | "bucket";

const rowKey = (e: ActivityEvent) => `${e.node}|${e.seq}`;

interface Filters {
  bucket: string;
  accessKey: string;
  group: ActivityGroup | "";
}

const EMPTY: ActivityPage = {
  events: [],
  next: null,
  partial: false,
  missingNodes: 0,
  denied: 0,
  dropped: 0,
};

const NOT_AVAILABLE = <span className="text-stone-600">N/A</span>;

function Who({ event }: { event: ActivityEvent }) {
  if (!event.accessKey) {
    return <span className="font-body text-stone-500">{activityWho(event)}</span>;
  }
  const key = safeDisplayName(event.accessKey);
  return (
    <span className="truncate font-mono" title={key}>
      <bdi>{key}</bdi>
    </span>
  );
}

function Status({ status }: { status: number }) {
  const failed = activityFailed(status);
  return (
    <span
      className="inline-flex items-center gap-2 font-mono"
      title={failed ? "Failed" : "Succeeded"}
    >
      <span
        aria-hidden="true"
        className={`shrink-0 text-base ${failed ? "icon-[tabler--circle-x] text-red-400" : "icon-[tabler--circle-check] text-green-400"}`}
      />
      {status}
      <span className="sr-only">{failed ? ", failed" : ", succeeded"}</span>
    </span>
  );
}

function Target({ event, scope }: { event: ActivityEvent; scope: Scope }) {
  const t = activityTarget(event);
  if (t.kind === "object") {
    const text = safeDisplayName(scope === "bucket" ? event.object : `${t.bucket}/${event.object}`);
    return (
      <Link
        href={objectsHref(t.bucket, t.prefix)}
        title={text}
        className="block truncate text-stone-200 hover:text-amber-400"
      >
        <bdi>{text}</bdi>
      </Link>
    );
  }
  if (t.kind === "objects") {
    const bucket = safeDisplayName(t.bucket);
    const count = plural(t.count, "object");
    return (
      <span className="block truncate text-stone-300" title={`${count} in ${bucket}`}>
        {count} in{" "}
        <Link href={objectsHref(t.bucket)} className="text-stone-200 hover:text-amber-400">
          <bdi>{bucket}</bdi>
        </Link>
      </span>
    );
  }
  if (t.kind === "bucket") {
    const bucket = safeDisplayName(t.bucket);
    return (
      <Link
        href={objectsHref(t.bucket)}
        title={bucket}
        className="block truncate text-stone-200 hover:text-amber-400"
      >
        <bdi>{bucket}</bdi>
      </Link>
    );
  }
  if (t.kind === "subject") {
    const name = safeDisplayName(t.name);
    return (
      <span className="block truncate font-mono text-stone-300" title={name}>
        <bdi>{name}</bdi>
      </span>
    );
  }
  return NOT_AVAILABLE;
}

function Notes({ page }: { page: ActivityPage }) {
  const counts = [
    page.denied > 0 ? `${plural(page.denied, "denied or anonymous write")} not listed` : "",
    page.dropped > 0 ? `${plural(page.dropped, "event")} dropped under load` : "",
  ].filter(Boolean);
  if (!page.partial && counts.length === 0) return null;
  return (
    <div className="space-y-1 text-[13px] text-stone-500">
      {page.partial && (
        <p className="flex items-center gap-2">
          <span aria-hidden="true" className="icon-[tabler--alert-triangle] shrink-0 text-base" />
          {plural(page.missingNodes, "node")} did not answer. The list may be incomplete.
        </p>
      )}
      {counts.length > 0 && <p>{counts.join(" | ")}</p>}
    </div>
  );
}

export function ActivityView({
  initial,
  filters,
  buckets,
  scope,
}: {
  initial: ActivityPage | { error: string };
  filters: { bucket: string; accessKey: string; group: ActivityGroup | "" };
  buckets: string[];
  scope: Scope;
}) {
  const [applied, setApplied] = useState<Filters>(filters);
  const [keyDraft, setKeyDraft] = useState(filters.accessKey);
  const [page, setPage] = useState<ActivityPage>("error" in initial ? EMPTY : initial);
  const [error, setError] = useState("error" in initial ? initial.error : "");
  const [loading, setLoading] = useState(false);
  const latest = useRef(0);
  // Focus first row
  const [focusKey, setFocusKey] = useState("");
  const focusRow = useRef<HTMLTableRowElement>(null);
  useEffect(() => {
    if (focusKey) focusRow.current?.focus();
  }, [focusKey]);

  // Newest request updates state
  async function load(next: Filters, before: ActivityCursor | null) {
    const id = ++latest.current;
    setLoading(true);
    let res: ActivityPage | { error: string };
    try {
      res = await listActivityAction({ ...next, before });
    } catch {
      res = { error: "Could not load activity" };
    }
    if (id !== latest.current) return;
    setLoading(false);
    if ("error" in res) {
      setError(res.error);
      if (!before) setPage(EMPTY);
      return;
    }
    const reply = res;
    setError("");
    setFocusKey(before && reply.events.length > 0 ? rowKey(reply.events[0]) : "");
    setPage((prev) =>
      before
        ? {
            ...reply,
            events: [...prev.events, ...reply.events],
            partial: prev.partial || reply.partial,
            missingNodes: Math.max(prev.missingNodes, reply.missingNodes),
          }
        : reply,
    );
  }

  function apply(patch: Partial<Filters>) {
    const next = { ...applied, ...patch };
    setApplied(next);
    load(next, null);
  }

  function submitKey(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const key = keyDraft.trim();
    if (key !== applied.accessKey) apply({ accessKey: key });
  }

  const filtered =
    (scope === "all" && applied.bucket !== "") || applied.accessKey !== "" || applied.group !== "";
  // Keep deleted bucket from URL selectable
  const bucketOptions =
    applied.bucket && !buckets.includes(applied.bucket) ? [applied.bucket, ...buckets] : buckets;
  const more = page.next;
  let emptyText = "No activity yet.";
  if (loading) emptyText = "Loading...";
  else if (filtered) emptyText = "No activity matches these filters.";

  return (
    <section aria-label="Activity" className="bg-abyss rounded-xl">
      <Toolbar>
        {scope === "all" && (
          <>
            <SelectMenu
              label="Bucket"
              value={applied.bucket}
              onChange={(bucket) => apply({ bucket })}
              options={[
                { value: "", label: "All buckets" },
                ...bucketOptions.map((b) => ({ value: b, label: safeDisplayName(b) })),
              ]}
              className="w-full sm:w-48"
            />
            <form onSubmit={submitKey} className="w-full sm:w-auto">
              <SearchField
                value={keyDraft}
                onChange={(v) => {
                  setKeyDraft(v);
                  if (v === "" && applied.accessKey) apply({ accessKey: "" });
                }}
                placeholder="Filter by access key"
              />
            </form>
          </>
        )}
        <SelectMenu<ActivityGroup | "">
          label="Action type"
          value={applied.group}
          onChange={(group) => apply({ group })}
          options={[
            { value: "", label: "All actions" },
            ...ACTIVITY_GROUPS.map((g) => ({ value: g.id, label: g.label })),
          ]}
          className="w-full sm:w-48"
        />
      </Toolbar>

      {error && (
        <div className="px-4 pb-4">
          <div
            role="alert"
            className="flex items-start gap-2 rounded-lg border border-red-500/20 bg-red-500/5 px-4 py-3 text-sm text-red-400"
          >
            <span
              aria-hidden="true"
              className="icon-[tabler--alert-circle] mt-0.5 shrink-0 text-base"
            />
            <span className="break-words">{error}</span>
          </div>
        </div>
      )}

      {page.events.length > 0 ? (
        <div className="px-4 pb-4">
          <div className="overflow-hidden rounded-lg border border-stone-800/70">
            <table
              aria-busy={loading}
              className={`w-full table-fixed transition-opacity ${loading ? "opacity-60" : ""}`}
            >
              <thead>
                <tr>
                  <th className="bg-surface font-body hidden h-8 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md sm:table-cell sm:w-28 lg:w-44">
                    Time
                  </th>
                  <th className="bg-surface font-body hidden h-8 w-44 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md sm:table-cell">
                    Who
                  </th>
                  <th className="bg-surface font-body h-8 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md max-sm:rounded-l-md">
                    Action
                  </th>
                  <th className="bg-surface font-body h-8 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md max-sm:rounded-r-md">
                    Target
                  </th>
                  <th className="bg-surface font-body hidden h-8 w-24 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md sm:table-cell">
                    Status
                  </th>
                </tr>
              </thead>
              <tbody>
                {page.events.map((e) => (
                  <tr
                    key={rowKey(e)}
                    ref={rowKey(e) === focusKey ? focusRow : undefined}
                    tabIndex={rowKey(e) === focusKey ? -1 : undefined}
                    className="hover:bg-white/[0.03]"
                  >
                    <td className="hidden h-10 truncate border-t border-stone-800/70 px-4 font-mono text-[13px] text-stone-400 sm:table-cell pointer-coarse:h-11">
                      <RelativeTime iso={e.time} />
                    </td>
                    <td className="hidden h-10 truncate border-t border-stone-800/70 px-4 text-[13px] text-stone-300 sm:table-cell pointer-coarse:h-11">
                      <Who event={e} />
                    </td>
                    <td className="h-10 border-t border-stone-800/70 px-4 text-[13px] pointer-coarse:h-11">
                      <div className="truncate">
                        <span className="text-stone-200">{activityLabel(e.api)}</span>
                        <span className="text-stone-500">{` | ${activitySourceLabel(e.source)}`}</span>
                      </div>
                      <div className="truncate text-[12px] text-stone-500 sm:hidden">
                        <Who event={e} />
                      </div>
                    </td>
                    <td className="h-10 border-t border-stone-800/70 px-4 text-[13px] pointer-coarse:h-11">
                      <Target event={e} scope={scope} />
                      <div className="flex items-center gap-2 overflow-hidden text-[12px] whitespace-nowrap text-stone-500 sm:hidden">
                        <span className="font-mono">
                          <RelativeTime iso={e.time} />
                        </span>
                        <span aria-hidden="true">|</span>
                        <Status status={e.status} />
                      </div>
                    </td>
                    <td className="hidden h-10 border-t border-stone-800/70 px-4 text-[13px] whitespace-nowrap text-stone-300 sm:table-cell pointer-coarse:h-11">
                      <Status status={e.status} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      ) : (
        !error && <p className="px-4 pt-2 pb-8 text-center text-sm text-stone-400">{emptyText}</p>
      )}

      {(more || page.partial || page.denied > 0 || page.dropped > 0) && (
        <div className="flex flex-col items-start gap-3 px-4 pb-4">
          <Notes page={page} />
          {more && (
            <button
              type="button"
              onClick={() => load(applied, more)}
              disabled={loading}
              className="bg-surface font-body hover:bg-surface-overlay inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-3.5 text-sm font-medium whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
            >
              {loading ? "Loading..." : "Load more"}
            </button>
          )}
        </div>
      )}
    </section>
  );
}
