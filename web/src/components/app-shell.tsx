import * as React from "react";
import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import {
  Bookmark,
  ChevronsUpDown,
  MessageSquareText,
  FileText,
  Home,
  Inbox,
  LogOut,
  Menu,
  Monitor,
  Moon,
  Plus,
  Search,
  Settings,
  Shield,
  Sun,
  Trash2,
  Upload,
  User,
} from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { api, setUnauthorizedHandler } from "@/lib/api";
import { useAIEnabled, useMe, useSavedViews, useStats, useStatus } from "@/lib/queries";
import { isAuthPage, safeRedirect } from "@/lib/redirect";
import { cn, modKey, setDateFormat, spaceDot, spaceLabel } from "@/lib/utils";
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
  Dialog,
  DialogContent,
} from "@/components/ui/overlay";
const CommandPalette = React.lazy(() => import("./command-palette").then((m) => ({ default: m.CommandPalette })));
import { NewSpaceDialog } from "./new-space-dialog";
import { ShortcutsHelp } from "./shortcuts-help";
import { OcrWarning } from "./ocr-warning";
import { DropOverlay, UploadChooser, UploadTray } from "./upload-tray";
import { NotificationsButton, useLiveEvents } from "./notifications";
import type { Me, Space } from "@/lib/types";
import { parseDocQuery } from "@/router";

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
      if (!isAuthPage(window.location.pathname)) navigate({ to: "/login", search: { redirect: safeRedirect(here) } });
    });
  }, [navigate]);

  React.useEffect(() => {
    if (status.data?.setup_needed) navigate({ to: "/setup" });
  }, [status.data, navigate]);

  React.useEffect(() => {
    // Already on its way to the login page: going again would wrap the URL in another
    // ?redirect= each time (and send people back to the login page after signing in).
    if (me.error && (me.error as { status?: number }).status === 401 && !isAuthPage(location.pathname)) {
      navigate({ to: "/login", search: { redirect: safeRedirect(location.href) } });
    }
  }, [me.error, navigate, location.href, location.pathname]);

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
  const request = useUploads((s) => s.request);
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
      if (files.length) request(files, "share");
      navigate({ to: "/inbox", replace: true });
    })().catch(() => undefined);
  }, []);
}

function Shell() {
  const setPaletteOpen = useUI((s) => s.setPaletteOpen);
  const navigate = useNavigate();
  const [help, setHelp] = React.useState(false);
  const paletteOpen = useUI((s) => s.paletteOpen);
  const [paletteLoaded, setPaletteLoaded] = React.useState(false); // cmdk is only downloaded the first time it is opened
  React.useEffect(() => {
    if (paletteOpen) setPaletteLoaded(true);
  }, [paletteOpen]);
  useLiveEvents();
  useSharedFiles();

  // "?" lists the shortcuts; "G" then I / D / H jumps between pages.
  React.useEffect(() => {
    let g = 0;
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName) || t.isContentEditable || e.metaKey || e.ctrlKey || e.altKey) return;
      if (e.key === "?") {
        e.preventDefault();
        setHelp(true);
      } else if (e.key === "g") {
        g = Date.now();
      } else if (g && Date.now() - g < 1200) {
        const to = { i: "/inbox", d: "/documents", h: "/" }[e.key.toLowerCase()];
        g = 0;
        if (to) {
          e.preventDefault();
          navigate({ to });
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [navigate]);

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
        <OcrWarning />
        <main id="main" className="relative flex-1 overflow-y-auto scrollbar-thin pb-20 lg:pb-0">
          <Outlet />
        </main>
      </div>
      <BottomNav />
      {paletteLoaded && (
        <React.Suspense fallback={null}>
          <CommandPalette />
        </React.Suspense>
      )}
      <UploadTray />
      <UploadChooser />
      <ShortcutsHelp open={help} onOpenChange={setHelp} />
      <DropOverlay />
    </div>
  );
}

/* ---------------------------------------------------------------- Upload button (file picker) */

