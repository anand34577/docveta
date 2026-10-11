import * as React from "react";
import { Link } from "@tanstack/react-router";
import { AlertTriangle, X } from "lucide-react";
import { useStats } from "@/lib/queries";
import { useCurrentUser } from "@/components/app-shell";

const KEY = "docveta.ocr-warning-hidden-until";
const hiddenUntil = () => {
  try {
    return Number(localStorage.getItem(KEY)) || 0;
  } catch {
    return 0;
  }
};

/** Shown when nothing can read scanned text: uploads still work, but scans wait in the queue. */
export function OcrWarning() {
  const stats = useStats();
  const me = useCurrentUser();
  const [hidden, setHidden] = React.useState(() => hiddenUntil() > Date.now());
  if (hidden || !stats.data || stats.data.ocr_available !== false) return null;
  const dismiss = () => {
    try {
      localStorage.setItem(KEY, String(Date.now() + 7 * 864e5)); // back in a week if still not set up
    } catch {
      /* private mode: hidden until reload */
    }
    setHidden(true);
  };
  return (
    <div role="status" className="flex items-start gap-3 border-b border-warning/30 bg-warning-soft page-x py-2.5 text-sm text-warning">
      <AlertTriangle className="mt-0.5 size-4 shrink-0" />
      <p className="flex-1">
        <span className="font-medium">No text-reading worker is connected.</span> Scans and photos will wait until one is. Documents with real text, such as most PDFs, are searchable already.{" "}
        {me.is_admin ? (
          <Link to="/admin/$section" params={{ section: "processing" }} className="font-medium underline underline-offset-2">
            Set one up
          </Link>
        ) : (
          "Ask your administrator to connect one."
        )}
      </p>
      <button type="button" onClick={dismiss} aria-label="Hide for a week" title="Hide for a week" className="-my-1 -mr-1 rounded-md p-1 hover:bg-warning/15">
        <X className="size-4" />
      </button>
    </div>
  );
}
