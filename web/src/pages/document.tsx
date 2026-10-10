import * as React from "react";
import { useNavigate, useParams, useRouter, useSearch } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, CheckCheck, ChevronLeft, ChevronRight, CloudOff, Download, FileDown, LayoutGrid, Link2, MessageSquareText, MoreHorizontal, RotateCcw, ScanText, Share2, Sparkles, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { invalidateDocuments, useAIEnabled, useDocument, useUpdateDocument } from "@/lib/queries";
import { cn, spaceLabel, usePageTitle } from "@/lib/utils";
import { useCurrentUser } from "@/components/app-shell";
import { useResultList } from "@/stores/ui";
import { forgetDoc, rememberDoc, rememberSearch } from "@/lib/recent";
import { DocViewer } from "@/components/documents/doc-viewer";
import { MetadataPanel } from "@/components/documents/metadata-panel";
import { HistoryTab, NotesTab, TextTab } from "@/components/documents/doc-tabs";
import { SimilarTab, VersionsTab } from "@/components/documents/versions-similar";
import { ShareDialog } from "@/components/documents/share-dialog";
const PageManager = React.lazy(() => import("@/components/documents/page-manager").then((m) => ({ default: m.PageManager })));
import { Button } from "@/components/ui/button";
import { EmptyState, Spinner, Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/misc";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/overlay";
import { confirm } from "@/components/ui/confirm";
import { NotFound } from "./not-found";
import type { Document } from "@/lib/types";

export function DocumentPage() {
  const { id } = useParams({ from: "/app/documents/$id" });
  const search = useSearch({ from: "/app/documents/$id" });
  const doc = useDocument(id);
  const router = useRouter();

  const title = doc.data?.title;
  const missing = (doc.error as { status?: number } | null)?.status === 404;
  React.useEffect(() => {
    if (missing) forgetDoc(id);
    else if (title) rememberDoc({ id, title });
  }, [id, title, missing]);
  React.useEffect(() => {
    if (title && search.q) rememberSearch(search.q);
  }, [id, title, search.q]);

  if (doc.isLoading) return <div className="flex h-full items-center justify-center"><Spinner /></div>;
  if (missing) return <NotFound />;
  // A failed refresh (the page checks again while text is being read) keeps what's on screen.
  if (!doc.data) {
    if (!doc.error) return <NotFound />;
    return (
      <EmptyState
        icon={<CloudOff />}
        title="Couldn't load this document"
        className="min-h-[60vh]"
        action={
          <Button onClick={() => doc.refetch()} loading={doc.isFetching}>
            Try again
          </Button>
        }
      >
        {errorMessage(doc.error)}
      </EmptyState>
    );
  }

  const highlight = search.q ? search.q.split(/\s+/).filter((t) => t && !t.includes(":") && !t.startsWith("-")).map((t) => t.replace(/"/g, "")) : undefined;
  return (
    <DocumentDetail
      doc={doc.data}
      page={search.page}
      highlight={highlight}
      onBack={() => (window.history.length > 1 ? router.history.back() : router.navigate({ to: "/documents" }))}
    />
  );
}

export function DocumentDetail({ doc, page, highlight, onBack, onReviewed, compact }: {
  doc: Document;
  page?: number;
  highlight?: string[];
  onBack?: () => void;
  onReviewed?: () => void;
  compact?: boolean;
}) {
  const me = useCurrentUser();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const update = useUpdateDocument(doc.id);
  const space = me.spaces.find((s) => s.id === doc.space.id);
  const canEdit = !!space && space.role !== "viewer";
  const [tab, setTab] = React.useState("details");
  React.useEffect(() => setTab("details"), [doc.id]);
  const [pane, setPane] = React.useState<"preview" | "details">("preview"); // phones show one at a time
  React.useEffect(() => setPane("preview"), [doc.id]);
  const [sharing, setSharing] = React.useState(false);
  const [arranging, setArranging] = React.useState(false);
  const ai = useAIEnabled().data;
  usePageTitle(compact ? undefined : doc.title);
  const pageEditable = canEdit && (doc.mime_type === "application/pdf" || doc.mime_type === "image/jpeg" || doc.mime_type === "image/png");

  const markReviewed = async () => {
    try {
      await update.mutateAsync({ patch: { inbox: false } });
      toast.success("Marked as reviewed", {
        action: {
          label: "Undo",
          onClick: () => api.patch(`/documents/${doc.id}`, { inbox: true }).then(() => invalidateDocuments(qc, doc.id), (e) => toast.error(errorMessage(e))),
        },
      });
      onReviewed?.();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };
  const trash = async () => {
    try {
      await api.del(`/documents/${doc.id}`);
      invalidateDocuments(qc, doc.id);
      toast("Moved to Trash", {
        action: {
          label: "Undo",
          onClick: () => api.post(`/documents/${doc.id}/restore`).then(() => invalidateDocuments(qc, doc.id), (e) => toast.error(errorMessage(e))),
        },
      });
      if (onReviewed) onReviewed();
      else navigate({ to: "/documents" });
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };
  const restore = async () => {
    await api.post(`/documents/${doc.id}/restore`).catch((e) => toast.error(errorMessage(e)));
    invalidateDocuments(qc, doc.id);
  };
  const purge = async () => {
    if (!(await confirm({ title: "Delete forever?", body: `“${doc.title}” will be permanently deleted. This can't be undone.`, confirmLabel: "Delete forever", destructive: true }))) return;
    await api.del(`/documents/${doc.id}`, { permanent: true }).then(
      () => {
        invalidateDocuments(qc);
        navigate({ to: "/trash" });
      },
      (e) => toast.error(errorMessage(e)),
    );
  };
  // The address of this page: it opens for people who are in the document's space.
  const copyLink = () =>
    navigator.clipboard.writeText(`${window.location.origin}/documents/${doc.id}`).then(
      () => toast.success("Link copied", { description: "It opens for people who have access to this space." }),
      () => toast.error("Couldn't copy the link"),
    );
  const reprocess = async (force: boolean) => {
    await api.post(`/documents/${doc.id}/reprocess`, undefined, { force_ocr: force }).then(
      () => {
        invalidateDocuments(qc, doc.id);
        toast.success("Reprocessing started");
      },
      (e) => toast.error(errorMessage(e)),
    );
  };

  return (
    <div className={cn("flex flex-col lg:flex-row", compact ? "h-full" : "lg:h-[calc(100dvh-3.5rem)]")}>
      <section className="flex min-h-0 min-w-0 flex-col border-border lg:flex-1 lg:border-r">
        <div className="flex h-14 shrink-0 items-center gap-2 border-b border-border bg-surface px-2 sm:px-3">
          {onBack && (
            <Button size="icon-sm" variant="ghost" onClick={onBack} aria-label="Back">
              <ArrowLeft />
            </Button>
          )}
          {!compact && <Neighbours id={doc.id} />}
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-[15px] font-semibold leading-tight">{doc.title}</h1>
            <div className="truncate text-xs text-subtle">{[spaceLabel(space ?? doc.space), doc.correspondent?.name].filter(Boolean).join(" · ")}</div>
          </div>
          {doc.deleted_at ? (
            <>
              <Button size="sm" onClick={restore}>
                <RotateCcw /> Restore
              </Button>
              <Button size="sm" variant="danger-ghost" onClick={purge}>
                <Trash2 /> <span className="hidden sm:inline">Delete forever</span>
              </Button>
            </>
          ) : (
            <>
              {doc.inbox && canEdit && (
                <Button size="sm" variant="primary" onClick={markReviewed} loading={update.isPending} aria-label={compact ? "Reviewed, go to next" : "Mark as reviewed"} title={compact ? "Reviewed, go to next (E)" : "Mark as reviewed"}>
                  <CheckCheck /> <span className={compact ? "hidden 2xl:inline" : "hidden sm:inline"}>{compact ? "Reviewed & next" : "Reviewed"}</span>
                </Button>
              )}
              <Button size="icon-sm" variant="ghost" asChild>
                <a href={`/api/v1/documents/${doc.id}/file?kind=original&download=1`} aria-label="Download">
                  <Download />
                </a>
              </Button>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button size="icon-sm" variant="ghost" aria-label="More actions">
                    <MoreHorizontal />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent>
                  <DropdownMenuItem asChild>
                    <a href={`/api/v1/documents/${doc.id}/file?kind=original&download=1`}>
                      <Download /> Download original
                    </a>
                  </DropdownMenuItem>
                  {doc.has_archive && (
                    <DropdownMenuItem asChild>
                      <a href={`/api/v1/documents/${doc.id}/file?kind=archive&download=1`}>
                        <FileDown /> Download searchable PDF
                      </a>
                    </DropdownMenuItem>
                  )}
                  <DropdownMenuItem onSelect={() => setSharing(true)}>
                    <Share2 /> Share with a link…
                  </DropdownMenuItem>
                  <DropdownMenuItem onSelect={copyLink}>
                    <Link2 /> Copy link for members
                  </DropdownMenuItem>
                  {ai?.chat && doc.status === "ready" && (
                    <DropdownMenuItem onSelect={() => navigate({ to: "/ask", search: { doc: doc.id } })}>
                      <MessageSquareText /> Ask about this document
                    </DropdownMenuItem>
                  )}
                  {pageEditable && (
                    <DropdownMenuItem onSelect={() => setArranging(true)}>
                      <LayoutGrid /> Arrange pages…
                    </DropdownMenuItem>
                  )}
                  {canEdit && (
                    <>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem onSelect={() => reprocess(false)}>
                        <RotateCcw /> Process again
                      </DropdownMenuItem>
                      <DropdownMenuItem onSelect={() => reprocess(true)}>
                        <ScanText /> Re-read text (force OCR)
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem destructive onSelect={trash}>
                        <Trash2 /> Move to Trash
                      </DropdownMenuItem>
                    </>
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
            </>
          )}
        </div>
        <div className="flex border-b border-border bg-surface p-1.5 lg:hidden" role="tablist" aria-label="Show">
          {([
            ["preview", "Preview"],
            ["details", "Details & notes"],
          ] as const).map(([k, l]) => (
            <button key={k} role="tab" aria-selected={pane === k} onClick={() => setPane(k)} className={cn("flex-1 rounded-md py-1.5 text-sm font-medium text-muted", pane === k && "bg-surface-2 text-fg")}>
              {l}
            </button>
          ))}
        </div>
        <div className={cn("min-h-0 lg:block lg:h-auto lg:flex-1", pane === "preview" ? "h-[calc(100dvh-14rem)]" : "hidden")}>
          <DocViewer doc={doc} page={page} highlight={highlight} />
        </div>
      </section>

      <aside className={cn("w-full shrink-0 overflow-y-auto scrollbar-thin bg-surface lg:block", compact ? "lg:w-[320px] 2xl:w-[400px]" : "lg:w-[360px] xl:w-[400px]", pane === "details" ? "block" : "hidden")}>
        <Tabs value={tab} onValueChange={setTab}>
          <TabsList className="sticky top-0 z-10 h-14 items-end bg-surface px-3">
            <TabsTrigger value="details">Details</TabsTrigger>
            <TabsTrigger value="notes">Notes{doc.note_count ? ` (${doc.note_count})` : ""}</TabsTrigger>
            <TabsTrigger value="text">Text</TabsTrigger>
            <TabsTrigger value="versions">Versions</TabsTrigger>
            {ai?.embeddings && (
              <TabsTrigger value="similar">
                <Sparkles className="mr-1 inline size-3.5" />Similar
              </TabsTrigger>
            )}
            <TabsTrigger value="history">History</TabsTrigger>
          </TabsList>
          <div className="p-4">
            <TabsContent value="details">
              <MetadataPanel doc={doc} />
            </TabsContent>
            <TabsContent value="notes">
              <NotesTab id={doc.id} />
            </TabsContent>
            <TabsContent value="text">{tab === "text" && <TextTab id={doc.id} />}</TabsContent>
            <TabsContent value="versions">{tab === "versions" && <VersionsTab id={doc.id} canEdit={canEdit && !doc.deleted_at} />}</TabsContent>
            <TabsContent value="similar">{tab === "similar" && <SimilarTab id={doc.id} />}</TabsContent>
            <TabsContent value="history">{tab === "history" && <HistoryTab id={doc.id} />}</TabsContent>
          </div>
        </Tabs>
      </aside>
      {sharing && <ShareDialog docId={doc.id} title={doc.title} onClose={() => setSharing(false)} />}
      {arranging && (
        <React.Suspense fallback={null}>
          <PageManager doc={doc} onClose={() => setArranging(false)} />
        </React.Suspense>
      )}
    </div>
  );
}

/** Previous / next document in the list it was opened from (buttons, or K / J). */
function Neighbours({ id }: { id: string }) {
  const { ids, q } = useResultList();
  const navigate = useNavigate();
  const i = ids.indexOf(id);
  const go = React.useCallback(
    (to?: string) => to && navigate({ to: "/documents/$id", params: { id: to }, search: { q }, replace: true }),
    [navigate, q],
  );
  const prev = i > 0 ? ids[i - 1] : undefined;
  const next = i >= 0 ? ids[i + 1] : undefined;
  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName) || t.isContentEditable || e.metaKey || e.ctrlKey || e.altKey || e.defaultPrevented) return;
      if (document.querySelector('[role="dialog"], [role="alertdialog"], [role="menu"]')) return;
      if (e.key === "j" && next) go(next);
      else if (e.key === "k" && prev) go(prev);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [prev, next, go]);
  if (i < 0 || ids.length < 2) return null;
  return (
    <div className="flex items-center">
      <Button size="icon-sm" variant="ghost" disabled={!prev} onClick={() => go(prev)} aria-label="Previous document" title="Previous document (K)">
        <ChevronLeft />
      </Button>
      <span className="hidden px-0.5 text-xs tabular-nums text-subtle sm:inline">
        {i + 1}/{ids.length}
      </span>
      <Button size="icon-sm" variant="ghost" disabled={!next} onClick={() => go(next)} aria-label="Next document" title="Next document (J)">
        <ChevronRight />
      </Button>
    </div>
  );
}