export function useFilePicker() {
  const request = useUploads((s) => s.request);
  return React.useCallback(
    (accept = "application/pdf,image/*,.heic,.heif,.txt,.tif,.tiff") => {
      const input = document.createElement("input");
      input.type = "file";
      input.multiple = true;
      input.accept = accept;
      input.onchange = () => {
        const files = Array.from(input.files ?? []);
        if (files.length) request(files);
      };
      input.click();
    },
    [request],
  );
}

/* ---------------------------------------------------------------- Sidebar (desktop) */

const navItem =
  "group flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm text-muted transition-colors hover:bg-surface-2 hover:text-fg " +
  "data-[status=active]:bg-surface data-[status=active]:font-medium data-[status=active]:text-fg data-[status=active]:shadow-sm [&_svg]:size-[18px] [&_svg]:shrink-0";

function NavItem({ to, icon, label, count, exact, active, onClick }: { to: string; icon: React.ReactNode; label: string; count?: number; exact?: boolean; active?: boolean; onClick?: () => void }) {
  return (
    <Link
      to={to}
      onClick={onClick}
      activeOptions={{ exact: exact ?? false, includeSearch: false }}
      // The router marks the link active by path; `active={false}` overrides that look.
      className={cn(navItem, active === false && "data-[status=active]:bg-transparent data-[status=active]:font-normal data-[status=active]:text-muted data-[status=active]:shadow-none")}
    >
      {icon}
      <span className="flex-1 truncate">{label}</span>
      {!!count && <span className="rounded-full bg-accent px-1.5 text-[11px] font-semibold leading-[18px] text-accent-fg tabular-nums">{count > 99 ? "99+" : count}</span>}
    </Link>
  );
}

/** Library, saved views, spaces and account links: the desktop sidebar and the mobile "More" sheet. */
function NavLinks({ onNavigate }: { onNavigate?: () => void }) {
  const me = useCurrentUser();
  const stats = useStats();
  const views = useSavedViews();
  const pinned = (views.data ?? []).filter((v) => v.pinned);
  const ai = useAIEnabled().data;
  const [newSpace, setNewSpace] = React.useState(false);
  // "All documents" isn't active while a single space is shown: that space's link is.
  const allDocs = useRouterState({ select: (s) => s.location.pathname === "/documents" && !parseDocQuery(s.location.search).space_id?.length });
  return (
    <>
      <div className="space-y-0.5">
        <NavItem to="/" exact icon={<Home />} label="Home" onClick={onNavigate} />
        <NavItem to="/inbox" icon={<Inbox />} label="Inbox" count={stats.data?.inbox} onClick={onNavigate} />
        <NavItem to="/documents" icon={<FileText />} label="All documents" active={allDocs} onClick={onNavigate} />
        {ai?.chat && <NavItem to="/ask" icon={<MessageSquareText />} label="Ask your documents" onClick={onNavigate} />}
      </div>

      {pinned.length > 0 && (
        <>
          <SectionLabel action={<Link to="/settings/$section" params={{ section: "views" }} onClick={onNavigate} className="normal-case tracking-normal text-subtle hover:text-fg">Manage</Link>}>Saved views</SectionLabel>
          <div className="space-y-0.5">
            {pinned.map((v) => (
              <NavItem key={v.id} to={`/views/${v.id}`} icon={<Bookmark />} label={v.name} onClick={onNavigate} />
            ))}
          </div>
        </>
      )}

      <SectionLabel
        action={
          <button onClick={() => setNewSpace(true)} className="flex items-center gap-0.5 rounded px-1 normal-case tracking-normal text-subtle hover:bg-surface-2 hover:text-fg" aria-label="New space">
            <Plus className="size-3.5" /> New
          </button>
        }
      >
        Spaces
      </SectionLabel>
      <div className="space-y-0.5">
        {me.spaces.map((s) => (
          <SpaceLink key={s.id} space={s} onNavigate={onNavigate} />
        ))}
      </div>
      {newSpace && <NewSpaceDialog onClose={() => setNewSpace(false)} />}

      <div className="mt-5 space-y-0.5 border-t border-border pt-3">
        <NavItem to="/trash" icon={<Trash2 />} label="Trash" onClick={onNavigate} />
        <NavItem to="/settings/profile" icon={<Settings />} label="Settings" onClick={onNavigate} />
        {me.is_admin && <NavItem to="/admin/users" icon={<Shield />} label="Administration" onClick={onNavigate} />}
      </div>
    </>
  );
}

