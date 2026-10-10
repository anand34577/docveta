// What this browser opened and searched for lately, so it can be offered again. Kept in the
// browser only (never sent anywhere) and forgotten on sign-out.

export interface RecentDoc {
  id: string;
  title: string;
}

const DOCS = "docveta.recentDocs";
const SEARCHES = "docveta.recentSearches";
const MAX = 8;

function read<T>(key: string): T[] {
  try {
    const v = JSON.parse(localStorage.getItem(key) ?? "[]");
    return Array.isArray(v) ? v : [];
  } catch {
    return [];
  }
}

function write(key: string, value: unknown[]) {
  try {
    if (value.length) localStorage.setItem(key, JSON.stringify(value.slice(0, MAX)));
    else localStorage.removeItem(key);
  } catch {
    /* private mode */
  }
}

/** Documents opened here, newest first. */
export function recentDocs(): RecentDoc[] {
  return read<RecentDoc>(DOCS).filter((d) => d && typeof d.id === "string" && typeof d.title === "string");
}

export function rememberDoc(d: RecentDoc) {
  write(DOCS, [{ id: d.id, title: d.title }, ...recentDocs().filter((x) => x.id !== d.id)]);
}

/** A document that is gone (deleted, or no longer shared with you). */
export function forgetDoc(id: string) {
  write(DOCS, recentDocs().filter((x) => x.id !== id));
}

/** Searches that led to a document, newest first. */
export function recentSearches(): string[] {
  return read<string>(SEARCHES).filter((s) => typeof s === "string");
}

export function rememberSearch(q: string) {
  const t = q.trim();
  if (t.length < 2) return;
  write(SEARCHES, [t, ...recentSearches().filter((s) => s.toLowerCase() !== t.toLowerCase())]);
}

export function clearRecentSearches() {
  write(SEARCHES, []);
}

export function clearRecent() {
  write(DOCS, []);
  write(SEARCHES, []);
}
