import * as React from "react";
import { useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Check } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { keys } from "@/lib/queries";
import type { Space } from "@/lib/types";
import { cn, colorNames, tagDot } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Field, Input, Textarea } from "@/components/ui/input";
import { Dialog, DialogContent, DialogFooter } from "@/components/ui/overlay";

/** Create a shared space (for a household, a business, a project). Members are added afterwards. */
export function NewSpaceDialog({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [name, setName] = React.useState("");
  const [description, setDescription] = React.useState("");
  const [color, setColor] = React.useState("indigo");
  const [err, setErr] = React.useState<ApiError | null>(null);
  const [busy, setBusy] = React.useState(false);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const s = await api.post<Space>("/spaces", { name: name.trim(), description, color });
      await qc.invalidateQueries({ queryKey: keys.me });
      onClose();
      navigate({ to: "/spaces/$id/$section", params: { id: s.id, section: "members" } });
    } catch (e) {
      setErr(e as ApiError);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent size="sm" title="New shared space" description="A space keeps documents together, for example for your family or a project. You choose who can see it.">
        <form onSubmit={submit} className="space-y-4">
          <Field label="Name" htmlFor="ns-name" error={err?.fieldError("name") ?? (err && !err.fields.length ? err.message : undefined)}>
            <Input id="ns-name" required value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Family, Acme Traders" autoFocus />
          </Field>
          <Field label="Description (optional)" htmlFor="ns-desc">
            <Textarea id="ns-desc" rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />
          </Field>
          <Field label="Colour">
            <div className="flex flex-wrap gap-2">
              {colorNames.map((c) => (
                <button
                  key={c}
                  type="button"
                  aria-label={c}
                  aria-pressed={color === c}
                  onClick={() => setColor(c)}
                  className={cn("flex size-7 items-center justify-center rounded-full text-white ring-offset-2 ring-offset-surface", tagDot[c], color === c && "ring-2 ring-fg")}
                >
                  {color === c && <Check className="size-4" />}
                </button>
              ))}
            </div>
          </Field>
          <DialogFooter>
            <Button onClick={onClose}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy} disabled={!name.trim()}>
              Create space
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
