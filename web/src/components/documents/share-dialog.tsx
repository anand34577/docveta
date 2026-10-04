import * as React from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Check, Copy, Link2, Lock } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { useShares } from "@/lib/queries";
import type { Share } from "@/lib/types";
import { formatDateTime, timeAgo } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Field, Input, NativeSelect } from "@/components/ui/input";
import { Badge, Checkbox, Skeleton } from "@/components/ui/misc";
import { Dialog, DialogContent } from "@/components/ui/overlay";
import { confirm } from "@/components/ui/confirm";

/** Create and manage private links that let anyone with the link view one document, without an account. */
export function ShareDialog({ docId, title, onClose }: { docId: string; title: string; onClose: () => void }) {
  const qc = useQueryClient();
  const shares = useShares(docId);
  const [days, setDays] = React.useState("7");
  const [password, setPassword] = React.useState("");
  const [download, setDownload] = React.useState(true);
  const [busy, setBusy] = React.useState(false);
  const [fresh, setFresh] = React.useState<string | null>(null);

  const create = async () => {
    setBusy(true);
    try {
      const r = await api.post<{ share: Share; link: string }>("/shares", {
        document_id: docId,
        expires_in_days: days ? Number(days) : undefined,
        password: password || undefined,
        allow_download: download,
      });
      setFresh(r.link);
      setPassword("");
      qc.invalidateQueries({ queryKey: ["shares"] });
      void copy(r.link);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  const revoke = async (s: Share) => {
    if (!(await confirm({ title: "Stop sharing?", body: "The link will stop working immediately.", confirmLabel: "Stop sharing", destructive: true }))) return;
    await api.del(`/shares/${s.id}`).catch((e) => toast.error(errorMessage(e)));
    qc.invalidateQueries({ queryKey: ["shares"] });
  };
  const active = (shares.data ?? []).filter((s) => s.status === "active");

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title="Share with a link" description={`Anyone with the link can view “${title}”. They don't need an account.`}>
        <div className="space-y-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Link works for" htmlFor="sh-days">
              <NativeSelect id="sh-days" value={days} onChange={(e) => setDays(e.target.value)}>
                <option value="1">1 day</option>
                <option value="7">7 days</option>
                <option value="30">30 days</option>
                <option value="">Until I stop it</option>
              </NativeSelect>
            </Field>
            <Field label="Password (optional)" htmlFor="sh-pw">
              <Input id="sh-pw" type="text" autoComplete="off" value={password} placeholder="No password" onChange={(e) => setPassword(e.target.value)} />
            </Field>
          </div>
          <label className="flex items-center gap-2 text-sm">
            <Checkbox checked={download} onCheckedChange={(v) => setDownload(v === true)} /> They can download the file
          </label>
          <Button variant="primary" loading={busy} onClick={create}>
            <Link2 /> Create link
          </Button>
          {fresh && (
            <div className="rounded-lg border border-success/40 bg-success-soft p-3">
              <div className="flex items-center gap-2 text-[13px] font-medium text-success">
                <Check className="size-4" /> Link created and copied
              </div>
              <div className="mt-2 flex gap-2">
                <Input readOnly value={fresh} onFocus={(e) => e.currentTarget.select()} className="font-mono text-xs" />
                <Button onClick={() => void copy(fresh)} aria-label="Copy link">
                  <Copy />
                </Button>
              </div>
              <p className="mt-2 text-xs text-muted">Keep it private: anyone who has it can open the document until it expires.</p>
            </div>
          )}
          <div>
            <h3 className="mb-2 text-[13px] font-medium">Active links</h3>
            {shares.isLoading ? (
              <Skeleton className="h-10" />
            ) : active.length === 0 ? (
              <p className="text-sm text-muted">Nothing shared yet.</p>
            ) : (
              <ul className="divide-y divide-border rounded-lg border border-border">
                {active.map((s) => (
                  <li key={s.id} className="flex flex-wrap items-center gap-2 px-3 py-2.5 text-sm">
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-1.5">
                        {s.has_password && (
                          <Badge>
                            <Lock className="size-3" /> Password
                          </Badge>
                        )}
                        {!s.allow_download && <Badge>View only</Badge>}
                        <span className="text-muted">{s.expires_at ? `until ${formatDateTime(s.expires_at)}` : "no expiry"}</span>
                      </div>
                      <div className="text-xs text-subtle">
                        {s.access_count} view{s.access_count === 1 ? "" : "s"}
                        {s.last_access_at && ` · last ${timeAgo(s.last_access_at)}`} · by {s.created_by}
                      </div>
                    </div>
                    <Button size="sm" variant="danger-ghost" onClick={() => revoke(s)}>
                      Stop
                    </Button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    /* clipboard blocked: the field is selectable */
  }
}
