import * as React from "react";
import { useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowRight, Building2, Check, Home, User } from "lucide-react";
import { api, ApiError, errorMessage } from "@/lib/api";
import { keys, useStatus } from "@/lib/queries";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Field, Input } from "@/components/ui/input";
import { LogoMark } from "@/components/app-shell";

export function AuthFrame({ title, subtitle, children }: { title: string; subtitle?: string; children: React.ReactNode }) {
  return (
    <div className="flex min-h-dvh flex-col items-center justify-center bg-bg bg-[radial-gradient(60rem_30rem_at_50%_-10%,var(--accent-soft),transparent)] px-4 py-10">
      <div className="mb-8 flex items-center gap-2.5">
        <LogoMark className="size-9" />
        <span className="text-xl font-semibold tracking-tight">Docveta</span>
      </div>
      <div className="w-full max-w-sm rounded-2xl border border-border bg-surface p-6 shadow-lg sm:p-8">
        <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
        {subtitle && <p className="mt-1 text-sm text-muted">{subtitle}</p>}
        <div className="mt-6">{children}</div>
      </div>
    </div>
  );
}

const presets = [
  { id: "home", icon: Home, label: "My household", space: "Family" },
  { id: "org", icon: Building2, label: "An organisation or team", space: "Company" },
  { id: "solo", icon: User, label: "Just me", space: "" },
] as const;

/** First-run wizard: create the administrator and (optionally) a shared space. */
export function SetupPage() {
  const status = useStatus();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [step, setStep] = React.useState(0);
  const [preset, setPreset] = React.useState<(typeof presets)[number]["id"]>("home");
  const [space, setSpace] = React.useState("Family");
  const [name, setName] = React.useState("");
  const [email, setEmail] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [err, setErr] = React.useState<ApiError | null>(null);
  const [busy, setBusy] = React.useState(false);

  React.useEffect(() => {
    if (status.data && !status.data.setup_needed) navigate({ to: "/login" });
  }, [status.data, navigate]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      await api.post("/setup", { email, display_name: name, password, shared_space_name: space });
      await qc.invalidateQueries({ queryKey: keys.status });
      await qc.invalidateQueries({ queryKey: keys.me });
      navigate({ to: "/" });
    } catch (e) {
      setErr(e instanceof ApiError ? e : new ApiError(0, "error", errorMessage(e)));
    } finally {
      setBusy(false);
    }
  };

  if (step === 0) {
    return (
      <AuthFrame title="Welcome to Docveta" subtitle="Let's set things up. It takes a minute.">
        <p className="mb-3 text-sm font-medium">Who will use Docveta?</p>
        <div className="space-y-2">
          {presets.map((p) => (
            <button
              key={p.id}
              type="button"
              onClick={() => {
                setPreset(p.id);
                setSpace(p.space);
              }}
              className={cn(
                "flex w-full items-center gap-3 rounded-lg border px-3.5 py-3 text-left text-sm transition-colors",
                preset === p.id ? "border-accent bg-accent-soft" : "border-border hover:bg-surface-2",
              )}
            >
              <p.icon className="size-5 text-muted" />
              <span className="flex-1 font-medium">{p.label}</span>
              {preset === p.id && <Check className="size-4 text-accent" />}
            </button>
          ))}
        </div>
        {preset !== "solo" && (
          <Field
            className="mt-5"
            label="Name of your shared space"
            htmlFor="space"
            hint="Everyone you invite can see documents in this space. Everyone also gets a private personal space."
          >
            <Input id="space" value={space} onChange={(e) => setSpace(e.target.value)} />
          </Field>
        )}
        <Button variant="primary" size="lg" className="mt-6 w-full" onClick={() => setStep(1)}>
          Continue <ArrowRight />
        </Button>
      </AuthFrame>
    );
  }

  return (
    <AuthFrame title="Create your admin account" subtitle="You'll manage users and settings with this account.">
      <form onSubmit={submit} className="space-y-4">
        {err && !err.fields.length && (
          <div className="rounded-md bg-danger-soft px-3 py-2.5 text-sm text-danger" role="alert">
            {err.message}
          </div>
        )}
        <Field label="Your name" htmlFor="name" error={err?.fieldError("display_name")}>
          <Input id="name" required autoComplete="name" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
        </Field>
        <Field label="Email" htmlFor="email" error={err?.fieldError("email")}>
          <Input id="email" type="email" required autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)} />
        </Field>
        <Field label="Password" htmlFor="pw" hint="At least 10 characters. A short sentence works well." error={err?.fieldError("password")}>
          <Input id="pw" type="password" required minLength={10} autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        <div className="flex gap-2 pt-2">
          <Button size="lg" onClick={() => setStep(0)}>
            Back
          </Button>
          <Button type="submit" variant="primary" size="lg" className="flex-1" loading={busy}>
            Create account
          </Button>
        </div>
      </form>
    </AuthFrame>
  );
}
