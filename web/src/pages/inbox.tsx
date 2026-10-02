import * as React from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { CheckCheck, Inbox, PartyPopper } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { invalidateDocuments, useDocument, useDocuments } from "@/lib/queries";
import { cn, formatDocDate } from "@/lib/utils";
import { PageHeader } from "@/components/app-shell";
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
      if (["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName) || t.isContentEditable || e.metaKey || e.ctrlKey) return;
      if (e.key === "j" || e.key === "ArrowDown") {
        e.preventDefault();
        move(1);
      } else if (e.key === "k" || e.key === "ArrowUp") {
        e.preventDefault();
        move(-1);
      } else if (e.key === "e" && selected) {
        e.preventDefault();
        try {
          await api.patch(`/documents/${selected}`, { inbox: false });
          afterReview();
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
    try {
      await api.post("/documents/bulk", { ids: items.map((d) => d.id), action: "update", update: { inbox: false } });
      invalidateDocuments(qc);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  if (res.isLoading) {
    return (
      <div className="space-y-2 p-6">
        {Array.from({ length: 6 }).map((_, i) => (
          <Skeleton key={i} className="h-16" />
        ))}
      </div>
    );
  }
  if (items.length === 0) {
    return (
      <>
        <PageHeader title="Inbox" />
        <EmptyState icon={<PartyPopper />} title="Inbox zero" className="min-h-[50vh]">
          New documents appear here so you can check what Docveta filled in. Nothing to review right now.
        </EmptyState>
      </>
    );
  }

  return (
    <div className="flex lg:h-[calc(100dvh-3.5rem)]">
      <div className="flex w-full flex-col border-r border-border lg:w-[360px] lg:shrink-0">
        <div className="flex items-center justify-between gap-2 px-4 pb-3 pt-5">
          <div>
            <h1 className="flex items-center gap-2 text-xl font-semibold tracking-tight">
              <Inbox className="size-5 text-muted" /> Inbox
              <span className="rounded-full bg-accent-soft px-2 text-sm text-accent-soft-fg">{total}</span>
            </h1>
            <p className="mt-1 hidden text-xs text-subtle lg:block">
              <Kbd>J</Kbd> <Kbd>K</Kbd> move · <Kbd>E</Kbd> reviewed · <Kbd>↵</Kbd> open
            </p>
          </div>
          <Button size="sm" variant="ghost" onClick={reviewAll}>
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
                  "flex gap-3 border-b border-border px-4 py-3 hover:bg-surface-2",
                  d.id === selected && isDesktop && "bg-accent-soft/60 hover:bg-accent-soft/60",
                )}
              >
                <Thumbnail doc={d} className="h-16 w-12 shrink-0 rounded border border-border" />
                <div className="min-w-0 flex-1">
                  <div className="line-clamp-2 text-sm font-medium leading-snug">{d.title}</div>
                  <div className="mt-0.5 truncate text-xs text-muted">
                    {[d.correspondent?.name, formatDocDate(d.document_date), d.space.name].filter(Boolean).join(" · ")}
                  </div>
                  <div className="mt-1 flex flex-wrap gap-1">
                    <StatusBadge doc={d} className="shadow-none" />
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
