import * as React from "react";
import { Spinner } from "@/components/ui/misc";
import { usePinchZoom, useViewState, ViewerToolbar } from "./viewer-toolbar";

/** Picture viewer with the same fit / zoom / turn controls as the PDF viewer. */
export function ImageViewer({ src, alt }: { src: string; alt: string }) {
  const view = useViewState();
  const { fit, zoom, rotation } = view;
  const container = React.useRef<HTMLDivElement>(null);
  const content = React.useRef<HTMLDivElement>(null);
  const [box, setBox] = React.useState({ w: 0, h: 0 });
  const [natural, setNatural] = React.useState<{ w: number; h: number } | null>(null);
  const [failed, setFailed] = React.useState(false);

  React.useEffect(() => {
    setNatural(null);
    setFailed(false);
  }, [src]);
  React.useLayoutEffect(() => {
    if (!container.current) return;
    const ro = new ResizeObserver((e) => setBox({ w: e[0].contentRect.width, h: e[0].contentRect.height }));
    ro.observe(container.current);
    return () => ro.disconnect();
  }, []);
  usePinchZoom(container, content, view.setZoom);

  const sideways = rotation % 180 !== 0;
  const shown = natural ? (sideways ? { w: natural.h, h: natural.w } : natural) : null;
  let scale = 1;
  if (shown && box.w) {
    const byWidth = Math.max(box.w - 32, 120) / shown.w;
    // Fit width never enlarges a small picture past its real size; zooming in still can.
    const fitScale = fit === "page" && box.h ? Math.min(byWidth, Math.max(box.h - 96, 120) / shown.h) : Math.min(byWidth, 1);
    scale = Math.min(fitScale * zoom, 8);
  }
  const outer = shown ? { width: shown.w * scale, height: shown.h * scale } : undefined;
  const inner = natural ? { width: natural.w * scale, height: natural.h * scale } : undefined;

  if (failed) return <div className="flex h-full items-center justify-center p-6 text-center text-sm text-muted">This picture couldn't be shown. You can still download it.</div>;
  return (
    <div className="relative isolate h-full">
      <div ref={container} className="h-full overflow-auto scrollbar-thin bg-surface-3 px-4 pt-4 pb-16 [touch-action:pan-x_pan-y]">
        {!natural && (
          <div className="flex h-full items-center justify-center">
            <Spinner />
          </div>
        )}
        <div ref={content} className="relative mx-auto" style={outer ?? { width: 0, height: 0, overflow: "hidden" }}>
          <img
            src={src}
            alt={alt}
            draggable={false}
            onLoad={(e) => setNatural({ w: e.currentTarget.naturalWidth || 1, h: e.currentTarget.naturalHeight || 1 })}
            onError={() => setFailed(true)}
            className="absolute top-1/2 left-1/2 max-w-none rounded-md bg-white shadow-md"
            style={{ ...inner, transform: `translate(-50%, -50%) rotate(${rotation}deg)`, imageOrientation: "from-image" }}
          />
        </div>
      </div>
      {natural && <ViewerToolbar view={view} percent={Math.round(scale * 100)} />}
    </div>
  );
}
