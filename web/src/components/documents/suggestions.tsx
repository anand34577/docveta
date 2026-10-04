import * as React from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Check, Sparkles, X } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { invalidateDocuments, useSuggestions } from "@/lib/queries";
import type { AISuggestion } from "@/lib/types";
import { cn, formatDocDate } from "@/lib/utils";
import { Button } from "@/components/ui/button";

const fieldLabel: Record<AISuggestion["field"], string> = {
  tag: "Tag",
  correspondent: "From",
  document_type: "Type",
  document_date: "Date",
  title: "Title",
  custom_field: "",
};

function label(s: AISuggestion): { name: string; text: string; isNew: boolean } {
  const v = s.value as { name?: string; field?: string; new?: boolean };
  const text = s.field === "document_date" ? formatDocDate(v.name) : (v.name ?? "");
  return { name: s.field === "custom_field" ? (v.field ?? "Field") : fieldLabel[s.field], text, isNew: !!v.new };
}

/**
 * AI suggestions for a document: nothing is applied until you accept. One click takes
 * them all, or accept and dismiss them one by one.
 */
export function Suggestions({ docId, count, className }: { docId: string; count?: number; className?: string }) {
  const q = useSuggestions(docId, (count ?? 1) > 0);
  const qc = useQueryClient();
  const [busy, setBusy] = React.useState(false);
  const list = q.data ?? [];
  if (!list.length) return null;

  const resolve = async (accept: boolean, ids?: string[]) => {
    setBusy(true);
    try {
      await api.post(`/documents/${docId}/suggestions/${accept ? "accept" : "reject"}`, { ids });
      invalidateDocuments(qc, docId);
      qc.invalidateQueries({ queryKey: ["document", docId, "suggestions"] });
      qc.invalidateQueries({ queryKey: ["document", docId, "history"] });
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className={cn("rounded-lg border border-accent/30 bg-accent-soft/50 p-3", className)}>
      <div className="flex items-center gap-2 text-sm font-medium text-accent-soft-fg">
        <Sparkles className="size-4" /> Suggested by AI
        <span className="ml-auto flex gap-1">
          <Button size="sm" variant="primary" disabled={busy} onClick={() => resolve(true)}>
            <Check /> Accept all
          </Button>
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => resolve(false)} title="Dismiss all suggestions">
            Dismiss
          </Button>
        </span>
      </div>
      <ul className="mt-2.5 flex flex-wrap gap-1.5">
        {list.map((s) => {
          const l = label(s);
          return (
            <li key={s.id} className="flex items-center overflow-hidden rounded-md border border-border bg-surface text-[13px] shadow-sm">
              <span className="px-2 py-1">
                {l.name && <span className="text-subtle">{l.name}: </span>}
                <span className="font-medium">{l.text}</span>
                {l.isNew && <span className="ml-1 text-xs text-accent">new</span>}
                <span className="ml-1.5 text-[11px] tabular-nums text-subtle" title="How sure the AI is">
                  {Math.round(s.confidence * 100)}%
                </span>
              </span>
              <button className="border-l border-border px-1.5 py-1.5 text-success hover:bg-success-soft" aria-label={`Accept ${l.text}`} disabled={busy} onClick={() => resolve(true, [s.id])}>
                <Check className="size-3.5" />
              </button>
              <button className="border-l border-border px-1.5 py-1.5 text-muted hover:bg-danger-soft hover:text-danger" aria-label={`Dismiss ${l.text}`} disabled={busy} onClick={() => resolve(false, [s.id])}>
                <X className="size-3.5" />
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
