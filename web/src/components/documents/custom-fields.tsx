import * as React from "react";
import { BellRing, CalendarPlus } from "lucide-react";
import { useCustomFields } from "@/lib/queries";
import type { CustomField, CustomValue } from "@/lib/types";
import { cn, downloadIcs } from "@/lib/utils";
import { Field, Input, Select, Textarea } from "@/components/ui/input";
import { DateInput } from "@/components/ui/date-input";
import { Switch } from "@/components/ui/misc";

/** Typed editors for the custom fields of the document's space (Amount, Due date, …). Saves per field. */
export function CustomFieldsEditor({ docTitle, spaceId, values, canEdit, onSave }: {
  docTitle: string;
  spaceId: string;
  values: CustomValue[] | undefined;
  canEdit: boolean;
  onSave: (fieldId: string, value: unknown) => void;
}) {
  const fields = useCustomFields(spaceId);
  const list = (fields.data ?? []).filter((f) => f.space_id === spaceId);
  if (!list.length) return null;
  const byId = new Map((values ?? []).map((v) => [v.field_id, v]));
  return (
    <div className="space-y-4">
      {list.map((f) => (
        <FieldEditor key={f.id} docTitle={docTitle} field={f} value={byId.get(f.id)?.value} canEdit={canEdit} onSave={(v) => onSave(f.id, v)} />
      ))}
    </div>
  );
}

function FieldEditor({ docTitle, field, value, canEdit, onSave }: { docTitle: string; field: CustomField; value: unknown; canEdit: boolean; onSave: (v: unknown) => void }) {
  const id = `cf-${field.id}`;
  const label = field.data_type === "monetary" && field.options.currency ? `${field.name} (${field.options.currency})` : field.name;
  const [text, setText] = React.useState(display(value));
  React.useEffect(() => setText(display(value)), [value]);
  const commit = () => text !== display(value) && onSave(text.trim() === "" ? null : text);

  switch (field.data_type) {
    case "boolean":
      return (
        <div className="flex items-center justify-between gap-3">
          <label htmlFor={id} className="text-[13px] font-medium">
            {field.name}
          </label>
          <Switch id={id} checked={value === true} disabled={!canEdit} onCheckedChange={(v) => onSave(v)} />
        </div>
      );
    case "date":
      return (
        <Field label={label} htmlFor={id}>
          <DateInput id={id} value={typeof value === "string" ? value : null} disabled={!canEdit} onChange={(v) => onSave(v)} />
          {typeof value === "string" && (
            <div className="flex items-center justify-between gap-2 text-xs text-muted">
              <span className="flex min-w-0 items-center gap-1">
                {field.remind && (
                  <>
                    <BellRing className="size-3.5 shrink-0 text-accent" />
                    <span className="truncate">Reminds {field.remind_days.map((d) => (d ? `${d}d` : "on the day")).join(", ")} before</span>
                  </>
                )}
              </span>
              <button type="button" className="flex shrink-0 items-center gap-1 rounded px-1 py-0.5 font-medium text-accent hover:bg-accent-soft"
                onClick={() => downloadIcs({ title: `${docTitle}: ${field.name}`, date: value, url: location.href,
                  description: `${field.name} of “${docTitle}”, from Docveta.`, alarmDays: field.remind ? field.remind_days : [7] })}>
                <CalendarPlus className="size-3.5" /> Add to calendar
              </button>
            </div>
          )}
        </Field>
      );
    case "select":
      return (
        <Field label={label} htmlFor={id}>
          <Select id={id} value={typeof value === "string" ? value : ""} disabled={!canEdit} onChange={(e) => onSave(e.target.value || null)}>
            <option value="">—</option>
            {(field.options.choices ?? []).map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </Select>
        </Field>
      );
    case "multiselect": {
      const chosen = Array.isArray(value) ? (value as string[]) : [];
      return (
        <Field label={label}>
          <div className="flex flex-wrap gap-1.5">
            {(field.options.choices ?? []).map((c) => {
              const on = chosen.includes(c);
              return (
                <button
                  key={c}
                  type="button"
                  disabled={!canEdit}
                  aria-pressed={on}
                  onClick={() => onSave(on ? chosen.filter((x) => x !== c) : [...chosen, c])}
                  className={cn("rounded-full border px-2.5 py-1 text-xs transition-colors disabled:opacity-60", on ? "border-accent bg-accent-soft font-medium text-accent-soft-fg" : "border-border text-muted hover:bg-surface-2")}
                >
                  {c}
                </button>
              );
            })}
          </div>
        </Field>
      );
    }
    case "longtext":
      return (
        <Field label={label} htmlFor={id}>
          <Textarea id={id} value={text} disabled={!canEdit} onChange={(e) => setText(e.target.value)} onBlur={commit} />
        </Field>
      );
    case "integer":
    case "decimal":
    case "monetary":
      return (
        <Field label={label} htmlFor={id}>
          <Input id={id} inputMode="decimal" value={text} disabled={!canEdit} onChange={(e) => setText(e.target.value)} onBlur={commit} onKeyDown={(e) => e.key === "Enter" && commit()} className="tabular-nums" />
        </Field>
      );
    default:
      return (
        <Field label={label} htmlFor={id}>
          <Input id={id} type={field.data_type === "url" ? "url" : "text"} value={text} disabled={!canEdit} onChange={(e) => setText(e.target.value)} onBlur={commit} onKeyDown={(e) => e.key === "Enter" && commit()} />
        </Field>
      );
  }
}

function display(v: unknown): string {
  return v === null || v === undefined ? "" : typeof v === "string" || typeof v === "number" ? String(v) : "";
}
