import { createRootRoute, createRoute, createRouter, lazyRouteComponent, Outlet } from "@tanstack/react-router";
import { AppLayout } from "@/components/app-shell";
import { LoginPage } from "@/pages/login";
import { SetupPage } from "@/pages/setup";
import { HomePage } from "@/pages/home";
import { InboxPage } from "@/pages/inbox";
import { DocumentsPage } from "@/pages/documents";
import { DocumentPage } from "@/pages/document";
import { SavedViewPage } from "@/pages/saved-view";
import { NotFound } from "@/pages/not-found";
import type { DocQuery } from "@/lib/types";

// Rarely used, heavier pages load on demand.
const SettingsPage = lazyRouteComponent(() => import("@/pages/settings"), "SettingsPage");
const SpaceSettingsPage = lazyRouteComponent(() => import("@/pages/space-settings"), "SpaceSettingsPage");
const AdminPage = lazyRouteComponent(() => import("@/pages/admin"), "AdminPage");

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
  component: () => <DocumentsPage trash />,
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

export const adminRoute = createRoute({ getParentRoute: () => appRoute, path: "/admin/$section", component: AdminPage });
const adminIndex = createRoute({ getParentRoute: () => appRoute, path: "/admin", component: AdminPage });

const routeTree = rootRoute.addChildren([
  loginRoute,
  setupRoute,
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
  ]),
]);

export const router = createRouter({ routeTree, defaultPreload: "intent", scrollRestoration: true });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
