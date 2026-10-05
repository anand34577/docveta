import * as React from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, ListChecks, Pencil, Play, Plus, Printer, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError, errorMessage } from "@/lib/api";
import { keys, useCustomFields, useWorkflows } from "@/lib/queries";
import type { CustomField, FieldType, Space, Workflow, WorkflowAction, WorkflowRun } from "@/lib/types";
import { cn, colorNames, formatDateTime, tagDot, timeAgo } from "@/lib/utils";
import { useCurrentUser } from "@/components/app-shell";
import { SettingsCard } from "@/components/settings-layout";
import { Button } from "@/components/ui/button";
import { Field, Input, NativeSelect, Textarea } from "@/components/ui/input";
import { Badge, EmptyState, Skeleton, Switch, SwitchRow } from "@/components/ui/misc";
import { Dialog, DialogContent, DialogFooter } from "@/components/ui/overlay";
import { EntityPicker } from "@/components/ui/entity-picker";
import { confirm } from "@/components/ui/confirm";

function useSaveSpace(space: Space) {
  const qc = useQueryClient();
  return async (patch: Record<string, unknown>, quiet = false) => {
    try {
      await api.patch(`/spaces/${space.id}`, patch);
      await qc.invalidateQueries({ queryKey: keys.me });
      if (!quiet) toast.success("Saved");
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };
}

/* ------------------------------------------------------------------ Colour */

export function SpaceColor({ space }: { space: Space }) {
  const save = useSaveSpace(space);
  const owner = space.role === "owner";
  return (
    <Field label="Colour" hint="Shown next to this space's name in the sidebar, filters and document lists.">
      <div className="flex flex-wrap gap-2">
        {colorNames.map((c) => (
          <button
            key={c}
            type="button"
            disabled={!owner}
            aria-label={c}
            aria-pressed={space.color === c}
            onClick={() => save({ color: c }, true)}
            className={cn("flex size-7 items-center justify-center rounded-full text-white ring-offset-2 ring-offset-surface transition disabled:opacity-60", tagDot[c], space.color === c && "ring-2 ring-fg")}
          >
            {space.color === c && <Check className="size-4" />}
          </button>
        ))}
      </div>
    </Field>
  );
}

/* ------------------------------------------------------------------ AI */

export function SpaceAI({ space }: { space: Space }) {
  const save = useSaveSpace(space);
  const owner = space.role === "owner";
  const stats = useQuery({
    queryKey: ["ai-stats", space.id],
    queryFn: () => api.get<{ accepted: number; rejected: number; pending: number; accept_rate: number }>(`/spaces/${space.id}/ai-stats`),
    retry: false,
  });
  return (
    <>
      <SettingsCard title="AI assistance" description="Suggest tags, sender, type, date and custom fields for new documents. You always see what it would change first, unless you pick automatic.">
        <div className="grid max-w-md gap-4">
          <Field label="Use AI for this space" htmlFor="ai-policy" hint="“Local only” sends text only to providers marked as running on your own hardware.">
            <NativeSelect id="ai-policy" disabled={!owner} value={space.ai_policy} onChange={(e) => save({ ai_policy: e.target.value })}>
              <option value="off">Off</option>
              <option value="local_only">Only with a local provider</option>
              <option value="any">Any configured provider</option>
            </NativeSelect>
          </Field>
          {space.ai_policy !== "off" && (
            <Field label="When AI has suggestions" htmlFor="ai-mode">
              <NativeSelect id="ai-mode" disabled={!owner} value={space.ai_apply_mode} onChange={(e) => save({ ai_apply_mode: e.target.value })}>
                <option value="suggest">Ask me first (shown in the Inbox)</option>
                <option value="auto">Apply automatically</option>
              </NativeSelect>
            </Field>
          )}
        </div>
      </SettingsCard>
      {space.ai_policy !== "off" && stats.data && (
        <SettingsCard title="How well is it doing?" description="Based on the suggestions you've accepted or dismissed.">
          <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
            {[
              ["Accepted", stats.data.accepted],
              ["Dismissed", stats.data.rejected],
              ["Waiting", stats.data.pending],
              ["Accept rate", `${Math.round(stats.data.accept_rate * 100)}%`],
            ].map(([k, v]) => (
              <div key={k as string}>
                <dt className="text-xs text-subtle">{k}</dt>
                <dd className="text-2xl font-semibold tabular-nums">{v}</dd>
              </div>
            ))}
          </dl>
        </SettingsCard>
      )}
    </>
  );
}

/* ------------------------------------------------------------------ Scanning */

export function SpaceScanning({ space }: { space: Space }) {
  const save = useSaveSpace(space);
  const owner = space.role === "owner";
  const [asn, setAsn] = React.useState("1");
  return (
    <>
      <SettingsCard title="Batch scanning" description="Scan a whole stack in one go and let Docveta cut it into documents.">
        <div className="divide-y divide-border">
          <SwitchRow
            label="Split at separator sheets"
            description="Put a printed separator page between documents. Docveta splits the scan there and drops the separator."
            checked={!!space.split_on_separators}
            disabled={!owner}
            onCheckedChange={(v) => save({ split_on_separators: v })}
          />
          <SwitchRow
            label="Read archive-number barcodes"
            description="If a page carries an ASN label (see below), that number becomes the document's archive number."
            checked={!!space.read_asn_barcodes}
            disabled={!owner}
            onCheckedChange={(v) => save({ read_asn_barcodes: v })}
          />
        </div>
      </SettingsCard>
      <SettingsCard title="Print sheets and labels" description="Print on plain paper. Separators work from any scanner that can scan a stack to one PDF.">
        <div className="flex flex-wrap items-end gap-3">
          <Button asChild>
            <a href="/api/v1/barcodes/separator.png" target="_blank" rel="noreferrer">
              <Printer /> Separator sheet
            </a>
          </Button>
          <Field label="Archive-number label" htmlFor="asn-n" className="w-32">
            <Input id="asn-n" type="number" min={1} value={asn} onChange={(e) => setAsn(e.target.value)} />
          </Field>
          <Button asChild>
            <a href={`/api/v1/barcodes/asn.png?n=${Number(asn) || 1}`} target="_blank" rel="noreferrer">
              <Printer /> Label for {Number(asn) || 1}
            </a>
          </Button>
        </div>
      </SettingsCard>
    </>
  );
}

/* ------------------------------------------------------------------ Custom fields */

const fieldTypes: [FieldType, string][] = [
  ["text", "Short text"],
  ["longtext", "Long text"],
  ["integer", "Whole number"],
  ["decimal", "Number with decimals"],
  ["monetary", "Amount of money"],
  ["date", "Date"],
  ["boolean", "Yes / No"],
  ["url", "Web address"],
  ["select", "One choice from a list"],
  ["multiselect", "Several choices from a list"],
];

export function SpaceFields({ space }: { space: Space }) {
  const fields = useCustomFields(space.id);
  const qc = useQueryClient();
  const owner = space.role === "owner";
  const [editing, setEditing] = React.useState<CustomField | "new" | null>(null);
  const list = (fields.data ?? []).filter((f) => f.space_id === space.id);
  const remove = async (f: CustomField) => {
    if (!(await confirm({ title: `Delete “${f.name}”?`, body: `Its value is removed from ${f.document_count} document${f.document_count === 1 ? "" : "s"}.`, confirmLabel: "Delete field", destructive: true }))) return;
    await api.del(`/custom-fields/${f.id}`).catch((e) => toast.error(errorMessage(e)));
    qc.invalidateQueries({ queryKey: ["custom-fields"] });
  };
  return (
    <SettingsCard
      title="Custom fields"
      description="Extra details to record on documents, such as Amount or Due date. Filter and sort by them in Documents (for example amount:>1500)."
      actions={
        owner && (
          <Button size="sm" variant="primary" onClick={() => setEditing("new")}>
            <Plus /> Add field
          </Button>
        )
      }
    >
      {fields.isLoading ? (
        <Skeleton className="h-16" />
      ) : list.length === 0 ? (
        <EmptyState icon={<ListChecks />} title="No custom fields" className="py-8">
          Add “Amount”, “Due date” or “Policy number” and fill them in on each document.
        </EmptyState>
      ) : (
        <ul className="divide-y divide-border">
          {list.map((f) => (
            <li key={f.id} className="flex items-center gap-3 py-3">
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium">{f.name}</div>
                <div className="text-xs text-subtle">
                  {fieldTypes.find(([t]) => t === f.data_type)?.[1] ?? f.data_type} · used on {f.document_count} document{f.document_count === 1 ? "" : "s"}
                  {f.options.choices?.length ? ` · ${f.options.choices.join(", ")}` : ""}
                </div>
              </div>
              {owner && (
                <>
                  <Button size="icon-sm" variant="ghost" aria-label={`Edit ${f.name}`} onClick={() => setEditing(f)}>
                    <Pencil />
                  </Button>
                  <Button size="icon-sm" variant="ghost" aria-label={`Delete ${f.name}`} onClick={() => remove(f)}>
                    <Trash2 />
                  </Button>
                </>
              )}
            </li>
          ))}
        </ul>
      )}
      {editing && <FieldDialog spaceId={space.id} field={editing === "new" ? null : editing} onClose={() => setEditing(null)} />}
    </SettingsCard>
  );
}

function FieldDialog({ spaceId, field, onClose }: { spaceId: string; field: CustomField | null; onClose: () => void }) {
  const qc = useQueryClient();
  const [name, setName] = React.useState(field?.name ?? "");
  const [type, setType] = React.useState<FieldType>(field?.data_type ?? "text");
  const [choices, setChoices] = React.useState((field?.options.choices ?? []).join("\n"));
  const [currency, setCurrency] = React.useState(field?.options.currency ?? "INR");
  const [err, setErr] = React.useState<ApiError | null>(null);
  const [busy, setBusy] = React.useState(false);
  const listType = type === "select" || type === "multiselect";
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    const options: Record<string, unknown> = {};
    if (listType) options.choices = choices.split("\n").map((c) => c.trim()).filter(Boolean);
    if (type === "monetary") options.currency = currency.trim().toUpperCase();
    try {
      if (field) await api.patch(`/custom-fields/${field.id}`, { name, options });
      else await api.post("/custom-fields", { space_id: spaceId, name, data_type: type, options });
      qc.invalidateQueries({ queryKey: ["custom-fields"] });
      onClose();
    } catch (e) {
      setErr(e as ApiError);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={field ? "Edit field" : "New custom field"} size="sm">
        <form onSubmit={submit} className="space-y-4">
          <Field label="Name" htmlFor="cf-name" error={err?.fieldError("name")}>
            <Input id="cf-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Amount, Due date" required autoFocus />
          </Field>
          <Field label="Type" htmlFor="cf-type" hint={field ? "The type can't change after creation." : undefined}>
            <NativeSelect id="cf-type" value={type} disabled={!!field} onChange={(e) => setType(e.target.value as FieldType)}>
              {fieldTypes.map(([t, l]) => (
                <option key={t} value={t}>
                  {l}
                </option>
              ))}
            </NativeSelect>
          </Field>
          {type === "monetary" && (
            <Field label="Currency code" htmlFor="cf-cur" hint="For example INR, USD, EUR.">
              <Input id="cf-cur" value={currency} maxLength={3} onChange={(e) => setCurrency(e.target.value)} className="w-24 uppercase" />
            </Field>
          )}
          {listType && (
            <Field label="Choices" htmlFor="cf-choices" hint="One per line." error={err?.fieldError("options")}>
              <Textarea id="cf-choices" rows={4} value={choices} onChange={(e) => setChoices(e.target.value)} />
            </Field>
          )}
          {err && !err.fields.length && <p className="text-sm text-danger">{err.message}</p>}
          <DialogFooter>
            <Button onClick={onClose}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy}>
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/* ------------------------------------------------------------------ Workflows */

const triggers: Record<Workflow["trigger"], string> = {
  added: "a document is added",
  processed: "a document finishes processing (text is read)",
  updated: "a document is changed",
  schedule: "every day at a set time",
};

const actionLabels: Record<WorkflowAction["type"], string> = {
  add_tags: "Add tags",
  remove_tags: "Remove tags",
  set_correspondent: "Set who it's from",
  set_document_type: "Set the document type",
  set_field: "Set a custom field",
  set_inbox: "Put in or take out of the Inbox",
  move_to_space: "Move to another space",
  notify: "Send a notification",
  webhook: "Call a webhook",
  run_ai: "Ask AI for suggestions",
};

export function SpaceWorkflows({ space }: { space: Space }) {
  const wf = useWorkflows(space.id);
  const qc = useQueryClient();
  const owner = space.role === "owner";
  const [editing, setEditing] = React.useState<Workflow | "new" | null>(null);
  const [runs, setRuns] = React.useState<Workflow | null>(null);
  const list = (wf.data ?? []).filter((w) => w.space_id === space.id);
  const refresh = () => qc.invalidateQueries({ queryKey: ["workflows"] });
  const toggle = async (w: Workflow, enabled: boolean) => {
    await api.patch(`/workflows/${w.id}`, { enabled }).catch((e) => toast.error(errorMessage(e)));
    refresh();
  };
  const remove = async (w: Workflow) => {
    if (!(await confirm({ title: `Delete “${w.name}”?`, confirmLabel: "Delete", destructive: true }))) return;
    await api.del(`/workflows/${w.id}`).catch((e) => toast.error(errorMessage(e)));
    refresh();
  };
  return (
    <SettingsCard
      title="Workflows"
      description="Do things automatically. For example: when a document from HDFC is added, tag it “Bank” and set its type to Statement."
      actions={
        owner && (
          <Button size="sm" variant="primary" onClick={() => setEditing("new")}>
            <Plus /> New workflow
          </Button>
        )
      }
    >
      {wf.isLoading ? (
        <Skeleton className="h-16" />
      ) : list.length === 0 ? (
        <EmptyState icon={<Play />} title="No workflows yet" className="py-8" />
      ) : (
        <ul className="divide-y divide-border">
          {list.map((w) => (
            <li key={w.id} className="flex flex-wrap items-center gap-3 py-3">
              <Switch checked={w.enabled} disabled={!owner} onCheckedChange={(v) => toggle(w, v)} aria-label={`Enable ${w.name}`} />
              <div className="min-w-48 flex-1">
                <div className="text-sm font-medium">{w.name}</div>
                <div className="text-xs text-subtle">
                  When {triggers[w.trigger]}
                  {w.trigger === "schedule" && ` (${w.schedule_time})`} · {w.actions.length} action{w.actions.length === 1 ? "" : "s"} · ran {w.run_count} time{w.run_count === 1 ? "" : "s"}
                  {w.last_run_at && `, last ${timeAgo(w.last_run_at)}`}
                </div>
              </div>
              <div className="ml-auto flex items-center gap-1">
                <Button size="sm" variant="ghost" onClick={() => setRuns(w)}>
                  History
                </Button>
                {owner && (
                  <>
                    <Button size="icon-sm" variant="ghost" aria-label={`Edit ${w.name}`} onClick={() => setEditing(w)}>
                      <Pencil />
                    </Button>
                    <Button size="icon-sm" variant="ghost" aria-label={`Delete ${w.name}`} onClick={() => remove(w)}>
                      <Trash2 />
                    </Button>
                  </>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      {editing && <WorkflowDialog space={space} workflow={editing === "new" ? null : editing} onClose={() => setEditing(null)} onSaved={refresh} />}
      {runs && <RunsDialog workflow={runs} onClose={() => setRuns(null)} />}
    </SettingsCard>
  );
}

interface Cond {
  q?: string;
  correspondent_ids?: string[];
  type_ids?: string[];
  any_tag_ids?: string[];
  untagged?: boolean;
}

function WorkflowDialog({ space, workflow, onClose, onSaved }: { space: Space; workflow: Workflow | null; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = React.useState(workflow?.name ?? "");
  const [trigger, setTrigger] = React.useState<Workflow["trigger"]>(workflow?.trigger ?? "added");
  const [time, setTime] = React.useState(workflow?.schedule_time || "09:00");
  const [cond, setCond] = React.useState<Cond>((workflow?.conditions as Cond) ?? {});
  const [actions, setActions] = React.useState<WorkflowAction[]>(workflow?.actions?.length ? workflow.actions : [{ type: "add_tags", names: [] }]);
  const [err, setErr] = React.useState<ApiError | null>(null);
  const [busy, setBusy] = React.useState(false);
  const setAction = (i: number, a: WorkflowAction) => setActions((l) => l.map((x, j) => (j === i ? a : x)));
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    const conditions: Cond = {};
    if (cond.q?.trim()) conditions.q = cond.q.trim();
    if (cond.correspondent_ids?.length) conditions.correspondent_ids = cond.correspondent_ids;
    if (cond.type_ids?.length) conditions.type_ids = cond.type_ids;
    if (cond.any_tag_ids?.length) conditions.any_tag_ids = cond.any_tag_ids;
    if (cond.untagged) conditions.untagged = true;
    const body = { space_id: space.id, name, trigger, schedule_time: trigger === "schedule" ? time : undefined, conditions, actions };
    try {
      if (workflow) await api.patch(`/workflows/${workflow.id}`, body);
      else await api.post("/workflows", body);
      onSaved();
      onClose();
    } catch (e) {
      setErr(e as ApiError);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent size="lg" title={workflow ? "Edit workflow" : "New workflow"}>
        <form onSubmit={submit} className="space-y-5">
          <Field label="Name" htmlFor="wf-name" error={err?.fieldError("name")}>
            <Input id="wf-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. File HDFC statements" required autoFocus />
          </Field>
          <section className="space-y-3 rounded-lg border border-border p-4">
            <h3 className="text-sm font-semibold">When</h3>
            <div className="flex flex-wrap items-center gap-2">
              <NativeSelect aria-label="Trigger" className="max-w-xs" value={trigger} onChange={(e) => setTrigger(e.target.value as Workflow["trigger"])}>
                {(Object.keys(triggers) as Workflow["trigger"][]).map((t) => (
                  <option key={t} value={t}>
                    {triggers[t][0].toUpperCase() + triggers[t].slice(1)}
                  </option>
                ))}
              </NativeSelect>
              {trigger === "schedule" && <Input type="time" aria-label="Time" className="w-32" value={time} onChange={(e) => setTime(e.target.value)} />}
            </div>
            <h3 className="pt-1 text-sm font-semibold">…and the document matches</h3>
            <p className="-mt-2 text-xs text-muted">Leave everything empty to match every document.</p>
            <Field label="Text or filters" htmlFor="wf-q" hint="Same as the search box: words, or things like from:HDFC type:statement">
              <Input id="wf-q" value={cond.q ?? ""} onChange={(e) => setCond({ ...cond, q: e.target.value })} placeholder="e.g. from:HDFC" />
            </Field>
            <div className="grid gap-3 sm:grid-cols-3">
              <Field label="From">
                <EntityPicker kind="correspondents" spaceId={space.id} multiple allowCreate={false} value={cond.correspondent_ids ?? []} onChange={(v) => setCond({ ...cond, correspondent_ids: v })} placeholder="Anyone" />
              </Field>
              <Field label="Type">
                <EntityPicker kind="document-types" spaceId={space.id} multiple allowCreate={false} value={cond.type_ids ?? []} onChange={(v) => setCond({ ...cond, type_ids: v })} placeholder="Any type" />
              </Field>
              <Field label="Has any of these tags">
                <EntityPicker kind="tags" spaceId={space.id} multiple allowCreate={false} value={cond.any_tag_ids ?? []} onChange={(v) => setCond({ ...cond, any_tag_ids: v })} placeholder="Any" />
              </Field>
            </div>
          </section>
          <section className="space-y-3 rounded-lg border border-border p-4">
            <h3 className="text-sm font-semibold">Then</h3>
            {actions.map((a, i) => (
              <ActionRow key={i} space={space} action={a} onChange={(x) => setAction(i, x)} onRemove={actions.length > 1 ? () => setActions((l) => l.filter((_, j) => j !== i)) : undefined} />
            ))}
            <Button size="sm" onClick={() => setActions((l) => [...l, { type: "add_tags", names: [] }])}>
              <Plus /> Add action
            </Button>
          </section>
          {err && <p className="text-sm text-danger">{err.fields.length ? err.fields.map((f) => f.message).join(" · ") : err.message}</p>}
          <DialogFooter>
            <Button onClick={onClose}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy}>
              Save workflow
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function ActionRow({ space, action, onChange, onRemove }: { space: Space; action: WorkflowAction; onChange: (a: WorkflowAction) => void; onRemove?: () => void }) {
  const fields = useCustomFields(space.id);
  const me = useCurrentUser();
  const t = action.type;
  const names = (action.names ?? []).join(", ");
  const field = (fields.data ?? []).find((f) => f.id === action.field_id);
  return (
    <div className="flex flex-wrap items-start gap-2 rounded-md bg-surface-2/60 p-2.5">
      <NativeSelect aria-label="Action" className="w-full sm:w-56" value={t} onChange={(e) => onChange({ type: e.target.value as WorkflowAction["type"] })}>
        {(Object.keys(actionLabels) as WorkflowAction["type"][]).map((k) => (
          <option key={k} value={k}>
            {actionLabels[k]}
          </option>
        ))}
      </NativeSelect>
      <div className="min-w-0 flex-1 basis-56 space-y-2">
        {(t === "add_tags" || t === "remove_tags") && (
          <Input aria-label="Tag names" placeholder="Tag names, comma-separated" value={names} onChange={(e) => onChange({ ...action, names: e.target.value.split(",").map((s) => s.trim()).filter(Boolean) })} />
        )}
        {(t === "set_correspondent" || t === "set_document_type") && <Input aria-label="Name" placeholder="Name (created if missing)" value={action.name ?? ""} onChange={(e) => onChange({ ...action, name: e.target.value })} />}
        {t === "set_field" && (
          <div className="flex flex-wrap gap-2">
            <NativeSelect aria-label="Field" className="w-44" value={action.field_id ?? ""} onChange={(e) => onChange({ ...action, field_id: e.target.value, value: undefined })}>
              <option value="">Choose a field…</option>
              {(fields.data ?? []).filter((f) => f.space_id === space.id).map((f) => (
                <option key={f.id} value={f.id}>
                  {f.name}
                </option>
              ))}
            </NativeSelect>
            {field?.data_type === "boolean" ? (
              <NativeSelect aria-label="Value" className="w-28" value={String(action.value ?? "")} onChange={(e) => onChange({ ...action, value: e.target.value === "true" })}>
                <option value="">—</option>
                <option value="true">Yes</option>
                <option value="false">No</option>
              </NativeSelect>
            ) : (
              <Input aria-label="Value" className="min-w-0 flex-1" placeholder="Value" value={String(action.value ?? "")} onChange={(e) => onChange({ ...action, value: e.target.value })} />
            )}
          </div>
        )}
        {t === "set_inbox" && (
          <NativeSelect aria-label="Inbox" className="w-48" value={String(action.value ?? true)} onChange={(e) => onChange({ ...action, value: e.target.value === "true" })}>
            <option value="true">Put in the Inbox</option>
            <option value="false">Mark as reviewed</option>
          </NativeSelect>
        )}
        {t === "move_to_space" && (
          <NativeSelect aria-label="Space" value={action.space_id ?? ""} onChange={(e) => onChange({ ...action, space_id: e.target.value })}>
            <option value="">Choose a space…</option>
            {me.spaces.filter((s) => s.role !== "viewer" && s.id !== space.id).map((s) => (
              <option key={s.id} value={s.id}>
                {s.kind === "personal" ? "Personal" : s.name}
              </option>
            ))}
          </NativeSelect>
        )}
        {t === "notify" && (
          <>
            <NativeSelect aria-label="Who to notify" className="w-56" value={action.to ?? "space_owners"} onChange={(e) => onChange({ ...action, to: e.target.value as WorkflowAction["to"] })}>
              <option value="owner">The person who added it</option>
              <option value="space_owners">Space owners</option>
              <option value="space_members">Everyone in the space</option>
            </NativeSelect>
            <Input aria-label="Title" placeholder="Title" value={action.title ?? ""} onChange={(e) => onChange({ ...action, title: e.target.value })} />
            <Input aria-label="Message" placeholder="Message (optional)" value={action.message ?? ""} onChange={(e) => onChange({ ...action, message: e.target.value })} />
          </>
        )}
        {t === "webhook" && <Input aria-label="Webhook URL" type="url" placeholder="https://example.com/hook" value={action.url ?? ""} onChange={(e) => onChange({ ...action, url: e.target.value })} />}
        {t === "run_ai" && <p className="pt-2 text-xs text-muted">Needs AI turned on for this space (AI tab).</p>}
      </div>
      {onRemove && (
        <Button size="icon-sm" variant="ghost" aria-label="Remove action" onClick={onRemove}>
          <X />
        </Button>
      )}
    </div>
  );
}

function RunsDialog({ workflow, onClose }: { workflow: Workflow; onClose: () => void }) {
  const runs = useQuery({ queryKey: ["workflow-runs", workflow.id], queryFn: () => api.get<{ items: WorkflowRun[] }>(`/workflows/${workflow.id}/runs`).then((r) => r.items) });
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent size="lg" title={`History: ${workflow.name}`}>
        {runs.isLoading ? (
          <Skeleton className="h-20" />
        ) : (runs.data ?? []).length === 0 ? (
          <p className="text-sm text-muted">It hasn't run yet.</p>
        ) : (
          <ul className="divide-y divide-border">
            {runs.data!.map((r) => (
              <li key={r.id} className="py-2.5 text-sm">
                <div className="flex items-center gap-2">
                  <Badge tone={r.status === "done" ? "success" : r.status === "failed" ? "danger" : "neutral"}>{r.status === "done" ? "Done" : r.status === "failed" ? "Failed" : "Skipped"}</Badge>
                  <span className="min-w-0 flex-1 truncate font-medium">{r.document_title || "Scheduled run"}</span>
                  <span className="shrink-0 text-xs text-subtle">{formatDateTime(r.ran_at)}</span>
                </div>
                {r.summary && <p className="mt-0.5 text-[13px] text-muted">{r.summary}</p>}
              </li>
            ))}
          </ul>
        )}
      </DialogContent>
    </Dialog>
  );
}
