import * as React from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { CheckCheck, PartyPopper, Sparkles } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { invalidateDocuments, useDocument, useDocuments } from "@/lib/queries";
import { cn, formatDocDate, spaceLabel } from "@/lib/utils";
import { PageHeader, useCurrentUser } from "@/components/app-shell";
import { StatusBadge, Thumbnail } from "@/components/documents/doc-items";
import { DocumentDetail } from "./document";
import { Button } from "@/components/ui/button";
import { EmptyState, Kbd, Skeleton, Spinner, TagChip } from "@/components/ui/misc";
import { confirm } from "@/components/ui/confirm";

const inboxQuery = { inbox: true };

/**
 * Inbox = triage. New documents land here; review what Docveta filled in, fix anything,
 * then mark as reviewed. Keyboard: J/K to move, E to mark reviewed.
 */
export function InboxPage() {
  const me = useCurrentUser();
  const res = useDocuments(inboxQuery, { refetchWhileProcessing: true });
  const items = React.useMemo(() => res.data?.pages.flatMap((p) => p.items) ?? [], [res.data]);
  const total = res.data?.pages[0]?.total ?? 0;
  const [selected, setSelected] = React.useState<string | null>(null);
  const navigate = useNavigate();
  const qc = useQueryClient();
  const isDesktop = useMediaQuery("(min-width: 1024px)");

  React.useEffect(() => {
    if (isDesktop && items.length && (!selected || !items.some((d) => d.id === selected))) setSelected(items[0].id);
  }, [items, selected, isDesktop]);

  const index = items.findIndex((d) => d.id === selected);
  const move = React.useCallback(
    (delta: number) => {
      const next = items[Math.min(items.length - 1, Math.max(0, index + delta))];
      if (next) setSelected(next.id);
    },
    [items, index],
  );

  const afterReview = React.useCallback(() => {
    const next = items[index + 1] ?? items[index - 1];
    setSelected(next?.id ?? null);
    invalidateDocuments(qc);
  }, [items, index, qc]);

  React.useEffect(() => {
    if (!isDesktop) return;
    const onKey = async (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName) || t.isContentEditable || e.metaKey || e.ctrlKey || e.altKey || e.defaultPrevented) return;
      if (document.querySelector('[role="dialog"], [role="alertdialog"], [role="menu"]')) return; // a dialog or menu has the keyboard
      if (e.key === "j" || e.key === "ArrowDown") {
        e.preventDefault();
        move(1);
      } else if (e.key === "k" || e.key === "ArrowUp") {
        e.preventDefault();
        move(-1);
      } else if (e.key === "e" && selected) {
        e.preventDefault();
        try {
          const id = selected;
          await api.patch(`/documents/${id}`, { inbox: false });
          toast.success("Marked as reviewed", {
            action: {
              label: "Undo",
              onClick: () => api.patch(`/documents/${id}`, { inbox: true }).then(() => invalidateDocuments(qc), (er) => toast.error(errorMessage(er))),
            },
          });
          afterReview();
        } catch (err) {
          toast.error(errorMessage(err));
        }
      } else if (e.key === "a" && selected && items.find((d) => d.id === selected)?.suggestion_count) {
        e.preventDefault();
        try {
          await api.post(`/documents/${selected}/suggestions/accept`, {});
          qc.invalidateQueries({ queryKey: ["document", selected, "suggestions"] });
          invalidateDocuments(qc, selected);
          toast.success("Suggestions accepted");
        } catch (err) {
          toast.error(errorMessage(err));
        }
      } else if (e.key === "Enter" && selected) {
        navigate({ to: "/documents/$id", params: { id: selected } });
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [isDesktop, move, selected, afterReview, navigate]);

  const reviewAll = async () => {
    if (!(await confirm({ title: `Mark all ${total} as reviewed?`, body: "They'll leave the Inbox but stay in your documents.", confirmLabel: "Mark all reviewed" }))) return;
    // The server picks the documents (all of the Inbox, not just the ones loaded here), in
    // batches of up to 5000.
    const t = toast.loading("Marking as reviewed…");
    try {
      let done = 0;
      let skipped = 0;
      for (let round = 0; round < 100; round++) {
        const r = await api.post<{ succeeded: number; failed: unknown[]; remaining: number }>("/documents/bulk", { select: "inbox", action: "update", update: { inbox: false } });
        done += r.succeeded;
        skipped = r.failed.length;
        if (r.remaining <= 0 || r.succeeded === 0) break;
        toast.loading(`Marked ${done.toLocaleString()} as reviewed…`, { id: t });
      }
      toast.success(`Marked ${done.toLocaleString()} as reviewed${skipped ? ` (${skipped} you can only view stay in the Inbox)` : ""}`, { id: t });
      invalidateDocuments(qc);
    } catch (e) {
      toast.error(errorMessage(e), { id: t });
      invalidateDocuments(qc);
    }
  };

  if (res.isLoading) {
    return (
      <div className="flex lg:h-[calc(100dvh-3.5rem)]">
        <div className="w-full space-y-px border-r border-border lg:w-[280px] 2xl:w-[360px]">
          <Skeleton className="m-4 h-7 w-32" />
          {Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="flex gap-3 px-4 py-3">
              <Skeleton className="h-16 w-12" />
              <div className="flex-1 space-y-2 pt-1">
                <Skeleton className="h-3.5 w-3/4" />
                <Skeleton className="h-3 w-1/2" />
              </div>
            </div>
          ))}
        </div>
        <div className="hidden flex-1 bg-surface-3/40 lg:block" />
      </div>
    );
  }
  if (items.length === 0) {
    return (
      <>
        <PageHeader title="Inbox" />
        <EmptyState
          icon={<PartyPopper />}
          title="Inbox zero"
          className="min-h-[50vh]"
          action={
            <Button asChild>
              <Link to="/documents">Browse all documents</Link>
            </Button>
          }
        >
          New documents appear here so you can check what Docveta filled in. Nothing to review right now.
        </EmptyState>
      </>
    );
  }

  return (
    <div className="flex lg:h-[calc(100dvh-3.5rem)]">
      <div className="flex w-full flex-col border-r border-border lg:w-[280px] lg:shrink-0 2xl:w-[360px]">
        <div className="flex min-h-14 items-center justify-between gap-2 border-b border-border px-4 py-2">
          <div className="min-w-0">
            <h1 className="flex items-center gap-2 text-[17px] font-semibold leading-tight tracking-tight">
              Inbox
              <span className="rounded-full bg-accent-soft px-2 text-sm tabular-nums text-accent-soft-fg">{total}</span>
            </h1>
            <p className="mt-0.5 text-xs text-muted">{isDesktop && index >= 0 ? `${index + 1} of ${total} · check and mark reviewed` : "Check what Docveta filled in"}</p>
          </div>
          <Button size="sm" variant="ghost" onClick={reviewAll} title="Mark everything in the Inbox as reviewed">
            <CheckCheck /> All reviewed
          </Button>
        </div>
        <ul className="flex-1 overflow-y-auto scrollbar-thin" role="listbox" aria-label="Documents to review">
          {items.map((d) => (
            <li key={d.id} role="option" aria-selected={d.id === selected}>
              <Link
                to="/documents/$id"
                params={{ id: d.id }}
                onClick={(e) => {
                  if (isDesktop) {
                    e.preventDefault();
                    setSelected(d.id);
                  }
                }}
                className={cn(
                  "flex gap-3 border-b border-l-2 border-b-border border-l-transparent px-4 py-3 transition-colors hover:bg-surface-2",
                  d.id === selected && isDesktop && "border-l-accent bg-accent-soft/50 hover:bg-accent-soft/50",
                )}
              >
                <Thumbnail doc={d} className="h-16 w-12 shrink-0 rounded border border-border" />
                <div className="min-w-0 flex-1">
                  <div className="line-clamp-2 text-sm font-medium leading-snug">{d.title}</div>
                  <div className="mt-0.5 truncate text-xs text-muted">
                    {[d.correspondent?.name, formatDocDate(d.document_date), spaceLabel(me.spaces.find((s) => s.id === d.space.id) ?? d.space)].filter(Boolean).join(" · ")}
                  </div>
                  <div className="mt-1 flex flex-wrap gap-1">
                    <StatusBadge doc={d} className="shadow-none" />
                    {!!d.suggestion_count && (
                      <span className="inline-flex items-center gap-1 rounded-full bg-accent-soft px-2 py-0.5 text-[11px] font-medium text-accent-soft-fg" title="AI has suggestions to review">
                        <Sparkles className="size-3" /> {d.suggestion_count}
                      </span>
                    )}
                    {d.tags.slice(0, 2).map((t) => (
                      <TagChip key={t.id} name={t.name} color={t.color} />
                    ))}
                  </div>
                </div>
              </Link>
            </li>
          ))}
          {res.hasNextPage && (
            <li className="p-3 text-center">
              <Button size="sm" variant="ghost" loading={res.isFetchingNextPage} onClick={() => res.fetchNextPage()}>
                Load more
              </Button>
            </li>
          )}
        </ul>
        {isDesktop && (
          <p className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-border px-4 py-2.5 text-xs text-subtle">
            {[["J K", "move"], ["E", "reviewed"], ["A", "accept AI"], ["↵", "open"]].map(([keys, label]) => (
              <span key={label} className="inline-flex items-center gap-1 whitespace-nowrap">
                {keys.split(" ").map((k) => <Kbd key={k}>{k}</Kbd>)} {label}
              </span>
            ))}
          </p>
        )}
      </div>
      {isDesktop && (
        <div className="min-w-0 flex-1">
          {selected ? <InboxDetail id={selected} onReviewed={afterReview} /> : null}
        </div>
      )}
    </div>
  );
}

function InboxDetail({ id, onReviewed }: { id: string; onReviewed: () => void }) {
  const doc = useDocument(id);
  if (!doc.data) return <div className="flex h-full items-center justify-center"><Spinner /></div>;
  return <DocumentDetail doc={doc.data} onReviewed={onReviewed} compact />;
}

function useMediaQuery(q: string) {
  const [match, setMatch] = React.useState(() => window.matchMedia(q).matches);
  React.useEffect(() => {
    const m = window.matchMedia(q);
    const on = () => setMatch(m.matches);
    m.addEventListener("change", on);
    return () => m.removeEventListener("change", on);
  }, [q]);
  return match;
}
