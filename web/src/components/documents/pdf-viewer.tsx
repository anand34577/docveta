import * as React from "react";
import * as pdfjs from "pdfjs-dist";
import type { PDFDocumentProxy, PDFPageProxy } from "pdfjs-dist";
import workerUrl from "pdfjs-dist/build/pdf.worker.min.mjs?url";
import { ChevronDown, ChevronUp, Search, X } from "lucide-react";
import { Spinner } from "@/components/ui/misc";
import { cn } from "@/lib/utils";
import { findAll, markMatches, squash, squashQuery } from "./pdf-find";
import { usePinchZoom, useViewState, ViewerToolbar } from "./viewer-toolbar";

pdfjs.GlobalWorkerOptions.workerSrc = workerUrl;

/** The match to show: its page, its number on that page, and a counter that changes on every move. */
interface FindTarget {
  q: string;
  page: number;
  n: number;
  seq: number;
}

interface Props {
  url: string;
  initialPage?: number;
  highlight?: string[];
  onPageCount?: (n: number) => void;
}

/**
 * Lightweight PDF viewer: pages render lazily as they scroll into view, with a text
 * layer for selection and search-term highlighting, and Find (Ctrl+F) across all pages, including
 * ones not drawn yet. Loaded on demand (separate chunk).
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
  const find = useFind(doc);

  // Show the chosen match: bring its page into view; the page then scrolls to the match itself.
  React.useEffect(() => {
    const t = find.target;
    if (!t || t.page < 1) return;
    const el = container.current?.querySelector<HTMLElement>(`[data-page="${t.page}"]`);
    if (el && !el.querySelector(".kz-find-active")) el.scrollIntoView({ block: "start" });
  }, [find.target]);

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
          className={cn("flex-1 overflow-auto scrollbar-thin bg-surface-3 pb-16 [touch-action:pan-x_pan-y]", find.open ? "pt-16" : "pt-4")}
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
                <PdfPage key={i} doc={doc} pageNumber={i + 1} scale={scale} rotation={rotation} estimate={shown ?? baseSize} highlight={terms} find={find.target} root={container} />
              ))}
            </div>
          )}
        </div>
        {doc && find.open && <FindBar find={find} />}
        {doc && <ViewerToolbar view={view} percent={percent} page={current} pages={doc.numPages} thumbs={thumbs} onThumbs={() => setThumbs((t) => !t)} onFind={find.show} />}
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

function PdfPage({ doc, pageNumber, scale, rotation, estimate, highlight, find, root }: {
  doc: PDFDocumentProxy;
  pageNumber: number;
  scale: number;
  rotation: number;
  estimate: { w: number; h: number };
  highlight?: string[];
  find?: FindTarget | null;
  root: React.RefObject<HTMLDivElement | null>;
}) {
  const ref = React.useRef<HTMLDivElement>(null);
  // The drawn text layer's spans and their own text, for Find to mark up.
  const spans = React.useRef<{ leaves: HTMLElement[]; originals: string[] } | null>(null);
  const findRef = React.useRef(find);
  findRef.current = find;
  const scrolled = React.useRef(-1);
  const applyFind = React.useCallback(() => {
    const s = spans.current;
    if (!s) return;
    const f = findRef.current;
    const active = markMatches(s.leaves, s.originals, f?.q ?? "", f && f.page === pageNumber ? f.n : -1);
    if (active && f && f.seq !== scrolled.current) {
      scrolled.current = f.seq;
      active.scrollIntoView({ block: "center", inline: "nearest" });
    }
  }, [pageNumber]);
  React.useEffect(applyFind, [applyFind, find?.q, find?.page, find?.n, find?.seq]);
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
    spans.current = null;
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
        const leaves = Array.from(textDiv.querySelectorAll<HTMLElement>("span")).filter((s) => !s.classList.contains("markedContent"));
        spans.current = { leaves, originals: leaves.map((s) => s.textContent ?? "") };
        applyFind();
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
      task.cancel();
      textLayer?.cancel();
    };
  }, [page, scale, rotation, highlight, applyFind]);

  const w = Math.floor(estimate.w * scale);
  const h = Math.floor(estimate.h * scale);
  return <div ref={ref} data-page={pageNumber} className="pdf-page" style={{ width: page ? undefined : w, height: page ? undefined : h, minWidth: 50, minHeight: 50 }} />;
}

/** Find in document: searches every page's text, not only the pages drawn so far. */
function useFind(doc: PDFDocumentProxy | null) {
  const [open, setOpen] = React.useState(false);
  const [query, setQuery] = React.useState("");
  const [matches, setMatches] = React.useState<{ page: number; n: number }[]>([]);
  const [index, setIndex] = React.useState(0);
  const [seq, setSeq] = React.useState(0);
  const [state, setState] = React.useState<"idle" | "searching" | "done" | "no-text">("idle");
  const texts = React.useRef(new Map<number, string>());
  const input = React.useRef<HTMLInputElement>(null);

  React.useEffect(() => {
    texts.current = new Map();
  }, [doc]);

  React.useEffect(() => {
    const q = squashQuery(query);
    if (!doc || !open || !q) {
      setMatches([]);
      setState("idle");
      return;
    }
    let cancelled = false;
    setState("searching");
    const t = setTimeout(async () => {
      const found: { page: number; n: number }[] = [];
      let anyText = false;
      for (let p = 1; p <= doc.numPages; p++) {
        let text = texts.current.get(p);
        if (text === undefined) {
          const page = await doc.getPage(p);
          const content = await page.getTextContent();
          text = squash(content.items.map((i) => ("str" in i ? i.str : ""))).text;
          texts.current.set(p, text);
        }
        if (cancelled) return;
        if (text) anyText = true;
        findAll(text, q).forEach((_, n) => found.push({ page: p, n }));
      }
      setMatches(found);
      setIndex(0);
      setSeq((s) => s + 1);
      setState(anyText ? "done" : "no-text");
    }, 200);
    return () => {
      cancelled = true;
      clearTimeout(t);
    };
  }, [doc, open, query]);

  const show = React.useCallback(() => {
    setOpen(true);
    requestAnimationFrame(() => {
      input.current?.focus();
      input.current?.select();
    });
  }, []);
  const close = React.useCallback(() => setOpen(false), []);
  const move = React.useCallback(
    (d: number) => {
      if (!matches.length) return;
      setIndex((i) => (i + d + matches.length) % matches.length);
      setSeq((s) => s + 1);
    },
    [matches.length],
  );

  // Ctrl/⌘+F opens it (pressing it again inside the box gives the browser's own find);
  // F3 / Ctrl+G go to the next match, with Shift the previous one.
  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.ctrlKey || e.metaKey;
      const key = e.key.toLowerCase();
      const typing = e.target instanceof HTMLElement && e.target !== input.current && (e.target.isContentEditable || /^(input|textarea|select)$/i.test(e.target.tagName));
      if (mod && !e.altKey && key === "f" && e.target !== input.current && !typing) {
        e.preventDefault();
        show();
      } else if (open && (e.key === "F3" || (mod && key === "g"))) {
        e.preventDefault();
        move(e.shiftKey ? -1 : 1);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, show, move]);

  const m = matches[index];
  const target = React.useMemo<FindTarget | null>(
    () => (open && query.trim() ? { q: query, page: m?.page ?? -1, n: m?.n ?? -1, seq } : null),
    [open, query, m?.page, m?.n, seq],
  );
  return { open, show, close, query, setQuery, matches, index, move, state, input, target };
}

