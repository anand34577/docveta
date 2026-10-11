import * as React from "react";
import { Clock } from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger } from "./overlay";
import { cn } from "@/lib/utils";

const pad = (n: number) => String(n).padStart(2, "0");
const HOURS = Array.from({ length: 24 }, (_, i) => pad(i));
const MINUTES = Array.from({ length: 12 }, (_, i) => pad(i * 5));

/** A time of day ("HH:MM", 24-hour) picked from hour and minute columns. */
export function TimeInput({ value, onChange, className, "aria-label": ariaLabel }: { value: string; onChange: (v: string) => void; className?: string; "aria-label"?: string }) {
  const [open, setOpen] = React.useState(false);
  const [h = "00", m = "00"] = value.split(":");
  const lists = React.useRef<HTMLDivElement>(null);
  React.useEffect(() => {
    if (!open) return;
    // Bring the chosen hour and minute into view when the picker opens.
    requestAnimationFrame(() => lists.current?.querySelectorAll<HTMLElement>("[aria-selected=true]").forEach((el) => el.scrollIntoView({ block: "center" })));
  }, [open]);
  const column = (items: string[], cur: string, pick: (v: string) => void, label: string) => (
    <div role="listbox" aria-label={label} className="scrollbar-thin h-56 w-16 overflow-y-auto p-1">
      {items.map((v) => (
        <button key={v} type="button" role="option" aria-selected={v === cur} onClick={() => pick(v)}
          className={cn("flex h-8 w-full items-center justify-center rounded-md text-sm tabular-nums transition-colors",
            v === cur ? "bg-accent font-semibold text-accent-fg" : "hover:bg-surface-2")}>
          {v}
        </button>
      ))}
    </div>
  );
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger aria-label={ariaLabel}
        className={cn("flex h-10 w-28 items-center justify-between gap-2 rounded-md border border-border bg-surface px-3 text-[15px] tabular-nums text-fg transition-colors hover:border-border-strong focus:outline-none focus-visible:border-accent focus-visible:ring-3 focus-visible:ring-ring sm:h-9 sm:text-sm", className)}>
        {pad(Number(h))}:{pad(Number(m))}
        <Clock className="size-4 text-subtle" />
      </PopoverTrigger>
      <PopoverContent className="p-1">
        <div ref={lists} className="flex divide-x divide-border">
          {column(HOURS, pad(Number(h)), (v) => onChange(`${v}:${pad(Number(m))}`), "Hour")}
          {/* A minute that isn't a multiple of 5 (set elsewhere) is still shown as chosen. */}
          {column(MINUTES.includes(pad(Number(m))) ? MINUTES : [...MINUTES, pad(Number(m))].sort(), pad(Number(m)), (v) => { onChange(`${pad(Number(h))}:${v}`); setOpen(false); }, "Minute")}
        </div>
      </PopoverContent>
    </Popover>
  );
}
