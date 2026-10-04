import * as React from "react";
import * as pdfjs from "pdfjs-dist";
import type { PDFDocumentProxy, PDFPageProxy } from "pdfjs-dist";
import workerUrl from "pdfjs-dist/build/pdf.worker.min.mjs?url";
import { Minus, PanelLeft, Plus, RotateCw } from "lucide-react";
import { Spinner } from "@/components/ui/misc";
import { cn } from "@/lib/utils";

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
  const [zoom, setZoom] = React.useState(1); // multiplier on fit-width
  const [width, setWidth] = React.useState(0);
  const [current, setCurrent] = React.useState(1);
  const [baseSize, setBaseSize] = React.useState<{ w: number; h: number } | null>(null);
  const [rotation, setRotation] = React.useState(0); // viewing only; Arrange pages makes it permanent
  const [thumbs, setThumbs] = React.useState(false);
  const container = React.useRef<HTMLDivElement>(null);

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
        onPageCount?.(d.numPages);
      },
      (e: Error) => !cancelled && setError(e.name === "PasswordException" ? "This PDF is password protected." : "This PDF couldn't be displayed. You can still download it."),
    );
    return () => {
      cancelled = true;
      void task.destroy();
    };
  }, [url, onPageCount]);

  React.useLayoutEffect(() => {
    if (!container.current) return;
    const ro = new ResizeObserver((e) => setWidth(e[0].contentRect.width));
    ro.observe(container.current);
    return () => ro.disconnect();
  }, []);

  const sideways = rotation % 180 !== 0;
  const shown = baseSize ? (sideways ? { w: baseSize.h, h: baseSize.w } : baseSize) : null;
  const scale = shown && width ? Math.min(((width - 32) / shown.w) * zoom, 4) : 1;
  const goTo = (n: number) => container.current?.querySelector(`[data-page="${n}"]`)?.scrollIntoView({ block: "start", behavior: "smooth" });

  // Scroll to the requested page once laid out.
  React.useEffect(() => {
    if (!doc || !initialPage || initialPage < 2 || !scale) return;
    const el = container.current?.querySelector(`[data-page="${initialPage}"]`);
    el?.scrollIntoView({ block: "start" });
  }, [doc, initialPage, scale]);

  if (error) return <div className="flex h-full items-center justify-center p-6 text-center text-sm text-muted">{error}</div>;

  return (
    <div className="relative flex h-full">
      {thumbs && doc && <Thumbs doc={doc} current={current} onPick={goTo} rotation={rotation} />}
      <div className="relative flex min-w-0 flex-1 flex-col">
      <div ref={container} className="flex-1 overflow-auto scrollbar-thin bg-surface-3 py-4" onScroll={(e) => {
        const el = e.currentTarget;
        const pages = el.querySelectorAll<HTMLElement>("[data-page]");
        const mid = el.scrollTop + el.clientHeight / 3;
        for (const p of pages) {
          if (p.offsetTop + p.offsetHeight > mid) {
            setCurrent(Number(p.dataset.page));
            break;
          }
        }
      }}>
        {!doc || !baseSize ? (
          <div className="flex h-full items-center justify-center">
            <Spinner />
          </div>
        ) : (
          Array.from({ length: doc.numPages }, (_, i) => (
            <PdfPage key={i} doc={doc} pageNumber={i + 1} scale={scale} rotation={rotation} estimate={shown ?? baseSize} highlight={highlight} root={container} />
          ))
        )}
      </div>
      {doc && (
        <div className="pointer-events-none absolute inset-x-0 bottom-3 flex justify-center">
          <div className="pointer-events-auto flex items-center gap-1 rounded-full border border-border bg-surface/95 px-1.5 py-1 text-xs shadow-md backdrop-blur">
            <button className={cn("rounded-full p-1.5 hover:bg-surface-2", thumbs && "bg-accent-soft text-accent-soft-fg")} onClick={() => setThumbs((t) => !t)} aria-label="Page thumbnails" aria-pressed={thumbs}>
              <PanelLeft className="size-3.5" />
            </button>
            <button className="rounded-full p-1.5 hover:bg-surface-2" onClick={() => setZoom((z) => Math.max(0.5, z - 0.25))} aria-label="Zoom out">
              <Minus className="size-3.5" />
            </button>
            <button className="min-w-12 tabular-nums text-muted" onClick={() => setZoom(1)} aria-label="Fit width">
              {Math.round(zoom * 100)}%
            </button>
            <button className="rounded-full p-1.5 hover:bg-surface-2" onClick={() => setZoom((z) => Math.min(3, z + 0.25))} aria-label="Zoom in">
              <Plus className="size-3.5" />
            </button>
            <button className="rounded-full p-1.5 hover:bg-surface-2" onClick={() => setRotation((r) => (r + 90) % 360)} aria-label="Turn the view" title="Turn the view (just for looking; use Arrange pages to save)">
              <RotateCw className="size-3.5" />
            </button>
            <span className="border-l border-border pl-2 pr-1.5 tabular-nums text-muted">
              {current} / {doc.numPages}
            </span>
          </div>
        </div>
      )}
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
