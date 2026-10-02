import * as React from "react";
import { Command } from "cmdk";
import { Check, ChevronsUpDown, Plus, X } from "lucide-react";
import { toast } from "sonner";
import { Popover, PopoverContent, PopoverTrigger } from "./overlay";
import { TagChip } from "./misc";
import { useCreateTaxonomy, useTaxonomy } from "@/lib/queries";
import { errorMessage } from "@/lib/api";
import { cn, colorNames, tagDot } from "@/lib/utils";
import type { TaxonomyKind } from "@/lib/types";

interface Props {
  kind: TaxonomyKind;
  spaceId: string;
  value: string[];
  onChange: (ids: string[]) => void;
  multiple?: boolean;
  placeholder?: string;
  disabled?: boolean;
  allowCreate?: boolean;
  className?: string;
  id?: string;
}

/**
 * Searchable picker for tags, correspondents and document types with inline creation:
 * type a name that doesn't exist and press Enter to create it.
 */
export function EntityPicker({ kind, spaceId, value, onChange, multiple, placeholder, disabled, allowCreate = true, className, id }: Props) {
  const [open, setOpen] = React.useState(false);
  const [search, setSearch] = React.useState("");
  const { data: items = [] } = useTaxonomy(kind, spaceId);
  const create = useCreateTaxonomy(kind);
  const selected = items.filter((i) => value.includes(i.id));
  const exact = items.some((i) => i.name.toLowerCase() === search.trim().toLowerCase());

  const toggle = (itemId: string) => {
    if (multiple) {
      onChange(value.includes(itemId) ? value.filter((v) => v !== itemId) : [...value, itemId]);
      setSearch("");
    } else {
      onChange(value[0] === itemId ? [] : [itemId]);
      setOpen(false);
    }
  };

  const doCreate = async () => {
    const name = search.trim();
    if (!name) return;
    try {
      const color = kind === "tags" ? colorNames[Math.floor(Math.random() * colorNames.length)] : undefined;
      const item = await create.mutateAsync({ space_id: spaceId, name, color });
      onChange(multiple ? [...value, item.id] : [item.id]);
      setSearch("");
      if (!multiple) setOpen(false);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  const label = { tags: "tag", correspondents: "correspondent", "document-types": "document type" }[kind];

  return (
    <Popover open={open} onOpenChange={(o) => { setOpen(o); if (!o) setSearch(""); }}>
      <PopoverTrigger asChild disabled={disabled}>
        <button
          id={id}
          type="button"
          className={cn(
            "flex min-h-10 sm:min-h-9 w-full items-center gap-1.5 rounded-md border border-border bg-surface px-2.5 py-1 text-left text-sm",
            "hover:border-border-strong focus:outline-none focus-visible:border-accent focus-visible:ring-3 focus-visible:ring-ring disabled:opacity-60",
            className,
          )}
        >
          <span className="flex flex-1 flex-wrap items-center gap-1 min-w-0">
            {selected.length === 0 && <span className="text-subtle">{placeholder ?? `Choose ${label}`}</span>}
            {multiple
              ? selected.map((s) => (
                  <TagChip
                    key={s.id}
                    name={s.name}
                    color={s.color}
                    onRemove={disabled ? undefined : () => onChange(value.filter((v) => v !== s.id))}
                  />
                ))
              : selected[0] && <span className="truncate">{selected[0].name}</span>}
          </span>
          {!multiple && selected[0] && !disabled ? (
            <span
              role="button"
              tabIndex={-1}
              aria-label="Clear"
              className="rounded p-0.5 text-subtle hover:text-fg"
              onClick={(e) => {
                e.stopPropagation();
                onChange([]);
              }}
            >
              <X className="size-3.5" />
            </span>
          ) : (
            <ChevronsUpDown className="size-3.5 shrink-0 text-subtle" />
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-[var(--radix-popover-trigger-width)] min-w-64 p-0">
        <Command loop shouldFilter>
          <Command.Input
            value={search}
            onValueChange={setSearch}
            placeholder={`Search or create ${label}…`}
            className="h-10 w-full border-b border-border bg-transparent px-3 text-sm outline-none placeholder:text-subtle"
            onKeyDown={(e) => {
              if (e.key === "Enter" && allowCreate && search.trim() && !exact) {
                const listHasMatch = items.some((i) => i.name.toLowerCase().includes(search.trim().toLowerCase()));
                if (!listHasMatch) {
                  e.preventDefault();
                  void doCreate();
                }
              }
            }}
          />
          <Command.List className="max-h-64 overflow-y-auto scrollbar-thin p-1">
            <Command.Empty className="px-3 py-4 text-center text-sm text-muted">
              {allowCreate && search.trim() ? "Press Enter to create it" : `No ${label}s yet`}
            </Command.Empty>
            {items.map((it) => (
              <Command.Item
                key={it.id}
                value={`${it.name} ${it.id}`}
                onSelect={() => toggle(it.id)}
                className="flex cursor-default items-center gap-2 rounded-md px-2.5 py-2 text-sm data-[selected=true]:bg-surface-2"
              >
                {kind === "tags" && <span className={cn("size-2 rounded-full", tagDot[it.color ?? "slate"])} />}
                <span className="flex-1 truncate">{it.name}</span>
                <span className="text-xs text-subtle">{it.document_count || ""}</span>
                {value.includes(it.id) && <Check className="size-4 text-accent" />}
              </Command.Item>
            ))}
            {allowCreate && search.trim() && !exact && (
              <Command.Item
                value={`__create__ ${search}`}
                onSelect={doCreate}
                className="flex cursor-default items-center gap-2 rounded-md px-2.5 py-2 text-sm text-accent data-[selected=true]:bg-surface-2"
              >
                <Plus className="size-4" />
                Create “{search.trim()}”
              </Command.Item>
            )}
          </Command.List>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
