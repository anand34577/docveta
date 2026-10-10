import * as React from "react";
import { useNavigate } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Bell, CheckCheck, Settings } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { keys } from "@/lib/queries";
import type { Notification } from "@/lib/types";
import { cn, formatDateTime } from "@/lib/utils";
import { PageHeader } from "@/components/app-shell";
import { severityIcons } from "@/components/notifications";
import { Button } from "@/components/ui/button";
import { EmptyState, Skeleton } from "@/components/ui/misc";
import { Link } from "@tanstack/react-router";

/** Every notification, newest first (the bell shows only the latest few). */
export function NotificationsPage() {
  const [unreadOnly, setUnreadOnly] = React.useState(false);
  const qc = useQueryClient();
  const navigate = useNavigate();
  const list = useQuery({
    queryKey: ["notifications-all", unreadOnly],
    queryFn: () => api.get<{ items: Notification[]; unread: number }>("/notifications", { limit: 200, unread: unreadOnly }),
  });
  const refresh = () => {
    qc.invalidateQueries({ queryKey: keys.notifications });
    qc.invalidateQueries({ queryKey: ["notifications-all"] });
  };
  const markAll = async () => {
    await api.post("/notifications/read", { all: true }).then(refresh, (e) => toast.error(errorMessage(e)));
  };
  const open = async (n: Notification) => {
    // Marking it read mustn't stand in the way of opening it.
    if (!n.read_at) void api.post("/notifications/read", { ids: [n.id] }).then(refresh, () => undefined);
    if (n.link) navigate({ to: n.link });
  };
  const items = list.data?.items ?? [];
  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader
        title="Notifications"
        description={list.data ? (list.data.unread ? `${list.data.unread} unread` : "You're all caught up") : undefined}
        actions={
          <>
            <Button size="sm" variant="ghost" asChild>
              <Link to="/settings/$section" params={{ section: "notifications" }}>
                <Settings /> Preferences
              </Link>
            </Button>
            {!!list.data?.unread && (
              <Button size="sm" onClick={markAll}>
                <CheckCheck /> Mark all read
              </Button>
            )}
          </>
        }
      />
      <div className="page-x pb-10">
        <div className="mb-3 inline-flex rounded-lg bg-surface-2 p-0.5 text-sm" role="tablist">
          {[
            [false, "All"],
            [true, "Unread"],
          ].map(([v, l]) => (
            <button key={String(v)} role="tab" aria-selected={unreadOnly === v} onClick={() => setUnreadOnly(v as boolean)} className={cn("rounded-md px-3 py-1 font-medium text-muted", unreadOnly === v && "bg-surface text-fg shadow-sm")}>
              {l as string}
            </button>
          ))}
        </div>
        {list.isLoading ? (
          <Skeleton className="h-40" />
        ) : list.isError && !list.data ? (
          <EmptyState icon={<Bell />} title="Couldn't load notifications" action={<Button onClick={() => list.refetch()} loading={list.isFetching}>Try again</Button>}>
            {errorMessage(list.error)}
          </EmptyState>
        ) : items.length === 0 ? (
          <EmptyState icon={<Bell />} title={unreadOnly ? "Nothing unread" : "No notifications yet"} />
        ) : (
          <ul className="divide-y divide-border overflow-hidden rounded-xl border border-border bg-surface">
            {items.map((n) => (
              <li key={n.id}>
                <button onClick={() => open(n)} className={cn("flex w-full items-start gap-3 px-4 py-3 text-left hover:bg-surface-2", !n.read_at && "bg-accent-soft/40")}>
                  <span className="mt-0.5">{severityIcons[n.severity] ?? severityIcons.info}</span>
                  <span className="min-w-0 flex-1">
                    <span className="block text-sm font-medium leading-snug">{n.title}</span>
                    {n.body && <span className="mt-0.5 block text-[13px] text-muted">{n.body}</span>}
                    <span className="mt-1 block text-xs text-subtle">{formatDateTime(n.created_at)}</span>
                  </span>
                  {!n.read_at && <span className="mt-1.5 size-2 shrink-0 rounded-full bg-accent" />}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
