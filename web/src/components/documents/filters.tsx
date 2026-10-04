import * as React from "react";
import { DateInput } from "@/components/ui/date-input";
import { CalendarRange, Check, ChevronDown, LayoutGrid, List, ListFilter, Search, Sparkles, SlidersHorizontal, X } from "lucide-react";
import { useAIEnabled, useCustomFields, useTaxonomy } from "@/lib/queries";
import type { CustomField, DocQuery, Facets, TaxonomyKind } from "@/lib/types";
import { cn, formatDocDate, spaceLabel, tagDot } from "@/lib/utils";
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
  totalCapped?: boolean;
  facets?: Facets;
  found?: string; // how the results were found (keyword | semantic | hybrid)
  hideSpace?: boolean;
  actions?: React.ReactNode;
}

/** Search box + filter chips. Every filter lives in the URL, so views are shareable and Back works. */
export function FilterBar({ query, onChange, total, totalCapped, facets, found, hideSpace, actions }: Props) {
  const me = useCurrentUser();
  const ai = useAIEnabled().data;
  const fieldsQ = useCustomFields(query.space_id?.length === 1 ? query.space_id[0] : undefined);
  const fields = fieldsQ.data ?? [];
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
    <div className="space-y-3 page-x pb-4">
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
        {ai?.embeddings && text.trim() && (
          <div className="hidden rounded-md border border-border bg-surface p-0.5 text-[13px] sm:flex" role="radiogroup" aria-label="How to search">
            {([
              [undefined, "Words"],
              ["hybrid", "Meaning"],
            ] as const).map(([m, l]) => (
              <button
                key={l}
                role="radio"
                aria-checked={query.mode === m}
                onClick={() => set({ mode: m })}
                title={m ? "Also finds documents that mean the same thing, even with different words" : "Matches the words you type"}
                className={cn("flex items-center gap-1 rounded px-2 py-1 text-muted", query.mode === m && "bg-surface-2 font-medium text-fg")}
              >
                {m && <Sparkles className="size-3.5" />}
                {l}
              </button>
            ))}
          </div>
        )}
        {actions}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        {!hideSpace && me.spaces.length > 1 && (
          <SpaceFilter
            value={spaceId}
            onChange={(id) => set({ space_id: id ? [id] : undefined, tag_id: undefined, correspondent_id: undefined, document_type_id: undefined })}
          />
        )}
        <TaxonomyFilter kind="tags" label="Tags" spaceId={spaceId} facets={facets?.tags} value={query.tag_id ?? []} onChange={(v) => set({ tag_id: v.length ? v : undefined })} />
        <TaxonomyFilter
          kind="correspondents"
          label="From"
          spaceId={spaceId}
          facets={facets?.correspondents}
          value={query.correspondent_id ?? []}
          onChange={(v) => set({ correspondent_id: v.length ? v : undefined })}
        />
        <TaxonomyFilter
          kind="document-types"
          label="Type"
          spaceId={spaceId}
          facets={facets?.types}
          value={query.document_type_id ?? []}
          onChange={(v) => set({ document_type_id: v.length ? v : undefined })}
        />
        <DateFilter from={query.date_from} to={query.date_to} onChange={(date_from, date_to) => set({ date_from, date_to })} />
        {fields.length > 0 && <FieldFilter fields={fields} query={query} onChange={onChange} />}
        <MoreFilters query={query} onChange={onChange} />
        {activeCount > 0 && (
          <Button size="sm" variant="ghost" onClick={() => onChange({ q: query.q, space_id: query.space_id, sort: query.sort, trash: query.trash, inbox: query.inbox })}>
            Clear filters
          </Button>
        )}
        <div className="ml-auto flex items-center gap-2">
          {found === "hybrid" || found === "semantic" ? (
            <span className="flex items-center gap-1 text-[13px] text-accent" title="Includes documents that match by meaning">
              <Sparkles className="size-3.5" /> by meaning
            </span>
          ) : null}
          {total !== undefined && <span className="text-[13px] tabular-nums text-subtle">{total.toLocaleString()}{totalCapped ? "+" : ""} document{total === 1 && !totalCapped ? "" : "s"}</span>}
          <NativeSelect value={query.sort ?? ""} onChange={(e) => set({ sort: e.target.value || undefined })} className="h-8 w-auto text-[13px]" aria-label="Sort">
            <option value="">{query.q ? "Best match" : "Newest added"}</option>
            {query.q && <option value="-added">Newest added</option>}
            <option value="added">Oldest added</option>
            <option value="-date">Document date (newest)</option>
            <option value="date">Document date (oldest)</option>
            <option value="title">Title A–Z</option>
            <option value="-updated">Recently changed</option>
            {fields.filter((f) => ["integer", "decimal", "monetary", "date", "text", "select"].includes(f.data_type)).map((f) => (
              <React.Fragment key={f.id}>
                <option value={`-cf:${f.id}`}>{f.name} (high to low)</option>
                <option value={`cf:${f.id}`}>{f.name} (low to high)</option>
              </React.Fragment>
            ))}
          </NativeSelect>
        </div>
      </div>
    </div>
  );
}

function SpaceFilter({ value, onChange }: { value?: string; onChange: (id?: string) => void }) {
  const me = useCurrentUser();
  const [open, setOpen] = React.useState(false);
  const current = me.spaces.find((s) => s.id === value);
  const options: [string | undefined, string][] = [[undefined, "All spaces"], ...me.spaces.map((s) => [s.id, spaceLabel(s)] as [string, string])];
  return (
    <FilterButton label={current ? spaceLabel(current) : "All spaces"} count={current ? 1 : 0} hideCount open={open} onOpenChange={setOpen}>
      <div className="p-1" role="listbox" aria-label="Space">
        {options.map(([id, label]) => (
          <button
            key={id ?? "all"}
            role="option"
            aria-selected={value === id}
            onClick={() => {
              onChange(id);
              setOpen(false);
            }}
            className="flex w-full items-center gap-2.5 rounded-md px-2.5 py-1.5 text-left text-sm hover:bg-surface-2"
          >
            <span className="flex-1 truncate">{label}</span>
            {value === id && <Check className="size-4 text-accent" />}
          </button>
        ))}
      </div>
    </FilterButton>
  );
}

