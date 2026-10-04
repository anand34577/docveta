import * as React from "react";
import { useNavigate, useParams, useRouter, useSearch } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, CheckCheck, Download, FileDown, LayoutGrid, MoreHorizontal, RotateCcw, ScanText, Share2, Sparkles, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { invalidateDocuments, useAIEnabled, useDocument, useUpdateDocument } from "@/lib/queries";
import { cn, spaceLabel } from "@/lib/utils";
import { useCurrentUser } from "@/components/app-shell";
import { DocViewer } from "@/components/documents/doc-viewer";
import { MetadataPanel } from "@/components/documents/metadata-panel";
import { HistoryTab, NotesTab, TextTab } from "@/components/documents/doc-tabs";
import { SimilarTab, VersionsTab } from "@/components/documents/versions-similar";
import { ShareDialog } from "@/components/documents/share-dialog";
const PageManager = React.lazy(() => import("@/components/documents/page-manager").then((m) => ({ default: m.PageManager })));
import { Button } from "@/components/ui/button";
import { Spinner, Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/misc";
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

  if (doc.isLoading) return <div className="flex h-full items-center justify-center"><Spinner /></div>;
  if (doc.isError || !doc.data) return <NotFound />;

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
          onClick: async () => {
            await api.post(`/documents/${doc.id}/restore`);
            invalidateDocuments(qc, doc.id);
          },
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
                <Button size="sm" variant="primary" onClick={markReviewed} loading={update.isPending}>
                  <CheckCheck /> <span className="hidden sm:inline">{compact ? "Reviewed & next" : "Reviewed"}</span>
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

      <aside className={cn("w-full shrink-0 overflow-y-auto scrollbar-thin bg-surface lg:block lg:w-[360px] xl:w-[400px]", pane === "details" ? "block" : "hidden")}>
        <Tabs value={tab} onValueChange={setTab}>
          <TabsList className="sticky top-0 z-10 h-14 items-end overflow-x-auto bg-surface px-3 scrollbar-thin [&>button]:shrink-0">
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
