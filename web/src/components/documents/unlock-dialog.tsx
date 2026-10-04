import * as React from "react";
import { useQueryClient } from "@tanstack/react-query";
import { LockKeyhole } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { invalidateDocuments } from "@/lib/queries";
import type { Document } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Field, Input } from "@/components/ui/input";
import { Dialog, DialogContent, DialogFooter } from "@/components/ui/overlay";

/** Opens a password-protected PDF and keeps an unlocked copy as a new version. The password is never stored. */
export function UnlockDialog({ doc, onClose }: { doc: Document; onClose: () => void }) {
  const qc = useQueryClient();
  const [password, setPassword] = React.useState("");
  const [err, setErr] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr("");
    try {
      await api.post(`/documents/${doc.id}/unlock`, { password });
      invalidateDocuments(qc, doc.id);
      toast.success("Unlocked. Reading the text now.");
      onClose();
    } catch (e) {
      setErr(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent size="sm" title="Unlock this PDF" description="Docveta keeps an unlocked copy so it can read and search it. The original stays as version 1, and the password is not saved.">
        <form onSubmit={submit} className="space-y-4">
          <Field label="PDF password" htmlFor="unlock-pw" error={err}>
            <Input id="unlock-pw" type="password" autoComplete="off" value={password} onChange={(e) => setPassword(e.target.value)} autoFocus required />
          </Field>
          <DialogFooter>
            <Button onClick={onClose}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy}>
              <LockKeyhole /> Unlock
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
