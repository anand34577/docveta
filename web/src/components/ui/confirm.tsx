import * as React from "react";
import { create } from "zustand";
import { Button } from "./button";
import { Input } from "./input";
import { Dialog, DialogContent, DialogFooter } from "./overlay";

interface ConfirmOptions {
  title: string;
  body?: React.ReactNode;
  confirmLabel?: string;
  destructive?: boolean;
}

interface ConfirmState extends ConfirmOptions {
  open: boolean;
  value?: string; // set → the dialog shows a text field (see prompt)
  resolve?: (ok: boolean) => void;
}

const useConfirmStore = create<ConfirmState>(() => ({ open: false, title: "" }));

/** Imperative confirmation dialog: `if (await confirm({...})) { ... }` */
export function confirm(opts: ConfirmOptions): Promise<boolean> {
  return new Promise((resolve) => useConfirmStore.setState({ ...opts, value: undefined, open: true, resolve }));
}

/** Imperative text-input dialog: resolves the trimmed text, or null if cancelled/empty. */
export function prompt(opts: ConfirmOptions & { defaultValue?: string }): Promise<string | null> {
  return new Promise((resolve) =>
    useConfirmStore.setState({
      ...opts,
      value: opts.defaultValue ?? "",
      open: true,
      resolve: (ok) => resolve(ok ? useConfirmStore.getState().value?.trim() || null : null),
    }),
  );
}

export function ConfirmHost() {
  const s = useConfirmStore();
  const body = React.useRef<HTMLDivElement>(null);
  const close = (ok: boolean) => {
    s.resolve?.(ok);
    useConfirmStore.setState({ open: false, resolve: undefined });
  };
  return (
    <Dialog open={s.open} onOpenChange={(o) => !o && close(false)}>
      {/* Radix would focus the close button; focus the field, the primary action, or Cancel for destructive ones. */}
      <DialogContent title={s.title} size="sm" onOpenAutoFocus={(e) => {
        e.preventDefault();
        body.current?.querySelector<HTMLElement>("[data-autofocus]")?.focus();
      }}>
        <div ref={body}>
        {s.body && <div className="text-sm text-muted">{s.body}</div>}
        {s.value !== undefined && (
          <form id="confirm-prompt" onSubmit={(e) => { e.preventDefault(); close(true); }}>
            <Input aria-label={s.title} value={s.value} data-autofocus onFocus={(e) => e.currentTarget.select()}
              onChange={(e) => useConfirmStore.setState({ value: e.target.value })} />
          </form>
        )}
        <DialogFooter>
          <Button onClick={() => close(false)} data-autofocus={s.value === undefined && s.destructive ? "" : undefined}>Cancel</Button>
          <Button variant={s.destructive ? "danger" : "primary"} onClick={() => close(true)}
            data-autofocus={s.value === undefined && !s.destructive ? "" : undefined}>
            {s.confirmLabel ?? "Confirm"}
          </Button>
        </DialogFooter>
        </div>
      </DialogContent>
    </Dialog>
  );
}
