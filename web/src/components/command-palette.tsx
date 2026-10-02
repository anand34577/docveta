import * as React from "react";
import { Command } from "cmdk";
import { Dialog as D } from "radix-ui";
import { useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, FileText, Home, Inbox, Moon, Search, Settings, Sun, Trash2, Upload } from "lucide-react";
import { api } from "@/lib/api";
import { useSavedViews } from "@/lib/queries";
import { useUI } from "@/stores/ui";
import { useFilePicker } from "./app-shell";

interface Suggestion {
  id: string;
  title: string;
}

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = React.useState(value);
  React.useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

/** Ctrl/⌘+K: search documents as you type, jump anywhere, run actions. */
export function CommandPalette() {
  const { paletteOpen: open, setPaletteOpen: setOpen, setTheme, theme } = useUI();
  const [q, setQ] = React.useState("");
  const dq = useDebounced(q.trim(), 120);
  const navigate = useNavigate();
  const pick = useFilePicker();
  const views = useSavedViews();
  const results = useQuery({
    queryKey: ["suggest", dq],
    queryFn: ({ signal }) => api.get<{ items: Suggestion[] }>("/documents/suggest", { q: dq, limit: 8 }, signal).then((r) => r.items),
    enabled: open && dq.length > 0,
    staleTime: 10_000,
  });

  React.useEffect(() => {
    if (!open) setQ("");
  }, [open]);

  const go = (fn: () => void) => {
    setOpen(false);
    fn();
  };

  const item =
    "flex cursor-default items-center gap-3 rounded-md px-3 py-2.5 text-sm data-[selected=true]:bg-surface-2 [&_svg]:size-4 [&_svg]:text-muted";

  return (
    <D.Root open={open} onOpenChange={setOpen}>
      <D.Portal>
        <D.Overlay className="fixed inset-0 z-50 bg-overlay animate-in" />
        <D.Content
          className="fixed inset-x-2 top-2 z-50 mx-auto max-w-xl overflow-hidden rounded-xl border border-border bg-surface shadow-lg animate-pop sm:top-[14vh]"
          aria-describedby={undefined}
        >
          <D.Title className="sr-only">Search and commands</D.Title>
          <Command shouldFilter={false} loop>
            <div className="flex items-center gap-2 border-b border-border px-3">
              <Search className="size-4 text-subtle" />
              <Command.Input
                value={q}
                onValueChange={setQ}
                placeholder="Search documents or type a command…"
                className="h-12 flex-1 bg-transparent text-[15px] outline-none placeholder:text-subtle"
              />
            </div>
            <Command.List className="max-h-[60vh] overflow-y-auto scrollbar-thin p-1.5">
              {dq && (
                <Command.Group heading="Documents" className="[&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:text-subtle">
                  {(results.data ?? []).map((r) => (
                    <Command.Item key={r.id} value={`doc-${r.id}`} className={item} onSelect={() => go(() => navigate({ to: "/documents/$id", params: { id: r.id } }))}>
                      <FileText />
                      <span className="flex-1 truncate">{r.title}</span>
                    </Command.Item>
                  ))}
                  <Command.Item
                    value="search-all"
                    className={item}
                    onSelect={() => go(() => navigate({ to: "/documents", search: { q: dq } }))}
                  >
                    <Search />
                    <span className="flex-1">
                      Search everything for “<span className="font-medium">{dq}</span>”
                    </span>
                    <ArrowRight />
                  </Command.Item>
                </Command.Group>
              )}
              {!dq && (
                <>
                  <Command.Group heading="Go to" className="[&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:text-subtle">
                    <Command.Item value="home" className={item} onSelect={() => go(() => navigate({ to: "/" }))}>
                      <Home /> Home
                    </Command.Item>
                    <Command.Item value="inbox" className={item} onSelect={() => go(() => navigate({ to: "/inbox" }))}>
                      <Inbox /> Inbox
                    </Command.Item>
                    <Command.Item value="documents" className={item} onSelect={() => go(() => navigate({ to: "/documents" }))}>
                      <FileText /> All documents
                    </Command.Item>
                    {(views.data ?? []).map((v) => (
                      <Command.Item key={v.id} value={`view-${v.id}`} className={item} onSelect={() => go(() => navigate({ to: "/views/$id", params: { id: v.id } }))}>
                        <FileText /> {v.name}
                      </Command.Item>
                    ))}
                    <Command.Item value="trash" className={item} onSelect={() => go(() => navigate({ to: "/trash" }))}>
                      <Trash2 /> Trash
                    </Command.Item>
                  </Command.Group>
                  <Command.Group heading="Actions" className="[&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:text-subtle">
                    <Command.Item value="upload" className={item} onSelect={() => go(() => pick())}>
                      <Upload /> Upload documents
                    </Command.Item>
                    <Command.Item
                      value="theme"
                      className={item}
                      onSelect={() => go(() => setTheme(theme === "dark" ? "light" : "dark"))}
                    >
                      {theme === "dark" ? <Sun /> : <Moon />} Switch to {theme === "dark" ? "light" : "dark"} theme
                    </Command.Item>
                    <Command.Item value="settings" className={item} onSelect={() => go(() => navigate({ to: "/settings/$section", params: { section: "profile" } }))}>
                      <Settings /> Settings
                    </Command.Item>
                  </Command.Group>
                </>
              )}
            </Command.List>
            <div className="hidden items-center gap-4 border-t border-border px-3 py-2 text-xs text-subtle sm:flex">
              <span>↑↓ to navigate</span>
              <span>↵ to open</span>
              <span>Esc to close</span>
              <span className="ml-auto">Tip: try <code className="text-muted">tag:tax from:hdfc date:2025</code></span>
            </div>
          </Command>
        </D.Content>
      </D.Portal>
    </D.Root>
  );
}
