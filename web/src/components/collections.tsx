import * as React from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Check, Library, Plus, Search, X } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError, errorMessage } from "@/lib/api";
import { invalidateDocuments, useCollections } from "@/lib/queries";
import type { Collection } from "@/lib/types";
import { cn, colorNames, formatCount, spaceLabel, tagColors, tagDot } from "@/lib/utils";
import { useCurrentUser } from "@/components/app-shell";
import { Button } from "@/components/ui/button";
import { Field, Input, Select, Textarea } from "@/components/ui/input";
import { Dialog, DialogContent, DialogFooter } from "@/components/ui/overlay";
import { Skeleton } from "@/components/ui/misc";

/** "Only you" or "Shared with Family". */
export function useSharedLabel() {
  const me = useCurrentUser();
  return (c: Collection) => {
    const s = c.space_id ? me.spaces.find((x) => x.id === c.space_id) : undefined;
    return s ? `Shared with ${spaceLabel(s)}` : "Only you";
  };
}

/** Up to four thumbnails of what's inside, as a 2×2 cover. */
export function CollectionCovers({ c, className }: { c: Collection; className?: string }) {
  const covers = c.covers.slice(0, 4);
  if (!covers.length) {
    return (
      <div className={cn("flex items-center justify-center", tagColors[c.color] ?? tagColors.blue, className)}>
        <Library className="size-8 opacity-70" />
      </div>
    );
  }
  return (
    <div className={cn("grid gap-0.5 overflow-hidden bg-border", covers.length > 1 ? "grid-cols-2" : "grid-cols-1", covers.length > 2 ? "grid-rows-2" : "grid-rows-1", className)}>
      {covers.map((id, i) => (
        <div key={id} className={cn("relative min-h-0 overflow-hidden bg-white", covers.length === 3 && i === 0 && "row-span-2")}>
          <img src={`/api/v1/documents/${id}/thumbnail`} alt="" loading="lazy" decoding="async" className="absolute inset-0 size-full object-cover object-top"
            onError={(e) => (e.currentTarget.style.visibility = "hidden")} />
        </div>
      ))}
    </div>
  );
}

/** Create or edit a collection: name, what it's for, colour, and who sees it. */
export function CollectionDialog({ collection, initialName, onClose, onSaved }: { collection?: Collection; initialName?: string; onClose: () => void; onSaved?: (c: Collection) => void }) {
  const me = useCurrentUser();
  const qc = useQueryClient();
  const [name, setName] = React.useState(collection?.name ?? initialName ?? "");
  const [description, setDescription] = React.useState(collection?.description ?? "");
  const [color, setColor] = React.useState(collection?.color ?? "blue");
  const [space, setSpace] = React.useState(collection?.space_id ?? "");
  const [err, setErr] = React.useState<ApiError | null>(null);
  const [busy, setBusy] = React.useState(false);
  const shareable = me.spaces.filter((s) => s.kind === "shared" && s.role !== "viewer");
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const body = { name, description, color, space_id: space || "00000000-0000-0000-0000-000000000000" };
      const c = collection ? await api.patch<Collection>(`/collections/${collection.id}`, body) : await api.post<Collection>("/collections", body);
      qc.invalidateQueries({ queryKey: ["collections"] });
      onSaved?.(c);
      onClose();
    } catch (e) {
      setErr(e as ApiError);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={collection ? "Edit collection" : "New collection"} description={collection ? undefined : "Keep related documents together, like all the papers for your car or your house."} size="sm">
        <form onSubmit={submit} className="space-y-4">
          <Field label="Name" htmlFor="col-name" error={err?.fieldError("name")}>
            <Input id="col-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Car papers, House, Medical" required autoFocus maxLength={80} />
          </Field>
          <Field label="Description" htmlFor="col-desc" hint="Optional. What belongs here." error={err?.fieldError("description")}>
            <Textarea id="col-desc" rows={2} value={description} maxLength={500} onChange={(e) => setDescription(e.target.value)} placeholder="RC, insurance, pollution certificate, service bills" />
          </Field>
          <Field label="Colour">
            <div className="flex flex-wrap gap-2" role="radiogroup" aria-label="Colour">
              {colorNames.filter((c) => c !== "lime").map((c) => (
                <button key={c} type="button" role="radio" aria-checked={color === c} aria-label={c} title={c} onClick={() => setColor(c)}
                  className={cn("flex size-7 items-center justify-center rounded-full ring-offset-2 ring-offset-surface transition", tagDot[c], color === c && "ring-2 ring-fg")}>
                  {color === c && <Check className="size-3.5 text-white" strokeWidth={3} />}
                </button>
              ))}
            </div>
          </Field>
          {(shareable.length > 0 || collection?.space_id) && (
            <Field label="Who sees it" htmlFor="col-space" hint="People in a space see the documents in it they can open; its editors can add and remove.">
              <Select id="col-space" value={space} onChange={(e) => setSpace(e.target.value)}>
                <option value="">Only me</option>
                {shareable.map((s) => (
                  <option key={s.id} value={s.id}>Everyone in {spaceLabel(s)}</option>
                ))}
              </Select>
            </Field>
          )}
          {err && !err.fields.length && <p className="text-sm text-danger">{err.message}</p>}
          <DialogFooter>
            <Button onClick={onClose}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy}>{collection ? "Save" : "Create collection"}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** Pick a collection (or make one) for some documents. */
