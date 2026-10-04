import { Link } from "@tanstack/react-router";
import { AlertTriangle } from "lucide-react";
import { useStats } from "@/lib/queries";
import { useCurrentUser } from "@/components/app-shell";

/** Shown when nothing can read scanned text: uploads still work, but scans wait in the queue. */
export function OcrWarning() {
  const stats = useStats();
  const me = useCurrentUser();
  if (!stats.data || stats.data.ocr_available !== false) return null;
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
    </div>
  );
}
