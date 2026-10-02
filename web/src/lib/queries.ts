import { keepPreviousData, QueryClient, useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./api";
import type {
  Channel,
  DirectoryEntry,
  DocQuery,
  Document,
  DocumentList,
  HistoryEntry,
  Me,
  Note,
  Notification,
  Page,
  SavedView,
  Stats,
  Status,
  TaxonomyItem,
  TaxonomyKind,
} from "./types";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      retry: (count, err) => {
        const status = (err as { status?: number }).status ?? 0;
        return status >= 500 && count < 2;
      },
      refetchOnWindowFocus: true,
    },
  },
});

export const keys = {
  status: ["status"] as const,
  me: ["me"] as const,
  taxonomy: (kind: TaxonomyKind, spaceId?: string) => ["taxonomy", kind, spaceId ?? "all"] as const,
  documents: (q: DocQuery) => ["documents", q] as const,
  document: (id: string) => ["document", id] as const,
  notes: (id: string) => ["document", id, "notes"] as const,
  history: (id: string) => ["document", id, "history"] as const,
  pages: (id: string) => ["document", id, "pages"] as const,
  stats: ["stats"] as const,
  views: ["saved-views"] as const,
  notifications: ["notifications"] as const,
  directory: ["directory"] as const,
  channels: ["channels"] as const,
};

export function useStatus() {
  return useQuery({ queryKey: keys.status, queryFn: () => api.get<Status>("/status"), staleTime: 60_000 });
}

export function useMe(enabled = true) {
  return useQuery({ queryKey: keys.me, queryFn: () => api.get<Me>("/me"), enabled, staleTime: 60_000, retry: false });
}

export function useTaxonomy(kind: TaxonomyKind, spaceId?: string) {
  return useQuery({
    queryKey: keys.taxonomy(kind, spaceId),
    queryFn: () => api.get<{ items: TaxonomyItem[] }>(`/${kind}`, { space_id: spaceId }).then((r) => r.items),
    staleTime: 60_000,
  });
}

/** Turns UI filters into API query params. */
export function docParams(q: DocQuery, cursor?: string | null) {
  return {
    q: q.q,
    space_id: q.space_id,
    tag_id: q.tag_id,
    correspondent_id: q.correspondent_id,
    document_type_id: q.document_type_id,
    date_from: q.date_from,
    date_to: q.date_to,
    inbox: q.inbox === undefined ? undefined : String(q.inbox),
    status: q.status,
    untagged: q.untagged,
    trash: q.trash,
    sort: q.sort,
    cursor: cursor ?? undefined,
    limit: 60,
  };
}

export function useDocuments(q: DocQuery, opts: { refetchWhileProcessing?: boolean } = {}) {
  return useInfiniteQuery({
    queryKey: keys.documents(q),
    queryFn: ({ pageParam, signal }) => api.get<DocumentList>("/documents", docParams(q, pageParam), signal),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.next_cursor,
    placeholderData: keepPreviousData,
    refetchInterval: (query) => {
      if (!opts.refetchWhileProcessing) return false;
      const pages = query.state.data?.pages ?? [];
      return pages.some((p) => p.items.some((d) => d.status === "processing")) ? 4000 : false;
    },
  });
}

export function useDocument(id: string) {
  return useQuery({
    queryKey: keys.document(id),
    queryFn: () => api.get<Document>(`/documents/${id}`),
    refetchInterval: (q) => (q.state.data?.status === "processing" ? 3000 : false),
  });
}

export function useNotes(id: string) {
  return useQuery({ queryKey: keys.notes(id), queryFn: () => api.get<{ items: Note[] }>(`/documents/${id}/notes`).then((r) => r.items) });
}

export function useHistory(id: string, enabled: boolean) {
  return useQuery({
    queryKey: keys.history(id),
    queryFn: () => api.get<{ items: HistoryEntry[] }>(`/documents/${id}/history`).then((r) => r.items),
    enabled,
  });
}

export function usePages(id: string, enabled: boolean) {
  return useQuery({
    queryKey: keys.pages(id),
    queryFn: () => api.get<{ items: Page[] }>(`/documents/${id}/pages`).then((r) => r.items),
    enabled,
  });
}

export function useStats() {
  return useQuery({
    queryKey: keys.stats,
    queryFn: () => api.get<Stats>("/documents/stats"),
    refetchInterval: (q) => ((q.state.data?.processing ?? 0) > 0 ? 5000 : false),
  });
}

export function useSavedViews() {
  return useQuery({ queryKey: keys.views, queryFn: () => api.get<{ items: SavedView[] }>("/saved-views").then((r) => r.items) });
}

export function useNotifications() {
  return useQuery({
    queryKey: keys.notifications,
    queryFn: () => api.get<{ items: Notification[]; unread: number }>("/notifications", { limit: 30 }),
    refetchInterval: 60_000,
  });
}

export function useDirectory() {
  return useQuery({
    queryKey: keys.directory,
    queryFn: () => api.get<{ items: DirectoryEntry[] }>("/users/directory").then((r) => r.items),
    staleTime: 5 * 60_000,
  });
}

export function useChannels() {
  return useQuery({
    queryKey: keys.channels,
    queryFn: () => api.get<{ items: Channel[]; event_types: string[] }>("/notification-channels"),
  });
}

/** Invalidate everything that lists documents after a change. */
export function invalidateDocuments(qc: QueryClient, id?: string) {
  qc.invalidateQueries({ queryKey: ["documents"] });
  qc.invalidateQueries({ queryKey: keys.stats });
  if (id) qc.invalidateQueries({ queryKey: keys.document(id) });
}

export type DocPatch = Partial<{
  title: string;
  document_date: string | null;
  correspondent_id: string | null;
  document_type_id: string | null;
  tag_ids: string[];
  add_tag_ids: string[];
  remove_tag_ids: string[];
  language: string;
  asn: number | null;
  physical_location: string;
  inbox: boolean;
  space_id: string;
}>;

export function useUpdateDocument(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ patch, version }: { patch: DocPatch; version?: number }) =>
      api.patch<Document>(`/documents/${id}`, patch, version !== undefined ? { "If-Match": `"${version}"` } : undefined),
    onSuccess: (doc) => {
      qc.setQueryData(keys.document(id), doc);
      qc.invalidateQueries({ queryKey: ["documents"] });
      qc.invalidateQueries({ queryKey: keys.stats });
      qc.invalidateQueries({ queryKey: keys.history(id) });
    },
  });
}

export function useCreateTaxonomy(kind: TaxonomyKind) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { space_id: string; name: string; color?: string }) => api.post<TaxonomyItem>(`/${kind}`, body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["taxonomy", kind] }),
  });
}
