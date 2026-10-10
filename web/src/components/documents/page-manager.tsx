import * as React from "react";
import * as pdfjs from "pdfjs-dist";
import type { PDFDocumentProxy } from "pdfjs-dist";
import workerUrl from "pdfjs-dist/build/pdf.worker.min.mjs?url";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, ArrowRight, RotateCcw, RotateCw, Scissors, Trash2, Undo2 } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { invalidateDocuments } from "@/lib/queries";
import type { Document } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/misc";
import { Dialog, DialogContent } from "@/components/ui/overlay";

pdfjs.GlobalWorkerOptions.workerSrc = workerUrl;

interface Item {
  key: number; // stable identity (original page number)
  rotate: number; // extra clockwise degrees
}

/**
 * Arrange pages: turn, delete, reorder and pull pages out into a new document.
 * Saving writes a new version of the file (the old one stays in Versions).
 */
export function PageManager({ doc, onClose }: { doc: Document; onClose: () => void }) {
  const isPdf = doc.mime_type === "application/pdf";
  const qc = useQueryClient();
  const [pdf, setPdf] = React.useState<PDFDocumentProxy | null>(null);
  const [items, setItems] = React.useState<Item[]>([]);
  const [picked, setPicked] = React.useState<Set<number>>(new Set());
  const [busy, setBusy] = React.useState(false);
  const [dragKey, setDragKey] = React.useState<number | null>(null);
  const [failed, setFailed] = React.useState(false);
  const original = React.useRef<Item[]>([]);

  React.useEffect(() => {
    if (!isPdf) {
      original.current = [{ key: 1, rotate: 0 }];
      setItems(original.current);
      return;
    }
    let dead = false;
    const task = pdfjs.getDocument({ url: `/api/v1/documents/${doc.id}/file?kind=original&v=${doc.version}` });
    task.promise.then(
      (d) => {
        if (dead) return;
        setPdf(d);
        original.current = Array.from({ length: d.numPages }, (_, i) => ({ key: i + 1, rotate: 0 }));
        setItems(original.current);
      },
      () => !dead && setFailed(true),
    );
    return () => {
      dead = true;
      void task.destroy();
    };
  }, [doc.id, doc.version, isPdf]);

  const changed = JSON.stringify(items) !== JSON.stringify(original.current);
  const rotate = (key: number, by: number) => setItems((l) => l.map((i) => (i.key === key ? { ...i, rotate: (((i.rotate + by) % 360) + 360) % 360 } : i)));
  const remove = (keys: number[]) => {
    setItems((l) => (l.length - keys.length >= 1 ? l.filter((i) => !keys.includes(i.key)) : l));
    setPicked(new Set());
  };
  const move = (key: number, delta: number) =>
    setItems((l) => {
      const i = l.findIndex((x) => x.key === key);
      const j = i + delta;
      if (i < 0 || j < 0 || j >= l.length) return l;
      const c = [...l];
      [c[i], c[j]] = [c[j], c[i]];
      return c;
    });
  const dropOn = (target: number) => {
    if (dragKey === null || dragKey === target) return;
    setItems((l) => {
      const from = l.findIndex((i) => i.key === dragKey);
      const to = l.findIndex((i) => i.key === target);
      const c = [...l];
      const [m] = c.splice(from, 1);
      c.splice(to, 0, m);
      return c;
    });
    setDragKey(null);
  };
  const toggle = (key: number) =>
    setPicked((s) => {
      const n = new Set(s);
      if (n.has(key)) n.delete(key);
      else n.add(key);
      return n;
    });

  const save = async () => {
    setBusy(true);
    try {
      await api.post(`/documents/${doc.id}/pages/edit`, { pages: items.map((i) => ({ from: i.key, rotate: i.rotate })) });
      invalidateDocuments(qc, doc.id);
      qc.invalidateQueries({ queryKey: ["document", doc.id] });
      toast.success("Pages saved", { description: "The previous file is kept under Versions." });
      onClose();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const extract = async () => {
    const nums = items.filter((i) => picked.has(i.key)).map((i) => i.key).sort((a, b) => a - b);
    if (!nums.length) return;
    setBusy(true);
    try {
      await api.post(`/documents/${doc.id}/split`, { ranges: [nums.join(",")], trash_original: false });
      invalidateDocuments(qc, doc.id);
      toast.success(`${nums.length} page${nums.length > 1 ? "s" : ""} copied into a new document`);
      setPicked(new Set());
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent
        size="xl"
        title="Arrange pages"
        description={isPdf ? "Turn, reorder or delete pages. Select pages to copy them into a new document." : "Turn the picture the right way up."}
        className="sm:top-[6vh] sm:max-h-[88vh]"
      >
        {failed ? (
          <div className="flex h-48 flex-col items-center justify-center gap-3 text-center text-sm text-muted" role="alert">
            Couldn't open the pages of this file. It may be damaged or still locked with a password.
            <Button onClick={onClose}>Close</Button>
          </div>
        ) : !items.length ? (
          <div className="flex h-48 items-center justify-center">
            <Spinner />
          </div>
        ) : (
          <>
            <div className="mb-4 flex flex-wrap items-center gap-2 text-sm">
              {isPdf && (
                <>
                  <Button size="sm" disabled={!picked.size || busy} onClick={extract}>
                    <Scissors /> New document from {picked.size || "selected"}
                  </Button>
                  <Button size="sm" variant="danger-ghost" disabled={!picked.size || busy} onClick={() => remove([...picked])}>
                    <Trash2 /> Delete {picked.size || "selected"}
                  </Button>
                  <Button size="sm" variant="ghost" disabled={!picked.size} onClick={() => setPicked(new Set())}>
                    Clear selection
                  </Button>
                </>
              )}
              <span className="ml-auto text-xs text-subtle">{items.length} page{items.length > 1 ? "s" : ""}</span>
            </div>
            <ul className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4">
              {items.map((it, idx) => (
                <li
                  key={it.key}
                  draggable={isPdf}
                  onDragStart={() => setDragKey(it.key)}
                  onDragOver={(e) => e.preventDefault()}
                  onDrop={() => dropOn(it.key)}
                  className={cn("group rounded-lg border bg-surface p-2 transition-shadow", picked.has(it.key) ? "border-accent ring-2 ring-ring" : "border-border", dragKey === it.key && "opacity-50")}
                >
                  <button type="button" className="relative block w-full" onClick={() => isPdf && toggle(it.key)} aria-pressed={picked.has(it.key)} aria-label={`Page ${it.key}${picked.has(it.key) ? ", selected" : ""}`}>
                    <PageThumb pdf={pdf} page={it.key} rotate={it.rotate} src={isPdf ? undefined : `/api/v1/documents/${doc.id}/file?kind=original&v=${doc.version}`} />
                    <span className="absolute left-1.5 top-1.5 rounded bg-black/60 px-1.5 text-[11px] font-medium text-white">{idx + 1}</span>
                  </button>
                  <div className="mt-2 flex items-center justify-center gap-0.5">
                    <Button size="icon-sm" variant="ghost" aria-label="Turn left" onClick={() => rotate(it.key, -90)}>
                      <RotateCcw />
                    </Button>
                    <Button size="icon-sm" variant="ghost" aria-label="Turn right" onClick={() => rotate(it.key, 90)}>
                      <RotateCw />
                    </Button>
                    {isPdf && (
                      <>
                        <Button size="icon-sm" variant="ghost" aria-label="Move earlier" disabled={idx === 0} onClick={() => move(it.key, -1)}>
                          <ArrowLeft />
                        </Button>
                        <Button size="icon-sm" variant="ghost" aria-label="Move later" disabled={idx === items.length - 1} onClick={() => move(it.key, 1)}>
                          <ArrowRight />
                        </Button>
                        <Button size="icon-sm" variant="danger-ghost" aria-label="Delete page" disabled={items.length === 1} onClick={() => remove([it.key])}>
                          <Trash2 />
                        </Button>
                      </>
                    )}
                  </div>
                </li>
              ))}
            </ul>
            {/* Sticky boxes stop at the scroll area's padding: reach past it so no page shows under the buttons. */}
            <div className="sticky -bottom-5 -mx-5 -mb-5 mt-5 flex flex-col-reverse gap-2 border-t border-border bg-surface px-5 py-3 sm:flex-row sm:justify-end">
              <Button disabled={!changed} onClick={() => setItems(original.current)}>
                <Undo2 /> Reset
              </Button>
              <Button onClick={onClose}>Cancel</Button>
              <Button variant="primary" loading={busy} disabled={!changed} onClick={save}>
                Save changes
              </Button>
            </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

function PageThumb({ pdf, page, rotate, src }: { pdf: PDFDocumentProxy | null; page: number; rotate: number; src?: string }) {
  const canvas = React.useRef<HTMLCanvasElement>(null);
  const [ratio, setRatio] = React.useState(1.35);
  // Draw a page only when its tile comes near the screen: a 300-page file would otherwise
  // render all 300 the moment the dialog opens.
  const [seen, setSeen] = React.useState(false);
  React.useEffect(() => {
    if (!canvas.current) return;
    const io = new IntersectionObserver((e) => e.some((x) => x.isIntersecting) && setSeen(true), { rootMargin: "400px" });
    io.observe(canvas.current);
    return () => io.disconnect();
  }, []);
  React.useEffect(() => {
    if (!pdf || !canvas.current || !seen) return;
    let dead = false;
    pdf.getPage(page).then((p) => {
      if (dead || !canvas.current) return;
      const base = p.getViewport({ scale: 1 });
      const scale = 200 / base.width;
      const vp = p.getViewport({ scale });
      const c = canvas.current;
      c.width = vp.width;
      c.height = vp.height;
      setRatio(vp.height / vp.width);
      void p.render({ canvas: c, viewport: vp }).promise.catch(() => undefined);
    });
    return () => {
      dead = true;
    };
  }, [pdf, page, seen]);
  const turned = rotate % 180 !== 0;
  return (
    <div className="flex aspect-[3/4] w-full items-center justify-center overflow-hidden rounded bg-surface-3">
      {src ? (
        <img src={src} alt="" className="max-h-full max-w-full object-contain transition-transform" style={{ transform: `rotate(${rotate}deg)${turned ? " scale(0.75)" : ""}` }} />
      ) : (
        <canvas
          ref={canvas}
          className="max-h-full max-w-full bg-white object-contain shadow-sm transition-transform"
          style={{ transform: `rotate(${rotate}deg)${turned ? ` scale(${Math.min(1, 1 / ratio) * 1.0})` : ""}` }}
        />
      )}
    </div>
  );
}
