import * as React from "react";
import { CalendarDays } from "lucide-react";
import { Input } from "./input";
import { formatDocDate, parseDocDate } from "@/lib/utils";
import { cn } from "@/lib/utils";

interface Props {
  value: string | null | undefined; // YYYY-MM-DD
  onChange: (v: string | null) => void;
  id?: string;
  disabled?: boolean;
  className?: string;
  placeholder?: string;
}

/**
 * A date field that shows and accepts dates the way the person set them up (for example
 * 31/07/2026), with the browser's own calendar one click away.
 */
export function DateInput({ value, onChange, id, disabled, className, placeholder }: Props) {
  const [text, setText] = React.useState(formatDocDate(value));
  const [bad, setBad] = React.useState(false);
  const native = React.useRef<HTMLInputElement>(null);
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
    const iso = parseDocDate(t);
    if (!iso) {
      setBad(true);
      return;
    }
    setBad(false);
    if (iso !== value) onChange(iso);
    else setText(formatDocDate(iso));
  };

  return (
    <div className={cn("relative", className)}>
      <Input
        id={id}
        value={text}
        disabled={disabled}
        aria-invalid={bad || undefined}
        placeholder={placeholder ?? formatDocDate("2026-12-31").replace("2026", "yyyy").replace("12", "mm").replace("31", "dd")}
        className="pr-9"
        onChange={(e) => setText(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => e.key === "Enter" && commit()}
      />
      <button
        type="button"
        disabled={disabled}
        aria-label="Pick from calendar"
        className="absolute right-1 top-1/2 flex size-7 -translate-y-1/2 items-center justify-center rounded text-subtle hover:bg-surface-2 hover:text-fg disabled:opacity-50"
        onClick={() => native.current?.showPicker?.()}
      >
        <CalendarDays className="size-4" />
      </button>
      <input
        ref={native}
        type="date"
        tabIndex={-1}
        aria-hidden
        value={value ?? ""}
        onChange={(e) => onChange(e.target.value || null)}
        className="pointer-events-none absolute right-0 top-full h-0 w-0 opacity-0"
      />
      {bad && <p className="mt-1 text-[13px] text-danger">Enter a date like {formatDocDate("2026-12-31")}</p>}
    </div>
  );
}
