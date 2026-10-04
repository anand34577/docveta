import * as React from "react";
import * as pdfjs from "pdfjs-dist";
import type { PDFDocumentProxy, PDFPageProxy } from "pdfjs-dist";
import workerUrl from "pdfjs-dist/build/pdf.worker.min.mjs?url";
import { Spinner } from "@/components/ui/misc";
import { cn } from "@/lib/utils";
import { usePinchZoom, useViewState, ViewerToolbar } from "./viewer-toolbar";

pdfjs.GlobalWorkerOptions.workerSrc = workerUrl;

interface Props {
  url: string;
  initialPage?: number;
  highlight?: string[];
  onPageCount?: (n: number) => void;
}

/**
 * Lightweight PDF viewer: pages render lazily as they scroll into view, with a text
 * layer for selection and search-term highlighting. Loaded on demand (separate chunk).
 */
export default function PdfViewer({ url, initialPage, highlight, onPageCount }: Props) {
  const [doc, setDoc] = React.useState<PDFDocumentProxy | null>(null);
  const [error, setError] = React.useState<string | null>(null);
  const view = useViewState();
  const { fit, zoom, rotation } = view;
  const [box, setBox] = React.useState({ w: 0, h: 0 });
  const [current, setCurrent] = React.useState(1);
  const [baseSize, setBaseSize] = React.useState<{ w: number; h: number } | null>(null);
  const [thumbs, setThumbs] = React.useState(false);
  const container = React.useRef<HTMLDivElement>(null);
  const content = React.useRef<HTMLDivElement>(null);
  // Callers often pass a new function or array on every render; don't reload or repaint for that.
  const pageCountCb = React.useRef(onPageCount);
  pageCountCb.current = onPageCount;
  const highlightKey = (highlight ?? []).join("");
  const terms = React.useMemo(() => (highlightKey ? highlightKey.split("") : []), [highlightKey]);

  React.useEffect(() => {
    let cancelled = false;
    setDoc(null);
    setError(null);
    const task = pdfjs.getDocument({ url, enableXfa: false });
    task.promise.then(
      async (d) => {
        if (cancelled) return;
        const p1 = await d.getPage(1);
        const vp = p1.getViewport({ scale: 1 });
        setBaseSize({ w: vp.width, h: vp.height });
        setDoc(d);
        pageCountCb.current?.(d.numPages);
      },
      (e: Error) => !cancelled && setError(e.name === "PasswordException" ? "This PDF is password protected." : "This PDF couldn't be displayed. You can still download it."),
    );
    return () => {
      cancelled = true;
      void task.destroy();
    };
  }, [url]);

  React.useLayoutEffect(() => {
    if (!container.current) return;
    const ro = new ResizeObserver((e) => setBox({ w: e[0].contentRect.width, h: e[0].contentRect.height }));
    ro.observe(container.current);
    return () => ro.disconnect();
  }, []);
  usePinchZoom(container, content, view.setZoom);

  const sideways = rotation % 180 !== 0;
  const shown = baseSize ? (sideways ? { w: baseSize.h, h: baseSize.w } : baseSize) : null;
  let scale = 1;
  if (shown && box.w) {
    const byWidth = Math.max(box.w - 32, 120) / shown.w;
    const fitScale = fit === "page" && box.h ? Math.min(byWidth, Math.max(box.h - 32, 120) / shown.h) : byWidth;
    scale = Math.min(fitScale * zoom, 8);
  }
  // pdf.js scale 1 is 72 dpi; "100%" means the page's real size on a 96 dpi screen.
  const percent = Math.round((scale * 72 * 100) / 96);
  const goTo = (n: number) => container.current?.querySelector(`[data-page="${n}"]`)?.scrollIntoView({ block: "start", behavior: "smooth" });

  // Scroll to the requested page once laid out.
  React.useEffect(() => {
    if (!doc || !initialPage || initialPage < 2 || !scale) return;
    const el = container.current?.querySelector(`[data-page="${initialPage}"]`);
    el?.scrollIntoView({ block: "start" });
  }, [doc, initialPage]); // not on zoom: that would jump back while reading

  if (error) return <div className="flex h-full items-center justify-center p-6 text-center text-sm text-muted">{error}</div>;

  return (
    <div className="relative flex h-full">
      {thumbs && doc && <Thumbs doc={doc} current={current} onPick={goTo} rotation={rotation} />}
      <div className="relative isolate flex min-w-0 flex-1 flex-col">
        <div
          ref={container}
          className="flex-1 overflow-auto scrollbar-thin bg-surface-3 pt-4 pb-16 [touch-action:pan-x_pan-y]"
          onScroll={(e) => {
            const el = e.currentTarget;
            const pages = el.querySelectorAll<HTMLElement>("[data-page]");
            const mid = el.scrollTop + el.clientHeight / 3;
            for (const p of pages) {
              if (p.offsetTop + p.offsetHeight > mid) {
                setCurrent(Number(p.dataset.page));
                break;
              }
            }
          }}
        >
          {!doc || !baseSize ? (
            <div className="flex h-full items-center justify-center">
              <Spinner />
            </div>
          ) : (
            <div ref={content} className="px-4">
              {Array.from({ length: doc.numPages }, (_, i) => (
                <PdfPage key={i} doc={doc} pageNumber={i + 1} scale={scale} rotation={rotation} estimate={shown ?? baseSize} highlight={terms} root={container} />
              ))}
            </div>
          )}
        </div>
        {doc && <ViewerToolbar view={view} percent={percent} page={current} pages={doc.numPages} thumbs={thumbs} onThumbs={() => setThumbs((t) => !t)} />}
      </div>
    </div>
  );
}

