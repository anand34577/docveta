import * as React from "react";
import { CalendarRange, ChevronDown, LayoutGrid, List, Search, SlidersHorizontal, X } from "lucide-react";
import { useTaxonomy } from "@/lib/queries";
import type { DocQuery, TaxonomyKind } from "@/lib/types";
import { cn, formatDocDate, tagDot } from "@/lib/utils";
import { useUI } from "@/stores/ui";
import { Button } from "@/components/ui/button";
import { Input, NativeSelect } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/overlay";
import { Checkbox } from "@/components/ui/misc";
import { useCurrentUser } from "@/components/app-shell";

interface Props {
  query: DocQuery;
  onChange: (q: DocQuery) => void;
  total?: number;
  hideSpace?: boolean;
  actions?: React.ReactNode;
}

/** Search box + filter chips. Every filter lives in the URL, so views are shareable and Back works. */
export function FilterBar({ query, onChange, total, hideSpace, actions }: Props) {
  const me = useCurrentUser();
  const { layout, setLayout } = useUI();
  const [text, setText] = React.useState(query.q ?? "");
  React.useEffect(() => setText(query.q ?? ""), [query.q]);

  // Debounced search-as-you-type.
  React.useEffect(() => {
    const t = setTimeout(() => {
      if ((query.q ?? "") !== text) onChange({ ...query, q: text || undefined });
    }, 250);
    return () => clearTimeout(t);
  }, [text]);

  const spaceId = query.space_id?.length === 1 ? query.space_id[0] : undefined;
  const set = (patch: Partial<DocQuery>) => onChange({ ...query, ...patch });
  const activeCount =
    (query.tag_id?.length ?? 0) + (query.correspondent_id?.length ?? 0) + (query.document_type_id?.length ?? 0) +
    (query.date_from || query.date_to ? 1 : 0) + (query.untagged ? 1 : 0) + (query.status ? 1 : 0);

  return (
    <div className="space-y-3 px-4 pb-4 sm:px-6">
      <div className="flex gap-2">
        <div className="relative flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-subtle" />
          <Input
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder="Search text, titles, tags…"
            className="pl-9 pr-8"
            aria-label="Search documents"
          />
          {text && (
            <button onClick={() => setText("")} className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-1 text-subtle hover:text-fg" aria-label="Clear search">
              <X className="size-3.5" />
            </button>
          )}
        </div>
        <div className="hidden rounded-md border border-border bg-surface p-0.5 sm:flex">
          <button
            onClick={() => setLayout("grid")}
            className={cn("rounded p-1.5 text-muted", layout === "grid" && "bg-surface-2 text-fg")}
            aria-label="Grid view"
            aria-pressed={layout === "grid"}
          >
            <LayoutGrid className="size-4" />
          </button>
          <button
            onClick={() => setLayout("list")}
            className={cn("rounded p-1.5 text-muted", layout === "list" && "bg-surface-2 text-fg")}
            aria-label="List view"
            aria-pressed={layout === "list"}
          >
            <List className="size-4" />
          </button>
        </div>
        {actions}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        {!hideSpace && me.spaces.length > 1 && (
          <NativeSelect
            value={spaceId ?? ""}
            onChange={(e) => set({ space_id: e.target.value ? [e.target.value] : undefined, tag_id: undefined, correspondent_id: undefined, document_type_id: undefined })}
            className="h-8 w-auto text-[13px]"
            aria-label="Space"
          >
            <option value="">All spaces</option>
            {me.spaces.map((s) => (
              <option key={s.id} value={s.id}>
                {s.kind === "personal" ? "Personal" : s.name}
              </option>
            ))}
          </NativeSelect>
        )}
        <TaxonomyFilter kind="tags" label="Tags" spaceId={spaceId} value={query.tag_id ?? []} onChange={(v) => set({ tag_id: v.length ? v : undefined })} />
        <TaxonomyFilter
          kind="correspondents"
          label="From"
          spaceId={spaceId}
          value={query.correspondent_id ?? []}
          onChange={(v) => set({ correspondent_id: v.length ? v : undefined })}
        />
        <TaxonomyFilter
          kind="document-types"
          label="Type"
          spaceId={spaceId}
          value={query.document_type_id ?? []}
          onChange={(v) => set({ document_type_id: v.length ? v : undefined })}
        />
        <DateFilter from={query.date_from} to={query.date_to} onChange={(date_from, date_to) => set({ date_from, date_to })} />
        <MoreFilters query={query} onChange={onChange} />
        {activeCount > 0 && (
          <Button size="sm" variant="ghost" onClick={() => onChange({ q: query.q, space_id: query.space_id, sort: query.sort, trash: query.trash, inbox: query.inbox })}>
            Clear filters
          </Button>
        )}
        <div className="ml-auto flex items-center gap-2">
          {total !== undefined && <span className="text-[13px] tabular-nums text-subtle">{total.toLocaleString()} document{total === 1 ? "" : "s"}</span>}
          <NativeSelect value={query.sort ?? ""} onChange={(e) => set({ sort: e.target.value || undefined })} className="h-8 w-auto text-[13px]" aria-label="Sort">
            <option value="">{query.q ? "Best match" : "Newest added"}</option>
            {query.q && <option value="-added">Newest added</option>}
            <option value="added">Oldest added</option>
            <option value="-date">Document date (newest)</option>
            <option value="date">Document date (oldest)</option>
            <option value="title">Title A–Z</option>
            <option value="-updated">Recently changed</option>
          </NativeSelect>
        </div>
      </div>
    </div>
  );
}

