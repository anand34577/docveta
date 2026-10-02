import * as React from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { BookmarkPlus, FileSearch, FileText, Trash2, Upload } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { keys } from "@/lib/queries";
import type { DocQuery, Document, SavedView } from "@/lib/types";
import { useUI } from "@/stores/ui";
import { PageHeader, useCurrentUser, useFilePicker } from "@/components/app-shell";
import { FilterBar } from "@/components/documents/filters";
import { DocumentResults } from "@/components/documents/doc-results";
import { BulkBar } from "@/components/documents/bulk-bar";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/misc";
import { Dialog, DialogContent, DialogFooter } from "@/components/ui/overlay";
import { Field, Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/misc";
import { parseDocQuery } from "@/router";

export function DocumentsPage({ trash }: { trash?: boolean }) {
  const raw = useSearch({ strict: false }) as Record<string, unknown>;
  const query: DocQuery = React.useMemo(() => ({ ...parseDocQuery(trash ? {} : raw), trash: trash || undefined }), [raw, trash]);
  const navigate = useNavigate();
  return (
    <DocumentBrowser
      query={query}
      onChange={(q) => navigate({ to: trash ? "/trash" : "/documents", search: trash ? {} : (q as Record<string, unknown>), replace: true })}
      trash={trash}
    />
  );
}

interface BrowserProps {
  query: DocQuery;
  onChange: (q: DocQuery) => void;
  trash?: boolean;
  title?: React.ReactNode;
  headerActions?: React.ReactNode;
}

/** Shared by All documents, Trash, Saved views and space pages. */
export function DocumentBrowser({ query, onChange, trash, title, headerActions }: BrowserProps) {
  const me = useCurrentUser();
  const layout = useUI((s) => s.layout);
  const pick = useFilePicker();
  const [info, setInfo] = React.useState<{ total?: number; items: Document[] }>({ items: [] });
  const [saveOpen, setSaveOpen] = React.useState(false);
  const onLoaded = React.useCallback((i: { total?: number; items: Document[] }) => setInfo(i), []);
  const spaceName = query.space_id?.length === 1 ? me.spaces.find((s) => s.id === query.space_id![0]) : undefined;
  const filtered = !!(query.q || query.tag_id || query.correspondent_id || query.document_type_id || query.date_from || query.date_to || query.untagged || query.status);

  const heading = title ?? (trash ? "Trash" : spaceName ? (spaceName.kind === "personal" ? "Personal" : spaceName.name) : "All documents");

  return (
    <div>
      <PageHeader
        title={heading}
        description={trash ? "Documents here are deleted permanently after 30 days." : undefined}
        actions={
          headerActions ??
          (!trash && (
            <>
              {filtered && (
                <Button size="sm" variant="ghost" onClick={() => setSaveOpen(true)}>
                  <BookmarkPlus /> Save view
                </Button>
              )}
              {spaceName && spaceName.role === "owner" && (
                <Button size="sm" variant="ghost" asChild>
                  <a href={`/spaces/${spaceName.id}/general`}>Space settings</a>
                </Button>
              )}
            </>
          ))
        }
      />
      <FilterBar query={query} onChange={onChange} total={info.total} hideSpace={trash} />
      <DocumentResults
        query={query}
        layout={layout}
        onLoaded={onLoaded}
        empty={
          trash ? (
            <EmptyState icon={<Trash2 />} title="Trash is empty" />
          ) : filtered ? (
            <EmptyState icon={<FileSearch />} title="No matching documents">
              Try different words or remove some filters. Search also matches text inside scanned documents.
            </EmptyState>
          ) : (
            <EmptyState
              icon={<FileText />}
              title="No documents yet"
              action={
                <Button variant="primary" onClick={() => pick()}>
                  <Upload /> Upload documents
                </Button>
              }
            >
              Drag files anywhere on this page, or upload from your phone. Docveta reads the text so you can find everything later.
            </EmptyState>
          )
        }
      />
      <BulkBar items={info.items} trash={trash} />
      {saveOpen && <SaveViewDialog query={query} onClose={() => setSaveOpen(false)} />}
    </div>
  );
}

function SaveViewDialog({ query, onClose }: { query: DocQuery; onClose: () => void }) {
  const me = useCurrentUser();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const spaceId = query.space_id?.length === 1 ? query.space_id[0] : undefined;
  const space = me.spaces.find((s) => s.id === spaceId);
  const [name, setName] = React.useState(query.q ?? "");
  const [share, setShare] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      const { trash: _t, ...q } = query;
      void _t;
      const v = await api.post<SavedView>("/saved-views", { name, query: q, space_id: share && spaceId ? spaceId : null, pinned: true });
      qc.invalidateQueries({ queryKey: keys.views });
      toast.success("View saved to the sidebar");
      onClose();
      navigate({ to: "/views/$id", params: { id: v.id } });
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title="Save this view" description="Saved views appear in the sidebar and stay up to date as documents are added." size="sm">
        <form onSubmit={save} className="space-y-4">
          <Field label="Name" htmlFor="vname">
            <Input id="vname" required value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Tax FY 2025–26" autoFocus />
          </Field>
          {space && space.kind === "shared" && space.role !== "viewer" && (
            <label className="flex items-center gap-2.5 text-sm">
              <Checkbox checked={share} onCheckedChange={(v) => setShare(!!v)} />
              Share with everyone in {space.name}
            </label>
          )}
          <DialogFooter>
            <Button onClick={onClose}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy}>
              Save view
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
