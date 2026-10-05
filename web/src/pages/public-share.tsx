import * as React from "react";
import { useParams } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, FileQuestion, Lock } from "lucide-react";
import { api, ApiError, errorMessage } from "@/lib/api";
import type { PublicDoc, PublicShare } from "@/lib/types";
import { browserImage, cn, fileKind, formatBytes, formatDocDate } from "@/lib/utils";
import { LogoMark } from "@/components/app-shell";
import { Button } from "@/components/ui/button";
import { Field, Input } from "@/components/ui/input";
import { EmptyState, Spinner, useScrollEdges } from "@/components/ui/misc";
import { ImageViewer } from "@/components/documents/image-viewer";

const PdfViewer = React.lazy(() => import("@/components/documents/pdf-viewer"));

/** What someone sees when they open a shared link: no account, no navigation, just the document. */
export function PublicSharePage() {
  const { token } = useParams({ strict: false }) as { token: string };
  const qc = useQueryClient();
  const share = useQuery({ queryKey: ["public-share", token], queryFn: () => api.get<PublicShare>(`/public/shares/${token}`), retry: false });
  const [active, setActive] = React.useState<string | null>(null);
  const nav = useScrollEdges<HTMLElement>();

  React.useEffect(() => {
    document.title = share.data?.title ? `${share.data.title}: shared document` : "Shared document";
  }, [share.data?.title]);
  const docs = share.data?.documents ?? [];
  const doc = docs.find((d) => d.id === active) ?? docs[0];

  return (
    <div className="flex h-dvh flex-col bg-bg">
      <header className="flex h-14 shrink-0 items-center gap-3 border-b border-border bg-surface px-4">
        <LogoMark />
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-[15px] font-semibold leading-tight">{share.data?.title ?? "Shared document"}</h1>
          <p className="truncate text-xs text-subtle">Shared with you through Docveta{share.data?.expires_at ? ` · link works until ${formatDocDate(share.data.expires_at.slice(0, 10))}` : ""}</p>
        </div>
        {doc && share.data?.allow_download && (
          <Button size="sm" asChild>
            <a href={`/api/v1/public/shares/${token}/documents/${doc.id}/file?kind=original&download=1`} aria-label="Download" title="Download">
              <Download /> <span className="hidden sm:inline">Download</span>
            </a>
          </Button>
        )}
      </header>
      {share.isLoading ? (
        <div className="flex flex-1 items-center justify-center">
          <Spinner />
        </div>
      ) : share.isError ? (
        <EmptyState icon={<FileQuestion />} title="This link doesn't work" className="flex-1">
          {share.error instanceof ApiError && share.error.status === 429 ? "Too many attempts. Try again in a few minutes." : "It may have expired or been turned off by the person who shared it."}
        </EmptyState>
      ) : share.data!.requires_password && docs.length === 0 ? (
        <PasswordGate token={token} onUnlocked={() => qc.invalidateQueries({ queryKey: ["public-share", token] })} />
      ) : (
        <div className="flex min-h-0 flex-1 flex-col md:flex-row">
          {docs.length > 1 && (
            <nav ref={nav} aria-label="Documents" className="scroll-x flex shrink-0 gap-1 border-b border-border bg-surface p-2 md:w-64 md:flex-col md:overflow-y-auto md:border-b-0 md:border-r">
              {docs.map((d) => (
                <button key={d.id} onClick={() => setActive(d.id)} className={cn("min-w-40 rounded-md px-3 py-2 text-left text-sm hover:bg-surface-2 md:min-w-0", d.id === doc?.id && "bg-surface-2 font-medium")}>
                  <span className="line-clamp-2">{d.title}</span>
                  <span className="text-xs text-subtle">{formatBytes(d.size_bytes)}</span>
                </button>
              ))}
            </nav>
          )}
          <main className="min-h-0 min-w-0 flex-1">{doc && <SharedViewer token={token} doc={doc} />}</main>
        </div>
      )}
    </div>
  );
}

function PasswordGate({ token, onUnlocked }: { token: string; onUnlocked: () => void }) {
  const [pw, setPw] = React.useState("");
  const [err, setErr] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr("");
    try {
      await api.post(`/public/shares/${token}/unlock`, { password: pw });
      onUnlocked();
    } catch (e) {
      setErr(e instanceof ApiError && e.status === 429 ? "Too many attempts. Try again in a few minutes." : e instanceof ApiError && e.status < 500 ? "That password isn't right." : errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="flex flex-1 items-center justify-center p-4">
      <form onSubmit={submit} className="w-full max-w-sm space-y-4 rounded-xl border border-border bg-surface p-6 shadow-sm">
        <div className="flex size-10 items-center justify-center rounded-full bg-accent-soft text-accent">
          <Lock className="size-5" />
        </div>
        <div>
          <h2 className="text-lg font-semibold">Password needed</h2>
          <p className="text-sm text-muted">The person who shared this asked for a password.</p>
        </div>
        <Field label="Password" htmlFor="sh-pw" error={err}>
          <Input id="sh-pw" type="password" autoComplete="off" value={pw} onChange={(e) => setPw(e.target.value)} autoFocus required />
        </Field>
        <Button type="submit" variant="primary" className="w-full" loading={busy}>
          Open
        </Button>
      </form>
    </div>
  );
}

function SharedViewer({ token, doc }: { token: string; doc: PublicDoc }) {
  const file = (kind: string) => `/api/v1/public/shares/${token}/documents/${doc.id}/file?kind=${kind}`;
  const kind = fileKind(doc.mime_type);
  // A converted copy first (Office → PDF, HEIC → JPEG), then the searchable PDF of a scan, then
  // the file itself. Recognised pictures go to the PDF viewer: with downloads off, the link
  // serves their archive PDF rather than the original picture.
  let view: React.ReactNode = null;
  if (doc.has_derived) {
    view = kind === "image" ? <ImageViewer src={file("derived")} alt={doc.title} /> : <PdfViewer url={file("derived")} />;
  } else if (kind === "pdf" || (kind === "image" && doc.has_archive)) {
    view = <PdfViewer url={file(doc.has_archive ? "archive" : "best")} />;
  } else if (kind === "image" && browserImage(doc.mime_type)) {
    view = <ImageViewer src={file("best")} alt={doc.title} />;
  }
  if (!view)
    return (
      <EmptyState icon={<FileQuestion />} title="No preview for this file" className="h-full">
        Use Download to open it on your device.
      </EmptyState>
    );
  return <React.Suspense fallback={<div className="flex h-full items-center justify-center"><Spinner /></div>}>{view}</React.Suspense>;
}
