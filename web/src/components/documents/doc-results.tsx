import * as React from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useDocuments } from "@/lib/queries";
import type { DocQuery, Document } from "@/lib/types";
import { useSelection } from "@/stores/ui";
import { Skeleton, Spinner } from "@/components/ui/misc";
import { DocumentCard, DocumentRow } from "./doc-items";

interface Props {
  query: DocQuery;
  layout: "grid" | "list";
  empty: React.ReactNode;
  onLoaded?: (info: { total?: number; items: Document[] }) => void;
}

/** Width of an element that may mount later (the results list appears after loading). */
function useElementWidth(el: HTMLElement | null) {
  const [w, setW] = React.useState(0);
  React.useLayoutEffect(() => {
    if (!el) return;
    const ro = new ResizeObserver((entries) => setW(entries[0].contentRect.width));
    ro.observe(el);
    setW(el.getBoundingClientRect().width);
    return () => ro.disconnect();
  }, [el]);
  return w;
}

/**
 * Virtualized, infinitely scrolling document results. Only visible rows are rendered,
 * so 10k+ documents scroll smoothly on phones.
 */
export function DocumentResults({ query, layout, empty, onLoaded }: Props) {
  const res = useDocuments(query, { refetchWhileProcessing: true });
  const items = React.useMemo(() => res.data?.pages.flatMap((p) => p.items) ?? [], [res.data]);
  const total = res.data?.pages[0]?.total;
  const { ids, toggle, set } = useSelection();
  const selecting = ids.size > 0;
  const lastClicked = React.useRef<number | null>(null);

  React.useEffect(() => {
    onLoaded?.({ total, items });
  }, [total, items, onLoaded]);

  const listRef = React.useRef<HTMLDivElement | null>(null);
  const [listEl, setListEl] = React.useState<HTMLDivElement | null>(null);
  const setList = React.useCallback((el: HTMLDivElement | null) => {
    listRef.current = el;
    setListEl(el);
  }, []);
  const width = useElementWidth(listEl);
  const cols = layout === "grid" ? Math.max(2, Math.floor((width + 16) / 216)) : 1;
  const rows = Math.ceil(items.length / cols);
  const scrollEl = React.useCallback(() => document.getElementById("main"), []);
  const [margin, setMargin] = React.useState(0);
  React.useLayoutEffect(() => {
    if (listRef.current) setMargin(listRef.current.offsetTop);
  });

  const virtualizer = useVirtualizer({
    count: rows,
    getScrollElement: scrollEl,
    estimateSize: () => (layout === "grid" ? (width / cols) * 1.42 + 16 : 68),
    overscan: 4,
    scrollMargin: margin,
  });
  const vItems = virtualizer.getVirtualItems();

  React.useEffect(() => {
    const last = vItems[vItems.length - 1];
    if (last && last.index >= rows - 3 && res.hasNextPage && !res.isFetchingNextPage) void res.fetchNextPage();
  }, [vItems, rows, res]);

  React.useEffect(() => {
    virtualizer.measure();
  }, [layout, cols, virtualizer]);

  const onToggle = (index: number) => (e: React.MouseEvent | React.KeyboardEvent) => {
    e.stopPropagation();
    if (e.shiftKey && lastClicked.current !== null) {
      const [a, b] = [Math.min(lastClicked.current, index), Math.max(lastClicked.current, index)];
      set([...new Set([...ids, ...items.slice(a, b + 1).map((d) => d.id)])]);
    } else {
      toggle(items[index].id);
    }
    lastClicked.current = index;
  };

  if (res.isLoading) {
    return layout === "grid" ? (
      <div className="grid grid-cols-[repeat(auto-fill,minmax(180px,1fr))] gap-4 px-4 sm:px-6">
        {Array.from({ length: 12 }).map((_, i) => (
          <Skeleton key={i} className="aspect-[3/4.2] rounded-xl" />
        ))}
      </div>
    ) : (
      <div className="space-y-2 px-4 sm:px-6">
        {Array.from({ length: 10 }).map((_, i) => (
          <Skeleton key={i} className="h-14" />
        ))}
      </div>
    );
  }
  if (res.isError) {
    return <div className="px-6 py-10 text-center text-sm text-danger">{(res.error as Error).message}</div>;
  }
  if (items.length === 0) return <>{empty}</>;

  return (
    <div ref={setList} className={layout === "grid" ? "px-4 sm:px-6" : "border-t border-border"}>
      <div style={{ height: virtualizer.getTotalSize(), position: "relative" }}>
        {vItems.map((row) => {
          const start = row.index * cols;
          const rowItems = items.slice(start, start + cols);
          return (
            <div
              key={row.key}
              data-index={row.index}
              ref={virtualizer.measureElement}
              style={{ position: "absolute", top: 0, left: 0, width: "100%", transform: `translateY(${row.start - margin}px)` }}
            >
              {layout === "grid" ? (
                <div className="grid gap-4 pb-4" style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}>
                  {rowItems.map((d, i) => (
                    <DocumentCard key={d.id} doc={d} selected={ids.has(d.id)} selecting={selecting} onToggle={onToggle(start + i)} query={query.q} />
                  ))}
                </div>
              ) : (
                rowItems.map((d, i) => (
                  <DocumentRow key={d.id} doc={d} selected={ids.has(d.id)} selecting={selecting} onToggle={onToggle(start + i)} query={query.q} />
                ))
              )}
            </div>
          );
        })}
      </div>
      {res.isFetchingNextPage && (
        <div className="flex justify-center py-6">
          <Spinner />
        </div>
      )}
    </div>
  );
}
