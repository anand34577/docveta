import * as React from "react";
import { useParams } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Activity, Bell, Cpu, KeyRound, Mail, Plus, RotateCw, ScrollText, Server, Trash2, Users } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError, errorMessage } from "@/lib/api";
import type { AuditEntry, OIDCConfig, ProcessingSettings, QueueStats, SMTPConfig, SystemInfo, TaskView, User, Worker } from "@/lib/types";
import { cn, formatBytes, formatDateTime, timeAgo } from "@/lib/utils";
import { useCurrentUser } from "@/components/app-shell";
import { SecretReveal, SettingsCard, SettingsLayout } from "@/components/settings-layout";
import { Button } from "@/components/ui/button";
import { Field, Input, NativeSelect } from "@/components/ui/input";
import { Avatar, Badge, EmptyState, Skeleton, SwitchRow } from "@/components/ui/misc";
import { Dialog, DialogContent, DialogFooter } from "@/components/ui/overlay";
import { confirm } from "@/components/ui/confirm";
import { Notifications } from "./settings";
import { NotFound } from "./not-found";

const sections = [
  { id: "users", label: "Users", icon: <Users /> },
  { id: "processing", label: "Processing", icon: <Cpu /> },
  { id: "authentication", label: "Single sign-on", icon: <KeyRound /> },
  { id: "email", label: "Email", icon: <Mail /> },
  { id: "alerts", label: "Alerts", icon: <Bell /> },
  { id: "system", label: "System", icon: <Server /> },
  { id: "audit", label: "Audit log", icon: <ScrollText /> },
];

export function AdminPage() {
  const me = useCurrentUser();
  const params = useParams({ strict: false }) as { section?: string };
  const active = params.section ?? "users";
  if (!me.is_admin) return <NotFound />;
  return (
    <SettingsLayout title="Administration" base="/admin" sections={sections} active={active}>
      {active === "users" && <UsersAdmin />}
      {active === "processing" && <ProcessingAdmin />}
      {active === "authentication" && <OIDCAdmin />}
      {active === "email" && <SMTPAdmin />}
      {active === "alerts" && <Notifications system />}
      {active === "system" && <SystemAdmin />}
      {active === "audit" && <AuditAdmin />}
    </SettingsLayout>
  );
}

/* ------------------------------------------------------------------ Users */

