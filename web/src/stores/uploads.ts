import { create } from "zustand";
import { ApiError, uploadDocument } from "@/lib/api";
import { invalidateDocuments, keys, queryClient } from "@/lib/queries";
import type { Document, Me } from "@/lib/types";

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
  /** Files waiting for the person to choose a space. */
  pending: { files: File[]; source?: string } | null;
  /** Start uploading, asking which space first when there is a choice. */
  request: (files: File[], source?: string) => void;
  choose: (spaceId: string, remember: boolean) => void;
  cancelChoice: () => void;
  retry: (id: string, allowDuplicate?: boolean) => void;
  remove: (id: string) => void;
  clearFinished: () => void;
}

const CONCURRENCY = 3;
let seq = 0;
const sources = new Map<string, string>();

const ASK_KEY = "docveta.uploadAsk"; // "0" once someone ticks "always upload here"
const LAST_KEY = "docveta.uploadSpace";
export const shouldAsk = () => {
  try {
    return localStorage.getItem(ASK_KEY) !== "0";
  } catch {
    return true;
  }
};

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
    pending: null,
    request: (files, source) => {
      const me = queryClient.getQueryData<Me>(keys.me);
      const writable = (me?.spaces ?? []).filter((s) => s.role !== "viewer");
      let last: string | null = null;
      try {
        last = localStorage.getItem(LAST_KEY);
      } catch {
        /* private mode */
      }
      if (writable.length <= 1 || (!shouldAsk() && writable.some((s) => s.id === last))) {
        get().add(files, writable.length <= 1 ? writable[0]?.id : last ?? undefined, source);
      } else set({ pending: { files, source } });
    },
    choose: (spaceId, remember) => {
      const p = get().pending;
      if (!p) return;
      try {
        localStorage.setItem(LAST_KEY, spaceId);
        localStorage.setItem(ASK_KEY, remember ? "0" : "1");
      } catch {
        /* private mode */
      }
      set({ pending: null });
      get().add(p.files, spaceId, p.source);
    },
    cancelChoice: () => set({ pending: null }),
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
