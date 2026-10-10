import * as React from "react";
import { Command } from "cmdk";
import { Dialog as D } from "radix-ui";
import { useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { recentDocs, type RecentDoc } from "@/lib/recent";
import { ArrowRight, Bell, Bookmark, FileText, FolderOpen, History, Home, Inbox, MessageSquareText, Moon, Search, Settings, Shield, Sun, Tag, Trash2, Upload } from "lucide-react";
import { api } from "@/lib/api";
import { useAIEnabled, useSavedViews, useTaxonomy } from "@/lib/queries";
import { useCurrentUser } from "./app-shell";
import { spaceLabel } from "@/lib/utils";
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
  const me = useCurrentUser();
  const tags = useTaxonomy("tags");
  const ai = useAIEnabled().data;
  const needle = dq.toLowerCase();
  const spaceHits = needle ? me.spaces.filter((s) => spaceLabel(s).toLowerCase().includes(needle)).slice(0, 4) : [];
  const tagHits = needle ? [...new Map((tags.data ?? []).filter((t) => t.name.toLowerCase().includes(needle)).map((t) => [t.name.toLowerCase(), t])).values()].slice(0, 5) : [];
  const results = useQuery({
    queryKey: ["suggest", dq],
    queryFn: ({ signal }) => api.get<{ items: Suggestion[] }>("/documents/suggest", { q: dq, limit: 8 }, signal).then((r) => r.items),
    enabled: open && dq.length > 0,
    staleTime: 10_000,
  });

  const [recent, setRecent] = React.useState<RecentDoc[]>([]);
  React.useEffect(() => {
    if (!open) setQ("");
    else setRecent(recentDocs().slice(0, 4));
  }, [open]);

  const go = (fn: () => void) => {
    setOpen(false);
    fn();
  };

  type Cmd = { id: string; label: string; icon: React.ReactNode; run: () => void };
  const nav: Cmd[] = [
    { id: "home", label: "Home", icon: <Home />, run: () => navigate({ to: "/" }) },
    { id: "inbox", label: "Inbox", icon: <Inbox />, run: () => navigate({ to: "/inbox" }) },
    { id: "documents", label: "All documents", icon: <FileText />, run: () => navigate({ to: "/documents" }) },
    ...(ai?.chat ? [{ id: "ask", label: "Ask your documents", icon: <MessageSquareText />, run: () => navigate({ to: "/ask" }) }] : []),
    ...(views.data ?? []).map((v) => ({ id: `view-${v.id}`, label: v.name, icon: <Bookmark />, run: () => navigate({ to: "/views/$id", params: { id: v.id } }) })),
    ...me.spaces.map((s) => ({ id: `space-${s.id}`, label: spaceLabel(s), icon: <FolderOpen />, run: () => navigate({ to: "/documents", search: { space_id: [s.id] } }) })),
    { id: "notifications", label: "Notifications", icon: <Bell />, run: () => navigate({ to: "/notifications" }) },
    { id: "trash", label: "Trash", icon: <Trash2 />, run: () => navigate({ to: "/trash" }) },
  ];
  const dark = theme === "dark" || (theme === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);
  const actions: Cmd[] = [
    { id: "upload", label: "Upload documents", icon: <Upload />, run: () => pick() },
    { id: "theme", label: `Switch to ${dark ? "light" : "dark"} theme`, icon: dark ? <Sun /> : <Moon />, run: () => setTheme(dark ? "light" : "dark") },
    { id: "settings", label: "Settings", icon: <Settings />, run: () => navigate({ to: "/settings/$section", params: { section: "profile" } }) },
    ...(me.is_admin ? [{ id: "admin", label: "Administration", icon: <Shield />, run: () => navigate({ to: "/admin/$section", params: { section: "users" } }) }] : []),
  ];
  // Typing filters the commands too (spaces already show as their own matches above them).
  const cmdHits = needle ? [...nav, ...actions].filter((c) => !c.id.startsWith("space-") && c.label.toLowerCase().includes(needle)) : [];
  // Results arrive after the debounce, so keep the top one highlighted for Enter.
  const top = results.data?.[0];
  const first = !dq ? (recent[0] ? `recent-${recent[0].id}` : "home") : cmdHits[0] ? `cmd-${cmdHits[0].id}` : top ? `doc-${top.id}` : spaceHits[0] ? `space-${spaceHits[0].id}` : tagHits[0] ? `tag-${tagHits[0].id}` : "search-all";
  const [selected, setSelected] = React.useState(first);
  React.useEffect(() => setSelected(first), [first]);

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
          <Command shouldFilter={false} loop value={selected} onValueChange={setSelected}>
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
              {cmdHits.length > 0 && (
                <Command.Group heading="Commands" className="[&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:text-subtle">
                  {cmdHits.map((c) => (
                    <Command.Item key={c.id} value={`cmd-${c.id}`} className={item} onSelect={() => go(c.run)}>
                      {c.icon} {c.label}
                    </Command.Item>
                  ))}
                </Command.Group>
              )}
              {dq && (
                <Command.Group heading="Documents" className="[&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:text-subtle">
                  {(results.data ?? []).map((r) => (
                    <Command.Item key={r.id} value={`doc-${r.id}`} className={item} onSelect={() => go(() => navigate({ to: "/documents/$id", params: { id: r.id } }))}>
                      <FileText />
                      <span className="flex-1 truncate">{r.title}</span>
                    </Command.Item>
                  ))}
                  {spaceHits.map((s) => (
                    <Command.Item key={s.id} value={`space-${s.id}`} className={item} onSelect={() => go(() => navigate({ to: "/documents", search: { space_id: [s.id] } }))}>
                      <FolderOpen />
                      <span className="flex-1 truncate">Open space {spaceLabel(s)}</span>
                    </Command.Item>
                  ))}
                  {tagHits.map((t) => (
                    <Command.Item key={t.id} value={`tag-${t.id}`} className={item} onSelect={() => go(() => navigate({ to: "/documents", search: { tag_id: [t.id] } }))}>
                      <Tag />
                      <span className="flex-1 truncate">Documents tagged {t.name}</span>
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
              {!dq && recent.length > 0 && (
                <Command.Group heading="Recently opened" className="[&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:text-subtle">
                  {recent.map((r) => (
                    <Command.Item key={r.id} value={`recent-${r.id}`} className={item} onSelect={() => go(() => navigate({ to: "/documents/$id", params: { id: r.id } }))}>
                      <History />
                      <span className="flex-1 truncate">{r.title}</span>
                    </Command.Item>
                  ))}
                </Command.Group>
              )}
              {!dq &&
                ([["Go to", nav], ["Actions", actions]] as const).map(([heading, list]) => (
                  <Command.Group key={heading} heading={heading} className="[&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:text-subtle">
                    {list.map((c) => (
                      <Command.Item key={c.id} value={c.id} className={item} onSelect={() => go(c.run)}>
                        {c.icon} {c.label}
                      </Command.Item>
                    ))}
                  </Command.Group>
                ))}
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
