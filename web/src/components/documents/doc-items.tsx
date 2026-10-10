import * as React from "react";
import { Link } from "@tanstack/react-router";
import { AlertCircle, FileImage, FileText, FileType2, Loader2, Lock, MessageSquare } from "lucide-react";
import { cn, fileKind, formatDocDate, spaceDot, spaceLabel } from "@/lib/utils";
import { useCurrentUser } from "@/components/app-shell";
import type { Document, Segment } from "@/lib/types";
import { Checkbox, TagChip } from "@/components/ui/misc";

export function Thumbnail({ doc, className }: { doc: Document; className?: string }) {
  const [failed, setFailed] = React.useState(false);
  const kind = fileKind(doc.mime_type);
  const Icon = kind === "image" ? FileImage : kind === "text" ? FileType2 : FileText;
  return (
    <div className={cn("relative overflow-hidden bg-surface-2", className)}>
      {doc.has_thumbnail && !failed ? (
        <img
          src={`/api/v1/documents/${doc.id}/thumbnail?v=${doc.version}`}
          alt=""
          loading="lazy"
          decoding="async"
          onError={() => setFailed(true)}
          className="size-full object-cover object-top"
        />
      ) : (
        <div className="flex size-full items-center justify-center text-subtle">
          <Icon className="size-8" strokeWidth={1.5} />
        </div>
      )}
    </div>
  );
}

export function StatusBadge({ doc, className }: { doc: Document; className?: string }) {
  if (doc.status === "ready") return null;
  const map = {
    processing: { icon: <Loader2 className="size-3 animate-spin" />, label: processingLabel(doc), cls: "bg-surface/90 text-muted" },
    failed: { icon: <AlertCircle className="size-3" />, label: "Couldn't process", cls: "bg-danger-soft text-danger" },
    needs_password: { icon: <Lock className="size-3" />, label: "Password protected", cls: "bg-warning-soft text-warning" },
  } as const;
  const m = map[doc.status];
  return (
    <span className={cn("inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium shadow-sm backdrop-blur", m.cls, className)}>
      {m.icon}
      {m.label}
    </span>
  );
}

/** Marks documents still in the Inbox (not yet reviewed). */
export function NewBadge({ className }: { className?: string }) {
  return (
    <span className={cn("rounded bg-accent-soft px-1.5 text-[11px] font-medium leading-[18px] text-accent-soft-fg", className)} title="In your Inbox, not reviewed yet">
      New
    </span>
  );
}

/** The stage, with pages read so far while reading: "Reading text · 3 of 12 pages". */
export function processingLabel(doc: Document): string {
  const label = stageLabel(doc.processing_stage);
  const p = doc.progress;
  if (!p || doc.processing_stage !== "ocr") return label;
  if (p.pages_total > 1) return `${label} · ${p.pages_done} of ${p.pages_total} pages`;
  return p.pages_total === 0 && p.pages_done > 1 ? `${label} · ${p.pages_done} pages read` : label;
}

export function stageLabel(stage: string): string {
  switch (stage) {
    case "awaiting_ocr":
      return "Waiting to read text";
    case "ocr":
      return "Reading text";
    case "classifying":
    case "indexing":
      return "Organising";
    default:
      return "Processing";
  }
}

export function Snippet({ segments, className }: { segments?: Segment[]; className?: string }) {
  if (!segments?.length) return null;
  return (
    <p className={cn("line-clamp-2 text-[13px] leading-relaxed text-muted", className)}>
      {segments.map((s, i) =>
        s.hit ? (
          <mark key={i} className="rounded-sm bg-amber-200/70 px-0.5 text-fg dark:bg-amber-400/25">
            {s.text}
          </mark>
        ) : (
          <React.Fragment key={i}>{s.text}</React.Fragment>
        ),
      )}
    </p>
  );
}

/** The space a document lives in, as a coloured dot and name. Only shown to people with more than one space. */
export function SpaceTag({ doc, className }: { doc: Document; className?: string }) {
  const me = useCurrentUser();
  if (me.spaces.length < 2) return null;
  const s = me.spaces.find((x) => x.id === doc.space.id);
  return (
    <span className={cn("inline-flex min-w-0 items-center gap-1.5 text-xs text-muted", className)} title={`In ${spaceLabel(s ?? doc.space)}`}>
      <span className={cn("size-2 shrink-0 rounded-full", s?.kind === "personal" ? "bg-slate-400" : spaceDot(s?.color))} />
      <span className="truncate">{spaceLabel(s ?? doc.space)}</span>
    </span>
  );
}

function TagList({ tags, max = 3 }: { tags: Document["tags"]; max?: number }) {
  if (!tags.length) return null;
  return (
    <div className="flex min-w-0 flex-wrap gap-1">
      {tags.slice(0, max).map((t) => (
        <TagChip key={t.id} name={t.name} color={t.color} />
      ))}
      {tags.length > max && <span className="text-xs text-subtle">+{tags.length - max}</span>}
    </div>
  );
}

interface ItemProps {
  doc: Document;
  selected: boolean;
  selecting: boolean;
  onToggle: (e: React.MouseEvent | React.KeyboardEvent) => void;
  query?: string;
}

