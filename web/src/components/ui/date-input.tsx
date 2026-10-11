import * as React from "react";
import { addDays, addMonths, addYears, format, isSameDay, isSameMonth, startOfMonth, startOfWeek } from "date-fns";
import { CalendarDays, ChevronLeft, ChevronRight } from "lucide-react";
import { Input } from "./input";
import { Popover, PopoverAnchor, PopoverContent } from "./overlay";
import { cn, formatDocDate, parseDocDate } from "@/lib/utils";

interface Props {
  value: string | null | undefined; // YYYY-MM-DD
  onChange: (v: string | null) => void;
  id?: string;
  disabled?: boolean;
  className?: string;
  placeholder?: string;
}

const iso = (d: Date) => format(d, "yyyy-MM-dd");
const fromIso = (s: string | null | undefined) => {
  if (!s) return null;
  const [y, m, d] = s.split("-").map(Number);
  return new Date(y, m - 1, d);
};

/**
 * A date field that shows and accepts dates the way the person set them up (for example
 * 31/07/2026), with a calendar one click away.
 */
export function DateInput({ value, onChange, id, disabled, className, placeholder }: Props) {
  const [text, setText] = React.useState(formatDocDate(value));
  const [bad, setBad] = React.useState(false);
  const [open, setOpen] = React.useState(false);
  React.useEffect(() => {
    setText(formatDocDate(value));
    setBad(false);
  }, [value]);

  const commit = () => {
    const t = text.trim();
    if (!t) {
      setBad(false);
      if (value) onChange(null);
      return;
    }
    const parsed = parseDocDate(t);
    if (!parsed) {
      setBad(true);
      return;
    }
    setBad(false);
    if (parsed !== value) onChange(parsed);
    else setText(formatDocDate(parsed));
  };

  return (
    <div className={cn("relative", className)}>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverAnchor asChild>
          <div className="relative">
            <Input
              id={id}
              value={text}
              disabled={disabled}
              aria-invalid={bad || undefined}
              placeholder={placeholder ?? formatDocDate("2026-12-31").replace("2026", "yyyy").replace("12", "mm").replace("31", "dd")}
              className="pr-9"
              onChange={(e) => setText(e.target.value)}
              onBlur={commit}
              onKeyDown={(e) => {
                if (e.key === "Enter") commit();
                if (e.key === "ArrowDown" && e.altKey) setOpen(true);
              }}
            />
            <button
              type="button"
              disabled={disabled}
              aria-label="Pick from calendar"
              aria-expanded={open}
              className="absolute right-1 top-1/2 flex size-7 -translate-y-1/2 items-center justify-center rounded text-subtle hover:bg-surface-2 hover:text-fg disabled:opacity-50"
              onClick={() => setOpen(!open)}
            >
              <CalendarDays className="size-4" />
            </button>
          </div>
        </PopoverAnchor>
        <PopoverContent align="end" className="w-[296px] p-3" onOpenAutoFocus={(e) => e.preventDefault()}>
          <Calendar
            value={value ?? null}
            onSelect={(v) => {
              setOpen(false);
              setBad(false);
              if (v !== (value ?? null)) onChange(v);
              else setText(formatDocDate(v));
            }}
          />
        </PopoverContent>
      </Popover>
      {bad && <p className="mt-1 text-[13px] text-danger">Enter a date like {formatDocDate("2026-12-31")}</p>}
    </div>
  );
}

// Monday or Sunday first, as people in the browser's region expect (1 = Monday … 7 = Sunday).
const firstDay = (() => {
  try {
    const l = new Intl.Locale(navigator.language) as Intl.Locale & { weekInfo?: { firstDay: number }; getWeekInfo?: () => { firstDay: number } };
    return ((l.getWeekInfo?.() ?? l.weekInfo)?.firstDay ?? 1) % 7;
  } catch {
    return 1;
  }
})() as 0 | 1 | 2 | 3 | 4 | 5 | 6;

