import * as React from "react";
import { Maximize, Minus, MoveHorizontal, PanelLeft, Plus, RotateCw } from "lucide-react";
import { cn } from "@/lib/utils";

export type FitMode = "width" | "page";

const MIN_ZOOM = 0.25;
const MAX_ZOOM = 5;
export const clampZoom = (z: number) => Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, z));

/** View state shared by the PDF and picture viewers: fit mode, zoom on top of the fit, turn. */
export function useViewState() {
  const [fit, setFit] = React.useState<FitMode>("width");
  const [zoom, setZoom] = React.useState(1); // multiplier on the fit
  const [rotation, setRotation] = React.useState(0); // viewing only
  const setFitMode = React.useCallback((f: FitMode) => {
    setFit(f);
    setZoom(1);
  }, []);
  return { fit, setFit: setFitMode, zoom, setZoom, rotation, setRotation };
}

/**
 * Pinch (two fingers) and Ctrl/⌘ + wheel zoom on a scroll container. While the fingers move the
 * content is only scaled with CSS (cheap); the new zoom is committed when they lift, so pages
 * re-render once.
 */
export function usePinchZoom(scroller: React.RefObject<HTMLElement | null>, content: React.RefObject<HTMLElement | null>, setZoom: React.Dispatch<React.SetStateAction<number>>) {
  React.useEffect(() => {
    const el = scroller.current;
    if (!el) return;
    let start = 0;
    let ratio = 1;
    const dist = (t: TouchList) => Math.hypot(t[0].clientX - t[1].clientX, t[0].clientY - t[1].clientY);
    const onStart = (e: TouchEvent) => {
      if (e.touches.length === 2) {
        start = dist(e.touches);
        ratio = 1;
      }
    };
    const onMove = (e: TouchEvent) => {
      if (e.touches.length !== 2 || !start) return;
      e.preventDefault(); // our zoom, not the browser's page zoom
      ratio = Math.min(4, Math.max(0.25, dist(e.touches) / start));
      if (content.current) {
        content.current.style.transformOrigin = "top center";
        content.current.style.transform = `scale(${ratio})`;
      }
    };
    const onEnd = (e: TouchEvent) => {
      if (!start || e.touches.length >= 2) return;
      start = 0;
      if (content.current) content.current.style.transform = "";
      if (Math.abs(ratio - 1) > 0.02) setZoom((z) => clampZoom(z * ratio));
    };
    const onWheel = (e: WheelEvent) => {
      if (!e.ctrlKey && !e.metaKey) return; // trackpad pinch arrives as ctrl+wheel too
      e.preventDefault();
      setZoom((z) => clampZoom(z * Math.exp(-e.deltaY / 300)));
    };
    el.addEventListener("touchstart", onStart, { passive: true });
    el.addEventListener("touchmove", onMove, { passive: false });
    el.addEventListener("touchend", onEnd);
    el.addEventListener("touchcancel", onEnd);
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => {
      el.removeEventListener("touchstart", onStart);
      el.removeEventListener("touchmove", onMove);
      el.removeEventListener("touchend", onEnd);
      el.removeEventListener("touchcancel", onEnd);
      el.removeEventListener("wheel", onWheel);
    };
  }, [scroller, content, setZoom]);
}

function ToolButton({ label, active, className, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement> & { label: string; active?: boolean }) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      aria-pressed={active}
      className={cn("inline-flex size-8 items-center justify-center rounded-full text-fg/80 hover:bg-surface-2 hover:text-fg focus-visible:outline-2 focus-visible:outline-ring sm:size-7", active && "bg-accent-soft text-accent-soft-fg", className)}
      {...props}
    />
  );
}

/**
 * Floating controls at the bottom of a viewer. It sits above the document's text layer and can't
 * be selected, so tapping it never selects document text (z-20 inside the viewer's isolated
 * stacking context; select-none; touch-action: manipulation stops double-tap word selection).
 */
export function ViewerToolbar({ view, percent, page, pages, thumbs, onThumbs }: {
  view: ReturnType<typeof useViewState>;
  percent: number;
  page?: number;
  pages?: number;
  thumbs?: boolean;
  onThumbs?: () => void;
}) {
  const { fit, setFit, setZoom, setRotation } = view;
  return (
    <div className="pointer-events-none absolute inset-x-0 bottom-3 z-20 flex select-none justify-center px-2 pb-safe [touch-action:manipulation]">
      <div role="toolbar" aria-label="View" className="pointer-events-auto flex items-center gap-0.5 rounded-full border border-border bg-surface/95 px-1.5 py-1 text-xs shadow-md backdrop-blur">
        {onThumbs && (
          <ToolButton label="Page thumbnails" active={thumbs} onClick={onThumbs} className="hidden sm:inline-flex">
            <PanelLeft className="size-4 sm:size-3.5" />
          </ToolButton>
        )}
        <ToolButton label="Fit width" active={fit === "width" && view.zoom === 1} onClick={() => setFit("width")}>
          <MoveHorizontal className="size-4 sm:size-3.5" />
        </ToolButton>
        <ToolButton label="Fit page" active={fit === "page" && view.zoom === 1} onClick={() => setFit("page")}>
          <Maximize className="size-4 sm:size-3.5" />
        </ToolButton>
        <span className="mx-0.5 h-5 w-px bg-border" aria-hidden />
        <ToolButton label="Zoom out" onClick={() => setZoom((z) => clampZoom(z / 1.25))}>
          <Minus className="size-4 sm:size-3.5" />
        </ToolButton>
        <span className="min-w-11 text-center tabular-nums text-muted" aria-live="polite" aria-label={`Zoom ${percent}%`}>
          {percent}%
        </span>
        <ToolButton label="Zoom in" onClick={() => setZoom((z) => clampZoom(z * 1.25))}>
          <Plus className="size-4 sm:size-3.5" />
        </ToolButton>
        <ToolButton label="Turn the view (just for looking; use Arrange pages to save)" onClick={() => setRotation((r) => (r + 90) % 360)}>
          <RotateCw className="size-4 sm:size-3.5" />
        </ToolButton>
        {pages !== undefined && pages > 0 && (
          <span className="ml-0.5 border-l border-border pl-2 pr-1.5 tabular-nums text-muted">
            {page} / {pages}
          </span>
        )}
      </div>
    </div>
  );
}
