import * as React from "react";
import { Checkbox as CB, Switch as SW, Tabs as TB } from "radix-ui";
import { Check, Minus } from "lucide-react";
import { cn, tagColors } from "@/lib/utils";

/* ---------------------------------------------------------------- Badge / tag chip */

export function Badge({ className, tone = "neutral", ...props }: React.HTMLAttributes<HTMLSpanElement> & { tone?: "neutral" | "accent" | "success" | "warning" | "danger" }) {
  const tones = {
    neutral: "bg-surface-2 text-muted",
    accent: "bg-accent-soft text-accent-soft-fg",
    success: "bg-success-soft text-success",
    warning: "bg-warning-soft text-warning",
    danger: "bg-danger-soft text-danger",
  };
  return <span className={cn("inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap", tones[tone], className)} {...props} />;
}

export function TagChip({ name, color = "slate", className, onRemove }: { name: string; color?: string; className?: string; onRemove?: () => void }) {
  return (
    <span className={cn("inline-flex max-w-full items-center gap-1 rounded-md px-1.5 py-0.5 text-xs font-medium", tagColors[color] ?? tagColors.slate, className)}>
      <span className="truncate">{name}</span>
      {onRemove && (
        // Not a <button>: chips sit inside the entity picker's trigger button, and buttons can't nest.
        // Keyboard users remove tags from the picker's list instead.
        <span
          role="button"
          aria-label={`Remove ${name}`}
          onClick={(e) => {
            e.preventDefault();
            e.stopPropagation();
            onRemove();
          }}
          className="-mr-0.5 cursor-pointer rounded px-0.5 opacity-60 hover:opacity-100"
        >
          ×
        </span>
      )}
    </span>
  );
}

/* ---------------------------------------------------------------- Card */

export function Card({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("rounded-xl border border-border bg-surface shadow-sm", className)} {...props} />;
}

/* ---------------------------------------------------------------- Skeleton / spinner */

export function Skeleton({ className }: { className?: string }) {
  return <div className={cn("animate-pulse rounded-md bg-surface-3", className)} />;
}

export function Spinner({ className }: { className?: string }) {
  return (
    <svg className={cn("size-5 animate-spin text-muted", className)} viewBox="0 0 24 24" fill="none" aria-label="Loading">
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeOpacity="0.2" strokeWidth="3" />
      <path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
    </svg>
  );
}

/* ---------------------------------------------------------------- Empty state */

export function EmptyState({ icon, title, children, action, className }: { icon?: React.ReactNode; title: string; children?: React.ReactNode; action?: React.ReactNode; className?: string }) {
  return (
    <div className={cn("flex flex-col items-center justify-center px-6 py-16 text-center", className)}>
      {icon && <div className="mb-4 flex size-12 items-center justify-center rounded-xl bg-surface-2 text-muted [&_svg]:size-6">{icon}</div>}
      <h3 className="text-base font-semibold">{title}</h3>
      {children && <p className="mt-1.5 max-w-sm text-sm text-muted">{children}</p>}
      {action && <div className="mt-5">{action}</div>}
    </div>
  );
}

/* ---------------------------------------------------------------- Checkbox / switch */

export function Checkbox({ className, checked, ...props }: React.ComponentPropsWithoutRef<typeof CB.Root>) {
  return (
    <CB.Root
      checked={checked}
      className={cn(
        "flex size-[18px] shrink-0 items-center justify-center rounded-[5px] border border-border-strong bg-surface transition-colors",
        "data-[state=checked]:border-accent data-[state=checked]:bg-accent data-[state=checked]:text-accent-fg",
        "data-[state=indeterminate]:border-accent data-[state=indeterminate]:bg-accent data-[state=indeterminate]:text-accent-fg",
        className,
      )}
      {...props}
    >
      <CB.Indicator>{checked === "indeterminate" ? <Minus className="size-3" strokeWidth={3} /> : <Check className="size-3" strokeWidth={3} />}</CB.Indicator>
    </CB.Root>
  );
}

export function Switch({ className, ...props }: React.ComponentPropsWithoutRef<typeof SW.Root>) {
  return (
    <SW.Root
      className={cn(
        "relative inline-flex h-[22px] w-[38px] shrink-0 items-center rounded-full bg-border-strong transition-colors data-[state=checked]:bg-accent disabled:opacity-50",
        className,
      )}
      {...props}
    >
      <SW.Thumb className="block size-[18px] translate-x-[2px] rounded-full bg-white shadow-sm transition-transform data-[state=checked]:translate-x-[18px]" />
    </SW.Root>
  );
}

/** A labelled switch row used in settings. */
export function SwitchRow({ label, description, checked, onCheckedChange, disabled }: { label: string; description?: string; checked: boolean; onCheckedChange: (v: boolean) => void; disabled?: boolean }) {
  const id = React.useId();
  return (
    <div className="flex items-start justify-between gap-4 py-3">
      <label htmlFor={id} className="flex-1 cursor-pointer">
        <div className="text-sm font-medium">{label}</div>
        {description && <div className="mt-0.5 text-[13px] text-muted">{description}</div>}
      </label>
      <Switch id={id} checked={checked} onCheckedChange={onCheckedChange} disabled={disabled} />
    </div>
  );
}

/* ---------------------------------------------------------------- Tabs */

export const Tabs = TB.Root;
export const TabsContent = TB.Content;

export function TabsList({ className, ...props }: React.ComponentPropsWithoutRef<typeof TB.List>) {
  return <TB.List className={cn("flex gap-1 border-b border-border", className)} {...props} />;
}

export function TabsTrigger({ className, ...props }: React.ComponentPropsWithoutRef<typeof TB.Trigger>) {
  return (
    <TB.Trigger
      className={cn(
        "relative -mb-px border-b-2 border-transparent px-3 py-2 text-sm font-medium text-muted transition-colors hover:text-fg",
        "data-[state=active]:border-accent data-[state=active]:text-fg",
        className,
      )}
      {...props}
    />
  );
}

/* ---------------------------------------------------------------- Avatar */

export function Avatar({ name, className }: { name: string; className?: string }) {
  const hue = [...name].reduce((h, c) => (h * 31 + c.charCodeAt(0)) % 360, 0);
  const parts = name.trim().split(/\s+/);
  const ini = ((parts[0]?.[0] || "") + (parts.length > 1 ? parts[parts.length - 1][0] : "")).toUpperCase() || "?";
  return (
    <span
      className={cn("inline-flex size-8 shrink-0 select-none items-center justify-center rounded-full text-xs font-semibold text-white", className)}
      style={{ background: `oklch(0.6 0.12 ${hue})` }}
      aria-hidden
    >
      {ini}
    </span>
  );
}

/* ---------------------------------------------------------------- Kbd */

export function Kbd({ children, className }: { children: React.ReactNode; className?: string }) {
  return <kbd className={cn("rounded border border-border bg-surface-2 px-1.5 py-px font-sans text-[11px] text-muted", className)}>{children}</kbd>;
}
