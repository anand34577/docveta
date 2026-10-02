import { create } from "zustand";
import { ApiError, uploadDocument } from "@/lib/api";
import { invalidateDocuments, queryClient } from "@/lib/queries";
import type { Document } from "@/lib/types";

export type UploadStatus = "queued" | "uploading" | "done" | "duplicate" | "error";

export interface UploadItem {
  id: string;
  file: File;
  spaceId?: string;
  status: UploadStatus;
  progress: number;
  error?: string;
  doc?: Document;
  duplicateOf?: { id: string; title: string };
  controller?: AbortController;
}

interface UploadState {
  items: UploadItem[];
  add: (files: File[], spaceId?: string, source?: string) => void;
  retry: (id: string, allowDuplicate?: boolean) => void;
  remove: (id: string) => void;
  clearFinished: () => void;
}

const CONCURRENCY = 3;
let seq = 0;
const sources = new Map<string, string>();

export const useUploads = create<UploadState>((set, get) => {
  const update = (id: string, patch: Partial<UploadItem>) =>
    set((s) => ({ items: s.items.map((i) => (i.id === id ? { ...i, ...patch } : i)) }));

  const pump = () => {
    const { items } = get();
    const active = items.filter((i) => i.status === "uploading").length;
    const next = items.filter((i) => i.status === "queued").slice(0, Math.max(0, CONCURRENCY - active));
    next.forEach((item) => void run(item));
  };

  const run = async (item: UploadItem, allowDuplicate = false) => {
    const controller = new AbortController();
    update(item.id, { status: "uploading", progress: 0, error: undefined, controller });
    try {
      const doc = await uploadDocument<Document>({
        file: item.file,
        spaceId: item.spaceId,
        allowDuplicate,
        source: sources.get(item.id),
        signal: controller.signal,
        onProgress: (p) => update(item.id, { progress: p }),
      });
      update(item.id, { status: "done", progress: 1, doc, controller: undefined });
      invalidateDocuments(queryClient);
    } catch (e) {
      if ((e as Error).name === "AbortError") {
        set((s) => ({ items: s.items.filter((i) => i.id !== item.id) }));
      } else if (e instanceof ApiError && e.code === "duplicate_document") {
        update(item.id, {
          status: "duplicate",
          error: e.message,
          duplicateOf: { id: String(e.extra.document_id), title: String(e.extra.title) },
          controller: undefined,
        });
      } else {
        update(item.id, { status: "error", error: (e as Error).message, controller: undefined });
      }
    } finally {
      pump();
    }
  };

  return {
    items: [],
    add: (files, spaceId, source) => {
      const items = files.map<UploadItem>((file) => {
        const id = `u${++seq}`;
        if (source) sources.set(id, source);
        return { id, file, spaceId, status: "queued", progress: 0 };
      });
      set((s) => ({ items: [...s.items, ...items] }));
      pump();
    },
    retry: (id, allowDuplicate) => {
      const item = get().items.find((i) => i.id === id);
      if (item) void run(item, allowDuplicate);
    },
    remove: (id) => {
      const item = get().items.find((i) => i.id === id);
      item?.controller?.abort();
      set((s) => ({ items: s.items.filter((i) => i.id !== id) }));
    },
    clearFinished: () => set((s) => ({ items: s.items.filter((i) => i.status === "queued" || i.status === "uploading") })),
  };
});