function Sidebar() {
  const pick = useFilePicker();
  return (
    <aside className="hidden w-64 shrink-0 flex-col border-r border-border bg-sidebar lg:flex">
      <div className="flex h-14 items-center px-4">
        <Logo />
      </div>
      <div className="px-3 pb-3">
        <Button variant="primary" className="w-full" onClick={() => pick()}>
          <Upload /> Upload
        </Button>
      </div>
      <nav className="flex-1 overflow-y-auto scrollbar-thin px-3 pb-4" aria-label="Main">
        <NavLinks />
      </nav>
      <div className="border-t border-border p-2">
        <UserMenu />
      </div>
    </aside>
  );
}

function SectionLabel({ children, action }: { children: React.ReactNode; action?: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between px-2.5 pb-1.5 pt-5 text-[11px] font-semibold uppercase tracking-wider text-subtle">
      {children}
      {action && <span className="text-xs font-medium">{action}</span>}
    </div>
  );
}

function SpaceLink({ space, onNavigate }: { space: Space; onNavigate?: () => void }) {
  const active = useRouterState({
    select: (s) => s.location.pathname === "/documents" && parseDocQuery(s.location.search).space_id?.[0] === space.id,
  });
  const personal = space.kind === "personal";
  const owner = space.role === "owner";
  return (
    <div className="group/space relative">
      <Link to="/documents" search={{ space_id: [space.id] }} onClick={onNavigate} data-status={active ? "active" : undefined} className={navItem}>
        <span className={cn("flex size-[18px] shrink-0 items-center justify-center rounded-[5px] text-[10px] font-bold text-white", personal ? "bg-slate-500" : spaceDot(space.color))}>
          {personal ? <User className="!size-3" /> : space.name[0]?.toUpperCase()}
        </span>
        <span className="flex-1 truncate">{spaceLabel(space)}</span>
        <span className={cn("text-xs tabular-nums text-subtle", owner && "lg:group-hover/space:invisible lg:group-focus-within/space:invisible")}>{space.document_count || ""}</span>
      </Link>
      {owner && (
        <Link
          to="/spaces/$id/$section"
          params={{ id: space.id, section: "general" }}
          onClick={onNavigate}
          className="absolute right-1.5 top-1/2 -translate-y-1/2 rounded p-1 text-subtle hover:bg-surface-3 hover:text-fg lg:opacity-0 lg:focus-visible:opacity-100 lg:group-hover/space:opacity-100"
          aria-label={`${spaceLabel(space)} settings`}
        >
          <Settings className="size-3.5" />
        </Link>
      )}
    </div>
  );
}

export function LogoMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" className={cn("size-7", className)} aria-hidden>
      <rect width="32" height="32" rx="8" className="fill-accent" />
      <path d="M10 8h8l5 5v11a1 1 0 0 1-1 1H10a1 1 0 0 1-1-1V9a1 1 0 0 1 1-1Z" fill="white" />
      <path d="M18 8v5h5" fill="none" stroke="currentColor" strokeWidth="1.2" className="text-accent" />
      <path d="M12.5 17h7M12.5 20h5" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" className="text-accent" />
    </svg>
  );
}

export function Logo({ className }: { className?: string }) {
  return (
    <Link to="/" className={cn("flex items-center gap-2 font-semibold tracking-tight", className)} aria-label="Docveta home">
      <LogoMark />
      <span className="text-[17px]">Docveta</span>
    </Link>
  );
}

