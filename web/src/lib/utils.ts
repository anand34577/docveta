import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";
import { format, formatDistanceToNowStrict, isToday, isYesterday, parseISO } from "date-fns";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]}`;
}

const dateFormats: Record<string, string> = {
  "DD/MM/YYYY": "dd/MM/yyyy",
  "MM/DD/YYYY": "MM/dd/yyyy",
  "YYYY-MM-DD": "yyyy-MM-dd",
  "DD.MM.YYYY": "dd.MM.yyyy",
  "D MMM YYYY": "d MMM yyyy",
};

let userDateFormat = "dd/MM/yyyy";
export function setDateFormat(f: string) {
  userDateFormat = dateFormats[f] || "dd/MM/yyyy";
}

/** Formats a date-only string (YYYY-MM-DD) without timezone shifts. */
export function formatDocDate(d: string | null | undefined): string {
  if (!d) return "";
  const [y, m, day] = d.split("-").map(Number);
  return format(new Date(y, m - 1, day), userDateFormat);
}

export function formatDateTime(iso: string): string {
  const d = parseISO(iso);
  if (isToday(d)) return `Today, ${format(d, "HH:mm")}`;
  if (isYesterday(d)) return `Yesterday, ${format(d, "HH:mm")}`;
  return format(d, `${userDateFormat}, HH:mm`);
}

export function timeAgo(iso: string): string {
  return formatDistanceToNowStrict(parseISO(iso), { addSuffix: true });
}

export function initials(name: string): string {
  const parts = name.trim().split(/\s+/);
  return ((parts[0]?.[0] || "") + (parts.length > 1 ? parts[parts.length - 1][0] : "")).toUpperCase() || "?";
}

export const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);
export const modKey = isMac ? "⌘" : "Ctrl";

/** Tag colour classes (full literal class names so Tailwind picks them up). */
export const tagColors: Record<string, string> = {
  slate: "bg-slate-100 text-slate-700 dark:bg-slate-400/15 dark:text-slate-300",
  red: "bg-red-100 text-red-800 dark:bg-red-400/15 dark:text-red-300",
  orange: "bg-orange-100 text-orange-800 dark:bg-orange-400/15 dark:text-orange-300",
  amber: "bg-amber-100 text-amber-800 dark:bg-amber-400/15 dark:text-amber-300",
  lime: "bg-lime-100 text-lime-800 dark:bg-lime-400/15 dark:text-lime-300",
  green: "bg-green-100 text-green-800 dark:bg-green-400/15 dark:text-green-300",
  teal: "bg-teal-100 text-teal-800 dark:bg-teal-400/15 dark:text-teal-300",
  cyan: "bg-cyan-100 text-cyan-800 dark:bg-cyan-400/15 dark:text-cyan-300",
  blue: "bg-blue-100 text-blue-800 dark:bg-blue-400/15 dark:text-blue-300",
  indigo: "bg-indigo-100 text-indigo-800 dark:bg-indigo-400/15 dark:text-indigo-300",
  violet: "bg-violet-100 text-violet-800 dark:bg-violet-400/15 dark:text-violet-300",
  pink: "bg-pink-100 text-pink-800 dark:bg-pink-400/15 dark:text-pink-300",
};

export const tagDot: Record<string, string> = {
  slate: "bg-slate-400", red: "bg-red-500", orange: "bg-orange-500", amber: "bg-amber-500", lime: "bg-lime-500",
  green: "bg-green-500", teal: "bg-teal-500", cyan: "bg-cyan-500", blue: "bg-blue-500", indigo: "bg-indigo-500",
  violet: "bg-violet-500", pink: "bg-pink-500",
};

export const colorNames = Object.keys(tagColors);

export function fileKind(mime: string): "pdf" | "image" | "text" | "other" {
  if (mime === "application/pdf") return "pdf";
  if (mime.startsWith("image/")) return "image";
  if (mime === "text/plain") return "text";
  return "other";
}

/** Browser-renderable images (HEIC/AVIF may not be). */
export function browserImage(mime: string): boolean {
  return ["image/jpeg", "image/png", "image/webp", "image/gif", "image/bmp", "image/avif"].includes(mime);
}
