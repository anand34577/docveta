// Where to go after signing in. Only same-site paths, and never back to a sign-in page:
// that sent people to the login page again after single sign-on, and each round wrapped the
// address in another ?redirect=.

const authPages = ["/login", "/setup", "/invite/"];

/** Pages people see while signed out; they never become a place to return to. */
export function isAuthPage(path: string): boolean {
  return authPages.some((p) => path === p || path.startsWith(p.endsWith("/") ? p : p + "?") || path.startsWith(p + "/"));
}

/**
 * A safe path to return to after signing in, or undefined for the home page. A login address
 * (even nested many times, as older versions produced) is unwrapped to what it pointed at.
 */
export function safeRedirect(target: string | undefined | null): string | undefined {
  let t = target ?? "";
  for (let i = 0; i < 100 && isAuthPage(t.split("#")[0]); i++) {
    const inner = new URLSearchParams(t.split("?")[1] ?? "").get("redirect");
    if (!inner) return undefined;
    t = inner;
  }
  if (!t.startsWith("/") || t.startsWith("//") || t.startsWith("/\\") || isAuthPage(t) || t === "/") return undefined;
  return t;
}
