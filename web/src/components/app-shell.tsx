import * as React from "react";
import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import {
  Bookmark,
  ChevronDown,
  FileText,
  Home,
  Inbox,
  LogOut,
  Monitor,
  Moon,
  Plus,
  Search,
  Settings,
  Shield,
  Sun,
  Trash2,
  Upload,
  Users,
} from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { api, setUnauthorizedHandler } from "@/lib/api";
import { useMe, useSavedViews, useStats, useStatus } from "@/lib/queries";
import { cn, modKey, setDateFormat } from "@/lib/utils";
import { applyTheme, useUI } from "@/stores/ui";
import { useUploads } from "@/stores/uploads";
import { Button } from "@/components/ui/button";
import { Avatar, Kbd, Spinner } from "@/components/ui/misc";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/overlay";
import { CommandPalette } from "./command-palette";
import { DropOverlay, UploadTray } from "./upload-tray";
import { NotificationsButton, useLiveEvents } from "./notifications";
import type { Me } from "@/lib/types";

const MeContext = React.createContext<Me | null>(null);

/** The signed-in user. Only usable inside the app layout. */
export function useCurrentUser(): Me {
  const me = React.useContext(MeContext);
  if (!me) throw new Error("useCurrentUser outside AppLayout");
  return me;
}

/** Auth gate + shell for all signed-in pages. */
export function AppLayout() {
  const navigate = useNavigate();
  const status = useStatus();
  const me = useMe(status.data ? !status.data.setup_needed : false);
  const location = useRouterState({ select: (s) => s.location });

  React.useEffect(() => {
    setUnauthorizedHandler(() => {
      const here = window.location.pathname + window.location.search;
      if (!window.location.pathname.startsWith("/login")) navigate({ to: "/login", search: { redirect: here } });
    });
  }, [navigate]);

  React.useEffect(() => {
    if (status.data?.setup_needed) navigate({ to: "/setup" });
  }, [status.data, navigate]);

  React.useEffect(() => {
    if (me.error && (me.error as { status?: number }).status === 401) {
      navigate({ to: "/login", search: { redirect: location.href !== "/" ? location.href : undefined } });
    }
  }, [me.error, navigate, location.href]);

  React.useEffect(() => {
    if (me.data) setDateFormat(me.data.date_format);
  }, [me.data]);

  if (status.isLoading || me.isLoading || !me.data) {
    return (
      <div className="flex h-dvh items-center justify-center">
        {me.error && (me.error as { status?: number }).status !== 401 ? (
          <div className="text-center">
            <p className="text-sm text-muted">Couldn't reach Docveta.</p>
            <Button className="mt-3" onClick={() => me.refetch()}>
              Try again
            </Button>
          </div>
        ) : (
          <Spinner />
        )}
      </div>
    );
  }
  return (
    <MeContext.Provider value={me.data}>
      <Shell />
    </MeContext.Provider>
  );
}

/** Uploads files shared to the installed PWA from Android's share sheet (see public/sw.js). */
function useSharedFiles() {
  const add = useUploads((s) => s.add);
  const me = useCurrentUser();
  const spaceId = useUI((s) => s.uploadSpaceId);
  const navigate = useNavigate();
  React.useEffect(() => {
    if (!new URLSearchParams(window.location.search).has("shared") || !("caches" in window)) return;
    (async () => {
      const cache = await caches.open("docveta-share-v1");
      const reqs = await cache.keys();
      const files: File[] = [];
      for (const req of reqs) {
        const res = await cache.match(req);
        if (!res) continue;
        const blob = await res.blob();
        const name = decodeURIComponent(res.headers.get("X-Filename") || "shared");
        files.push(new File([blob], name, { type: blob.type }));
        await cache.delete(req);
      }
      const target = me.spaces.find((s) => s.id === spaceId && s.role !== "viewer") ?? me.spaces.find((s) => s.role !== "viewer");
      if (files.length && target) add(files, target.id, "share");
      navigate({ to: "/inbox", replace: true });
    })().catch(() => undefined);
  }, []);
}

