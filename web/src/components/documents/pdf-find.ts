import { create } from "zustand";

/** A match to show: its page, its number on that page, and a counter that changes on every move. */
export interface FindTarget {
  q: string;
  page: number;
  n: number;
  seq: number;
}

/** Text selected in a document's Text tab, which the viewer highlights on the page. */
export const useLocate = create<{ docId?: string; target: FindTarget | null; set: (docId: string, target: FindTarget | null) => void }>((set) => ({
  target: null,
  set: (docId, target) => set({ docId, target }),
}));

/**
 * Find in a PDF. Matching ignores case and all spaces, so "amount due" also finds text that a
 * scan's text layer stores word by word ("Amount" "due") or split mid-word ("Elec" "tricity").
 */

export interface Squashed {
  text: string;
  piece: number[]; // for each character of text: which piece it came from
  off: number[]; // ...and where in that piece
}

/** Joins text pieces (a page's text items or text-layer spans) without spaces, lower-cased. */
export function squash(pieces: string[]): Squashed {
  let text = "";
  const piece: number[] = [];
  const off: number[] = [];
  pieces.forEach((p, i) => {
    for (let j = 0; j < p.length; j++) {
      const ch = p[j];
      if (/\s/.test(ch)) continue;
      for (const low of ch.toLowerCase()) {
        text += low;
        piece.push(i);
        off.push(j);
      }
    }
  });
  return { text, piece, off };
}

export const squashQuery = (q: string) => q.toLowerCase().replace(/\s+/g, "");

/** Start positions of q in text, not overlapping. */
export function findAll(text: string, q: string): number[] {
  const out: number[] = [];
  if (!q) return out;
  for (let i = text.indexOf(q); i >= 0; i = text.indexOf(q, i + q.length)) out.push(i);
  return out;
}

/**
 * Highlights every match of q in a page's text-layer spans, and the one numbered `active`
 * differently. Returns the active match's first highlight, to scroll to.
 */
export function markMatches(leaves: HTMLElement[], originals: string[], q: string, active: number): HTMLElement | null {
  leaves.forEach((el, i) => {
    if (el.dataset.kzFind) {
      el.textContent = originals[i];
      delete el.dataset.kzFind;
    }
  });
  const query = squashQuery(q);
  if (!query) return null;
  const s = squash(originals);
  // Per span: [from, to, which match] ranges in its own text.
  const ranges = new Map<number, [number, number, number][]>();
  findAll(s.text, query).forEach((start, m) => {
    for (let k = start; k < start + query.length; k++) {
      const p = s.piece[k];
      const o = s.off[k];
      const list = ranges.get(p) ?? [];
      const last = list[list.length - 1];
      if (last && last[2] === m) last[1] = o + 1; // same match, same span: also covers spaces inside it
      else list.push([o, o + 1, m]);
      ranges.set(p, list);
    }
  });
  let first: HTMLElement | null = null;
  for (const [p, list] of ranges) {
    const el = leaves[p];
    const text = originals[p];
    const parts: Node[] = [];
    let at = 0;
    for (const [from, to, m] of list) {
      if (from > at) parts.push(document.createTextNode(text.slice(at, from)));
      const mark = document.createElement("mark");
      mark.className = m === active ? "kz-find kz-find-active" : "kz-find";
      mark.textContent = text.slice(from, to);
      parts.push(mark);
      if (m === active && !first) first = mark;
      at = to;
    }
    if (at < text.length) parts.push(document.createTextNode(text.slice(at)));
    el.replaceChildren(...parts);
    el.dataset.kzFind = "1";
  }
  return first;
}
