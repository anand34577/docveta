import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { useNotificationPrefs } from "@/lib/queries";
import type { NotificationPrefs } from "@/lib/types";
import { SettingsCard } from "@/components/settings-layout";
import { Input } from "@/components/ui/input";
import { SwitchRow } from "@/components/ui/misc";

/** Quiet hours: in-app notifications still arrive, but phone/email/webhook pushes wait until the end. */
export function QuietHours() {
  const prefs = useNotificationPrefs();
  const qc = useQueryClient();
  const save = useMutation({
    mutationFn: (p: NotificationPrefs) => api.put<NotificationPrefs>("/me/notification-prefs", p),
    onSuccess: (p) => {
      qc.setQueryData(["notification-prefs"], p);
      toast.success("Saved");
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  const p = prefs.data;
  if (!p) return null;
  const set = (patch: Partial<NotificationPrefs>) => save.mutate({ ...p, ...patch });
  return (
    <SettingsCard title="Quiet hours" description="Hold messages to your phone, email and webhooks overnight. They're sent when quiet hours end; the in-app bell is unaffected.">
      <SwitchRow label="Pause pushes during quiet hours" checked={p.quiet_enabled} onCheckedChange={(v) => set({ quiet_enabled: v })} />
      {p.quiet_enabled && (
        <div className="mt-2 flex flex-wrap items-center gap-3 text-sm">
          From
          <Input type="time" className="w-32" defaultValue={p.quiet_start} onBlur={(e) => e.target.value && e.target.value !== p.quiet_start && set({ quiet_start: e.target.value })} />
          until
          <Input type="time" className="w-32" defaultValue={p.quiet_end} onBlur={(e) => e.target.value && e.target.value !== p.quiet_end && set({ quiet_end: e.target.value })} />
          <span className="text-xs text-subtle">in your time zone</span>
        </div>
      )}
    </SettingsCard>
  );
}
