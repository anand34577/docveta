import * as React from "react";
import { Link } from "@tanstack/react-router";
import { AlertCircle, FileImage, FileText, FileType2, Loader2, Lock, MessageSquare } from "lucide-react";
import { cn, fileKind, formatDocDate } from "@/lib/utils";
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
    processing: { icon: <Loader2 className="size-3 animate-spin" />, label: stageLabel(doc.processing_stage), cls: "bg-surface/90 text-muted" },
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
      <Link to="/documents/$id" params={{ id: doc.id }} search={linkSearch(doc, query)} className="flex flex-1 flex-col" onClick={(e) => selecting && (e.preventDefault(), onToggle(e))}>
        <Thumbnail doc={doc} className="aspect-[4/3.6] border-b border-border" />
        <div className="flex flex-1 flex-col gap-1.5 p-3">
          <h3 className="line-clamp-2 text-sm font-medium leading-snug">{doc.title}</h3>
          <div className="truncate text-xs text-muted">
            {[formatDocDate(doc.document_date), doc.correspondent?.name].filter(Boolean).join(" · ") || " "}
          </div>
          <Snippet segments={doc.snippet} />
          <div className="mt-auto pt-1">
            <TagList tags={doc.tags} max={2} />
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
      {doc.inbox && doc.status === "ready" && <span className="absolute right-2.5 top-2.5 size-2.5 rounded-full bg-accent ring-2 ring-surface" title="In inbox" />}
    </div>
  );
}

export function DocumentRow({ doc, selected, selecting, onToggle, query }: ItemProps) {
  return (
    <div className={cn("group flex items-center gap-3 border-b border-border px-3 py-2.5 hover:bg-surface-2 sm:px-4", selected && "bg-accent-soft/50")}>
      <div className={cn("transition-opacity", selecting || selected ? "opacity-100" : "sm:opacity-0 sm:group-hover:opacity-100")}>
        <Checkbox checked={selected} onClick={(e) => onToggle(e)} aria-label={`Select ${doc.title}`} />
      </div>
      <Link
        to="/documents/$id"
        params={{ id: doc.id }}
        search={linkSearch(doc, query)}
        onClick={(e) => selecting && (e.preventDefault(), onToggle(e))}
        className="flex min-w-0 flex-1 items-center gap-3"
      >
        <Thumbnail doc={doc} className="h-12 w-10 shrink-0 rounded border border-border" />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            {doc.inbox && <span className="size-2 shrink-0 rounded-full bg-accent" title="In inbox" />}
            <span className="truncate text-sm font-medium">{doc.title}</span>
            <StatusBadge doc={doc} className="shrink-0 shadow-none" />
          </div>
          {doc.snippet?.length ? (
            <Snippet segments={doc.snippet} className="line-clamp-1" />
          ) : (
            <div className="truncate text-xs text-muted sm:hidden">
              {[formatDocDate(doc.document_date), doc.correspondent?.name].filter(Boolean).join(" · ")}
            </div>
          )}
        </div>
        <div className="hidden w-40 truncate text-sm text-muted md:block">{doc.correspondent?.name}</div>
        <div className="hidden w-32 truncate text-sm text-muted xl:block">{doc.document_type?.name}</div>
        <div className="hidden w-48 lg:block">
          <TagList tags={doc.tags} max={2} />
        </div>
        <div className="hidden w-24 text-right text-sm tabular-nums text-muted sm:block">{formatDocDate(doc.document_date)}</div>
        <div className="hidden w-8 text-right text-xs text-subtle sm:block">
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
