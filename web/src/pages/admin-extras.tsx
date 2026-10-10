import * as React from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Brain, Download, FolderSync, Link2, Mail, Pencil, Plus, RefreshCw, Trash2, Zap } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError, errorMessage } from "@/lib/api";
import { useAIProviders, useFolders, useInvites } from "@/lib/queries";
import type { AIProvider, AITestResult, AITuning, Invite, WatchedFolder } from "@/lib/types";
import { formatDateTime, spaceLabel, timeAgo } from "@/lib/utils";
import { useCurrentUser } from "@/components/app-shell";
import { SecretReveal, SettingsCard } from "@/components/settings-layout";
import { Button } from "@/components/ui/button";
import { Field, Input, NativeSelect, Textarea } from "@/components/ui/input";
import { Badge, Checkbox, EmptyState, Skeleton, Switch, SwitchRow } from "@/components/ui/misc";
import { Dialog, DialogContent, DialogFooter } from "@/components/ui/overlay";
import { EntityPicker } from "@/components/ui/entity-picker";
import { confirm } from "@/components/ui/confirm";

/* ------------------------------------------------------------------ AI providers */

const presets: { label: string; base_url: string; chat_model: string; embedding_model: string; is_local: boolean }[] = [
  { label: "OpenAI", base_url: "https://api.openai.com/v1", chat_model: "gpt-4o-mini", embedding_model: "text-embedding-3-small", is_local: false },
  { label: "Ollama (this computer)", base_url: "http://localhost:11434/v1", chat_model: "llama3.1", embedding_model: "nomic-embed-text", is_local: true },
  { label: "LM Studio", base_url: "http://localhost:1234/v1", chat_model: "", embedding_model: "", is_local: true },
  { label: "OpenRouter", base_url: "https://openrouter.ai/api/v1", chat_model: "", embedding_model: "", is_local: false },
];

