import * as React from "react";
import { useQueryClient } from "@tanstack/react-query";
import { CheckCheck, Download, FolderInput, RotateCcw, Tag, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { invalidateDocuments } from "@/lib/queries";
import { useSelection } from "@/stores/ui";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/components/ui/overlay";
import { NativeSelect } from "@/components/ui/input";
import { EntityPicker } from "@/components/ui/entity-picker";
import { confirm } from "@/components/ui/confirm";
import { useCurrentUser } from "@/components/app-shell";
import type { Document } from "@/lib/types";

interface BulkResult {
  succeeded: number;
  failed: { id: string; message: string }[];
}

/** Floating action bar for selected documents. */
export function BulkBar({ items, trash }: { items: Document[]; trash?: boolean }) {
  const { ids, clear, set } = useSelection();
  const qc = useQueryClient();
  const [dialog, setDialog] = React.useState<null | "tags" | "move">(null);
  const [busy, setBusy] = React.useState(false);
  const selected = items.filter((d) => ids.has(d.id));
  const n = ids.size;

  React.useEffect(() => () => clear(), [clear]);
  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && n > 0 && clear();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [n, clear]);

  if (n === 0) return null;

  const run = async (action: string, update?: Record<string, unknown>, success?: string) => {
    setBusy(true);
    try {
      const res = await api.post<BulkResult>("/documents/bulk", { ids: [...ids], action, update: update ?? {} });
      invalidateDocuments(qc);
      if (res.failed.length) toast.warning(`${res.succeeded} done, ${res.failed.length} failed`, { description: res.failed[0].message });
      else toast.success(success ?? `Updated ${res.succeeded} document${res.succeeded === 1 ? "" : "s"}`);
      clear();
      setDialog(null);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const spaces = [...new Set(selected.map((d) => d.space.id))];

  return (
    <>
      <div className="fixed inset-x-3 bottom-20 z-40 mx-auto flex max-w-2xl items-center gap-1 overflow-x-auto rounded-xl border border-border bg-surface p-1.5 shadow-lg animate-pop lg:bottom-6">
        <Button size="sm" variant="ghost" onClick={clear} aria-label="Clear selection">
          <X />
        </Button>
        <span className="whitespace-nowrap px-1 text-sm font-medium">{n} selected</span>
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
            <Button size="sm" variant="ghost" disabled={spaces.length !== 1} onClick={() => setDialog("tags")} title={spaces.length !== 1 ? "Select documents from one space to tag them" : undefined}>
              <Tag /> <span className="hidden sm:inline">Tags</span>
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setDialog("move")}>
              <FolderInput /> <span className="hidden sm:inline">Move</span>
            </Button>
            {n === 1 && (
              <Button size="sm" variant="ghost" asChild>
                <a href={`/api/v1/documents/${[...ids][0]}/file?kind=original&download=1`}>
                  <Download />
                </a>
              </Button>
            )}
            <Button size="sm" variant="danger-ghost" loading={busy} onClick={() => run("trash", undefined, `Moved ${n} to Trash`)}>
              <Trash2 /> <span className="hidden sm:inline">Delete</span>
            </Button>
          </>
        )}
      </div>
      {dialog === "tags" && spaces.length === 1 && <TagsDialog spaceId={spaces[0]} busy={busy} onClose={() => setDialog(null)} onApply={(add, remove) => run("update", { add_tag_ids: add, remove_tag_ids: remove })} />}
      {dialog === "move" && <MoveDialog busy={busy} onClose={() => setDialog(null)} onApply={(space) => run("update", { space_id: space }, "Moved")} />}
    </>
  );
}

function TagsDialog({ spaceId, busy, onClose, onApply }: { spaceId: string; busy: boolean; onClose: () => void; onApply: (add: string[], remove: string[]) => void }) {
  const [add, setAdd] = React.useState<string[]>([]);
  const [remove, setRemove] = React.useState<string[]>([]);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title="Change tags" size="sm">
        <div className="space-y-4">
          <div>
            <div className="mb-1.5 text-[13px] font-medium">Add tags</div>
            <EntityPicker kind="tags" spaceId={spaceId} multiple value={add} onChange={setAdd} placeholder="Choose tags to add" />
          </div>
          <div>
            <div className="mb-1.5 text-[13px] font-medium">Remove tags</div>
            <EntityPicker kind="tags" spaceId={spaceId} multiple allowCreate={false} value={remove} onChange={setRemove} placeholder="Choose tags to remove" />
          </div>
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

function MoveDialog({ busy, onClose, onApply }: { busy: boolean; onClose: () => void; onApply: (spaceId: string) => void }) {
  const me = useCurrentUser();
  const writable = me.spaces.filter((s) => s.role !== "viewer");
  const [space, setSpace] = React.useState(writable[0]?.id ?? "");
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title="Move to another space" description="Tags, correspondent and type are matched by name in the new space (created if missing)." size="sm">
        <NativeSelect value={space} onChange={(e) => setSpace(e.target.value)}>
          {writable.map((s) => (
            <option key={s.id} value={s.id}>
              {s.kind === "personal" ? "Personal" : s.name}
            </option>
          ))}
        </NativeSelect>
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
