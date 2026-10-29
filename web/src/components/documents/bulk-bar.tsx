import * as React from "react";
import { useQueryClient } from "@tanstack/react-query";
import { CheckCheck, Combine, Download, FolderInput, FolderMinus, Library, MoreHorizontal, RotateCcw, RotateCw, Sparkles, Tag, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { api, downloadZip, errorMessage, ZIP_MAX } from "@/lib/api";
import { invalidateDocuments, useAIEnabled, useTaxonomy } from "@/lib/queries";
import { useSelection } from "@/stores/ui";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/overlay";
import { Field, Input, Select } from "@/components/ui/input";
import { SuggestInput } from "@/components/ui/suggest-input";
import { Checkbox, TagChip, useScrollEdges } from "@/components/ui/misc";
import { confirm } from "@/components/ui/confirm";
import { useCurrentUser } from "@/components/app-shell";
import { formatCount, overlayOpen, spaceLabel } from "@/lib/utils";
import type { Collection, Document } from "@/lib/types";
import { AddToCollectionDialog } from "@/components/collections";

interface BulkResult {
  succeeded: number;
  failed: { id: string; message: string }[];
}

/** Documents per request: each takes a few milliseconds, and a request must finish before a proxy gives up on it. */
const BULK_CHUNK = 500;

/** Runs a bulk action a chunk at a time, saying how far it got when there are many. */
async function bulkInChunks(ids: string[], action: string, update: Record<string, unknown>, progress?: string | number): Promise<BulkResult> {
  const res: BulkResult = { succeeded: 0, failed: [] };
  for (let i = 0; i < ids.length; i += BULK_CHUNK) {
    const r = await api.post<BulkResult>("/documents/bulk", { ids: ids.slice(i, i + BULK_CHUNK), action, update });
    res.succeeded += r.succeeded;
    res.failed.push(...r.failed);
    if (progress !== undefined) toast.loading(`${Math.min(i + BULK_CHUNK, ids.length).toLocaleString()} of ${ids.length.toLocaleString()} done…`, { id: progress });
  }
  return res;
}

/** Floating action bar for selected documents. */
export function BulkBar({ items, trash, collection }: { items: Document[]; trash?: boolean; collection?: Collection }) {
  const { ids, clear, set } = useSelection();
  const qc = useQueryClient();
  const [dialog, setDialog] = React.useState<null | "tags" | "move" | "merge" | "collect">(null);
  const [busy, setBusy] = React.useState(false);
  const bar = useScrollEdges<HTMLDivElement>();
  const selected = items.filter((d) => ids.has(d.id));
  const ai = useAIEnabled().data;
  const n = ids.size;

  React.useEffect(() => () => clear(), [clear]);
  React.useEffect(() => {
    // Esc clears the selection, unless it's closing one of the bar's dialogs or a menu.
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && n > 0 && !dialog && !overlayOpen() && clear();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [n, clear, dialog]);

  if (n === 0) return null;

  const run = async (action: string, update?: Record<string, unknown>, success?: string) => {
    setBusy(true);
    const chosen = [...ids];
    const progress = chosen.length > BULK_CHUNK ? toast.loading("Working…") : undefined;
    try {
      const res = await bulkInChunks(chosen, action, update ?? {}, progress);
      if (progress !== undefined) toast.dismiss(progress);
      invalidateDocuments(qc);
      // Moving to Trash is the bulk action that's easiest to hit by mistake: offer the way back.
      const undo =
        action === "trash" && res.succeeded > 0
          ? {
              label: "Undo",
              onClick: () => {
                const failed = new Set(res.failed.map((f) => f.id));
                bulkInChunks(chosen.filter((id) => !failed.has(id)), "restore", {}).then(
                  () => invalidateDocuments(qc),
                  (e) => toast.error(errorMessage(e)),
                );
              },
            }
          : undefined;
      if (res.failed.length) toast.warning(`${res.succeeded} done, ${res.failed.length} failed`, { description: res.failed[0].message, action: undo });
      else toast.success(success ?? `Updated ${res.succeeded} document${res.succeeded === 1 ? "" : "s"}`, { action: undo });
      clear();
      setDialog(null);
    } catch (e) {
      // Chunks done before the failure stay done: show them.
      toast.error(errorMessage(e), { id: progress });
      invalidateDocuments(qc);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <div ref={bar} className="fixed inset-x-3 bottom-20 z-40 mx-auto flex max-w-2xl items-center gap-1 scroll-x rounded-xl border border-border bg-surface p-1.5 shadow-lg animate-pop lg:bottom-6">
        <Button size="sm" variant="ghost" onClick={clear} aria-label="Clear selection">
          <X />
        </Button>
        <span className="whitespace-nowrap px-1 text-sm font-medium">{formatCount(n)} selected</span>
        {items.length > n && (
          <Button size="sm" variant="link" className="px-2" onClick={() => set(items.map((d) => d.id))}>
            Select all {items.length}
          </Button>
        )}
        <div className="flex-1" />
        {trash ? (
          <>
            <Button size="sm" variant="ghost" loading={busy} onClick={() => run("restore", undefined, "Restored")}>
              <RotateCcw /> Restore
            </Button>
            <Button
              size="sm"
              variant="danger-ghost"
              onClick={async () => {
                if (await confirm({ title: `Delete ${n} document${n > 1 ? "s" : ""} forever?`, body: "This can't be undone.", confirmLabel: "Delete forever", destructive: true }))
                  void run("purge", undefined, "Deleted permanently");
              }}
            >
              <Trash2 /> Delete forever
            </Button>
          </>
        ) : (
          <>
            <Button size="sm" variant="ghost" loading={busy} onClick={() => run("update", { inbox: false }, "Marked as reviewed")}>
              <CheckCheck /> <span className="hidden sm:inline">Reviewed</span>
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setDialog("tags")}>
              <Tag /> <span className="hidden sm:inline">Tags</span>
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setDialog("move")}>
              <FolderInput /> <span className="hidden sm:inline">Move</span>
            </Button>
            {n > 1 && selected.every((d) => d.mime_type === "application/pdf" || d.mime_type.startsWith("image/")) && (
              <Button size="sm" variant="ghost" onClick={() => setDialog("merge")} title="Combine into one PDF, in the order selected">
                <Combine /> <span className="hidden sm:inline">Merge</span>
              </Button>
            )}
            {n === 1 ? (
              <Button size="sm" variant="ghost" asChild>
                <a href={`/api/v1/documents/${[...ids][0]}/file?kind=original&download=1`} aria-label="Download" title="Download">
                  <Download />
                </a>
              </Button>
            ) : (
              <Button
                size="sm"
                variant="ghost"
                aria-label={`Download ${n} documents as a ZIP file`}
                title="Download as one ZIP file"
                onClick={() => {
                  if (n > ZIP_MAX) return void toast.error(`Choose up to ${ZIP_MAX} documents for one download.`);
                  downloadZip([...ids]);
                  toast.success(`Preparing a ZIP of ${n} documents`, { description: "The download starts in a moment." });
                }}
              >
                <Download /> <span className="hidden sm:inline">ZIP</span>
              </Button>
            )}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button size="sm" variant="ghost" aria-label="More actions" title="More actions">
                  <MoreHorizontal />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent>
                <DropdownMenuItem onSelect={() => setDialog("collect")}>
                  <Library /> Add to collection
                </DropdownMenuItem>
                {collection?.can_edit && (
                  <DropdownMenuItem
                    onSelect={async () => {
                      try {
                        const r = await api.post<{ count: number }>(`/collections/${collection.id}/documents/remove`, { ids: [...ids] });
                        invalidateDocuments(qc);
                        qc.invalidateQueries({ queryKey: ["collections"] });
                        toast.success(`Took ${r.count} out of ${collection.name}`, { description: "The documents themselves are still in your library." });
                        clear();
                      } catch (e) {
                        toast.error(errorMessage(e));
                      }
                    }}
                  >
                    <FolderMinus /> Remove from {collection.name}
                  </DropdownMenuItem>
                )}
                {ai?.chat && (
                  <DropdownMenuItem onSelect={() => run("suggest", undefined, `Asked AI to suggest tags, type and sender for ${n} document${n === 1 ? "" : "s"}. Suggestions appear on each document.`)}>
                    <Sparkles /> Suggest tags & type with AI
                  </DropdownMenuItem>
                )}
                <DropdownMenuItem
                  onSelect={async () => {
                    if (await confirm({ title: `Process ${n} document${n === 1 ? "" : "s"} again?`, body: "Text is read again from the original file, then tags and details are suggested again.", confirmLabel: "Process again" }))
                      void run("reprocess", undefined, `Processing ${n} document${n === 1 ? "" : "s"} again`);
                  }}
                >
                  <RotateCw /> Process again
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <Button size="sm" variant="danger-ghost" loading={busy} onClick={() => run("trash", undefined, `Moved ${n} to Trash`)}>
              <Trash2 /> <span className="hidden sm:inline">Delete</span>
            </Button>
          </>
        )}
      </div>
      {dialog === "tags" && <TagsDialog busy={busy} onClose={() => setDialog(null)} onApply={(add, remove) => run("update", { add_tag_names: add, remove_tag_names: remove })} />}
      {dialog === "merge" && <MergeDialog ids={[...ids]} titleHint={selected[0]?.title ?? ""} onClose={() => setDialog(null)} onDone={() => { invalidateDocuments(qc); clear(); setDialog(null); }} />}
      {dialog === "collect" && <AddToCollectionDialog ids={[...ids]} onClose={() => setDialog(null)} onDone={() => { clear(); setDialog(null); }} />}
      {dialog === "move" && <MoveDialog busy={busy} onClose={() => setDialog(null)} onApply={(space) => run("update", { space_id: space }, "Moved")} />}
    </>
  );
}

/** Tag names work across spaces: each document's own space gets the tag (created if missing). */
function TagsDialog({ busy, onClose, onApply }: { busy: boolean; onClose: () => void; onApply: (add: string[], remove: string[]) => void }) {
  const [add, setAdd] = React.useState<string[]>([]);
  const [remove, setRemove] = React.useState<string[]>([]);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title="Change tags" description="Works across spaces: each document gets the tag in its own space." size="sm">
        <div className="space-y-4">
          <TagNames label="Add tags" value={add} onChange={setAdd} placeholder="Type a tag and press Enter" />
          <TagNames label="Remove tags" value={remove} onChange={setRemove} placeholder="Type a tag to remove" />
        </div>
        <DialogFooter>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" loading={busy} disabled={!add.length && !remove.length} onClick={() => onApply(add, remove)}>
            Apply
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function TagNames({ label, value, onChange, placeholder }: { label: string; value: string[]; onChange: (v: string[]) => void; placeholder: string }) {
  const [text, setText] = React.useState("");
  const all = useTaxonomy("tags");
  const names = [...new Set((all.data ?? []).map((t) => t.name))].sort((x, y) => x.localeCompare(y));
  const commit = (picked?: string) => {
    const n = (picked ?? text).trim().replace(/,$/, "").trim();
    if (n && !value.some((v) => v.toLowerCase() === n.toLowerCase())) onChange([...value, n]);
    setText("");
  };
  return (
    <div>
      <div className="mb-1.5 text-[13px] font-medium">{label}</div>
      {value.length > 0 && (
        <div className="mb-2 flex flex-wrap gap-1">
          {value.map((v) => (
            <TagChip key={v} name={v} onRemove={() => onChange(value.filter((x) => x !== v))} />
          ))}
        </div>
      )}
      <SuggestInput
        suggestions={names.filter((n) => !value.some((v) => v.toLowerCase() === n.toLowerCase()))}
        onPick={commit}
        value={text}
        placeholder={placeholder}
        onChange={(e) => (e.target.value.endsWith(",") ? commit(e.target.value) : setText(e.target.value))}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            commit();
          }
        }}
        onBlur={() => commit()}
      />
    </div>
  );
}

