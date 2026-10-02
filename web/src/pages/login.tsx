import * as React from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { KeyRound } from "lucide-react";
import { api, ApiError, errorMessage } from "@/lib/api";
import { keys, useMe, useStatus } from "@/lib/queries";
import { Button } from "@/components/ui/button";
import { Field, Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/misc";
import { AuthFrame } from "./setup";

export function LoginPage() {
  const search = useSearch({ from: "/login" });
  const status = useStatus();
  const me = useMe();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [email, setEmail] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [error, setError] = React.useState(search.error ?? "");
  const [busy, setBusy] = React.useState(false);

  React.useEffect(() => {
    if (status.data?.setup_needed) navigate({ to: "/setup" });
  }, [status.data, navigate]);

  const redirect = search.redirect && search.redirect.startsWith("/") && !search.redirect.startsWith("//") ? search.redirect : "/";

  React.useEffect(() => {
    if (me.data) navigate({ to: redirect, replace: true }); // already signed in
  }, [me.data, navigate, redirect]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.post("/auth/login", { email, password });
      await qc.invalidateQueries({ queryKey: keys.me });
      navigate({ to: redirect });
    } catch (err) {
      setError(err instanceof ApiError && err.status === 401 ? "Email or password is incorrect" : errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  if (status.isLoading) {
    return (
      <div className="flex h-dvh items-center justify-center">
        <Spinner />
      </div>
    );
  }
  const s = status.data;
  return (
    <AuthFrame title="Welcome back" subtitle="Sign in to your documents">
      {error && (
        <div className="mb-4 rounded-md bg-danger-soft px-3 py-2.5 text-sm text-danger" role="alert">
          {error}
        </div>
      )}
      {s?.oidc.enabled && (
        <>
          <Button asChild size="lg" variant={s.password_login ? "secondary" : "primary"} className="w-full">
            <a href={`/api/v1/auth/oidc/start?return_to=${encodeURIComponent(redirect)}`}>
              <KeyRound /> {s.oidc.button_label || "Sign in with SSO"}
            </a>
          </Button>
          {s.password_login && (
            <div className="my-5 flex items-center gap-3 text-xs text-subtle">
              <div className="h-px flex-1 bg-border" /> or <div className="h-px flex-1 bg-border" />
            </div>
          )}
        </>
      )}
      {s?.password_login !== false && (
        <form onSubmit={submit} className="space-y-4">
          <Field label="Email" htmlFor="email">
            <Input id="email" type="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} autoFocus />
          </Field>
          <Field label="Password" htmlFor="password">
            <Input id="password" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
          <Button type="submit" variant="primary" size="lg" className="w-full" loading={busy}>
            Sign in
          </Button>
          <p className="text-center text-xs text-subtle">Forgot your password? Ask your Docveta administrator to reset it.</p>
        </form>
      )}
    </AuthFrame>
  );
}
