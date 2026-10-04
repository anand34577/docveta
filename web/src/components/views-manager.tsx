import * as React from "react";
import { Link } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, Bookmark, Pencil, Pin, PinOff, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { keys, useSavedViews } from "@/lib/queries";
import type { SavedView } from "@/lib/types";
import { spaceLabel } from "@/lib/utils";
import { SettingsCard } from "@/components/settings-layout";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge, EmptyState, Skeleton } from "@/components/ui/misc";
import { confirm } from "@/components/ui/confirm";
import { useCurrentUser } from "@/components/app-shell";

/** Rename, pin to the sidebar, reorder and delete saved views. */
export function ViewsManager() {
  const views = useSavedViews();
  const me = useCurrentUser();
  const qc = useQueryClient();
  const [editing, setEditing] = React.useState<string | null>(null);
  const [name, setName] = React.useState("");
  const list = [...(views.data ?? [])].sort((a, b) => a.sort_order - b.sort_order || a.name.localeCompare(b.name));
  const refresh = () => qc.invalidateQueries({ queryKey: keys.views });
  const patch = async (v: SavedView, body: Record<string, unknown>) => {
    await api.patch(`/saved-views/${v.id}`, body).then(refresh, (e) => toast.error(errorMessage(e)));
  };
  const move = async (i: number, d: number) => {
    const a = list[i];
    const b = list[i + d];
    if (!a || !b) return;
    // Give every view a clean position so equal numbers can't leave the order ambiguous.
    await Promise.all(list.map((v, idx) => idx !== i && idx !== i + d && v.sort_order !== idx ? patch(v, { sort_order: idx }) : null));
    await Promise.all([patch(a, { sort_order: i + d }), patch(b, { sort_order: i })]);
  };
  const remove = async (v: SavedView) => {
    if (!(await confirm({ title: `Delete “${v.name}”?`, body: "Only the saved view goes; no documents are deleted.", confirmLabel: "Delete view", destructive: true }))) return;
    await api.del(`/saved-views/${v.id}`).then(refresh, (e) => toast.error(errorMessage(e)));
  };
  const rename = async (v: SavedView) => {
    if (name.trim() && name.trim() !== v.name) await patch(v, { name: name.trim() });
    setEditing(null);
  };
  return (
    <SettingsCard title="Saved views" description="Filters you saved from Documents. Pinned views appear in the sidebar, in this order.">
      {views.isLoading ? (
        <Skeleton className="h-16" />
      ) : list.length === 0 ? (
        <EmptyState icon={<Bookmark />} title="No saved views yet" className="py-8">
          Search or filter in Documents, then choose “Save view”.
        </EmptyState>
      ) : (
        <ul className="divide-y divide-border">
          {list.map((v, i) => (
            <li key={v.id} className="flex flex-wrap items-center gap-2 py-2.5">
              <div className="min-w-0 flex-1">
                {editing === v.id ? (
                  <Input value={name} autoFocus aria-label="View name" onChange={(e) => setName(e.target.value)} onBlur={() => rename(v)} onKeyDown={(e) => (e.key === "Enter" ? rename(v) : e.key === "Escape" && setEditing(null))} />
                ) : (
                  <>
                    <Link to="/views/$id" params={{ id: v.id }} className="text-sm font-medium hover:underline">
                      {v.name}
                    </Link>
                    {v.space_id && (
                      <Badge className="ml-2" tone="accent">
                        Shared in {spaceLabel(me.spaces.find((s) => s.id === v.space_id)) || "a space"}
                      </Badge>
                    )}
                  </>
                )}
              </div>
              {v.can_edit && (
                <>
                  <Button size="icon-sm" variant="ghost" aria-label="Move up" disabled={i === 0} onClick={() => move(i, -1)}>
                    <ArrowUp />
                  </Button>
                  <Button size="icon-sm" variant="ghost" aria-label="Move down" disabled={i === list.length - 1} onClick={() => move(i, 1)}>
                    <ArrowDown />
                  </Button>
                  <Button size="icon-sm" variant="ghost" aria-label={v.pinned ? "Unpin from sidebar" : "Pin to sidebar"} title={v.pinned ? "Pinned to the sidebar" : "Not in the sidebar"} onClick={() => patch(v, { pinned: !v.pinned })}>
                    {v.pinned ? <Pin className="text-accent" /> : <PinOff />}
                  </Button>
                  <Button size="icon-sm" variant="ghost" aria-label="Rename" onClick={() => (setEditing(v.id), setName(v.name))}>
                    <Pencil />
                  </Button>
                  <Button size="icon-sm" variant="ghost" aria-label="Delete" onClick={() => remove(v)}>
                    <Trash2 />
                  </Button>
                </>
              )}
            </li>
          ))}
        </ul>
      )}
    </SettingsCard>
  );
}