function UserMenu({ compact }: { compact?: boolean }) {
  const me = useCurrentUser();
  const navigate = useNavigate();
  const qc = useQueryClient();

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
              <ChevronsUpDown className="size-4 text-subtle" />
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
        <div className="px-1 pb-1">
          <ThemePicker />
        </div>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={logout}>
          <LogOut /> Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** Light / dark / auto, saved locally and to the account. Used in the account menu and Settings. */
export function ThemePicker() {
  const { theme, setTheme } = useUI();
  React.useEffect(() => applyTheme(theme), [theme]);
  return (
    <div className="grid grid-cols-3 gap-1" role="radiogroup" aria-label="Theme">
      {(
        [
          ["light", <Sun key="l" />, "Light"],
          ["dark", <Moon key="d" />, "Dark"],
          ["system", <Monitor key="s" />, "Auto"],
        ] as const
      ).map(([t, icon, label]) => (
        <button
          key={t}
          role="radio"
          aria-checked={theme === t}
          onClick={() => {
            setTheme(t);
            api.patch("/me", { theme: t }).catch(() => undefined);
          }}
          className={cn(
            "flex flex-col items-center gap-1 rounded-md border border-transparent py-2 text-xs text-muted hover:bg-surface-2 [&_svg]:size-4",
            theme === t && "border-border bg-surface-2 font-medium text-fg",
          )}
        >
          {icon}
          {label}
        </button>
      ))}
    </div>
  );
}

/* ---------------------------------------------------------------- Top bar */

function TopBar() {
  const setPaletteOpen = useUI((s) => s.setPaletteOpen);
  return (
    <header className="sticky top-0 z-30 flex h-14 shrink-0 items-center gap-2 border-b border-border bg-bg/85 page-x backdrop-blur">
      <div className="lg:hidden">
        <Logo />
      </div>
      <button
        onClick={() => setPaletteOpen(true)}
        className="ml-auto flex h-9 w-9 items-center justify-center gap-2 rounded-lg border border-border bg-surface text-sm text-subtle shadow-sm transition-colors hover:border-border-strong hover:text-muted sm:w-auto sm:min-w-72 sm:justify-start sm:px-3 lg:ml-0 lg:w-full lg:max-w-md"
        aria-label="Search documents"
      >
        <Search className="size-4 shrink-0" />
        <span className="hidden flex-1 text-left sm:inline">Search or jump to…</span>
        <Kbd className="hidden sm:inline">{modKey} K</Kbd>
      </button>
      <div className="hidden flex-1 lg:block" />
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
  const [more, setMore] = React.useState(false);
  const item = "flex flex-1 flex-col items-center gap-0.5 py-2 text-[11px] text-muted data-[status=active]:text-accent [&_svg]:size-[22px]";
  return (
    <>
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
              className="-mt-5 flex size-13 items-center justify-center rounded-full bg-accent text-accent-fg shadow-lg ring-4 ring-bg active:scale-95"
              aria-label="Upload or scan"
            >
              <Plus className="size-7" />
            </button>
          </div>
          <Link to="/documents" className={item}>
            <FileText /> Documents
          </Link>
          <button onClick={() => setMore(true)} className={item} aria-haspopup="dialog">
            <Menu /> More
          </button>
        </div>
      </nav>
      <Dialog open={more} onOpenChange={setMore}>
        <DialogContent title="Browse" className="lg:hidden">
          <nav aria-label="More">
            <NavLinks onNavigate={() => setMore(false)} />
          </nav>
        </DialogContent>
      </Dialog>
    </>
  );
}

/** Page header used by most pages. */
export function PageHeader({ title, description, actions, eyebrow, className }: { title: React.ReactNode; description?: React.ReactNode; actions?: React.ReactNode; eyebrow?: React.ReactNode; className?: string }) {
  return (
    <div className={cn("flex flex-wrap items-end justify-between gap-3 page-x pb-4 pt-6 sm:pt-8", className)}>
      <div className="min-w-0">
        {eyebrow && <div className="mb-1 text-[13px] font-medium text-subtle">{eyebrow}</div>}
        <h1 className="truncate text-xl font-semibold tracking-tight sm:text-2xl">{title}</h1>
        {description && <p className="mt-1 text-sm text-muted">{description}</p>}
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  );
}
