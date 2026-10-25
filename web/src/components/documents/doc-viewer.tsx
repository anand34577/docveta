import * as React from "react";
import { Download, FileQuestion } from "lucide-react";
import type { Document } from "@/lib/types";
import { browserImage, fileKind } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { EmptyState, Spinner } from "@/components/ui/misc";
import { ImageViewer } from "./image-viewer";
import { useLocate } from "./pdf-find";

const PdfViewer = React.lazy(() => import("./pdf-viewer"));

/** Shows a document: PDF (archive copy preferred), image, or text. */
export function DocViewer({ doc, page, highlight }: { doc: Document; page?: number; highlight?: string[] }) {
  const kind = fileKind(doc.mime_type);
  const v = doc.version;
  const located = useLocate();
  const locate = located.docId === doc.id ? located.target : null;
  // A scanned picture shows text selected in the Text tab through its searchable PDF copy.
  // Kept per document: the Inbox shows the next one in the same viewer.
  const [archiveFor, setArchiveFor] = React.useState<string | null>(null);
  const viaArchive = archiveFor === doc.id;
  if (locate && !viaArchive && doc.has_archive && kind === "image") setArchiveFor(doc.id);
  if (doc.has_derived) {
    // A working copy exists: Office documents became a PDF, HEIC/AVIF pictures a JPEG.
    const url = `/api/v1/documents/${doc.id}/file?kind=derived&v=${v}`;
    if (kind === "image") return <ImageViewer src={url} alt={doc.title} />;
    return (
      <React.Suspense fallback={<div className="flex h-full items-center justify-center"><Spinner /></div>}>
        <PdfViewer url={url} initialPage={page} highlight={highlight} locate={locate} />
      </React.Suspense>
    );
  }
  if (kind === "pdf" || (doc.has_archive && kind === "image" && (viaArchive || !browserImage(doc.mime_type)))) {
    const fileKindParam = doc.has_archive ? "archive" : "original";
    return (
      <React.Suspense fallback={<div className="flex h-full items-center justify-center"><Spinner /></div>}>
        <PdfViewer url={`/api/v1/documents/${doc.id}/file?kind=${fileKindParam}&v=${v}`} initialPage={page} highlight={highlight} locate={locate} />
      </React.Suspense>
    );
  }
  if (kind === "image" && browserImage(doc.mime_type)) {
    return <ImageViewer src={`/api/v1/documents/${doc.id}/file?kind=original&v=${v}`} alt={doc.title} />;
  }
  if (kind === "text") return <TextViewer id={doc.id} version={v} />;
  return (
    <EmptyState
      icon={<FileQuestion />}
      title="No preview available"
      className="h-full"
      action={
        <Button asChild>
          <a href={`/api/v1/documents/${doc.id}/file?kind=original&download=1`}>
            <Download /> Download
          </a>
        </Button>
      }
    >
      {doc.has_archive ? "" : "This file type can't be shown in the browser."}
    </EmptyState>
  );
}

function TextViewer({ id, version }: { id: string; version: number }) {
  const [text, setText] = React.useState<string | null>(null);
  React.useEffect(() => {
    // Stepping quickly through documents (J / K) mustn't show an earlier one's text.
    const ctl = new AbortController();
    setText(null);
    fetch(`/api/v1/documents/${id}/file?kind=original&v=${version}`, { credentials: "same-origin", signal: ctl.signal })
      .then((r) => (r.ok ? r.text() : Promise.reject(new Error(String(r.status)))))
      .then(setText)
      .catch(() => !ctl.signal.aborted && setText("Couldn't load the file."));
    return () => ctl.abort();
  }, [id, version]);
  if (text === null) return <div className="flex h-full items-center justify-center"><Spinner /></div>;
  return (
    <div className="h-full overflow-auto scrollbar-thin bg-surface-3 p-4">
      <pre className="mx-auto max-w-3xl whitespace-pre-wrap rounded-md bg-surface p-6 font-mono text-[13px] leading-relaxed shadow-md">{text}</pre>
    </div>
  );
}
