import * as React from "react";
import { Input } from "./input";
import { Popover, PopoverAnchor, PopoverContent } from "./overlay";
import { cn } from "@/lib/utils";

type Props = React.InputHTMLAttributes<HTMLInputElement> & {
  suggestions: string[];
  /** Called with the suggestion picked; without it, picking sets the value like typing does. */
  onPick?: (v: string) => void;
};

/** A text field with a list of suggestions under it (what <datalist> does, in the app's style). */
export function SuggestInput({ suggestions, onPick, value, onChange, onKeyDown, onFocus, className, ...props }: Props) {
  const [open, setOpen] = React.useState(false);
  const [typed, setTyped] = React.useState(false);
  const [active, setActive] = React.useState(-1);
  const input = React.useRef<HTMLInputElement>(null);
  const q = String(value ?? "").trim().toLowerCase();
  // Everything on focus; once typing, what contains the text.
  const matches = suggestions.filter((s) => s.toLowerCase() !== q && (!typed || s.toLowerCase().includes(q))).slice(0, 8);
  const shown = open && matches.length > 0;
  const pick = (s: string) => {
    if (onPick) onPick(s);
    else onChange?.({ target: { value: s } } as React.ChangeEvent<HTMLInputElement>);
    setOpen(false);
    setActive(-1);
  };
  return (
    <Popover open={shown} onOpenChange={setOpen}>
      <PopoverAnchor asChild>
        <Input
          ref={input}
          value={value}
          autoComplete="off"
          role="combobox"
          aria-expanded={shown}
          aria-autocomplete="list"
          className={className}
          onFocus={(e) => {
            setTyped(false);
            setOpen(true);
            onFocus?.(e);
          }}
          onChange={(e) => {
            setTyped(true);
            setOpen(true);
            setActive(-1);
            onChange?.(e);
          }}
          onKeyDown={(e) => {
            if (shown && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
              e.preventDefault();
              setActive((a) => (a + (e.key === "ArrowDown" ? 1 : -1) + matches.length) % matches.length);
              return;
            }
            if (shown && e.key === "Enter" && active >= 0) {
              e.preventDefault();
              pick(matches[active]);
              return;
            }
            if (e.key === "Escape" && shown) {
              e.preventDefault();
              setOpen(false);
              return;
            }
            onKeyDown?.(e);
          }}
          {...props}
        />
      </PopoverAnchor>
      <PopoverContent
        className="w-[var(--radix-popper-anchor-width)] min-w-40 p-1"
        onOpenAutoFocus={(e) => e.preventDefault()}
        onInteractOutside={(e) => e.target === input.current && e.preventDefault()}
      >
        <div role="listbox" className="scrollbar-thin max-h-60 overflow-y-auto">
          {matches.map((s, i) => (
            <button
              key={s}
              type="button"
              role="option"
              aria-selected={i === active}
              onMouseDown={(e) => e.preventDefault()} // keep focus (and typing) in the field
              onClick={() => pick(s)}
              className={cn("flex w-full items-center rounded-md px-2.5 py-1.5 text-left text-sm", i === active ? "bg-surface-2" : "hover:bg-surface-2")}
            >
              {s}
            </button>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );
}
