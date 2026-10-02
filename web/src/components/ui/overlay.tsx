import * as React from "react";
import { Dialog as D, DropdownMenu as DM, Popover as P, Tooltip as T } from "radix-ui";
import { X } from "lucide-react";
import { cn } from "@/lib/utils";

/* ---------------------------------------------------------------- Dialog */

export const Dialog = D.Root;
export const DialogTrigger = D.Trigger;
export const DialogClose = D.Close;

interface DialogContentProps extends Omit<React.ComponentPropsWithoutRef<typeof D.Content>, "title"> {
  title: React.ReactNode;
  description?: React.ReactNode;
  size?: "sm" | "md" | "lg" | "xl";
  hideClose?: boolean;
}

/** Centered dialog on desktop, bottom sheet on phones. */
export function DialogContent({ title, description, size = "md", hideClose, className, children, ...props }: DialogContentProps) {
  const width = { sm: "sm:max-w-sm", md: "sm:max-w-lg", lg: "sm:max-w-2xl", xl: "sm:max-w-4xl" }[size];
  return (
    <D.Portal>
      <D.Overlay className="fixed inset-0 z-50 bg-overlay animate-in backdrop-blur-[2px]" />
      <D.Content
        className={cn(
          "fixed z-50 bg-surface shadow-lg flex flex-col outline-none",
          "inset-x-0 bottom-0 max-h-[92dvh] rounded-t-xl animate-sheet pb-safe",
          "sm:inset-auto sm:left-1/2 sm:top-[12vh] sm:-translate-x-1/2 sm:w-[calc(100vw-2rem)] sm:max-h-[76vh] sm:rounded-xl sm:animate-pop sm:pb-0",
          width,
          className,
        )}
        {...props}
      >
        <div className="flex items-start gap-3 px-5 pt-5 pb-3">
          <div className="flex-1 min-w-0">
            <D.Title className="text-[17px] font-semibold leading-tight">{title}</D.Title>
            {description ? (
              <D.Description className="mt-1 text-sm text-muted">{description}</D.Description>
            ) : (
              <D.Description className="sr-only">{typeof title === "string" ? title : "Dialog"}</D.Description>
            )}
          </div>
          {!hideClose && (
            <D.Close className="-mr-1.5 -mt-1 rounded-md p-1.5 text-muted hover:bg-surface-2 hover:text-fg" aria-label="Close">
              <X className="size-4" />
            </D.Close>
          )}
        </div>
        <div className="flex-1 overflow-y-auto scrollbar-thin px-5 pb-5">{children}</div>
      </D.Content>
    </D.Portal>
  );
}

export function DialogFooter({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("mt-5 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end", className)} {...props} />;
}

/* ---------------------------------------------------------------- Dropdown menu */

export const DropdownMenu = DM.Root;
export const DropdownMenuTrigger = DM.Trigger;
export const DropdownMenuGroup = DM.Group;

export function DropdownMenuContent({ className, align = "end", sideOffset = 6, ...props }: React.ComponentPropsWithoutRef<typeof DM.Content>) {
  return (
    <DM.Portal>
      <DM.Content
        align={align}
        sideOffset={sideOffset}
        className={cn(
          "z-50 min-w-48 rounded-lg border border-border bg-surface p-1 shadow-md animate-pop max-h-[var(--radix-dropdown-menu-content-available-height)] overflow-y-auto",
          className,
        )}
        {...props}
      />
    </DM.Portal>
  );
}

export function DropdownMenuItem({ className, destructive, ...props }: React.ComponentPropsWithoutRef<typeof DM.Item> & { destructive?: boolean }) {
  return (
    <DM.Item
      className={cn(
        "flex cursor-default select-none items-center gap-2.5 rounded-md px-2.5 py-2 text-sm outline-none [&_svg]:size-4 [&_svg]:text-muted",
        "data-[highlighted]:bg-surface-2 data-[disabled]:opacity-50 data-[disabled]:pointer-events-none",
        destructive && "text-danger [&_svg]:text-danger data-[highlighted]:bg-danger-soft",
        className,
      )}
      {...props}
    />
  );
}

export function DropdownMenuLabel({ className, ...props }: React.ComponentPropsWithoutRef<typeof DM.Label>) {
  return <DM.Label className={cn("px-2.5 py-1.5 text-xs font-medium text-subtle", className)} {...props} />;
}

export function DropdownMenuSeparator() {
  return <DM.Separator className="my-1 h-px bg-border" />;
}

/* ---------------------------------------------------------------- Popover */

export const Popover = P.Root;
export const PopoverTrigger = P.Trigger;
export const PopoverAnchor = P.Anchor;

export function PopoverContent({ className, align = "start", sideOffset = 6, ...props }: React.ComponentPropsWithoutRef<typeof P.Content>) {
  return (
    <P.Portal>
      <P.Content
        align={align}
        sideOffset={sideOffset}
        className={cn("z-50 rounded-lg border border-border bg-surface shadow-md animate-pop outline-none", className)}
        {...props}
      />
    </P.Portal>
  );
}

/* ---------------------------------------------------------------- Tooltip */

export const TooltipProvider = T.Provider;

export function Tooltip({ content, children, side = "bottom" }: { content: React.ReactNode; children: React.ReactNode; side?: "top" | "bottom" | "left" | "right" }) {
  return (
    <T.Root delayDuration={400}>
      <T.Trigger asChild>{children}</T.Trigger>
      <T.Portal>
        <T.Content side={side} sideOffset={6} className="z-50 rounded-md bg-fg px-2 py-1 text-xs text-bg shadow-md animate-in">
          {content}
        </T.Content>
      </T.Portal>
    </T.Root>
  );
}