function FilterButton({ label, count, children }: { label: string; count: number; children: React.ReactNode }) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          className={cn(
            "flex h-8 items-center gap-1.5 rounded-md border px-2.5 text-[13px] transition-colors",
            count ? "border-accent/40 bg-accent-soft text-accent-soft-fg" : "border-border bg-surface text-muted hover:text-fg",
          )}
        >
          {label}
          {count > 0 && <span className="rounded-full bg-accent px-1.5 text-[11px] font-semibold text-accent-fg">{count}</span>}
          <ChevronDown className="size-3.5" />
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-72 p-0">{children}</PopoverContent>
    </Popover>
  );
}

function TaxonomyFilter({ kind, label, spaceId, value, onChange }: { kind: TaxonomyKind; label: string; spaceId?: string; value: string[]; onChange: (v: string[]) => void }) {
  const { data: items = [] } = useTaxonomy(kind, spaceId);
  const [q, setQ] = React.useState("");
  const shown = items.filter((i) => i.name.toLowerCase().includes(q.toLowerCase()));
  return (
    <FilterButton label={label} count={value.length}>
      <div className="border-b border-border p-2">
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={`Filter ${label.toLowerCase()}…`} className="h-8" autoFocus />
      </div>
      <div className="max-h-72 overflow-y-auto scrollbar-thin p-1">
        {shown.length === 0 && <div className="px-3 py-4 text-center text-sm text-muted">Nothing here yet</div>}
        {shown.map((it) => {
          const on = value.includes(it.id);
          return (
            <label key={it.id} className="flex cursor-pointer items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm hover:bg-surface-2">
              <Checkbox checked={on} onCheckedChange={() => onChange(on ? value.filter((v) => v !== it.id) : [...value, it.id])} />
              {kind === "tags" && <span className={cn("size-2 rounded-full", tagDot[it.color ?? "slate"])} />}
              <span className="flex-1 truncate">{it.name}</span>
              <span className="text-xs text-subtle">{it.document_count}</span>
            </label>
          );
        })}
      </div>
      {value.length > 0 && (
        <div className="border-t border-border p-1.5">
          <Button size="sm" variant="ghost" className="w-full" onClick={() => onChange([])}>
            Clear
          </Button>
        </div>
      )}
    </FilterButton>
  );
}

function DateFilter({ from, to, onChange }: { from?: string; to?: string; onChange: (from?: string, to?: string) => void }) {
  const y = new Date().getFullYear();
  const presets: [string, string, string][] = [
    ["This year", `${y}-01-01`, `${y}-12-31`],
    ["Last year", `${y - 1}-01-01`, `${y - 1}-12-31`],
    // Indian financial year (Apr–Mar), commonly needed for taxes.
    [`FY ${y - 1}–${String(y).slice(2)}`, `${y - 1}-04-01`, `${y}-03-31`],
  ];
  const label = from || to ? [from && formatDocDate(from), to && formatDocDate(to)].filter(Boolean).join(" – ") : "Date";
  return (
    <FilterButton label={label} count={from || to ? 1 : 0}>
      <div className="space-y-3 p-3">
        <div className="flex flex-wrap gap-1.5">
          {presets.map(([l, f, t]) => (
            <Button key={l} size="sm" variant={from === f && to === t ? "soft" : "secondary"} onClick={() => onChange(f, t)}>
              {l}
            </Button>
          ))}
        </div>
        <div className="grid grid-cols-2 gap-2">
          <label className="text-xs text-muted">
            From
            <Input type="date" value={from ?? ""} onChange={(e) => onChange(e.target.value || undefined, to)} className="mt-1 h-8" />
          </label>
          <label className="text-xs text-muted">
            To
            <Input type="date" value={to ?? ""} onChange={(e) => onChange(from, e.target.value || undefined)} className="mt-1 h-8" />
          </label>
        </div>
        {(from || to) && (
          <Button size="sm" variant="ghost" className="w-full" onClick={() => onChange(undefined, undefined)}>
            <CalendarRange /> Any date
          </Button>
        )}
      </div>
    </FilterButton>
  );
}

function MoreFilters({ query, onChange }: { query: DocQuery; onChange: (q: DocQuery) => void }) {
  const count = (query.untagged ? 1 : 0) + (query.status ? 1 : 0);
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          className={cn(
            "flex h-8 items-center gap-1.5 rounded-md border px-2.5 text-[13px]",
            count ? "border-accent/40 bg-accent-soft text-accent-soft-fg" : "border-border bg-surface text-muted hover:text-fg",
          )}
          aria-label="More filters"
        >
          <SlidersHorizontal className="size-3.5" />
          {count > 0 && <span className="rounded-full bg-accent px-1.5 text-[11px] font-semibold text-accent-fg">{count}</span>}
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-64 space-y-1 p-2">
        <label className="flex cursor-pointer items-center gap-2.5 rounded-md px-2 py-1.5 text-sm hover:bg-surface-2">
          <Checkbox checked={!!query.untagged} onCheckedChange={(v) => onChange({ ...query, untagged: v ? true : undefined })} />
          Without tags
        </label>
        <div className="px-2 pt-2 text-xs font-medium text-subtle">Status</div>
        {[
          ["", "Any"],
          ["processing", "Processing"],
          ["failed,needs_password", "Needs attention"],
        ].map(([v, l]) => (
          <label key={v} className="flex cursor-pointer items-center gap-2.5 rounded-md px-2 py-1.5 text-sm hover:bg-surface-2">
            <input type="radio" name="status" checked={(query.status ?? "") === v} onChange={() => onChange({ ...query, status: v || undefined })} className="accent-[var(--accent)]" />
            {l}
          </label>
        ))}
      </PopoverContent>
    </Popover>
  );
}
