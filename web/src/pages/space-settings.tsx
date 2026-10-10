import * as React from "react";
import { useNavigate, useParams } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { FileType, ListChecks, Merge, Pencil, Plus, ScanLine, Settings2, Sparkles, Tags, Trash2, UserPlus, Users, Wand2, Workflow } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError, errorMessage } from "@/lib/api";
import { keys, useDirectory, useTaxonomy } from "@/lib/queries";
import type { MatchAlgorithm, Member, Space, SpaceRole, TaxonomyItem, TaxonomyKind } from "@/lib/types";
import { cn, colorNames, tagDot } from "@/lib/utils";
import { useCurrentUser } from "@/components/app-shell";
import { SettingsCard, SettingsLayout } from "@/components/settings-layout";
import { Button } from "@/components/ui/button";
import { Field, Input, NativeSelect, Textarea } from "@/components/ui/input";
import { Avatar, Badge, Checkbox, EmptyState, TagChip } from "@/components/ui/misc";
import { Dialog, DialogContent, DialogFooter } from "@/components/ui/overlay";
import { confirm } from "@/components/ui/confirm";
import { NotFound } from "./not-found";
import { SpaceAI, SpaceColor, SpaceFields, SpaceScanning, SpaceWorkflows } from "./space-extras";

const sections = [
  { id: "general", label: "General", icon: <Settings2 /> },
  { id: "members", label: "Members", icon: <Users /> },
  { id: "tags", label: "Tags", icon: <Tags /> },
  { id: "correspondents", label: "Correspondents", icon: <UserPlus /> },
  { id: "document-types", label: "Document types", icon: <FileType /> },
  { id: "fields", label: "Custom fields", icon: <ListChecks /> },
  { id: "workflows", label: "Workflows", icon: <Workflow /> },
  { id: "ai", label: "AI", icon: <Sparkles /> },
  { id: "scanning", label: "Scanning", icon: <ScanLine /> },
];

export function SpaceSettingsPage() {
  const { id, section } = useParams({ from: "/app/spaces/$id/$section" });
  const me = useCurrentUser();
  const space = me.spaces.find((s) => s.id === id);
  if (!space) return <NotFound />;
  const shown = space.kind === "personal" ? sections.filter((s) => s.id !== "members") : sections;
  if (!shown.some((s) => s.id === section)) return <NotFound />;
  return (
    <SettingsLayout
      title={space.kind === "personal" ? "Personal space" : space.name}
      description={space.kind === "personal" ? "Only you can see documents here." : `${space.member_count} member${space.member_count === 1 ? "" : "s"} · ${space.document_count} document${space.document_count === 1 ? "" : "s"}`}
      base={`/spaces/${id}`}
      sections={shown}
      active={section}
    >
      {section === "general" && <General space={space} />}
      {section === "members" && <Members space={space} />}
      {section === "fields" && <SpaceFields space={space} />}
      {section === "workflows" && <SpaceWorkflows space={space} />}
      {section === "ai" && <SpaceAI space={space} />}
      {section === "scanning" && <SpaceScanning space={space} />}
      {(section === "tags" || section === "correspondents" || section === "document-types") && <Vocabulary space={space} kind={section} />}
    </SettingsLayout>
  );
}

