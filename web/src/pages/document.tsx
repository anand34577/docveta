import * as React from "react";
import { useNavigate, useParams, useRouter, useSearch } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, CheckCheck, Download, FileDown, MoreHorizontal, RotateCcw, ScanText, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { invalidateDocuments, useDocument, useUpdateDocument } from "@/lib/queries";
import { cn } from "@/lib/utils";
import { useCurrentUser } from "@/components/app-shell";
import { DocViewer } from "@/components/documents/doc-viewer";
import { MetadataPanel } from "@/components/documents/metadata-panel";
import { HistoryTab, NotesTab, TextTab } from "@/components/documents/doc-tabs";
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

  const markReviewed = async () => {
    try {
      await update.mutateAsync({ patch: { inbox: false } });
      toast.success("Marked as reviewed");
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
      <section className="flex min-h-0 flex-col border-border lg:flex-1 lg:border-r">
        <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-2 sm:px-3">
          {onBack && (
            <Button size="icon-sm" variant="ghost" onClick={onBack} aria-label="Back">
              <ArrowLeft />
            </Button>
          )}
          <h1 className="min-w-0 flex-1 truncate text-[15px] font-semibold">{doc.title}</h1>
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
                  <CheckCheck /> <span className="hidden sm:inline">Reviewed</span>
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
        <div className="h-[62vh] min-h-0 lg:h-auto lg:flex-1">
          <DocViewer doc={doc} page={page} highlight={highlight} />
        </div>
      </section>

      <aside className="w-full shrink-0 overflow-y-auto scrollbar-thin bg-surface lg:w-[400px]">
        <Tabs value={tab} onValueChange={setTab}>
          <TabsList className="sticky top-0 z-10 bg-surface px-3">
            <TabsTrigger value="details">Details</TabsTrigger>
            <TabsTrigger value="notes">Notes{doc.note_count ? ` (${doc.note_count})` : ""}</TabsTrigger>
            <TabsTrigger value="text">Text</TabsTrigger>
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
            <TabsContent value="history">{tab === "history" && <HistoryTab id={doc.id} />}</TabsContent>
          </div>
        </Tabs>
      </aside>
    </div>
  );
}
