import { Link } from "@tanstack/react-router";
import { AlertCircle, ArrowRight, Bookmark, CalendarPlus, Files, Inbox, Loader2, Settings, Tags, Upload, User } from "lucide-react";
import { useDocuments, useSavedViews, useStats, useTaxonomy } from "@/lib/queries";
import { cn, formatBytes, formatDocDate, spaceDot, spaceLabel } from "@/lib/utils";
import { useCurrentUser, useFilePicker } from "@/components/app-shell";
import { Thumbnail, StatusBadge } from "@/components/documents/doc-items";
import { Button } from "@/components/ui/button";
import { Card, EmptyState, Skeleton, TagChip } from "@/components/ui/misc";

function greeting() {
  const h = new Date().getHours();
  return h < 12 ? "Good morning" : h < 17 ? "Good afternoon" : "Good evening";
}

export function HomePage() {
  const me = useCurrentUser();
  const stats = useStats();
  const recent = useDocuments({}, { refetchWhileProcessing: true });
  const pick = useFilePicker();
  const docs = recent.data?.pages[0]?.items.slice(0, 8) ?? [];
  const s = stats.data;

  return (
    <div className="mx-auto max-w-6xl page-x pb-12 pt-6 sm:pt-8">
      <h1 className="text-2xl font-semibold tracking-tight">
        {greeting()}, {me.display_name.split(" ")[0]}
      </h1>
      <p className="mt-1 text-sm text-muted">{s && !s.inbox ? "You're all caught up. Nothing waiting for review." : "Here's what's new in your documents."}</p>

      {!!s?.inbox && (
        <Link
          to="/inbox"
          className="group mt-6 flex items-center gap-4 rounded-xl border border-accent/25 bg-accent-soft p-4 transition-colors hover:border-accent/50 sm:p-5"
        >
          <span className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-accent text-accent-fg">
            <Inbox className="size-5" />
          </span>
          <span className="min-w-0 flex-1">
            <span className="block font-semibold text-accent-soft-fg">
              {s.inbox} new document{s.inbox === 1 ? "" : "s"} to review
            </span>
            <span className="block text-sm text-accent-soft-fg/80">Check the date, sender and tags Docveta filled in, then mark them reviewed.</span>
          </span>
          <span className="hidden items-center gap-1 text-sm font-medium text-accent-soft-fg sm:flex">
            Start reviewing <ArrowRight className="size-4 transition-transform group-hover:translate-x-0.5" />
          </span>
        </Link>
      )}

      <div className="mt-6 grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatCard to="/documents" icon={<Files />} label="Documents" value={s?.total} hint={s ? formatBytes(s.bytes) : undefined} />
        <StatCard to="/documents" search={{ status: "processing" }} icon={<Loader2 className={s?.processing ? "animate-spin" : ""} />} label="Processing" value={s?.processing} />
        <StatCard to="/documents" search={{ status: "failed,needs_password" }} icon={<AlertCircle />} label="Need attention" value={s?.failed} tone={s?.failed ? "warning" : undefined} />
        <StatCard to="/documents" icon={<CalendarPlus />} label="Added this week" value={s?.added_this_week} />
      </div>

      <div className="mt-10 grid gap-8 lg:grid-cols-[minmax(0,1fr)_300px]">
        <section aria-labelledby="recent-h">
          <SectionHeader id="recent-h" title="Recently added" link={{ to: "/documents", label: "View all" }} />
          {recent.isLoading ? (
            <Card className="divide-y divide-border">
              {Array.from({ length: 5 }).map((_, i) => (
                <div key={i} className="flex items-center gap-3 p-3">
                  <Skeleton className="h-12 w-10" />
                  <div className="flex-1 space-y-2">
                    <Skeleton className="h-3.5 w-1/2" />
                    <Skeleton className="h-3 w-1/3" />
                  </div>
                </div>
              ))}
            </Card>
          ) : docs.length === 0 ? (
            <Card>
              <EmptyState
                icon={<Upload />}
                title="Add your first document"
                action={
                  <Button variant="primary" onClick={() => pick()}>
                    <Upload /> Upload documents
                  </Button>
                }
              >
                Bills, IDs, certificates, contracts — drop them anywhere or use your phone's camera. Docveta reads the text and helps organise them.
              </EmptyState>
            </Card>
          ) : (
            <Card className="divide-y divide-border overflow-hidden">
              {docs.map((d) => (
                <Link key={d.id} to="/documents/$id" params={{ id: d.id }} className="flex items-center gap-3 px-3 py-2.5 transition-colors hover:bg-surface-2 sm:px-4">
                  <Thumbnail doc={d} className="h-12 w-10 shrink-0 rounded border border-border" />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-sm font-medium">{d.title}</span>
                      {d.inbox && d.status === "ready" && <span className="shrink-0 rounded bg-accent-soft px-1.5 text-[11px] font-medium text-accent-soft-fg">New</span>}
                      <StatusBadge doc={d} className="shrink-0 shadow-none" />
                    </div>
                    <div className="mt-0.5 truncate text-xs text-muted">
                      {[d.correspondent?.name, formatDocDate(d.document_date), spaceLabel(me.spaces.find((x) => x.id === d.space.id) ?? d.space)].filter(Boolean).join(" · ")}
                    </div>
                  </div>
                  <div className="hidden max-w-48 flex-wrap justify-end gap-1 sm:flex">
                    {d.tags.slice(0, 2).map((t) => (
                      <TagChip key={t.id} name={t.name} color={t.color} />
                    ))}
                  </div>
                </Link>
              ))}
            </Card>
          )}
        </section>

        <aside className="space-y-8">
          <section aria-labelledby="spaces-h">
            <SectionHeader id="spaces-h" title="Spaces" />
            <Card className="divide-y divide-border overflow-hidden">
              {me.spaces.map((sp) => (
                <div key={sp.id} className="flex items-center">
                  <Link to="/documents" search={{ space_id: [sp.id] }} className="flex min-w-0 flex-1 items-center gap-3 px-4 py-3 transition-colors hover:bg-surface-2">
                    <span className={cn("flex size-8 shrink-0 items-center justify-center rounded-lg text-sm font-bold text-white", sp.kind === "personal" ? "bg-slate-500" : spaceDot(sp.color))}>
                      {sp.kind === "personal" ? <User className="size-4" /> : sp.name[0]?.toUpperCase()}
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm font-medium">{spaceLabel(sp)}</span>
                      <span className="block text-xs text-muted">
                        {sp.document_count} document{sp.document_count === 1 ? "" : "s"}
                        {sp.kind === "shared" && ` · ${sp.member_count} member${sp.member_count === 1 ? "" : "s"}`}
                      </span>
                    </span>
                  </Link>
                  {sp.role === "owner" && (
                    <Link to="/spaces/$id/$section" params={{ id: sp.id, section: "general" }} className="mr-2 rounded-md p-2 text-subtle hover:bg-surface-2 hover:text-fg" aria-label={`${spaceLabel(sp)} settings`}>
                      <Settings className="size-4" />
                    </Link>
                  )}
                </div>
              ))}
            </Card>
          </section>
          <SavedViewsCard />
          <TopTags />
        </aside>
      </div>
    </div>
  );
}

