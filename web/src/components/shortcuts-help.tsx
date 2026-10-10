import * as React from "react";
import { Dialog, DialogContent } from "@/components/ui/overlay";
import { Kbd } from "@/components/ui/misc";
import { modKey } from "@/lib/utils";

const groups: { title: string; rows: [string[], string][] }[] = [
  {
    title: "Everywhere",
    rows: [
      [[modKey, "K"], "Search and jump to anything"],
      [["/"], "Search"],
      [["?"], "Show this list"],
      [["G", "I"], "Go to Inbox"],
      [["G", "D"], "Go to all documents"],
      [["G", "H"], "Go to Home"],
    ],
  },
  {
    title: "Inbox",
    rows: [
      [["J"], "Next document"],
      [["K"], "Previous document"],
      [["E"], "Mark reviewed (Undo is offered)"],
      [["A"], "Accept all AI suggestions"],
      [["↵"], "Open the document"],
    ],
  },
  {
    title: "Documents",
    rows: [
      [["J"], "Next document (on a document page)"],
      [["K"], "Previous document"],
      [["Esc"], "Clear the selection"],
      [[modKey, "click"], "Add to the selection"],
      [["Shift", "click"], "Select a range"],
    ],
  },
];

/** Pressing "?" opens the shortcut list; "G" then a letter jumps between pages. */
export function ShortcutsHelp({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent title="Keyboard shortcuts" size="md">
        <div className="space-y-5">
          {groups.map((g) => (
            <section key={g.title}>
              <h3 className="mb-2 text-xs font-semibold uppercase tracking-wider text-subtle">{g.title}</h3>
              <ul className="space-y-1.5">
                {g.rows.map(([keys, what]) => (
                  <li key={what} className="flex items-center justify-between gap-4 text-sm">
                    <span>{what}</span>
                    <span className="flex shrink-0 items-center gap-1">
                      {keys.map((k, i) => (
                        <React.Fragment key={k}>
                          {i > 0 && <span className="text-xs text-subtle">{k === "click" ? "+" : "then"}</span>}
                          <Kbd>{k}</Kbd>
                        </React.Fragment>
                      ))}
                    </span>
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}