function FindBar({ find }: { find: ReturnType<typeof useFind> }) {
  const n = find.matches.length;
  const label =
    find.state === "searching" ? "Searching…" : find.state === "no-text" ? "No text yet" : find.state === "done" && !n ? "No matches" : n ? `${find.index + 1} of ${n}` : "";
  const btn = "inline-flex size-8 shrink-0 items-center justify-center rounded-full text-fg/80 hover:bg-surface-2 hover:text-fg disabled:opacity-40 sm:size-7";
  return (
    <div role="search" className="absolute inset-x-3 top-3 z-20 flex items-center gap-1 rounded-full border border-border bg-surface/95 py-1 pl-3 pr-1 shadow-md backdrop-blur sm:left-auto sm:w-96">
      <Search className="size-4 shrink-0 text-subtle" aria-hidden />
      <input
        ref={find.input}
        value={find.query}
        onChange={(e) => find.setQuery(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            find.move(e.shiftKey ? -1 : 1);
          } else if (e.key === "Escape") {
            e.preventDefault();
            e.stopPropagation();
            find.close();
          }
        }}
        placeholder="Find in document"
        aria-label="Find in document"
        enterKeyHint="search"
        className="h-8 min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-subtle sm:h-7"
      />
      <span
        className="shrink-0 px-1 text-xs tabular-nums text-muted"
        aria-live="polite"
        title={find.state === "no-text" ? "This document has no text to search yet. Scans get it once Docveta has read them." : undefined}
      >
        {label}
      </span>
      <button type="button" className={btn} onClick={() => find.move(-1)} disabled={!n} aria-label="Previous match" title="Previous (Shift+Enter)">
        <ChevronUp className="size-4" />
      </button>
      <button type="button" className={btn} onClick={() => find.move(1)} disabled={!n} aria-label="Next match" title="Next (Enter)">
        <ChevronDown className="size-4" />
      </button>
      <button type="button" className={btn} onClick={find.close} aria-label="Close find" title="Close (Esc)">
        <X className="size-4" />
      </button>
    </div>
  );
}
