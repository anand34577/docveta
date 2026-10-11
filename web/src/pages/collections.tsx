import * as React from "react";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Check, Library, MoreHorizontal, Pencil, Plus, Search, Trash2, Users } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { invalidateDocuments, useCollections, useDocuments } from "@/lib/queries";
import type { Collection, DocQuery } from "@/lib/types";
import { cn, formatDocDate, tagDot, timeAgo } from "@/lib/utils";
import { PageHeader } from "@/components/app-shell";
import { CollectionCovers, CollectionDialog, useSharedLabel } from "@/components/collections";
import { Thumbnail } from "@/components/documents/doc-items";
import { DocumentBrowser } from "./documents";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, EmptyState, Skeleton, Spinner } from "@/components/ui/misc";
import { Dialog, DialogContent, DialogFooter, DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/overlay";
import { confirm } from "@/components/ui/confirm";
import { NotFound } from "./not-found";

const ideas = ["Car papers", "House", "Medical", "Travel", "Education", "Tax 2025–26"];

export function CollectionsPage() {
  const list = useCollections();
  const navigate = useNavigate();
  const shared = useSharedLabel();
  const [creating, setCreating] = React.useState<string | null>(null);
  const [filter, setFilter] = React.useState("");
  const shown = (list.data ?? []).filter((c) => (c.name + " " + c.description).toLowerCase().includes(filter.trim().toLowerCase()));

  return (
    <div className="pb-12">
      <PageHeader
        title="Collections"
        description="Related documents kept together, whatever their type or space: everything for the car, the house, a trip."
        actions={
          <Button size="sm" variant="primary" onClick={() => setCreating("")}>
            <Plus /> New collection
          </Button>
        }
      />
      <div className="page-x">
        {(list.data?.length ?? 0) > 8 && (
          <div className="relative mb-5 max-w-sm">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-subtle" />
            <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Find a collection" className="pl-9" aria-label="Find a collection" />
          </div>
        )}
        {list.isLoading ? (
          <div className="grid grid-cols-[repeat(auto-fill,minmax(220px,1fr))] gap-4">
            {Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-60 rounded-xl" />)}
          </div>
        ) : !list.data?.length ? (
          <Card>
            <EmptyState icon={<Library />} title="No collections yet" className="py-14"
              action={
                <div className="flex max-w-md flex-wrap justify-center gap-2">
                  {ideas.map((n) => (
                    <button key={n} type="button" onClick={() => setCreating(n)}
                      className="rounded-full border border-border px-3 py-1.5 text-sm transition-colors hover:border-accent hover:bg-accent-soft hover:text-accent-soft-fg">
                      <Plus className="mr-1 inline size-3.5" />{n}
                    </button>
                  ))}
                </div>
              }>
              Put the documents that belong together in one place: the RC, insurance and service bills of your car, or every paper for the house. Start with one of these:
            </EmptyState>
          </Card>
        ) : shown.length === 0 ? (
          <p className="py-10 text-center text-sm text-muted">No collection matches “{filter}”.</p>
        ) : (
          <div className="grid grid-cols-[repeat(auto-fill,minmax(220px,1fr))] gap-4">
            {shown.map((c) => (
              <Link key={c.id} to="/collections/$id" params={{ id: c.id }}
                className="group flex flex-col overflow-hidden rounded-xl border border-border bg-surface shadow-sm transition hover:-translate-y-0.5 hover:border-border-strong hover:shadow-md focus-visible:-translate-y-0.5">
                <CollectionCovers c={c} className="aspect-[4/3] w-full" />
                <div className="flex flex-1 flex-col gap-1 border-t border-border p-3.5">
                  <div className="flex items-center gap-2">
                    <span className={cn("size-2.5 shrink-0 rounded-full", tagDot[c.color])} aria-hidden />
                    <h3 className="truncate text-sm font-semibold">{c.name}</h3>
                  </div>
                  {c.description && <p className="line-clamp-2 text-xs text-muted">{c.description}</p>}
                  <div className="mt-auto flex items-center gap-1.5 pt-1 text-xs text-subtle">
                    {c.space_id && <Users className="size-3.5" />}
                    <span className="truncate">{c.document_count} document{c.document_count === 1 ? "" : "s"} · {shared(c)}</span>
                  </div>
                </div>
              </Link>
            ))}
            <button type="button" onClick={() => setCreating("")}
              className="flex min-h-60 flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed border-border text-sm text-muted transition-colors hover:border-accent hover:text-accent">
              <Plus className="size-6" /> New collection
            </button>
          </div>
        )}
      </div>
      {creating !== null && <CollectionDialog initialName={creating} onClose={() => setCreating(null)} onSaved={(c) => navigate({ to: "/collections/$id", params: { id: c.id } })} />}
    </div>
  );
}

export function CollectionPage() {
  const { id } = useParams({ from: "/app/collections/$id" });
  const list = useCollections();
  const c = list.data?.find((x) => x.id === id);
  const [filters, setFilters] = React.useState<DocQuery>({});
  const [editing, setEditing] = React.useState(false);
  const [adding, setAdding] = React.useState(false);
  const shared = useSharedLabel();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const query = React.useMemo(() => ({ ...filters, collection_id: id }), [filters, id]);

  if (list.isLoading) return <div className="flex justify-center py-20"><Spinner /></div>;
  if (!c) return <NotFound />;

  const remove = async () => {
    if (!(await confirm({ title: `Delete “${c.name}”?`, body: "Only the collection goes. Its documents stay in your library.", confirmLabel: "Delete collection", destructive: true }))) return;
    try {
      await api.del(`/collections/${c.id}`);
      qc.invalidateQueries({ queryKey: ["collections"] });
      toast.success(`Deleted ${c.name}`);
      navigate({ to: "/collections" });
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  return (
    <>
      <DocumentBrowser
        query={query}
        onChange={({ collection_id: _c, ...rest }) => { void _c; setFilters(rest); }}
        title={c.name}
        eyebrow={
          <Link to="/collections" className="inline-flex items-center gap-1.5 hover:text-fg">
            <span className={cn("size-2 rounded-full", tagDot[c.color])} aria-hidden /> Collection · {shared(c)} · updated {timeAgo(c.updated_at)}
          </Link>
        }
        description={c.description || undefined}
        collection={c}
        headerActions={
          c.can_edit && (
            <>
              <Button size="sm" variant="primary" onClick={() => setAdding(true)}>
                <Plus /> Add documents
              </Button>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button size="icon-sm" variant="ghost" aria-label="Collection options">
                    <MoreHorizontal />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent>
                  <DropdownMenuItem onSelect={() => setEditing(true)}>
                    <Pencil /> Edit name, colour, sharing
                  </DropdownMenuItem>
                  <DropdownMenuItem destructive onSelect={remove}>
                    <Trash2 /> Delete collection
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </>
          )
        }
        empty={
          <EmptyState icon={<Library />} title="Nothing in this collection yet"
            action={c.can_edit && <Button variant="primary" onClick={() => setAdding(true)}><Plus /> Add documents</Button>}>
            Add documents here, or select documents anywhere in your library and choose ⋯ → Add to collection.
          </EmptyState>
        }
      />
      {editing && <CollectionDialog collection={c} onClose={() => setEditing(false)} />}
      {adding && <AddDocumentsDialog collection={c} onClose={() => setAdding(false)} />}
    </>
  );
}

/** Search the library and tick documents to put in the collection. */
function AddDocumentsDialog({ collection, onClose }: { collection: Collection; onClose: () => void }) {
  const [q, setQ] = React.useState("");
  const deferred = React.useDeferredValue(q.trim());
  const results = useDocuments({ q: deferred || undefined, sort: deferred ? undefined : "-added" });
  const inside = useDocuments({ collection_id: collection.id });
  const already = React.useMemo(() => new Set(inside.data?.pages.flatMap((p) => p.items.map((d) => d.id)) ?? []), [inside.data]);
  const items = results.data?.pages.flatMap((p) => p.items) ?? [];
  const [picked, setPicked] = React.useState<Set<string>>(new Set());
  const [busy, setBusy] = React.useState(false);
  const qc = useQueryClient();
  const toggle = (id: string) => setPicked((s) => { const n = new Set(s); if (n.has(id)) n.delete(id); else n.add(id); return n; });
  const add = async () => {
    setBusy(true);
    try {
      const r = await api.post<{ count: number }>(`/collections/${collection.id}/documents`, { ids: [...picked] });
      qc.invalidateQueries({ queryKey: ["collections"] });
      invalidateDocuments(qc);
      toast.success(`Added ${r.count} to ${collection.name}`);
      onClose();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`Add to ${collection.name}`} description="Search by words in the document, its title, sender or tags." size="lg">
        <div className="relative mb-3">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-subtle" />
          <Input autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder="e.g. insurance, from:hdfc, tag:car" className="pl-9" aria-label="Search documents" />
        </div>
        <div className="-mx-2 h-[min(24rem,50vh)] overflow-y-auto scrollbar-thin" role="listbox" aria-multiselectable aria-label="Documents">
          {results.isLoading ? (
            <div className="space-y-2 px-2">{Array.from({ length: 5 }).map((_, i) => <Skeleton key={i} className="h-14" />)}</div>
          ) : items.length === 0 ? (
            <p className="py-10 text-center text-sm text-muted">No documents match.</p>
          ) : (
            items.map((d) => {
              const isIn = already.has(d.id);
              const on = picked.has(d.id);
              return (
                <button key={d.id} type="button" role="option" aria-selected={on || isIn} disabled={isIn} onClick={() => toggle(d.id)}
                  className={cn("flex w-full items-center gap-3 rounded-lg px-2 py-2 text-left transition-colors", on ? "bg-accent-soft" : "hover:bg-surface-2", isIn && "opacity-60")}>
                  <span className={cn("flex size-[18px] shrink-0 items-center justify-center rounded-[5px] border", on || isIn ? "border-accent bg-accent text-accent-fg" : "border-border-strong")}>
                    {(on || isIn) && <Check className="size-3" strokeWidth={3} />}
                  </span>
                  <Thumbnail doc={d} className="h-11 w-9 shrink-0 rounded border border-border" />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-medium">{d.title}</span>
                    <span className="block truncate text-xs text-muted">
                      {isIn ? "Already in this collection" : [d.correspondent?.name, d.document_type?.name, formatDocDate(d.document_date)].filter(Boolean).join(" · ")}
                    </span>
                  </span>
                </button>
              );
            })
          )}
          {results.hasNextPage && (
            <div className="p-2 text-center">
              <Button size="sm" variant="ghost" loading={results.isFetchingNextPage} onClick={() => results.fetchNextPage()}>Show more</Button>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={!picked.size} loading={busy} onClick={add}>
            Add {picked.size || ""} document{picked.size === 1 ? "" : "s"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
