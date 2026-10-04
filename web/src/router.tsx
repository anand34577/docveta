import * as React from "react";
import { createRootRoute, createRoute, createRouter, lazyRouteComponent, Outlet } from "@tanstack/react-router";
import { AppLayout } from "@/components/app-shell";
import { NotFound } from "@/pages/not-found";
import type { DocQuery } from "@/lib/types";

// Rarely used, heavier pages load on demand.
// Every page loads on demand, so the first screen only downloads the shell and what it shows.
const LoginPage = lazyRouteComponent(() => import("@/pages/login"), "LoginPage");
const SetupPage = lazyRouteComponent(() => import("@/pages/setup"), "SetupPage");
const InvitePage = lazyRouteComponent(() => import("@/pages/invite"), "InvitePage");
const PublicSharePage = lazyRouteComponent(() => import("@/pages/public-share"), "PublicSharePage");
const HomePage = lazyRouteComponent(() => import("@/pages/home"), "HomePage");
const InboxPage = lazyRouteComponent(() => import("@/pages/inbox"), "InboxPage");
const DocumentsPage = lazyRouteComponent(() => import("@/pages/documents"), "DocumentsPage");
const DocumentPage = lazyRouteComponent(() => import("@/pages/document"), "DocumentPage");
const SavedViewPage = lazyRouteComponent(() => import("@/pages/saved-view"), "SavedViewPage");
const NotificationsPage = lazyRouteComponent(() => import("@/pages/notifications"), "NotificationsPage");
const TrashDocuments = React.lazy(() => import("@/pages/documents").then((m) => ({ default: m.DocumentsPage })));
const SettingsPage = lazyRouteComponent(() => import("@/pages/settings"), "SettingsPage");
const SpaceSettingsPage = lazyRouteComponent(() => import("@/pages/space-settings"), "SpaceSettingsPage");
const AdminPage = lazyRouteComponent(() => import("@/pages/admin"), "AdminPage");
const AskPage = lazyRouteComponent(() => import("@/pages/ask"), "AskPage");

const rootRoute = createRootRoute({ component: Outlet, notFoundComponent: NotFound });

export const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  validateSearch: (s: Record<string, unknown>): { redirect?: string; error?: string } => ({
    redirect: typeof s.redirect === "string" ? s.redirect : undefined,
    error: typeof s.error === "string" ? s.error : undefined,
  }),
  component: LoginPage,
});

const inviteRoute = createRoute({ getParentRoute: () => rootRoute, path: "/invite/$token", component: InvitePage });
const shareRoute = createRoute({ getParentRoute: () => rootRoute, path: "/s/$token", component: PublicSharePage });

const setupRoute = createRoute({ getParentRoute: () => rootRoute, path: "/setup", component: SetupPage });

const appRoute = createRoute({ getParentRoute: () => rootRoute, id: "app", component: AppLayout });

const homeRoute = createRoute({ getParentRoute: () => appRoute, path: "/", component: HomePage });
const inboxRoute = createRoute({ getParentRoute: () => appRoute, path: "/inbox", component: InboxPage });

const arr = (v: unknown): string[] | undefined =>
  Array.isArray(v) ? v.map(String) : typeof v === "string" && v ? v.split(",") : undefined;

export function parseDocQuery(s: Record<string, unknown>): DocQuery {
  return {
    q: typeof s.q === "string" && s.q ? s.q : undefined,
    space_id: arr(s.space_id),
    tag_id: arr(s.tag_id),
    correspondent_id: arr(s.correspondent_id),
    document_type_id: arr(s.document_type_id),
    date_from: typeof s.date_from === "string" ? s.date_from : undefined,
    date_to: typeof s.date_to === "string" ? s.date_to : undefined,
    inbox: s.inbox === true || s.inbox === "true" ? true : undefined,
    status: typeof s.status === "string" ? s.status : undefined,
    untagged: s.untagged === true || s.untagged === "true" ? true : undefined,
    trash: s.trash === true || s.trash === "true" ? true : undefined,
    sort: typeof s.sort === "string" ? s.sort : undefined,
    mode: s.mode === "semantic" || s.mode === "hybrid" ? s.mode : undefined,
  };
}

export const documentsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/documents",
  validateSearch: parseDocQuery,
  component: DocumentsPage,
});

export const documentRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/documents/$id",
  validateSearch: (s: Record<string, unknown>): { page?: number; q?: string } => ({
    page: Number(s.page) > 0 ? Number(s.page) : undefined,
    q: typeof s.q === "string" ? s.q : undefined,
  }),
  component: DocumentPage,
});

export const viewRoute = createRoute({ getParentRoute: () => appRoute, path: "/views/$id", component: SavedViewPage });

export const trashRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/trash",
  component: () => (
    <React.Suspense fallback={null}>
      <TrashDocuments trash />
    </React.Suspense>
  ),
});

export const settingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/$section",
  component: SettingsPage,
});

const settingsIndex = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings",
  component: SettingsPage,
});

export const spaceSettingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/spaces/$id/$section",
  component: SpaceSettingsPage,
});

const askRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/ask",
  // ?doc=<id>: ask about one document (from the document page).
  validateSearch: (s: Record<string, unknown>): { doc?: string } => ({ doc: typeof s.doc === "string" && /^[0-9a-f-]{36}$/i.test(s.doc) ? s.doc : undefined }),
  component: AskPage,
});
const notificationsRoute = createRoute({ getParentRoute: () => appRoute, path: "/notifications", component: NotificationsPage });

export const adminRoute = createRoute({ getParentRoute: () => appRoute, path: "/admin/$section", component: AdminPage });
const adminIndex = createRoute({ getParentRoute: () => appRoute, path: "/admin", component: AdminPage });

const routeTree = rootRoute.addChildren([
  loginRoute,
  setupRoute,
  inviteRoute,
  shareRoute,
  appRoute.addChildren([
    homeRoute,
    inboxRoute,
    documentsRoute,
    documentRoute,
    viewRoute,
    trashRoute,
    settingsIndex,
    settingsRoute,
    spaceSettingsRoute,
    adminIndex,
    adminRoute,
    askRoute,
    notificationsRoute,
  ]),
]);

export const router = createRouter({ routeTree, defaultPreload: "intent", scrollRestoration: true });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