/** Strip of small page previews; click one to jump to it. */
function Thumbs({ doc, current, onPick, rotation }: { doc: PDFDocumentProxy; current: number; onPick: (n: number) => void; rotation: number }) {
  return (
    <div className="hidden w-28 shrink-0 space-y-2 overflow-y-auto scrollbar-thin border-r border-border bg-surface-2 p-2 sm:block" aria-label="Pages">
      {Array.from({ length: doc.numPages }, (_, i) => (
        <Thumb key={i} doc={doc} n={i + 1} active={current === i + 1} onPick={onPick} rotation={rotation} />
      ))}
    </div>
  );
}

function Thumb({ doc, n, active, onPick, rotation }: { doc: PDFDocumentProxy; n: number; active: boolean; onPick: (n: number) => void; rotation: number }) {
  const host = React.useRef<HTMLButtonElement>(null);
  const canvas = React.useRef<HTMLCanvasElement>(null);
  const [seen, setSeen] = React.useState(false);
  React.useEffect(() => {
    if (!host.current) return;
    const io = new IntersectionObserver((e) => e[0].isIntersecting && setSeen(true), { rootMargin: "300px" });
    io.observe(host.current);
    return () => io.disconnect();
  }, []);
  React.useEffect(() => {
    if (!seen) return;
    let dead = false;
    doc.getPage(n).then((p) => {
      if (dead || !canvas.current) return;
      const vp0 = p.getViewport({ scale: 1, rotation });
      const vp = p.getViewport({ scale: 96 / vp0.width, rotation });
      const c = canvas.current;
      c.width = vp.width;
      c.height = vp.height;
      void p.render({ canvas: c, viewport: vp }).promise.catch(() => undefined);
    });
    return () => {
      dead = true;
    };
  }, [seen, doc, n, rotation]);
  return (
    <button ref={host} onClick={() => onPick(n)} aria-label={`Page ${n}`} aria-current={active} className={cn("block w-full rounded border bg-surface p-1 text-center", active ? "border-accent ring-2 ring-ring" : "border-border hover:border-border-strong")}>
      <canvas ref={canvas} className="mx-auto max-w-full bg-white" style={{ minHeight: 40 }} />
      <span className="mt-0.5 block text-[11px] tabular-nums text-subtle">{n}</span>
    </button>
  );
}

function PdfPage({ doc, pageNumber, scale, rotation, estimate, highlight, root }: {
  doc: PDFDocumentProxy;
  pageNumber: number;
  scale: number;
  rotation: number;
  estimate: { w: number; h: number };
  highlight?: string[];
  root: React.RefObject<HTMLDivElement | null>;
}) {
  const ref = React.useRef<HTMLDivElement>(null);
  const [visible, setVisible] = React.useState(false);
  const [page, setPage] = React.useState<PDFPageProxy | null>(null);

  React.useEffect(() => {
    if (!ref.current) return;
    const io = new IntersectionObserver((e) => e[0].isIntersecting && setVisible(true), { root: root.current, rootMargin: "800px 0px" });
    io.observe(ref.current);
    return () => io.disconnect();
  }, [root]);

  React.useEffect(() => {
    if (!visible) return;
    let cancelled = false;
    doc.getPage(pageNumber).then((p) => !cancelled && setPage(p));
    return () => {
      cancelled = true;
    };
  }, [visible, doc, pageNumber]);

  React.useEffect(() => {
    if (!page || !ref.current) return;
    const host = ref.current;
    const viewport = page.getViewport({ scale, rotation });
    const dpr = Math.min(window.devicePixelRatio || 1, 2);
    const canvas = document.createElement("canvas");
    canvas.width = Math.floor(viewport.width * dpr);
    canvas.height = Math.floor(viewport.height * dpr);
    canvas.style.width = `${Math.floor(viewport.width)}px`;
    canvas.style.height = `${Math.floor(viewport.height)}px`;
    const textDiv = document.createElement("div");
    textDiv.className = "textLayer";
    textDiv.style.setProperty("--total-scale-factor", String(scale));

    const task = page.render({ canvas, viewport, transform: dpr !== 1 ? [dpr, 0, 0, dpr, 0, 0] : undefined });
    let textLayer: pdfjs.TextLayer | null = null;
    let cancelled = false;
    task.promise
      .then(async () => {
        if (cancelled) return;
        host.replaceChildren(canvas, textDiv);
        textLayer = new pdfjs.TextLayer({ textContentSource: page.streamTextContent(), container: textDiv, viewport });
        await textLayer.render();
        if (highlight?.length) {
          const terms = highlight.map((t) => t.toLowerCase()).filter((t) => t.length > 1);
          textDiv.querySelectorAll("span").forEach((s) => {
            const t = (s.textContent || "").toLowerCase();
            if (terms.some((term) => t.includes(term))) s.classList.add("kz-hit");
          });
        }
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
      task.cancel();
      textLayer?.cancel();
    };
  }, [page, scale, rotation, highlight]);

  const w = Math.floor(estimate.w * scale);
  const h = Math.floor(estimate.h * scale);
  return <div ref={ref} data-page={pageNumber} className="pdf-page" style={{ width: page ? undefined : w, height: page ? undefined : h, minWidth: 50, minHeight: 50 }} />;
}
