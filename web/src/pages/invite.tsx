import * as React from "react";
import { useNavigate, useParams } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "@/lib/api";
import { keys } from "@/lib/queries";
import type { InvitePreview } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Field, Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/misc";
import { AuthFrame } from "./setup";

/** Where an invitation link lands: pick a name and password, and you're in. */
export function InvitePage() {
  const { token } = useParams({ strict: false }) as { token: string };
  const navigate = useNavigate();
  const qc = useQueryClient();
  const preview = useQuery({ queryKey: ["invite", token], queryFn: () => api.get<InvitePreview>(`/invites/${token}`), retry: false });
  const [name, setName] = React.useState("");
  const [email, setEmail] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [err, setErr] = React.useState<ApiError | null>(null);
  const [busy, setBusy] = React.useState(false);
  React.useEffect(() => {
    if (preview.data) {
      setName(preview.data.display_name);
      setEmail(preview.data.email ?? "");
    }
  }, [preview.data]);

  if (preview.isLoading)
    return (
      <div className="flex h-dvh items-center justify-center">
        <Spinner />
      </div>
    );
  if (preview.isError || !preview.data)
    return (
      <AuthFrame title="This invitation can't be used" subtitle="The link may have expired or already been used. Ask the person who invited you for a new one.">
        <Button variant="primary" size="lg" className="w-full" onClick={() => navigate({ to: "/login" })}>
          Go to sign in
        </Button>
      </AuthFrame>
    );

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      await api.post(`/invites/${token}/accept`, { display_name: name.trim(), email: preview.data!.email ? undefined : email.trim(), password });
      await qc.invalidateQueries({ queryKey: keys.me });
      navigate({ to: "/" });
    } catch (e) {
      setErr(e as ApiError);
    } finally {
      setBusy(false);
    }
  };
  return (
    <AuthFrame title="Join Docveta" subtitle={`${preview.data.invited_by} invited you. Choose a password to finish.`}>
      <form onSubmit={submit} className="space-y-4">
        {err && !err.fields.length && (
          <div className="rounded-md bg-danger-soft px-3 py-2.5 text-sm text-danger" role="alert">
            {err.message}
          </div>
        )}
        <Field label="Your name" htmlFor="i-name" error={err?.fieldError("display_name")}>
          <Input id="i-name" required autoComplete="name" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
        </Field>
        <Field label="Email" htmlFor="i-email" error={err?.fieldError("email")}>
          <Input id="i-email" type="email" required autoComplete="username" value={email} readOnly={!!preview.data.email} onChange={(e) => setEmail(e.target.value)} />
        </Field>
        <Field label="Password" htmlFor="i-pw" hint="At least 10 characters. A short sentence works well." error={err?.fieldError("password")}>
          <Input id="i-pw" type="password" required minLength={10} autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        <Button type="submit" variant="primary" size="lg" className="w-full" loading={busy}>
          Create my account
        </Button>
      </form>
    </AuthFrame>
  );
}