function UsersAdmin() {
  const me = useCurrentUser();
  const qc = useQueryClient();
  const users = useQuery({ queryKey: ["admin-users"], queryFn: () => api.get<{ items: User[] }>("/admin/users").then((r) => r.items) });
  const [dialog, setDialog] = React.useState<User | "new" | null>(null);
  const refresh = () => {
    qc.invalidateQueries({ queryKey: ["admin-users"] });
    qc.invalidateQueries({ queryKey: ["directory"] });
  };
  const patch = async (u: User, body: Record<string, unknown>) => {
    try {
      await api.patch(`/admin/users/${u.id}`, body);
      refresh();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };
  const remove = async (u: User) => {
    if (!(await confirm({ title: `Delete ${u.display_name}?`, body: "Their personal space must be empty. Documents they added to shared spaces stay.", confirmLabel: "Delete user", destructive: true }))) return;
    try {
      await api.del(`/admin/users/${u.id}`);
      refresh();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };
  return (
    <SettingsCard
      title="Users"
      description="Everyone gets a private personal space. Add people to shared spaces from the space's Members page."
      actions={
        <Button size="sm" variant="primary" onClick={() => setDialog("new")}>
          <Plus /> Add user
        </Button>
      }
    >
      {users.isLoading ? (
        <Skeleton className="h-24" />
      ) : (
        <ul className="divide-y divide-border">
          {(users.data ?? []).map((u) => (
            <li key={u.id} className="flex flex-wrap items-center gap-3 py-3">
              <Avatar name={u.display_name} />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-1.5 text-sm font-medium">
                  {u.display_name}
                  {u.is_admin && <Badge tone="accent">Admin</Badge>}
                  {u.status === "disabled" && <Badge tone="danger">Disabled</Badge>}
                  {!u.has_password && <Badge>SSO only</Badge>}
                </div>
                <div className="text-xs text-subtle">
                  {u.email} · {u.last_login_at ? `last seen ${timeAgo(u.last_login_at)}` : "never signed in"}
                </div>
              </div>
              <Button size="sm" variant="ghost" onClick={() => setDialog(u)}>
                Edit
              </Button>
              {u.id !== me.id && (
                <>
                  <Button size="sm" variant="ghost" onClick={() => patch(u, { status: u.status === "active" ? "disabled" : "active" })}>
                    {u.status === "active" ? "Disable" : "Enable"}
                  </Button>
                  <Button size="icon-sm" variant="ghost" onClick={() => remove(u)} aria-label={`Delete ${u.display_name}`}>
                    <Trash2 />
                  </Button>
                </>
              )}
            </li>
          ))}
        </ul>
      )}
      {dialog && <UserDialog user={dialog === "new" ? null : dialog} onClose={() => setDialog(null)} onSaved={refresh} />}
    </SettingsCard>
  );
}

function UserDialog({ user, onClose, onSaved }: { user: User | null; onClose: () => void; onSaved: () => void }) {
  const me = useCurrentUser();
  const [name, setName] = React.useState(user?.display_name ?? "");
  const [email, setEmail] = React.useState(user?.email ?? "");
  const [password, setPassword] = React.useState("");
  const [admin, setAdmin] = React.useState(user?.is_admin ?? false);
  const [err, setErr] = React.useState<ApiError | null>(null);
  const [busy, setBusy] = React.useState(false);
  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      if (user) {
        const body: Record<string, unknown> = { display_name: name };
        if (user.id !== me.id) body.is_admin = admin;
        if (password) body.password = password;
        await api.patch(`/admin/users/${user.id}`, body);
      } else {
        await api.post("/admin/users", { email, display_name: name, password: password || null, is_admin: admin });
      }
      onSaved();
      onClose();
      toast.success(user ? "User updated" : "User added. Share the password with them securely.");
    } catch (e) {
      setErr(e as ApiError);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={user ? `Edit ${user.display_name}` : "Add user"}>
        <form onSubmit={save} className="space-y-4">
          <Field label="Name" htmlFor="u-name" error={err?.fieldError("display_name")}>
            <Input id="u-name" required value={name} onChange={(e) => setName(e.target.value)} autoFocus />
          </Field>
          {!user && (
            <Field label="Email" htmlFor="u-email" error={err?.fieldError("email") ?? (err?.code === "email_taken" ? err.message : undefined)}>
              <Input id="u-email" type="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
            </Field>
          )}
          <Field
            label={user ? "Reset password" : "Password"}
            htmlFor="u-pw"
            hint={user ? "Leave empty to keep the current password. Resetting signs them out everywhere." : "Leave empty if they'll sign in with single sign-on."}
            error={err?.fieldError("password")}
          >
            <Input id="u-pw" type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
          {user?.id !== me.id && <SwitchRow label="Administrator" description="Can manage users, workers and settings. Doesn't automatically see others' documents." checked={admin} onCheckedChange={setAdmin} />}
          {err && !err.fields.length && err.code !== "email_taken" && <p className="text-sm text-danger">{err.message}</p>}
          <DialogFooter>
            <Button onClick={onClose}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy}>
              {user ? "Save" : "Add user"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/* ------------------------------------------------------------------ Processing */

function ProcessingAdmin() {
  const qc = useQueryClient();
  const workers = useQuery({ queryKey: ["workers"], queryFn: () => api.get<{ items: Worker[] }>("/admin/workers").then((r) => r.items), refetchInterval: 10_000 });
  const [status, setStatus] = React.useState("");
  const tasks = useQuery({
    queryKey: ["tasks", status],
    queryFn: () => api.get<{ items: TaskView[]; stats: QueueStats }>("/admin/tasks", { status, limit: 100 }),
    refetchInterval: 5000,
  });
  const [newWorker, setNewWorker] = React.useState(false);
  const [token, setToken] = React.useState<{ name: string; token: string } | null>(null);

  const action = async (fn: () => Promise<unknown>, msg?: string) => {
    try {
      await fn();
      qc.invalidateQueries({ queryKey: ["workers"] });
      qc.invalidateQueries({ queryKey: ["tasks"] });
      if (msg) toast.success(msg);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };
  const st = tasks.data?.stats;

  return (
    <>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {[
          ["Waiting", st?.queued],
          ["In progress", st?.leased],
          ["Pages read (24h)", st?.pages_done_24h],
          ["Failed (24h)", st?.failed_24h],
        ].map(([l, v]) => (
          <div key={l as string} className="rounded-xl border border-border bg-surface p-4 shadow-sm">
            <div className="text-2xl font-semibold tabular-nums">{v ?? "–"}</div>
            <div className="text-[13px] text-muted">{l}</div>
          </div>
        ))}
      </div>

      <SettingsCard
        title="Processing workers"
        description="Workers read text from scans (OCR). Run one on your Rockchip NPU board (RK3588/RK3576/RK3566) or any computer; it connects to Docveta with a token."
        actions={
          <Button size="sm" variant="primary" onClick={() => setNewWorker(true)}>
            <Plus /> Add worker
          </Button>
        }
      >
        {(workers.data ?? []).length === 0 ? (
          <EmptyState icon={<Cpu />} title="No workers yet" className="py-8">
            Without a worker, Docveta still stores and shows everything and searches PDFs that already contain text. Add a worker to read scans and photos.
          </EmptyState>
        ) : (
          <ul className="divide-y divide-border">
            {workers.data!.map((w) => (
              <li key={w.id} className="py-3">
                <div className="flex flex-wrap items-center gap-3">
                  <span className={cn("size-2.5 rounded-full", w.online ? "bg-success" : "bg-border-strong")} title={w.online ? "Online" : "Offline"} />
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-1.5 text-sm font-medium">
                      {w.name}
                      {!w.enabled && <Badge tone="danger">Disabled</Badge>}
                      {w.version && <span className="text-xs font-normal text-subtle">v{w.version}</span>}
                    </div>
                    <div className="text-xs text-subtle">
                      {w.online ? "Online" : w.last_seen_at ? `Last seen ${timeAgo(w.last_seen_at)}` : "Never connected"}
                      {w.host && ` · ${w.host}`} · {w.active_tasks} active · {w.done_24h} done / {w.failed_24h} failed today
                    </div>
                  </div>
                  <Button size="sm" variant="ghost" onClick={() => action(() => api.patch(`/admin/workers/${w.id}`, { enabled: !w.enabled }))}>
                    {w.enabled ? "Disable" : "Enable"}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={async () => {
                      if (!(await confirm({ title: `New token for ${w.name}?`, body: "The old token stops working immediately.", confirmLabel: "Rotate token" }))) return;
                      try {
                        const r = await api.post<{ token: string }>(`/admin/workers/${w.id}/rotate-token`);
                        setToken({ name: w.name, token: r.token });
                      } catch (e) {
                        toast.error(errorMessage(e));
                      }
                    }}
                  >
                    New token
                  </Button>
                  <Button
                    size="icon-sm"
                    variant="ghost"
                    aria-label={`Remove ${w.name}`}
                    onClick={async () => {
                      if (await confirm({ title: `Remove ${w.name}?`, confirmLabel: "Remove", destructive: true })) void action(() => api.del(`/admin/workers/${w.id}`));
                    }}
                  >
                    <Trash2 />
                  </Button>
                </div>
                {w.capabilities.length > 0 && (
                  <div className="ml-5 mt-2 flex flex-wrap gap-1.5">
                    {w.capabilities.map((c, i) => (
                      <span key={i} className="rounded-md bg-surface-2 px-2 py-1 text-xs text-muted">
                        <span className="font-medium text-fg">{c.task_type}</span> · {c.engine}
                        {c.languages?.length ? ` · ${c.languages.join(", ")}` : ""}
                        {c.tags?.length ? ` · ${c.tags.join(", ")}` : ""}
                        {c.concurrency ? ` · ×${c.concurrency}` : ""}
                      </span>
                    ))}
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
      </SettingsCard>

      <ProcessingSettingsCard />

      <SettingsCard
        title="Recent tasks"
        actions={
          <NativeSelect value={status} onChange={(e) => setStatus(e.target.value)} className="h-8 w-auto text-[13px]">
            <option value="">All</option>
            <option value="queued">Waiting</option>
            <option value="leased">In progress</option>
            <option value="failed">Failed</option>
            <option value="done">Done</option>
          </NativeSelect>
        }
      >
        {(tasks.data?.items ?? []).length === 0 ? (
          <EmptyState icon={<Activity />} title="No tasks" className="py-6" />
        ) : (
          <div className="-mx-5 overflow-x-auto">
            <table className="w-full text-left text-[13px]">
              <thead className="text-xs text-subtle">
                <tr>
                  <th className="px-5 py-2 font-medium">Document</th>
                  <th className="px-2 py-2 font-medium">Task</th>
                  <th className="px-2 py-2 font-medium">Status</th>
                  <th className="px-2 py-2 font-medium">Worker</th>
                  <th className="px-5 py-2 font-medium" />
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {tasks.data!.items.map((t) => (
                  <tr key={t.id}>
                    <td className="max-w-56 px-5 py-2">
                      <a href={`/documents/${t.document_id}`} className="block truncate hover:underline">
                        {t.document_title}
                      </a>
                      {t.last_error && <div className="truncate text-xs text-danger" title={t.last_error}>{t.last_error}</div>}
                    </td>
                    <td className="whitespace-nowrap px-2 py-2 text-muted">
                      {t.type}
                      {t.page_from ? ` p${t.page_from}–${t.page_to}` : ""}
                    </td>
                    <td className="px-2 py-2">
                      <Badge tone={t.status === "done" ? "success" : t.status === "failed" ? "danger" : t.status === "leased" ? "accent" : "neutral"}>{t.status}</Badge>
                      {t.attempt > 1 && <span className="ml-1 text-xs text-subtle">try {t.attempt}</span>}
                    </td>
                    <td className="px-2 py-2 text-muted">{t.worker ?? "—"}</td>
                    <td className="px-5 py-2 text-right">
                      {t.status === "failed" && (
                        <Button size="sm" variant="ghost" onClick={() => action(() => api.post(`/admin/tasks/${t.id}/retry`), "Queued again")}>
                          <RotateCw /> Retry
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </SettingsCard>

      <NewWorkerDialog open={newWorker} onClose={() => setNewWorker(false)} onCreated={(n, t) => { setToken({ name: n, token: t }); qc.invalidateQueries({ queryKey: ["workers"] }); }} />
      {token && (
        <Dialog open onOpenChange={(o) => !o && setToken(null)}>
          <DialogContent title={`Token for ${token.name}`}>
            <SecretReveal label="Worker token" value={token.token} />
            <div className="mt-4 text-sm">
              <div className="mb-1.5 font-medium">Start the Rockchip NPU worker</div>
              <pre className="overflow-x-auto rounded-md bg-surface-2 p-3 text-xs leading-relaxed">
{`# On the Rockchip board, from the Docveta repository:
cd workers && docker build -t docveta-worker-rknn -f rknn/Dockerfile .
docker run -d --name docveta-worker --restart unless-stopped \\
  --device /dev/rknpu --device /dev/dri \\
  -v /opt/docveta/models:/app/models:ro \\
  -e DOCVETA_URL=${window.location.origin} \\
  -e DOCVETA_WORKER_TOKEN='<the token above>' \\
  docveta-worker-rknn`}
              </pre>
              <p className="mt-2 text-xs text-muted">No NPU? The CPU worker in <code>workers/tesseract</code> runs on any computer. See docs/workers.md.</p>
            </div>
            <DialogFooter>
              <Button variant="primary" onClick={() => setToken(null)}>
                Done
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </>
  );
}

function NewWorkerDialog({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: (name: string, token: string) => void }) {
  const [name, setName] = React.useState("rk3588-npu-1");
  const [err, setErr] = React.useState<ApiError | null>(null);
  const [busy, setBusy] = React.useState(false);
  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const r = await api.post<{ token: string }>("/admin/workers", { name });
      onClose();
      onCreated(name, r.token);
    } catch (e) {
      setErr(e as ApiError);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent title="Add processing worker" size="sm">
        <form onSubmit={create} className="space-y-4">
          <Field label="Name" htmlFor="w-name" hint="A name to recognise this machine." error={err?.fieldError("name") ?? (err && !err.fields.length ? err.message : undefined)}>
            <Input id="w-name" required value={name} onChange={(e) => setName(e.target.value)} autoFocus />
          </Field>
          <DialogFooter>
            <Button onClick={onClose}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy}>
              Create & show token
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function ProcessingSettingsCard() {
  const q = useQuery({ queryKey: ["processing-settings"], queryFn: () => api.get<ProcessingSettings>("/admin/settings/processing") });
  const [s, setS] = React.useState<ProcessingSettings | null>(null);
  const [busy, setBusy] = React.useState(false);
  React.useEffect(() => {
    if (q.data) setS(q.data);
  }, [q.data]);
  if (!s) return <Skeleton className="h-40" />;
  const save = async () => {
    setBusy(true);
    try {
      const r = await api.put<ProcessingSettings>("/admin/settings/processing", s);
      setS(r);
      toast.success("Saved");
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <SettingsCard title="Processing settings" actions={<Button size="sm" variant="primary" loading={busy} onClick={save}>Save</Button>}>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Prefer workers tagged" htmlFor="ps-tags" hint="Tasks go to workers with these tags first (e.g. npu).">
          <Input id="ps-tags" value={s.prefer_tags.join(", ")} onChange={(e) => setS({ ...s, prefer_tags: e.target.value.split(",").map((x) => x.trim()).filter(Boolean) })} />
        </Field>
        <Field label="Fall back to any worker after (minutes)" htmlFor="ps-fb" hint="0 = immediately.">
          <Input id="ps-fb" type="number" min={0} value={s.fallback_after_minutes} onChange={(e) => setS({ ...s, fallback_after_minutes: Number(e.target.value) })} />
        </Field>
        <Field label="Pages per task" htmlFor="ps-batch" hint="Large PDFs are split so several workers/NPU cores can share them.">
          <Input id="ps-batch" type="number" min={1} max={200} value={s.page_batch_size} onChange={(e) => setS({ ...s, page_batch_size: Number(e.target.value) })} />
        </Field>
        <Field label="Attempts per task" htmlFor="ps-att">
          <Input id="ps-att" type="number" min={1} max={10} value={s.max_attempts} onChange={(e) => setS({ ...s, max_attempts: Number(e.target.value) })} />
        </Field>
      </div>
      <div className="mt-2 divide-y divide-border">
        <SwitchRow label="Skip OCR when a PDF already has text" description="Saves time on PDFs created by software (bank statements, e-bills)." checked={s.skip_ocr_with_text} onCheckedChange={(v) => setS({ ...s, skip_ocr_with_text: v })} />
        <SwitchRow label="Create searchable PDFs" description="Adds an invisible text layer to scans so you can select and search text in any PDF reader." checked={s.archive} onCheckedChange={(v) => setS({ ...s, archive: v })} />
      </div>
    </SettingsCard>
  );
}

/* ------------------------------------------------------------------ OIDC */

function OIDCAdmin() {
  const q = useQuery({ queryKey: ["oidc"], queryFn: () => api.get<OIDCConfig>("/admin/settings/oidc") });
  const [c, setC] = React.useState<OIDCConfig | null>(null);
  const [secret, setSecret] = React.useState("");
  const [err, setErr] = React.useState<ApiError | null>(null);
  const [busy, setBusy] = React.useState(false);
  React.useEffect(() => {
    if (q.data) setC(q.data);
  }, [q.data]);
  if (!c) return <Skeleton className="h-60" />;
  const list = (v: string) => v.split(",").map((x) => x.trim()).filter(Boolean);
  const save = async () => {
    setBusy(true);
    setErr(null);
    try {
      const body: OIDCConfig = { ...c };
      if (secret) body.client_secret = secret;
      else delete body.client_secret;
      const r = await api.put<OIDCConfig>("/admin/settings/oidc", body);
      setC(r);
      setSecret("");
      toast.success("Saved");
    } catch (e) {
      setErr(e as ApiError);
    } finally {
      setBusy(false);
    }
  };
  return (
    <SettingsCard
      title="Single sign-on (OIDC)"
      description="Let people sign in with Authentik, Keycloak, Authelia, Pocket ID, Google and other OpenID Connect providers."
      actions={<Button size="sm" variant="primary" loading={busy} onClick={save}>Save</Button>}
    >
      <SwitchRow label="Enable single sign-on" checked={c.enabled} onCheckedChange={(v) => setC({ ...c, enabled: v })} />
      <div className="mt-2 grid gap-4 sm:grid-cols-2">
        <Field label="Issuer URL" htmlFor="o-iss" className="sm:col-span-2" error={err?.fieldError("issuer")}>
          <Input id="o-iss" value={c.issuer} placeholder="https://auth.example.com/application/o/docveta/" onChange={(e) => setC({ ...c, issuer: e.target.value })} />
        </Field>
        <Field label="Client ID" htmlFor="o-cid" error={err?.fieldError("client_id")}>
          <Input id="o-cid" value={c.client_id} onChange={(e) => setC({ ...c, client_id: e.target.value })} />
        </Field>
        <Field label="Client secret" htmlFor="o-sec" hint={c.has_client_secret ? "A secret is saved. Enter a new one to replace it." : undefined}>
          <Input id="o-sec" type="password" value={secret} placeholder={c.has_client_secret ? "••••••••" : ""} onChange={(e) => setSecret(e.target.value)} />
        </Field>
        <Field label="Redirect URI (add this in your provider)" htmlFor="o-red" className="sm:col-span-2">
          <Input id="o-red" readOnly value={`${window.location.origin}/api/v1/auth/oidc/callback`} onFocus={(e) => e.currentTarget.select()} />
        </Field>
        <Field label="Button label" htmlFor="o-btn">
          <Input id="o-btn" value={c.button_label} onChange={(e) => setC({ ...c, button_label: e.target.value })} />
        </Field>
        <Field label="Scopes" htmlFor="o-sc">
          <Input id="o-sc" value={c.scopes.join(", ")} onChange={(e) => setC({ ...c, scopes: list(e.target.value) })} />
        </Field>
        <Field label="Groups claim" htmlFor="o-gc">
          <Input id="o-gc" value={c.groups_claim} onChange={(e) => setC({ ...c, groups_claim: e.target.value })} />
        </Field>
        <Field label="Allowed groups" htmlFor="o-ag" hint="Empty = anyone your provider lets through.">
          <Input id="o-ag" value={c.allowed_groups.join(", ")} onChange={(e) => setC({ ...c, allowed_groups: list(e.target.value) })} />
        </Field>
        <Field label="Admin groups" htmlFor="o-adm" hint="Members become Docveta administrators.">
          <Input id="o-adm" value={c.admin_groups.join(", ")} onChange={(e) => setC({ ...c, admin_groups: list(e.target.value) })} />
        </Field>
      </div>
      <div className="mt-3 divide-y divide-border">
        <SwitchRow label="Create accounts automatically" description="New people who sign in get an account and a personal space." checked={c.auto_provision} onCheckedChange={(v) => setC({ ...c, auto_provision: v })} />
        <SwitchRow
          label="Link to existing accounts by verified email"
          description="Only enable if you trust your provider to verify email addresses."
          checked={c.link_by_verified_email}
          onCheckedChange={(v) => setC({ ...c, link_by_verified_email: v })}
        />
        <SwitchRow
          label="Disable password sign-in"
          description="Everyone must use single sign-on. Recovery: docveta user reset-password on the server."
          checked={c.disable_password_login}
          onCheckedChange={(v) => setC({ ...c, disable_password_login: v })}
        />
      </div>
      {err && !err.fields.length && <p className="mt-3 text-sm text-danger">{err.message}</p>}
    </SettingsCard>
  );
}

/* ------------------------------------------------------------------ SMTP */

function SMTPAdmin() {
  const q = useQuery({ queryKey: ["smtp"], queryFn: () => api.get<SMTPConfig>("/admin/settings/smtp") });
  const [c, setC] = React.useState<SMTPConfig | null>(null);
  const [pw, setPw] = React.useState("");
  const [err, setErr] = React.useState<ApiError | null>(null);
  const [busy, setBusy] = React.useState(false);
  React.useEffect(() => {
    if (q.data) setC(q.data);
  }, [q.data]);
  if (!c) return <Skeleton className="h-60" />;
  const save = async () => {
    setBusy(true);
    setErr(null);
    try {
      const body: SMTPConfig = { ...c };
      if (pw) body.password = pw;
      else delete body.password;
      setC(await api.put<SMTPConfig>("/admin/settings/smtp", body));
      setPw("");
      toast.success("Saved");
    } catch (e) {
      setErr(e as ApiError);
    } finally {
      setBusy(false);
    }
  };
  const test = async () => {
    const t = toast.loading("Sending test email…");
    try {
      await api.post("/admin/settings/smtp/test");
      toast.success("Test email sent to your address", { id: t });
    } catch (e) {
      toast.error(errorMessage(e), { id: t });
    }
  };
  return (
    <SettingsCard
      title="Email (SMTP)"
      description="Used for email notifications."
      actions={
        <>
          <Button size="sm" onClick={test} disabled={!c.enabled}>
            Send test
          </Button>
          <Button size="sm" variant="primary" loading={busy} onClick={save}>
            Save
          </Button>
        </>
      }
    >
      <SwitchRow label="Enable email" checked={c.enabled} onCheckedChange={(v) => setC({ ...c, enabled: v })} />
      <div className="mt-2 grid gap-4 sm:grid-cols-3">
        <Field label="Server" htmlFor="m-host" className="sm:col-span-2" error={err?.fieldError("host")}>
          <Input id="m-host" value={c.host} placeholder="smtp.gmail.com" onChange={(e) => setC({ ...c, host: e.target.value })} />
        </Field>
        <Field label="Port" htmlFor="m-port" error={err?.fieldError("port")}>
          <Input id="m-port" type="number" value={c.port} onChange={(e) => setC({ ...c, port: Number(e.target.value) })} />
        </Field>
        <Field label="Security" htmlFor="m-sec">
          <NativeSelect id="m-sec" value={c.security} onChange={(e) => setC({ ...c, security: e.target.value as SMTPConfig["security"] })}>
            <option value="starttls">STARTTLS (587)</option>
            <option value="tls">TLS (465)</option>
            <option value="none">None (not recommended)</option>
          </NativeSelect>
        </Field>
        <Field label="Username" htmlFor="m-user">
          <Input id="m-user" value={c.username} onChange={(e) => setC({ ...c, username: e.target.value })} />
        </Field>
        <Field label="Password" htmlFor="m-pw" hint={c.has_password ? "Saved. Enter a new one to replace it." : undefined}>
          <Input id="m-pw" type="password" value={pw} placeholder={c.has_password ? "••••••••" : ""} onChange={(e) => setPw(e.target.value)} />
        </Field>
        <Field label="From address" htmlFor="m-from" className="sm:col-span-3" error={err?.fieldError("from")}>
          <Input id="m-from" value={c.from} placeholder="Docveta <docveta@example.com>" onChange={(e) => setC({ ...c, from: e.target.value })} />
        </Field>
      </div>
    </SettingsCard>
  );
}

/* ------------------------------------------------------------------ System */

function SystemAdmin() {
  const q = useQuery({ queryKey: ["system"], queryFn: () => api.get<SystemInfo>("/admin/system"), refetchInterval: 30_000 });
  const s = q.data;
  if (!s) return <Skeleton className="h-60" />;
  const rows: [string, React.ReactNode][] = [
    ["Version", s.version],
    ["Platform", `${s.platform} · ${s.go_version}`],
    ["Documents", `${s.documents.toLocaleString()} (${s.pages.toLocaleString()} pages)`],
    ["Users", s.users],
    ["File storage used", formatBytes(s.storage_bytes)],
    ["Free disk space", s.storage_free_bytes >= 0 ? formatBytes(s.storage_free_bytes) : "unknown"],
    ["Database size", formatBytes(s.database_bytes)],
    ["Workers online", `${s.workers_online} of ${s.workers_total}`],
    ["Processing queue", `${s.queue.queued} waiting · ${s.queue.leased} in progress`],
  ];
  return (
    <SettingsCard title="System" description="Health and capacity of this Docveta server.">
      <dl className="grid gap-x-6 gap-y-2.5 text-sm sm:grid-cols-[200px_1fr]">
        {rows.map(([k, v]) => (
          <React.Fragment key={k}>
            <dt className="text-muted">{k}</dt>
            <dd className="font-medium">{v}</dd>
          </React.Fragment>
        ))}
      </dl>
      <p className="mt-5 text-xs text-subtle">
        Back up regularly: the database (pg_dump) and the data directory. See docs/operations.md. Metrics for Prometheus are at /metrics.
      </p>
    </SettingsCard>
  );
}

/* ------------------------------------------------------------------ Audit */

function AuditAdmin() {
  const [action, setAction] = React.useState("");
  const q = useQuery({ queryKey: ["audit", action], queryFn: () => api.get<{ items: AuditEntry[] }>("/admin/audit", { action, limit: 200 }).then((r) => r.items) });
  return (
    <SettingsCard
      title="Audit log"
      description="Sign-ins, account and security changes, and admin actions."
      actions={
        <NativeSelect value={action} onChange={(e) => setAction(e.target.value)} className="h-8 w-auto text-[13px]">
          <option value="">All</option>
          <option value="auth.">Sign-ins</option>
          <option value="user.">Users</option>
          <option value="token.">API tokens</option>
          <option value="worker.">Workers</option>
          <option value="settings.">Settings</option>
          <option value="space.">Spaces</option>
        </NativeSelect>
      }
    >
      {(q.data ?? []).length === 0 ? (
        <EmptyState title="Nothing logged yet" className="py-6" />
      ) : (
        <ul className="divide-y divide-border">
          {q.data!.map((e) => (
            <li key={e.id} className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 py-2.5 text-[13px]">
              <span className={cn("font-medium", e.action.includes("failed") && "text-danger")}>{e.action}</span>
              <span className="text-muted">{e.actor_name || e.actor_type}</span>
              {e.details && Object.keys(e.details).length > 0 && (
                <span className="truncate text-xs text-subtle">{Object.entries(e.details).map(([k, v]) => `${k}: ${typeof v === "object" ? JSON.stringify(v) : v}`).join(" · ")}</span>
              )}
              <span className="ml-auto whitespace-nowrap text-xs text-subtle">
                {e.ip} · {formatDateTime(e.at)}
              </span>
            </li>
          ))}
        </ul>
      )}
    </SettingsCard>
  );
}
