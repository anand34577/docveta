import * as React from "react";
import { useQueryClient } from "@tanstack/react-query";
import { AtSign, Copy, History, MessageSquare, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { keys, useDirectory, useHistory, useNotes, usePages } from "@/lib/queries";
import { formatDateTime, timeAgo } from "@/lib/utils";
import { Avatar, EmptyState, Skeleton } from "@/components/ui/misc";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/input";
import { findAll, squashQuery, useLocate } from "./pdf-find";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/overlay";
import { confirm } from "@/components/ui/confirm";

const mentionRe = /@\[([^\]]{1,80})\]\(([0-9a-fA-F-]{36})\)/g;

function renderBody(body: string) {
  const out: React.ReactNode[] = [];
  let last = 0;
  for (const m of body.matchAll(mentionRe)) {
    out.push(body.slice(last, m.index));
    out.push(
      <span key={m.index} className="rounded bg-accent-soft px-1 font-medium text-accent-soft-fg">
        @{m[1]}
      </span>,
    );
    last = (m.index ?? 0) + m[0].length;
  }
  out.push(body.slice(last));
  return out;
}

export function NotesTab({ id }: { id: string }) {
  const notes = useNotes(id);
  const dir = useDirectory();
  const qc = useQueryClient();
  const [body, setBody] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const ref = React.useRef<HTMLTextAreaElement>(null);

  const add = async () => {
    if (!body.trim()) return;
    setBusy(true);
    try {
      await api.post(`/documents/${id}/notes`, { body });
      setBody("");
      qc.invalidateQueries({ queryKey: keys.notes(id) });
      qc.invalidateQueries({ queryKey: keys.document(id) });
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  const remove = async (noteId: string) => {
    if (!(await confirm({ title: "Delete this note?", body: "This can't be undone.", confirmLabel: "Delete note", destructive: true }))) return;
    await api.del(`/documents/${id}/notes/${noteId}`).catch((e) => toast.error(errorMessage(e)));
    qc.invalidateQueries({ queryKey: keys.notes(id) });
    qc.invalidateQueries({ queryKey: keys.document(id) }); // the count on the Notes tab
  };
  const mention = (name: string, uid: string) => {
    const el = ref.current;
    const pos = el?.selectionStart ?? body.length;
    const text = `@[${name}](${uid}) `;
    setBody(body.slice(0, pos) + text + body.slice(pos));
    requestAnimationFrame(() => el?.focus());
  };

  return (
    <div className="space-y-4">
      <div className="rounded-lg border border-border bg-surface focus-within:border-accent focus-within:ring-3 focus-within:ring-ring">
        <Textarea
          ref={ref}
          value={body}
          onChange={(e) => setBody(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) void add();
          }}
          placeholder="Add a note… (only people with access to this space can see it)"
          className="border-0 focus:ring-0"
          rows={3}
        />
        <div className="flex items-center justify-between px-2 pb-2">
          <Popover>
            <PopoverTrigger asChild>
              <Button size="sm" variant="ghost">
                <AtSign /> Mention
              </Button>
            </PopoverTrigger>
            <PopoverContent className="max-h-64 w-56 overflow-y-auto p-1">
              {(dir.data ?? []).map((u) => (
                <button key={u.id} onClick={() => mention(u.display_name, u.id)} className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-surface-2">
                  <Avatar name={u.display_name} className="size-6 text-[10px]" />
                  <span className="truncate">{u.display_name}</span>
                </button>
              ))}
            </PopoverContent>
          </Popover>
          <Button size="sm" variant="primary" loading={busy} disabled={!body.trim()} onClick={add}>
            Add note
          </Button>
        </div>
      </div>
      {notes.isLoading ? (
        <Skeleton className="h-16" />
      ) : (notes.data ?? []).length === 0 ? (
        <EmptyState icon={<MessageSquare />} title="No notes yet" className="py-8">
          Notes are great for reminders like “claim submitted on 12 March”.
        </EmptyState>
      ) : (
        <ul className="space-y-4">
          {notes.data!.map((n) => (
            <li key={n.id} className="group flex gap-3">
              <Avatar name={n.author?.name ?? "?"} className="size-7 text-[10px]" />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 text-[13px]">
                  <span className="font-medium">{n.author?.name ?? "Former member"}</span>
                  <span className="text-subtle" title={formatDateTime(n.created_at)}>
                    {timeAgo(n.created_at)}
                  </span>
                  {n.can_delete && (
                    <button onClick={() => remove(n.id)} className="ml-auto rounded p-1 text-subtle opacity-0 hover:text-danger focus-visible:opacity-100 group-hover:opacity-100 [@media(hover:none)]:opacity-100" aria-label="Delete note">
                      <Trash2 className="size-3.5" />
                    </button>
                  )}
                </div>
                <p className="mt-0.5 whitespace-pre-wrap text-sm leading-relaxed">{renderBody(n.body)}</p>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

const actionLabels: Record<string, string> = {
  created: "Uploaded",
  updated: "Edited",
  trashed: "Moved to Trash",
  restored: "Restored from Trash",
  ocr_queued: "Waiting for text recognition",
  text_extracted: "Text recognised",
  auto_classified: "Organised automatically",
  archive_created: "Searchable PDF created",
  processing_failed: "Processing failed",
  reprocess_requested: "Reprocessing requested",
  asn_assigned: "Archive number assigned",
};

function describe(action: string, d: Record<string, unknown>): string | null {
  switch (action) {
    case "updated":
      return Object.keys(d)
        .map((k) => k.replace(/_ids?$/, "").replace(/_/g, " "))
        .join(", ");
    case "text_extracted":
      return `${d.pages ?? "?"} page(s) via ${d.engine ?? "?"}`;
    case "auto_classified":
      return Object.entries(d)
        .map(([k, v]) => `${k.replace("_", " ")}: ${Array.isArray(v) ? v.join(", ") : v}`)
        .join(" · ");
    case "processing_failed":
      return String(d.error ?? "");
    case "created":
      return d.filename ? String(d.filename) : null;
  }
  return null;
}

export function HistoryTab({ id }: { id: string }) {
  const h = useHistory(id, true);
  if (h.isLoading) return <Skeleton className="h-32" />;
  if (!h.data?.length) return <EmptyState icon={<History />} title="No history" />;
  return (
    <ol className="relative space-y-4 border-l border-border pl-5">
      {h.data.map((e) => {
        const detail = describe(e.action, e.details);
        return (
          <li key={e.id} className="relative">
            <span className="absolute -left-[25px] top-1.5 size-2 rounded-full bg-border-strong ring-4 ring-surface" />
            <div className="text-sm">
              <span className="font-medium">{actionLabels[e.action] ?? e.action}</span>
              {e.actor && <span className="text-muted"> · {e.actor.name}</span>}
            </div>
            {detail && <div className="mt-0.5 text-[13px] text-muted">{detail}</div>}
            <div className="mt-0.5 text-xs text-subtle">{formatDateTime(e.created_at)}</div>
          </li>
        );
      })}
    </ol>
  );
}

/** Selecting text shows where it is on the page (PDFs, and scans with a searchable copy). */
function useShowSelection(id: string) {
  const set = useLocate((s) => s.set);
  React.useEffect(() => () => set(id, null), [id, set]);
  return (e: React.SyntheticEvent<HTMLElement>, page: number) => {
    const sel = window.getSelection();
    const el = e.currentTarget;
    const q = sel && !sel.isCollapsed && el.contains(sel.anchorNode) ? sel.toString() : "";
    if (squashQuery(q).length < 2) return set(id, null);
    // Which occurrence on the page: count the ones up to and including the selection.
    const before = document.createRange();
    before.setStart(el, 0);
    const r = sel!.getRangeAt(0);
    before.setEnd(r.endContainer, r.endOffset);
    const n = Math.max(0, findAll(squashQuery(before.toString()), squashQuery(q)).length - 1);
    set(id, { q, page, n, seq: Date.now() });
  };
}

export function TextTab({ id }: { id: string }) {
  const pages = usePages(id, true);
  const showSelection = useShowSelection(id);
  if (pages.isLoading) return <Skeleton className="h-40" />;
  if (!pages.data?.length) return <EmptyState title="No text yet">Text appears here once Docveta has read the document.</EmptyState>;
  const all = pages.data.map((p) => p.text).join("\n\n");
  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs text-subtle">Select text to see it on the page.</span>
        <Button
          size="sm"
          variant="ghost"
          onClick={() => navigator.clipboard.writeText(all).then(() => toast.success("Text copied"), () => toast.error("Couldn't copy the text"))}
        >
          <Copy /> Copy all
        </Button>
      </div>
      {pages.data.map((p) => (
        <div key={p.page_no}>
          <div className="mb-1 flex items-center gap-2 text-xs text-subtle">
            Page {p.page_no}
            {p.confidence !== null && <span>· {Math.round(p.confidence * 100)}% confidence</span>}
          </div>
          <pre
            className="whitespace-pre-wrap rounded-md bg-surface-2 p-3 font-sans text-[13px] leading-relaxed"
            onMouseUp={(e) => showSelection(e, p.page_no)}
            onKeyUp={(e) => showSelection(e, p.page_no)}
          >
            {p.text || "(no text)"}
          </pre>
        </div>
      ))}
    </div>
  );
}
