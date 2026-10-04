import * as React from "react";
import { toast } from "sonner";
import { AlertTriangle, Check, ChevronDown, Hash, Loader2, MapPin } from "lucide-react";
import { ApiError, errorMessage } from "@/lib/api";
import { useUpdateDocument, type DocPatch } from "@/lib/queries";
import type { Document } from "@/lib/types";
import { formatBytes, formatDateTime, spaceLabel } from "@/lib/utils";
import { Field, Input, NativeSelect } from "@/components/ui/input";
import { EntityPicker } from "@/components/ui/entity-picker";
import { Button } from "@/components/ui/button";
import { DateInput } from "@/components/ui/date-input";
import { useCurrentUser } from "@/components/app-shell";
import { api } from "@/lib/api";
import { useQueryClient } from "@tanstack/react-query";
import { keys } from "@/lib/queries";
import { stageLabel } from "./doc-items";
import { Suggestions } from "./suggestions";
import { CustomFieldsEditor } from "./custom-fields";
import { UnlockDialog } from "./unlock-dialog";

const languages: [string, string][] = [
  ["en", "English"], ["hi", "Hindi"], ["mr", "Marathi"], ["bn", "Bengali"], ["gu", "Gujarati"], ["ta", "Tamil"], ["te", "Telugu"],
  ["kn", "Kannada"], ["ml", "Malayalam"], ["pa", "Punjabi"], ["ur", "Urdu"], ["de", "German"], ["fr", "French"], ["es", "Spanish"],
];

type SaveState = "idle" | "saving" | "saved" | "error";

/**
 * Editable metadata with autosave — no Save button. Each field saves on change (text
 * fields on blur / after a pause). Conflicting edits by someone else are detected via
 * the document version (If-Match) and reported instead of silently overwritten.
 */