function Shell() {
  const setPaletteOpen = useUI((s) => s.setPaletteOpen);
  useLiveEvents();
  useSharedFiles();

  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPaletteOpen(true);
      }
      const t = e.target as HTMLElement;
      if (e.key === "/" && !["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName) && !t.isContentEditable) {
        e.preventDefault();
        setPaletteOpen(true);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [setPaletteOpen]);

  return (
    <div className="flex h-dvh overflow-hidden">
      <a href="#main" className="sr-only focus:not-sr-only focus:fixed focus:left-2 focus:top-2 focus:z-50 focus:rounded-md focus:bg-surface focus:px-3 focus:py-2">
        Skip to content
      </a>
      <Sidebar />
      <div className="flex min-w-0 flex-1 flex-col">
        <TopBar />
        <main id="main" className="relative flex-1 overflow-y-auto scrollbar-thin pb-20 lg:pb-0">
          <Outlet />
        </main>
      </div>
      <BottomNav />
      <CommandPalette />
      <UploadTray />
      <DropOverlay />
    </div>
  );
}

/* ---------------------------------------------------------------- Upload button (file picker) */

export function useFilePicker() {
  const add = useUploads((s) => s.add);
  const spaceId = useUI((s) => s.uploadSpaceId);
  const me = useCurrentUser();
  return React.useCallback(
    (accept = "application/pdf,image/*,.heic,.heif,.txt,.tif,.tiff") => {
      const input = document.createElement("input");
      input.type = "file";
      input.multiple = true;
      input.accept = accept;
      input.onchange = () => {
        const files = Array.from(input.files ?? []);
        const target = me.spaces.find((s) => s.id === spaceId && s.role !== "viewer") ?? me.spaces.find((s) => s.role !== "viewer");
        if (files.length) add(files, target?.id);
      };
      input.click();
    },
    [add, spaceId, me.spaces],
  );
}

/* ---------------------------------------------------------------- Sidebar (desktop) */

function NavItem({ to, icon, label, count, exact }: { to: string; icon: React.ReactNode; label: string; count?: number; exact?: boolean }) {
  return (
    <Link
      to={to}
      activeOptions={{ exact: exact ?? false, includeSearch: false }}
      className="group flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm text-muted transition-colors hover:bg-surface-2 hover:text-fg data-[status=active]:bg-surface-2 data-[status=active]:font-medium data-[status=active]:text-fg [&_svg]:size-[18px]"
    >
      {icon}
      <span className="flex-1 truncate">{label}</span>
      {!!count && <span className="rounded-full bg-accent-soft px-1.5 text-xs font-semibold text-accent-soft-fg">{count}</span>}
    </Link>
  );
}

