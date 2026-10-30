"use client";

import Link from "next/link";
import { useState } from "react";
import { humanCount, humanSize, plural } from "@/lib/format";
import { type BucketSortField, objectsHref, paginate, sortBuckets } from "@/lib/objects";
import type { AccessLabel } from "@/lib/rpc";
import { safeDisplayName } from "@/lib/safe-name";
import { useBuckets } from "./BucketProvider";
import { AsOf } from "./LocalTime";
import { Menu, MenuItem, MenuLink } from "./Menu";
import { Pager } from "./Pager";
import { SearchField, Toolbar } from "./Toolbar";

// Display text and ISO timestamp
export interface BucketRow {
  name: string;
  created: string;
  createdAt: string;
  access: AccessLabel | null;
  size: number | null;
  objects: number | null;
}

type Sort = { field: BucketSortField; asc: boolean };

// Hide small labels but keep for screen readers
export function AccessTag({
  access,
  pill = false,
  compact = false,
}: {
  access: AccessLabel;
  pill?: boolean;
  compact?: boolean;
}) {
  const priv = access === "Private";
  return (
    <span
      className={`inline-flex items-center gap-1.5 align-middle whitespace-nowrap ${
        pill ? "bg-surface h-7 rounded-md border border-stone-800/70 px-2.5 text-xs" : ""
      } ${priv ? "text-stone-300" : "text-amber-400"}`}
    >
      <span
        className={`${priv ? "icon-[tabler--shield-lock]" : "icon-[tabler--world]"} shrink-0 text-base`}
      />
      {compact ? <span className="sr-only sm:not-sr-only">{access}</span> : access}
    </span>
  );
}

function SortHeader({
  field,
  label,
  sort,
  onSort,
  className = "",
  right = false,
}: {
  field: BucketSortField;
  label: string;
  sort: Sort | null;
  onSort: (field: BucketSortField) => void;
  className?: string;
  right?: boolean;
}) {
  const dir = sort && sort.field === field ? (sort.asc ? "ascending" : "descending") : undefined;
  return (
    <th
      className={`bg-surface font-body h-8 px-4 text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md ${right ? "text-right" : "text-left"} ${className}`}
      aria-sort={dir}
    >
      <button
        type="button"
        onClick={() => onSort(field)}
        className="inline-flex items-center gap-1 uppercase hover:text-stone-200"
      >
        {label}
        {dir && (
          <span
            className={`${dir === "ascending" ? "icon-[tabler--chevron-up]" : "icon-[tabler--chevron-down]"} text-amber-500`}
          />
        )}
      </button>
    </th>
  );
}

function NotAvailable() {
  return <span className="text-stone-600">N/A</span>;
}

