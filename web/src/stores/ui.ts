import { create } from "zustand";

type Theme = "system" | "light" | "dark";

function readLocal<T extends string>(key: string, fallback: T): T {
  try {
    return (localStorage.getItem(key) as T) || fallback;
  } catch {
    return fallback;
  }
}

function writeLocal(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* private mode */
  }
}

const media = typeof window !== "undefined" ? window.matchMedia("(prefers-color-scheme: dark)") : null;

export function applyTheme(t: Theme) {
  const dark = t === "dark" || (t === "system" && !!media?.matches);
  document.documentElement.classList.toggle("dark", dark);
}

interface UIState {
  theme: Theme;
  setTheme: (t: Theme) => void;
  layout: "grid" | "list";
  setLayout: (l: "grid" | "list") => void;
  paletteOpen: boolean;
  setPaletteOpen: (o: boolean) => void;
  uploadSpaceId?: string;
  setUploadSpaceId: (id?: string) => void;
}

export const useUI = create<UIState>((set) => ({
  theme: readLocal<Theme>("docveta.theme", "system"),
  setTheme: (theme) => {
    writeLocal("docveta.theme", theme);
    applyTheme(theme);
    set({ theme });
  },
  layout: readLocal<"grid" | "list">("docveta.layout", "grid"),
  setLayout: (layout) => {
    writeLocal("docveta.layout", layout);
    set({ layout });
  },
  paletteOpen: false,
  setPaletteOpen: (paletteOpen) => set({ paletteOpen }),
  uploadSpaceId: readLocal<string>("docveta.uploadSpace", "") || undefined,
  setUploadSpaceId: (id) => {
    writeLocal("docveta.uploadSpace", id ?? "");
    set({ uploadSpaceId: id });
  },
}));

media?.addEventListener("change", () => applyTheme(useUI.getState().theme));

/** Selected documents for bulk actions. */
interface SelectionState {
  ids: Set<string>;
  toggle: (id: string) => void;
  set: (ids: string[]) => void;
  clear: () => void;
}

export const useSelection = create<SelectionState>((set) => ({
  ids: new Set(),
  toggle: (id) =>
    set((s) => {
      const ids = new Set(s.ids);
      if (ids.has(id)) ids.delete(id);
      else ids.add(id);
      return { ids };
    }),
  set: (list) => set({ ids: new Set(list) }),
  clear: () => set({ ids: new Set() }),
}));