function General({ space }: { space: Space }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const owner = space.role === "owner";
  const [name, setName] = React.useState(space.name);
  const [desc, setDesc] = React.useState(space.description);
  const [lang, setLang] = React.useState(space.default_language);
  const [busy, setBusy] = React.useState(false);
  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      await api.patch(`/spaces/${space.id}`, { name, description: desc, default_language: lang });
      qc.invalidateQueries({ queryKey: keys.me });
      toast.success("Saved");
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  const remove = async () => {
    if (!(await confirm({ title: `Delete “${space.name}”?`, body: "The space must be empty (including Trash).", confirmLabel: "Delete space", destructive: true }))) return;
    try {
      await api.del(`/spaces/${space.id}`);
      await qc.invalidateQueries({ queryKey: keys.me });
      navigate({ to: "/" });
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };
  const leave = async () => {
    const me = qc.getQueryData<{ id: string }>(keys.me);
    if (!me || !(await confirm({ title: `Leave “${space.name}”?`, body: "You'll lose access to its documents.", confirmLabel: "Leave", destructive: true }))) return;
    try {
      await api.del(`/spaces/${space.id}/members/${me.id}`);
      await qc.invalidateQueries({ queryKey: keys.me });
      navigate({ to: "/" });
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };
  return (
    <>
      <SettingsCard title="General">
        <form onSubmit={save} className="grid gap-4">
          {space.kind === "shared" && (
            <Field label="Name" htmlFor="sp-name">
              <Input id="sp-name" value={name} onChange={(e) => setName(e.target.value)} disabled={!owner} />
            </Field>
          )}
          <SpaceColor space={space} />
          <Field label="Description" htmlFor="sp-desc">
            <Textarea id="sp-desc" rows={2} value={desc} onChange={(e) => setDesc(e.target.value)} disabled={!owner} />
          </Field>
          <Field label="Default document language" htmlFor="sp-lang" hint="The first guess for new documents. Text recognition also spots Hindi, Tamil, Telugu, Kannada and English pages by itself, and corrects the language when the text is in another script.">
            <NativeSelect id="sp-lang" value={lang} onChange={(e) => setLang(e.target.value)} disabled={!owner}>
              <option value="en">English</option>
              <option value="hi">Hindi</option>
              <option value="mr">Marathi</option>
              <option value="bn">Bengali</option>
              <option value="gu">Gujarati</option>
              <option value="ta">Tamil</option>
              <option value="te">Telugu</option>
              <option value="kn">Kannada</option>
              <option value="ml">Malayalam</option>
              <option value="de">German</option>
              <option value="fr">French</option>
              <option value="es">Spanish</option>
            </NativeSelect>
          </Field>
          {owner && (
            <div>
              <Button type="submit" variant="primary" loading={busy}>
                Save
              </Button>
            </div>
          )}
        </form>
      </SettingsCard>
      {space.kind === "shared" && (
        <SettingsCard title={owner ? "Delete space" : "Leave space"} description={owner ? "Move or delete all documents first." : undefined}>
          {owner ? (
            <Button variant="danger-ghost" onClick={remove}>
              <Trash2 /> Delete this space
            </Button>
          ) : (
            <Button variant="danger-ghost" onClick={leave}>
              Leave this space
            </Button>
          )}
        </SettingsCard>
      )}
    </>
  );
}

const roleInfo: Record<SpaceRole, string> = {
  owner: "Everything, including members and settings",
  editor: "Upload, edit, tag and delete documents",
  viewer: "View, search, download and comment",
};

function Members({ space }: { space: Space }) {
  const qc = useQueryClient();
  const me = useCurrentUser();
  const members = useQuery({
    queryKey: ["members", space.id],
    queryFn: () => api.get<{ items: Member[] }>(`/spaces/${space.id}/members`).then((r) => r.items),
  });
  const dir = useDirectory();
  const owner = space.role === "owner";
  const [adding, setAdding] = React.useState("");
  const [role, setRole] = React.useState<SpaceRole>("editor");
  const candidates = (dir.data ?? []).filter((u) => !members.data?.some((m) => m.user_id === u.id));

  const setMember = async (uid: string, r: SpaceRole) => {
    try {
      await api.put(`/spaces/${space.id}/members/${uid}`, { role: r });
      qc.invalidateQueries({ queryKey: ["members", space.id] });
      qc.invalidateQueries({ queryKey: keys.me });
      setAdding("");
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };
  const remove = async (m: Member) => {
    if (!(await confirm({ title: `Remove ${m.display_name}?`, body: `They'll lose access to documents in ${space.name}.`, confirmLabel: "Remove", destructive: true }))) return;
    try {
      await api.del(`/spaces/${space.id}/members/${m.user_id}`);
      qc.invalidateQueries({ queryKey: ["members", space.id] });
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  return (
    <SettingsCard title="Members" description="People in this space can see all of its documents.">
      {owner && (
        <div className="mb-4 flex flex-wrap gap-2">
          <NativeSelect value={adding} onChange={(e) => setAdding(e.target.value)} className="min-w-48 flex-1" aria-label="Person to add">
            <option value="">Add a person…</option>
            {candidates.map((u) => (
              <option key={u.id} value={u.id}>
                {u.display_name} ({u.email})
              </option>
            ))}
          </NativeSelect>
          <NativeSelect value={role} onChange={(e) => setRole(e.target.value as SpaceRole)} className="w-auto" aria-label="Role">
            <option value="viewer">Viewer</option>
            <option value="editor">Editor</option>
            <option value="owner">Owner</option>
          </NativeSelect>
          <Button variant="primary" disabled={!adding} onClick={() => setMember(adding, role)}>
            <Plus /> Add
          </Button>
        </div>
      )}
      {owner && candidates.length === 0 && (dir.data?.length ?? 0) <= (members.data?.length ?? 0) && (
        <p className="mb-4 text-[13px] text-muted">Everyone with a Docveta account is already here. Ask an administrator to add more people.</p>
      )}
      <ul className="divide-y divide-border">
        {(members.data ?? []).map((m) => (
          <li key={m.user_id} className="flex flex-wrap items-center gap-3 py-3">
            <Avatar name={m.display_name} />
            <div className="min-w-48 flex-1">
              <div className="text-sm font-medium">
                {m.display_name} {m.user_id === me.id && <span className="text-subtle">(you)</span>}
              </div>
              <div className="text-xs text-subtle">{m.email}</div>
            </div>
            {owner ? (
              <NativeSelect value={m.role} onChange={(e) => setMember(m.user_id, e.target.value as SpaceRole)} className="ml-auto h-8 w-auto text-[13px]" title={roleInfo[m.role]}>
                <option value="viewer">Viewer</option>
                <option value="editor">Editor</option>
                <option value="owner">Owner</option>
              </NativeSelect>
            ) : (
              <Badge>{m.role}</Badge>
            )}
            {owner && m.user_id !== me.id && (
              <Button size="icon-sm" variant="ghost" onClick={() => remove(m)} aria-label={`Remove ${m.display_name}`}>
                <Trash2 />
              </Button>
            )}
          </li>
        ))}
      </ul>
      <div className="mt-4 grid gap-1 rounded-lg bg-surface-2 p-3 text-xs text-muted sm:grid-cols-3">
        {(Object.keys(roleInfo) as SpaceRole[]).map((r) => (
          <div key={r}>
            <span className="font-medium capitalize text-fg">{r}:</span> {roleInfo[r]}
          </div>
        ))}
      </div>
    </SettingsCard>
  );
}

const kindLabels: Record<TaxonomyKind, { one: string; many: string; help: string }> = {
  tags: { one: "tag", many: "Tags", help: "Labels like Tax, Medical or Car. A document can have many." },
  correspondents: { one: "correspondent", many: "Correspondents", help: "Who a document is from or to — a bank, a hospital, a company." },
  "document-types": { one: "document type", many: "Document types", help: "The broad kind of document — Identification, Bill, Insurance, Certificate. Each document has one; tags (Aadhaar, PAN) say what exactly it is." },
};

const matchHelp: Record<MatchAlgorithm, string> = {
  none: "Don't assign automatically",
  any: "Any of these words appears",
  all: "All of these words appear",
  exact: "This exact phrase appears",
  regex: "Regular expression matches",
  fuzzy: "Similar phrase appears (tolerates scanning errors)",
};

function Vocabulary({ space, kind }: { space: Space; kind: TaxonomyKind }) {
  const items = useTaxonomy(kind, space.id);
  const canEdit = space.role !== "viewer";
  const [editing, setEditing] = React.useState<TaxonomyItem | "new" | null>(null);
  const [selected, setSelected] = React.useState<string[]>([]);
  const [filter, setFilter] = React.useState("");
  const qc = useQueryClient();
  const L = kindLabels[kind];
  const shown = (items.data ?? []).filter((i) => i.name.toLowerCase().includes(filter.toLowerCase()));

  const remove = async (it: TaxonomyItem) => {
    if (!(await confirm({ title: `Delete ${L.one} “${it.name}”?`, body: it.document_count ? `It will be removed from ${it.document_count} document(s). The documents are kept.` : undefined, confirmLabel: "Delete", destructive: true })))
      return;
    try {
      await api.del(`/${kind}/${it.id}`);
      qc.invalidateQueries({ queryKey: ["taxonomy", kind] });
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };
  const merge = async () => {
    const [target, ...rest] = selected;
    const t = items.data?.find((i) => i.id === target);
    if (!t || !(await confirm({ title: `Merge ${selected.length} ${L.many.toLowerCase()} into “${t.name}”?`, body: "Documents keep everything; duplicates are removed.", confirmLabel: "Merge" }))) return;
    try {
      await api.post(`/${kind}/${target}/merge`, { source_ids: rest });
      setSelected([]);
      qc.invalidateQueries({ queryKey: ["taxonomy", kind] });
      toast.success("Merged");
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  return (
    <SettingsCard
      title={L.many}
      description={L.help}
      actions={
        canEdit && (
          <>
            {selected.length > 1 && (
              <Button size="sm" onClick={merge}>
                <Merge /> Merge {selected.length}
              </Button>
            )}
            <Button size="sm" variant="primary" onClick={() => setEditing("new")}>
              <Plus /> New {L.one}
            </Button>
          </>
        )
      }
    >
      {(items.data?.length ?? 0) > 8 && <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder={`Filter ${L.many.toLowerCase()}…`} className="mb-3" />}
      {shown.length === 0 ? (
        <EmptyState title={`No ${L.many.toLowerCase()} yet`} className="py-8">
          You can also create them right from a document.
        </EmptyState>
      ) : (
        <ul className="divide-y divide-border">
          {shown.map((it) => (
            <li key={it.id} className="flex items-center gap-3 py-2.5">
              {canEdit && <Checkbox checked={selected.includes(it.id)} onCheckedChange={(v) => setSelected(v ? [...selected, it.id] : selected.filter((x) => x !== it.id))} aria-label={`Select ${it.name}`} />}
              <div className="min-w-0 flex-1">
                {kind === "tags" ? <TagChip name={it.name} color={it.color} /> : <span className="text-sm font-medium">{it.name}</span>}
                {it.match_algorithm !== "none" && it.match_pattern && (
                  <div className="mt-0.5 flex items-center gap-1 truncate text-xs text-subtle">
                    <Wand2 className="size-3" /> Auto: {matchHelp[it.match_algorithm].toLowerCase()} — <code>{it.match_pattern}</code>
                  </div>
                )}
              </div>
              <span className="text-xs tabular-nums text-subtle">{it.document_count} doc{it.document_count === 1 ? "" : "s"}</span>
              {canEdit && (
                <>
                  <Button size="icon-sm" variant="ghost" onClick={() => setEditing(it)} aria-label={`Edit ${it.name}`}>
                    <Pencil />
                  </Button>
                  <Button size="icon-sm" variant="ghost" onClick={() => remove(it)} aria-label={`Delete ${it.name}`}>
                    <Trash2 />
                  </Button>
                </>
              )}
            </li>
          ))}
        </ul>
      )}
      {editing && <ItemDialog kind={kind} spaceId={space.id} item={editing === "new" ? null : editing} onClose={() => setEditing(null)} />}
    </SettingsCard>
  );
}

function ItemDialog({ kind, spaceId, item, onClose }: { kind: TaxonomyKind; spaceId: string; item: TaxonomyItem | null; onClose: () => void }) {
  const qc = useQueryClient();
  const [name, setName] = React.useState(item?.name ?? "");
  const [color, setColor] = React.useState(item?.color ?? "blue");
  const [algo, setAlgo] = React.useState<MatchAlgorithm>(item?.match_algorithm ?? "none");
  const [pattern, setPattern] = React.useState(item?.match_pattern ?? "");
  const [cs, setCs] = React.useState(item?.case_sensitive ?? false);
  const [err, setErr] = React.useState<ApiError | null>(null);
  const [busy, setBusy] = React.useState(false);
  const L = kindLabels[kind];
  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    const body: Record<string, unknown> = { name, match_algorithm: algo, match_pattern: pattern, case_sensitive: cs };
    if (kind === "tags") body.color = color;
    try {
      if (item) await api.patch(`/${kind}/${item.id}`, body);
      else await api.post(`/${kind}`, { ...body, space_id: spaceId });
      qc.invalidateQueries({ queryKey: ["taxonomy", kind] });
      onClose();
    } catch (e) {
      setErr(e as ApiError);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={item ? `Edit ${L.one}` : `New ${L.one}`}>
        <form onSubmit={save} className="space-y-4">
          <Field label="Name" htmlFor="i-name" error={err?.fieldError("name") ?? (err?.code === "name_taken" ? err.message : undefined)}>
            <Input id="i-name" required value={name} onChange={(e) => setName(e.target.value)} autoFocus />
          </Field>
          {kind === "tags" && (
            <div>
              <div className="mb-2 text-[13px] font-medium">Colour</div>
              <div className="flex flex-wrap gap-2">
                {colorNames.map((c) => (
                  <button
                    key={c}
                    type="button"
                    onClick={() => setColor(c)}
                    className={cn("size-7 rounded-full ring-offset-2 ring-offset-surface", tagDot[c], color === c && "ring-2 ring-fg")}
                    aria-label={c}
                    aria-pressed={color === c}
                  />
                ))}
              </div>
            </div>
          )}
          <div className="rounded-lg border border-border p-3">
            <div className="flex items-center gap-2 text-[13px] font-medium">
              <Wand2 className="size-4 text-accent" /> Assign automatically
            </div>
            <p className="mt-0.5 text-xs text-muted">When new documents contain matching text, Docveta adds this {L.one} for you.</p>
            <div className="mt-3 grid gap-3">
              <NativeSelect value={algo} onChange={(e) => setAlgo(e.target.value as MatchAlgorithm)} aria-label="Matching method">
                {(Object.keys(matchHelp) as MatchAlgorithm[]).map((a) => (
                  <option key={a} value={a}>
                    {matchHelp[a]}
                  </option>
                ))}
              </NativeSelect>
              {algo !== "none" && (
                <>
                  <Field
                    label={algo === "regex" ? "Regular expression" : "Words or phrase"}
                    htmlFor="i-pat"
                    hint={algo === "any" || algo === "all" ? 'Separate words with spaces. Use quotes for phrases: BESCOM "electricity bill"' : undefined}
                    error={err?.fieldError("match_pattern")}
                  >
                    <Input id="i-pat" value={pattern} onChange={(e) => setPattern(e.target.value)} className={algo === "regex" ? "font-mono" : undefined} />
                  </Field>
                  <label className="flex items-center gap-2 text-sm">
                    <Checkbox checked={cs} onCheckedChange={(v) => setCs(!!v)} /> Case sensitive
                  </label>
                </>
              )}
            </div>
          </div>
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