/** Combine the selected documents into one new PDF, in the order they were selected. */
function MergeDialog({ ids, titleHint, onClose, onDone }: { ids: string[]; titleHint: string; onClose: () => void; onDone: () => void }) {
  const [title, setTitle] = React.useState(`${titleHint} (merged)`);
  const [trash, setTrash] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  const go = async () => {
    setBusy(true);
    try {
      await api.post("/documents/merge", { ids, title: title.trim(), trash_originals: trash });
      toast.success(`Merged ${ids.length} documents`);
      onDone();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`Merge ${ids.length} documents`} description="Creates one new PDF with all their pages, one after another." size="sm">
        <div className="space-y-4">
          <Field label="Title of the new document" htmlFor="merge-title">
            <Input id="merge-title" value={title} onChange={(e) => setTitle(e.target.value)} autoFocus />
          </Field>
          <label className="flex items-center gap-2 text-sm">
            <Checkbox checked={trash} onCheckedChange={(v) => setTrash(v === true)} /> Move the originals to Trash afterwards
          </label>
        </div>
        <DialogFooter>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" loading={busy} disabled={!title.trim()} onClick={go}>
            Merge
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function MoveDialog({ busy, onClose, onApply }: { busy: boolean; onClose: () => void; onApply: (spaceId: string) => void }) {
  const me = useCurrentUser();
  const writable = me.spaces.filter((s) => s.role !== "viewer");
  const [space, setSpace] = React.useState(writable[0]?.id ?? "");
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title="Move to another space" description="Tags, correspondent and type are matched by name in the new space (created if missing)." size="sm">
        <Select value={space} onChange={(e) => setSpace(e.target.value)} aria-label="Space to move to">
          {writable.map((s) => (
            <option key={s.id} value={s.id}>
              {spaceLabel(s)}
            </option>
          ))}
        </Select>
        <DialogFooter>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" loading={busy} onClick={() => onApply(space)}>
            Move
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
