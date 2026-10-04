import * as React from "react";
import { useParams } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Bell, Bookmark, KeyRound, Link2, LogIn, Monitor, Plus, Shield, Smartphone, Trash2, User } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError, errorMessage } from "@/lib/api";
import { keys, useChannels, useStatus } from "@/lib/queries";
import type { ApiToken, Channel, Identity, Session } from "@/lib/types";
import { formatDateTime, setDateFormat, timeAgo } from "@/lib/utils";
import { ThemePicker, useCurrentUser } from "@/components/app-shell";
import { SecretReveal, SettingsCard, SettingsLayout } from "@/components/settings-layout";
import { Button } from "@/components/ui/button";
import { Field, Input, NativeSelect } from "@/components/ui/input";
import { Badge, Checkbox, EmptyState, Switch } from "@/components/ui/misc";
import { Dialog, DialogContent, DialogFooter } from "@/components/ui/overlay";
import { confirm } from "@/components/ui/confirm";
import { TwoFactorCard } from "@/components/two-factor";
import { QuietHours } from "@/components/quiet-hours";
import { ViewsManager } from "@/components/views-manager";

const sections = [
  { id: "profile", label: "Profile", icon: <User /> },
  { id: "security", label: "Security", icon: <Shield /> },
  { id: "notifications", label: "Notifications", icon: <Bell /> },
  { id: "views", label: "Saved views", icon: <Bookmark /> },
  { id: "api", label: "API tokens", icon: <KeyRound /> },
];

export function SettingsPage() {
  const params = useParams({ strict: false }) as { section?: string };
  const active = params.section ?? "profile";
  return (
    <SettingsLayout title="Settings" base="/settings" sections={sections} active={active}>
      {active === "profile" && <Profile />}
      {active === "security" && <Security />}
      {active === "notifications" && <Notifications />}
      {active === "views" && <ViewsManager />}
      {active === "api" && <Tokens />}
    </SettingsLayout>
  );
}

/* ------------------------------------------------------------------ Profile */