/** While selecting, or with Ctrl/⌘/Shift held, a click selects instead of opening. */
function selectClick(e: React.MouseEvent, selecting: boolean, onToggle: ItemProps["onToggle"]) {
  if (!selecting && !e.metaKey && !e.ctrlKey && !e.shiftKey) return;
  e.preventDefault();
  onToggle(e);
}

function linkSearch(doc: Document, query?: string) {
  return { page: doc.matched_page, q: query || undefined };
}

export function DocumentCard({ doc, selected, selecting, onToggle, query }: ItemProps) {
  return (
    <div
      className={cn(
        "group relative flex flex-col overflow-hidden rounded-xl border bg-surface shadow-sm transition-all hover:-translate-y-0.5 hover:shadow-md",
        selected ? "border-accent ring-2 ring-accent/30" : "border-border",
      )}
    >
      <Link to="/documents/$id" params={{ id: doc.id }} search={linkSearch(doc, query)} className="flex flex-1 flex-col" onClick={(e) => selectClick(e, selecting, onToggle)}>
        <Thumbnail doc={doc} className="aspect-[4/3.6] border-b border-border" />
        <div className="flex flex-1 flex-col gap-1.5 p-3">
          <h3 className="line-clamp-2 break-words text-sm font-medium leading-snug" title={doc.title}>{doc.title}</h3>
          <div className="truncate text-xs text-muted">
            {[formatDocDate(doc.document_date), doc.correspondent?.name].filter(Boolean).join(" · ") || " "}
          </div>
          <Snippet segments={doc.snippet} />
          <div className="mt-auto space-y-1.5 pt-1">
            <TagList tags={doc.tags} max={2} />
            <SpaceTag doc={doc} />
          </div>
        </div>
      </Link>
      <div className="absolute left-2 right-2 top-2 flex items-start justify-between gap-2">
        <div
          className={cn("rounded-md bg-surface/90 p-1 shadow-sm transition-opacity", selecting || selected ? "opacity-100" : "opacity-0 group-hover:opacity-100 focus-within:opacity-100")}
        >
          <Checkbox checked={selected} onClick={(e) => onToggle(e)} aria-label={`Select ${doc.title}`} />
        </div>
        <StatusBadge doc={doc} />
      </div>
      {doc.inbox && doc.status === "ready" && <NewBadge className="absolute right-2 top-2 shadow-sm" />}
    </div>
  );
}

export function DocumentRow({ doc, selected, selecting, onToggle, query }: ItemProps) {
  return (
    <div className={cn("group flex items-center gap-3 border-b border-border page-x py-2.5 hover:bg-surface-2", selected && "bg-accent-soft/50")}>
      <div className={cn("transition-opacity", selecting || selected ? "opacity-100" : "sm:opacity-0 sm:group-hover:opacity-100")}>
        <Checkbox checked={selected} onClick={(e) => onToggle(e)} aria-label={`Select ${doc.title}`} />
      </div>
      <Link
        to="/documents/$id"
        params={{ id: doc.id }}
        search={linkSearch(doc, query)}
        onClick={(e) => selectClick(e, selecting, onToggle)}
        className="flex min-w-0 flex-1 items-center gap-3"
      >
        <Thumbnail doc={doc} className="h-12 w-10 shrink-0 rounded border border-border" />
        <div className="min-w-[14rem] flex-1">
          <div className="flex min-w-0 items-center gap-2">
            <span className="truncate text-sm font-medium" title={doc.title}>{doc.title}</span>
            {doc.inbox && doc.status === "ready" && <NewBadge className="shrink-0" />}
            <StatusBadge doc={doc} className="shrink-0 shadow-none" />
          </div>
          {doc.snippet?.length ? (
            <Snippet segments={doc.snippet} className="line-clamp-1" />
          ) : (
            <div className="truncate text-xs text-muted lg:hidden">
              {[formatDocDate(doc.document_date), doc.correspondent?.name].filter(Boolean).join(" · ")}
            </div>
          )}
        </div>
        <div className="hidden w-36 shrink-0 truncate text-sm text-muted lg:block" title={doc.correspondent?.name}>{doc.correspondent?.name}</div>
        <div className="hidden w-28 shrink-0 truncate text-sm text-muted 2xl:block" title={doc.document_type?.name}>{doc.document_type?.name}</div>
        <div className="hidden w-44 shrink-0 xl:block">
          <TagList tags={doc.tags} max={2} />
        </div>
        <div className="hidden w-28 shrink-0 2xl:block">
          <SpaceTag doc={doc} />
        </div>
        <div className="hidden w-24 shrink-0 text-right text-sm tabular-nums text-muted sm:block">{formatDocDate(doc.document_date)}</div>
        <div className="hidden w-8 shrink-0 text-right text-xs text-subtle sm:block">
          {doc.note_count > 0 && (
            <span className="inline-flex items-center gap-0.5">
              <MessageSquare className="size-3" />
              {doc.note_count}
            </span>
          )}
        </div>
      </Link>
    </div>
  );
}
