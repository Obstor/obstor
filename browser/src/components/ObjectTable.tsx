"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { Fragment, useEffect, useState } from "react";
import { Checkbox } from "@/components/Checkbox";
import { getObjectLocationsAction } from "@/lib/actions";
import { humanSize, plural } from "@/lib/format";
import {
  type Entry,
  entryName,
  isFolder,
  locationColumn,
  objectsHref,
  paginate,
  parentPrefix,
  prefixCrumbs,
  type SortField,
  sortEntries,
} from "@/lib/objects";
import { safeDisplayName } from "@/lib/safe-name";
import { LocalTime } from "./LocalTime";
import { Menu, MenuItem } from "./Menu";
import { Pager } from "./Pager";
import { RefreshButton } from "./RefreshButton";
import { SelectMenu } from "./SelectMenu";
import { SearchField, Toolbar } from "./Toolbar";

const COLUMNS: { field: SortField; label: string; className: string }[] = [
  { field: "name", label: "Name", className: "" },
  { field: "size", label: "Size", className: "w-24 md:w-28" },
  { field: "modified", label: "Modified", className: "hidden w-48 md:table-cell" },
];

const NOT_AVAILABLE = <span className="text-stone-600">N/A</span>;

export function ObjectTable({
  bucket,
  prefix,
  entries,
  showLocations,
  onNewFolder,
  onDownload,
  onShare,
  onChecksums,
  onDelete,
}: {
  bucket: string;
  prefix: string;
  entries: Entry[];
  showLocations: boolean;
  onNewFolder: () => void;
  onDownload: (name: string) => void;
  onShare: (name: string) => void;
  onChecksums: (entry: Entry) => void;
  onDelete: (names: string[]) => void;
}) {
  const router = useRouter();
  const [filter, setFilter] = useState("");
  const [sort, setSort] = useState<{ field: SortField; asc: boolean }>({
    field: "name",
    asc: true,
  });
  const [picked, setPicked] = useState<Set<string>>(() => new Set());
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(50);
  const [located, setLocated] = useState<{ key: string; locs: Record<string, string[]> }>({
    key: "",
    locs: {},
  });

  const needle = filter.trim().toLowerCase();
  const rows = sortEntries(
    needle
      ? entries.filter((e) => entryName(e.name, prefix).toLowerCase().includes(needle))
      : entries,
    sort.field,
    sort.asc,
  );
  const view = paginate(rows, page, perPage);
  const present = new Set(entries.map((e) => e.name));
  // Names deleted by a refresh drop out of selection
  const selected = [...picked].filter((n) => present.has(n));
  const pageNames = view.rows.map((e) => e.name);
  const allOnPage = pageNames.length > 0 && pageNames.every((n) => picked.has(n));
  const someOnPage = pageNames.some((n) => picked.has(n));

  // Locations are fetched for visible files, no folders
  const fileNames = pageNames.filter((n) => !isFolder(n));
  const namesKey = showLocations && fileNames.length > 0 ? JSON.stringify(fileNames) : "";
  useEffect(() => {
    if (!namesKey) return;
    let live = true;
    getObjectLocationsAction(bucket, JSON.parse(namesKey) as string[]).then((res) => {
      if (!live) return;
      const locs = typeof res.error === "string" ? {} : (res as Record<string, string[]>);
      setLocated({ key: namesKey, locs });
    });
    return () => {
      live = false;
    };
  }, [bucket, namesKey]);
  const loc = locationColumn(fileNames, located.key === namesKey ? located.locs : {});
  const showLoc = namesKey !== "" && loc.show;
  const cellFor = (name: string) => (Object.hasOwn(loc.cells, name) ? loc.cells[name] : null);

  const toggle = (name: string) =>
    setPicked((s) => {
      const next = new Set(s);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  const togglePage = () =>
    setPicked((s) => {
      const next = new Set(s);
      for (const n of pageNames) {
        if (allOnPage) next.delete(n);
        else next.add(n);
      }
      return next;
    });
  const sortBy = (field: SortField) =>
    setSort((s) => ({ field, asc: s.field === field ? !s.asc : true }));

  return (
    <section className="bg-abyss flex flex-col overflow-hidden rounded-xl lg:min-h-0 lg:flex-1">
      {selected.length > 0 ? (
        <div className="flex flex-wrap items-center gap-3 bg-amber-500/5 p-4">
          <span className="text-sm">{plural(selected.length, "item")} selected</span>
          <button
            type="button"
            onClick={() => setPicked(new Set())}
            className="font-body hover:bg-surface inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg px-3.5 text-sm font-medium whitespace-nowrap text-stone-400 transition-colors hover:text-stone-100 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
          >
            Clear
          </button>
          <button
            type="button"
            onClick={() => onDelete(selected)}
            className="font-body ml-auto inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg bg-red-500 px-3.5 text-sm font-medium whitespace-nowrap text-white transition-colors hover:bg-red-500/90 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
          >
            <span className="icon-[tabler--trash] text-base" />
            Delete
          </button>
        </div>
      ) : (
        <Toolbar>
          <div className="min-w-0 flex-1 sm:flex-none">
            <SearchField
              value={filter}
              onChange={(v) => {
                setFilter(v);
                setPage(1);
              }}
              placeholder="Filter this folder"
            />
          </div>
          <div className="ml-auto flex shrink-0 items-center gap-2">
            <RefreshButton onRefresh={() => router.refresh()} />
            <button
              type="button"
              aria-label="New folder"
              title="New folder"
              onClick={onNewFolder}
              className="bg-surface font-body hover:bg-surface-overlay inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-3.5 text-sm font-medium whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 max-sm:w-9 max-sm:px-0 pointer-coarse:min-h-11"
            >
              <span className="icon-[tabler--folder-plus] text-base" />
              <span className="max-sm:hidden">New folder</span>
            </button>
          </div>
        </Toolbar>
      )}

      {prefix && (
        <div className="flex items-center gap-2 px-4 pb-3">
          <Link
            href={objectsHref(bucket, parentPrefix(prefix))}
            aria-label="Parent folder"
            title="Parent folder"
            className="bg-surface hover:bg-surface-overlay inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-stone-800 text-lg text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11"
          >
            <span className="icon-[tabler--chevron-left]" />
          </Link>
          <nav
            aria-label="Folder path"
            className="flex min-w-0 items-center gap-1.5 overflow-x-auto text-sm"
          >
            <Link
              href={objectsHref(bucket)}
              className="whitespace-nowrap text-stone-400 hover:text-stone-100"
            >
              <bdi>{safeDisplayName(bucket)}</bdi>
            </Link>
            {prefixCrumbs(prefix).map((c, i, all) => (
              <Fragment key={c.prefix}>
                <span className="text-stone-600">/</span>
                {i === all.length - 1 ? (
                  <span
                    aria-current="page"
                    className="font-medium whitespace-nowrap text-stone-100"
                  >
                    <bdi>{safeDisplayName(c.name)}</bdi>
                  </span>
                ) : (
                  <Link
                    href={objectsHref(bucket, c.prefix)}
                    className="whitespace-nowrap text-stone-400 hover:text-stone-100"
                  >
                    <bdi>{safeDisplayName(c.name)}</bdi>
                  </Link>
                )}
              </Fragment>
            ))}
          </nav>
        </div>
      )}

      <div className="px-4 lg:flex lg:min-h-0 lg:flex-1 lg:flex-col">
        <div className="overflow-clip rounded-lg border border-stone-800/70 lg:min-h-0 lg:flex-1 lg:overflow-y-auto">
          <table className="w-full table-fixed">
            <thead className="sticky top-0 z-20">
              <tr>
                <th className="bg-surface font-body h-9 w-10 px-4 pr-0 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md">
                  <label className="flex cursor-pointer items-center">
                    <Checkbox
                      aria-label="Select all on this page"
                      ref={(el) => {
                        if (el) el.indeterminate = someOnPage && !allOnPage;
                      }}
                      checked={allOnPage}
                      onChange={togglePage}
                    />
                  </label>
                </th>
                {COLUMNS.map((c) => (
                  <th
                    key={c.field}
                    className={`bg-surface font-body h-9 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md ${c.className}`}
                    aria-sort={
                      sort.field === c.field ? (sort.asc ? "ascending" : "descending") : undefined
                    }
                  >
                    <button
                      type="button"
                      onClick={() => sortBy(c.field)}
                      className="inline-flex items-center gap-1 uppercase hover:text-stone-200"
                    >
                      {c.label}
                      {sort.field === c.field && (
                        <span
                          className={`${sort.asc ? "icon-[tabler--chevron-up]" : "icon-[tabler--chevron-down]"} text-amber-500`}
                        />
                      )}
                    </button>
                  </th>
                ))}
                {showLoc && (
                  <th className="bg-surface font-body hidden h-9 w-48 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md xl:table-cell">
                    Location
                  </th>
                )}
                <th className="bg-surface font-body h-9 w-14 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md md:w-32">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {prefix && (
                <tr className="relative hover:bg-white/[0.03]">
                  <td className="h-9 border-t border-stone-800/70 px-4 text-[13px] pointer-coarse:h-11" />
                  <td className="h-9 border-t border-stone-800/70 px-4 text-[13px] pointer-coarse:h-11">
                    <Link
                      href={objectsHref(bucket, parentPrefix(prefix))}
                      aria-label="Parent folder"
                      className="flex items-center gap-3 text-stone-400 after:absolute after:inset-0 hover:text-stone-100"
                    >
                      <span className="icon-[tabler--corner-left-up] shrink-0 text-lg" />
                      <span className="font-mono">..</span>
                    </Link>
                  </td>
                  <td className="h-9 border-t border-stone-800/70 px-4 font-mono text-[13px] pointer-coarse:h-11">
                    {NOT_AVAILABLE}
                  </td>
                  <td className="hidden h-9 border-t border-stone-800/70 px-4 font-mono text-[13px] md:table-cell pointer-coarse:h-11">
                    {NOT_AVAILABLE}
                  </td>
                  {showLoc && (
                    <td className="hidden h-9 border-t border-stone-800/70 px-4 font-mono text-[13px] xl:table-cell pointer-coarse:h-11">
                      {NOT_AVAILABLE}
                    </td>
                  )}
                  <td className="h-9 border-t border-stone-800/70 px-4 text-right text-[13px] pointer-coarse:h-11">
                    <span className="icon-[tabler--chevron-right] hidden align-middle text-lg text-stone-300 md:inline-block" />
                  </td>
                </tr>
              )}
              {view.rows.map((e) => {
                const folder = isFolder(e.name);
                const label = safeDisplayName(entryName(e.name, prefix));
                const cell = folder ? null : cellFor(e.name);
                return (
                  <tr key={e.name} className="relative hover:bg-white/[0.03]">
                    <td className="relative z-10 h-9 border-t border-stone-800/70 p-0 text-[13px] pointer-coarse:h-11">
                      <label className="flex h-full min-h-9 cursor-pointer items-center pl-4">
                        <Checkbox
                          aria-label={`Select ${label}`}
                          checked={picked.has(e.name)}
                          onChange={() => toggle(e.name)}
                        />
                      </label>
                    </td>
                    <td className="h-9 border-t border-stone-800/70 px-4 text-[13px] pointer-coarse:h-11">
                      {folder ? (
                        <Link
                          href={objectsHref(bucket, e.name)}
                          className="flex min-w-0 items-center gap-3 text-sm text-stone-200 after:absolute after:inset-0 hover:text-amber-400"
                        >
                          <span className="icon-[tabler--folder] shrink-0 text-lg text-amber-500" />
                          <span className="truncate" title={label}>
                            <bdi>{label}</bdi>
                          </span>
                        </Link>
                      ) : (
                        <span className="flex min-w-0 items-center gap-3 text-sm text-stone-200">
                          <span className="icon-[tabler--file] shrink-0 text-lg text-stone-400" />
                          <span className="truncate" title={label}>
                            <bdi>{label}</bdi>
                          </span>
                        </span>
                      )}
                    </td>
                    <td className="h-9 border-t border-stone-800/70 px-4 pr-0 font-mono text-[13px] whitespace-nowrap text-stone-300 md:pr-4 pointer-coarse:h-11">
                      {folder ? NOT_AVAILABLE : humanSize(e.size)}
                    </td>
                    <td className="hidden h-9 border-t border-stone-800/70 px-4 font-mono text-[13px] whitespace-nowrap text-stone-400 md:table-cell pointer-coarse:h-11">
                      {folder ? NOT_AVAILABLE : <LocalTime iso={e.modified} format="stamp" />}
                    </td>
                    {showLoc && (
                      <td className="hidden h-9 border-t border-stone-800/70 px-4 font-mono text-[13px] text-stone-400 xl:table-cell pointer-coarse:h-11">
                        {cell ? (
                          <span className="block truncate" title={cell.title}>
                            {cell.label}
                          </span>
                        ) : (
                          NOT_AVAILABLE
                        )}
                      </td>
                    )}
                    <td className="relative z-10 h-9 border-t border-stone-800/70 px-4 pl-0 text-[13px] md:pl-4 pointer-coarse:h-11">
                      <div className="flex items-center justify-end gap-0.5">
                        {folder ? (
                          <>
                            <span className="icon-[tabler--chevron-right] hidden text-lg text-stone-300 md:inline-block" />
                            <Menu
                              label={`Actions for ${label}`}
                              triggerClassName="inline-flex shrink-0 items-center justify-center rounded-lg text-lg transition-colors disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11 text-stone-400 hover:bg-surface hover:text-stone-100 h-7 w-7"
                              trigger={<span className="icon-[tabler--dots-vertical]" />}
                            >
                              <MenuItem
                                icon="icon-[tabler--trash]"
                                danger
                                onSelect={() => onDelete([e.name])}
                              >
                                Delete folder
                              </MenuItem>
                            </Menu>
                          </>
                        ) : (
                          <>
                            <button
                              type="button"
                              aria-label={`Download ${label}`}
                              title="Download"
                              onClick={() => onDownload(e.name)}
                              className="hover:bg-surface hidden h-7 w-7 shrink-0 items-center justify-center rounded-lg text-lg text-stone-400 transition-colors hover:text-stone-100 disabled:pointer-events-none disabled:opacity-40 md:inline-flex pointer-coarse:min-h-11 pointer-coarse:min-w-11"
                            >
                              <span className="icon-[tabler--download]" />
                            </button>
                            <button
                              type="button"
                              aria-label={`Share ${label}`}
                              title="Share link"
                              onClick={() => onShare(e.name)}
                              className="hover:bg-surface hidden h-7 w-7 shrink-0 items-center justify-center rounded-lg text-lg text-stone-400 transition-colors hover:text-stone-100 disabled:pointer-events-none disabled:opacity-40 md:inline-flex pointer-coarse:min-h-11 pointer-coarse:min-w-11"
                            >
                              <span className="icon-[tabler--link]" />
                            </button>
                            <Menu
                              label={`Actions for ${label}`}
                              triggerClassName="inline-flex shrink-0 items-center justify-center rounded-lg text-lg transition-colors disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11 text-stone-400 hover:bg-surface hover:text-stone-100 h-7 w-7"
                              trigger={<span className="icon-[tabler--dots-vertical]" />}
                            >
                              <MenuItem
                                icon="icon-[tabler--download]"
                                className="md:hidden"
                                onSelect={() => onDownload(e.name)}
                              >
                                Download
                              </MenuItem>
                              <MenuItem
                                icon="icon-[tabler--link]"
                                className="md:hidden"
                                onSelect={() => onShare(e.name)}
                              >
                                Share link
                              </MenuItem>
                              <MenuItem icon="icon-[tabler--hash]" onSelect={() => onChecksums(e)}>
                                Checksums
                              </MenuItem>
                              <MenuItem
                                icon="icon-[tabler--trash]"
                                danger
                                onSelect={() => onDelete([e.name])}
                              >
                                Delete
                              </MenuItem>
                            </Menu>
                          </>
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>

          {entries.length === 0 ? (
            <p className="px-4 py-12 text-center text-sm text-stone-400">
              This folder is empty. Drop files here or use Upload objects.
            </p>
          ) : (
            rows.length === 0 && (
              <p className="px-4 py-8 text-center text-sm text-stone-400">
                No objects match that filter.
              </p>
            )
          )}
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-3 px-4 py-3">
        <span className="font-mono text-xs text-stone-400">{plural(rows.length, "item")}</span>
        <div className="ml-auto flex flex-wrap items-center justify-end gap-3">
          <SelectMenu
            label="Objects per page"
            value={perPage}
            onChange={(count) => {
              setPerPage(count);
              setPage(1);
            }}
            options={[50, 100, 200].map((n) => ({ value: n, label: `${n} per page` }))}
            className="h-8 text-[13px]"
          />
          <Pager page={view.page} pages={view.pages} onPage={setPage} />
        </div>
      </div>
    </section>
  );
}
