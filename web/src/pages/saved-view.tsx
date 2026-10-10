import * as React from "react";
import { useNavigate, useParams } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { MoreHorizontal, Pencil, Save, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { keys, useSavedViews } from "@/lib/queries";
import type { DocQuery } from "@/lib/types";
import { DocumentBrowser } from "./documents";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/overlay";
import { confirm, prompt } from "@/components/ui/confirm";
import { Spinner } from "@/components/ui/misc";
import { NotFound } from "./not-found";

export function SavedViewPage() {
  const { id } = useParams({ from: "/app/views/$id" });
  const views = useSavedViews();
  const view = views.data?.find((v) => v.id === id);
  const [query, setQuery] = React.useState<DocQuery | null>(null);
  const qc = useQueryClient();
  const navigate = useNavigate();

  React.useEffect(() => {
    if (view) setQuery(view.query ?? {});
  }, [view]);

  if (views.isLoading) return <div className="flex justify-center py-20"><Spinner /></div>;
  if (!view) return <NotFound />;
  if (!query) return null;

  const dirty = JSON.stringify(query) !== JSON.stringify(view.query ?? {});
  const saveQuery = async () => {
    try {
      await api.patch(`/saved-views/${view.id}`, { query });
      qc.invalidateQueries({ queryKey: keys.views });
      toast.success("View updated");
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };
  const rename = async () => {
    const name = await prompt({ title: "Rename view", defaultValue: view.name, confirmLabel: "Rename" });
    if (!name || name === view.name) return;
    try {
      await api.patch(`/saved-views/${view.id}`, { name });
      qc.invalidateQueries({ queryKey: keys.views });
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };
  const remove = async () => {
    if (!(await confirm({ title: `Delete “${view.name}”?`, body: "Only the saved view is deleted, not the documents.", confirmLabel: "Delete view", destructive: true }))) return;
    try {
      await api.del(`/saved-views/${view.id}`);
    } catch (e) {
      toast.error(errorMessage(e)); // still there: stay on it
      return;
    }
    qc.invalidateQueries({ queryKey: keys.views });
    navigate({ to: "/documents" });
  };

  return (
    <DocumentBrowser
      query={query}
      onChange={setQuery}
      title={view.name}
      headerActions={
        view.can_edit && (
          <>
            {dirty && (
              <Button size="sm" variant="soft" onClick={saveQuery}>
                <Save /> Save changes
              </Button>
            )}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button size="icon-sm" variant="ghost" aria-label="View options">
                  <MoreHorizontal />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent>
                <DropdownMenuItem onSelect={rename}>
                  <Pencil /> Rename
                </DropdownMenuItem>
                <DropdownMenuItem destructive onSelect={remove}>
                  <Trash2 /> Delete view
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </>
        )
      }
    />
  );
}