export function BucketTable({
  rows,
  paged = false,
  usage,
  usageAsOf = "",
}: {
  rows: BucketRow[];
  paged?: boolean;
  usage: boolean;
  usageAsOf?: string;
}) {
  const { pins, togglePin, openSettings, confirmDelete } = useBuckets();
  const [filter, setFilter] = useState("");
  const [page, setPage] = useState(1);
  // Tables keep order until sorted
  const [sort, setSort] = useState<Sort | null>(paged ? { field: "name", asc: true } : null);
  const needle = filter.trim().toLowerCase();
  const matched = needle ? rows.filter((r) => r.name.toLowerCase().includes(needle)) : rows;
  const sorted = sort ? sortBuckets(matched, sort.field, sort.asc) : matched;
  const view = paged ? paginate(sorted, page, 50) : { rows: sorted, page: 1, pages: 1 };
  const showCreated = paged || !usage;
  const sortBy = (field: BucketSortField) => {
    setSort((s) => ({ field, asc: s && s.field === field ? !s.asc : true }));
    setPage(1);
  };

  return (
    <div>
      {paged && (
        <Toolbar>
          <SearchField
            value={filter}
            onChange={(v) => {
              setFilter(v);
              setPage(1);
            }}
            placeholder="Filter buckets"
          />
        </Toolbar>
      )}
      <div className="px-4 pb-4">
        <div className="overflow-hidden rounded-lg border border-stone-800/70">
          <table className="w-full table-fixed">
            <thead>
              <tr>
                <SortHeader field="name" label="Name" sort={sort} onSort={sortBy} />
                {usage && (
                  <SortHeader
                    field="size"
                    label="Size"
                    sort={sort}
                    onSort={sortBy}
                    right
                    className="w-24 sm:w-28"
                  />
                )}
                {usage && (
                  <SortHeader
                    field="objects"
                    label="Objects"
                    sort={sort}
                    onSort={sortBy}
                    right
                    className="hidden w-28 sm:table-cell"
                  />
                )}
                {showCreated && (
                  <SortHeader
                    field="created"
                    label="Created"
                    sort={sort}
                    onSort={sortBy}
                    className="hidden w-40 md:table-cell"
                  />
                )}
                <th className="bg-surface font-body h-8 w-12 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md sm:w-44">
                  <span className="sr-only sm:not-sr-only">Access</span>
                </th>
                <th className="bg-surface font-body h-8 w-14 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {view.rows.map((r) => {
                const pinned = pins.includes(r.name);
                const name = safeDisplayName(r.name);
                return (
                  <tr key={r.name} className="relative hover:bg-white/[0.03]">
                    <td className="h-10 border-t border-stone-800/70 px-4 text-[13px] pointer-coarse:h-11">
                      <Link
                        href={objectsHref(r.name)}
                        className="flex min-w-0 items-center gap-3 text-sm text-stone-200 after:absolute after:inset-0 hover:text-amber-400"
                      >
                        <span className="icon-[tabler--bucket] shrink-0 text-lg text-amber-500" />
                        <span className="truncate" title={name}>
                          <bdi>{name}</bdi>
                        </span>
                        {pinned && (
                          <>
                            <span
                              aria-hidden="true"
                              title="Pinned"
                              className="icon-[tabler--pinned-filled] shrink-0 text-sm text-stone-500"
                            />
                            <span className="sr-only">Pinned</span>
                          </>
                        )}
                      </Link>
                    </td>
                    {usage && (
                      <td
                        className="h-10 border-t border-stone-800/70 px-4 text-right font-mono text-[13px] whitespace-nowrap text-stone-300 pointer-coarse:h-11"
                        title={r.size === null ? undefined : `${r.size} bytes`}
                      >
                        {r.size === null ? <NotAvailable /> : humanSize(r.size)}
                      </td>
                    )}
                    {usage && (
                      <td className="hidden h-10 border-t border-stone-800/70 px-4 text-right font-mono text-[13px] text-stone-300 sm:table-cell pointer-coarse:h-11">
                        {r.objects === null ? <NotAvailable /> : humanCount(r.objects)}
                      </td>
                    )}
                    {showCreated && (
                      <td className="hidden h-10 border-t border-stone-800/70 px-4 font-mono text-[13px] whitespace-nowrap text-stone-400 md:table-cell pointer-coarse:h-11">
                        {r.created}
                      </td>
                    )}
                    <td className="h-10 border-t border-stone-800/70 px-4 text-[13px] whitespace-nowrap pointer-coarse:h-11">
                      {r.access ? <AccessTag access={r.access} compact /> : <NotAvailable />}
                    </td>
                    <td className="relative z-10 h-10 border-t border-stone-800/70 px-4 text-right text-[13px] pointer-coarse:h-11">
                      <Menu
                        label={`Actions for ${name}`}
                        triggerClassName="inline-flex shrink-0 items-center justify-center rounded-lg text-lg transition-colors disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11 text-stone-400 hover:bg-surface hover:text-stone-100 h-8 w-8"
                        trigger={<span className="icon-[tabler--dots]" />}
                      >
                        <MenuLink href={objectsHref(r.name)} icon="icon-[tabler--folder-open]">
                          Open
                        </MenuLink>
                        <MenuItem
                          icon="icon-[tabler--settings]"
                          onSelect={() => openSettings(r.name)}
                        >
                          Settings
                        </MenuItem>
                        <MenuItem
                          icon={pinned ? "icon-[tabler--pinned-off]" : "icon-[tabler--pin]"}
                          onSelect={() => togglePin(r.name)}
                        >
                          {pinned ? "Unpin" : "Pin"}
                        </MenuItem>
                        <MenuItem
                          icon="icon-[tabler--trash]"
                          danger
                          onSelect={() => confirmDelete(r.name)}
                        >
                          Delete
                        </MenuItem>
                      </Menu>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {matched.length === 0 && (
            <p className="px-4 py-8 text-center text-sm text-stone-400">
              No buckets match that filter.
            </p>
          )}
        </div>
      </div>
      {paged && (
        <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
          <span className="flex items-center gap-3 text-xs text-stone-400">
            <span className="font-mono">{plural(matched.length, "bucket")}</span>
            <AsOf iso={usageAsOf} />
          </span>
          {view.pages > 1 && <Pager page={view.page} pages={view.pages} onPage={setPage} />}
        </div>
      )}
    </div>
  );
}