/** Month grid with month and year views for jumping far (an ID issued in 2009). */
export function Calendar({ value, onSelect }: { value: string | null; onSelect: (v: string | null) => void }) {
  const selected = fromIso(value);
  const today = new Date();
  const [focus, setFocus] = React.useState(selected ?? today);
  const [view, setView] = React.useState<"days" | "months" | "years">("days");
  const grid = React.useRef<HTMLDivElement>(null);
  const moved = React.useRef(false);

  React.useEffect(() => {
    // Keep keyboard focus on the focused day as it moves (and when the calendar opens).
    if (view === "days") grid.current?.querySelector<HTMLElement>('[tabindex="0"]')?.focus({ preventScroll: !moved.current });
  }, [focus, view]);

  const start = startOfWeek(startOfMonth(focus), { weekStartsOn: firstDay });
  const days = Array.from({ length: 42 }, (_, i) => addDays(start, i));
  const weekdays = days.slice(0, 7).map((d) => format(d, "EEEEEE"));

  const onKey = (e: React.KeyboardEvent) => {
    const step: Record<string, () => Date> = {
      ArrowLeft: () => addDays(focus, -1),
      ArrowRight: () => addDays(focus, 1),
      ArrowUp: () => addDays(focus, -7),
      ArrowDown: () => addDays(focus, 7),
      PageUp: () => (e.shiftKey ? addYears(focus, -1) : addMonths(focus, -1)),
      PageDown: () => (e.shiftKey ? addYears(focus, 1) : addMonths(focus, 1)),
      Home: () => startOfWeek(focus, { weekStartsOn: firstDay }),
      End: () => addDays(startOfWeek(focus, { weekStartsOn: firstDay }), 6),
    };
    if (step[e.key]) {
      e.preventDefault();
      moved.current = true;
      setFocus(step[e.key]());
    }
  };

  const nav = (delta: number) =>
    setFocus(view === "days" ? addMonths(focus, delta) : view === "months" ? addYears(focus, delta) : addYears(focus, delta * 12));
  const decade = Math.floor(focus.getFullYear() / 12) * 12;
  const cell = "flex items-center justify-center rounded-md text-sm transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring";

  return (
    <div className="select-none">
      <div className="mb-2 flex items-center justify-between">
        <button type="button" onClick={() => setView(view === "days" ? "months" : "years")}
          className="rounded-md px-2 py-1 text-sm font-semibold hover:bg-surface-2" aria-live="polite">
          {view === "days" ? format(focus, "MMMM yyyy") : view === "months" ? format(focus, "yyyy") : `${decade} – ${decade + 11}`}
        </button>
        <div className="flex gap-0.5">
          <button type="button" onClick={() => nav(-1)} aria-label="Previous" className="flex size-8 items-center justify-center rounded-md text-muted hover:bg-surface-2 hover:text-fg">
            <ChevronLeft className="size-4" />
          </button>
          <button type="button" onClick={() => nav(1)} aria-label="Next" className="flex size-8 items-center justify-center rounded-md text-muted hover:bg-surface-2 hover:text-fg">
            <ChevronRight className="size-4" />
          </button>
        </div>
      </div>

      {view === "days" && (
        <div role="grid" ref={grid} onKeyDown={onKey} aria-label={format(focus, "MMMM yyyy")}>
          <div role="row" className="grid grid-cols-7 pb-1">
            {weekdays.map((w) => (
              <div key={w} role="columnheader" className="text-center text-[11px] font-medium uppercase text-subtle">{w}</div>
            ))}
          </div>
          {[0, 1, 2, 3, 4, 5].map((r) => (
            <div key={r} role="row" className="grid grid-cols-7 gap-0.5">
              {days.slice(r * 7, r * 7 + 7).map((d) => {
                const isSel = !!selected && isSameDay(d, selected);
                return (
                  <button key={d.toISOString()} type="button" role="gridcell" aria-selected={isSel}
                    tabIndex={isSameDay(d, focus) ? 0 : -1} aria-label={format(d, "d MMMM yyyy")}
                    onClick={() => onSelect(iso(d))}
                    className={cn(cell, "h-9",
                      isSel ? "bg-accent font-semibold text-accent-fg" : "hover:bg-surface-2",
                      !isSel && !isSameMonth(d, focus) && "text-subtle/60",
                      !isSel && isSameDay(d, today) && "font-semibold text-accent ring-1 ring-inset ring-accent/40")}>
                    {d.getDate()}
                  </button>
                );
              })}
            </div>
          ))}
        </div>
      )}

      {view === "months" && (
        <div className="grid grid-cols-3 gap-1">
          {Array.from({ length: 12 }, (_, m) => new Date(focus.getFullYear(), m, 1)).map((d) => (
            <button key={d.getMonth()} type="button" onClick={() => { setFocus(new Date(d.getFullYear(), d.getMonth(), Math.min(focus.getDate(), 28))); setView("days"); }}
              className={cn(cell, "h-11", d.getMonth() === focus.getMonth() ? "bg-accent-soft font-semibold text-accent-soft-fg" : "hover:bg-surface-2")}>
              {format(d, "MMM")}
            </button>
          ))}
        </div>
      )}

      {view === "years" && (
        <div className="grid grid-cols-3 gap-1">
          {Array.from({ length: 12 }, (_, i) => decade + i).map((y) => (
            <button key={y} type="button" onClick={() => { setFocus(new Date(y, focus.getMonth(), Math.min(focus.getDate(), 28))); setView("months"); }}
              className={cn(cell, "h-11 tabular-nums", y === focus.getFullYear() ? "bg-accent-soft font-semibold text-accent-soft-fg" : "hover:bg-surface-2", y === today.getFullYear() && "text-accent")}>
              {y}
            </button>
          ))}
        </div>
      )}

      <div className="mt-2 flex items-center justify-between border-t border-border pt-2">
        <button type="button" onClick={() => onSelect(null)} className="rounded-md px-2 py-1 text-sm text-muted hover:bg-surface-2 hover:text-fg">Clear</button>
        <button type="button" onClick={() => onSelect(iso(today))} className="rounded-md px-2 py-1 text-sm font-medium text-accent hover:bg-accent-soft">Today</button>
      </div>
    </div>
  );
}
