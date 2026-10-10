import * as React from "react";
import { Link } from "@tanstack/react-router";
import { cn, usePageTitle } from "@/lib/utils";
import { Card, useScrollEdges } from "./ui/misc";

export interface SectionDef {
  id: string;
  label: string;
  icon: React.ReactNode;
}

/** Two-column settings layout: section list (tabs on mobile) + content. */
export function SettingsLayout({ title, description, base, sections, active, children }: {
  title: string;
  description?: React.ReactNode;
  base: string;
  sections: SectionDef[];
  active: string;
  children: React.ReactNode;
}) {
  const nav = useScrollEdges<HTMLElement>();
  usePageTitle(title);
  return (
    <div className="mx-auto max-w-5xl page-x pb-16 pt-6 sm:pt-8">
      <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
      {description && <p className="mt-1 text-sm text-muted">{description}</p>}
      <div className="mt-6 flex flex-col gap-6 md:flex-row">
        <nav ref={nav} className="scroll-x -mx-4 flex gap-1 border-b border-border px-4 pb-2 sm:-mx-6 sm:px-6 md:sticky md:top-4 md:mx-0 md:max-h-[calc(100dvh-6rem)] md:w-52 md:shrink-0 md:flex-col md:self-start md:overflow-y-auto md:border-0 md:px-0 md:pb-0" aria-label="Sections">
          {sections.map((s) => (
            <Link
              key={s.id}
              to={`${base}/${s.id}` as "/"}
              className={cn(
                "flex shrink-0 items-center gap-2.5 rounded-md px-3 py-2 text-sm text-muted hover:bg-surface-2 hover:text-fg [&_svg]:size-4",
                active === s.id && "bg-surface-2 font-medium text-fg md:shadow-[inset_2px_0_0_var(--accent)]",
              )}
            >
              {s.icon}
              {s.label}
            </Link>
          ))}
        </nav>
        <div className="min-w-0 flex-1 space-y-6">{children}</div>
      </div>
    </div>
  );
}

export function SettingsCard({ title, description, children, actions, className }: {
  title: React.ReactNode;
  description?: React.ReactNode;
  children?: React.ReactNode;
  actions?: React.ReactNode;
  className?: string;
}) {
  return (
    <Card className={cn("overflow-hidden", className)}>
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-border px-5 py-4">
        <div>
          <h2 className="text-[15px] font-semibold">{title}</h2>
          {description && <p className="mt-0.5 text-[13px] text-muted">{description}</p>}
        </div>
        {actions && <div className="flex gap-2">{actions}</div>}
      </div>
      {children && <div className="px-5 py-4">{children}</div>}
    </Card>
  );
}

/** Shows a secret exactly once with a copy button. */
export function SecretReveal({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = React.useState(false);
  return (
    <div className="rounded-lg border border-warning/40 bg-warning-soft p-3">
      <div className="text-[13px] font-medium text-warning">{label}</div>
      <div className="mt-2 flex items-center gap-2">
        <code className="flex-1 break-all rounded bg-surface px-2 py-1.5 font-mono text-xs">{value}</code>
        <button
          className="shrink-0 rounded-md border border-border bg-surface px-2.5 py-1.5 text-xs hover:bg-surface-2"
          onClick={() => {
            void navigator.clipboard.writeText(value);
            setCopied(true);
          }}
        >
          {copied ? "Copied" : "Copy"}
        </button>
      </div>
      <p className="mt-2 text-xs text-muted">You won't be able to see this again.</p>
    </div>
  );
}
