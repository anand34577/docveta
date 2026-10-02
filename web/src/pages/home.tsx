import { Link } from "@tanstack/react-router";
import { AlertCircle, ArrowRight, FileText, Inbox, Loader2, Upload } from "lucide-react";
import { useDocuments, useStats } from "@/lib/queries";
import { formatBytes } from "@/lib/utils";
import { useCurrentUser, useFilePicker } from "@/components/app-shell";
import { Thumbnail, StatusBadge } from "@/components/documents/doc-items";
import { Button } from "@/components/ui/button";
import { Card, EmptyState, Skeleton } from "@/components/ui/misc";

function greeting() {
  const h = new Date().getHours();
  return h < 12 ? "Good morning" : h < 17 ? "Good afternoon" : "Good evening";
}

export function HomePage() {
  const me = useCurrentUser();
  const stats = useStats();
  const recent = useDocuments({}, { refetchWhileProcessing: true });
  const pick = useFilePicker();
  const docs = recent.data?.pages[0]?.items.slice(0, 12) ?? [];
  const s = stats.data;

  return (
    <div className="mx-auto max-w-6xl px-4 pb-10 pt-6 sm:px-6 sm:pt-8">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">
            {greeting()}, {me.display_name.split(" ")[0]}
          </h1>
          <p className="mt-1 text-sm text-muted">
            {s ? `${s.total.toLocaleString()} documents · ${formatBytes(s.bytes)}` : " "}
          </p>
        </div>
        <Button variant="primary" onClick={() => pick()} className="sm:hidden">
          <Upload /> Upload
        </Button>
      </div>

      <div className="mt-6 grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatCard to="/inbox" icon={<Inbox />} label="To review" value={s?.inbox} highlight={!!s?.inbox} />
        <StatCard to="/documents" search={{ status: "processing" }} icon={<Loader2 className={s?.processing ? "animate-spin" : ""} />} label="Processing" value={s?.processing} />
        <StatCard to="/documents" search={{ status: "failed,needs_password" }} icon={<AlertCircle />} label="Need attention" value={s?.failed} warn={!!s?.failed} />
        <StatCard to="/documents" icon={<FileText />} label="Added this week" value={s?.added_this_week} />
      </div>

      <div className="mt-10 flex items-center justify-between">
        <h2 className="text-base font-semibold">Recently added</h2>
        <Link to="/documents" className="flex items-center gap-1 text-sm text-accent hover:underline">
          View all <ArrowRight className="size-3.5" />
        </Link>
      </div>
      {recent.isLoading ? (
        <div className="mt-4 grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-6">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="aspect-[3/4] rounded-xl" />
          ))}
        </div>
      ) : docs.length === 0 ? (
        <Card className="mt-4">
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
        <div className="mt-4 grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-6">
          {docs.map((d) => (
            <Link key={d.id} to="/documents/$id" params={{ id: d.id }} className="group relative overflow-hidden rounded-xl border border-border bg-surface shadow-sm transition-all hover:-translate-y-0.5 hover:shadow-md">
              <Thumbnail doc={d} className="aspect-[4/4.4]" />
              <div className="border-t border-border p-2.5">
                <div className="line-clamp-2 text-[13px] font-medium leading-snug">{d.title}</div>
              </div>
              <StatusBadge doc={d} className="absolute right-2 top-2" />
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}

function StatCard({ to, search, icon, label, value, highlight, warn }: { to: string; search?: Record<string, string>; icon: React.ReactNode; label: string; value?: number; highlight?: boolean; warn?: boolean }) {
  return (
    <Link
      to={to}
      search={search}
      className="group rounded-xl border border-border bg-surface p-4 shadow-sm transition-colors hover:border-border-strong"
    >
      <div className={`flex size-9 items-center justify-center rounded-lg [&_svg]:size-[18px] ${highlight ? "bg-accent text-accent-fg" : warn ? "bg-warning-soft text-warning" : "bg-surface-2 text-muted"}`}>
        {icon}
      </div>
      <div className="mt-3 text-2xl font-semibold tabular-nums">{value ?? "–"}</div>
      <div className="text-[13px] text-muted">{label}</div>
    </Link>
  );
}