export function AddToCollectionDialog({ ids, onClose, onDone }: { ids: string[]; onClose: () => void; onDone?: () => void }) {
  const list = useCollections();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const shared = useSharedLabel();
  const [filter, setFilter] = React.useState("");
  const [creating, setCreating] = React.useState(false);
  const [busy, setBusy] = React.useState<string | null>(null);
  const editable = (list.data ?? []).filter((c) => c.can_edit && c.name.toLowerCase().includes(filter.trim().toLowerCase()));
  const add = async (c: Collection) => {
    setBusy(c.id);
    try {
      const r = await api.post<{ count: number }>(`/collections/${c.id}/documents`, { ids });
      qc.invalidateQueries({ queryKey: ["collections"] });
      invalidateDocuments(qc);
      const already = ids.length - r.count;
      toast.success(r.count ? `Added ${r.count} to ${c.name}` : `Already in ${c.name}`, {
        description: r.count && already > 0 ? `${already} ${already === 1 ? "was" : "were"} in it already.` : undefined,
        action: { label: "Open", onClick: () => navigate({ to: "/collections/$id", params: { id: c.id } }) },
      });
      onDone?.();
      onClose();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(null);
    }
  };
  if (creating) return <CollectionDialog initialName={filter.trim()} onClose={() => setCreating(false)} onSaved={add} />;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`Add ${ids.length === 1 ? "to a collection" : `${ids.length} documents to a collection`}`} size="sm">
        {(list.data?.length ?? 0) > 6 && (
          <div className="relative mb-3">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-subtle" />
            <Input autoFocus value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Find a collection" className="pl-9" />
          </div>
        )}
        <div className="-mx-2 max-h-80 space-y-0.5 overflow-y-auto scrollbar-thin">
          {list.isLoading && <Skeleton className="mx-2 h-12" />}
          {editable.map((c) => (
            <button key={c.id} type="button" disabled={!!busy} onClick={() => add(c)}
              className="flex w-full items-center gap-3 rounded-lg px-2 py-2 text-left transition-colors hover:bg-surface-2 disabled:opacity-60">
              <CollectionCovers c={c} className="size-10 shrink-0 overflow-hidden rounded-md" />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-medium">{c.name}</span>
                <span className="block truncate text-xs text-muted">{formatCount(c.document_count)} document{c.document_count === 1 ? "" : "s"} · {shared(c)}</span>
              </span>
              {busy === c.id && <span className="text-xs text-subtle">Adding…</span>}
            </button>
          ))}
          {!list.isLoading && !editable.length && (
            <p className="px-2 py-4 text-center text-sm text-muted">{filter ? "No collection by that name." : "No collections yet."}</p>
          )}
        </div>
        <DialogFooter className="sm:justify-between">
          <Button variant="ghost" onClick={() => setCreating(true)}>
            <Plus /> New collection{filter.trim() ? ` “${filter.trim()}”` : ""}
          </Button>
          <Button onClick={onClose}>Cancel</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** On a document: the collections it is in, with a way to add it to another. */
export function DocumentCollections({ docId }: { docId: string }) {
  const list = useCollections(docId);
  const qc = useQueryClient();
  const [adding, setAdding] = React.useState(false);
  const remove = async (c: Collection) => {
    try {
      await api.post(`/collections/${c.id}/documents/remove`, { ids: [docId] });
      qc.invalidateQueries({ queryKey: ["collections"] });
      invalidateDocuments(qc);
      toast.success(`Removed from ${c.name}`, {
        action: { label: "Undo", onClick: () => api.post(`/collections/${c.id}/documents`, { ids: [docId] }).then(() => qc.invalidateQueries({ queryKey: ["collections"] })) },
      });
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };
  return (
    <div>
      <div className="mb-1.5 flex items-center justify-between">
        <span className="text-[13px] font-medium">Collections</span>
        <button type="button" onClick={() => setAdding(true)} className="flex items-center gap-1 rounded px-1 py-0.5 text-xs font-medium text-accent hover:bg-accent-soft">
          <Plus className="size-3.5" /> Add
        </button>
      </div>
      {list.data?.length ? (
        <div className="flex flex-wrap gap-1.5">
          {list.data.map((c) => (
            <span key={c.id} className={cn("inline-flex max-w-full items-center gap-1 rounded-md py-0.5 pl-1.5 pr-1 text-xs font-medium", tagColors[c.color] ?? tagColors.blue)}>
              <Library className="size-3 shrink-0" />
              <Link to="/collections/$id" params={{ id: c.id }} className="truncate hover:underline">{c.name}</Link>
              {c.can_edit && (
                <button type="button" onClick={() => remove(c)} aria-label={`Remove from ${c.name}`} className="rounded opacity-60 hover:opacity-100">
                  <X className="size-3" />
                </button>
              )}
            </span>
          ))}
        </div>
      ) : (
        <p className="text-xs text-subtle">Not in a collection. Group it with related documents, like all your car papers.</p>
      )}
      {adding && <AddToCollectionDialog ids={[docId]} onClose={() => setAdding(false)} />}
    </div>
  );
}
