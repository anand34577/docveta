import * as React from "react";
import { Link } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Download, RotateCcw, Sparkles, Upload } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { invalidateDocuments, useSimilar, useVersions } from "@/lib/queries";
import { formatBytes, formatDateTime, formatDocDate } from "@/lib/utils";
import { Thumbnail } from "@/components/documents/doc-items";
import { Button } from "@/components/ui/button";
import { Badge, EmptyState, Skeleton } from "@/components/ui/misc";
import { confirm } from "@/components/ui/confirm";

/** Every file this document has had. A new upload or a page change adds one; restoring brings an old one back. */
export function VersionsTab({ id, canEdit }: { id: string; canEdit: boolean }) {
  const versions = useVersions(id, true);
  const qc = useQueryClient();
  const input = React.useRef<HTMLInputElement>(null);
  const [busy, setBusy] = React.useState(false);

  const refresh = () => {
    invalidateDocuments(qc, id);
    qc.invalidateQueries({ queryKey: ["document", id] });
  };
  const upload = async (file: File) => {
    setBusy(true);
    try {
      const form = new FormData();
      form.append("note", "Replaced file");
      form.append("file", file, file.name);
      await api.post(`/documents/${id}/versions`, form);
      refresh();
      toast.success("New version added. Text recognition runs again.");
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  const restore = async (no: number) => {
    if (!(await confirm({ title: `Go back to version ${no}?`, body: "It becomes the current file. Nothing is lost: the current one stays in this list.", confirmLabel: "Restore" }))) return;
    await api.post(`/documents/${id}/versions/${no}/restore`).then(refresh, (e) => toast.error(errorMessage(e)));
  };

  return (
    <div className="space-y-3">
      {canEdit && (
        <>
          <Button size="sm" loading={busy} onClick={() => input.current?.click()}>
            <Upload /> Upload a new version
          </Button>
          <input ref={input} type="file" hidden onChange={(e) => e.target.files?.[0] && upload(e.target.files[0])} />
        </>
      )}
      {versions.isLoading ? (
        <Skeleton className="h-16" />
      ) : (
        <ul className="divide-y divide-border rounded-lg border border-border">
          {(versions.data ?? []).map((v) => (
            <li key={v.version_no} className="flex items-center gap-3 px-3 py-2.5">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-1.5 text-sm font-medium">
                  Version {v.version_no} {v.current && <Badge tone="accent">Current</Badge>}
                </div>
                <div className="truncate text-xs text-subtle">
                  {formatDateTime(v.created_at)} · {formatBytes(v.size_bytes)}
                  {v.created_by && ` · ${v.created_by.name}`}
                  {v.note && ` · ${v.note}`}
                </div>
              </div>
              <Button size="icon-sm" variant="ghost" asChild>
                <a href={`/api/v1/documents/${id}/file?kind=original&version=${v.version_no}&download=1`} aria-label={`Download version ${v.version_no}`}>
                  <Download />
                </a>
              </Button>
              {canEdit && !v.current && (
                <Button size="sm" variant="ghost" onClick={() => restore(v.version_no)}>
                  <RotateCcw /> Restore
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/** Documents that mean the same thing (AI embeddings) or share details like the sender and type. */
export function SimilarTab({ id }: { id: string }) {
  const similar = useSimilar(id, true);
  if (similar.isLoading) return <Skeleton className="h-24" />;
  const items = similar.data ?? [];
  if (!items.length)
    return (
      <EmptyState icon={<Sparkles />} title="Nothing similar yet" className="py-8">
        {similar.isError ? "Similar documents need an AI provider with an embedding model (Administration → AI)." : "Once more documents are added, related ones show up here."}
      </EmptyState>
    );
  return (
    <ul className="space-y-1">
      {items.map(({ document: d, reason, score }) => (
        <li key={d.id}>
          <Link to="/documents/$id" params={{ id: d.id }} className="flex gap-3 rounded-lg p-2 hover:bg-surface-2">
            <Thumbnail doc={d} className="h-14 w-10 shrink-0 rounded border border-border" />
            <div className="min-w-0 flex-1">
              <div className="line-clamp-2 text-sm font-medium leading-snug">{d.title}</div>
              <div className="truncate text-xs text-muted">{[d.correspondent?.name, formatDocDate(d.document_date)].filter(Boolean).join(" · ")}</div>
              <div className="mt-0.5 text-[11px] text-subtle">{reason === "meaning" ? `Similar content · ${Math.round(score * 100)}%` : "Same sender or type"}</div>
            </div>
          </Link>
        </li>
      ))}
    </ul>
  );
}

