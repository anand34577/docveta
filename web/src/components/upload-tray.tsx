import * as React from "react";
import { Link } from "@tanstack/react-router";
import { AlertCircle, CheckCircle2, ChevronDown, Copy, FileUp, RotateCw, X } from "lucide-react";
import { useUploads } from "@/stores/uploads";
import { useUI } from "@/stores/ui";
import { cn, formatBytes } from "@/lib/utils";
import { Button } from "./ui/button";
import { NativeSelect } from "./ui/input";
import { useCurrentUser } from "./app-shell";

/** Floating panel showing upload progress; collapses when everything is done. */
export function UploadTray() {
  const { items, retry, remove, clearFinished } = useUploads();
  const [collapsed, setCollapsed] = React.useState(false);
  if (items.length === 0) return null;
  const active = items.filter((i) => i.status === "queued" || i.status === "uploading").length;
  const problems = items.filter((i) => i.status === "error" || i.status === "duplicate").length;
  const title = active > 0 ? `Uploading ${active} file${active > 1 ? "s" : ""}…` : problems > 0 ? `${problems} need attention` : "Uploads complete";

  return (
    <div className="fixed bottom-20 right-3 z-40 w-[calc(100vw-1.5rem)] max-w-sm overflow-hidden rounded-xl border border-border bg-surface shadow-lg animate-pop lg:bottom-4 lg:right-4">
      <div className="flex items-center gap-2 border-b border-border px-3 py-2">
        <FileUp className="size-4 text-muted" />
        <span className="flex-1 text-sm font-medium">{title}</span>
        <button onClick={() => setCollapsed(!collapsed)} className="rounded p-1 text-muted hover:bg-surface-2" aria-label={collapsed ? "Expand" : "Collapse"}>
          <ChevronDown className={cn("size-4 transition-transform", collapsed && "rotate-180")} />
        </button>
        {active === 0 && (
          <button onClick={clearFinished} className="rounded p-1 text-muted hover:bg-surface-2" aria-label="Close">
            <X className="size-4" />
          </button>
        )}
      </div>
      {!collapsed && (
        <ul className="max-h-72 overflow-y-auto scrollbar-thin" aria-live="polite">
          {items.map((it) => (
            <li key={it.id} className="flex items-start gap-3 border-b border-border px-3 py-2.5 last:border-0">
              <div className="mt-0.5">
                {it.status === "done" ? (
                  <CheckCircle2 className="size-4 text-success" />
                ) : it.status === "error" ? (
                  <AlertCircle className="size-4 text-danger" />
                ) : it.status === "duplicate" ? (
                  <Copy className="size-4 text-warning" />
                ) : (
                  <svg className="size-4 -rotate-90" viewBox="0 0 20 20" aria-label={`${Math.round(it.progress * 100)}%`}>
                    <circle cx="10" cy="10" r="8" fill="none" strokeWidth="3" className="stroke-surface-3" />
                    <circle
                      cx="10" cy="10" r="8" fill="none" strokeWidth="3" strokeLinecap="round"
                      className="stroke-accent transition-all"
                      strokeDasharray={`${it.progress * 50.3} 50.3`}
                    />
                  </svg>
                )}
              </div>
              <div className="min-w-0 flex-1">
                {it.doc ? (
                  <Link to="/documents/$id" params={{ id: it.doc.id }} className="block truncate text-sm hover:underline">
                    {it.doc.title}
                  </Link>
                ) : (
                  <div className="truncate text-sm">{it.file.name}</div>
                )}
                <div className="text-xs text-subtle">
                  {it.status === "done" ? "Uploaded · reading text…" : it.status === "queued" ? "Waiting…" : formatBytes(it.file.size)}
                </div>
                {it.error && <div className="mt-1 text-xs text-danger">{it.error}</div>}
                {it.status === "duplicate" && it.duplicateOf && (
                  <div className="mt-1.5 flex gap-2">
                    <Button size="sm" asChild>
                      <Link to="/documents/$id" params={{ id: it.duplicateOf.id }}>
                        Open existing
                      </Link>
                    </Button>
                    <Button size="sm" variant="ghost" onClick={() => retry(it.id, true)}>
                      Upload anyway
                    </Button>
                  </div>
                )}
              </div>
              {it.status === "error" && (
                <button onClick={() => retry(it.id)} className="rounded p-1 text-muted hover:bg-surface-2" aria-label="Retry">
                  <RotateCw className="size-4" />
                </button>
              )}
              {it.status !== "done" && (
                <button onClick={() => remove(it.id)} className="rounded p-1 text-muted hover:bg-surface-2" aria-label="Cancel">
                  <X className="size-4" />
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/** Full-window drop zone shown while dragging files over the app. */
export function DropOverlay() {
  const [dragging, setDragging] = React.useState(false);
  const add = useUploads((s) => s.add);
  const me = useCurrentUser();
  const { uploadSpaceId, setUploadSpaceId } = useUI();
  const writable = me.spaces.filter((s) => s.role !== "viewer");
  const target = writable.find((s) => s.id === uploadSpaceId) ?? writable[0];
  const depth = React.useRef(0);

  React.useEffect(() => {
    const hasFiles = (e: DragEvent) => Array.from(e.dataTransfer?.types ?? []).includes("Files");
    const enter = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      depth.current++;
      setDragging(true);
    };
    const leave = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      depth.current = Math.max(0, depth.current - 1);
      if (depth.current === 0) setDragging(false);
    };
    const over = (e: DragEvent) => {
      if (hasFiles(e)) e.preventDefault();
    };
    const drop = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      depth.current = 0;
      setDragging(false);
      const files = Array.from(e.dataTransfer?.files ?? []);
      if (files.length && target) add(files, target.id);
    };
    window.addEventListener("dragenter", enter);
    window.addEventListener("dragleave", leave);
    window.addEventListener("dragover", over);
    window.addEventListener("drop", drop);
    return () => {
      window.removeEventListener("dragenter", enter);
      window.removeEventListener("dragleave", leave);
      window.removeEventListener("dragover", over);
      window.removeEventListener("drop", drop);
    };
  }, [add, target]);

  if (!dragging) return null;
  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center bg-accent/10 p-6 backdrop-blur-sm animate-in">
      <div className="pointer-events-auto flex w-full max-w-md flex-col items-center rounded-2xl border-2 border-dashed border-accent bg-surface/95 px-8 py-10 text-center shadow-lg">
        <FileUp className="size-10 text-accent" />
        <div className="mt-3 text-lg font-semibold">Drop to upload</div>
        {writable.length > 1 ? (
          <label className="mt-3 flex items-center gap-2 text-sm text-muted">
            into
            <NativeSelect className="w-auto" value={target?.id} onChange={(e) => setUploadSpaceId(e.target.value)}>
              {writable.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.kind === "personal" ? "Personal" : s.name}
                </option>
              ))}
            </NativeSelect>
          </label>
        ) : (
          <div className="mt-1 text-sm text-muted">PDFs, photos and scans</div>
        )}
      </div>
    </div>
  );
}