function Sidebar() {
  const me = useCurrentUser();
  const stats = useStats();
  const views = useSavedViews();
  const pinned = (views.data ?? []).filter((v) => v.pinned);
  return (
    <aside className="hidden w-60 shrink-0 flex-col border-r border-border bg-surface lg:flex">
      <div className="flex h-14 items-center gap-2 px-4">
        <Logo />
      </div>
      <nav className="flex-1 space-y-0.5 overflow-y-auto scrollbar-thin px-2 pb-4" aria-label="Main">
        <NavItem to="/" exact icon={<Home />} label="Home" />
        <NavItem to="/inbox" icon={<Inbox />} label="Inbox" count={stats.data?.inbox} />
        <NavItem to="/documents" icon={<FileText />} label="All documents" />

        {pinned.length > 0 && <SectionLabel>Saved views</SectionLabel>}
        {pinned.map((v) => (
          <NavItem key={v.id} to={`/views/${v.id}`} icon={<Bookmark />} label={v.name} />
        ))}

        <SectionLabel>Spaces</SectionLabel>
        {me.spaces.map((s) => (
          <SpaceLink key={s.id} id={s.id} name={s.name} personal={s.kind === "personal"} count={s.document_count} />
        ))}

        <div className="pt-3">
          <NavItem to="/trash" icon={<Trash2 />} label="Trash" />
        </div>
      </nav>
      <div className="border-t border-border p-2">
        <UserMenu />
      </div>
    </aside>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return <div className="px-2.5 pb-1 pt-5 text-[11px] font-semibold uppercase tracking-wider text-subtle">{children}</div>;
}

function SpaceLink({ id, name, personal, count }: { id: string; name: string; personal: boolean; count: number }) {
  const active = useRouterState({
    select: (s) => s.location.pathname === "/documents" && (s.location.search as { space_id?: string[] }).space_id?.[0] === id,
  });
  return (
    <Link
      to="/documents"
      search={{ space_id: [id] }}
      data-status={active ? "active" : undefined}
      className="flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm text-muted hover:bg-surface-2 hover:text-fg data-[status=active]:bg-surface-2 data-[status=active]:font-medium data-[status=active]:text-fg"
    >
      <span className={cn("flex size-[18px] items-center justify-center rounded text-[10px] font-bold text-white", personal ? "bg-slate-500" : "bg-accent")}>
        {personal ? <Users className="size-3" /> : name[0]?.toUpperCase()}
      </span>
      <span className="flex-1 truncate">{personal ? "Personal" : name}</span>
      <span className="text-xs text-subtle">{count || ""}</span>
    </Link>
  );
}

export function Logo({ className }: { className?: string }) {
  return (
    <Link to="/" className={cn("flex items-center gap-2 font-semibold tracking-tight", className)} aria-label="Docveta home">
      <svg viewBox="0 0 32 32" className="size-7" aria-hidden>
        <rect width="32" height="32" rx="8" className="fill-accent" />
        <path d="M10 8h8l5 5v11a1 1 0 0 1-1 1H10a1 1 0 0 1-1-1V9a1 1 0 0 1 1-1Z" fill="white" />
        <path d="M18 8v5h5" fill="none" stroke="currentColor" strokeWidth="1.2" className="text-accent" />
        <path d="M12.5 17h7M12.5 20h5" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" className="text-accent" />
      </svg>
      <span className="text-[17px]">Docveta</span>
    </Link>
  );
}

function UserMenu({ compact }: { compact?: boolean }) {
  const me = useCurrentUser();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { theme, setTheme } = useUI();
  React.useEffect(() => applyTheme(theme), [theme]);

  const logout = async () => {
    await api.post("/auth/logout").catch(() => undefined);
    qc.clear();
    navigate({ to: "/login" });
  };
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          className={cn("flex w-full items-center gap-2.5 rounded-md p-1.5 text-left hover:bg-surface-2", compact && "w-auto")}
          aria-label="Account menu"
        >
          <Avatar name={me.display_name} className="size-7" />
          {!compact && (
            <>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-medium">{me.display_name}</span>
                <span className="block truncate text-xs text-subtle">{me.email}</span>
              </span>
              <ChevronDown className="size-4 text-subtle" />
            </>
          )}
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align={compact ? "end" : "start"} side={compact ? "bottom" : "top"} className="w-60">
        <DropdownMenuLabel>{me.email}</DropdownMenuLabel>
        <DropdownMenuItem onSelect={() => navigate({ to: "/settings/$section", params: { section: "profile" } })}>
          <Settings /> Settings
        </DropdownMenuItem>
        {me.is_admin && (
          <DropdownMenuItem onSelect={() => navigate({ to: "/admin/$section", params: { section: "users" } })}>
            <Shield /> Administration
          </DropdownMenuItem>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuLabel>Theme</DropdownMenuLabel>
        <div className="grid grid-cols-3 gap-1 px-1 pb-1">
          {(
            [
              ["light", <Sun key="l" />, "Light"],
              ["dark", <Moon key="d" />, "Dark"],
              ["system", <Monitor key="s" />, "Auto"],
            ] as const
          ).map(([t, icon, label]) => (
            <button
              key={t}
              onClick={() => {
                setTheme(t);
                api.patch("/me", { theme: t }).catch(() => undefined);
              }}
              className={cn(
                "flex flex-col items-center gap-1 rounded-md py-2 text-xs text-muted hover:bg-surface-2 [&_svg]:size-4",
                theme === t && "bg-surface-2 text-fg font-medium",
              )}
            >
              {icon}
              {label}
            </button>
          ))}
        </div>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={logout}>
          <LogOut /> Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/* ---------------------------------------------------------------- Top bar */

function TopBar() {
  const setPaletteOpen = useUI((s) => s.setPaletteOpen);
  const pick = useFilePicker();
  return (
    <header className="sticky top-0 z-30 flex h-14 shrink-0 items-center gap-2 border-b border-border bg-bg/85 px-3 backdrop-blur sm:px-4">
      <div className="lg:hidden">
        <Logo />
      </div>
      <button
        onClick={() => setPaletteOpen(true)}
        className="ml-auto flex h-9 w-9 items-center justify-center gap-2 rounded-md border border-border bg-surface text-sm text-subtle hover:border-border-strong sm:w-auto sm:min-w-72 sm:justify-start sm:px-3 lg:ml-0"
        aria-label="Search documents"
      >
        <Search className="size-4" />
        <span className="hidden flex-1 text-left sm:inline">Search documents…</span>
        <Kbd className="hidden sm:inline">{modKey} K</Kbd>
      </button>
      <div className="hidden flex-1 lg:block" />
      <Button variant="primary" className="hidden sm:inline-flex" onClick={() => pick()}>
        <Upload /> Upload
      </Button>
      <NotificationsButton />
      <div className="lg:hidden">
        <UserMenu compact />
      </div>
    </header>
  );
}

/* ---------------------------------------------------------------- Bottom nav (mobile) */

function BottomNav() {
  const stats = useStats();
  const pick = useFilePicker();
  const setPaletteOpen = useUI((s) => s.setPaletteOpen);
  const item = "flex flex-1 flex-col items-center gap-0.5 py-2 text-[11px] text-muted data-[status=active]:text-accent [&_svg]:size-[22px]";
  return (
    <nav className="fixed inset-x-0 bottom-0 z-40 border-t border-border bg-surface/95 pb-safe backdrop-blur lg:hidden" aria-label="Main">
      <div className="mx-auto flex max-w-md items-stretch">
        <Link to="/" activeOptions={{ exact: true }} className={item}>
          <Home /> Home
        </Link>
        <Link to="/inbox" className={cn(item, "relative")}>
          <Inbox /> Inbox
          {!!stats.data?.inbox && (
            <span className="absolute right-[calc(50%-20px)] top-1 min-w-4 rounded-full bg-accent px-1 text-[10px] font-semibold text-accent-fg">
              {stats.data.inbox > 99 ? "99+" : stats.data.inbox}
            </span>
          )}
        </Link>
        <div className="flex flex-1 items-center justify-center">
          <button
            onClick={() => pick("application/pdf,image/*,.heic,.heif")}
            className="-mt-5 flex size-13 items-center justify-center rounded-full bg-accent text-accent-fg shadow-lg active:scale-95"
            aria-label="Upload or scan"
          >
            <Plus className="size-7" />
          </button>
        </div>
        <button onClick={() => setPaletteOpen(true)} className={item}>
          <Search /> Search
        </button>
        <Link to="/documents" className={item}>
          <FileText /> Documents
        </Link>
      </div>
    </nav>
  );
}

/** Page header used by most pages. */
export function PageHeader({ title, description, actions, className }: { title: React.ReactNode; description?: React.ReactNode; actions?: React.ReactNode; className?: string }) {
  return (
    <div className={cn("flex flex-wrap items-end justify-between gap-3 px-4 pb-4 pt-5 sm:px-6 sm:pt-7", className)}>
      <div className="min-w-0">
        <h1 className="text-xl font-semibold tracking-tight sm:text-2xl">{title}</h1>
        {description && <p className="mt-1 text-sm text-muted">{description}</p>}
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  );
}