export function AIAdmin() {
  const providers = useAIProviders();
  const qc = useQueryClient();
  const [editing, setEditing] = React.useState<AIProvider | "new" | null>(null);
  const [reindexing, setReindexing] = React.useState(false);
  const refresh = () => qc.invalidateQueries({ queryKey: ["ai-providers"] }).then(() => qc.invalidateQueries({ queryKey: ["ai-enabled"] }));
  const list = providers.data ?? [];
  const test = async (p: AIProvider) => {
    const t = toast.loading(`Testing ${p.name}…`);
    try {
      const r = await api.post<AITestResult>(`/admin/ai/providers/${p.id}/test`);
      if (r.ok) {
        const missing = [r.chat_model_found === false && `chat model “${p.chat_model}”`, r.embedding_model_found === false && `embedding model “${p.embedding_model}”`].filter(Boolean);
        if (missing.length) toast.warning("Connected, but the provider doesn't list the " + missing.join(" or "), { id: t });
        else toast.success("Connected", { id: t });
      } else toast.error(r.error || "Couldn't connect", { id: t });
      refresh();
    } catch (e) {
      toast.error(errorMessage(e), { id: t });
    }
  };
  const remove = async (p: AIProvider) => {
    if (!(await confirm({ title: `Remove ${p.name}?`, body: "Suggestions already made stay. New ones stop until you add another provider.", confirmLabel: "Remove", destructive: true }))) return;
    await api.del(`/admin/ai/providers/${p.id}`).then(refresh, (e) => toast.error(errorMessage(e)));
  };
  const toggle = async (p: AIProvider, enabled: boolean) => {
    await api.patch(`/admin/ai/providers/${p.id}`, { enabled }).then(refresh, (e) => toast.error(errorMessage(e)));
  };
  const reindex = async () => {
    setReindexing(true);
    try {
      const r = await api.post<{ queued: number }>("/admin/ai/reindex");
      toast.success(r.queued ? `Preparing ${r.queued} document${r.queued === 1 ? "" : "s"} for meaning-based search` : "Everything is already prepared");
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setReindexing(false);
    }
  };
  return (
    <>
      <SettingsCard
        title="AI providers"
        description="Any OpenAI-compatible service works: OpenAI, OpenRouter, or a model on your own machine (Ollama, LM Studio). Docveta sends document text only to providers you add here, and only for spaces where AI is turned on."
        actions={
          <Button size="sm" variant="primary" onClick={() => setEditing("new")}>
            <Plus /> Add provider
          </Button>
        }
      >
        {providers.isLoading ? (
          <Skeleton className="h-16" />
        ) : list.length === 0 ? (
          <EmptyState icon={<Brain />} title="No AI provider yet" className="py-8">
            Add one to get tag and sender suggestions, meaning-based search, similar documents and “Ask your documents”.
          </EmptyState>
        ) : (
          <ul className="divide-y divide-border">
            {list.map((p) => (
              <li key={p.id} className="flex flex-wrap items-center gap-3 py-3">
                <Switch checked={p.enabled} onCheckedChange={(v) => toggle(p, v)} aria-label={`Enable ${p.name}`} />
                <div className="min-w-48 flex-1">
                  <div className="flex flex-wrap items-center gap-1.5 text-sm font-medium">
                    {p.name}
                    {p.is_default && <Badge tone="accent">Default</Badge>}
                    <Badge tone={p.is_local ? "success" : "neutral"}>{p.is_local ? "Runs locally" : "Cloud"}</Badge>
                  </div>
                  <div className="truncate text-xs text-subtle">
                    {p.base_url} · chat: {p.chat_model || "none"} · embeddings: {p.embedding_model || "none"}
                  </div>
                  {p.last_error ? <div className="mt-0.5 text-xs text-danger">Last error: {p.last_error}</div> : p.last_ok_at ? <div className="text-xs text-success">Worked {timeAgo(p.last_ok_at)}</div> : null}
                </div>
                <div className="ml-auto flex items-center gap-1">
                  <Button size="sm" variant="ghost" onClick={() => test(p)}>
                    <Zap /> Test
                  </Button>
                  <Button size="icon-sm" variant="ghost" aria-label={`Edit ${p.name}`} onClick={() => setEditing(p)}>
                    <Pencil />
                  </Button>
                  <Button size="icon-sm" variant="ghost" aria-label={`Remove ${p.name}`} onClick={() => remove(p)}>
                    <Trash2 />
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
        {editing && <ProviderDialog provider={editing === "new" ? null : editing} first={list.length === 0} onClose={() => setEditing(null)} onSaved={refresh} />}
      </SettingsCard>
      {list.some((p) => p.enabled && p.embedding_model) && (
        <SettingsCard title="Meaning-based search" description="New documents are prepared automatically. Run this once after adding a provider so older documents can be found by meaning too.">
          <Button loading={reindexing} onClick={reindex}>
            <RefreshCw /> Prepare existing documents
          </Button>
        </SettingsCard>
      )}
      {list.length > 0 && <AITuningCard />}
    </>
  );
}

/** Server-wide AI settings: what every space's AI starts from. */
function AITuningCard() {
  const qc = useQueryClient();
  const saved = useQuery({ queryKey: ["ai-tuning"], queryFn: () => api.get<AITuning>("/admin/ai/settings") });
  const [f, setF] = React.useState<{ types: string; sources: string; perDoc: string; text: string } | null>(null);
  const [busy, setBusy] = React.useState(false);
  const [errors, setErrors] = React.useState<Record<string, string>>({});
  const show = (t: AITuning) => setF({ types: t.common_types.join("\n"), sources: String(t.ask_sources), perDoc: String(t.ask_sources_per_document), text: String(t.suggest_text_tokens) });
  React.useEffect(() => {
    if (saved.data) show(saved.data);
  }, [saved.data]);
  if (!f || !saved.data) return null;
  const set = (k: keyof typeof f, v: string) => setF({ ...f, [k]: v });
  const dirty =
    f.types.split("\n").map((s) => s.trim()).filter(Boolean).join("\n") !== saved.data.common_types.join("\n") ||
    Number(f.sources) !== saved.data.ask_sources ||
    Number(f.perDoc) !== saved.data.ask_sources_per_document ||
    Number(f.text) !== saved.data.suggest_text_tokens;
  const done = (t: AITuning, message: string) => {
    qc.setQueryData(["ai-tuning"], t);
    show(t);
    setErrors({});
    toast.success(message);
  };
  const fail = (e: unknown) => {
    if (e instanceof ApiError && e.fields.length) setErrors(Object.fromEntries(e.fields.map((x) => [x.field, x.message])));
    else toast.error(errorMessage(e));
  };
  const save = async () => {
    setBusy(true);
    try {
      const body = { common_types: f.types.split("\n").map((s) => s.trim()).filter(Boolean), ask_sources: Number(f.sources), ask_sources_per_document: Number(f.perDoc), suggest_text_tokens: Number(f.text) };
      done(await api.put<AITuning>("/admin/ai/settings", body), "Saved");
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };
  const reset = async () => {
    if (!(await confirm({ title: "Go back to the built-in settings?", body: "The document types below, and the numbers for answers and long documents, return to what Docveta came with. Types and tags already on documents stay.", confirmLabel: "Reset" }))) return;
    setBusy(true);
    try {
      done(await api.del<AITuning>("/admin/ai/settings"), "Back to the built-in settings");
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <SettingsCard
      title="Tuning"
      description="For every space. How sure AI must be and whether it may create tags and types is set per space (Space settings → AI assistance); how much a model can read at once is set on the provider (Context size)."
      actions={
        <Button size="sm" variant="ghost" disabled={busy} onClick={reset}>
          Reset
        </Button>
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field
          label="Document types AI may suggest"
          htmlFor="ai-types"
          className="sm:row-span-3"
          error={errors.common_types}
          hint="One per line. Offered to AI when none of a space's own types fits, so that the same kind of document always gets the same name. Keep them broad (Identification); the specifics (Aadhaar, PAN) are tags."
        >
          <Textarea id="ai-types" rows={11} value={f.types} onChange={(e) => set("types", e.target.value)} spellCheck={false} />
        </Field>
        <Field label="Passages an answer is made from" htmlFor="ai-src" error={errors.ask_sources} hint="For Ask your documents. More gives fuller answers and needs a larger context size (2 to 20).">
          <Input id="ai-src" type="number" min={2} max={20} value={f.sources} onChange={(e) => set("sources", e.target.value)} />
        </Field>
        <Field label="Of those, from one document at most" htmlFor="ai-per" error={errors.ask_sources_per_document} hint="Keeps one long document from crowding out the others.">
          <Input id="ai-per" type="number" min={1} max={20} value={f.perDoc} onChange={(e) => set("perDoc", e.target.value)} />
        </Field>
        <Field label="Text read for suggestions, at most (tokens)" htmlFor="ai-text" error={errors.suggest_text_tokens} hint="How far into a long document AI reads to file it: 4000 is about 16,000 English characters. Never more than fits the provider's context size.">
          <Input id="ai-text" type="number" min={500} step={500} value={f.text} onChange={(e) => set("text", e.target.value)} />
        </Field>
      </div>
      <div className="mt-4 flex justify-end gap-2">
        {dirty && (
          <Button variant="ghost" disabled={busy} onClick={() => show(saved.data)}>
            Discard changes
          </Button>
        )}
        <Button variant="primary" loading={busy} disabled={!dirty} onClick={save}>
          Save
        </Button>
      </div>
    </SettingsCard>
  );
}

function ProviderDialog({ provider, first, onClose, onSaved }: { provider: AIProvider | null; first: boolean; onClose: () => void; onSaved: () => void }) {
  const [f, setF] = React.useState({
    name: provider?.name ?? "",
    base_url: provider?.base_url ?? "",
    api_key: "",
    chat_model: provider?.chat_model ?? "",
    embedding_model: provider?.embedding_model ?? "",
    is_local: provider?.is_local ?? false,
    is_default: provider?.is_default ?? first,
    timeout_seconds: provider?.timeout_seconds ?? 60,
    max_concurrency: provider?.max_concurrency ?? 2,
    context_tokens: provider?.context_tokens ?? 8192,
  });
  const [err, setErr] = React.useState<ApiError | null>(null);
  const [busy, setBusy] = React.useState(false);
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF((x) => ({ ...x, [k]: v }));
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    const body: Record<string, unknown> = { ...f };
    if (!f.api_key) delete body.api_key; // blank = keep the saved key
    try {
      if (provider) await api.patch(`/admin/ai/providers/${provider.id}`, body);
      else await api.post("/admin/ai/providers", body);
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
      <DialogContent size="lg" title={provider ? `Edit ${provider.name}` : "Add an AI provider"}>
        <form onSubmit={submit} className="space-y-4">
          {!provider && (
            <div className="flex flex-wrap gap-2">
              {presets.map((p) => (
                <Button key={p.label} size="sm" onClick={() => setF((x) => ({ ...x, name: x.name || p.label, base_url: p.base_url, chat_model: p.chat_model, embedding_model: p.embedding_model, is_local: p.is_local }))}>
                  {p.label}
                </Button>
              ))}
            </div>
          )}
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Name" htmlFor="ai-name" error={err?.fieldError("name")}>
              <Input id="ai-name" required value={f.name} onChange={(e) => set("name", e.target.value)} />
            </Field>
            <Field label="Address (base URL)" htmlFor="ai-url" error={err?.fieldError("base_url")} hint="Ends in /v1 for most services.">
              <Input id="ai-url" required type="url" value={f.base_url} onChange={(e) => set("base_url", e.target.value)} placeholder="https://api.openai.com/v1" />
            </Field>
            <Field label="API key" htmlFor="ai-key" hint={provider?.has_api_key ? "Leave blank to keep the saved key." : "Not needed for most local models."}>
              <Input id="ai-key" type="password" autoComplete="off" value={f.api_key} onChange={(e) => set("api_key", e.target.value)} />
            </Field>
            <div className="hidden sm:block" />
            <Field label="Chat model" htmlFor="ai-chat" hint="Used for suggestions and answers.">
              <Input id="ai-chat" value={f.chat_model} onChange={(e) => set("chat_model", e.target.value)} placeholder="e.g. gpt-4o-mini" />
            </Field>
            <Field label="Embedding model" htmlFor="ai-emb" hint="Used for meaning-based search and similar documents.">
              <Input id="ai-emb" value={f.embedding_model} onChange={(e) => set("embedding_model", e.target.value)} placeholder="e.g. text-embedding-3-small" />
            </Field>
            <Field label="Waits up to (seconds)" htmlFor="ai-to">
              <Input id="ai-to" type="number" min={5} max={600} value={f.timeout_seconds} onChange={(e) => set("timeout_seconds", Number(e.target.value))} />
            </Field>
            <Field label="Requests at once" htmlFor="ai-cc">
              <Input id="ai-cc" type="number" min={1} max={16} value={f.max_concurrency} onChange={(e) => set("max_concurrency", Number(e.target.value))} />
            </Field>
            <Field
              label="Context size (tokens)"
              htmlFor="ai-ctx"
              className="sm:col-span-2"
              hint="How much text the chat model can take at once. Docveta shortens long documents to fit. Local models often run with 4096 (Ollama: num_ctx); set the same number here."
            >
              <Input id="ai-ctx" type="number" min={1024} step={1024} list="ai-ctx-sizes" value={f.context_tokens} onChange={(e) => set("context_tokens", Number(e.target.value))} />
              <datalist id="ai-ctx-sizes">
                {[2048, 4096, 8192, 16384, 32768, 131072].map((n) => (
                  <option key={n} value={n} />
                ))}
              </datalist>
            </Field>
          </div>
          <div className="divide-y divide-border">
            <SwitchRow label="This runs on my own hardware" description="Spaces set to “local only” may use it. Don't turn this on for a cloud service." checked={f.is_local} onCheckedChange={(v) => set("is_local", v)} />
            <SwitchRow label="Use as the default provider" checked={f.is_default} onCheckedChange={(v) => set("is_default", v)} />
          </div>
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

/* ------------------------------------------------------------------ Watched folders */

export function FoldersAdmin() {
  const folders = useFolders();
  const me = useCurrentUser();
  const qc = useQueryClient();
  const [editing, setEditing] = React.useState<WatchedFolder | "new" | null>(null);
  const refresh = () => qc.invalidateQueries({ queryKey: ["folders"] });
  const spaceName = (id: string) => spaceLabel(me.spaces.find((s) => s.id === id)) || "Unknown space";
  const scan = async (f: WatchedFolder) => {
    const t = toast.loading("Looking for new files…");
    try {
      const r = await api.post<{ imported: number; failed: number; skipped: number }>(`/admin/folders/${f.id}/scan`);
      toast.success(`${r.imported} imported${r.failed ? `, ${r.failed} failed` : ""}${r.skipped ? `, ${r.skipped} skipped` : ""}`, { id: t });
      refresh();
    } catch (e) {
      toast.error(errorMessage(e), { id: t });
    }
  };
  const remove = async (f: WatchedFolder) => {
    if (!(await confirm({ title: "Stop watching this folder?", body: "Files already imported stay in Docveta. Nothing on disk is deleted.", confirmLabel: "Stop watching", destructive: true }))) return;
    await api.del(`/admin/folders/${f.id}`).then(refresh, (e) => toast.error(errorMessage(e)));
  };
  const toggle = async (f: WatchedFolder, enabled: boolean) => {
    await api.patch(`/admin/folders/${f.id}`, { enabled }).then(refresh, (e) => toast.error(errorMessage(e)));
  };
  return (
    <SettingsCard
      title="Watched folders"
      description="Drop files into a folder on the server (for example from a network scanner) and Docveta imports them. The folder must be inside a location the server is allowed to watch."
      actions={
        <Button size="sm" variant="primary" onClick={() => setEditing("new")}>
          <Plus /> Watch a folder
        </Button>
      }
    >
      {folders.data && (
        <p className="mb-3 rounded-md bg-surface-2 px-3 py-2 text-[13px] text-muted">
          {folders.data.roots.length ? (
            <>
              Allowed locations: {folders.data.roots.map((r) => <code key={r} className="mx-0.5 rounded bg-surface px-1">{r}</code>)}
            </>
          ) : (
            <>No location is allowed yet. Set <code>DOCVETA_WATCH_ROOTS</code> (for example <code>/data/inbox</code>) and restart Docveta.</>
          )}
        </p>
      )}
      {folders.isLoading ? (
        <Skeleton className="h-16" />
      ) : (folders.data?.items ?? []).length === 0 ? (
        <EmptyState icon={<FolderSync />} title="No watched folders" className="py-8" />
      ) : (
        <ul className="divide-y divide-border">
          {folders.data!.items.map((f) => (
            <li key={f.id} className="flex flex-wrap items-center gap-3 py-3">
              <Switch checked={f.enabled} onCheckedChange={(v) => toggle(f, v)} aria-label={`Watch ${f.path}`} />
              <div className="min-w-48 flex-1">
                <div className="truncate font-mono text-[13px] font-medium">{f.path}</div>
                <div className="text-xs text-subtle">
                  into {spaceName(f.space_id)} · {f.imported_count} imported{f.failed_count ? `, ${f.failed_count} failed` : ""} · {f.last_scan_at ? `checked ${timeAgo(f.last_scan_at)}` : "not checked yet"}
                </div>
                {f.last_error && <div className="mt-0.5 text-xs text-danger">{f.last_error}</div>}
              </div>
              <div className="ml-auto flex items-center gap-1">
                <Button size="sm" variant="ghost" onClick={() => scan(f)}>
                  <RefreshCw /> Check now
                </Button>
                <Button size="icon-sm" variant="ghost" aria-label="Edit" onClick={() => setEditing(f)}>
                  <Pencil />
                </Button>
                <Button size="icon-sm" variant="ghost" aria-label="Stop watching" onClick={() => remove(f)}>
                  <Trash2 />
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}
      {editing && <FolderDialog folder={editing === "new" ? null : editing} onClose={() => setEditing(null)} onSaved={refresh} />}
    </SettingsCard>
  );
}

function FolderDialog({ folder, onClose, onSaved }: { folder: WatchedFolder | null; onClose: () => void; onSaved: () => void }) {
  const me = useCurrentUser();
  const shared = me.spaces.filter((s) => s.role !== "viewer");
  const [f, setF] = React.useState({
    path: folder?.path ?? "",
    space_id: folder?.space_id ?? shared[0]?.id ?? "",
    recursive: folder?.recursive ?? true,
    subfolders: folder?.subfolders ?? "none",
    after_import: folder?.after_import ?? "move",
    stable_seconds: folder?.stable_seconds ?? 5,
    tag_ids: folder?.tag_ids ?? [],
  });
  const [err, setErr] = React.useState<ApiError | null>(null);
  const [busy, setBusy] = React.useState(false);
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF((x) => ({ ...x, [k]: v }));
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      if (folder) await api.patch(`/admin/folders/${folder.id}`, f);
      else await api.post("/admin/folders", f);
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
      <DialogContent size="lg" title={folder ? "Edit watched folder" : "Watch a folder"}>
        <form onSubmit={submit} className="space-y-4">
          <Field label="Folder on the server" htmlFor="wf-path" error={err?.fieldError("path")} hint="For example /data/inbox. Inside Docker, the folder as the container sees it.">
            <Input id="wf-path" required value={f.path} onChange={(e) => set("path", e.target.value)} className="font-mono" disabled={!!folder} />
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Import into" htmlFor="wf-space">
              <NativeSelect id="wf-space" value={f.space_id} onChange={(e) => set("space_id", e.target.value)}>
                {shared.map((s) => (
                  <option key={s.id} value={s.id}>
                    {spaceLabel(s)}
                  </option>
                ))}
              </NativeSelect>
            </Field>
            <Field label="Tags for everything imported" htmlFor="wf-tags">
              <EntityPicker id="wf-tags" kind="tags" multiple spaceId={f.space_id} value={f.tag_ids} onChange={(v) => set("tag_ids", v)} placeholder="None" />
            </Field>
            <Field label="Subfolders" htmlFor="wf-sub">
              <NativeSelect id="wf-sub" value={f.subfolders} onChange={(e) => set("subfolders", e.target.value as typeof f.subfolders)}>
                <option value="none">Ignore folder names</option>
                <option value="tag">Use the subfolder name as a tag</option>
                <option value="space">Use the subfolder name as the space</option>
              </NativeSelect>
            </Field>
            <Field label="After importing" htmlFor="wf-after">
              <NativeSelect id="wf-after" value={f.after_import} onChange={(e) => set("after_import", e.target.value as typeof f.after_import)}>
                <option value="move">Move the file to an “imported” folder</option>
                <option value="delete">Delete the file</option>
              </NativeSelect>
            </Field>
            <Field label="Wait until a file stops changing (seconds)" htmlFor="wf-stable" hint="Avoids importing a file the scanner is still writing.">
              <Input id="wf-stable" type="number" min={1} max={600} value={f.stable_seconds} onChange={(e) => set("stable_seconds", Number(e.target.value))} />
            </Field>
          </div>
          <label className="flex items-center gap-2 text-sm">
            <Checkbox checked={f.recursive} onCheckedChange={(v) => set("recursive", v === true)} /> Also look inside subfolders
          </label>
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

/* ------------------------------------------------------------------ Office documents (Gotenberg) */

export function OfficeAdmin() {
  const info = useQuery({ queryKey: ["office"], queryFn: () => api.get<{ url: string; from_env: boolean; enabled: boolean }>("/admin/settings/office") });
  const qc = useQueryClient();
  const [url, setUrl] = React.useState<string | null>(null);
  const [busy, setBusy] = React.useState(false);
  const value = url ?? info.data?.url ?? "";
  const save = async () => {
    setBusy(true);
    try {
      await api.put("/admin/settings/office", { url: value.trim() });
      await qc.invalidateQueries({ queryKey: ["office"] });
      setUrl(null);
      toast.success("Saved");
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  const test = async () => {
    const t = toast.loading("Testing…");
    try {
      await api.post("/admin/settings/office/test", { url: value.trim() || undefined });
      toast.success("Gotenberg answered", { id: t });
    } catch (e) {
      toast.error(errorMessage(e), { id: t });
    }
  };
  return (
    <SettingsCard
      title="Word, Excel and PowerPoint"
      description={
        <>
          Office files are converted to PDF by <a className="text-accent underline" href="https://gotenberg.dev" target="_blank" rel="noreferrer">Gotenberg</a>, a small service you run next to Docveta (<code>docker compose --profile office up</code>). Converted files can be viewed and searched; the original is kept untouched.
        </>
      }
    >
      <div className="max-w-xl space-y-3">
        <Field label="Gotenberg address" htmlFor="office-url" hint={info.data?.from_env ? "Set by the DOCVETA_GOTENBERG_URL environment variable; change it there." : "Leave empty to turn Office support off."}>
          <Input id="office-url" type="url" disabled={info.data?.from_env} value={value} placeholder="http://gotenberg:3000" onChange={(e) => setUrl(e.target.value)} />
        </Field>
        <div className="flex items-center gap-2">
          {!info.data?.from_env && (
            <Button variant="primary" loading={busy} disabled={url === null} onClick={save}>
              Save
            </Button>
          )}
          <Button onClick={test} disabled={!value.trim()}>
            Test connection
          </Button>
          {info.data?.enabled ? <Badge tone="success">On</Badge> : <Badge>Off</Badge>}
        </div>
      </div>
    </SettingsCard>
  );
}

/* ------------------------------------------------------------------ Invitations */

export function InvitesPanel() {
  const invites = useInvites();
  const qc = useQueryClient();
  const [open, setOpen] = React.useState(false);
  const refresh = () => qc.invalidateQueries({ queryKey: ["invites"] });
  const pending = (invites.data ?? []).filter((i) => i.status === "pending");
  const revoke = async (i: Invite) => {
    await api.del(`/admin/invites/${i.id}`).then(refresh, (e) => toast.error(errorMessage(e)));
  };
  return (
    <SettingsCard
      title="Invitations"
      description="Invite people with a link. They choose their own password; you never see or type it."
      actions={
        <Button size="sm" variant="primary" onClick={() => setOpen(true)}>
          <Link2 /> Invite someone
        </Button>
      }
    >
      {pending.length === 0 ? (
        <p className="text-sm text-muted">No pending invitations.</p>
      ) : (
        <ul className="divide-y divide-border">
          {pending.map((i) => (
            <li key={i.id} className="flex flex-wrap items-center gap-3 py-2.5 text-sm">
              <div className="min-w-0 flex-1">
                <div className="font-medium">{i.display_name || i.email || "Invitation"}</div>
                <div className="text-xs text-subtle">
                  {i.email ? `${i.email} · ` : ""}expires {formatDateTime(i.expires_at)} · invited by {i.invited_by}
                  {i.spaces.length ? ` · ${i.spaces.map((s) => s.name ?? "a space").join(", ")}` : ""}
                </div>
              </div>
              <Button size="sm" variant="danger-ghost" onClick={() => revoke(i)}>
                Cancel
              </Button>
            </li>
          ))}
        </ul>
      )}
      {open && <InviteDialog onClose={() => setOpen(false)} onSaved={refresh} />}
    </SettingsCard>
  );
}

function InviteDialog({ onClose, onSaved }: { onClose: () => void; onSaved: () => void }) {
  const me = useCurrentUser();
  const shared = me.spaces.filter((s) => s.kind === "shared");
  const [f, setF] = React.useState({ email: "", display_name: "", is_admin: false, note: "", expires_in_days: 7, send_email: false });
  const [picked, setPicked] = React.useState<Record<string, "editor" | "viewer" | "owner">>({});
  const [err, setErr] = React.useState<ApiError | null>(null);
  const [busy, setBusy] = React.useState(false);
  const [done, setDone] = React.useState<{ link: string; email_sent: boolean; email_error?: string | null } | null>(null);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const r = await api.post<{ link: string; email_sent: boolean; email_error?: string | null }>("/admin/invites", {
        ...f,
        email: f.email.trim() || undefined,
        spaces: Object.entries(picked).map(([space_id, role]) => ({ space_id, role })),
      });
      setDone(r);
      onSaved();
    } catch (e) {
      setErr(e as ApiError);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={done ? "Invitation ready" : "Invite someone"} size="md">
        {done ? (
          <>
            <SecretReveal label="Send them this link" value={done.link} />
            {done.email_sent && <p className="mt-3 flex items-center gap-1.5 text-sm text-success"><Mail className="size-4" /> We also emailed it.</p>}
            {done.email_error && <p className="mt-3 text-sm text-warning">The email couldn't be sent ({done.email_error}). Share the link yourself.</p>}
            <DialogFooter>
              <Button variant="primary" onClick={onClose}>
                Done
              </Button>
            </DialogFooter>
          </>
        ) : (
          <form onSubmit={submit} className="space-y-4">
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Name" htmlFor="inv-name" error={err?.fieldError("display_name")}>
                <Input id="inv-name" value={f.display_name} onChange={(e) => setF({ ...f, display_name: e.target.value })} autoFocus />
              </Field>
              <Field label="Email (optional)" htmlFor="inv-email" error={err?.fieldError("email")} hint="If set, only this address can accept.">
                <Input id="inv-email" type="email" value={f.email} onChange={(e) => setF({ ...f, email: e.target.value })} />
              </Field>
            </div>
            {shared.length > 0 && (
              <div>
                <div className="mb-1.5 text-[13px] font-medium">Add to these spaces</div>
                <ul className="space-y-1.5">
                  {shared.map((s) => (
                    <li key={s.id} className="flex items-center gap-2 text-sm">
                      <Checkbox checked={!!picked[s.id]} onCheckedChange={(v) => setPicked((p) => { const n = { ...p }; if (v) n[s.id] = "editor"; else delete n[s.id]; return n; })} />
                      <span className="flex-1">{s.name}</span>
                      {picked[s.id] && (
                        <NativeSelect aria-label={`Role in ${s.name}`} className="w-32" value={picked[s.id]} onChange={(e) => setPicked({ ...picked, [s.id]: e.target.value as "editor" })}>
                          <option value="viewer">Can view</option>
                          <option value="editor">Can edit</option>
                          <option value="owner">Owner</option>
                        </NativeSelect>
                      )}
                    </li>
                  ))}
                </ul>
              </div>
            )}
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Link works for" htmlFor="inv-days">
                <NativeSelect id="inv-days" value={f.expires_in_days} onChange={(e) => setF({ ...f, expires_in_days: Number(e.target.value) })}>
                  <option value={1}>1 day</option>
                  <option value={7}>7 days</option>
                  <option value={30}>30 days</option>
                </NativeSelect>
              </Field>
              <Field label="Note to them (optional)" htmlFor="inv-note">
                <Input id="inv-note" value={f.note} onChange={(e) => setF({ ...f, note: e.target.value })} />
              </Field>
            </div>
            <div className="divide-y divide-border">
              <SwitchRow label="Make them an administrator" checked={f.is_admin} onCheckedChange={(v) => setF({ ...f, is_admin: v })} />
              {f.email && <SwitchRow label="Email them the link" description="Needs email set up under Administration → Email." checked={f.send_email} onCheckedChange={(v) => setF({ ...f, send_email: v })} />}
            </div>
            {err && !err.fields.length && <p className="text-sm text-danger">{err.message}</p>}
            <DialogFooter>
              <Button onClick={onClose}>Cancel</Button>
              <Button type="submit" variant="primary" loading={busy}>
                Create invitation
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

/* ------------------------------------------------------------------ Export */

export function DataAdmin() {
  return (
    <>
      <SettingsCard title="Export everything" description="A zip with every document and its text, notes, tags, custom fields and original files, in readable folders. It can be imported into another Docveta, or kept as a backup you can open without Docveta.">
        <Button asChild variant="primary">
          <a href="/api/v1/admin/export">
            <Download /> Download export
          </a>
        </Button>
        <p className="mt-3 text-xs text-muted">Large libraries take a while to start; the download streams as it is built. For very large libraries use the command line instead:</p>
        <pre className="mt-2 overflow-x-auto rounded-md bg-surface-2 p-3 font-mono text-xs">docveta export --zip docveta-export.zip</pre>
      </SettingsCard>
      <SettingsCard title="Import" description="Import an export on the server. It's safe to run twice: documents that are already there are skipped.">
        <pre className="overflow-x-auto rounded-md bg-surface-2 p-3 font-mono text-xs">{`unzip docveta-export.zip -d export\ndocveta import --dir export`}</pre>
      </SettingsCard>
    </>
  );
}

