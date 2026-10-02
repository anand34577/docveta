import * as React from "react";
import { useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Bell, CheckCheck, CheckCircle2, Info, XCircle } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { invalidateDocuments, keys, useNotifications } from "@/lib/queries";
import { cn, timeAgo } from "@/lib/utils";
import type { Notification } from "@/lib/types";
import { Popover, PopoverContent, PopoverTrigger } from "./ui/overlay";
import { EmptyState } from "./ui/misc";

const icons = {
  info: <Info className="size-4 text-accent" />,
  success: <CheckCircle2 className="size-4 text-success" />,
  warning: <AlertTriangle className="size-4 text-warning" />,
  error: <XCircle className="size-4 text-danger" />,
};

/** Subscribes to server-sent events and refreshes data + shows toasts. */
export function useLiveEvents() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  React.useEffect(() => {
    let es: EventSource | null = null;
    let closed = false;
    let retry: ReturnType<typeof setTimeout>;
    const connect = () => {
      es = new EventSource("/api/v1/events");
      es.addEventListener("notification", (ev) => {
        const n = JSON.parse((ev as MessageEvent).data) as Notification;
        qc.invalidateQueries({ queryKey: keys.notifications });
        if (n.event_type.startsWith("document.")) invalidateDocuments(qc);
        const opts = {
          description: n.body || undefined,
          action: n.link ? { label: "Open", onClick: () => navigate({ to: n.link }) } : undefined,
        };
        if (n.severity === "error") toast.error(n.title, opts);
        else if (n.severity === "warning") toast.warning(n.title, opts);
        else if (n.severity === "success") toast.success(n.title, opts);
        else toast(n.title, opts);
      });
      es.onerror = () => {
        es?.close();
        if (!closed) retry = setTimeout(connect, 10_000);
      };
    };
    connect();
    return () => {
      closed = true;
      clearTimeout(retry);
      es?.close();
    };
  }, [qc, navigate]);
}

export function NotificationsButton() {
  const { data } = useNotifications();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [open, setOpen] = React.useState(false);
  const unread = data?.unread ?? 0;

  const markAll = async () => {
    await api.post("/notifications/read", { all: true });
    qc.invalidateQueries({ queryKey: keys.notifications });
  };
  const openItem = async (n: Notification) => {
    setOpen(false);
    if (!n.read_at) {
      api.post("/notifications/read", { ids: [n.id] }).then(() => qc.invalidateQueries({ queryKey: keys.notifications }));
    }
    if (n.link) navigate({ to: n.link });
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button className="relative flex size-9 items-center justify-center rounded-md text-muted hover:bg-surface-2 hover:text-fg" aria-label={`Notifications${unread ? ` (${unread} unread)` : ""}`}>
          <Bell className="size-[18px]" />
          {unread > 0 && <span className="absolute right-1.5 top-1.5 size-2 rounded-full bg-accent ring-2 ring-bg" />}
        </button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[min(24rem,calc(100vw-1rem))] p-0">
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <span className="text-sm font-semibold">Notifications</span>
          {unread > 0 && (
            <button onClick={markAll} className="flex items-center gap-1 text-xs text-accent hover:underline">
              <CheckCheck className="size-3.5" /> Mark all read
            </button>
          )}
        </div>
        <div className="max-h-[60vh] overflow-y-auto scrollbar-thin">
          {(data?.items ?? []).length === 0 ? (
            <EmptyState icon={<Bell />} title="You're all caught up" className="py-10" />
          ) : (
            data!.items.map((n) => (
              <button
                key={n.id}
                onClick={() => openItem(n)}
                className={cn("flex w-full items-start gap-3 border-b border-border px-4 py-3 text-left last:border-0 hover:bg-surface-2", !n.read_at && "bg-accent-soft/40")}
              >
                <span className="mt-0.5">{icons[n.severity] ?? icons.info}</span>
                <span className="min-w-0 flex-1">
                  <span className="block text-sm font-medium leading-snug">{n.title}</span>
                  {n.body && <span className="mt-0.5 line-clamp-2 block text-[13px] text-muted">{n.body}</span>}
                  <span className="mt-1 block text-xs text-subtle">{timeAgo(n.created_at)}</span>
                </span>
                {!n.read_at && <span className="mt-1.5 size-2 shrink-0 rounded-full bg-accent" />}
              </button>
            ))
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}