function Profile() {
  const me = useCurrentUser();
  const qc = useQueryClient();
  const [name, setName] = React.useState(me.display_name);
  const [busy, setBusy] = React.useState(false);
  const save = async (patch: Record<string, string>) => {
    setBusy(true);
    try {
      await api.patch("/me", patch);
      if (patch.date_format) setDateFormat(patch.date_format);
      await qc.invalidateQueries({ queryKey: keys.me });
      toast.success("Saved");
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  // Browsers list some zones under old names (Chrome has Asia/Calcutta, not Asia/Kolkata):
  // keep the saved zone selectable so the picker never shows the wrong one.
  const zones = React.useMemo(() => {
    const all = (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf?.("timeZone") ?? [];
    return all.includes(me.timezone) ? all : [me.timezone, ...all];
  }, [me.timezone]);
  return (
    <>
    <SettingsCard title="Profile" description={me.email}>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Name" htmlFor="p-name" className="sm:col-span-2">
          <div className="flex gap-2">
            <Input id="p-name" value={name} onChange={(e) => setName(e.target.value)} />
            <Button loading={busy} disabled={name.trim() === me.display_name || !name.trim()} onClick={() => save({ display_name: name.trim() })}>
              Save
            </Button>
          </div>
        </Field>
        <Field label="Date format" htmlFor="p-df" hint="Also decides how dates like 03/04 are read from documents.">
          <NativeSelect id="p-df" value={me.date_format} onChange={(e) => save({ date_format: e.target.value })}>
            <option value="DD/MM/YYYY">31/12/2026 (DD/MM/YYYY)</option>
            <option value="MM/DD/YYYY">12/31/2026 (MM/DD/YYYY)</option>
            <option value="YYYY-MM-DD">2026-12-31 (ISO)</option>
            <option value="DD.MM.YYYY">31.12.2026</option>
            <option value="D MMM YYYY">31 Dec 2026</option>
          </NativeSelect>
        </Field>
        <Field label="Time zone" htmlFor="p-tz">
          <NativeSelect id="p-tz" value={me.timezone} onChange={(e) => save({ timezone: e.target.value })}>
            {zones.map((z) => (
              <option key={z} value={z}>
                {z.replace(/_/g, " ")}
              </option>
            ))}
          </NativeSelect>
        </Field>
      </div>
    </SettingsCard>
    <SettingsCard title="Appearance" description="Auto follows your device's light or dark setting.">
      <div className="max-w-xs">
        <ThemePicker />
      </div>
    </SettingsCard>
    </>
  );
}

/* ------------------------------------------------------------------ Security */

function Security() {
  const me = useCurrentUser();
  const qc = useQueryClient();
  const sessions = useQuery({ queryKey: ["sessions"], queryFn: () => api.get<{ items: Session[] }>("/me/sessions").then((r) => r.items) });
  const [cur, setCur] = React.useState("");
  const [next, setNext] = React.useState("");
  const [err, setErr] = React.useState<ApiError | null>(null);
  const [busy, setBusy] = React.useState(false);

  const changePassword = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      await api.post("/me/password", { current_password: cur, new_password: next });
      setCur("");
      setNext("");
      toast.success("Password changed. Other devices were signed out.");
      qc.invalidateQueries({ queryKey: ["sessions"] });
    } catch (e) {
      setErr(e as ApiError);
    } finally {
      setBusy(false);
    }
  };
  const revoke = async (id: string) => {
    await api.del(`/me/sessions/${id}`).catch((e) => toast.error(errorMessage(e)));
    qc.invalidateQueries({ queryKey: ["sessions"] });
  };

  return (
    <>
      <SettingsCard title="Password" description={me.has_password ? "Use at least 10 characters." : "You sign in with single sign-on. You can also set a password."}>
        <form onSubmit={changePassword} className="grid max-w-md gap-4">
          {me.has_password && (
            <Field label="Current password" htmlFor="s-cur" error={err?.fieldError("current_password")}>
              <Input id="s-cur" type="password" autoComplete="current-password" value={cur} onChange={(e) => setCur(e.target.value)} required />
            </Field>
          )}
          <Field label="New password" htmlFor="s-new" error={err?.fieldError("new_password") ?? (err && !err.fields.length ? err.message : undefined)}>
            <Input id="s-new" type="password" autoComplete="new-password" minLength={10} value={next} onChange={(e) => setNext(e.target.value)} required />
          </Field>
          <div>
            <Button type="submit" variant="primary" loading={busy}>
              Change password
            </Button>
          </div>
        </form>
      </SettingsCard>

      <TwoFactorCard />

      <SingleSignOnCard />

      <SettingsCard
        title="Signed-in devices"
        description="Sign out devices you don't recognise."
        actions={
          (sessions.data?.length ?? 0) > 1 && (
            <Button size="sm" variant="danger-ghost" onClick={() => revoke("others")}>
              Sign out all others
            </Button>
          )
        }
      >
        <ul className="divide-y divide-border">
          {(sessions.data ?? []).map((s) => (
            <li key={s.id} className="flex items-center gap-3 py-3">
              {/Android|iPhone|iPad|Mobile/.test(s.user_agent) ? <Smartphone className="size-5 text-muted" /> : <Monitor className="size-5 text-muted" />}
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm">
                  {browserName(s.user_agent)} {s.current && <Badge tone="success">This device</Badge>}
                </div>
                <div className="text-xs text-subtle">
                  {s.ip} · active {timeAgo(s.last_seen_at)}
                </div>
              </div>
              {!s.current && (
                <Button size="sm" variant="ghost" onClick={() => revoke(s.id)}>
                  Sign out
                </Button>
              )}
            </li>
          ))}
        </ul>
      </SettingsCard>
    </>
  );
}

/** Connect or disconnect single sign-on accounts (shown when an administrator turned SSO on). */
function SingleSignOnCard() {
  const status = useStatus();
  const qc = useQueryClient();
  const ids = useQuery({ queryKey: ["identities"], queryFn: () => api.get<{ items: Identity[] }>("/me/identities").then((r) => r.items) });
  React.useEffect(() => {
    // Back from the provider: /settings/security?sso=linked or ?sso_error=…
    const q = new URLSearchParams(window.location.search);
    if (q.get("sso") === "linked") toast.success("Single sign-on connected. You can now use it to sign in.");
    const e = q.get("sso_error");
    if (e) toast.error(e);
    if (q.has("sso") || q.has("sso_error")) window.history.replaceState(null, "", window.location.pathname);
  }, []);
  if (!status.data?.oidc.enabled && !ids.data?.length) return null;
  const unlink = async (id: string) => {
    if (!(await confirm({ title: "Disconnect single sign-on?", body: "You won't be able to sign in with it until you connect it again.", confirmLabel: "Disconnect", destructive: true }))) return;
    await api.del(`/me/identities/${id}`).then(
      () => qc.invalidateQueries({ queryKey: ["identities"] }),
      (e) => toast.error(errorMessage(e)),
    );
  };
  return (
    <SettingsCard
      title="Single sign-on"
      description="Sign in with your organisation's account instead of a password."
      actions={
        status.data?.oidc.enabled && !ids.data?.length && (
          <Button size="sm" asChild>
            <a href={`/api/v1/auth/oidc/start?link=1&return_to=${encodeURIComponent("/settings/security")}`}>
              <LogIn /> Connect
            </a>
          </Button>
        )
      }
    >
      {ids.data?.length ? (
        <ul className="divide-y divide-border">
          {ids.data.map((i) => (
            <li key={i.id} className="flex items-center gap-3 py-3">
              <Link2 className="size-5 shrink-0 text-muted" />
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm">{i.email || "Connected account"}</div>
                <div className="truncate text-xs text-subtle">
                  {hostOf(i.provider)} · connected {timeAgo(i.created_at)}
                </div>
              </div>
              <Button size="sm" variant="ghost" onClick={() => unlink(i.id)}>
                Disconnect
              </Button>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-sm text-muted">No account connected yet. Choose Connect, sign in with your provider, and you'll come back here.</p>
      )}
    </SettingsCard>
  );
}

function hostOf(u: string): string {
  try {
    return new URL(u).host;
  } catch {
    return u;
  }
}

function browserName(ua: string): string {
  const os = /Android/.test(ua) ? "Android" : /iPhone|iPad/.test(ua) ? "iOS" : /Windows/.test(ua) ? "Windows" : /Mac OS/.test(ua) ? "macOS" : /Linux/.test(ua) ? "Linux" : "";
  const br = /Edg\//.test(ua) ? "Edge" : /Firefox\//.test(ua) ? "Firefox" : /Chrome\//.test(ua) ? "Chrome" : /Safari\//.test(ua) ? "Safari" : ua ? "App" : "Unknown";
  return os ? `${br} on ${os}` : br;
}

/* ------------------------------------------------------------------ Notifications */

const eventLabels: Record<string, string> = {
  "document.processed": "Document is ready",
  "document.failed": "Document couldn't be processed",
  "note.mention": "Someone mentions me",
  "reminder.due": "Reminders",
  "security.new_login": "New sign-in to my account",
  "security.token_created": "New API token",
  "worker.offline": "Processing worker offline (admins)",
  "worker.online": "Processing worker back online (admins)",
  "storage.low": "Low disk space (admins)",
  "security.2fa_changed": "Two-step sign-in changed",
  "import.failed": "A watched-folder file couldn't be imported",
  "workflow.notice": "Workflow messages",
};

const channelTypes: Record<Channel["type"], { label: string; fields: readonly (readonly [string, string, string])[] }> = {
  gotify: { label: "Gotify", fields: [["url", "Server URL", "https://gotify.example.com"], ["token", "Application token", ""]] },
  ntfy: { label: "ntfy", fields: [["server", "Server (optional)", "https://ntfy.sh"], ["topic", "Topic", "my-docveta-alerts"], ["token", "Access token (optional)", ""]] },
  email: { label: "Email", fields: [["to", "Send to (optional)", "Defaults to your account email"]] },
  webhook: { label: "Webhook", fields: [["url", "URL", "https://example.com/hooks/docveta"], ["secret", "Signing secret (optional)", "Generated if empty"]] },
  apprise: { label: "Apprise", fields: [["url", "Apprise API URL", "http://apprise:8000"], ["key", "Saved configuration key (optional)", ""], ["urls", "Or service URLs, comma-separated", "tgram://token/chat, discord://id/token"], ["tag", "Tag (optional)", ""]] },
};

export function Notifications({ system }: { system?: boolean }) {
  const channels = useChannels();
  const qc = useQueryClient();
  const [editing, setEditing] = React.useState<Channel | "new" | null>(null);
  const list = (channels.data?.items ?? []).filter((c) => c.system === !!system);
  const remove = async (c: Channel) => {
    if (!(await confirm({ title: `Remove “${c.name}”?`, confirmLabel: "Remove", destructive: true }))) return;
    await api.del(`/notification-channels/${c.id}`).catch((e) => toast.error(errorMessage(e)));
    qc.invalidateQueries({ queryKey: keys.channels });
  };
  const test = async (c: Channel) => {
    const t = toast.loading("Sending test…");
    try {
      await api.post(`/notification-channels/${c.id}/test`);
      toast.success("Test sent", { id: t });
    } catch (e) {
      toast.error(errorMessage(e), { id: t });
    }
  };
  const toggle = async (c: Channel, enabled: boolean) => {
    await api.patch(`/notification-channels/${c.id}`, { enabled }).catch((e) => toast.error(errorMessage(e)));
    qc.invalidateQueries({ queryKey: keys.channels });
  };
  return (
    <>
    <SettingsCard
      title={system ? "Admin alert channels" : "Where to notify me"}
      description={
        system
          ? "Instance alerts (worker offline, low disk) go to these channels, in addition to admins' in-app notifications."
          : "You always get notifications in Docveta. Add Gotify, ntfy, Apprise, email or a webhook to get them on your phone or elsewhere."
      }
      actions={
        <Button size="sm" variant="primary" onClick={() => setEditing("new")}>
          <Plus /> Add channel
        </Button>
      }
    >
      {list.length === 0 ? (
        <EmptyState icon={<Bell />} title="No channels yet" className="py-8" />
      ) : (
        <ul className="divide-y divide-border">
          {list.map((c) => (
            <li key={c.id} className="flex flex-wrap items-center gap-3 py-3">
              <Switch checked={c.enabled} onCheckedChange={(v) => toggle(c, v)} aria-label={`Enable ${c.name}`} />
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium">
                  {c.name} <Badge>{channelTypes[c.type].label}</Badge>
                </div>
                <div className="text-xs text-subtle">{c.events.length ? c.events.map((e) => eventLabels[e] ?? e).join(", ") : "All events"}</div>
                {c.last_error && <div className="mt-0.5 text-xs text-danger">Last error: {c.last_error}</div>}
              </div>
              <Button size="sm" variant="ghost" onClick={() => test(c)}>
                Test
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setEditing(c)}>
                Edit
              </Button>
              <Button size="icon-sm" variant="ghost" onClick={() => remove(c)} aria-label="Remove">
                <Trash2 />
              </Button>
            </li>
          ))}
        </ul>
      )}
      {editing && <ChannelDialog channel={editing === "new" ? null : editing} system={system} eventTypes={channels.data?.event_types ?? []} onClose={() => setEditing(null)} />}
    </SettingsCard>
    {!system && <QuietHours />}
    </>
  );
}

function ChannelDialog({ channel, system, eventTypes, onClose }: { channel: Channel | null; system?: boolean; eventTypes: string[]; onClose: () => void }) {
  const qc = useQueryClient();
  const [type, setType] = React.useState<Channel["type"]>(channel?.type ?? "gotify");
  const [name, setName] = React.useState(channel?.name ?? "");
  const [config, setConfig] = React.useState<Record<string, string>>(channel?.config ?? {});
  const [events, setEvents] = React.useState<string[]>(channel?.events ?? []);
  const [err, setErr] = React.useState<ApiError | null>(null);
  const [busy, setBusy] = React.useState(false);
  const shownEvents = eventTypes.filter((e) => (system ? e.startsWith("worker.") || e.startsWith("storage.") : true));

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const body = { name: name || channelTypes[type].label, type, config, events, system: !!system };
      if (channel) await api.patch(`/notification-channels/${channel.id}`, { name: body.name, config, events });
      else await api.post("/notification-channels", body);
      qc.invalidateQueries({ queryKey: keys.channels });
      onClose();
    } catch (e) {
      setErr(e as ApiError);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={channel ? `Edit ${channel.name}` : "Add notification channel"}>
        <form onSubmit={save} className="space-y-4">
          {!channel && (
            <div className="grid grid-cols-3 gap-2 sm:grid-cols-5">
              {(Object.keys(channelTypes) as Channel["type"][]).map((t) => (
                <button
                  key={t}
                  type="button"
                  onClick={() => {
                    setType(t);
                    setConfig({});
                  }}
                  className={`rounded-lg border px-2 py-2.5 text-sm ${type === t ? "border-accent bg-accent-soft font-medium" : "border-border hover:bg-surface-2"}`}
                >
                  {channelTypes[t].label}
                </button>
              ))}
            </div>
          )}
          <Field label="Name" htmlFor="c-name">
            <Input id="c-name" value={name} placeholder={channelTypes[type].label} onChange={(e) => setName(e.target.value)} />
          </Field>
          {channelTypes[type].fields.map(([k, label, ph]) => (
            <Field key={k} label={label} htmlFor={`c-${k}`} error={err?.fieldError(`config.${k}`)}>
              <Input id={`c-${k}`} value={config[k] ?? ""} placeholder={ph} onChange={(e) => setConfig({ ...config, [k]: e.target.value })} />
            </Field>
          ))}
          {type === "email" && <p className="text-xs text-muted">Email uses the server's SMTP settings (Administration → Email).</p>}
          <div>
            <div className="mb-2 text-[13px] font-medium">Send these events</div>
            <div className="grid gap-1.5 sm:grid-cols-2">
              {shownEvents.map((ev) => (
                <label key={ev} className="flex items-center gap-2 text-sm">
                  <Checkbox checked={events.includes(ev)} onCheckedChange={(v) => setEvents(v ? [...events, ev] : events.filter((x) => x !== ev))} />
                  {eventLabels[ev] ?? ev}
                </label>
              ))}
            </div>
            <p className="mt-1.5 text-xs text-subtle">Leave all unchecked to receive everything.</p>
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

/* ------------------------------------------------------------------ API tokens */

const scopeLabels: Record<string, string> = {
  "documents:read": "Read documents",
  "documents:write": "Edit documents",
  upload: "Upload documents",
  admin: "Administration",
};

function Tokens() {
  const me = useCurrentUser();
  const qc = useQueryClient();
  const tokens = useQuery({ queryKey: ["tokens"], queryFn: () => api.get<{ items: ApiToken[] }>("/me/tokens").then((r) => r.items) });
  const [open, setOpen] = React.useState(false);
  const [name, setName] = React.useState("");
  const [scopes, setScopes] = React.useState<string[]>(["documents:read", "upload"]);
  const [days, setDays] = React.useState("365");
  const [secret, setSecret] = React.useState<string | null>(null);
  const [busy, setBusy] = React.useState(false);

  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      const r = await api.post<{ secret: string }>("/me/tokens", { name, scopes, expires_in_days: days ? Number(days) : null });
      setSecret(r.secret);
      qc.invalidateQueries({ queryKey: ["tokens"] });
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  const revoke = async (t: ApiToken) => {
    if (!(await confirm({ title: `Revoke “${t.name}”?`, body: "Apps using this token will stop working.", confirmLabel: "Revoke", destructive: true }))) return;
    await api.del(`/me/tokens/${t.id}`).catch((e) => toast.error(errorMessage(e)));
    qc.invalidateQueries({ queryKey: ["tokens"] });
  };

  return (
    <SettingsCard
      title="API tokens"
      description="For scripts, scanners, Home Assistant, AI assistants (MCP at /mcp, read-only) and other apps. Use as: Authorization: Bearer <token>"
      actions={
        <Button size="sm" variant="primary" onClick={() => { setOpen(true); setSecret(null); setName(""); }}>
          <Plus /> New token
        </Button>
      }
    >
      {(tokens.data ?? []).length === 0 ? (
        <EmptyState icon={<KeyRound />} title="No tokens" className="py-8" />
      ) : (
        <ul className="divide-y divide-border">
          {tokens.data!.map((t) => (
            <li key={t.id} className="flex flex-wrap items-center gap-3 py-3">
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium">
                  {t.name} <code className="ml-1 text-xs text-subtle">{t.prefix}…</code>
                </div>
                <div className="text-xs text-subtle">
                  {t.scopes.map((s) => scopeLabels[s] ?? s).join(", ")} · {t.last_used_at ? `used ${timeAgo(t.last_used_at)}` : "never used"}
                  {t.expires_at && ` · expires ${formatDateTime(t.expires_at)}`}
                </div>
              </div>
              <Button size="sm" variant="danger-ghost" onClick={() => revoke(t)}>
                Revoke
              </Button>
            </li>
          ))}
        </ul>
      )}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent title="New API token">
          {secret ? (
            <>
              <SecretReveal label="Copy your new token now" value={secret} />
              <DialogFooter>
                <Button variant="primary" onClick={() => setOpen(false)}>
                  Done
                </Button>
              </DialogFooter>
            </>
          ) : (
            <form onSubmit={create} className="space-y-4">
              <Field label="Name" htmlFor="t-name" hint="Something to recognise it by, e.g. “Scanner” or “Home Assistant”.">
                <Input id="t-name" required value={name} onChange={(e) => setName(e.target.value)} autoFocus />
              </Field>
              <div>
                <div className="mb-2 text-[13px] font-medium">Permissions</div>
                <div className="space-y-1.5">
                  {Object.entries(scopeLabels)
                    .filter(([s]) => s !== "admin" || me.is_admin)
                    .map(([s, l]) => (
                      <label key={s} className="flex items-center gap-2 text-sm">
                        <Checkbox checked={scopes.includes(s)} onCheckedChange={(v) => setScopes(v ? [...scopes, s] : scopes.filter((x) => x !== s))} />
                        {l}
                      </label>
                    ))}
                </div>
              </div>
              <Field label="Expires" htmlFor="t-exp">
                <NativeSelect id="t-exp" value={days} onChange={(e) => setDays(e.target.value)}>
                  <option value="30">In 30 days</option>
                  <option value="90">In 90 days</option>
                  <option value="365">In 1 year</option>
                  <option value="">Never</option>
                </NativeSelect>
              </Field>
              <DialogFooter>
                <Button onClick={() => setOpen(false)}>Cancel</Button>
                <Button type="submit" variant="primary" loading={busy} disabled={!scopes.length}>
                  Create token
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </SettingsCard>
  );
}