function SectionHeader({ id, title, link }: { id: string; title: string; link?: { to: string; label: string } }) {
  return (
    <div className="mb-3 flex items-center justify-between">
      <h2 id={id} className="text-[15px] font-semibold">
        {title}
      </h2>
      {link && (
        <Link to={link.to} className="flex items-center gap-1 text-sm text-accent hover:underline">
          {link.label} <ArrowRight className="size-3.5" />
        </Link>
      )}
    </div>
  );
}

function SavedViewsCard() {
  const views = useSavedViews();
  if (!views.data?.length) return null;
  return (
    <section aria-labelledby="views-h">
      <SectionHeader id="views-h" title="Saved views" />
      <Card className="divide-y divide-border overflow-hidden">
        {views.data.slice(0, 6).map((v) => (
          <Link key={v.id} to="/views/$id" params={{ id: v.id }} className="flex items-center gap-3 px-4 py-2.5 text-sm transition-colors hover:bg-surface-2">
            <Bookmark className="size-4 text-subtle" />
            <span className="truncate">{v.name}</span>
          </Link>
        ))}
      </Card>
    </section>
  );
}

function TopTags() {
  const tags = useTaxonomy("tags");
  // The same tag name can exist in several spaces; show it once. `tag:` search matches all of them.
  const byName = new Map<string, { name: string; color?: string; count: number }>();
  for (const t of tags.data ?? []) {
    const k = t.name.toLowerCase();
    const cur = byName.get(k);
    if (cur) cur.count += t.document_count;
    else byName.set(k, { name: t.name, color: t.color, count: t.document_count });
  }
  const top = [...byName.values()].filter((t) => t.count > 0).sort((a, b) => b.count - a.count).slice(0, 12);
  if (!top.length) return null;
  return (
    <section aria-labelledby="tags-h">
      <SectionHeader id="tags-h" title="Browse by tag" />
      <div className="flex flex-wrap gap-1.5">
        {top.map((t) => (
          <Link key={t.name} to="/documents" search={{ q: /\s/.test(t.name) ? `tag:"${t.name}"` : `tag:${t.name}` }} className="rounded-md transition-opacity hover:opacity-80">
            <TagChip name={`${t.name} · ${t.count}`} color={t.color} className="px-2 py-1" />
          </Link>
        ))}
        <Link to="/documents" search={{ untagged: true }} className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs text-muted hover:text-fg">
          <Tags className="size-3.5" /> Untagged
        </Link>
      </div>
    </section>
  );
}

function StatCard({ to, search, icon, label, value, hint, tone }: { to: string; search?: Record<string, string>; icon: React.ReactNode; label: string; value?: number; hint?: string; tone?: "accent" | "warning" }) {
  return (
    <Link to={to} search={search} className="group flex items-center gap-3 rounded-xl border border-border bg-surface p-3.5 shadow-sm transition-colors hover:border-border-strong sm:p-4">
      <div
        className={cn(
          "flex size-9 shrink-0 items-center justify-center rounded-lg [&_svg]:size-[18px]",
          tone === "accent" ? "bg-accent-soft text-accent-soft-fg" : tone === "warning" ? "bg-warning-soft text-warning" : "bg-surface-2 text-muted",
        )}
      >
        {icon}
      </div>
      <div className="min-w-0">
        <div className="text-xl font-semibold leading-tight tabular-nums">
          {value?.toLocaleString() ?? "–"}
          {hint && <span className="ml-1.5 hidden text-xs font-normal text-subtle sm:inline">{hint}</span>}
        </div>
        <div className="text-[13px] leading-snug text-muted">{label}</div>
      </div>
    </Link>
  );
}