function FilterButton({ label, count, hideCount, open, onOpenChange, children }: { label: string; count: number; hideCount?: boolean; open?: boolean; onOpenChange?: (o: boolean) => void; children: React.ReactNode }) {
  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger asChild>
        <button
          className={cn(
            "flex h-8 items-center gap-1.5 rounded-md border px-2.5 text-[13px] transition-colors",
            count ? "border-accent/40 bg-accent-soft text-accent-soft-fg" : "border-border bg-surface text-muted hover:text-fg",
          )}
        >
          {label}
          {count > 0 && !hideCount && <span className="rounded-full bg-accent px-1.5 text-[11px] font-semibold text-accent-fg">{count}</span>}
          <ChevronDown className="size-3.5" />
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-72 p-0">{children}</PopoverContent>
    </Popover>
  );
}

function TaxonomyFilter({ kind, label, spaceId, facets, value, onChange }: { kind: TaxonomyKind; label: string; spaceId?: string; facets?: Record<string, number>; value: string[]; onChange: (v: string[]) => void }) {
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
              <span className={cn("text-xs tabular-nums text-subtle", facets && !facets[it.id] && !on && "opacity-50")} title={facets ? "Documents matching your current filters" : undefined}>
                {facets ? (facets[it.id] ?? 0) : it.document_count}
              </span>
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
            <DateInput value={from} onChange={(v) => onChange(v ?? undefined, to)} className="mt-1" />
          </label>
          <label className="text-xs text-muted">
            To
            <DateInput value={to} onChange={(v) => onChange(from, v ?? undefined)} className="mt-1" />
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


const ops: Record<string, [string, string][]> = {
  number: [[">", "is more than"], [">=", "is at least"], ["<", "is less than"], ["<=", "is at most"], ["=", "is exactly"]],
  date: [["<", "is before"], [">", "is after"], ["=", "is on"]],
  text: [["~", "contains"], ["=", "is"], ["!=", "is not"]],
  boolean: [["=", "is"]],
  has: [["", "has a value"]],
};

/** Filter by a custom field. It adds a term to the search box (cf:Amount>1500), so it also shows up in the URL. */
function FieldFilter({ fields, query, onChange }: { fields: CustomField[]; query: DocQuery; onChange: (q: DocQuery) => void }) {
  const [open, setOpen] = React.useState(false);
  const [fid, setFid] = React.useState(fields[0]?.id ?? "");
  const field = fields.find((f) => f.id === fid) ?? fields[0];
  const family = !field ? "text" : ["integer", "decimal", "monetary"].includes(field.data_type) ? "number" : field.data_type === "date" ? "date" : field.data_type === "boolean" ? "boolean" : "text";
  const choices = [...ops[family], ...(family === "boolean" ? [] : ops.has)];
  const [op, setOp] = React.useState(choices[0][0]);
  const [value, setValue] = React.useState("");
  const active = (query.q?.match(/cf:/g) ?? []).length;
  React.useEffect(() => setOp(choices[0][0]), [family]); // eslint-disable-line react-hooks/exhaustive-deps
  const apply = () => {
    if (!field) return;
    const term = op === "" ? `cf:"${field.name}"` : `cf:"${field.name}"${op}"${value.trim() || (family === "boolean" ? "yes" : "")}"`;
    onChange({ ...query, q: [query.q, term].filter(Boolean).join(" ") });
    setValue("");
    setOpen(false);
  };
  return (
    <FilterButton label="Fields" count={active} open={open} onOpenChange={setOpen}>
      <div className="space-y-2.5 p-3">
        <NativeSelect aria-label="Field" value={field?.id} onChange={(e) => setFid(e.target.value)}>
          {fields.map((f) => (
            <option key={f.id} value={f.id}>
              {f.name}
            </option>
          ))}
        </NativeSelect>
        <NativeSelect aria-label="Condition" value={op} onChange={(e) => setOp(e.target.value)}>
          {choices.map(([o, l]) => (
            <option key={l} value={o}>
              {l}
            </option>
          ))}
        </NativeSelect>
        {op !== "" &&
          (family === "date" ? (
            <DateInput value={value} onChange={(v) => setValue(v ?? "")} />
          ) : family === "boolean" ? (
            <NativeSelect aria-label="Value" value={value || "yes"} onChange={(e) => setValue(e.target.value)}>
              <option value="yes">Yes</option>
              <option value="no">No</option>
            </NativeSelect>
          ) : field?.options.choices?.length ? (
            <NativeSelect aria-label="Value" value={value} onChange={(e) => setValue(e.target.value)}>
              <option value="">Choose…</option>
              {field.options.choices.map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </NativeSelect>
          ) : (
            <Input aria-label="Value" inputMode={family === "number" ? "decimal" : undefined} value={value} onChange={(e) => setValue(e.target.value)} placeholder="Value" onKeyDown={(e) => e.key === "Enter" && apply()} />
          ))}
        <Button variant="primary" size="sm" className="w-full" disabled={op !== "" && !value.trim() && family !== "boolean"} onClick={apply}>
          <ListFilter /> Add filter
        </Button>
        <p className="text-xs text-subtle">Added to the search box. Remove it there to clear it.</p>
      </div>
    </FilterButton>
  );
}
