import * as React from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError, errorMessage } from "@/lib/api";
import { SettingsCard } from "@/components/settings-layout";
import { Button } from "@/components/ui/button";
import { Field, Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/misc";
import { Dialog, DialogContent, DialogFooter } from "@/components/ui/overlay";
import { useCurrentUser } from "@/components/app-shell";

type Status = { enabled: boolean; recovery_codes_left: number };
type Step = null | "setup" | "codes" | "disable" | "regen";

/** Two-factor sign-in with an authenticator app (TOTP), plus one-time recovery codes. */
export function TwoFactorCard() {
  const me = useCurrentUser();
  const qc = useQueryClient();
  const status = useQuery({ queryKey: ["2fa"], queryFn: () => api.get<Status>("/me/2fa") });
  const [step, setStep] = React.useState<Step>(null);
  const [codes, setCodes] = React.useState<string[]>([]);
  const on = status.data?.enabled;
  const done = (c?: string[]) => {
    qc.invalidateQueries({ queryKey: ["2fa"] });
    if (c) {
      setCodes(c);
      setStep("codes");
    } else setStep(null);
  };
  return (
    <SettingsCard
      title={
        <span className="flex items-center gap-2">
          Two-step sign-in {on && <Badge tone="success">On</Badge>}
        </span>
      }
      description="Ask for a 6-digit code from an authenticator app (Google Authenticator, Aegis, 1Password…) after your password."
      actions={
        on ? (
          <>
            <Button size="sm" onClick={() => setStep("regen")}>
              New recovery codes
            </Button>
            <Button size="sm" variant="danger-ghost" onClick={() => setStep("disable")}>
              Turn off
            </Button>
          </>
        ) : (
          me.has_password && (
            <Button size="sm" variant="primary" onClick={() => setStep("setup")}>
              <ShieldCheck /> Turn on
            </Button>
          )
        )
      }
    >
      <p className="text-sm text-muted">
        {on
          ? `You have ${status.data?.recovery_codes_left ?? 0} recovery code${status.data?.recovery_codes_left === 1 ? "" : "s"} left. Keep them somewhere safe: each works once if you lose your phone.`
          : me.has_password
            ? "Recommended if Docveta is reachable from the internet."
            : "Two-step sign-in applies to password sign-in. You sign in with single sign-on, which has its own protection."}
      </p>
      {step === "setup" && <Setup onClose={() => setStep(null)} onDone={done} />}
      {step === "codes" && <Codes codes={codes} onClose={() => setStep(null)} />}
      {step === "disable" && <Confirm kind="disable" onClose={() => setStep(null)} onDone={() => done()} />}
      {step === "regen" && <Confirm kind="regen" onClose={() => setStep(null)} onDone={done} />}
    </SettingsCard>
  );
}

function Setup({ onClose, onDone }: { onClose: () => void; onDone: (codes: string[]) => void }) {
  const [info, setInfo] = React.useState<{ secret: string; uri: string; qr: string } | null>(null);
  const [code, setCode] = React.useState("");
  const [err, setErr] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  React.useEffect(() => {
    api.post<{ secret: string; uri: string; qr: string }>("/me/2fa/setup").then(setInfo, (e) => setErr(errorMessage(e)));
  }, []);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr("");
    try {
      const r = await api.post<{ recovery_codes: string[] }>("/me/2fa/enable", { code: code.replace(/\s/g, "") });
      onDone(r.recovery_codes);
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title="Turn on two-step sign-in" description="Scan the code with your authenticator app, then enter the 6 digits it shows.">
        <form onSubmit={submit} className="space-y-4">
          <div className="flex flex-col items-center gap-3 rounded-lg border border-border p-4">
            {info?.qr ? <img src={info.qr} alt="QR code for your authenticator app" className="size-44 rounded bg-white p-1" /> : <div className="size-44 animate-pulse rounded bg-surface-3" />}
            {info && (
              <div className="text-center text-xs text-muted">
                Can't scan? Enter this key:
                <div className="mt-1 flex items-center justify-center gap-1.5">
                  <code className="select-all break-all font-mono text-[13px] text-fg">{info.secret}</code>
                  <button type="button" aria-label="Copy key" className="rounded p-1 hover:bg-surface-2" onClick={() => void navigator.clipboard.writeText(info.secret)}>
                    <Copy className="size-3.5" />
                  </button>
                </div>
              </div>
            )}
          </div>
          <Field label="6-digit code" htmlFor="tf-code" error={err}>
            <Input id="tf-code" inputMode="numeric" autoComplete="one-time-code" maxLength={7} placeholder="123 456" value={code} onChange={(e) => setCode(e.target.value)} autoFocus />
          </Field>
          <DialogFooter>
            <Button onClick={onClose}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy} disabled={!info || code.replace(/\s/g, "").length < 6}>
              Verify and turn on
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function Codes({ codes, onClose }: { codes: string[]; onClose: () => void }) {
  const text = codes.join("\n");
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title="Save your recovery codes" description="If you lose your phone, each of these signs you in once. You won't see them again.">
        <div className="grid grid-cols-2 gap-2 rounded-lg bg-surface-2 p-4 font-mono text-sm">
          {codes.map((c) => (
            <span key={c} className="select-all">
              {c}
            </span>
          ))}
        </div>
        <div className="mt-3 flex gap-2">
          <Button size="sm" onClick={() => void navigator.clipboard.writeText(text).then(() => toast.success("Copied"))}>
            <Copy /> Copy
          </Button>
          <Button size="sm" asChild>
            <a href={`data:text/plain;charset=utf-8,${encodeURIComponent(text + "\n")}`} download="docveta-recovery-codes.txt">
              Download
            </a>
          </Button>
        </div>
        <DialogFooter>
          <Button variant="primary" onClick={onClose}>
            I've saved them
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function Confirm({ kind, onClose, onDone }: { kind: "disable" | "regen"; onClose: () => void; onDone: (codes?: string[]) => void }) {
  const [password, setPassword] = React.useState("");
  const [code, setCode] = React.useState("");
  const [err, setErr] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr("");
    try {
      if (kind === "disable") {
        await api.post("/me/2fa/disable", { password, code: code.replace(/\s/g, "") });
        toast.success("Two-step sign-in is off");
        onDone();
      } else {
        const r = await api.post<{ recovery_codes: string[] }>("/me/2fa/recovery-codes", { code: code.replace(/\s/g, "") });
        onDone(r.recovery_codes);
      }
    } catch (e) {
      setErr(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={kind === "disable" ? "Turn off two-step sign-in" : "New recovery codes"} description="Confirm it's you. The old recovery codes stop working." size="sm">
        <form onSubmit={submit} className="space-y-4">
          {kind === "disable" && (
            <Field label="Password" htmlFor="tf-pw">
              <Input id="tf-pw" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
            </Field>
          )}
          <Field label="Code from your app (or a recovery code)" htmlFor="tf-c" error={err}>
            <Input id="tf-c" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} required autoFocus={kind === "regen"} />
          </Field>
          <DialogFooter>
            <Button onClick={onClose}>Cancel</Button>
            <Button type="submit" variant={kind === "disable" ? "danger" : "primary"} loading={busy}>
              {kind === "disable" ? "Turn off" : "Create new codes"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