export function MetadataPanel({ doc }: { doc: Document }) {
  const me = useCurrentUser();
  const space = me.spaces.find((s) => s.id === doc.space.id);
  const canEdit = !!space && space.role !== "viewer" && !doc.deleted_at;
  const update = useUpdateDocument(doc.id);
  const qc = useQueryClient();
  const [state, setState] = React.useState<SaveState>("idle");
  const [title, setTitle] = React.useState(doc.title);
  const [location, setLocation] = React.useState(doc.physical_location);
  React.useEffect(() => setTitle(doc.title), [doc.title]);
  React.useEffect(() => setLocation(doc.physical_location), [doc.physical_location]);

  const save = React.useCallback(
    async (patch: DocPatch) => {
      setState("saving");
      try {
        await update.mutateAsync({ patch, version: doc.version });
        setState("saved");
        setTimeout(() => setState((s) => (s === "saved" ? "idle" : s)), 1500);
      } catch (e) {
        setState("error");
        if (e instanceof ApiError && e.status === 412) {
          toast.error("Someone else just changed this document", {
            description: "We've loaded their changes. Please make your edit again.",
          });
          qc.invalidateQueries({ queryKey: keys.document(doc.id) });
        } else {
          toast.error(errorMessage(e));
        }
      }
    },
    [update, doc.version, doc.id, qc],
  );

  // Title autosave after a short pause.
  React.useEffect(() => {
    if (title.trim() === doc.title || !title.trim()) return;
    const t = setTimeout(() => save({ title: title.trim() }), 800);
    return () => clearTimeout(t);
  }, [title]);

  const assignASN = async () => {
    try {
      const d = await api.post<Document>(`/documents/${doc.id}/asn`);
      qc.setQueryData(keys.document(doc.id), d);
      toast.success(`Archive number ${d.asn} assigned`, { description: "Write it on the paper original so you can find it later." });
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  return (
    <div className="space-y-4">
      {doc.status !== "ready" && <ProcessingNotice doc={doc} canEdit={canEdit} />}
      {canEdit && <Suggestions docId={doc.id} count={doc.suggestion_count} />}

      <Field
        label={
          <span className="flex items-center justify-between">
            Title
            <span className="flex items-center gap-1 text-xs font-normal text-subtle" aria-live="polite">
              {state === "saving" && (
                <>
                  <Loader2 className="size-3 animate-spin" /> Saving…
                </>
              )}
              {state === "saved" && (
                <span className="flex items-center gap-1 text-success">
                  <Check className="size-3" /> Saved
                </span>
              )}
              {state === "idle" && canEdit && "Autosaves"}
            </span>
          </span>
        }
        htmlFor="m-title"
      >
        <Input
          id="m-title"
          value={title}
          disabled={!canEdit}
          onChange={(e) => setTitle(e.target.value)}
          onBlur={() => title.trim() && title.trim() !== doc.title && save({ title: title.trim() })}
        />
      </Field>

      <Field label="Date on the document" htmlFor="m-date">
        <DateInput id="m-date" value={doc.document_date} disabled={!canEdit} onChange={(v) => save({ document_date: v })} />
      </Field>

      <Field label="Who is it from?" htmlFor="m-corr">
        <EntityPicker
          id="m-corr"
          kind="correspondents"
          spaceId={doc.space.id}
          value={doc.correspondent ? [doc.correspondent.id] : []}
          onChange={(v) => save({ correspondent_id: v[0] ?? null })}
          placeholder="e.g. HDFC Bank, BESCOM"
          disabled={!canEdit}
        />
      </Field>

      <Field label="What is it?" htmlFor="m-type">
        <EntityPicker
          id="m-type"
          kind="document-types"
          spaceId={doc.space.id}
          value={doc.document_type ? [doc.document_type.id] : []}
          onChange={(v) => save({ document_type_id: v[0] ?? null })}
          placeholder="e.g. Bill, Invoice, Certificate"
          disabled={!canEdit}
        />
      </Field>

      <Field label="Tags" htmlFor="m-tags">
        <EntityPicker
          id="m-tags"
          kind="tags"
          multiple
          spaceId={doc.space.id}
          value={doc.tags.map((t) => t.id)}
          onChange={(v) => save({ tag_ids: v })}
          placeholder="Add tags"
          disabled={!canEdit}
        />
      </Field>

      <CustomFieldsEditor spaceId={doc.space.id} values={doc.custom_fields} canEdit={canEdit} onSave={(id, v) => save({ custom_fields: { [id]: v } })} />

      <details className="group rounded-lg border border-border">
        <summary className="flex cursor-pointer select-none list-none items-center justify-between px-3 py-2.5 text-sm font-medium text-muted hover:text-fg [&::-webkit-details-marker]:hidden">
          More details
          <ChevronDown className="size-4 transition-transform group-open:rotate-180" />
        </summary>
        <div className="space-y-4 border-t border-border p-3">
          <Field label="Space" htmlFor="m-space" hint="Moving keeps tags and correspondent by matching names in the new space.">
            <NativeSelect
              id="m-space"
              value={doc.space.id}
              disabled={!canEdit}
              onChange={(e) => save({ space_id: e.target.value })}
            >
              {me.spaces
                .filter((s) => s.role !== "viewer" || s.id === doc.space.id)
                .map((s) => (
                  <option key={s.id} value={s.id}>
                    {spaceLabel(s)}
                  </option>
                ))}
            </NativeSelect>
          </Field>
          <Field label="Language" htmlFor="m-lang" hint="Used for text recognition and search.">
            <NativeSelect id="m-lang" value={doc.language} disabled={!canEdit} onChange={(e) => save({ language: e.target.value })}>
              {!languages.some(([c]) => c === doc.language) && <option value={doc.language}>{doc.language}</option>}
              {languages.map(([c, n]) => (
                <option key={c} value={c}>
                  {n}
                </option>
              ))}
            </NativeSelect>
          </Field>
          <Field label="Where is the paper original?" htmlFor="m-loc">
            <div className="relative">
              <MapPin className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-subtle" />
              <Input
                id="m-loc"
                className="pl-9"
                value={location}
                placeholder="e.g. Blue folder, cupboard 2"
                disabled={!canEdit}
                onChange={(e) => setLocation(e.target.value)}
                onBlur={() => location !== doc.physical_location && save({ physical_location: location })}
              />
            </div>
          </Field>
          <div>
            <div className="mb-1.5 text-[13px] font-medium">Archive number (ASN)</div>
            {doc.asn ? (
              <div className="flex items-center gap-2 text-sm">
                <Hash className="size-4 text-subtle" />
                <span className="font-mono">{doc.asn}</span>
              </div>
            ) : (
              <Button size="sm" disabled={!canEdit} onClick={assignASN}>
                Assign next number
              </Button>
            )}
          </div>
        </div>
      </details>

      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 border-t border-border pt-4 text-[13px]">
        <dt className="text-subtle">Added</dt>
        <dd className="text-muted">
          {formatDateTime(doc.added_at)}
          {doc.owner && ` by ${doc.owner.name}`}
        </dd>
        <dt className="text-subtle">File</dt>
        <dd className="truncate text-muted" title={doc.original_filename}>
          {doc.original_filename || "—"}
        </dd>
        <dt className="text-subtle">Size</dt>
        <dd className="text-muted">
          {formatBytes(doc.size_bytes)}
          {doc.page_count ? ` · ${doc.page_count} page${doc.page_count > 1 ? "s" : ""}` : ""}
        </dd>
        <dt className="text-subtle">Searchable</dt>
        <dd className="text-muted">{doc.has_archive ? "Yes, with text layer" : doc.status === "ready" ? "Yes" : "Not yet"}</dd>
      </dl>
    </div>
  );
}

function ProcessingNotice({ doc, canEdit }: { doc: Document; canEdit: boolean }) {
  const [unlocking, setUnlocking] = React.useState(false);
  if (doc.status === "processing") {
    return (
      <div className="flex items-start gap-2.5 rounded-lg bg-accent-soft px-3 py-2.5 text-sm text-accent-soft-fg">
        <Loader2 className="mt-0.5 size-4 shrink-0 animate-spin" />
        <div>
          <div className="font-medium">{stageLabel(doc.processing_stage)}…</div>
          <div className="text-[13px] opacity-80">You can already view, download and edit this document.</div>
        </div>
      </div>
    );
  }
  return (
    <div className="flex items-start gap-2.5 rounded-lg bg-warning-soft px-3 py-2.5 text-sm text-warning">
      <AlertTriangle className="mt-0.5 size-4 shrink-0" />
      <div>
        <div className="font-medium">{doc.status === "needs_password" ? "Password protected" : "Couldn't read this document"}</div>
        <div className="text-[13px] opacity-90">{doc.processing_error}</div>
        {doc.status === "needs_password" && canEdit && (
          <Button size="sm" className="mt-2" onClick={() => setUnlocking(true)}>
            Enter password…
          </Button>
        )}
      </div>
      {unlocking && <UnlockDialog doc={doc} onClose={() => setUnlocking(false)} />}
    </div>
  );
}
